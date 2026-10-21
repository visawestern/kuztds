// Package cost — модели цены кампании G-119…G-123 (паритет Binom v2).
//
// CPC — фикс за клик; CPM — за 1000 (клик стоит value/1000); CPV — за визит;
// CPA — назначается в момент конверсии; Revshare — payout × Payout%.
// Потери трафика и процент постбеков — отдельные чистые функции.
// Валюта приводится к USD по таблице курсов (синк курсов — cron, отдельно).
package cost

import (
	"strings"

	"github.com/egerkuzma/kuztds/internal/conversion"
)

// Kinds of price models.
const (
	KindCPC      = "cpc"
	KindCPM      = "cpm"
	KindCPA      = "cpa"
	KindCPV      = "cpv"
	KindRevshare = "revshare"
)

// Model — цена кампании: вид, значение в Currency за единицу,
// PayoutPct — доля выплаты для revshare.
type Model struct {
	Kind        string
	Value       float64
	Currency    string
	PayoutPct   float64
	LossPct     float64 // G-122: потери трафика, %
	PostbackPct float64 // G-123: доля постбеков источнику, % (100 = все)
}

// normalize lowercases the kind for comparison.
func (m Model) kind() string { return strings.ToLower(strings.TrimSpace(m.Kind)) }

// ForClick — стоимость одного клика/визита. CPA/Revshare на клике стоят 0:
// их цена назначается только конверсией.
func ForClick(m Model, _ float64) float64 {
	switch m.kind() {
	case KindCPC, KindCPV:
		return m.Value
	case KindCPM:
		return m.Value / 1000
	default:
		return 0
	}
}

// ForConversion — стоимость в момент конверсии (payout — выплата сети).
func ForConversion(m Model, payout float64) float64 {
	switch m.kind() {
	case KindCPA:
		return m.Value
	case KindRevshare:
		return payout * m.PayoutPct / 100
	default:
		return 0
	}
}

// AdjustForLoss — цена клика с поправкой на потери трафика G-122.
// Формула Binom v2: new CPC = CPC / (1 - loss/100) — источник берёт деньги за
// трафик, который до трекера не дошёл, поэтому дошедший клик стоит дороже.
// Невалидный процент (<=0 или >=100) цену не меняет: делить на ноль нельзя,
// а такой конфиг отклоняет Validate.
func AdjustForLoss(price, lossPct float64) float64 {
	if lossPct <= 0 || lossPct >= 100 {
		return price
	}
	return price / (1 - lossPct/100)
}

// PassesPercent — детерминированное ядро сэмплирования G-123:
// draw — равномерное в [0,100) (источник — снаружи, тестируемо).
func PassesPercent(pct, draw float64) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	return draw < pct
}

// NormalizeToUSD приводит сумму к USD. Пустая валюта = USD.
// Курсы — единицы USD за 1 единицу валюты (как у ECB).
func NormalizeToUSD(amount float64, currency string, rates map[string]float64) (float64, bool) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "USD" {
		return amount, true
	}
	r, ok := rates[cur]
	if !ok {
		return 0, false
	}
	return amount * r, true
}

// ForClickUSD — стоимость клика в USD: сначала модель, потом валюта.
// ok=false: валюту без курсов посчитать нельзя — вызывающий обязан записать 0
// и залогировать, а не молча смешивать валюты в агрегатах.
func ForClickUSD(m Model, rates map[string]float64) (float64, bool) {
	return NormalizeToUSD(ForClick(m, 0), m.Currency, rates)
}

// ForConversionStatus — стоимость в момент конверсии с учётом статуса.
// trash отменяет выплату (зеркало conversion.RevenueByStatus): стоимости нет.
func ForConversionStatus(m Model, status string, payout float64) float64 {
	if strings.ToLower(strings.TrimSpace(status)) == conversion.StatusTrash {
		return 0
	}
	return ForConversion(m, payout)
}

// ForConversionStatusUSD — стоимость конверсии в USD.
// ok=false: валюту без курсов посчитать нельзя (см. ForClickUSD).
func ForConversionStatusUSD(m Model, status string, payout float64, rates map[string]float64) (float64, bool) {
	return NormalizeToUSD(ForConversionStatus(m, status, payout), m.Currency, rates)
}

// ForClickPaidUSD — фактическая цена клика в USD: модель, поправка на потери
// (G-122), затем валюта. CPA/Revshare на клике стоят 0 и потерями не
// трогаются — их цена назначается конверсией.
func ForClickPaidUSD(m Model, rates map[string]float64) (float64, bool) {
	return NormalizeToUSD(AdjustForLoss(ForClick(m, 0), m.LossPct), m.Currency, rates)
}
