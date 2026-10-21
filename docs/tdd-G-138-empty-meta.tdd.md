# TDD evidence — G-138 «Мета-слово @empty»

## RED

```
--- FAIL: TestSelect_EmptyMetaWord  (empty referer with @empty = <nil>)
--- FAIL: TestSelect_QueryEmptyMetaWord (absent parameter = <nil>)
```

## GREEN

```
--- PASS: TestSelect_EmptyMetaWord       (@empty в IS; IS NOT @empty только непустые; не подстрока)
--- PASS: TestSelect_QueryEmptyMetaWord  (name=@empty: отсутствует, пустой; не matches заполненный)
--- PASS: TestE2E_EmptyRefererMetaWord   (нет Referer → норef-поток, с Referer → обычный)
--- PASS: существующие Select-тесты       (нет регрессии по литералам)
```

## Поведение

- `@empty` (регистр не важен, пробелы обрезаются) — мета-слово, а не текст:
  совпадает с пустым/отсутствующим значением. `IS NOT @empty` пропускает
  только непустые.
- Поддержано в текстовых списках: referer, keyword, UA, domain, city/region,
  org, OS/browser/brand, lang/country; для URL-параметров — `name=@empty`
  (параметр отсутствует или пуст). Литерал `x@empty` не совпадает.
- Пустое значение — то же, что у движка: `""` и служебный `"-"` (referer).
- UI-подсказки у Referer/Keyword/URL parameter; USAGE EN/RU, README.
- Схема/отчёты не менялись: фильтр влияет только на выбор потока.

## Гейты

`gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -tags=uitest ./internal/admin/` — зелёные.
