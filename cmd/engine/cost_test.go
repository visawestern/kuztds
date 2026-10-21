package main

import (
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
)

// Проводка цены в событие (G-119…G-121): стоимость клика пишется в лог
// по модели ВХОДНОЙ кампании; без курсов не-USD даёт честный 0.

func costGroup(id, model string, value float64, cur string) *config.Groups {
	return config.NewGroups(&config.Group{
		ID: id, Status: true, Redirect: "stop",
		Cost:    config.CostModel{Model: model, Value: value, Currency: cur},
		Streams: []config.Stream{{Name: "s", Status: true}},
	})
}

func TestCostLoggedFromEntryCampaign(t *testing.T) {
	h, ins, stop := curlEnv(t, costGroup("g", "cpc", 0.05, "USD"), "0")
	do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if len(ins.events) != 1 {
		t.Fatalf("want 1 logged event, got %d", len(ins.events))
	}
	if got := ins.events[0].Cost; got != 0.05 {
		t.Errorf("event cost = %v, want 0.05", got)
	}
	if got := ins.events[0].CostCur; got != "USD" {
		t.Errorf("event cost_cur = %q, want USD", got)
	}
}

func TestCostZeroWithoutModel(t *testing.T) {
	h, ins, stop := curlEnv(t, costGroup("g", "", 0, ""), "0")
	do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if len(ins.events) != 1 {
		t.Fatalf("want 1 logged event, got %d", len(ins.events))
	}
	if got := ins.events[0].Cost; got != 0 {
		t.Errorf("event cost = %v, want 0", got)
	}
}

func TestCostZeroForNonUSDWithoutRates(t *testing.T) {
	// Курсов нет (ECB-синк впереди): EUR честно даёт 0, а не мусор в агрегатах.
	h, ins, stop := curlEnv(t, costGroup("g", "cpc", 0.05, "EUR"), "0")
	do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if len(ins.events) != 1 {
		t.Fatalf("want 1 logged event, got %d", len(ins.events))
	}
	if got := ins.events[0].Cost; got != 0 {
		t.Errorf("event cost = %v, want 0 (no rates)", got)
	}
}

func TestCostFromEntryGroupAcrossLink(t *testing.T) {
	// Деньги принадлежат входной кампании: a (cpc) → b (без цены).
	gg := config.NewGroups(
		&config.Group{ID: "a", Status: true, Redirect: "stop",
			Cost:    config.CostModel{Model: "cpc", Value: 0.07},
			Streams: []config.Stream{{Name: "s", Status: true, Out: config.Output{Redirect: "group", Out: "b"}}}},
		&config.Group{ID: "b", Status: true, Redirect: "stop",
			Streams: []config.Stream{{Name: "s", Status: true}}},
	)
	h, ins, stop := curlEnv(t, gg, "0")
	do(t, h, "/a", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if len(ins.events) != 1 {
		t.Fatalf("want 1 logged event, got %d", len(ins.events))
	}
	e := ins.events[0]
	if e.GroupID != "b" {
		t.Errorf("event group = %q, want b (serving group)", e.GroupID)
	}
	if e.Cost != 0.07 {
		t.Errorf("event cost = %v, want 0.07 (entry campaign price)", e.Cost)
	}
}

func TestCostAdjustedForTrafficLoss(t *testing.T) {
	// G-122: Binom new CPC = CPC / (1 - loss/100); 10% потерь → 0.05/0.9.
	gg := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Cost:    config.CostModel{Model: "cpc", Value: 0.05, LossPct: 10},
		Streams: []config.Stream{{Name: "s", Status: true}},
	})
	h, ins, stop := curlEnv(t, gg, "0")
	do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if len(ins.events) != 1 {
		t.Fatalf("want 1 logged event, got %d", len(ins.events))
	}
	if got := ins.events[0].Cost; got < 0.0555 || got > 0.0556 {
		t.Errorf("event cost = %v, want 0.0556 (loss-adjusted CPC)", got)
	}
}
