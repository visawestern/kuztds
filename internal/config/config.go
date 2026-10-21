// Package config provides the typed configuration for kuztds.
//
// Secrets are not hardcoded here: they come from
// environment variables / a secret file. The stream config is
// loaded separately into a RAM cache with hot-reload — see ARCHITECTURE.md.
package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// App is the static process configuration (from yaml + env).
type App struct {
	ListenAddr     string        `yaml:"listen_addr"`
	TrustedProxies []string      `yaml:"trusted_proxies"` // CIDRs we trust for XFF/CF
	RequestTimeout time.Duration `yaml:"request_timeout"`
	GeoDBPath      string        `yaml:"geo_db_path"` // mmdb
	DataDir        string        `yaml:"data_dir"`    // directory of .dat lists
	ReloadInterval time.Duration `yaml:"reload_interval"`

	ClickHouse ClickHouse `yaml:"clickhouse"`
	Redis      Redis      `yaml:"redis"`
	Secrets    Secrets    `yaml:"-"` // only from env, not from a file in VCS
}

type ClickHouse struct {
	Addr     string `yaml:"addr"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
}

type Redis struct {
	Addr string `yaml:"addr"`
	DB   int    `yaml:"db"`
}

// Secrets are read only from the environment (KUZTDS_*), never from a file in the repo.
type Secrets struct {
	ClickHousePassword string
	RedisPassword      string
	ParamHMACKey       string // signature of packed parameters [ex]
	AdminSessionKey    string
}

// --- stream config model ---

// MaxGroupHops bounds a chain of "group" links (a stream or a default that
// hands the visitor to another group), so that groups pointing at each other
// end in the trash answer instead of spinning. The engine enforces it; the
// admin's simulator follows the same limit.
const MaxGroupHops = 3

// Group is a group of streams.
type Group struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases"` // alternative IDs for routing (http://tds/<alias>)
	Name     string   `json:"name"`
	Status   bool     `json:"status"`
	Redirect string   `json:"redirect"`
	Header   string   `json:"header"`
	Out      string   `json:"out"`
	// Geo picks which country source wins when both answer: "cf" — the CDN's
	// CF-IPCountry header first, the geo database as a fallback; anything else
	// ("db", or the legacy "sypex") — the database first, the header as a
	// fallback. City, region, time zone and ASN always come from the database.
	Geo string `json:"geo"`

	UniqMethod  string       `json:"uniq_method"`  // "ip" (default) | "cookie"
	UniqSeconds int          `json:"uniq_seconds"` // uniqueness window
	Firewall    FirewallRule `json:"firewall"`     // antiflood
	SaveKeys    bool         `json:"save_keys"`    // save keywords
	SaveKeysSE  bool         `json:"save_keys_se"` // save keywords from search engines

	Cost   CostModel `json:"cost"`   // G-119…G-123: price model of the campaign
	Offers []Offer   `json:"offers"` // G-048: offers of the campaign
	// G-055: ready-made traffic source templates and the active one.
	Sources []TrafficSource `json:"sources"`
	Source  string          `json:"source"`

	Streams []Stream `json:"streams"`
}

// TrafficSource is a template of query parameters to macro labels, so a flow
// can be pointed at an ad network without counting positional [PAR-n].
type TrafficSource struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Labels []SourceLabel `json:"labels"`
}

// SourceLabel maps one query parameter to one output macro label.
type SourceLabel struct {
	Param string `json:"param"`
	Label string `json:"label"`
}

var (
	sourceIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	sourceLblRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
	// Labels the renderer already owns; a template must not shadow them.
	sourceReserved = map[string]bool{
		"KEY": true, "PATH": true, "IP": true, "COUNTRY": true, "CITY": true,
		"REGION": true, "ASN": true, "ORG": true, "TIMEZONE": true, "UTC": true,
		"LANG": true, "DEVICE": true, "OPERATOR": true, "DOMAIN": true,
		"USERAGENT": true, "CID": true, "REMOTE": true,
	}
)

