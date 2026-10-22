# TDD: G-068 — пиксели-картинки на странице спасибо

## Источник
- `docs/TZ-parity.md` G-068: «Картинки событий для кабинетов».
- Рантайм: `cmd/engine` (`root`, `parsePostback`, общий `recordConversion`),
  `internal/config` (резерв путей), admin Flow settings → Offers.

## RED (по построению)
- В движке не было `/_px.gif`: запрос уходил в обычный роутинг групп, а
  картинку для страницы спасибо отдать было нечем.
- Новые тесты (`TestConversionImagePixel*`) на старом коде не проходили:
  не было ни GIF-ответа, ни записи конверсии.
- После правки все проверки GREEN.

## GREEN
- `GET /_px.gif?pb=KEY&cid=...&status=...&profit=...`:
  - те же параметры и статусы, что у `?pb=` и JS-пикселя G-309;
  - валидные ключ/параметры → конверсия пишется через
    `recordConversion` (цена G-305, оффер, режим дублей G-299, кап
    G-050/G-304);
  - ответ — прозрачный GIF 1×1, `Content-Type: image/gif`,
    `Cache-Control: no-store`; браузер не кэширует запрос;
  - нет/неверный ключ, нет `cid`, невалидный статус или ошибка записи → GIF
    всё равно отдаётся с HTTP 200; отказ journal-ится (кроме неверного ключа);
  - `HEAD` не пишет конверсию.
- `_px.gif` добавлен в резерв движка рядом с `_px.js`; `Group.ValidateRoutes`
  не даёт занять оба пути id/алиасом/share-slug.
- Admin: в карточке оффера рядом с JS-пикселем появился готовый `<img>` из
  токенов выбранной сети; в Links потока — шаблон `/_px.gif?...`. Обе строки
  копируются существующей кнопкой copy.
- Доки USAGE EN/RU описывают image-пиксель и его отличие от JS-варианта.

## Гарантии
| # | Гарантия | Тест | Итог |
|---|----------|------|------|
| 1 | GIF-пиксель пишет конверсию, отдаёт `image/gif` и `no-store` | `TestConversionImagePixelRecordsConversion` | PASS |
| 2 | Валидная запись тратит слот капа оффера | `TestConversionImagePixelRecordsConversion` | PASS |
| 3 | Дедуп с S2S работает через общий pipeline | `TestConversionPixelSharesDedupeWithPostback` | PASS |
| 4 | Группа/алиас/slug не могут занять `/_px.gif` | `TestValidateRoutesRejectsReservedPixelPath` | PASS |
| 5 | UI строит `<img>`-пиксель из токенов сети | `TestLogicHelpers` (`imagePixelMemo`) | PASS |

## Верификация
- `gofmt -l .` — пусто.
- `go build ./...`, `go vet ./...` — чисто.
- `go test ./cmd/engine/ -run 'TestConversionImagePixel|TestConversionPixel' -count=1` — PASS.
- `go test ./internal/config/ -run TestValidateRoutes -count=1` — PASS.
- `go test -tags=uitest ./internal/admin/ -run TestLogicHelpers -count=1` — PASS.
- Полные гейты и integration `./internal/store/` прогоняются перед коммитом.

## Ограничения
- Это входящее событие на странице спасибо (трекер получает `cid` и пишет
  конверсию). Исходящая передача покупки в рекламный кабинет сервером —
  отдельный пункт G-073.
- Пиксель не создаёт клик: `cid` должен уже существовать в `events`.
- Как и у JS-варианта, точный дедуп с S2S требует одинакового `tid`; без него
  ключом остаётся `cid`.
