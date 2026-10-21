# G-062 — conversion types: lead, sale, rejected, trash, custom

## RED
- `internal/admin/handlers_more_test.go`: `GET /api/postbacks/statuses` and
  `GET /api/postbacks/export` are missing (404), an invalid `status=` filter
  is accepted (200), and the export is not CSV.
- `internal/admin/web_logic_test.go` (node): there is no human label or
  semantic colour for a conversion status.

## GREEN
- Existing `conversion.ParseStatus` remains the single grammar: six system
  statuses plus `[a-z0-9_]{1,32}`, case/space normalised, empty or garbage
  rejected. The engine already records all of them; only `trash` moves money
  out of Revenue.
- `CH.Postbacks` takes a status filter (parameterised) and keeps totals in the
  same filter; `CH.PostbackStatuses` returns the exact set present in the
  period, with counts, including custom ones.
- Admin: `/api/postbacks/statuses`, `/api/postbacks/export` (CSV, formula-safe,
  up to 50 000 rows); an invalid status filter is a 400, not an empty report.
- Conversions page: flow + status selects (counts in the labels), status pills
  (`Lead`, `Sale`, … `Custom · vip`), export of the current filter.
- USAGE EN/RU list the statuses and the page behaviour.

## Scope note
Changing an existing sale's status by transaction id is a dedupe/update
problem (G-063: the postbacks table is append-only with a ClickHouse key
change required). G-062 covers the types themselves; G-063 stays open.

## Checks
- Handler/UI helper tests green, filter forwarding covered; full gate, vet and
  uitest green.
