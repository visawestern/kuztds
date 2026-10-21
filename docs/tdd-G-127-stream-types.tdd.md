# TDD evidence — G-127 «Типы потоков: forced / regular / default»

## RED

```
internal/config/stream_types_test.go: unknown field Type in Stream
internal/config/stream_types_test.go: g.ValidateStreams undefined
internal/router/router_test.go: unknown field Type
internal/admin/tools_test.go: unknown field Type
```

После появления поля, но до роутера: `TestSelect_ForcedBeatsEarlierRegular`
падал на выборе regular, `TestSelect_DefaultCatchesUnmatched` — на отсутствии
потока; симулятор показывал `catch` вместо forced.

## GREEN

```
--- PASS: TestValidateStreams            (forced/regular/empty/default; unknown, Forced, 2 default, disabled default)
--- PASS: TestSelect_ForcedBeatsEarlierRegular
--- PASS: TestSelect_DefaultCatchesUnmatched
--- PASS: TestSelect_DisabledTypesIgnored
--- PASS: TestE2E_StreamTypes             (forced из любой позиции, default ловит остаток)
--- PASS: TestGroupsSaveRejectsBadStreams (3 × 400, валидный → 200)
--- PASS: TestSimulate                    (выбор и объяснение через router.Select)
```

## Поведение

- `Stream.Type`: `forced`, `regular`, `default`; пусто = regular — старые
  конфиги и `X-Kuztds-Stream` не меняются.
- Выбор: (1) включённые forced в порядке списка, (2) включённые regular в
  порядке списка, (3) включённый default без проверки его условий. Неизвестный
  тип трактуется как regular, конфиг, загруженный в обход админки, не теряет
  трафик.
- Валидация админки: неизвестная роль и второй default → 400; default
  считается даже выключенным, чтобы включение не создало второй.
- Симулятор: выбранный поток и `Out` берутся из `router.Select`, default
  помечается `why:"default"` и не показывает ложное «would match».
- Отчёты не меняются: имя потока уже пишется в events и видно в summary/
  performance/логах и CSV; новых событий роль не создаёт.
- UI: селект роли в дровере потока, бейдж «forced/default» у выбранного на
  канвасе; пример — mobile_ru_apple forced, default default.
- USAGE EN/RU, README; PARITY: G-127 DONE.

## Гейты

`gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -tags=uitest ./internal/admin/` — зелёные.
