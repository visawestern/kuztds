# TDD: цены кампании G-119…G-123 (домен + поля конфига)

## Источник
- `docs/TZ-parity.md` G-119…G-123; эталон Binom v2 (CPC/CPM/CPA/CPV + Auto,
  ECB-конвертация, потери %, postback %).

## Решение (адвокат дьявола)
- Домен `internal/cost` — чистые функции без IO: `ForClick` (CPC/CPV=фикс,
  CPM=value/1000, CPA/Revshare на клике=0), `ForConversion` (CPA=фикс,
  Revshare=payout×pct), `ApplyLoss`, детерминированное ядро `PassesPercent`
  (рандом снаружи — тестируемо), `NormalizeToUSD` (пустая валюта=USD,
  курсы `USD за единицу` как у ECB; синк курсов — отдельный cron-шаг).
- `config.CostModel` — JSON-поля группы (обратно совместимо: отсутствие = без цены)
  + `Validate()` (модель из списка, value≥0, валюта 3 буквы, проценты 0..100).

## RED → GREEN
- RED ×2: `undefined: ForClick…`, затем `undefined: CostModel` (build failed).
- GREEN: `internal/cost` 10/10 PASS, 100% statements; `internal/config`
  CostModel valid/invalid PASS. Соседи (`cmd/engine`, `store`) — ok.
- По ходу: тест поймал мою же ошибку формата (`Errorf` с лишним аргументом) —
  `go vet` до запуска, исправлено.
- `go build ./...`, `go vet`, `gofmt` — чисто.

## Что НЕ сделано (честно)
- ~~Проводка в движок~~ — сделано фазой 2 ниже (клик-модели CPC/CPM/CPV).
- Закрыто следующими фазами: стоимость CPA/Revshare в конверсии (фаза 3),
  редактор цены в UI (фаза 4), ECB-синк курсов (фаза 5).
- Остаток: применение LossPct/PostbackPct (G-122/123).

## Фаза 2 — проводка клик-стоимости (G-119…G-121)

### Решение (адвокат дьявола)
- Деньги принадлежат ВХОДНОЙ кампании: `entryCost` снимается до цепочки
  `group`-переходов (лог пишется на обслужившую группу, цена — с входной).
- Курсов пока нет: `ForClickUSD(m, nil)` — USD/пустая 1:1, чужая валюта даёт
  честный 0 + debug-лог, а не смешивание валют в агрегатах.
- `events.cost/cost_cur` (миграция 005) + проба в `ensureSchema`
  (`eventCostCols`, паттерн `netCols`): старые базы не падают.
- `Performance`: `Cost` суммой из events; `Profit = revenue − cost`
  (при нулевых ценах цифры бит-в-бит как раньше).
- UI perf-таблицы: колонка Cost, CSV с cost; агрегаты `zero()/add()` сложены.

### RED → GREEN
- RED: `undefined: ForClickUSD` (build failed); затем моя же ошибка в ожидании
  CPM-теста (0.0108 вместо верных 0.011664) — тест поймал, исправлено ожидание.
- GREEN: `internal/cost` 11/11; движок 4/4 (`cost_test.go`: цена с входной,
  0 без модели, 0 для EUR без курсов, цена через `group`-ссылку).
- Интеграционный: событие с Cost 0.5 → perf s1: cost 0.5, profit 2.0
  (исполнится в CI с инфрой).
- Верификация: build/vet/gofmt чисто; engine/store/logbuf/cost/config/admin
  (+uitest)/cron/router/render — ok.

## Фаза 3 — CPA/Revshare-стоимость в момент конверсии (G-120 до конца)

### Решение (адвокат дьявола)
- `cost.ForConversionStatusUSD` (покрыт тестами): trash → 0 всегда
  (зеркало RevenueByStatus — отменённая выплата не стоит ничего).
- Движок резолвит модель через `CH.EventGroupID(cid)` → `groups.Get` —
  best effort: любая ошибка даёт cost 0, конверсия пишется всегда.
  Ценообразование никогда не блокирует запись (зафиксировано комментарием).
- `postbacks.cost` (миграция 006) + проба в том же `postbackCols`-флаге
  (ALTER покрывает все три колонки сразу).
- `Performance`: `Cost` = клики + конверсии (ex-trash),
  `Profit = revenue − Cost`. UI perf-таблицы уже несёт Cost; conversions —
  колонка Cost.
- Сквозной engine→CH путь CPA без живой ClickHouse не тестируем
  (chConn — конкретный `*store.CH`, замокан не будет): покрыто unit
  (домен), интеграционным (store) и nil-CH тестом (200 без паники).
  Честный гэп, зафиксирован.

### RED → GREEN
- RED: `undefined: ForConversionStatus` (build failed).
- GREEN: cost 13/13 (incl. USD-вариант + trash-зеркало); engine
  `TestPostbackOnCPAGroupNoCHIs200` PASS; интеграционный расширен
  (convCost 3.0 → s1: cost 3.5, profit −1.0 — CPA дороже выплаты, так и должно быть).
