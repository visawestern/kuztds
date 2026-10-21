# TDD evidence — G-130 «Замена куска в URL потока»

## RED

```
internal/config/url_replace_test.go: unknown field URLReplace in Stream
cmd/engine/e2e_test.go: unknown field URLReplace; undefined config.URLEdit
cmd/engine/offer_test.go: URLReplace undefined
```

После появления поля: `TestE2E_URLReplacement` падал (`slot=OLD`, тело с OLD),
потому что замены ещё не применялись.

## GREEN

```
--- PASS: TestValidateStreamsURLReplace  (валидные пары; пустой/пробельный find → ошибка)
--- PASS: TestE2E_URLReplacement         (замена после макросов, [KEY] не переписывается; show_text не трогается)
--- PASS: TestOfferURLNotRewritten       (URL оффера остался KEEP)
--- PASS: TestGroupsSaveRejectsBadStreams (+ пустой find → 400)
--- PASS: TestLogicHelpers               (UI-парсер URL-замен)
```

## Поведение

- `Stream.URLReplace []URLEdit{find,repl}`; `find` обязателен, `repl` может быть
  пустым (удаление куска).
- Замена идёт по итоговому URL после `render.Expand` и подстановки `[REMOTE]`,
  без учёта регистра (тот же `replaceCI`, что у CURL-страниц), поэтому
  заменять можно уже развёрнутые значения; сами макросы до замены не
  дотягиваются.
- Только URL-выводы: `http_redirect`, `meta_refresh`, `js_redirect`,
  `iframe_redirect`, `js_selection`, `iframe_selection`, `curl`. `show_text`,
  `javascript`, `api`, `offer`, `group` — без замен.
- Решение фиксируется до блока офферов (`urlEditable`), иначе `offer`
  превращается в `http_redirect` и партнёрская ссылка была бы переписана.
- UI: строки find → replace в дровере потока (те же компоненты, что у CURL),
  коллект в `url_replace`; пример — desktop `/a` → `/a?utm_source=flow`.
- Доки EN/RU, README; PARITY: G-130 DONE.

## Гейты

`gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -tags=uitest ./internal/admin/` — зелёные.
