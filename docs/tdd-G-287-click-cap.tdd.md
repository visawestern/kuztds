# G-287 — daily click cap with a redirect

## RED
- `internal/config/offers_test.go`: `Offer` has no `ClickCap` / `CapURL`; the
  validation cases for a negative cap and a relative/FTP redirect do not
  compile.
- `internal/store/offercap_test.go`: `Counters.CountClick` does not exist; the
  test asks for two served clicks, a capped third, a day-scoped key and no
  Redis write when the cap is off.
- `cmd/engine/offercap_test.go`: an offer with a click cap still serves its URL
  forever; the cap URL, the fallback and the trash paths are all missing.

## GREEN
- `Offer.ClickCap` (`click_cap`) and `Offer.CapURL` (`cap_url`). Validation
  rejects a negative cap and a cap redirect that is not absolute http(s);
  disabled offers stay editable as drafts.
- `Counters.CountClick` uses one Redis `INCR` per served click and returns
  `n > cap`, so concurrent visitors cannot all take the last slot. The key is
  `offerclicks:<flow>:<offer>:<UTC date>` and expires at the next UTC midnight.
  A disabled cap does not touch Redis; a Redis error is reported and the
  engine fails open.
- Engine order after the purchase cap: cap URL → fallback offer → trash. The
  cap URL is a normal redirect (macros and find/replace apply); a fallback is
  served without another cap check, so two offers cannot loop.
- Offer editor: **Daily click cap** and **Redirect when the click cap is
  reached**; `normOffers` trims the URL and clamps the number.
- USAGE EN/RU describe both caps and the fail-open behaviour.

## Checks
- Config, store (miniredis) and engine tests green; node helper checks cover
  the new offer fields; full gate 18 packages ok.
