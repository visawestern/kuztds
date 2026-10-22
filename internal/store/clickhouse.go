package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/egerkuzma/kuztds/internal/logbuf"
)

// CH — a ClickHouse client for batch-inserting events. Implements logbuf.Inserter.
type CH struct {
	conn driver.Conn
	// netCols: the events table has the asn/org/timezone/via columns. They were
	// added after the first release, so a database created before that has
	// them only if the ALTER in ensureSchema went through. Writes and reads
	// both consult this rather than assume — an INSERT naming a column that is
	// not there fails as a whole, and that would cost every event.
	netCols bool
	// postbackCols: postbacks has status/tid/cost (migrations 004/006). Same rule:
	// fresh containers get them from /docker-entrypoint-initdb.d, older ones
	// from the ALTER below; a refused ALTER keeps the legacy column set.
	postbackCols bool
	// eventCostCols: events has cost/cost_cur (migration 005). Same rule.
	eventCostCols bool
	// eventOfferCols: events has offer (migration 007). Same rule.
	eventOfferCols bool
	// postbackOfferCols: postbacks has offer (migration 008). Same rule.
	postbackOfferCols bool
}

// OpenCH opens a connection to ClickHouse.
func OpenCH(addr, database, username, password string) (*CH, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:        []string{addr},
		Auth:        clickhouse.Auth{Database: database, Username: username, Password: password},
		DialTimeout: 5 * time.Second,
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("clickhouse: ping: %w", err)
	}
	c := &CH{conn: conn}
	c.ensureSchema(ctx)
	return c, nil
}

// netColumns — columns added to events after the first release.
const netColumns = `ADD COLUMN IF NOT EXISTS asn UInt32,
	ADD COLUMN IF NOT EXISTS org LowCardinality(String),
	ADD COLUMN IF NOT EXISTS timezone LowCardinality(String),
	ADD COLUMN IF NOT EXISTS via LowCardinality(String)`