// ValidateSources rejects templates the engine could not expand, and an active
// id that points nowhere. Templates may be drafts with no labels.
func (g Group) ValidateSources() error {
	seen := map[string]bool{}
	for i, s := range g.Sources {
		if !sourceIDRe.MatchString(s.ID) {
			return fmt.Errorf("config: group %s source %d: id %q must be letters, digits, _ or -", g.ID, i+1, s.ID)
		}
		if seen[s.ID] {
			return fmt.Errorf("config: group %s: duplicate source id %q", g.ID, s.ID)
		}
		seen[s.ID] = true
		labels := map[string]bool{}
		for j, l := range s.Labels {
			p := strings.TrimSpace(l.Param)
			if p == "" || len(p) > 64 || strings.ContainsAny(p, "&=#? \t\r\n") {
				return fmt.Errorf("config: group %s source %q label %d: parameter %q is not a query key", g.ID, s.ID, j+1, l.Param)
			}
			name := strings.ToUpper(strings.TrimSpace(l.Label))
			if !sourceLblRe.MatchString(strings.TrimSpace(l.Label)) {
				return fmt.Errorf("config: group %s source %q label %d: label %q must be letters, digits or _ and start with a letter", g.ID, s.ID, j+1, l.Label)
			}
			if sourceReserved[name] {
				return fmt.Errorf("config: group %s source %q: label %q is reserved by the engine", g.ID, s.ID, l.Label)
			}
			if labels[name] {
				return fmt.Errorf("config: group %s source %q: label %q is listed twice", g.ID, s.ID, l.Label)
			}
			labels[name] = true
		}
	}
	if g.Source != "" {
		if _, ok := g.SourceTemplate(g.Source); !ok {
			return fmt.Errorf("config: group %s: source %q does not exist", g.ID, g.Source)
		}
	}
	return nil
}

// SourceTemplate returns a traffic source template by id.
func (g Group) SourceTemplate(id string) (TrafficSource, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TrafficSource{}, false
	}
	for _, s := range g.Sources {
		if s.ID == id {
			return s, true
		}
	}
	return TrafficSource{}, false
}

// FirewallRule is a request limit from a single IP per window.
type FirewallRule struct {
	Enabled bool `json:"enabled"`
	Queries int  `json:"queries"` // g_f_queries
	Seconds int  `json:"seconds"` // g_f_time
}

// CostModel — цена кампании G-119…G-123 (паритет Binom v2).
// Model: "", "cpc", "cpm", "cpa", "cpv", "revshare". Пустая — цена не задана.
// Пересчёт в деньги — в internal/cost (чистые функции).
type CostModel struct {
	Model       string  `json:"model"`
	Value       float64 `json:"value"`
	Currency    string  `json:"currency"`
	PayoutPct   float64 `json:"payout_pct"`   // revshare: доля выплаты
	LossPct     float64 `json:"loss_pct"`     // G-122: потери трафика, %
	PostbackPct float64 `json:"postback_pct"` // G-123: доля постбеков источнику, %
}

// Validate rejects an unusable price config. Empty model (no pricing) is valid.
func (m CostModel) Validate() error {
	switch strings.ToLower(strings.TrimSpace(m.Model)) {
	case "", "cpc", "cpm", "cpa", "cpv", "revshare":
	default:
		return fmt.Errorf("config: unknown cost model %q", m.Model)
	}
	if m.Value < 0 {
		return fmt.Errorf("config: cost value must not be negative")
	}
	if cur := strings.TrimSpace(m.Currency); cur != "" {
		for _, r := range cur {
			if !unicode.IsLetter(r) {
				return fmt.Errorf("config: cost currency %q is not a code", m.Currency)
			}
		}
		if len([]rune(cur)) != 3 {
			return fmt.Errorf("config: cost currency %q is not a 3-letter code", m.Currency)
		}
	}
	if m.PayoutPct < 0 || m.PayoutPct > 100 {
		return fmt.Errorf("config: payout pct must be 0..100")
	}
	if m.LossPct < 0 || m.LossPct >= 100 {
		return fmt.Errorf("config: loss pct must be 0..99")
	}
	if m.PostbackPct < 0 || m.PostbackPct > 100 {
		return fmt.Errorf("config: postback pct must be 0..100")
	}
	return nil
}

