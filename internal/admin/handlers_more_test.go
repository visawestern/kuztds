package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/security"
)

type fakeLists struct{ data map[string]string }

func (f *fakeLists) Names() ([]string, error) { return []string{"ip_test.dat"}, nil }
func (f *fakeLists) Read(name string) (string, error) {
	v, ok := f.data[name]
	if !ok {
		return "", context.Canceled
	}
	return v, nil
}
func (f *fakeLists) Write(name, content string) error {
	if !strings.HasSuffix(name, ".dat") {
		return errInvalidName
	}
	f.data[name] = content
	return nil
}

var errInvalidName = &nameErr{}

type nameErr struct{}

func (*nameErr) Error() string { return "invalid list name" }

type fakeKeys struct{}

func (fakeKeys) Read(group, date string, se bool) (string, error) {
	if group == "g1" {
		return "kw1\nkw2", nil
	}
	return "", context.Canceled
}

type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string) (bool, error) { return false, nil }

// fullServer builds a server with all dependencies (Lists/Keys/Groups/Stats).
func fullServer(t *testing.T, limiter LoginLimiter) (*httptest.Server, *fakeGroups, *fakeLists) {
	t.Helper()
	hash, _ := security.HashPassword("p@ss")
	fg := &fakeGroups{}
	fl := &fakeLists{data: map[string]string{"ip_test.dat": "1.2.3.4\n"}}
	if limiter == nil {
		limiter = allowAll{}
	}
	s, err := New(Config{
		AdminUser: "admin", PasswordHash: hash,
		Sessions: security.NewMemoryStore(),
		Stats:    fakeStats{}, Groups: fg, Lists: fl, Keys: fakeKeys{},
		Limiter: limiter, SessionTTL: time.Hour, CookieSecure: false,
		EngineURL: "http://engine.local", PostbackKey: "pb-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, fg, fl
}

func authedClient(t *testing.T, base string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	_, csrf := login(t, c, base, "admin", "p@ss")
	if csrf == "" {
		t.Fatal("login failed")
	}
	return c, csrf
}

func TestRateLimitBlocksLogin(t *testing.T) {
	srv, _, _ := fullServer(t, denyLimiter{})
	c := &http.Client{}
	resp, err := c.Post(srv.URL+"/api/login", "application/json",
		strings.NewReader(`{"login":"admin","password":"p@ss"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited login → 429, got %d", resp.StatusCode)
	}
}

func TestGroupsGetAndSave(t *testing.T) {
	srv, fg, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	// GET
	resp, _ := c.Get(srv.URL + "/api/groups")
	var got []config.Group
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if len(got) != 1 || got[0].ID != "g1" {
		t.Fatalf("groups get: %v", got)
	}

	// PUT (with CSRF)
	body, _ := json.Marshal([]config.Group{{ID: "a"}, {ID: "b"}})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", bytes.NewReader(body))
	req.Header.Set("X-CSRF-Token", csrf)
	r2, _ := c.Do(req)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("groups save → 200, got %d", r2.StatusCode)
	}
	if len(fg.saved) != 2 {
		t.Errorf("expected 2 groups saved, got %d", len(fg.saved))
	}
}

func TestGroupsSaveValidation(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	// empty ID
	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	if code := put(`[{"id":""}]`); code != http.StatusBadRequest {
		t.Errorf("empty id → 400, got %d", code)
	}
	if code := put(`[{"id":"x"},{"id":"x"}]`); code != http.StatusBadRequest {
		t.Errorf("duplicate id → 400, got %d", code)
	}
	if code := put(`{bad json`); code != http.StatusBadRequest {
		t.Errorf("bad json → 400, got %d", code)
	}
}

func TestGroupsSaveRejectsBadCost(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	// G-119: unknown price model must not be saved (engine would price it 0 silently).
	if code := put(`[{"id":"x","cost":{"model":"barter","value":1}}]`); code != http.StatusBadRequest {
		t.Errorf("bad cost model → 400, got %d", code)
	}
	if code := put(`[{"id":"x","cost":{"model":"cpc","value":-1}}]`); code != http.StatusBadRequest {
		t.Errorf("negative cost → 400, got %d", code)
	}
	// Valid price saves fine.
	if code := put(`[{"id":"x","cost":{"model":"cpc","value":0.05,"currency":"USD"}}]`); code != http.StatusOK {
		t.Errorf("valid cost → 200, got %d", code)
	}
}

// G-048: an enabled offer with no reachable link (or a duplicate id) must not
// be saved — the flow would fall into the trash on every visit.
func TestGroupsSaveRejectsBadOffer(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	for _, payload := range []string{
		`[{"id":"x","offers":[{"id":"o1","status":true}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"net.example","status":true}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a"},{"id":"o1","url":"https://b"}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","payout":-1}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","payout":1,"currency":"US$"}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","cap_daily":-1}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","cap_daily":1,"fallback":"nope"}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","cap_daily":1,"fallback":"o2"},{"id":"o2","url":"https://b","status":false}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","countries":["USA"]}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","countries":["DE","de"]}]}]`,
	} {
		if code := put(payload); code != http.StatusBadRequest {
			t.Errorf("bad offer %s → 400, got %d", payload, code)
		}
	}
	// A disabled draft (no link yet) and a filled enabled offer both save.
	if code := put(`[{"id":"x","offers":[{"id":"draft"},{"id":"o2","name":"Backup","url":"https://backup.example","status":true},{"id":"o1","name":"Main","network":"net","url":"https://net.example/?cid=[CID]","status":true,"payout":1.5,"currency":"EUR","cap_daily":10,"fallback":"o2","countries":["de","FR"]}]}]`); code != http.StatusOK {
		t.Errorf("valid offers → 200, got %d", code)
	}
}

func TestListsEndpoints(t *testing.T) {
	srv, _, fl := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	// index
	resp, _ := c.Get(srv.URL + "/api/lists")
	var names []string
	json.NewDecoder(resp.Body).Decode(&names)
	resp.Body.Close()
	if len(names) != 1 {
		t.Fatalf("lists index: %v", names)
	}

	// read existing
	resp, _ = c.Get(srv.URL + "/api/lists/ip_test.dat")
	var rd struct{ Name, Content string }
	json.NewDecoder(resp.Body).Decode(&rd)
	resp.Body.Close()
	if rd.Content != "1.2.3.4\n" {
		t.Errorf("list read: %q", rd.Content)
	}

	// read missing → 404
	resp, _ = c.Get(srv.URL + "/api/lists/missing.dat")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing list → 404, got %d", resp.StatusCode)
	}

	// write (with CSRF)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/lists/ip_test.dat",
		strings.NewReader(`{"content":"9.9.9.9\n"}`))
	req.Header.Set("X-CSRF-Token", csrf)
	r2, _ := c.Do(req)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK || fl.data["ip_test.dat"] != "9.9.9.9\n" {
		t.Errorf("list write failed: code=%d data=%q", r2.StatusCode, fl.data["ip_test.dat"])
	}
}

func TestKeysEndpoint(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, _ := authedClient(t, srv.URL)

	resp, _ := c.Get(srv.URL + "/api/keys?group=g1&date=2026-06-07")
	var out struct{ Content string }
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Content != "kw1\nkw2" {
		t.Errorf("keys content: %q", out.Content)
	}
	// nonexistent group → empty content, 200
	resp, _ = c.Get(srv.URL + "/api/keys?group=nope&date=2026-06-07")
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Content != "" {
		t.Errorf("missing keys → empty, got %q", out.Content)
	}
}

func TestPasswordChange(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	post := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/password", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	// wrong current
	if code := post(`{"old":"wrong","new":"newpass1"}`); code != http.StatusUnauthorized {
		t.Errorf("wrong old → 401, got %d", code)
	}
	// too short new
	if code := post(`{"old":"p@ss","new":"123"}`); code != http.StatusBadRequest {
		t.Errorf("short new → 400, got %d", code)
	}
	// successful change
	if code := post(`{"old":"p@ss","new":"newpass1"}`); code != http.StatusOK {
		t.Errorf("valid change → 200, got %d", code)
	}
}

func TestBreakdownAndTimeSeriesAndPostbacks(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, _ := authedClient(t, srv.URL)

	for _, path := range []string{
		"/api/stats/breakdown?dim=country",
		"/api/stats/timeseries?step=3600",
		"/api/stats/performance",
		"/api/postbacks?group=g1",
		"/api/logs?limit=10",
	} {
		resp, err := c.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s → 200, got %d", path, resp.StatusCode)
		}
	}
}

func TestLogFiltersEndpoint(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, _ := authedClient(t, srv.URL)

	resp, err := c.Get(srv.URL + "/api/logs/filters")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logs/filters → 200, got %d", resp.StatusCode)
	}
	var out map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	// fakeStats.Breakdown returns [{ru,50}] for any dimension → each
	// dimension must contain a value.
	for _, dim := range []string{"group", "stream", "country", "device", "os", "browser", "brand"} {
		if vals := out[dim]; len(vals) == 0 {
			t.Errorf("no values in response for dimension %q", dim)
		}
	}
}

func TestLogsExportCSV(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, _ := authedClient(t, srv.URL)

	resp, err := c.Get(srv.URL + "/api/logs/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "csv") {
		t.Errorf("export Content-Type = %q", ct)
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), "ts,group,stream") {
		t.Errorf("CSV header missing: %q", buf.String())
	}
}

func TestDeleteLogs(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)

	del := func(q string) int {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/logs"+q, nil)
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	if code := del(""); code != http.StatusBadRequest {
		t.Errorf("delete without group → 400, got %d", code)
	}
	if code := del("?group=g1"); code != http.StatusOK {
		t.Errorf("delete group g1 → 200, got %d", code)
	}
}

func TestHealthNoAuth(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health → 200, got %d", resp.StatusCode)
	}
}

