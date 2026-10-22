package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PostbacksSortingKey returns the ClickHouse sorting key of the postbacks
// table. Fresh databases have "cid, tid"; an older one has "cid".
func (c *CH) PostbacksSortingKey(ctx context.Context) (string, error) {
	var key, engine string
	err := c.conn.QueryRow(ctx,
		`SELECT sorting_key, engine FROM system.tables
		 WHERE database = currentDatabase() AND name = 'postbacks'`).Scan(&key, &engine)
	if err != nil {
		return "", err
	}
	return key, nil
}

// MigratePostbacksDedupe rebuilds postbacks with ORDER BY (cid, tid) and a
// millisecond version column. It is explicit, idempotent and leaves the old
// table untouched until every row has been copied and counted. The old table
// was ORDER BY (cid), so different transactions of one click overwrote each
// other; the new key keeps them.
func (c *CH) MigratePostbacksDedupe(ctx context.Context) (bool, error) {
	key, err := c.PostbacksSortingKey(ctx)
	if err != nil {
		return false, fmt.Errorf("postbacks migration: %w", err)
	}
	norm := strings.ReplaceAll(key, " ", "")
	if norm == "cid,tid" {
		return false, nil
	}
	if norm != "cid" {
		return false, fmt.Errorf("postbacks migration: unsupported sorting key %q", key)
	}
	var dbEngine string
	if err := c.conn.QueryRow(ctx,
		`SELECT engine FROM system.databases WHERE name = currentDatabase()`).Scan(&dbEngine); err != nil {
		return false, fmt.Errorf("postbacks migration: database engine: %w", err)
	}
	if !strings.EqualFold(dbEngine, "Atomic") && !strings.EqualFold(dbEngine, "Replicated") {
		return false, fmt.Errorf("postbacks migration: database engine %q does not support EXCHANGE TABLES", dbEngine)
	}
	cols, err := c.postbackColumnSet(ctx)
	if err != nil {
		return false, err
	}
	for _, name := range []string{"ts", "domain", "page", "device", "operator", "country", "city",
		"profit", "group_name", "stream_name", "cid", "status", "tid", "cost", "offer", "version"} {
		if !cols[name] {
			return false, fmt.Errorf("postbacks migration: column %q is missing; run the engine once to add it", name)
		}
	}
	temp := fmt.Sprintf("postbacks_dedupe_%d", time.Now().UnixNano())
	quoted := "`" + temp + "`"
	defer func() { _ = c.conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+quoted) }()

	columnList := "ts, version, domain, page, device, operator, country, city, profit, group_name, stream_name, cid, status, tid, cost, offer"
	create := "CREATE TABLE " + quoted + ` ENGINE = ReplacingMergeTree(version)
		PARTITION BY toYYYYMM(ts)
		ORDER BY (cid, tid)
		TTL ts + INTERVAL 365 DAY
		AS SELECT ` + columnList + ` FROM postbacks LIMIT 0`
	if err := c.conn.Exec(ctx, create); err != nil {
		return false, fmt.Errorf("postbacks migration: create: %w", err)
	}
	insert := "INSERT INTO " + quoted + " (" + columnList + `)
		SELECT ts, toDateTime64(ts, 3), domain, page, device, operator, country, city, profit,
		       group_name, stream_name, cid, status, tid, cost, offer
		FROM postbacks`
	if err := c.conn.Exec(ctx, insert); err != nil {
		return false, fmt.Errorf("postbacks migration: copy: %w", err)
	}
	var oldCount, copied, distinct uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM postbacks").Scan(&oldCount); err != nil {
		return false, err
	}
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM "+quoted).Scan(&copied); err != nil {
		return false, err
	}
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM "+quoted+" FINAL").Scan(&distinct); err != nil {
		return false, err
	}
	if copied != oldCount {
		return false, fmt.Errorf("postbacks migration: copied %d of %d rows", copied, oldCount)
	}
	if oldCount > 0 && distinct == 0 {
		return false, fmt.Errorf("postbacks migration: copied table is empty after FINAL")
	}
	if err := c.conn.Exec(ctx, "EXCHANGE TABLES postbacks AND "+quoted); err != nil {
		return false, fmt.Errorf("postbacks migration: exchange: %w", err)
	}
	// The exchange renamed the old table to temp; remove it only after the new
	// table is live. A failure here is reported but does not undo the swap.
	if err := c.conn.Exec(ctx, "DROP TABLE "+quoted); err != nil {
		return true, fmt.Errorf("postbacks migration: old table is now %s, drop failed: %w", temp, err)
	}
	return true, nil
}

func (c *CH) postbackColumnSet(ctx context.Context) (map[string]bool, error) {
	rows, err := c.conn.Query(ctx,
		`SELECT name FROM system.columns WHERE database = currentDatabase() AND table = 'postbacks'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
