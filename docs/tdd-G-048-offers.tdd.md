# TDD evidence — G-048 «Карточка предложения со ссылкой» (офферы)

Критерии приёмки из `TZ-parity.md` G-048 и чем закрыты.

| # | Критерий | Где |
|---|----------|-----|
| 1 | Конфиг оффера + валидация | `internal/config/config.go` (`Offer`, `Group.Offers`, `ValidateOffers`, `Group.Offer`), `internal/config/offers_test.go` |
| 2 | e2e движка: 302 на ссылку оффера с макросами | `cmd/engine/handler.go` (блок `redirect == "offer"` перед `chance`), `cmd/engine/offer_test.go` |
| 3 | Оффер в логах: ClickHouse + UI + CSV | `internal/logbuf` (`Event.Offer`), миграция `007_event_offer.sql`, `internal/store/clickhouse.go` (`eventOfferCols`, `eventInsertCols/Row`, `LogRow.Offer`), `internal/admin/admin.go` (CSV), `internal/admin/web/index.html` (колонка и деталь) |
| 4 | Админ-экран словами | Flow settings → **Offers** (`offerCard`), тип вывода **Offer** в потоке и в default группы |
| 5 | Доки, пример, регресс | `docs/USAGE.md`/`USAGE.ru.md` → Offers, `configs/groups.example.json` → `offers`, полный `go test ./...` зелёный |

## RED

1. `internal/config/offers_test.go` + `cmd/engine/offer_test.go` написаны до кода:

```
cmd/engine/offer_test.go:16:3: unknown field Offers in struct literal of type config.Group
cmd/engine/offer_test.go:16:21: undefined: config.Offer
FAIL github.com/egerkuzma/kuztds/cmd/engine [build failed]
```

2. `internal/admin/handlers_more_test.go::TestGroupsSaveRejectsBadOffer` — сначала 200
   на мусор (валидации не было), после добавления `g.ValidateOffers()` в
   `handleGroupsSave` — 400 на три плохих payload'а и 200 на валидные.

3. `web_logic_test.go` (node, `-tags=uitest`) — проверка `normOffers` падала
   `ReferenceError: normOffers is not defined` до появления чистой функции.

## GREEN

```
=== RUN   TestOfferRedirectExpandsMacros
    Location = "https://net.example/click?cid=...&p=abc&c=..."
    logged offer = o1
--- PASS
=== RUN   TestOfferDisabledServesTrash   → 200, без Location
--- PASS
=== RUN   TestOfferUnknownServesTrash     → 200, без Location
--- PASS
=== RUN   TestOfferAsGroupDefault         → 302 на URL оффера
--- PASS
```

`TestEventInsertShape` сверяет число колонок и значений для всех комбинаций
флагов схемы (offer / network / cost) — страховка от сдвига INSERT.

## Гейты

- `go build ./...` — OK
- `go vet ./...` — OK
- `gofmt -l .` — пусто
- `go test ./... -count=1` — все пакеты `ok`
- `go test -tags=uitest ./internal/admin/ -run Logic -count=1` — OK (node 24)
- golangci-lint в окружении не установлен — гэп зафиксирован.

## Поведение

- Ссылка оффера проходит обычный путь макросов и распределения вариантов
  (после подстановки `redirect` становится `http_redirect`).
- Неизвестный/выключенный оффер → ответ trash по настройке `KUZTDS_TRASH_MODE`
  (как неизвестная группа), с warn-строкой в логе движка.
- Цена клика/конверсии не меняется: оффер — только маршрутизация.