// G-055: a broken traffic source template must not reach the engine.
func TestGroupsSaveRejectsBadSource(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)
	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	for _, payload := range []string{
		`[{"id":"x","source":"nope"}]`,
		`[{"id":"x","sources":[{"id":"a","labels":[{"param":"cid","label":"CID"}]}]}]`,
		`[{"id":"x","sources":[{"id":"a","labels":[{"param":"a&b","label":"GCLID"}]}]}]`,
		`[{"id":"x","sources":[{"id":"a","labels":[{"param":"gclid","label":"G-CLID"}]}]}]`,
		`[{"id":"x","sources":[{"id":"a"},{"id":"a"}]}]`,
	} {
		if code := put(payload); code != http.StatusBadRequest {
			t.Errorf("bad source %s → 400, got %d", payload, code)
		}
	}
	if code := put(`[{"id":"x","source":"google","sources":[{"id":"google","name":"Google Ads","labels":[{"param":"gclid","label":"gclid"},{"param":"utm_source","label":"SRC"}]}]}]`); code != http.StatusOK {
		t.Errorf("valid source → 200, got %d", code)
	}
}

// G-056: broken affiliate network templates and dangling offer references are
// refused at save time.
func TestGroupsSaveRejectsBadNetwork(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)
	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	for _, payload := range []string{
		`[{"id":"x","networks":[{"name":"net"}]}]`,
		`[{"id":"x","networks":[{"id":"a"},{"id":"a"}]}]`,
		`[{"id":"x","networks":[{"id":"a","cid_token":"subid"}]}]`,
		`[{"id":"x","networks":[{"id":"a","cid_token":"{sub id}"}]}]`,
		`[{"id":"x","networks":[{"id":"a","cid_token":"{x}","profit_token":"{x}"}]}]`,
		`[{"id":"x","offers":[{"id":"o1","url":"https://a","status":true,"network_id":"nope"}]}]`,
	} {
		if code := put(payload); code != http.StatusBadRequest {
			t.Errorf("bad network %s → 400, got %d", payload, code)
		}
	}
	if code := put(`[{"id":"x","offers":[{"id":"o1","url":"https://a","status":true,"network_id":"sticky"}],"networks":[{"id":"sticky","name":"StickyAds","cid_token":"{sub1}","profit_token":"{payout}"}]}]`); code != http.StatusOK {
		t.Errorf("valid network → 200, got %d", code)
	}
}

