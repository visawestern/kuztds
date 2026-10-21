package cron

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egerkuzma/kuztds/internal/cost"
)

const ecbFeed = `<?xml version="1.0"?>
<Envelope xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
  <Cube><Cube time='2026-10-20'>
    <Cube currency='USD' rate='1.0800'/>
    <Cube currency='JPY' rate='162.50'/>
  </Cube></Cube>
</Envelope>`

func TestRatesJobWritesFile(t *testing.T) {
	e := newEnv(t)
	url := e.serve("/ecb.xml", ecbFeed)
	msg, err := e.r.exec(context.Background(), Config{Rates: Rates{URL: url}}, JobRates)
	if err != nil {
		t.Fatalf("rates job: %v", err)
	}
	if !strings.Contains(msg, "3 currencies") {
		t.Errorf("message = %q, want the currency count", msg)
	}
	path := filepath.Join(e.dir, "data", "rates.json")
	rates, date, err := cost.LoadRates(path)
	if err != nil {
		t.Fatalf("LoadRates: %v", err)
	}
	if date != "2026-10-20" {
		t.Errorf("date = %q", date)
	}
	if rates["USD"] != 1 || rates["JPY"] == 0 {
		t.Errorf("rates = %v", rates)
	}
}

func TestRatesJobKeepsOldFileOnFailure(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.dir, "data", "rates.json")
	if err := cost.SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.08}, "2026-10-19"); err != nil {
		t.Fatal(err)
	}
	// A 200 that is not a feed must not replace yesterday's working rates.
	url := e.serve("/broken.xml", "<html>maintenance</html>")
	if _, err := e.r.exec(context.Background(), Config{Rates: Rates{URL: url}}, JobRates); err == nil {
		t.Fatal("broken feed: want an error")
	}
	rates, date, err := cost.LoadRates(path)
	if err != nil {
		t.Fatalf("previous rates must stay readable: %v", err)
	}
	if date != "2026-10-19" || rates["EUR"] != 1.08 {
		t.Errorf("previous rates replaced: %v %s", rates, date)
	}
}

func TestRatesJobNeedsURL(t *testing.T) {
	e := newEnv(t)
	if _, err := e.r.exec(context.Background(), Config{Rates: Rates{}}, JobRates); err == nil {
		t.Error("empty URL: want an error, not a silent no-op")
	}
}

func TestRatesConfigValidated(t *testing.T) {
	if err := (Config{Rates: Rates{URL: "ftp://x"}}).Validate(); err == nil {
		t.Error("ftp URL: want an error")
	}
	if err := (Config{Rates: Rates{URL: "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"}}).Validate(); err != nil {
		t.Errorf("ECB URL rejected: %v", err)
	}
}

func TestRatesJobIsScheduled(t *testing.T) {
	found := false
	for _, j := range Jobs {
		if j == JobRates {
			found = true
		}
	}
	if !found {
		t.Error("rates job is not in Jobs, so it never runs")
	}
	if got := (Config{Rates: Rates{EveryMinutes: 60}}).Interval(JobRates); got.Minutes() != 60 {
		t.Errorf("interval = %v", got)
	}
	if got := (Config{Rates: Rates{EveryMinutes: 1}}).Interval(JobRates); got.Minutes() != 60 {
		t.Errorf("interval below the minimum = %v", got)
	}
}

func TestRatesDefaultURL(t *testing.T) {
	if !strings.Contains(Default().Rates.URL, "eurofxref-daily.xml") {
		t.Errorf("default URL = %q", Default().Rates.URL)
	}
	if os.Getenv("KUZTDS_RATES_TEST") == "" {
		// default job stays off until the admin enables it
		if Default().Rates.Enabled {
			t.Error("rates must be off by default, like the other jobs")
		}
	}
}
