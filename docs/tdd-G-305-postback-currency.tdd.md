# TDD: G-305 — payout/profit и валюта входящего постбека

## Источник
- `docs/TZ-parity.md` G-305: «Сумма и код валюты в постбеке».
- Рантайм: Go, `cmd/engine` postback path, ClickHouse `postbacks`, ECB rates.

## RED (по построению)
- Старый `postbackData` не имел `Currency`, а `parsePostback` читал только
  `profit=` и молча считал его USD. Новые проверки (`pb.Currency`,
  `effectiveProfit(postbackData, …)`) на старом коде не компилировались.
- Поведенческий RED: `payout=10&currency=EUR` при курсе 1.1 записал бы
  `10` USD вместо `11`; неизвестная валюта записалась бы как USD.
- После правки все проверки GREEN.

## GREEN
- `parsePostback`:
  - сумма читается из `profit=`, затем из alias `payout=`;
  - валидный `profit=0` остаётся явным нулём (не заменяется выплатой оффера);
  - `NaN`/`Inf`/мусор не считаются суммой, но не ломают приём;
  - `currency=` нормализуется в верхний регистр, пусто = USD.
- `effectiveProfit`:
  - явная сумма переводится в USD через `cost.NormalizeToUSD` по курсам ЕЦБ;
  - конвертация работает даже без оффера у клика;
  - без суммы берётся `Offer.Payout` в валюте оффера (поведение G-049);
  - неизвестный код валюты → `ok=false`, в `postbacks` пишется 0, в debug —
    причина; валюты не смешиваются в USD-отчётах.
- Журнал G-066: allowlist query расширен полями `payout`, `currency`.
- USAGE EN/RU: пример постбека, precedence `profit` над `payout`, валюта суммы.

## Гарантии
| # | Гарантия | Тест | Итог |
|---|----------|------|------|
| 1 | `profit=` главнее `payout=`; явный 0 сохраняется | `TestParsePostbackPayoutAlias` | PASS |
| 2 | `currency` нормализуется; мусор/NaN/Inf не сумма | `TestParsePostbackPayoutAlias` | PASS |
| 3 | EUR-сумма конвертируется в USD | `TestPostbackCurrencyConvertedToUSD`, `TestEffectiveProfit` | PASS |
| 4 | Конвертация не зависит от наличия оффера | `TestPostbackCurrencyWithoutOffer` | PASS |
| 5 | Неизвестная валюта → 0, не «как USD» | `TestPostbackCurrencyWithoutOffer`, `TestEffectiveProfit` | PASS |
| 6 | Выплата оффера без суммы не изменилась | `TestEffectiveProfit` | PASS |
| 7 | Журнал не теряет `payout`/`currency` и не пропускает секреты | `TestJournalQueryDropsUnknownTokens` | PASS |

## Верификация
- `gofmt -l .` — пусто.
- `go build ./...`, `go vet ./...` — чисто.
- `go test ./cmd/engine/ ./internal/cost/ ./internal/conversion/ -count=1` — PASS.
- Полные гейты и integration `./internal/store/` прогоняются перед коммитом.

## Ограничения
- Если сеть присылает сумму без `currency=`, она по-прежнему трактуется как USD:
  обратная совместимость важнее догадок.
- Валюта без курса не отклоняет постбек: конверсия сохраняется с revenue 0,
  чтобы не потерять факт продажи.
