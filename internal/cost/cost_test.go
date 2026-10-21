package cost

import (
	"math"
	"testing"
)

// Цены кампании G-119…G-123 (паритет Binom v2): CPC/CPM/CPA/CPV/Revshare,
// потери трафика, процент постбеков источнику, валюта к USD.

// --- модели цены ---

func TestCostCPC(t *testing.T) {
	if got := ForClick(Model{Kind: KindCPC, Value: 0.05}, 0); got != 0.05 {
		t.Errorf("CPC click = %v, want 0.05", got)
	}
}

func TestCostCPM(t *testing.T) {
	// CPM — цена за 1000 показов: один клик стоит value/1000.
	if got := ForClick(Model{Kind: KindCPM, Value: 2.0}, 0); got != 0.002 {
		t.Errorf("CPM click = %v, want 0.002", got)
	}
}

func TestCostCPV(t *testing.T) {
	if got := ForClick(Model{Kind: KindCPV, Value: 0.01}, 0); got != 0.01 {
		t.Errorf("CPV visit = %v, want 0.01", got)
	}
}

func TestCostCPANotOnClick(t *testing.T) {
	// CPA назначается только в момент конверсии, клик стоит 0.
	if got := ForClick(Model{Kind: KindCPA, Value: 3.0}, 0); got != 0 {
		t.Errorf("CPA click = %v, want 0", got)
	}
}

func TestCostCPAOnConversion(t *testing.T) {
	if got := ForConversion(Model{Kind: KindCPA, Value: 3.0}, 0); got != 3.0 {
		t.Errorf("CPA conversion = %v, want 3.0", got)
	}
}

func TestCostRevshare(t *testing.T) {
	// Revshare: стоимость клика = payout конверсии × Payout%.
	if got := ForConversion(Model{Kind: KindRevshare, PayoutPct: 40}, 5.0); got != 2.0 {
		t.Errorf("Revshare conversion = %v, want 2.0", got)
	}
	if got := ForClick(Model{Kind: KindRevshare, PayoutPct: 40}, 0); got != 0 {
		t.Errorf("Revshare click = %v, want 0", got)
	}
}

func TestCostUnknownModelIsZero(t *testing.T) {
	if got := ForClick(Model{Kind: "barter"}, 0); got != 0 {
		t.Errorf("unknown model click = %v, want 0", got)
	}
	if got := ForConversion(Model{}, 7.0); got != 0 {
		t.Errorf("empty model conversion = %v, want 0", got)
	}
}

// --- потери и проценты ---

func TestAdjustForLoss(t *testing.T) {
	// Binom: new CPC = CPC / (1 - loss/100). 10% потерь → 0.05/0.9.
	if got := AdjustForLoss(0.05, 10); math.Abs(got-0.0555555555) > 1e-9 {
		t.Errorf("AdjustForLoss(0.05,10) = %v, want 0.05556", got)
	}
	if got := AdjustForLoss(0.05, 0); got != 0.05 {
		t.Errorf("AdjustForLoss(0.05,0) = %v, want 0.05", got)
	}
	if got := AdjustForLoss(0.05, 50); math.Abs(got-0.1) > 1e-12 {
		t.Errorf("AdjustForLoss(0.05,50) = %v, want 0.1", got)
	}
	// Невалидный процент не должен давать деление на ноль/Inf: цена как есть,
	// а конфиг такой процент не сохранит.
	for _, bad := range []float64{100, 101, -1} {
		if got := AdjustForLoss(0.05, bad); got != 0.05 {
			t.Errorf("AdjustForLoss(0.05,%v) = %v, want 0.05", bad, got)
		}
	}
}

func TestForClickPaidUSD(t *testing.T) {
	rates := map[string]float64{"USD": 1, "EUR": 1.08}
	m := Model{Kind: KindCPC, Value: 0.05, Currency: "EUR", LossPct: 10}
	got, ok := ForClickPaidUSD(m, rates)
	if !ok || math.Abs(got-0.06) > 1e-9 {
		t.Errorf("ForClickPaidUSD = (%v,%v), want 0.06 (0.05/0.9*1.08)", got, ok)
	}
	// CPA на клике не стоит: потери его не трогают.
	if got, ok := ForClickPaidUSD(Model{Kind: KindCPA, Value: 3, LossPct: 10}, rates); !ok || got != 0 {
		t.Errorf("CPA click = (%v,%v), want 0", got, ok)
	}
	if _, ok := ForClickPaidUSD(Model{Kind: KindCPC, Value: 1, Currency: "GBP", LossPct: 10}, rates); ok {
		t.Error("GBP without a rate must be not-ok")
	}
}

