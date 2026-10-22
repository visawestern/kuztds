# TDD: G-310 — ручное добавление конверсии

## Источник
- `docs/TZ-parity.md` G-310: «Вбить покупку руками из панели».
- Рантайм: Go admin (`internal/admin`), ClickHouse `postbacks`, Redis caps,
  существующие курсы ЕЦБ из `data/rates.json`.

## RED (по построению)
- В admin не было ни `PostbackRecorder`, ни маршрута `POST /api/conversions`:
  запрос возвращал 404, а форма в Conversions отсутствовала.
- Новые тесты (`TestManualConversion*`) на старом коде не проходили: не было
  ни записи, ни конвертации валюты, ни проверки клика/статуса.
- После правки все проверки GREEN.

## GREEN
- `admin.Config` получил optional-интерфейсы:
  - `PostbackRecorder` — `EventGroupID`, `EventOfferID`, `RecordPostback`
    (реализация `store.CH`, та же, что у движка);
  - `ConversionCaps` — `CountConversion` / `CountConversionOnce`
    (реализация `store.Counters` на Redis).
  Без ClickHouse ручка отвечает 503, без Redis конверсия пишется без капа.
- `POST /api/conversions` (admin auth + CSRF):
  - `cid` обязателен и должен существовать в `events` (иначе 404);
  - `status` — системный или кастомный `conversion.ParseStatus` (иначе 400);
  - `profit` (или alias `payout`) обязателен, явный `0` допустим;
    `NaN`/`Inf` отклоняются;
  - `currency` — 3 латинские буквы, пусто = USD; не-USD сумма переводится
    в USD по `rates.json`, без курса — 400 (оператор видит причину; в отличие
    от входящего постбека, где продажа сохраняется с revenue 0);
  - `tid` обрезается, лимит 256 символов;
  - режим дублей берётся из группы клика (`update` по умолчанию), стоимость
    конверсии — из модели группы (CPA/Revshare), как в движке;
  - после успешной записи кап тратится: `new` — всегда, `update`/`ignore` —
    `CountConversionOnce(DedupKey(cid,tid))`; `trash` и клик без оффера кап
    не тратят; ответ `recorded` / `ignored`.
- UI Conversions: кнопка **Add conversion** раскрывает форму (click id,
  status, transaction id, profit, currency), показывает причину ошибки,
  после успеха обновляет таблицу. Черновик не сохраняется — это разовая
  операция, а не настройка.
- Миграций и новых полей конфига не потребовалось: ручная конверсия
  использует существующие `postbacks`, режимы дублей и `rates.json`.

## Гарантии
| # | Гарантия | Тест | Итог |
|---|----------|------|------|
| 1 | Сумма в валюте переводится в USD, стоимость CPA считается в USD | `TestManualConversionConvertsCurrencyAndSpendsCap` | PASS |
| 2 | Пустой cid / плохой статус / нет суммы / плохая валюта / нет курса → 400 | `TestManualConversionValidation` | PASS |
| 3 | Несуществующий клик → 404 | `TestManualConversionValidation` | PASS |
| 4 | `new` тратит слот на каждую ручную запись | `TestManualConversionNewModeSpendsEverySlot` | PASS |
| 5 | `ignore`-дубль не тратит слот | `TestManualConversionIgnoreModeDoesNotSpendCap` | PASS |
| 6 | Ручная запись идёт через `RecordPostback` с режимом группы | `TestManualConversion*` | PASS |

## Верификация
- `gofmt -l .` — пусто.
- `go build ./...`, `go vet ./...` — чисто.
- `go test ./internal/admin/ -run TestManualConversion -count=1` — PASS.
- `go test -tags=uitest ./internal/admin/ -count=1` — PASS.
- Полные гейты и integration `./internal/store/` прогоняются перед коммитом.

## Ограничения
- Ручная конверсия не создаёт клик: `cid` должен уже быть в `events`.
- Не-USD сумма без курса отклоняется 400, а не пишется нулём: оператору нужен
  явный результат, чтобы не завести покупку с неверной выручкой.
- `ignore` остаётся pre-check (как и у входящего постбэка): одновременные
  дубли могут пройти проверку, итог страхуют `FINAL` и `CountConversionOnce`.
