package main

import (
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/conversion"
	"github.com/egerkuzma/kuztds/internal/cost"
)

// postbackData — разобранный входящий постбек (G-301…G-304).
type postbackData struct {
	CID       string
	Profit    float64
	HasProfit bool   // profit=/payout= был числом — ноль в сообщении не заменяется выплатой оффера (G-049)
	Currency  string // G-305: ISO 4217 прибыли; пусто = USD
	Status    string // нормализованный conversion.ParseStatus
	TID       string // номер сделки против дублей (может быть пустым)
}

// parsePostback разбирает query постбека (?pb=KEY&cid=&profit=&status=&tid=).
// ok=false: постбек игнорируется (записывать нечего), ответ всё равно 200 —
// так делает Keitaro 11 для постбеков без валидного status=.
func parsePostback(q url.Values) (postbackData, bool) {
	cid := q.Get("cid")
	if cid == "" {
		return postbackData{}, false
	}
	status, ok := conversion.ParseStatus(q.Get("status"))
	if !ok {
		return postbackData{}, false
	}
	profit, hasProfit := parsePostbackAmount(q)
	return postbackData{
		CID: cid, Profit: profit, HasProfit: hasProfit,
		Currency: strings.ToUpper(strings.TrimSpace(q.Get("currency"))),
		Status:   status, TID: q.Get("tid"),
	}, true
}

// parsePostbackAmount reads profit first (Keitaro's native name) and payout as
// the compatibility alias. An unparseable profit does not shadow a valid
// payout: a network sends one of them, never a deliberately broken first one.
func parsePostbackAmount(q url.Values) (float64, bool) {
	for _, key := range [...]string{"profit", "payout"} {
		raw := strings.TrimSpace(q.Get(key))
		if raw == "" {
			continue
		}
		if v, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v, true
		}
	}
	return 0, false
}

// effectiveProfit — выручка конверсии (G-049/G-305). Сумма из постбека главнее:
// сеть присылает фактическую выплату в валюте currency=. Если суммы нет,
// берётся выплата оффера, на который ушёл визит, в его валюте. ok=false: курса
// нет — вызывающий пишет 0 и объясняет это в debug-логе, а не смешивает валюты
// в отчётах.
func effectiveProfit(pbd postbackData, grp config.Group, offerID string, rates map[string]float64) (float64, bool) {
	if pbd.HasProfit {
		// G-305: the amount arrives in the network's currency. Unknown codes
		// return false so the caller records 0 and explains it, never mixing
		// currencies in the USD reports.
		return cost.NormalizeToUSD(pbd.Profit, pbd.Currency, rates)
	}
	off, ok := grp.Offer(offerID)
	if !ok || off.Payout <= 0 {
		return 0, true
	}
	return cost.NormalizeToUSD(off.Payout, off.Currency, rates)
}
