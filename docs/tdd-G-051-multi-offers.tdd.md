# TDD evidence — G-051 «Много предложений до и после клика» (PART)

Срез: список офферов в выводе потока с распределением и отказоустойчивостью к
битым id. Часть «после клика» закрывается только цепочкой потоков: лендингов в
kuzTDS нет, а без них «после клика» — это переход в другой поток, который
отдаёт оффер. Веса вариантов (Keitaro) пока не поддержаны — остаток зафиксирован
в PARITY.

## Поведение

- `"redirect": "offer", "out": "o1|||backup"` — список id через `|||`.
- Выбор — распределение потока: `rotator` (cookie), `evenly` (Redis), `random`
  (по умолчанию). Ключ ротации офферов отдельный (`<поток>__o`), чтобы состояние
  не смешивалось с распределением URL-вариантов самого оффера.
- Неизвестные и выключенные id отбрасываются до выбора: одна устаревшая запись
  не роняет поток. Если не осталось ни одной — trash + warn.
- Выбранный оффер дальше идёт по обычным правилам: страны (G-054) → fallback,
  дневной кап (G-050) → fallback, иначе trash.
- В лог пишется фактически отданный оффер (`events.offer`).

## RED

```
--- FAIL: TestOfferListSkipsBroken
    offer_test.go:208: visit 0 → "", want the only servable offer
```

(До правки `outRaw` целиком искался как один id: `"missing|||draft|||o2"` не
находился, визит получал trash.)

## GREEN

```
--- PASS: TestOfferListRotation                 (rotator: o1 → o2, лог совпадает)
--- PASS: TestOfferListSkipsBroken              (missing/draft пропущены; без живых → trash)
--- PASS: TestOfferListRotationKeepsCountryRules (US-визит на DE-оффер → country fallback)
ok  github.com/egerkuzma/kuztds/cmd/engine
```

## UI / доки / пример

- UI не менялся: вывод потока — обычное текстовое поле, синтаксис списка
  описан в USAGE.
- `configs/groups.example.json`: поток `default` теперь
  `"offer"` + `"o1|||backup"` + `rotator` — список виден в живом примере.
- USAGE EN/RU + README; PARITY: G-051 → PART с явным остатком.

## Гейты

`go build ./...`, `go vet ./...`, `gofmt -l .` — чисто; полный `go test ./...`
и `-tags=uitest ./internal/admin/` — зелёные.