// G-057: the memo needs the engine base and the postback key; both reach the
// authenticated admin only.
func TestMePostbackKey(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, _ := authedClient(t, srv.URL)
	resp, err := c.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var me map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me["engine"] != "http://engine.local" || me["postback_key"] != "pb-secret" {
		t.Errorf("/api/me = %v, want the engine base and the postback key", me)
	}
	anon := &http.Client{}
	if r, _ := anon.Get(srv.URL + "/api/me"); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous /api/me = %d, want 401", r.StatusCode)
	} else {
		r.Body.Close()
	}
}

// G-127: the admin refuses stream roles it cannot run, and a second default.
func TestGroupsSaveRejectsBadStreams(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)
	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	for _, payload := range []string{
		`[{"id":"x","streams":[{"name":"s","type":"fallback"}]}]`,
		`[{"id":"x","streams":[{"name":"s","type":"Forced"}]}]`,
		`[{"id":"x","streams":[{"name":"a","type":"default"},{"name":"b","type":"default"}]}]`,
		`[{"id":"x","streams":[{"name":"s","url_replace":[{"find":"","repl":"new"}]}]}]`,
	} {
		if code := put(payload); code != http.StatusBadRequest {
			t.Errorf("bad stream %s → 400, got %d", payload, code)
		}
	}
	if code := put(`[{"id":"x","streams":[{"name":"vip","type":"forced"},{"name":"rest","type":"default"}]}]`); code != http.StatusOK {
		t.Errorf("valid stream types → 200, got %d", code)
	}
}

// G-059: share links share the namespace with IDs and aliases, and the image
// must be a real absolute URL — a broken card is worse than no card.
func TestGroupsSaveRejectsBadShare(t *testing.T) {
	srv, _, _ := fullServer(t, nil)
	c, csrf := authedClient(t, srv.URL)
	put := func(payload string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/groups", strings.NewReader(payload))
		req.Header.Set("X-CSRF-Token", csrf)
		r, _ := c.Do(req)
		r.Body.Close()
		return r.StatusCode
	}
	for _, payload := range []string{
		`[{"id":"promo","status":true,"share":{"slug":"promo"}}]`,
		`[{"id":"a","share":{"slug":"promo"}},{"id":"b","share":{"slug":"promo"}}]`,
		`[{"id":"g","share":{"slug":"promo","image":"/p.jpg"}}]`,
		`[{"id":"g","share":{"title":"Preview"}}]`,
		`[{"id":"g","share":{"slug":"Summer"}}]`,
	} {
		if code := put(payload); code != http.StatusBadRequest {
			t.Errorf("bad share %s → 400, got %d", payload, code)
		}
	}
	if code := put(`[{"id":"g","share":{"slug":"promo","title":"Preview","image":"https://cdn.example/p.jpg"}}]`); code != http.StatusOK {
		t.Errorf("valid share → 200, got %d", code)
	}
}
