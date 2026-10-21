package cost

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ecbSample = `<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01"
                 xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
  <gesmes:subject>Reference rates</gesmes:subject>
  <Cube>
    <Cube time='2026-10-20'>
      <Cube currency='USD' rate='1.0800'/>
      <Cube currency='JPY' rate='162.50'/>
      <Cube currency='GBP' rate='0.8600'/>
    </Cube>
  </Cube>
</gesmes:Envelope>`

func TestParseECB(t *testing.T) {
	rates, date, err := ParseECB(strings.NewReader(ecbSample))
	if err != nil {
		t.Fatalf("ParseECB: %v", err)
	}
	if date != "2026-10-20" {
		t.Errorf("date = %q, want 2026-10-20", date)
	}
	// USD per 1 unit: EUR = 1.08 (1 EUR = 1.08 USD), JPY = 1.08/162.5.
	close := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	if got := rates["USD"]; got != 1 {
		t.Errorf("USD = %v, want 1", got)
	}
	if got := rates["EUR"]; !close(got, 1.08) {
		t.Errorf("EUR = %v, want 1.08", got)
	}
	if got := rates["JPY"]; !close(got, 1.08/162.5) {
		t.Errorf("JPY = %v, want %.10f", got, 1.08/162.5)
	}
	if got := rates["GBP"]; !close(got, 1.08/0.86) {
		t.Errorf("GBP = %v, want %.10f", got, 1.08/0.86)
	}
}

func TestParseECBRejectsBrokenFeeds(t *testing.T) {
	cases := map[string]string{
		"broken xml":  `<Cube><Cube time='x'>`,
		"no cubes":    `<?xml version="1.0"?><Envelope/>`,
		"no usd":      `<Cube><Cube time='2026-10-20'><Cube currency='JPY' rate='162.5'/></Cube></Cube>`,
		"zero rate":   `<Cube><Cube time='2026-10-20'><Cube currency='USD' rate='1'/><Cube currency='JPY' rate='0'/></Cube></Cube>`,
		"bad rate":    `<Cube><Cube time='2026-10-20'><Cube currency='USD' rate='1'/><Cube currency='JPY' rate='abc'/></Cube></Cube>`,
		"negative":    `<Cube><Cube time='2026-10-20'><Cube currency='USD' rate='1'/><Cube currency='JPY' rate='-2'/></Cube></Cube>`,
		"empty input": ``,
	}
	for name, raw := range cases {
		if _, _, err := ParseECB(strings.NewReader(raw)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRatesFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rates.json")
	if err := SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.08}, "2026-10-20"); err != nil {
		t.Fatalf("SaveRates: %v", err)
	}
	got, _, err := LoadRates(path)
	if err != nil {
		t.Fatalf("LoadRates: %v", err)
	}
	if got["EUR"] != 1.08 || got["USD"] != 1 {
		t.Errorf("round trip = %v", got)
	}
	// No temp file left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want just the rates file", len(entries))
	}
}

func TestLoadRatesMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if rates, _, err := LoadRates(filepath.Join(dir, "absent.json")); err != nil || rates != nil {
		t.Errorf("missing file: rates=%v err=%v, want nil,nil", rates, err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadRates(bad); err == nil {
		t.Error("corrupt file: want an error, the engine must not price traffic on garbage")
	}
	// An empty USD entry makes every conversion wrong — reject it.
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"rates":{"USD":0}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadRates(empty); err == nil {
		t.Error("USD=0: want an error")
	}
}
