package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/cost"
	"github.com/egerkuzma/kuztds/internal/security"
)

type fakeRecorder struct {
	group, offer string
	groupErr     error
	recordErr    error
	recorded     bool
	cid, status  string
	tid, mode    string
	profit, cost float64
}

func (f *fakeRecorder) EventGroupID(context.Context, string) (string, error) {
	return f.group, f.groupErr
}
func (f *fakeRecorder) EventOfferID(context.Context, string) (string, error) { return f.offer, nil }
func (f *fakeRecorder) RecordPostback(_ context.Context, cid string, profit float64, status, tid, _ string, convCost float64, mode string) (bool, error) {
	f.cid, f.profit, f.status, f.tid, f.cost, f.mode = cid, profit, status, tid, convCost, mode
	return f.recorded, f.recordErr
}

type fakeCaps struct {
	once map[string]bool
	inc  int
}

func (f *fakeCaps) CountConversion(context.Context, string, string, time.Time) error {
	f.inc++
	return nil
}
func (f *fakeCaps) CountConversionOnce(_ context.Context, _, _, dedup string, _ time.Time) (bool, error) {
	if f.once == nil {
		f.once = map[string]bool{}
	}
	if f.once[dedup] {
		return false, nil
	}
	f.once[dedup] = true
	return true, nil
}

func conversionServer(t *testing.T, rec *fakeRecorder, caps *fakeCaps, grp config.Group, dir string) *httptest.Server {
	t.Helper()
	hash, _ := security.HashPassword("p@ss")
	s, err := New(Config{
		AdminUser: "admin", PasswordHash: hash,
		Sessions: security.NewMemoryStore(),
		Stats:    fakeStats{}, Recorder: rec, Caps: caps,
		Groups: &fakeGroupsMode{group: grp}, Lists: &fakeLists{data: map[string]string{}},
		Limiter: allowAll{}, SessionTTL: time.Hour, CookieSecure: false,
		DataDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

type fakeGroupsMode struct{ group config.Group }

func (f *fakeGroupsMode) List(context.Context) ([]config.Group, error) {
	return []config.Group{f.group}, nil
}
func (f *fakeGroupsMode) Save(context.Context, []config.Group) error { return nil }

func postConversion(t *testing.T, c *http.Client, base, csrf, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/conversions", strings.NewReader(body))
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestManualConversionConvertsCurrencyAndSpendsCap(t *testing.T) {
	dir := t.TempDir()
	if err := cost.SaveRates(filepath.Join(dir, "rates.json"), map[string]float64{"USD": 1, "EUR": 1.1}, "2026-10-22"); err != nil {
		t.Fatal(err)
	}
	rec := &fakeRecorder{group: "g1", offer: "main", recorded: true}
	caps := &fakeCaps{}
	grp := config.Group{ID: "g1", Name: "demo", Cost: config.CostModel{Model: "cpa", Value: 3}}
	srv := conversionServer(t, rec, caps, grp, dir)
	c, csrf := authedClient(t, srv.URL)
	resp, out := postConversion(t, c, srv.URL, csrf, `{"cid":"c1","status":"sale","tid":"t1","payout":10,"currency":"eur"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create → 200, got %d (%v)", resp.StatusCode, out)
	}
	if rec.profit != 11 || rec.cost != 3 {
		t.Errorf("profit/cost = %v/%v, want 11/3", rec.profit, rec.cost)
	}
	if rec.mode != config.PostbackDedupeUpdate || out["status"] != "recorded" {
		t.Errorf("mode/status = %q/%v", rec.mode, out["status"])
	}
	if len(caps.once) != 1 {
		t.Errorf("cap once = %v, want exactly one slot", caps.once)
	}
}

func TestManualConversionValidation(t *testing.T) {
	rec := &fakeRecorder{group: "g1", recorded: true}
	srv := conversionServer(t, rec, &fakeCaps{}, config.Group{ID: "g1"}, t.TempDir())
	c, csrf := authedClient(t, srv.URL)
	for _, body := range []string{
		`{}`,
		`{"cid":"c1","status":"not/a/status","profit":1}`,
		`{"cid":"c1","status":"sale"}`,
		`{"cid":"c1","status":"sale","profit":1,"currency":"US"}`,
		`{"cid":"c1","status":"sale","profit":1,"currency":"GBP"}`,
	} {
		resp, out := postConversion(t, c, srv.URL, csrf, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s → 400, got %d (%v)", body, resp.StatusCode, out)
		}
	}
	rec.groupErr = errors.New("no click")
	resp, out := postConversion(t, c, srv.URL, csrf, `{"cid":"missing","status":"sale","profit":1}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown cid → 404, got %d (%v)", resp.StatusCode, out)
	}
}

func TestManualConversionNewModeSpendsEverySlot(t *testing.T) {
	rec := &fakeRecorder{group: "g1", offer: "main", recorded: true}
	caps := &fakeCaps{}
	srv := conversionServer(t, rec, caps, config.Group{ID: "g1", PostbackDedupe: config.PostbackDedupeNew}, t.TempDir())
	c, csrf := authedClient(t, srv.URL)
	for i := 0; i < 2; i++ {
		resp, out := postConversion(t, c, srv.URL, csrf, `{"cid":"c1","status":"sale","tid":"t1","profit":1}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create %d → 200, got %d (%v)", i, resp.StatusCode, out)
		}
	}
	if caps.inc != 2 || len(caps.once) != 0 {
		t.Fatalf("new mode caps = inc %d, once %v; want 2 and none", caps.inc, caps.once)
	}
}

func TestManualConversionIgnoreModeDoesNotSpendCap(t *testing.T) {
	rec := &fakeRecorder{group: "g1", offer: "main", recorded: false}
	caps := &fakeCaps{}
	srv := conversionServer(t, rec, caps, config.Group{ID: "g1", PostbackDedupe: config.PostbackDedupeIgnore}, t.TempDir())
	c, csrf := authedClient(t, srv.URL)
	resp, out := postConversion(t, c, srv.URL, csrf, `{"cid":"c1","status":"sale","profit":1}`)
	if resp.StatusCode != http.StatusOK || out["status"] != "ignored" {
		t.Fatalf("ignored → 200/ignored, got %d (%v)", resp.StatusCode, out)
	}
	if len(caps.once) != 0 || caps.inc != 0 {
		t.Fatalf("ignored conversion spent a cap slot: %+v", caps)
	}
}
