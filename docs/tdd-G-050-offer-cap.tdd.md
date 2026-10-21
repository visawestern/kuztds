# TDD evidence — G-050 «Лимит покупок в день и запасное»

| # | Критерий | Где |
|---|----------|-----|
| 1 | Конфиг + валидация | `Offer.CapDaily`, `Offer.Fallback`; `ValidateOffers`: cap ≥ 0, fallback ≠ self, fallback существует и включён (проверка после сбора всех id — ссылаться можно вперёд) |
| 2 | Движок, e2e | `handler.go`: проверка лимита в блоке `redirect == "offer"`, замена на fallback / trash; инкремент при принятом постбеке (кроме `trash`); `store.Counters.OfferCapReached/CountConversion` |
| 3 | Лог/отчёт | отданный оффер (в т.ч. запасной) — колонка `offer`; выручка конверсии по правилам G-049 |
| 4 | Админ | поля Daily purchase cap / Fallback в карточке оффера, `normOffers` (pure-блок), 400 на отрицательный cap, несуществующий и выключенный fallback |
| 5 | Доки + пример + регресс | USAGE EN/RU, README, `groups.example.json` (o1 → fallback backup), полный `go test ./...` |

## RED

```
cmd/engine/offercap_test.go:22:60: unknown field CapDaily in struct literal of type config.Offer
cmd/engine/offercap_test.go:54:14: c.CountConversion undefined (type *store.Counters has no field or method CountConversion)
FAIL github.com/egerkuzma/kuztds/cmd/engine [build failed]
```

```
FAIL offers are trimmed
  got  [{... "payout":1.5,"currency":"EUR"}]                    (без cap_daily/fallback)
  want [{... "payout":1.5,"currency":"EUR","cap_daily":3,"fallback":"o2"}]
FAIL negative payout and cap clamp to 0
```

Плюс `TestGroupsSaveRejectsBadOffer` с `cap_daily:-1`, неизвестным и
выключенным fallback — 400 только после серверной валидации.

## GREEN

```
=== RUN   TestOfferCapReached                     (store, miniredis)
--- PASS  — пустой день/0 = не достигнут; 2 покупки при cap 2 → достигнут;
            другой оффер и следующие сутки — с нуля
=== RUN   TestOfferCapIgnoresUnknownOffer          --- PASS
=== RUN   TestOfferCapServesFallback               → 302 https://backup.example
=== RUN   TestOfferCapNotReachedServesMain         → 302 https://main.example
=== RUN   TestOfferCapWithoutFallbackServesTrash   → 200 без Location
--- PASS
```

## Решения

- Счётчик: `offercap:<group>:<offer>:<YYYYMMDD>` (UTC), `INCR` + `EXPIREAT` на
  конец суток; хот-путь — один `GET`, только у офферов с cap > 0.
- Redis недоступен → **fail-open**: визит обслуживается, warn в логе. Трафик
  не блокируется инфраструктурой (требование p50/30k rps).
- Fallback отдаётся без проверки его собственного лимита — иначе два оффера
  могут ссылаться друг на друга и зациклиться.
- Покупка считается на постбеке при `status != trash`; неудачная запись в
  ClickHouse слот не возвращает (сеть уже продала) — счётчик best effort и
  логируется debug-строкой при ошибке.
- Ограничение: инкремент требует id оффера из `events` (ClickHouse) — при
  недоступном CH лимит не пополняется, клики продолжают обслуживаться.

## Гейты

- `go build ./...`, `go vet ./...`, `gofmt -l .` — чисто
- `go test ./... -count=1` — все пакеты `ok`
- `go test -tags=uitest ./internal/admin/ -count=1` — `ok`
- golangci-lint в окружении отсутствует — гэп зафиксирован.
