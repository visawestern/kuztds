package config

import "testing"

// Цена кампании G-119…G-123: поля модели в Group + валидация.

func TestCostModelValid(t *testing.T) {
	for _, m := range []CostModel{
		{},
		{Model: "cpc", Value: 0.05, Currency: "USD"},
		{Model: "CPM", Value: 2, Currency: "eur"},
		{Model: "cpa", Value: 3},
		{Model: "cpv", Value: 0.01},
		{Model: "revshare", PayoutPct: 40},
		{LossPct: 10, PostbackPct: 50},
	} {
		if err := m.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", m, err)
		}
	}
}

func TestCostModelInvalid(t *testing.T) {
	for _, m := range []CostModel{
		{Model: "barter", Value: 1},
		{Model: "cpc", Value: -1},
		{Model: "cpc", Value: 1, Currency: "US D"},
		{Model: "revshare", PayoutPct: 101},
		{Model: "revshare", PayoutPct: -1},
		{LossPct: 100}, // 100% потерь = деление на ноль в формуле Binom
		{LossPct: 101},
		{PostbackPct: -5},
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want error", m)
		}
	}
}
