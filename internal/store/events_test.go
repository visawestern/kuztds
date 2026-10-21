package store

import (
	"strings"
	"testing"
	"time"

	"github.com/egerkuzma/kuztds/internal/logbuf"
)

// G-048: the INSERT must keep columns and values in the same order when the
// optional groups (offer, network, cost) are present or absent.
func TestEventInsertShape(t *testing.T) {
	ev := logbuf.Event{
		Ts: time.Now(), GroupID: "g", Stream: "s", Out: "https://x",
		Offer: "o1", ASN: 15169, Org: "Google", Timezone: "+3", Via: "g/s",
		Cost: 0.05, CostCur: "USD",
	}
	for _, c := range []struct {
		net, cost, offer bool
	}{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{false, false, true},
		{true, true, true},
	} {
		cols := eventInsertCols(c.net, c.cost, c.offer)
		row := eventInsertRow(ev, c.net, c.cost, c.offer)
		n := len(strings.Split(cols, ","))
		if len(row) != n {
			t.Fatalf("net=%v cost=%v offer=%v: %d columns, %d values", c.net, c.cost, c.offer, n, len(row))
		}
		if c.offer && !strings.Contains(cols, "offer") {
			t.Errorf("offer column missing: %s", cols)
		}
		if !c.offer && strings.Contains(cols, "offer") {
			t.Errorf("offer column present without support: %s", cols)
		}
		if c.cost != strings.Contains(cols, "cost_cur") {
			t.Errorf("cost columns do not follow the flag: %s", cols)
		}
		if c.net != strings.Contains(cols, "asn") {
			t.Errorf("network columns do not follow the flag: %s", cols)
		}
	}
}
