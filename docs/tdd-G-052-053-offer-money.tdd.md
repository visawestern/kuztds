# TDD evidence — G-052 «Перевод валют выплат» и G-053 «Прибыль и расход на предложение»

## G-052 — закрыто инфраструктурой G-049

Выплата оффера и цена трафика задаются в своих валютах и переводятся в USD по
курсам ЕЦБ в момент события; отчёты и `cost` живут в USD, поэтому валюты не
смешиваются.

- `cost.NormalizeToUSD` + `ForClickUSD`/`ForConversionStatusUSD` (G-119…G-122);
- `effectiveProfit` (G-049): выплата оффера в его валюте → USD; без курса
  `ok=false`, пишется 0 и debug-строка;
- тесты: `internal/cost/cost_test.go`, `cmd/engine/offer_test.go`
  (`offer payout converted` — EUR 2.5 × 1.1 = 2.75; `currency without a rate`);
- UI: поле валюты выплаты у оффера, поле валюты цены у потока.

Нового кода срез не потребовал; доки дополнены (USAGE EN/RU, README).

## G-053 — профит и расход на оффер

| # | Критерий | Где |
|---|----------|-----|
| 1 | Конфиг/миграция | `migrations/clickhouse/008_postback_offer.sql`; проба `postbackOfferCols` + `ALTER … IF NOT EXISTS`; старые таблицы работают |
| 2 | Движок | `RecordPostback(..., offer, convCost)` — оффер визита из `events`; e2e-путь постбека не менялся (`cmd/engine/offer_test.go`, `postback_test.go`) |
| 3 | Лог/отчёт | `Performance` группирует события и постбеки по `(поток, стрим, оффер)`; `PerfRow.Offer`; оффер в таблице Conversions и в performance-CSV |
| 4 | Админ | performance-таблица показывает оффер как пилюлю у строки стрима; CSV-колонка `offer` |
| 5 | Доки + пример + регресс | USAGE EN/RU, README; `groups.example.json` уже содержит офферы; `go test ./...` |

### RED

```
internal/store/postbacks_test.go:66:40: too many arguments in call to postbackExtraInsert
    have (bool, bool)
    want (bool)
FAIL github.com/egerkuzma/kuztds/internal/store [build failed]
```

### GREEN

```
=== RUN   TestPostbackOfferColumns            --- PASS
=== RUN   TestPostbackRowCols                 --- PASS  (offer в row cols)
=== RUN   TestOfferCapServesFallback          --- PASS
=== RUN   TestEffectiveProfit                 --- PASS
ok  github.com/egerkuzma/kuztds/internal/store
ok  github.com/egerkuzma/kuztds/cmd/engine
```

Семантика: `Profit` = выручка (postbacks, trash исключён) − цена кликов
(`sum(events.cost)` по офферу) − стоимость конверсий (`postbacks.cost`);
визиты без оффера идут пустой ячейкой, как раньше. Пересланные визиты
(`via`) больше не приписываются строке оффера.

## Гейты

- `go build ./...`, `go vet ./...`, `gofmt -l .` — чисто
- `go test ./... -count=1` — все пакеты `ok`
- `go test -tags=uitest ./internal/admin/ -count=1` — `ok`
- golangci-lint в окружении отсутствует — гэп зафиксирован.