// Offer — предложение (оффер) кампании G-048: карточка со ссылкой.
// Ссылка получает те же макросы, что обычный вывод потока; Status=false —
// черновик, движок такой оффер не обслуживает.
type Offer struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Network  string  `json:"network"` // партнёрская сеть, для отчётов
	URL      string  `json:"url"`
	Status   bool    `json:"status"`
	Payout   float64 `json:"payout"`    // G-049: revenue per conversion, 0 = none
	Currency string  `json:"currency"`  // ISO 4217 of the payout; empty = USD
	CapDaily int     `json:"cap_daily"` // G-050: purchases per UTC day, 0 = unlimited
	Fallback string  `json:"fallback"`  // G-050: offer served once the cap is reached
	// G-054: ISO 3166-1 alpha-2 countries the network buys; empty = all.
	Countries []string `json:"countries"`
}

var (
	offerIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	offerCurRe   = regexp.MustCompile(`^[A-Za-z]{3}$`)
	offerCountry = regexp.MustCompile(`^[A-Za-z]{2}$`)
)

// ValidateOffers rejects offers the engine could not serve. Disabled offers
// may be incomplete: they are drafts the operator is still filling in.
func (g Group) ValidateOffers() error {
	seen := map[string]bool{}
	for i, o := range g.Offers {
		if o.ID != "" && !offerIDRe.MatchString(o.ID) {
			return fmt.Errorf("config: group %s offer %d: id %q must be letters, digits, _ or -", g.ID, i+1, o.ID)
		}
		if seen[o.ID] && o.ID != "" {
			return fmt.Errorf("config: group %s: duplicate offer id %q", g.ID, o.ID)
		}
		seen[o.ID] = true
		// G-049: the payout is a number in an ISO currency; the rate is looked
		// up at postback time, so only the shape is checked here.
		if o.Payout < 0 {
			return fmt.Errorf("config: group %s offer %q: payout must not be negative", g.ID, o.ID)
		}
		if o.Currency != "" && !offerCurRe.MatchString(o.Currency) {
			return fmt.Errorf("config: group %s offer %q: currency %q must be a 3-letter code", g.ID, o.ID, o.Currency)
		}
		if o.CapDaily < 0 {
			return fmt.Errorf("config: group %s offer %q: daily cap must not be negative", g.ID, o.ID)
		}
		if o.Fallback == o.ID && o.ID != "" {
			return fmt.Errorf("config: group %s offer %q: fallback must be another offer", g.ID, o.ID)
		}
		// G-054: countries are ISO alpha-2 codes, each listed once.
		cc := map[string]bool{}
		for _, c := range o.Countries {
			c = strings.ToUpper(strings.TrimSpace(c))
			if !offerCountry.MatchString(c) {
				return fmt.Errorf("config: group %s offer %q: country %q must be a two-letter code", g.ID, o.ID, c)
			}
			if cc[c] {
				return fmt.Errorf("config: group %s offer %q: country %q is listed twice", g.ID, o.ID, c)
			}
			cc[c] = true
		}
		if !o.Status {
			continue
		}
		if o.ID == "" {
			return fmt.Errorf("config: group %s offer %d: an enabled offer needs an id", g.ID, i+1)
		}
		if err := offerURL(o.URL); err != nil {
			return fmt.Errorf("config: group %s offer %q: %w", g.ID, o.ID, err)
		}
	}
	// Fallbacks may point forward, so they are checked once every id is known.
	for _, o := range g.Offers {
		if o.Fallback == "" {
			continue
		}
		fb, ok := g.Offer(o.Fallback)
		if !ok {
			return fmt.Errorf("config: group %s offer %q: fallback %q does not exist", g.ID, o.ID, o.Fallback)
		}
		if !fb.Status {
			return fmt.Errorf("config: group %s offer %q: fallback %q is disabled", g.ID, o.ID, o.Fallback)
		}
	}
	return nil
}

func offerURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}
	return nil
}

