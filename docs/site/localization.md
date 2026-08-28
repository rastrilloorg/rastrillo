# 🤖 Localization

You declare your locale set and supply catalogs from an `embed.FS`. Each
request resolves one locale, and your actions look strings up through a
request-scoped function.

## Declaring

```go
opts := rastrillo.Options{
	Locales:       []string{"en", "fr"},
	DefaultLocale: "en",
	LocaleFS:      localeFS, // embed.FS carrying locales/<code>.toml
}
```

Catalogs are flat TOML, nothing nested:

```toml
orders.title = "Your orders"
orders.greeting = "Hello, {name}"
```

## How a locale gets picked

In this order: a URL path prefix, stripped before your mux sees it so
`/fr/orders` and `/orders` reach the same route; then the
`rastrillo_locale` cookie, when it names a declared locale; then
`Accept-Language`, q-ordered, so a browser sending `fr-CA` matches a
declared `fr`; then your default.

The cookie is written by the framework's own `POST /_locale` route,
which `rastrillo.Serve` mounts whenever you declare locales at all. The
`locale-menu` partial renders a switcher that posts to it:

```html
{{template "locale-menu" dict "Items" .Locales "Return" .Path}}
```

where `.Locales` is `rastrillo.LocaleItems(r)` — empty for a one-locale
app, so the partial renders nothing — and `.Path` is the current path
and query to return to. Each item lands on the same path under the new
prefix, and the cookie makes the choice stick on unprefixed paths too.

## Looking strings up

```go
rastrillo.T(r, "orders.title")
rastrillo.Tf(r, "orders.greeting", "name", user.Name)
```

`Tf` interpolates `{name}`-style placeholders.

A missing translation stays visible on the page as `orders.title` —
never blank, never a crash. Lookup falls back through five levels,
listed in the next section.

## The framework base catalog

The framework ships its own strings — `rastrillo/ui`'s `rastrillo.ui.*`
keys — in twelve locales: `en`, `ga`, `zh-Hans`, `es`, `hi`, `pt`, `bn`,
`ru`, `ja`, `yue`, `vi`, `ar`. Declare one of those and the built-in
components speak it with no catalog of your own. Matching is by the code
you declare, exactly: `zh` does not find `zh-Hans`.

Lookup falls back through five levels: the requested locale's app
catalog, the default locale's app catalog, the framework's catalog for
the requested locale (when it ships one), the framework's English, and
finally the key itself. A missing translation stays visible on the page
as `orders.title` — never blank, never a crash.

Your own catalog entry for the same key wins, so you can reword a
component's built-in string without ejecting it.

`rastrillo.Dir(locale)` gives the `<html dir>` value — `rtl` for
Arabic, Persian, Hebrew and Urdu — so a layout never guesses.

Inside a partial, a caller-supplied value beats the built-in default.
`ui.FuncsWith` rebinds `T` to a request-scoped lookup so those defaults
resolve in the request's locale rather than in hardcoded English:

```go
tmpl.Funcs(ui.FuncsWith(func(key string, args ...any) string {
	return rastrillo.Tf(r, key, args...)
}))
```

## The pre-ship gate

```sh
rastrillo generate --check --default-locale en
```

Fails when a non-default catalog is missing keys the default has.

It also fails when a non-default catalog is for a locale the framework
does not ship and leaves any `rastrillo.ui.*` key untranslated — those
components would silently render in English. The message lists the keys;
copy them from the module's `locales/en.toml`.

This runs under `--check` only. Plain `rastrillo generate` — and so
`rastrillo dev` and `rastrillo new` — never fails on an incomplete
catalog. That split is the design: silent fallback while you iterate,
loud failure before you ship.

`--default-locale` defaults to `en` and is not read from
`Options.DefaultLocale`. If your app sets a different default, pass the
matching value here, or the check compares every catalog against the
wrong one and passes while telling you nothing.

## Two caveats

A locale code cannot also be a first path segment. An app declaring
locale `en` cannot serve an app route at `/en/...`, because the prefix is
stripped before the mux sees it. That is inherent to prefix routing.

A trailing-slash redirect under a locale prefix drops the locale. When
`ServeMux` issues its own trailing-slash redirect for a path under a
prefix, it emits the unprefixed path. Known limitation, affecting that
one redirect.