- Верификация: build/vet/gofmt чисто; engine/cost/store/admin(+uitest)/
  cron/config/logbuf — ok с `-count=1`.

## Фаза 4 — редактор цены в UI + валидация на сохранении (G-119)

### Решение (адвокат дьявола)
- Секция Price в настройках потока: модель/значение/валюта/revshare%/loss%/postback%.
  Сбор через чистый `normCost` (pure-блок): краевой случай «очищенный % постбеков»
  даёт 100 (все), а сохранённый 0 остаётся 0 — закреплено node-тестами.
- Серверная валидация `CostModel.Validate()` в `handleGroupsSave` (400):
  неизвестная модель молча стоила бы 0 — теперь не сохраняется.
- `normCost` вынесен в pure именно чтобы edge-случаи жили в тестах,
  а не в голове.

### RED → GREEN
- RED: `TestGroupsSaveRejectsBadCost` — 200 вместо 400 (×2 кейса).
- GREEN: валидация добавлена; node-чеки `normCost` (3 кейса) PASS;
  admin + uitest полностью ok.
- Верификация: build/vet/gofmt чисто.

## Фаза 5 — ECB-синк курсов (G-121)

### Решение (адвокат дьявола)
- Feed ЕЦБ котирует всё против EUR, поэтому база пересчёта — USD:
  `1 unit = USD_rate / unit_rate`, а EUR — не строка фида, а сам базис
  (`1 EUR = USD_rate`). USD жёстко = 1; фид без USD отвергается
  (угадывать базу = неправильно ценить каждый клик).
- Курсы живут в `<DataDir>/rates.json` (пишет cron, читает движок) —
  связь через файл, как у групп и .dat: движку не нужен cron-сокет.
- Запись только ПОСЛЕ успешного парсинга: 200 с maintenance-страницей
  не должен обнулить вчерашние курсы (закреплено тестом).
- Снимок курсов — `atomic.Pointer`: горячий путь читает неизменяемую
  мапу, перечитывание по mtime+size на тике reload групп.
- Битый/недочитанный файл не сдвигает stamp → повтор на следующем тике,
  старый снимок остаётся.
- `cost_cur` теперь честно `USD`: значение нормализовано, а не «EUR по
  курсу». Иначе агрегаты sum(cost) смешивали бы подписи валют.
- Ошибка скачивания/парсинга — ошибка джобы (красный статус в Automation),
  а не тихий no-op.

### RED → GREEN
- RED: `undefined: LoadRates` (build failed); затем `undefined: Rates/JobRates`.
- GREEN: `ParseECB` (включая 7 видов битых фидов), `SaveRates/LoadRates`
  (round-trip, отсутствие файла, битый JSON, USD≠1), cron-джоба (запись,
  сохранение старого файла при сбое, пустой URL, валидация URL,
  расписание/минимум интервала, дефолтный URL), engine-ratesStore
  (load/reload по mtime, битый файл), цена `0.05 EUR × 1.08 = 0.054 USD`
  в событии.
- Верификация: build/vet/gofmt чисто; engine/cost/cron/admin(+uitest) — ok
  с `-count=1`.

## Фаза 6 — потери трафика G-122 и колонка Trash G-302

### Решение (адвокат дьявола)
- Найдено расхождение спецификации: ранний `ApplyLoss` уменьшал оплачиваемые
  клики (raw×(100−loss)/100). Binom v2 документирует обратное:
  `new CPC = CPC / (1 − loss/100)` — источник берёт деньги и за недошедший
  трафик, поэтому дошедший клик дороже. Функция заменена на `AdjustForLoss`
  (единственный потребитель был тест — тихой совместимости не потребовалось).
- Loss применяется только к клик-моделям (CPC/CPM/CPV): CPA/Revshare стоят 0
  на клике, их цена назначается конверсией.
- `LossPct = 100` запрещён валидацией (деление на ноль); сама функция при
  невалидном проценте цену не меняет — Inf не должен попасть в суммы.
- G-123 (PostbackPct) честно остаётся PART: в kuzTDS нет исходящих S2S
  постбеков (это G-297), поэтому «применять процент» пока не к чему.
  Доменное ядро `PassesPercent` готово и покрыто.
- G-302 закрыт колонкой Trash в perf-таблице и CSV (данные PerfRow уже были).

### RED → GREEN
- RED: `undefined: ForClickPaidUSD`; затем `ApplyLoss(1000,10)`-тест заменён
  на формулу Binom; `LossPct:100` в конфиге проходил валидацию.
- GREEN: `AdjustForLoss`/`ForClickPaidUSD` + тесты (10%/50%/невалидные,
  EUR-курс, CPA=0), engine-тест `0.05 → 0.0556`, конфиг отклоняет 100%,
  perf/CSV с Trash.
- Верификация: build/vet/gofmt чисто; admin(+uitest)/engine/cost/config/cron —
  ok с `-count=1`.