// AcceptsCountry reports whether the offer buys traffic from the country. An
// empty list means every country is accepted. Codes compare case-insensitively.
func (o Offer) AcceptsCountry(country string) bool {
	if len(o.Countries) == 0 {
		return true
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	for _, c := range o.Countries {
		if strings.ToUpper(strings.TrimSpace(c)) == country {
			return true
		}
	}
	return false
}

// Offer returns a configured offer by id. A disabled offer is still returned:
// the caller decides between serving it and falling back.
func (g Group) Offer(id string) (Offer, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Offer{}, false
	}
	for _, o := range g.Offers {
		if o.ID == id {
			return o, true
		}
	}
	return Offer{}, false
}

// Stream is a stream with filtering rules and output parameters.
// The rules are factored out into Rules as data — the router runs them in a loop
// (declarative rules executed by the router in a loop).
type Stream struct {
	Name       string     `json:"name"`
	Status     bool       `json:"status"`
	Comment    string     `json:"comment"` // operator's note, not used by the engine
	Rules      Rules      `json:"rules"`
	Out        Output     `json:"out"`
	Bots       Bots       `json:"bots"`
	Separation Separation `json:"separation"`
	Curl       string     `json:"curl"`    // find/replace for CURL redirect
	BotCurl    string     `json:"b_curl"`  // find/replace for CURL to bots
	Remote     Remote     `json:"remote"`  // [REMOTE] fetch
	APIMac     APIMac     `json:"api_mac"` // mac code in the API response
}

