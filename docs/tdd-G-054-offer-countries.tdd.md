# TDD evidence — G-054 «Пометки по странам»

Пункт книги: «Какие страны берёт предложение» (Keitaro 11, Binom v2 частично).
Срез: у оффера список стран, которые покупает сеть; визит из страны вне списка
не отдаётся этой сети.

## Решение

- `Offer.Countries []string` — ISO 3166-1 alpha-2, сравнение без учёта регистра,
  пустой список = все страны (обратная совместимость).
- Валидация: код ровно из двух букв, дубли (в т.ч. в другом регистре) — ошибка;
  выключенный черновик тоже проверяется на форму, но не на полноту.
- Движок: после резолва оффера — если страна не принимается, пробуем `fallback`
  (он тоже должен принимать страну); иначе trash + warn. Проверка капа идёт
  после выбора по стране, `fallback` по капу тоже обязан принимать страну.
- Хот-путь: одно сравнение по короткому слайсу, без сети и блокировок.

## RED

```
internal/config/offers_test.go: ... unknown field Countries in struct literal of type config.Offer
cmd/engine/offer_test.go:127: unknown field Countries in struct literal of type config.Offer
FAIL github.com/egerkuzma/kuztds/cmd/engine [build failed]
```

## GREEN

```
--- PASS: TestOfferCountriesValidate
--- PASS: TestOfferAcceptsCountry
--- PASS: TestOfferCountries        (DE → main, US → backup, RU → trash; без fallback → trash)
--- PASS: TestGroupsSaveRejectsBadOffer
ok  internal/config, cmd/engine, internal/admin
```

## UI / API / доки

- `admin/web/index.html`: поле «Countries this offer buys» у карточки оффера;
  `normOffers` режет по запятым/пробелам, верхний регистр, дедуп; подпись
  fallback теперь «when the cap is reached or the country is not bought».
- `handlers_more_test.go`: 400 на `["USA"]` и `["DE","de"]`; валидный payload
  с `["de","FR"]` сохраняется.
- `configs/groups.example.json`: у `o1` — `["DE","FR","AT"]`, `backup` без
  ограничений; USAGE EN/RU + README.

## Гейты

`go build ./...`, `go vet ./...`, `gofmt -l .` — чисто; `go test ./...` и
`-tags=uitest ./internal/admin/` — зелёные.
