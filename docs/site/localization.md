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
`/fr/orders` and `/orders` reach the same route; then `Accept-Language`,
q-ordered, so a browser sending `fr-CA` matches a declared `fr`; then
the `rastrillo_locale` cookie; then your default.

Nothing writes that cookie yet. Persisting a user's locale choice across
requests is your job for now.

## Looking strings up

```go
rastrillo.T(r, "orders.title")
rastrillo.Tf(r, "orders.greeting", "name", user.Name)
```

`Tf` interpolates `{name}`-style placeholders.

Lookup falls back through four levels: the requested locale's catalog,
the default locale's catalog, the framework's base catalog, and finally
the key itself. A missing translation stays visible on the page as
`orders.title` — never blank, never a crash.

## The framework base catalog

`rastrillo.BaseCatalog()` carries `rastrillo/ui`'s own `rastrillo.ui.*`
strings and is wired into every served app automatically, so a
single-locale app gets correctly-worded built-in components without
writing a catalog at all.

Your own catalog entry for the same key wins, so you can reword a
component's built-in string without ejecting it.

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
