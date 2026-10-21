# G-066 — journal of rejected incoming postbacks

## RED
- `cmd/engine/pberrors_test.go`: no journal queue, no reason classifier and no
  redacted query; a handler with an invalid status and a recorder attached
  stores nothing.
- `internal/admin/handlers_more_test.go`: `/api/postback-errors` and its CSV
  export do not exist.
- `internal/store/clickhouse_integration_test.go` no longer compiled after the
  offer argument was added to `RecordPostback`; the stale calls are fixed as
  part of this slice.

## GREEN
- **Store.** `store.PostbackError` + `InsertPostbackErrors` (batched) and
  `PostbackErrors(from, to, limit)` over a new `postback_errors` table
  (`MergeTree`, 90-day TTL). The table is created by migration
  `009_postback_errors.sql` and idempotently by `ensureSchema`, so an existing
  database upgrades without manual SQL.
- **Engine.** Rejected postbacks are classified as `cid_missing`,
  `status_missing`, `status_invalid` or `not_recorded` (unknown click id /
  storage error, with the error clipped to 512 bytes). A bounded queue
  (`pbErrorLog`, 512 entries, batch 50, 2 s tick) drains to ClickHouse;
  `Push` never blocks the visitor, a full queue is counted and exposed as
  `X-Postback-Errors-Dropped` on `/healthz`, and shutdown drains it before the
  event buffer. The `pb` key is removed from the stored query; cid/status/tid
  are clipped and the query is capped at 2 KiB.
- **Admin.** `GET /api/postback-errors` (limit 1…500, default 100) and
  `GET /api/postback-errors/export` (CSV, 5 000 rows, formula-safe).
- **UI.** Conversions → **Rejected postbacks**: time, human reason, raw status,
  click id, txn, IP, redacted query, detail; CSV export of the same period.
- USAGE EN/RU document the reasons and the 90-day retention.

## Scope note
The journal is diagnostic and best-effort: a ClickHouse outage can also lose
the journal itself. Successful conversions are untouched; invalid postbacks
still answer HTTP 200, as before. There is no config field to add and therefore
no `configs/groups.example.json` change: the journal is always on when
ClickHouse is connected.

## Checks
- Engine queue/classification/redaction and handler journaling tests green;
  admin endpoint + CSV test green; store integration test added behind the
  `integration` tag; node helper checks green; full gate 18 packages ok.
