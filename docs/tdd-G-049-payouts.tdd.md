# TDD evidence — G-049 «Выплата числом, из сообщения, валюта»

| # | Критерий | Где |
|---|----------|-----|
| 1 | Конфиг + валидация | `internal/config/config.go`: `Offer.Payout`, `Offer.Currency`; `ValidateOffers` — payout ≥ 0, валюта `^[A-Za-z]{3}$` (пусто = USD) |
| 2 | Движок, детерминированно | `cmd/engine/postback.go`: `postbackData.HasProfit`, `effectiveProfit`; `handler.go` — выплата оффера до расчёта CPA/Revshare и записи; `EventOfferID` в `internal/store/clickhouse.go` |
| 3 | Лог/отчёт | выручка конверсии по-прежнему в `postbacks.profit`; оффер визита — колонка `offer` (G-048); CSV конверсий без изменений |
| 4 | Админ | поля Payout/Currency в карточке оффера (`offerCard`), `normOffers` (в pure-блоке), 400 на плохие значения |
| 5 | Доки + пример + регресс | USAGE EN/RU, README, `groups.example.json` (`payout: 1.5, currency: "EUR"`), полный `go test ./...` |

## RED

1. `internal/config/offers_test.go` — кейсы `negative payout`, `payout currency junk`,
   `payout currency long` до реализации полей:

```
cmd/engine/offer_test.go:88:29: unknown field Payout in struct literal of type config.Offer
cmd/engine/offer_test.go:108:14: undefined: effectiveProfit
FAIL github.com/egerkuzma/kuztds/cmd/engine [build failed]
```

2. `web_logic_test.go` — ожидание `payout/currency` в `normOffers`:

```
FAIL offers are trimmed
  got  [{"id":"o1","name":"Main",...}]                        (без payout/currency)
  want [{"id":"o1","name":"Main",...,"payout":1.5,"currency":"EUR"}]
FAIL negative payout clamps to 0
```

3. `TestGroupsSaveRejectsBadOffer` — добавлены payload'ы с payout −1 и `US$`;
   проходят только после серверной валидации.

## GREEN — семантика (таблица теста `TestEffectiveProfit`)

| profit= в постбеке | оффер | результат |
|---|---|---|
| `9` | o1 (USD 2.5) | 9 — сообщение главнее |
| `0` (явно) | o1 | 0 — явный ноль не заменяется |
| нет | o1 (USD 2.5) | 2.5 |
| нет | o1 (EUR 2.5, курс 1.1) | 2.75 |
| нет | оффер без payout | 0 |
| нет | неизвестный id / пусто | 0 |
| нет | GBP без курса | 0, `ok=false` → debug-строка, в отчёт не пишется догадка |

`TestParsePostbackProfitPresence`: отсутствующий и нечисловой `profit` —
`HasProfit=false`; `profit=0` — `HasProfit=true`.

## Гейты

- `go build ./...`, `go vet ./...`, `gofmt -l .` — чисто
- `go test ./... -count=1` — все пакеты `ok`
- `go test -tags=uitest ./internal/admin/ -count=1` — `ok`
- golangci-lint в окружении отсутствует — гэп зафиксирован.

## Замечание

Выручка пишется в USD (как `cost`), валюта выплаты переводится по курсам ЕЦБ;
без курса — 0 + debug, чтобы отчёты не смешивали валюты. `postback_pct`
(G-123) и исходящие S2S (G-297) по-прежнему вне среза.
