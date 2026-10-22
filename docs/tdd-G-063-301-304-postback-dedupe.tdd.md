# G-063 / G-301 / G-304 — idempotent postbacks and (cid, tid) dedupe

## RED
- `cmd/engine/postback_dedupe_test.go`: a failed ClickHouse insert still spent
  the offer's daily slot; a repeated `(cid, tid)` spent a second slot; two
  distinct tids of one click did not.
- `internal/store/offercap_test.go`: no `CountConversionOnce`; the daily cap
  could only be counted blindly, so a retry inflated it.
- `internal/store/postbacks_integration_test.go`: a duplicate `(cid, tid)` was
  two conversions and doubled revenue/cost; two tids of one cid were expected
  to remain two conversions.
- `internal/store/postbacks_migrate_integration_test.go`: a legacy
  `ORDER BY (cid)` table had no supported rebuild path.

## GREEN
- `postbackStore` is the narrow engine dependency
  (`EventGroupID`/`EventOfferID`/`RecordPostback`); a fake now proves insert
  failure and duplicate behaviour without ClickHouse.
- The cap is spent only after `RecordPostback` succeeds, only for non-trash
  conversions with a known offer. `Counters.CountConversionOnce` does `SADD`
  of `conversion.DedupKey(cid, tid)` and the daily `INCR` in one Lua script;
  the set lives 48h, the day counter expires at the next UTC midnight. Redis
  errors stay fail-open.
- ClickHouse `postbacks` is `ReplacingMergeTree(version)`, `ORDER BY (cid, tid)`.
  `version DateTime64(3) DEFAULT now64(3)` picks the newest status/profit for a
  repeated transaction. Reads use `FINAL`, so different tids of one click stay
  separate and retries collapse before they reach revenue, cost or status
  counts.
- `cmd/chmigrate` performs the explicit legacy rebuild: dry-run by default,
  `-apply` copies with `version=toDateTime64(ts, 3)`, verifies old/copied/FINAL
  counts, swaps with `EXCHANGE TABLES`, and only then drops the old table.
- Raw postback query is no longer logged; the journal uses an allowlist
  (`cid`, `status`, `tid`, `profit`), so `pb` and unknown network tokens do not
  reach logs or `postback_errors`.

## Verification found and fixed
- Fresh `001_events.sql` used `ORDER BY (cid, tid)` without a `tid` column, so
  a new ClickHouse container exited during init. The fresh schema now declares
  `status`, `tid`, `cost` and `offer`.
- `FINAL` exposed an alias collision in `Performance`: `sumIf(profit) AS profit`
  became a nested aggregate. The alias is now `revenue`.
- `PostbackStatuses` scanned ClickHouse `count()` (UInt64) into `int64`; the
  integration test now passes with `uint64` conversion.
- Cap tests used a fixed date and miniredis's real clock, so `EXPIREAT` made
  counters expire immediately after the date changed. Tests now call
  `mr.SetTime(now)`.

## Checks
- `go test -tags=integration ./internal/store/ -count=1` — all integration
  tests, including dedupe and legacy migration, green.
- Full unit/vet/build/uitest gates run before commit; 18 packages plus the
  admin UI helper suite.

## Remaining
- G-299 is still PART: dedupe/update by transaction id is done, but the
  new/ignore/update modes are not configurable.
- The engine → ClickHouse path is covered through the store interface fake and
  store integration tests, not a single live end-to-end process test.
