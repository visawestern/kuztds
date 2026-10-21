# TDD: статусы конверсий G-301/302/303 + ключ дедупа G-304 (домен)

## Источник
- `docs/TZ-parity.md` G-301…G-304; эталон Keitaro 11 (постбек только со `status=`, trash обнуляет Revenue).
- Рантайм: `go test` (Go, не npm — Step 0 скилла: детект по `go.mod` + `*_test.go`).

## User journeys
- Как арбитражник, хочу слать `?pb=KEY&cid=&profit=&status=trash`, чтобы мусор не портил Revenue, но сумма сохранялась в Revenue (trash).
- Как арбитражник, хочу свои статусы (`vip_level2`), чтобы резать отчёты под воронку.
- Как движок, хочу игнорить постбеки без/мусорным статусом (Keitaro 11 breaking change).

## RED (доказательство)
- `go test ./internal/conversion/` до реализации: `FAIL [build failed]` — `undefined: ParseStatus` (compile-time RED: тест ссылается на несуществующий путь, шаг 3 скилла).
- Плюс runtime-RED в процессе: `TestDedupKey` поймал коллизию `DedupKey("a\x00b","c") == DedupKey("a","b\x00c")` на наивном NUL-разделителе.

## GREEN
- `go test ./internal/conversion/ -v`: 7/7 PASS; покрытие 100.0% statements (`ParseStatus`, `RevenueByStatus`, `TrashRevenueByStatus`, `DedupKey`).
- Регресс соседей: `router`, `render`, `config`, `cmd/engine` — ok.
- `go build ./...`, `go vet`, `gofmt` — чисто. Полный `go test ./...` не прогнан (тяжёлый/интеграционный, требует инфру) —■ известный гэп.

## Гарантии (таблица)
| # | Гарантия | Тест | Тип | Итог |
|---|----------|------|-----|------|
| 1 | 6 системных статусов парсятся | `status_test.go:TestParseStatusKnown` | unit | PASS |
| 2 | Регистр/пробелы нормализуются | `TestParseStatusCaseInsensitiveAndTrimmed` | unit | PASS |
| 3 | Пустой статус → игнор | `TestParseStatusEmptyIgnored` | unit/error | PASS |
| 4 | Кастом `[a-z0-9_]{1,32}` принимается | `TestParseStatusCustom` | unit | PASS |
| 5 | Инъекции/кириллица/длинные → отказ | `TestParseStatusRejectsGarbage` | unit/error | PASS |
| 6 | trash: Revenue=0, Revenue(trash)=payout | `TestRevenueByStatus` | unit | PASS |
| 7 | Ключ дедупа без коллизий, пустой tid → cid | `TestDedupKey` | unit/edge | PASS |

## Что НЕ сделано (честно)
- ~~Проводка в движок~~ — сделано (фаза 2 ниже). Остаток: полный дедуп по паре
  (cid,tid) с хранением обоих строк требует пересоздания таблицы postbacks
  (смена ORDER BY) — отдельный шаг с пересозданием; сплит Revenue/Revenue(trash)
  в агрегатах отчётов — следующий цикл.
- Коммиты-чекпоинты не создавались (нет явного запроса на коммит).

## Фаза 2 — проводка status/tid (G-301…G-304)

### RED
- `go test ./cmd/engine/ -run TestParsePostback`: `FAIL [build failed]` —
  `undefined: parsePostback` (5 мест). Compile-time RED.

### GREEN (что изменено)
- `cmd/engine/postback.go` (new): `parsePostback(q)` — cid обязателен,
  `status` через `conversion.ParseStatus`, profit не парсится → 0 (совместимость),
  `tid` опционален.
- `cmd/engine/handler.go`: ветка постбека идёт через `parsePostback`; без
  валидного статуса — игнор + warn-лог, ответ всё равно 200.
- `internal/store/clickhouse.go`: `RecordPostback(ctx,cid,profit,status,tid)`,
  INSERT с `status,tid`; `PostbackRow` + `Postbacks` отдают `status/tid`.
- `migrations/clickhouse/004_postback_status.sql` (new, аддитивная):
  `ADD COLUMN IF NOT EXISTS status/tid` (миграции `001–003` не тронуты).
- `internal/admin/web/index.html`: колонки Status/Txn в Conversions, сниппет
  `?pb=` с `&status=sale`.
- `README.md/.ru.md`, `docs/USAGE.md/.ru.md`: пиксель с `&status=` + описание.
- Интеграционный тест обновлён под новую сигнатуру + ассёрт `status=sale`
  (исполнится только с инфрой; без ClickHouse — skip, как раньше).

### Верификация
- `go build ./...`, `go vet` (4 пакета), `gofmt` — чисто.
- `go test ./cmd/engine/ ./internal/conversion/` — ok (5 новых тестов парсинга PASS).
- `go test ./internal/store/ ./internal/admin/` — ok.
- `go test -tags=uitest ./internal/admin/` — ok (JS панели парсится).
- Полный `go test ./...` не прогнан (тяжёлый) — гэп фиксирую.

### Гарантии фазы 2
| # | Гарантия | Тест | Итог |
|---|----------|------|------|
| 8 | Валидный постбек разбирается полностью | `postback_test.go:TestParsePostbackValid` | PASS |
| 9 | `TRASH` с пробелами → `trash` | `TestParsePostbackTrashNormalized` | PASS |
| 10 | Без статуса / мусор / пустой cid → игнор | `TestParsePostbackNoStatusIgnored` | PASS |
| 11 | Битый profit → 0, но запись идёт | `TestParsePostbackBadProfitIsZeroButOk` | PASS |
| 12 | Кастомный статус проходит | `TestParsePostbackCustomStatus` | PASS |

## Фаза 3 — revenue-сплит trash в агрегатах (G-302 до конца)

### Решение (адвокат дьявола)
- Revenue исключает `status='trash'`; строки до миграции 004 (`status=''`)
  считаются обычной выручкой — иначе старые деньги исчезнут из отчётов.
- Миграции применяются только при первом старте контейнера
  (`/docker-entrypoint-initdb.d`), поэтому `ensureSchema` сам делает
  `ALTER TABLE postbacks` + пробу колонок (тот же паттерн, что `netCols`
  для events): старый сервер с новой базой и наоборот не падают —
  запросы ветвятся по флагу `postbackCols`.
- Сигнатура `Postbacks` расширена до `(rows, revenue, trash, err)`:
  задело 7 мест (2 интерфейса, 2 фейка, admin, cron, интеграционный тест) —
  принято вместо отдельного метода ради одного запроса вместо двух.
- `Conv` считает все конверсии включая trash (как статусы Keitaro),
  `Profit` — без trash, новое поле `PerfRow.Trash/TrashProfit`.

### RED → GREEN
- RED: `undefined: profitSums/convCounts/postbackRowCols` (build failed).
- GREEN: 4/4 PASS (`internal/store/postbacks_test.go`); интеграционный тест
  расширен (sale 2.5 + trash 10.0 → revenue 2.5, trash 10.0; perf s1:
  conv=2, profit=2.5, trash=1/10.0) — исполнится в CI с инфрой.
- Верификация: build/vet/gofmt чисто; `store/cron/engine/conversion/admin`
  + `uitest` (JS панели с новыми колонками Status/Txn и `r.trash`) — ok.
- UI Conversions: итог `profit X · trash Y` (скрыто пока trash=0).

### Остаток среза статусов (честно)
- Полный дедуп по паре (cid,tid) с хранением обоих строк — требует
  пересоздания таблицы (смена ORDER BY). Отложено сознательно, зафиксировано здесь.