// ensureSchema brings an older events table up to date and records whether it
// worked. The ALTER is idempotent and metadata-only, so running it on every
// start from both the engine and the admin is harmless. If it is refused (a
// read-only user, say), the client keeps working on the old column set.
func (c *CH) ensureSchema(ctx context.Context) {
	_ = c.conn.Exec(ctx, "ALTER TABLE events "+netColumns)
	var n uint64
	err := c.conn.QueryRow(ctx, `SELECT count() FROM system.columns
		WHERE database = currentDatabase() AND table = 'events'
		  AND name IN ('asn', 'org', 'timezone', 'via')`).Scan(&n)
	c.netCols = err == nil && n == 4
	_ = c.conn.Exec(ctx, `ALTER TABLE postbacks
		ADD COLUMN IF NOT EXISTS status LowCardinality(String),
		ADD COLUMN IF NOT EXISTS tid String,
		ADD COLUMN IF NOT EXISTS cost Float64,
		ADD COLUMN IF NOT EXISTS version DateTime64(3) DEFAULT now64(3)`)
	var m uint64
	merr := c.conn.QueryRow(ctx, `SELECT count() FROM system.columns
		WHERE database = currentDatabase() AND table = 'postbacks'
		  AND name IN ('status', 'tid', 'cost')`).Scan(&m)
	c.postbackCols = merr == nil && m == 3
	_ = c.conn.Exec(ctx, `ALTER TABLE events
		ADD COLUMN IF NOT EXISTS cost Float64,
		ADD COLUMN IF NOT EXISTS cost_cur LowCardinality(String)`)
	var k uint64
	kerr := c.conn.QueryRow(ctx, `SELECT count() FROM system.columns
		WHERE database = currentDatabase() AND table = 'events'
		  AND name IN ('cost', 'cost_cur')`).Scan(&k)
	c.eventCostCols = kerr == nil && k == 2
	if aerr := c.conn.Exec(ctx, `ALTER TABLE events ADD COLUMN IF NOT EXISTS offer String`); aerr == nil {
		var n uint64
		if qerr := c.conn.QueryRow(ctx, `SELECT count() FROM system.columns
			WHERE database = currentDatabase() AND table = 'events' AND name = 'offer'`).Scan(&n); qerr == nil {
			c.eventOfferCols = n == 1
		}
	}
	if aerr := c.conn.Exec(ctx, `ALTER TABLE postbacks ADD COLUMN IF NOT EXISTS offer String`); aerr == nil {
		var n uint64
		if qerr := c.conn.QueryRow(ctx, `SELECT count() FROM system.columns
			WHERE database = currentDatabase() AND table = 'postbacks' AND name = 'offer'`).Scan(&n); qerr == nil {
			c.postbackOfferCols = n == 1
		}
	}
	// G-066: rejected postbacks live in their own table. Creating it here as
	// well as in the migration keeps an existing database working after an
	// upgrade, the same way the ALTERs above do.
	_ = c.conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS postback_errors
	(
		ts     DateTime DEFAULT now(),
		reason LowCardinality(String),
		cid    String,
		status LowCardinality(String),
		tid    String,
		ip     String,
		query  String,
		detail String
	)
	ENGINE = MergeTree
	PARTITION BY toYYYYMMDD(ts)
	ORDER BY ts
	TTL ts + INTERVAL 90 DAY`)
}

// PostbackColumns reports whether postbacks carry status/tid.
func (c *CH) PostbackColumns() bool { return c != nil && c.postbackCols }

// NetworkColumns reports whether events carry asn/org/timezone/via.
func (c *CH) NetworkColumns() bool { return c != nil && c.netCols }

// Close closes the connection.
func (c *CH) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// StatsSummary — aggregates over a period (for the admin dashboard).
type StatsSummary struct {
	Total  int64 `json:"total"`
	Unique int64 `json:"unique"`
	Bots   int64 `json:"bots"`
}

// Summary computes event aggregates over a period. The query is parameterized (no
// concatenation) — unlike the string-built SQL in the old admin panel.
func (c *CH) Summary(ctx context.Context, from, to time.Time) (StatsSummary, error) {
	// count()/countIf() in ClickHouse are UInt64, so we scan into uint64.
	var total, uniq, bots uint64
	row := c.conn.QueryRow(ctx,
		`SELECT count() AS total,
		        countIf(uniq = 1) AS uniq,
		        countIf(bot != '' AND bot != '-') AS bots
		 FROM events
		 WHERE ts >= ? AND ts < ?`,
		from, to,
	)
	if err := row.Scan(&total, &uniq, &bots); err != nil {
		return StatsSummary{}, err
	}
	return StatsSummary{Total: int64(total), Unique: int64(uniq), Bots: int64(bots)}, nil
}

// TSPoint — a time-series point for charts.
type TSPoint struct {
	TS     time.Time `json:"ts"`
	Total  int64     `json:"total"`
	Unique int64     `json:"unique"`
	Bots   int64     `json:"bots"`
}

// TimeSeries — aggregates over time (stepSec-second step) for a period. Across all groups.
func (c *CH) TimeSeries(ctx context.Context, from, to time.Time, stepSec int) ([]TSPoint, error) {
	if stepSec <= 0 {
		stepSec = 3600
	}
	rows, err := c.conn.Query(ctx,
		`SELECT toStartOfInterval(ts, INTERVAL ? second) AS t,
		        count() AS total, countIf(uniq=1) AS uniq,
		        countIf(bot!='' AND bot!='-') AS bots
		 FROM events WHERE ts >= ? AND ts < ?
		 GROUP BY t ORDER BY t`,
		stepSec, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TSPoint
	for rows.Next() {
		var p TSPoint
		var total, uniq, bots uint64
		if err := rows.Scan(&p.TS, &total, &uniq, &bots); err != nil {
			return nil, err
		}
		p.Total, p.Unique, p.Bots = int64(total), int64(uniq), int64(bots)
		out = append(out, p)
	}
	return out, rows.Err()
}

// KV — a key/count pair for breakdowns.
type KV struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// breakdownDims — an allowlist of columns for grouping (protection against injection).
var breakdownDims = map[string]string{
	"device": "device", "country": "country", "os": "os",
	"browser": "browser", "brand": "brand", "operator": "operator",
	"group": "group_name", "stream": "stream_name", "bot": "bot",
	"domain": "domain", "city": "city", "lang": "lang",
	"region": "region", "redirect": "redirect",
}

// networkDims — breakdowns that need the asn/org/timezone columns.
var networkDims = map[string]string{
	"org": "org", "timezone": "timezone",
	"asn": "if(asn = 0, '', concat('AS', toString(asn)))",
}

// DeleteGroupLogs deletes a group's events (ALTER ... DELETE, asynchronous
// mutation). The key is the group's ID — the stable routing identifier the
// admin panel knows — not its display name, which is free text and may be
// empty or shared.
func (c *CH) DeleteGroupLogs(ctx context.Context, group string) error {
	return c.conn.Exec(ctx, "ALTER TABLE events DELETE WHERE group_id = ?", group)
}

// PerfRow — one group/stream cell of the performance table: traffic from
// events, conversions from postbacks. Stream "-" is the group's fallback
// (no stream matched).
type PerfRow struct {
	GroupID string  `json:"group_id"`
	Group   string  `json:"group"`
	Stream  string  `json:"stream"`
	Offer   string  `json:"offer"`
	Hits    int64   `json:"hits"`
	Unique  int64   `json:"unique"`
	Bots    int64   `json:"bots"`
	Conv    int64   `json:"conv"`
	Profit  float64 `json:"profit"`
	// Cost — USD spend on the visits (sum of events.cost, G-119).
	// Profit is net: postback revenue minus cost (0 cost = legacy numbers).
	Cost float64 `json:"cost"`
	// Trash/TrashProfit — conversions with status=trash: counted in Conv
	// but excluded from Profit (their amount rides here instead).
	Trash       int64   `json:"trash"`
	TrashProfit float64 `json:"trash_profit"`
	// Forwarded — visits this stream handed to another group (type "group").
	// They are logged under the group that served them; here they are added
	// to Hits/Unique/Bots of the stream that sent them on, so a flow's
	// numbers show everything that went through it.
	Forwarded int64 `json:"forwarded"`
}

// Performance aggregates events and postbacks per group and stream over a
// period. Two GROUP BY queries merged in Go rather than one JOIN: postbacks
// carry no group_id, and a JOIN across two MergeTree tables on free-text
// names is slower on ClickHouse than two scans plus a map lookup. Rows are
// ordered by hits, descending; a stream with conversions but no events in
// the window still appears, with zero hits.
func (c *CH) Performance(ctx context.Context, from, to time.Time) ([]PerfRow, error) {
	costSel := "0 AS cost"
	if c.eventCostCols {
		costSel = "sum(cost) AS cost"
	}
	offerSel := "'' AS offer"
	if c.eventOfferCols {
		offerSel = "offer"
	}
	rows, err := c.conn.Query(ctx,
		`SELECT group_id, group_name, stream_name, `+offerSel+`,
		        count() AS hits, countIf(uniq = 1) AS uniq,
		        countIf(bot != '' AND bot != '-') AS bots, `+costSel+`
		 FROM events WHERE ts >= ? AND ts < ?
		 GROUP BY group_id, group_name, stream_name, offer
		 ORDER BY hits DESC LIMIT 2000`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct{ group, stream, offer string }
	var out []PerfRow
	index := map[key]int{}
	for rows.Next() {
		var p PerfRow
		var hits, uniq, bots uint64
		if err := rows.Scan(&p.GroupID, &p.Group, &p.Stream, &p.Offer, &hits, &uniq, &bots, &p.Cost); err != nil {
			return nil, err
		}
		p.Hits, p.Unique, p.Bots = int64(hits), int64(uniq), int64(bots)
		index[key{p.Group, p.Stream, p.Offer}] = len(out)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if c.netCols {
		if err := c.addForwarded(ctx, from, to, &out); err != nil {
			return nil, err
		}
	}
	pbOfferSel := "'' AS offer"
	if c.postbackOfferCols {
		pbOfferSel = "offer"
	}
	pbs, err := c.conn.Query(ctx,
		`SELECT group_name, stream_name, `+pbOfferSel+`, count() AS conv, `+convTrashSelect(c.postbackCols)+`,
			`+profitSumsSelect(c.postbackCols)+`, `+convCostSelect(c.postbackCols)+`
		 FROM postbacks FINAL WHERE ts >= ? AND ts < ?
		 GROUP BY group_name, stream_name, offer`, from, to)
	if err != nil {
		return nil, err
	}
	defer pbs.Close()
	for pbs.Next() {
		var group, stream, offer string
		var conv, trash uint64
		var profit, trashProfit, convCost float64
		if err := pbs.Scan(&group, &stream, &offer, &conv, &trash, &profit, &trashProfit, &convCost); err != nil {
			return nil, err
		}
		i, ok := index[key{group, stream, offer}]
		if !ok {
			i = len(out)
			index[key{group, stream, offer}] = i
			out = append(out, PerfRow{Group: group, Stream: stream, Offer: offer})
		}
		out[i].Conv, out[i].Trash, out[i].TrashProfit = int64(conv), int64(trash), trashProfit
		out[i].Cost += convCost
		out[i].Profit = profit - out[i].Cost
	}
	return out, pbs.Err()
}

// Breakdown — a breakdown by dimension dim over a period (top limit).
func (c *CH) Breakdown(ctx context.Context, from, to time.Time, dim string, limit int) ([]KV, error) {
	col, ok := breakdownDims[dim]
	if !ok && c.netCols {
		col, ok = networkDims[dim]
	}
	if !ok {
		return nil, fmt.Errorf("store: unknown dimension %q", dim)
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := c.conn.Query(ctx,
		`SELECT `+col+` AS k, count() AS n FROM events
		 WHERE ts >= ? AND ts < ? GROUP BY k ORDER BY n DESC LIMIT ?`,
		from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KV
	for rows.Next() {
		var kv KV
		var n uint64
		if err := rows.Scan(&kv.Key, &n); err != nil {
			return nil, err
		}
		kv.Count = int64(n)
		out = append(out, kv)
	}
	return out, rows.Err()
}

// addForwarded adds the visits each linking stream sent on to another group.
func (c *CH) addForwarded(ctx context.Context, from, to time.Time, out *[]PerfRow) error {
	rows, err := c.conn.Query(ctx,
		`SELECT via, count() AS hits, countIf(uniq = 1) AS uniq,
		        countIf(bot != '' AND bot != '-') AS bots
		 FROM events WHERE ts >= ? AND ts < ? AND via != ''
		 GROUP BY via`, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var via string
		var hits, uniq, bots uint64
		if err := rows.Scan(&via, &hits, &uniq, &bots); err != nil {
			return err
		}
		gid, stream, ok := strings.Cut(via, "/")
		if !ok {
			continue
		}
		i := -1
		for k := range *out {
			if (*out)[k].GroupID == gid && (*out)[k].Stream == stream && (*out)[k].Offer == "" {
				i = k
				break
			}
		}
		if i < 0 {
			*out = append(*out, PerfRow{GroupID: gid, Stream: stream})
			i = len(*out) - 1
		}
		p := &(*out)[i]
		p.Hits += int64(hits)
		p.Unique += int64(uniq)
		p.Bots += int64(bots)
		p.Forwarded += int64(hits)
	}
	return rows.Err()
}

// LogFilter — log-viewing filters (all optional). Categorical fields are
// value lists (multi-select → SQL IN), IP is a single substring.
type LogFilter struct {
	From, To             time.Time
	Group, Stream        []string
	Country, Device      []string
	OS, Browser, Brand   []string
	IP                   string
	BotsOnly, HumansOnly bool
	Limit, Offset        int
}

const (
	// defaultLogRows — one page of the interactive log table.
	defaultLogRows = 100
	// maxLogRows bounds a single Logs call. The ceiling is the size of a CSV
	// export, not of a page: the export legitimately asks for a whole period,
	// and clamping it down to a page size silently truncated every export to
	// defaultLogRows rows. Paginating the interactive endpoint is the admin
	// layer's job (see maxPageRows there).
	maxLogRows = 50000
)

// clampLogLimit bounds a requested row count to something the process can hold.
func clampLogLimit(limit int) int {
	if limit <= 0 {
		return defaultLogRows
	}
	if limit > maxLogRows {
		return maxLogRows
	}
	return limit
}

// LogRow — a log row for the table.
type LogRow struct {
	TS      time.Time `json:"ts"`
	Group   string    `json:"group"`
	Stream  string    `json:"stream"`
	Country string    `json:"country"`
	City    string    `json:"city"`
	Device  string    `json:"device"`
	OS      string    `json:"os"`
	Browser string    `json:"browser"`
	Brand   string    `json:"brand"`
	Bot     string    `json:"bot"`
	Uniq    uint8     `json:"uniq"`
	IP      string    `json:"ip"`
	Keyword string    `json:"keyword"`
	Out     string    `json:"out"`
	Offer   string    `json:"offer"`

	// The rest is for the row's detail view, not the table.
	Region    string `json:"region"`
	Lang      string `json:"lang"`
	Operator  string `json:"operator"`
	Redirect  string `json:"redirect"`
	Referer   string `json:"referer"`
	UserAgent string `json:"useragent"`
	Domain    string `json:"domain"`
	CID       string `json:"cid"`
	ASN       uint32 `json:"asn"`
	Org       string `json:"org"`
	Timezone  string `json:"timezone"`
}

// Logs returns log rows matching the filter and the total number of matching rows.
// All conditions are parameterized (no concatenation of values).
func (c *CH) Logs(ctx context.Context, f LogFilter) ([]LogRow, int64, error) {
	where := "ts >= ? AND ts < ?"
	args := []any{f.From, f.To}
	in := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = "?"
			args = append(args, v)
		}
		where += " AND " + col + " IN (" + strings.Join(ph, ",") + ")"
	}
	eq := func(col, val string) {
		if val != "" {
			where += " AND " + col + " = ?"
			args = append(args, val)
		}
	}
	in("group_name", f.Group)
	in("stream_name", f.Stream)
	in("country", f.Country)
	in("device", f.Device)
	in("os", f.OS)
	in("browser", f.Browser)
	in("brand", f.Brand)
	eq("ip", f.IP)
	if f.BotsOnly {
		where += " AND bot != '' AND bot != '-'"
	}
	if f.HumansOnly {
		where += " AND (bot = '' OR bot = '-')"
	}

	var total uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM events WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := clampLogLimit(f.Limit)
	net := ", 0 AS asn, '' AS org, '' AS timezone"
	if c.netCols {
		net = ", asn, org, timezone"
	}
	offer := ", '' AS offer"
	if c.eventOfferCols {
		offer = ", offer"
	}
	q := "SELECT ts, group_name, stream_name, country, city, device, os, browser, brand, bot, uniq, ip, keyword, out," +
		" region, lang, operator, redirect, referer, useragent, domain, cid" + offer + net +
		" FROM events WHERE " + where + " ORDER BY ts DESC LIMIT ? OFFSET ?"
	rows, err := c.conn.Query(ctx, q, append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []LogRow
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.TS, &r.Group, &r.Stream, &r.Country, &r.City, &r.Device,
			&r.OS, &r.Browser, &r.Brand, &r.Bot, &r.Uniq, &r.IP, &r.Keyword, &r.Out,
			&r.Region, &r.Lang, &r.Operator, &r.Redirect, &r.Referer, &r.UserAgent, &r.Domain, &r.CID,
			&r.Offer, &r.ASN, &r.Org, &r.Timezone); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, int64(total), rows.Err()
}

// profitSums returns the (revenue, trash) sum expressions for postbacks
// aggregates. Revenue excludes status=trash; rows predating migration 004
// carry status=” and count as revenue. Without the columns it degrades to
// the legacy totals.
func profitSums(hasCols bool) (rev, trash string) {
	if !hasCols {
		return "sum(profit)", "0"
	}
	return "sumIf(profit, status != 'trash')", "sumIf(profit, status = 'trash')"
}

// profitSumsSelect renders both sums as one SELECT fragment.
func profitSumsSelect(hasCols bool) string {
	rev, trash := profitSums(hasCols)
	return rev + ` AS revenue, ` + trash + ` AS trash_profit`
}

// convCounts returns the (all, trash) conversion count expressions.
func convCounts(hasCols bool) (all, trash string) {
	if !hasCols {
		return "count()", "0"
	}
	return "count()", "countIf(status = 'trash')"
}

// convTrashSelect renders the trash conversion count fragment.
func convTrashSelect(hasCols bool) string {
	_, trash := convCounts(hasCols)
	return trash + ` AS trash`
}

// convCostSelect renders the conversion-cost sum fragment (CPA/Revshare,
// trash excluded like revenue). Legacy databases contribute 0.
func convCostSelect(hasCols bool) string {
	if !hasCols {
		return `0 AS conv_cost`
	}
	return `sumIf(cost, status != 'trash') AS conv_cost`
}

// postbackRowCols lists the postbacks columns for row reads.
func postbackRowCols(hasCols, hasOffer bool) string {
	cols := "ts, group_name, stream_name, domain, page, device, country, city, profit, cid"
	if hasCols {
		cols += ", status, tid, cost"
	}
	if hasOffer {
		cols += ", offer"
	}
	return cols
}

// postbackExtraInsert renders the optional INSERT column suffix.
func postbackExtraInsert(hasCols, hasOffer bool) string {
	s := ""
	if hasCols {
		s = ", status, tid, cost"
	}
	if hasOffer {
		s += ", offer"
	}
	return s
}

// EventOfferID returns the offer a visit was sent to (G-049), "" when the
// stream served a plain URL or the column did not exist yet.
func (c *CH) EventOfferID(ctx context.Context, cid string) (string, error) {
	if !c.eventOfferCols || cid == "" {
		return "", nil
	}
	var id string
	err := c.conn.QueryRow(ctx,
		`SELECT offer FROM events WHERE cid = ? ORDER BY ts DESC LIMIT 1`, cid).Scan(&id)
	return id, err
}

// RecordPostback finds an event by cid and writes a conversion to postbacks.
// status — нормализованный conversion.ParseStatus (пустым сюда не приходит:
// parsePostback в engine отбрасывает постбеки без статуса). tid — номер
// сделки против дублей (G-304); может быть пустым. convCost — стоимость
// конверсии в USD по модели кампании (CPA/Revshare, G-120); клик-модели,
// trash и отсутствие цены дают 0. Ценообразование никогда не блокирует запись:
// convCost считается вызывающим, сюда приходит готовым числом.
//
// Idempotency: postbacks is ReplacingMergeTree(version) ORDER BY (cid, tid),
// so a repeated (cid, tid) collapses on FINAL reads while different tids of
// the same click are kept. A legacy ORDER BY (cid) table needs the explicit
// MigratePostbacksDedupe rebuild.
func (c *CH) RecordPostback(ctx context.Context, cid string, profit float64, status, tid, offer string, convCost float64) error {
	var gname, sname, country, city, device, operator, domain, page string
	err := c.conn.QueryRow(ctx,
		`SELECT group_name, stream_name, country, city, device, operator, domain, page
		 FROM events WHERE cid = ? ORDER BY ts DESC LIMIT 1`, cid).
		Scan(&gname, &sname, &country, &city, &device, &operator, &domain, &page)
	if err != nil {
		return err
	}
	batch, err := c.conn.PrepareBatch(ctx, `INSERT INTO postbacks
		(domain, page, device, operator, country, city, profit, group_name, stream_name, cid`+postbackExtraInsert(c.postbackCols, c.postbackOfferCols)+`)`)
	if err != nil {
		return err
	}
	row := []any{domain, page, device, operator, country, city, profit, gname, sname, cid}
	if c.postbackCols {
		row = append(row, status, tid, convCost)
	}
	if c.postbackOfferCols {
		row = append(row, offer)
	}
	if err := batch.Append(row...); err != nil {
		return err
	}
	return batch.Send()
}

// EventGroupID returns the group_id of the event with this cid (for pricing
// a conversion by its entry campaign's model, G-120). Missing event → error;
// the caller must still record the conversion with zero cost.
func (c *CH) EventGroupID(ctx context.Context, cid string) (string, error) {
	var gid string
	if err := c.conn.QueryRow(ctx,
		`SELECT group_id FROM events WHERE cid = ? ORDER BY ts DESC LIMIT 1`, cid).
		Scan(&gid); err != nil {
		return "", err
	}
	return gid, nil
}

// PostbackRow — a conversion row.
type PostbackRow struct {
	TS      time.Time `json:"ts"`
	Group   string    `json:"group"`
	Stream  string    `json:"stream"`
	Offer   string    `json:"offer"`
	Domain  string    `json:"domain"`
	Page    string    `json:"page"`
	Device  string    `json:"device"`
	Country string    `json:"country"`
	City    string    `json:"city"`
	Profit  float64   `json:"profit"`
	CID     string    `json:"cid"`
	Status  string    `json:"status"`
	TID     string    `json:"tid"`
	Cost    float64   `json:"cost"`
}

// Postbacks returns conversions over a period (optional group filter),
// the Revenue total (excluding trash) and the Revenue (trash) total.
func (c *CH) Postbacks(ctx context.Context, from, to time.Time, group, status string, limit int) ([]PostbackRow, float64, float64, error) {
	where := "ts >= ? AND ts < ?"
	args := []any{from, to}
	if group != "" {
		where += " AND group_name = ?"
		args = append(args, group)
	}
	if status != "" {
		if !c.postbackCols {
			return []PostbackRow{}, 0, 0, nil
		}
		where += " AND status = ?"
		args = append(args, status)
	}
	rev, trashExpr := profitSums(c.postbackCols)
	var total, trash float64
	if err := c.conn.QueryRow(ctx, "SELECT "+rev+", "+trashExpr+" FROM postbacks FINAL WHERE "+where, args...).Scan(&total, &trash); err != nil {
		return nil, 0, 0, err
	}
	if limit <= 0 || limit > 50000 {
		limit = 200
	}
	rows, err := c.conn.Query(ctx,
		"SELECT "+postbackRowCols(c.postbackCols, c.postbackOfferCols)+" FROM postbacks FINAL WHERE "+
			where+" ORDER BY ts DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	var out []PostbackRow
	for rows.Next() {
		var p PostbackRow
		scan := []any{&p.TS, &p.Group, &p.Stream, &p.Domain, &p.Page, &p.Device, &p.Country, &p.City, &p.Profit, &p.CID}
		if c.postbackCols {
			scan = append(scan, &p.Status, &p.TID, &p.Cost)
		}
		if c.postbackOfferCols {
			scan = append(scan, &p.Offer)
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, 0, 0, err
		}
		out = append(out, p)
	}
	return out, total, trash, rows.Err()
}

// PostbackStatuses returns the conversion types present in the period, with
// counts, so the admin can offer the exact set including custom statuses.
func (c *CH) PostbackStatuses(ctx context.Context, from, to time.Time, group string) ([]KV, error) {
	if !c.postbackCols {
		return []KV{}, nil
	}
	where := "ts >= ? AND ts < ? AND status != ''"
	args := []any{from, to}
	if group != "" {
		where += " AND group_name = ?"
		args = append(args, group)
	}
	rows, err := c.conn.Query(ctx, "SELECT status, count() FROM postbacks FINAL WHERE "+where+" GROUP BY status ORDER BY status", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KV{}
	for rows.Next() {
		var k KV
		var n uint64
		if err := rows.Scan(&k.Key, &n); err != nil {
			return nil, err
		}
		k.Count = int64(n)
		out = append(out, k)
	}
	return out, rows.Err()
}

// InsertEvents inserts a batch of events in a single batch request (lock-free).
// eventInsertCols lists the event columns for the INSERT, including the
// optional groups the schema may or may not have.
func eventInsertCols(hasNet, hasCost, hasOffer bool) string {
	cols := `ts, group_id, group_name, stream_name, out, keyword, redirect, device,
		 operator, country, city, region, lang, uniq, bot, ip, referer, useragent,
		 domain, page, se, os, os_version, browser, browser_ver, brand,
		 counter, cid, postback`
	if hasOffer {
		cols += ", offer"
	}
	if hasNet {
		cols += ", asn, org, timezone, via"
	}
	if hasCost {
		cols += ", cost, cost_cur"
	}
	return cols
}

// eventInsertRow flattens an event in the same order as eventInsertCols.
func eventInsertRow(e logbuf.Event, hasNet, hasCost, hasOffer bool) []any {
	row := []any{
		e.Ts, e.GroupID, e.GroupName, e.Stream, e.Out, e.Keyword, e.Redirect,
		e.Device, e.Operator, e.Country, e.City, e.Region, e.Lang, e.Uniq,
		e.Bot, e.IP, e.Referer, e.UserAgent, e.Domain, e.Page, e.SE,
		e.OS, e.OSVersion, e.Browser, e.BrowserV, e.Brand,
		e.Counter, e.CID, e.Postback,
	}
	if hasOffer {
		row = append(row, e.Offer)
	}
	if hasNet {
		row = append(row, e.ASN, e.Org, e.Timezone, e.Via)
	}
	if hasCost {
		row = append(row, e.Cost, e.CostCur)
	}
	return row
}

func (c *CH) InsertEvents(ctx context.Context, events []logbuf.Event) error {
	if len(events) == 0 {
		return nil
	}
	cols := eventInsertCols(c.netCols, c.eventCostCols, c.eventOfferCols)
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO events ("+cols+")")
	if err != nil {
		return fmt.Errorf("clickhouse: prepare batch: %w", err)
	}
	for _, e := range events {
		row := eventInsertRow(e, c.netCols, c.eventCostCols, c.eventOfferCols)
		if err := batch.Append(row...); err != nil {
			return fmt.Errorf("clickhouse: append: %w", err)
		}
	}
	return batch.Send()
}

// PostbackError — a rejected incoming postback (G-066): why it was not
// written, what arrived, and where it came from. Query never contains the
// postback key.
type PostbackError struct {
	TS     time.Time
	Reason string
	CID    string
	Status string
	TID    string
	IP     string
	Query  string
	Detail string
}

// InsertPostbackErrors appends rejected postbacks to the journal. The rows are
// diagnostic only: a failure here must never affect the visitor's 200.
func (c *CH) InsertPostbackErrors(ctx context.Context, rows []PostbackError) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, `INSERT INTO postback_errors
		(ts, reason, cid, status, tid, ip, query, detail)`)
	if err != nil {
		return err
	}
	for _, x := range rows {
		if err := batch.Append(x.TS, x.Reason, x.CID, x.Status, x.TID, x.IP, x.Query, x.Detail); err != nil {
			return err
		}
	}
	return batch.Send()
}

// PostbackErrors returns the most recent rejected postbacks in the period.
func (c *CH) PostbackErrors(ctx context.Context, from, to time.Time, limit int) ([]PostbackError, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.conn.Query(ctx, `SELECT ts, reason, cid, status, tid, ip, query, detail
		FROM postback_errors WHERE ts >= ? AND ts < ? ORDER BY ts DESC LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PostbackError, 0, limit)
	for rows.Next() {
		var x PostbackError
		if err := rows.Scan(&x.TS, &x.Reason, &x.CID, &x.Status, &x.TID, &x.IP, &x.Query, &x.Detail); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
