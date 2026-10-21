# TDD evidence — G-056 «Шаблоны партнёрских сетей» + G-057 «Ссылка-памятка»

Два пункта закрыты одним срезом: шаблон сети задаёт токены кабинета, а карточка
оффера печатает готовую ссылку постбека, которую оператор вставляет в кабинет.

## G-056 — шаблоны сетей

- `Group.Networks []Network{ID,Name,CIDToken,ProfitToken,StatusToken,TxnToken}`;
  `Offer.NetworkID` ссылается на шаблон, свободный `Offer.Network` остаётся
  меткой для отчётов.
- Токены — то, что кабинет сети подставляет в URL: `{sub1}`, `{payout}`,
  `{status}`, `{txn}`. Пустые поля дают дефолты `{subid}`, `{payout}`,
  `{status}`, `{txn}` (`Network.Tokens`).
- Валидация: id как у офферов (уникальный), токен строго `{...}` из
  `[A-Za-z0-9_.-]` ≤32, дубли токенов запрещены, `Offer.NetworkID` обязан
  существовать; битый шаблон → 400 в админке.
- Движок не менялся: шаблон — это настройки для кабинета сети, входящие
  постбеки уже принимают `cid/profit/status/tid`; горячий путь не затронут.

## G-057 — ссылка-памятка

- `postbackMemo(base,key,networkID,networks)` (чистая функция в UI) собирает
  `https://tds.example/?pb=K&cid={sub1}&profit={payout}&status={status}&tid={txn}`.
- Ключ приходит из `/api/me` (`postback_key`), который админка читает из
  `KUZTDS_POSTBACK_KEY`; база — `KUZTDS_ENGINE_URL`. Без ключа или базы ссылка
  не показывается, вместо неё подсказка.
- В карточке оффера — код ссылки и кнопка copy; токены берутся из выбранного
  шаблона, при его отсутствии — дефолтные.
- `/api/me` доступен только аутентифицированному админу; ключ не попадает в
  публичные ответы.

## RED

```
handlers_more_test.go:442: bad network ... → 400, got 200   (5 кейсов)
--- FAIL: TestLogicHelpers  (normNetworks/postbackMemo не определены)
internal/config: undefined: Network, unknown field NetworkID
```

## GREEN

```
--- PASS: TestNetworkValidate        (7 отказов + валидный шаблон)
--- PASS: TestNetworkLookupDefaults  (поиск, дефолты, пустой id)
--- PASS: TestGroupsSaveRejectsBadNetwork (6 × 400, валидный → 200)
--- PASS: TestLogicHelpers           (normNetworks: trim/default; postbackMemo: токены, без базы/ключа — пусто)
ok  internal/config, internal/admin, internal/admin (uitest)
```

## UI / доки / пример

- Flow settings → Affiliate networks: карточки id/name + 4 токена; оффер
  выбирает шаблон; ссылка-памятка в карточке оффера.
- `configs/groups.example.json`: сеть `sticky` с `{sub1}/{payout}/{status}/{txn}`,
  оффер `o1` привязан к ней.
- USAGE EN/RU, README; PARITY: G-056 и G-057 DONE.

## Гейты

`go build ./...`, `go vet ./...`, `gofmt -l .` — чисто; полный `go test ./...` и
`-tags=uitest ./internal/admin/` — зелёные.
