# TDD evidence — G-055 «Шаблоны источников с метками»

Пункт книги: «Готовые метки под каждую рекламную сеть». Срез: шаблон источника
сопоставляет параметры запроса коротким макросам, чтобы ссылки не зависели от
порядка `[PAR-n]`.

## Поведение

- `Group.Sources []TrafficSource` + `Group.Source` — активный шаблон.
- `TrafficSource{ID,Name,Labels[{Param,Label}]}`; метка — `^[A-Za-z][A-Za-z0-9_]{0,31}$`,
  регистр не важен (движок и UI приводят к верхнему).
- Валидация: id как у офферов; параметр — непустой ключ запроса (≤64, без
  `& = # ?` и пробелов); дубли id/меток — ошибка; метки не могут затенять
  встроенные макросы (`CID`, `COUNTRY`, `KEY`, `REMOTE` и т.д.); активный id
  обязан существовать.
- Движок: читает только активный шаблон и только если у него есть метки; значение
  берётся из query (`?gclid=abc`), отсутствующее — пустая строка. Значения не
  пересканируются на макросы (один проход Expand).
- Хот-путь: `r.URL.Query()` один раз на визит при активном шаблоне; без шаблона
  расходов нет.

## RED

```
internal/render/extra_test.go: unknown field Extra in struct literal
cmd/engine/source_test.go:15: unknown field Sources in struct literal of type config.Group
cmd/engine/source_test.go:15:21: undefined: config.TrafficSource
FAIL github.com/egerkuzma/kuztds/cmd/engine [build failed]
```

Админ до правки: `TestGroupsSaveRejectsBadSource` — все битые payload'ы
сохранялись с 200.

## GREEN

```
--- PASS: TestExpandExtraMacros            (заполнение, пустая метка, [NOPE] остаётся, значение не пересканируется)
--- PASS: TestSourceValidate               (9 отказов + валидный шаблон)
--- PASS: TestSourceTemplateLookup
--- PASS: TestSourceLabelsExpand           ([GCLID]/[SRC]/[KEY] из query; без параметров — пусто)
--- PASS: TestGroupsSaveRejectsBadSource   (400 на unknown/reserved/param/label/duplicate, 200 на валидный)
```

## UI / доки

- Flow settings → Traffic source: активный шаблон, карточки с id/name, строки
  «параметр → метка», добавление/удаление; `normSources` (trim, upper, пустые
  строки отбрасываются) покрыт node-чеком.
- `configs/groups.example.json`: шаблон `google` (`gclid`, `utm_source`), активен;
  мобильный поток использует `[GCLID]` и `[SRC]`.
- USAGE EN/RU, README; PARITY: G-055 DONE.

## Гейты

`go build ./...`, `go vet ./...`, `gofmt -l .` — чисто; полный `go test ./...` и
`-tags=uitest ./internal/admin/` — зелёные.
