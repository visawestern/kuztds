# G-132 — custom 404 for the tracker

## RED
- `cmd/engine/notfound_test.go`: a missing page is not published, a blank or
  oversized page is refused, a working page survives a broken tick, the
  watcher stops with its context.
- `cmd/engine/e2e_test.go`: trash mode 3 serves the operator's HTML with a 404
  status, a known flow is untouched, and without a page the built-in 404 stays.
- `internal/admin/notfound_test.go`, `handlers_more_test.go`: the file store
  treats a missing file as empty, refuses oversized pages, and the API round
  trips HTML, rejects 64 KiB+, and clears it.

## GREEN
- `config.MaxNotFoundBytes` (64 KiB) is shared by the engine, the admin store
  and the API, so a hand-edited file cannot bypass the editor's limit.
- Engine `notFoundStore` keeps an immutable snapshot, hot-reloads on the groups
  tick, and returns to the built-in 404 when the file is cleared. `d.trash()`
  substitutes the page only when the trash answer is a 404; redirect, 403 and
  empty-200 modes keep their configured behaviour.
- Admin `FileNotFound` + `GET/PUT /api/notfound`, edited on Tools. The engine
  and the admin read the same `KUZTDS_404_FILE` (default `<data dir>/404.html`).
- USAGE EN/RU document the variable and the scope; `configs/404.html` is a
  starting page.

## Why there is no ClickHouse event
A 404 is not a visit that reached a flow: no stream, offer or cost exists for
it. Logging it as a click would invent traffic and distort CR. The existing
warnings for broken group links and failed proxy fetches still name the flow
and stream in the engine log.

## Checks
- Store/API coverage 100%; full gate, vet and uitest green.
