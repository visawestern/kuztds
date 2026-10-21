# G-059 — clean share links with an Open Graph preview

## RED
- `internal/config/share_test.go`: slug shape, namespace collisions, image URL,
  preview fields without a slug.
- `cmd/engine/e2e_test.go`: `/summer` serves an escaped OG card and forwards the
  query to `/g1`; the card request carries no stream; deeper paths, unknown
  slugs and disabled flows do not advertise; script tags in fields are escaped.
- `internal/admin/handlers_more_test.go`: `PUT /api/groups` rejects a slug that
  equals an ID, two groups with one slug, a relative image and fields without a
  slug; a valid share saves.
- `internal/admin/web_logic_test.go`: `normShare` trims, lower-cases and turns
  off a preview without a slug.

## GREEN
- `config.Share{Slug,Title,Description,Image}` on the group; `ValidateShares`
  keeps slugs in the same namespace as IDs/aliases and bounds preview fields.
- `config.Groups` indexes share links separately (`GetShare`); group lookup
  still wins, which is why the admin forbids collisions.
- Engine `serveShare`: `text/html`, `no-store`, `noindex`, OG/Twitter tags,
  meta refresh and a plain link to `/<id>` + the original query. It writes no
  click; the browser's follow-up goes through the normal pipeline.
- Admin: a Clean share link section in Flow settings, client-side slug check,
  the link in the Links block; preview text is stored through `normShare`.
- Example flow `promo` uses `summer-preview`; USAGE EN/RU describe the route.

## Why it is safe
- All preview fields are escaped with `html.EscapeString`; the image URL is
  validated as absolute http(s) at save time.
- Crawlers fetching the card do not create visits; the actual jump is tracked
  as usual, so stats stay comparable with a direct group link.