// Remote fetches external content into [REMOTE] (with caching and regex parsing).
type Remote struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`      // supports [IP][COUNTRY][CITY][LANG][KEY]
	Regexp   string `json:"regexp"`   // /.../ — take the first group; otherwise the whole response
	Reserved string `json:"reserved"` // fallback on error/empty response
	Cache    int    `json:"cache"`    // cache seconds
}

// APIMac is a mac code returned in an api-type response with probability Prob %.
type APIMac struct {
	Enabled bool   `json:"enabled"`
	Code    string `json:"code"`
	Prob    int    `json:"prob"`
}

// Bots holds settings for bot detection and serving them separately. Detection
// runs AFTER stream selection, based on its toggles.
type Bots struct {
	CheckUA   bool `json:"ch_ua"`         // check UA/referer signatures
	EmptyUA   bool `json:"ch_empty_ua"`   // empty UA = bot
	EmptyRef  bool `json:"ch_empty_ref"`  // empty referer = bot
	EmptyLang bool `json:"ch_empty_lang"` // empty language = bot
	IPv6      bool `json:"ch_ipv6"`       // IPv6 = bot
	PTR       bool `json:"ch_ptr"`        // reverse DNS
	ListUA    bool `json:"ch_list_ua"`    // ua_blacklist.dat
	IPBaidu   bool `json:"ch_bot_ip_baidu"`
	IPBing    bool `json:"ch_bot_ip_bing"`
	IPGoogle  bool `json:"ch_bot_ip_google"`
	IPMail    bool `json:"ch_bot_ip_mail"`
	IPYahoo   bool `json:"ch_bot_ip_yahoo"`
	IPYandex  bool `json:"ch_bot_ip_yandex"`
	IPOthers  bool `json:"ch_bot_ip_others"`
	SaveIP    bool `json:"save_ip"` // append bot IP to ip_<se>.dat

	Redirect string `json:"redirect"` // bot_redirect; "skip" = normal stream
	Out      string `json:"out"`      // out_bot
	Header   string `json:"header"`   // b_header (Content-Type for bots)
}

// Output holds the stream output parameters (used at the render phase).
type Output struct {
	Redirect     string `json:"redirect"`
	Out          string `json:"out"`
	Chance       int    `json:"chance"`
	Distribution string `json:"distribution"` // random | rotator | evenly (for out with |||)
	Header       string `json:"header"`       // Content-Type override; empty = the group's
}

// Separation substitutes the output by a keyword from a .dat file (format "key;out").
type Separation struct {
	Enabled bool   `json:"enabled"`
	File    string `json:"file"`
}

// Flag holds 0/1/2 filter flags. The exact meaning depends on the filter
// (see router), but the general principle is: 2 = filter disabled.
type Flag int

const (
	// FlagOff means the filter is disabled. It is the zero value of Flag, so an unset
	// rules field is safely treated as "disabled".
	FlagOff Flag = iota
	// FlagA means, for list filters: "exclude on match" (blacklist).
	// For devices/operators: "block".
	FlagA
	// FlagB means, for list filters: "allow only on match" (whitelist).
	// For operators: "whitelist".
	FlagB
)

// ListFilter holds a flag + filter values. Raw stores the original string (for
// detecting /regex/ and the contains semantics of lang/country); Values is the same
// string split by comma and trimmed (for element-wise checks).
type ListFilter struct {
	Flag   Flag     `json:"flag"`
	Raw    string   `json:"raw"`
	Values []string `json:"values"`
}

// IPListFilter filters by an IP list (.dat in DataDir).
type IPListFilter struct {
	Flag Flag   `json:"flag"`
	File string `json:"file"` // file name, not a user-supplied path
}

// Schedule is the stream's working schedule by day of week.
// Days is indexed by time.Weekday: [0]=Sunday … [6]=Saturday.
// When Enabled=true the stream is active only on the marked days.
type Schedule struct {
	Enabled bool    `json:"enabled"`
	Days    [7]bool `json:"days"`
}

// LimitRule is a stream impression limit (the check is delegated to Limiter at phase 4).
type LimitRule struct {
	Enabled bool `json:"enabled"`
	Type    int  `json:"type"`    // 1 = per day, 2 = per period (Seconds)
	Count   int  `json:"count"`   // threshold
	Seconds int  `json:"seconds"` // window for Type==2
}

// Rules is the full set of stream filtering predicates.
type Rules struct {
	Lang    ListFilter `json:"lang"`    // contains semantics
	Country ListFilter `json:"country"` // contains semantics
	City    ListFilter `json:"city"`    // element-wise exact match
	Region  ListFilter `json:"region"`  // element-wise exact match
	UAText  ListFilter `json:"ua_text"` // /regex/ or substring
	Referer ListFilter `json:"referer_text"`
	Domain  ListFilter `json:"domain_text"` // /regex/ or exact match
	Key     ListFilter `json:"key_text"`    // /regex/ or substring

	OS      ListFilter `json:"os"`      // by "name version", substring (Android, Android 13, iOS 16)
	Browser ListFilter `json:"browser"` // by "name version", substring (Chrome, Safari 16)
	Brand   ListFilter `json:"brand"`   // by device brand, exact (Apple, Samsung, Xiaomi)

	// By what the IP says about the network and the clock (needs the geo databases).
	ASN      ListFilter `json:"asn"`      // AS number, exact ("15169" or "AS15169")
	Org      ListFilter `json:"org"`      // AS organization, /regex/ or substring
	Timezone ListFilter `json:"timezone"` // UTC offset ("+3", "+5:30") or IANA name

	// By a query-string variable: "name" (present) or "name=value".
	Query ListFilter `json:"get"`

	Schedule Schedule `json:"schedule"` // stream working schedule by day of week

	IPList IPListFilter `json:"ip_list"`

	// Devices: FlagA(0) = block this type; otherwise allow.
	Computer Flag `json:"computer"`
	Phone    Flag `json:"phone"`
	Tablet   Flag `json:"tablet"`

	// Operators (beeline,megafon,mts,tele2,azerbaijan,belarus,kazakhstan,
	// ukraine,wap-1,wap-2,wap-3): FlagA(0) = block this operator;
	// FlagB(1) = whitelist (if at least one is set — allow only those).
	Operators map[string]Flag `json:"operators"`

	Unique     Flag `json:"unique"`      // 0=unique only,1=non-unique only,2=any
	YaBrowser  Flag `json:"yabrowser"`   // 0=exclude YaBrowser,1=only it,2=any
	HasReferer Flag `json:"has_referer"` // 0=require referer,1=require no referer,2=any

	Limit LimitRule `json:"limit"`
}
