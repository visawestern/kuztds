# G-134 — OR between stream conditions

## RED
- `internal/router/router_test.go`: `config.Rules` has no `Any` field, so the
  OR tests do not compile. The tests ask for two whitelists (language and
  country): with OR a visitor matching either must pass, a visitor matching
  neither must be rejected, and an empty rule set must still match everyone.
- A separate test pins that OR cannot bypass the impression limit.

## GREEN
- `config.Rules.Any` (`json:"any,omitempty"`) selects OR for the whole set of
  conditions; the zero value keeps the existing AND behaviour.
- `router.Why` now evaluates the configured conditions as a list of
  `(rule key, passed)` pairs. AND returns the first failing key exactly as
  before; OR admits on the first passing key and names the first configured
  key when none pass. Empty conditions match in both modes.
- The limit stays outside the list: it is checked only after the conditions
  admit the visitor, so OR cannot be used to exceed a cap.
- Flow editor: the When section has **Conditions must — all hold (AND) / any
  one holds (OR)**, the header hint follows the mode, `collectStream` saves
  `rules.any`, and old configs load as AND.
- USAGE EN/RU describe the switch and the limit caveat.

## Checks
- `go test ./internal/router/ ./internal/config/` green; `TestLogicHelpers`
  (node, `-tags=uitest`) covers the AND/OR hint; full gate 18 packages ok.
