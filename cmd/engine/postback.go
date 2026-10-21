package main

import (
	"net/url"
	"strconv"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/conversion"
	"github.com/egerkuzma/kuztds/internal/cost"
)

// postbackData — разобранный входящий постбек (G-301…G-304).
type postbackData struct {
	CID       string
	Profit    float64
	HasProfit bool   // profit= был числом — ноль в сообщении не заменяется выплатой оффера (G-049)
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
	var profit float64
	hasProfit := false
	if raw := q.Get("profit"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			profit, hasProfit = v, true
		}
	}
	return postbackData{CID: cid, Profit: profit, HasProfit: hasProfit, Status: status, TID: q.Get("tid")}, true
}

// effectiveProfit — выручка конверсии (G-049). Сумма из постбека главнее: сеть
// присылает фактическую выплату. Если её нет, берётся выплата оффера, на
// который ушёл визит, в его валюте. ok=false: курса нет — вызывающий пишет 0 и
// объясняет это в debug-логе, а не смешивает валюты в отчётах.
func effectiveProfit(profit float64, hasProfit bool, grp config.Group, offerID string, rates map[string]float64) (float64, bool) {
	if hasProfit {
		return profit, true
	}
	off, ok := grp.Offer(offerID)
	if !ok || off.Payout <= 0 {
		return 0, true
	}
	return cost.NormalizeToUSD(off.Payout, off.Currency, rates)
}
