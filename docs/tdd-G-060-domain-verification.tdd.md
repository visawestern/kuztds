# G-060 — domain verification files for ad cabinets

## RED
- `internal/config/verifications_test.go`: safe names, no `healthz`, no path
  traversal, no duplicates, no empty/oversized content, no collision with flow
  IDs, aliases or clean links.
- `cmd/engine/e2e_test.go`: `/google123.html` is served with its own type and
  no visit routing; `.txt` gets `text/plain`; HEAD has no body; a flow with the
  same name still wins; empty files and deeper paths are not published.
- `internal/admin/handlers_more_test.go`: GET returns an empty document, PUT
  rejects unsafe names/duplicates/empty bodies and stores a valid one.

## GREEN
- `config.Verification`/`Verifications` and `ValidateVerifications`; the same
  `ValidVerificationName` runs in the engine, so a hand-edited file cannot
  publish `../x` or `healthz`.
- Engine `verificationStore`: immutable snapshot, hot reload on the groups
  tick, 4 MiB document cap, 64 KiB per file, corrupt reloads keep the previous
  revision. Files are root-only and resolved after the flow lookup, so an
  existing link never disappears.
- Admin `GET/PUT /api/verifications`, `FileVerifications` (same
  `KUZTDS_VERIFY_FILE` the engine reads), a Tools-page editor with add/remove
  rows and an explicit Save.
- USAGE EN/RU document the variable and the page.

## Why there is no ClickHouse event
A cabinet fetching `/<name>` is infrastructure, not a visitor: logging it would
add fake traffic and cost. The acceptance point about reports applies to
visitor events, not to this check.

## Checks
- Name validation 100%, store and e2e green; full gate, vet and uitest green.
