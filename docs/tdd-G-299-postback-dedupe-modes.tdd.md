# TDD: G-299 — режимы дублей входящих постбеков

## Источник
- `docs/TZ-parity.md` G-299: дедуп входящих постбеков, режимы новый / игнор / править.
- Рантайм: Go (`go.mod`, `*_test.go`), ClickHouse 24.8, Redis/miniredis.

## RED
- Первый прогон после добавления тестов и смены интерфейса:
  `go test ./internal/store/` → `not enough arguments in call to postbackExtraInsert`
  (compile-time RED: тесты уже требовали `dedupe_key`, реализации ещё не было).
- Поведенческий RED до фикса: `TestCHPostbacksMigrateDedupe` падал на catch-up copy
  `Number of columns doesn't match (source: 16 and result: 17)` — новая колонка
  не копировалась из старой таблицы после `EXCHANGE TABLES`.

## GREEN
- Режимы в `config.Group.PostbackDedupe` + `PostbackDedupeMode/ValidatePostbackDedupe`,
  выбор в Flow settings и валидация в `PUT /api/groups`.
- `RecordPostback(..., mode) (recorded bool, err)`:
  - `update` — текущее поведение, побеждает больший `version`;
  - `ignore` — `SELECT count() ... FINAL WHERE cid/tid`; дубль не пишется, `false`;
  - `new` — уникальный `dedupe_key`, тот же tid остаётся отдельной конверсией.
- Схема: `dedupe_key String`, `ORDER BY (cid, tid, dedupe_key)`;
  существующая `(cid,tid)`-таблица апгрейдится метаданным `ALTER` при старте,
  старейшая `(cid)` — через `cmd/chmigrate` (rebuild с catch-up).
- Кап: `update`/`ignore` — `CountConversionOnce`; `new` — `CountConversion`.

## Гарантии
| # | Гарантия | Тест | Тип | Итог |
|---|----------|------|-----|------|
| 1 | Режим по умолчанию — update, неизвестный отклоняется админкой | `config/postback_dedupe_test.go` | unit | PASS |
| 2 | update заменяет сумму, оставляет одну строку | `store/postbacks_integration_test.go:TestCHPostbackDedupeModes` | integration | PASS |
| 3 | ignore не пишет дубль и не тратит слот | `cmd/engine/postback_dedupe_test.go:TestPostbackIgnoreModeKeepsFirstAndSkipsCap` | unit | PASS |
| 4 | new пишет два одинаковых tid и тратит два слота | `TestPostbackNewModeSpendsEverySale` + `TestCHPostbackDedupeModes` | unit/integration | PASS |
| 5 | Миграция старой `(cid)`-таблицы даёт 3-ключ и не теряет строки | `TestCHPostbacksMigrateDedupe` | integration | PASS |
| 6 | `(cid,tid)`-таблица апгрейдится на месте без rebuild | `TestCHPostbacksUpgradeDedupeKey` | integration | PASS |
| 7 | UI-селектор и JS панели не ломаются | `go test -tags=uitest ./internal/admin/` | UI | PASS |

## Верификация
- `gofmt -l .` — пусто.
- `go build ./...`, `go vet ./...` — чисто.
- `go test ./... -count=1` — 18 пакетов, PASS.
- `go test -race ./... -count=1` — PASS.
- `go test -tags=uitest ./internal/admin/ -count=1` — PASS.
- `go test -tags=integration ./internal/store/ -count=1` — PASS (живые ClickHouse/Redis).

## Что осталось / ограничения
- `ignore` — pre-check, не жёсткая уникальность: два одновременных дубля
  могут оба пройти проверку, но `FINAL` всё равно оставит одну строку, а
  `CountConversionOnce` — один слот.
- На legacy-таблице без `dedupe_key` режимы `ignore`/`new` деградируют в
  `update`; пересборка — `cmd/chmigrate -apply`.
- Исходящие постбеки (G-297/G-298) по-прежнему не реализованы.
