package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egerkuzma/kuztds/internal/cost"
)

// G-121: cron пишет rates.json, движок читает и горячо перечитывает —
// цена не-USD кампании перестаёт молча обнуляться.

func TestRatesStoreLoadsAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rates.json")
	s := newRatesStore(path, discardLog())
	if s.Rates() != nil {
		t.Error("missing file must give nil rates")
	}
	if _, err := s.reload(); err != nil {
		t.Errorf("missing file reload: %v", err)
	}
	if err := cost.SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.08}, "2026-10-20"); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.reload(); err != nil || !changed {
		t.Fatalf("first reload = (%v,%v), want (true,nil)", changed, err)
	}
	if got := s.Rates()["EUR"]; got != 1.08 {
		t.Errorf("EUR = %v, want 1.08", got)
	}
	if changed, _ := s.reload(); changed {
		t.Error("unchanged file must not reload")
	}
	if err := cost.SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.10}, "2026-10-21"); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.reload(); err != nil || !changed {
		t.Fatalf("second reload = (%v,%v)", changed, err)
	}
	if got := s.Rates()["EUR"]; got != 1.10 {
		t.Errorf("EUR after reload = %v, want 1.10", got)
	}
}

func TestRatesStoreKeepsOldOnCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rates.json")
	if err := cost.SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.08}, "2026-10-20"); err != nil {
		t.Fatal(err)
	}
	s := newRatesStore(path, discardLog())
	if s.Rates()["EUR"] != 1.08 {
		t.Fatal("initial load failed")
	}
	if err := os.WriteFile(path, []byte("{torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reload(); err == nil {
		t.Error("corrupt file: want an error")
	}
	if got := s.Rates()["EUR"]; got != 1.08 {
		t.Errorf("EUR after corrupt write = %v, want the previous 1.08", got)
	}
}

func TestCostUsesRatesFile(t *testing.T) {
	// 0.05 EUR по курсу 1.08 = 0.054 USD.
	h, ins, stop := curlEnvWith(t, costGroup("g", "cpc", 0.05, "EUR"), "0", func(d *engineDeps) {
		path := filepath.Join(d.dataDir, "rates.json")
		if err := cost.SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.08}, "2026-10-20"); err != nil {
			t.Fatal(err)
		}
		d.rates = newRatesStore(path, d.log)
	})
	do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if len(ins.events) != 1 {
		t.Fatalf("want 1 logged event, got %d", len(ins.events))
	}
	if got := ins.events[0].Cost; got < 0.0539 || got > 0.0541 {
		t.Errorf("event cost = %v, want 0.054 USD", got)
	}
	if got := ins.events[0].CostCur; got != "USD" {
		t.Errorf("event cost_cur = %q, want USD (value is normalized)", got)
	}
}
