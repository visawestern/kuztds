# TDD: G-309 — пиксель конверсий на странице спасибо

## Источник
- `docs/TZ-parity.md` G-309: «JS-пиксель + гибрид с S2S и дедупом».
- Рантайм: `cmd/engine` (`root`, `parsePostback`, `RecordPostback`), общий
  pipeline конверсии, admin Flow settings → Offers.

## RED (по построению)
- В движке не было ни маршрута пикселя, ни способа отдать JS со страницы
  спасибо; запрос `/_px.js` уходил в обычный роутинг групп.
- Новые тесты (`TestConversionPixel*`) на старом коде не проходили: не было
  записи конверсии, `no-store` и проверки ключа.
- После правки все проверки GREEN.

## GREEN
- Зарезервирован путь `/_px.js` (`config.ConversionPixelPath`). Группа, алиас
  или share-slug с этим именем теперь отклоняются `Group.ValidateRoutes` при
  сохранении из админки — иначе маршрут был бы недостижим.
- `GET /_px.js?pb=KEY&cid=...&status=...&profit=...`:
  - те же параметры и статусы, что у `?pb=` (`profit`/`payout`, `currency`,
    `tid`);
  - валидный ключ и параметры → конверсия пишется сервером в момент загрузки
    скрипта; ответ — `application/javascript`, `Cache-Control: no-store`;
  - нет/неверный `pb`, нет `cid`, невалидный статус или ошибка записи → JS
    всё равно отдаётся с HTTP 200; отказ пишется в журнал отклонённых
    постбеков (кроме неверного ключа — он только debug-лог);
  - `HEAD` отдаёт заголовки и никогда не пишет конверсию.
- Общий pipeline вынесен в `engineDeps.recordConversion`: пиксель и S2S
  постбэк используют один и тот же расчёт суммы (G-305), выбор оффера,
  режим дублей (G-299), стоимость конверсии (G-120) и кап оффера
  (G-050/G-304). Поэтому `ignore`/`update`/`new` работают одинаково, а кап
  тратится только после успешной записи.
- Admin: в карточке оффера рядом со ссылкой постбэка появился готовый
  `<script src=".../_px.js?...">` из токенов выбранной сети; в блоке Links
  потока — шаблон пикселя. Кнопка copy использует существующий `data-memo`.

## Гарантии
| # | Гарантия | Тест | Итог |
|---|----------|------|------|
| 1 | Пиксель пишет конверсию, отдаёт JS и `no-store` | `TestConversionPixelRecordsConversion` | PASS |
| 2 | Пиксель и S2S дедупятся по `(cid, tid)` в режиме `ignore` | `TestConversionPixelSharesDedupeWithPostback` | PASS |
| 3 | Неверный ключ не пишет конверсию | `TestConversionPixelBadKeyDoesNotRecord` | PASS |
| 4 | Невалидный статус не пишет конверсию | `TestConversionPixelBadStatusDoesNotRecord` | PASS |
| 5 | `HEAD` не пишет конверсию | `TestConversionPixelHeadDoesNotRecord` | PASS |
| 6 | Полный путь через `newEng`/`root` доходит до store | `TestConversionPixelE2E` | PASS |
| 7 | Группа/алиас/slug не могут занять `/_px.js` | `TestValidateRoutesRejectsReservedPixelPath` | PASS |
| 8 | UI строит пиксель из токенов сети | `TestLogicHelpers` (`pixelMemo`) | PASS |

## Верификация
- `gofmt -l .` — пусто.
- `go build ./...`, `go vet ./...` — чисто.
- `go test ./cmd/engine/ -run TestConversionPixel -count=1` — PASS.
- `go test ./internal/config/ -run TestValidateRoutes -count=1` — PASS.
- `go test -tags=uitest ./internal/admin/ -run TestLogicHelpers -count=1` — PASS.
- Полные гейты и integration `./internal/store/` прогоняются перед коммитом.

## Ограничения
- Пиксель не создаёт клик: `cid` должен уже существовать в `events`.
- Без `tid` пиксель и S2S схлопнутся только по `cid`; для точного дедупа
  передавайте одинаковый `tid` в обоих сообщениях.
- Ключ `pb` находится в URL страницы спасибо; это тот же секрет, что и у
  постбэка, поэтому его нельзя публиковать.
- Визуальных событий в кабинетах рекламных сетей (G-068) этот срез не
  добавляет — он закрывает только входящий JS-пиксель/гибрид с S2S.