func TestPassesPercent(t *testing.T) {
	// Детерминированное ядро сэмплирования (draw в [0,100)).
	if !PassesPercent(50, 49.9) || PassesPercent(50, 50.0) {
		t.Errorf("PassesPercent(50) boundary wrong")
	}
	if !PassesPercent(100, 99.99) || PassesPercent(0, 0) {
		t.Errorf("PassesPercent edges wrong")
	}
}

// --- валюта ---

func TestNormalizeToUSD(t *testing.T) {
	rates := map[string]float64{"USD": 1, "EUR": 1.08, "RUB": 0.011}
	if got, ok := NormalizeToUSD(100, "EUR", rates); !ok || got < 107.99 || got > 108.01 {
		t.Errorf("100 EUR = (%v,%v), want ~108", got, ok)
	}
	if got, ok := NormalizeToUSD(50, "usd", rates); !ok || got != 50 {
		t.Errorf("50 usd = (%v,%v), want 50", got, ok)
	}
	if _, ok := NormalizeToUSD(10, "XXX", rates); ok {
		t.Errorf("unknown currency must fail")
	}
	if _, ok := NormalizeToUSD(10, "EUR", nil); ok {
		t.Errorf("nil rates must fail (кроме USD)")
	}
	if got, ok := NormalizeToUSD(10, "", nil); !ok || got != 10 {
		t.Errorf("empty currency = USD by default, got (%v,%v)", got, ok)
	}
}

func TestForClickUSD(t *testing.T) {
	// Без курсов: USD/пустая валюта считаются 1:1, чужая — честный отказ.
	if got, ok := ForClickUSD(Model{Kind: KindCPC, Value: 0.05}, nil); !ok || got != 0.05 {
		t.Errorf("CPC USD = (%v,%v), want (0.05,true)", got, ok)
	}
	if _, ok := ForClickUSD(Model{Kind: KindCPC, Value: 0.05, Currency: "EUR"}, nil); ok {
		t.Errorf("CPC EUR without rates must fail, not silently zero")
	}
	rates := map[string]float64{"EUR": 1.08}
	if got, ok := ForClickUSD(Model{Kind: KindCPM, Value: 10.8, Currency: "EUR"}, rates); !ok || got < 0.01166 || got > 0.01167 {
		t.Errorf("CPM 10.8 EUR = (%v,%v), want ~0.011664", got, ok)
	}
}

func TestForConversionStatus(t *testing.T) {
	cpa := Model{Kind: KindCPA, Value: 3.0}
	if got := ForConversionStatus(cpa, "sale", 9.99); got != 3.0 {
		t.Errorf("CPA sale = %v, want 3.0", got)
	}
	// Trash отменяет выплату — стоимости конверсии нет (зеркало RevenueByStatus).
	if got := ForConversionStatus(cpa, "trash", 9.99); got != 0 {
		t.Errorf("CPA trash = %v, want 0", got)
	}
	rs := Model{Kind: KindRevshare, PayoutPct: 40}
	if got := ForConversionStatus(rs, "sale", 5.0); got != 2.0 {
		t.Errorf("Revshare sale = %v, want 2.0", got)
	}
	if got := ForConversionStatus(rs, "trash", 5.0); got != 0 {
		t.Errorf("Revshare trash = %v, want 0", got)
	}
	// Клик-модели в конверсии не платят второй раз.
	if got := ForConversionStatus(Model{Kind: KindCPC, Value: 0.05}, "sale", 5.0); got != 0 {
		t.Errorf("CPC conversion = %v, want 0", got)
	}
}

func TestForConversionStatusUSD(t *testing.T) {
	m := Model{Kind: KindCPA, Value: 3.0}
	if got, ok := ForConversionStatusUSD(m, "sale", 9.99, nil); !ok || got != 3.0 {
		t.Errorf("CPA USD sale = (%v,%v), want (3,true)", got, ok)
	}
	me := Model{Kind: KindCPA, Value: 3.0, Currency: "EUR"}
	if _, ok := ForConversionStatusUSD(me, "sale", 9.99, nil); ok {
		t.Errorf("CPA EUR without rates must fail")
	}
}
