# TDD: G-064 — ввоз покупок списком и файлом

## Источник
- `docs/TZ-parity.md` G-064: «Загрузить старые покупки пачкой».
- Рантайм: Go admin + ClickHouse `postbacks` + существующие курсы ЕЦБ.

## RED (по построению)
- В admin не было маршрута импорта, CSV-парсера и формы: загрузка списка
  возвращала 404.
- `postbacks.ts` всегда заполнялся `now()`, поэтому старую покупку нельзя было
  положить на её реальную дату.
- Новые тесты (`TestConversionImport*`, `TestCHPostbackHistoricalTimestamp`)
  на старом коде не проходили. После правки — GREEN.

## GREEN
- `store.CH.RecordPostbackAt` — та же запись, что `RecordPostback`, но с явным
  `ts`; `RecordPostback` делегирует с `time.Now()`. Все прочие вызовы движка
  не изменились.
- `POST /api/conversions/import` (admin auth + CSRF), тело `text/csv`, лимит
  5 МБ и 1000 строк:
  - заголовок `ts,cid,status,tid,profit,currency`; алиасы `click_id`,
    `transaction_id`, `payout`, `date`/`time`; лишние колонки — ошибка;
  - без заголовка порядок тот же: `cid,status,tid,profit,currency,ts`;
  - обязательны `cid` и `profit`; статус по умолчанию `sale`, валюта USD,
    `ts` — текущее время;
  - `ts` принимает RFC3339, `YYYY-MM-DD HH:MM[:SS]`, `YYYY-MM-DD` (UTC);
  - плохая строка попадает в `errors[{row,error}]`, остальные продолжают
    импортироваться; строка не откатывает уже записанные;
  - импорт всегда использует `update`: повторная загрузка файла обновляет
    покупку по `(cid,tid)`, а не дублирует её;
  - импорт **не тратит** суточный лимит оффера: старые покупки не должны
    съедать слоты живого трафика;
  - стоимость CPA/Revshare считается по модели группы клика, как в движке.
- UI Conversions → **Import purchases**: вставка CSV или выбор `.csv`-файла,
  результат `Imported / Failed` и первые ошибки по номерам строк.
- USAGE EN/RU: формат, алиасы, даты, идемпотентность и отсутствие капа.

## Гарантии
| # | Гарантия | Тест | Итог |
|---|----------|------|------|
| 1 | Дата покупки пишется в `postbacks.ts` и попадает в исторический отчёт | `TestCHPostbackHistoricalTimestamp` | PASS |
| 2 | Импорт CSV с заголовком, `payout`, валютой и датой | `TestConversionImportCSVHistoricalAndNoCap` | PASS |
| 3 | Список без заголовка читается в объявленном порядке | `TestConversionImportHeaderlessUsesColumnOrder` | PASS |
| 4 | Плохие строки не прерывают валидные, ошибки с номерами | `TestConversionImportReportsBadRows` | PASS |
| 5 | Пустой файл и неизвестная колонка → 400 | `TestConversionImportRejectsBadFile` | PASS |
| 6 | Импорт всегда `update` и не тратит кап даже при `new` у группы | `TestConversionImportCSVHistoricalAndNoCap` | PASS |

## Верификация
- `gofmt -l .` — пусто.
- `go build ./...`, `go vet ./...` — чисто.
- `go test ./internal/admin/ -run TestConversionImport -count=1` — PASS.
- `go test -tags=integration ./internal/store/ -run TestCHPostbackHistoricalTimestamp -count=1` — PASS.
- `go test -tags=uitest ./internal/admin/ -count=1` — PASS.
- Полные гейты прогоняются перед коммитом.

## Ограничения
- Клик должен существовать в `events`: импорт переиспользует размерности и
  группу визита. Покупки старше retention событий импортировать нельзя.
- Импорт не создаёт клик и не прогоняет фильтры/лендинги — это исторические
  покупки, а не визиты.
- Частичный успех не откатывается: валидные строки уже записаны, повторная
  загрузка идемпотентна по `(cid,tid)`.
- Время — UTC; смещение в RFC3339 переводится в UTC. Дата без времени —
  полночь UTC.
- Валюта переводится по текущим курсам ЕЦБ, а не по курсу на дату покупки:
  исторических рядов курсов в проекте нет.
- Лимит 1000 строк на запрос; больший файл нужно разбить. Номера строк в
  ошибках — записи CSV; физические строки внутри quoted-полей могут отличаться.
