# 🤖 Localization

An app declares its locale set and supplies catalogs from an
`embed.FS`; each request resolves one locale; actions look strings up
through a request-scoped function.

## Declaring

```go
opts := rastrillo.Options{
	Locales:       []string{"en", "fr"},
	DefaultLocale: "en",
	LocaleFS:      localeFS, // embed.FS carrying locales/<code>.toml
}
```

Catalogs are flat TOML — `key = "value"`, nothing nested:

```toml
orders.title = "Your orders"
orders.greeting = "Hello, {name}"
```

## Resolving

Each request resolves a locale in this order:

1. **URL path prefix** — stripped before the app's mux sees it, so
   `/fr/orders` and `/orders` reach the same route.
2. **`Accept-Language`** — q-ordered, so a browser sending `fr-CA`
   matches a declared `fr`.
3. **The `rastrillo_locale` cookie.**
4. **The default locale.**

Nothing writes that cookie yet. Persisting a user's locale choice across
requests is your app's job for now.

## Looking strings up

```go
rastrillo.T(r, "orders.title")
rastrillo.Tf(r, "orders.greeting", "name", user.Name)
```

`Tf` interpolates `{name}`-style placeholders.

Lookup falls back through four levels: the requested locale's catalog,
the default locale's catalog, the framework's base catalog, and finally
**the key itself**. A missing translation therefore stays visible on the
page as `orders.title` — never blank, never a crash.

## The framework base catalog

`rastrillo.BaseCatalog()` carries `rastrillo/ui`'s own `rastrillo.ui.*`
strings and is wired into every served app's `Locales` automatically. A
single-locale app gets correctly-worded built-in components without
writing a catalog at all.

An app catalog entry for the same key still wins, so you can reword a
component's built-in string without ejecting it.

Inside a partial, a caller-supplied value always beats the built-in
default. `ui.FuncsWith` rebinds `T` to a request-scoped
`rastrillo.T` lookup so those defaults resolve in the request's locale
rather than in hardcoded English:

```go
tmpl.Funcs(ui.FuncsWith(func(key string, args ...any) string {
	return rastrillo.Tf(r, key, args...)
}))
```

## The pre-ship gate

```sh
rastrillo generate --check --default-locale en
```

Fails loudly when a non-default catalog is missing keys the default has.

This gate runs under `--check` **only**. Plain `rastrillo generate` —
and so `rastrillo dev` and `rastrillo new` — never fails on an
incomplete catalog. That split is the design: silent fallback while you
iterate, loud failure before you ship.

`--default-locale` defaults to `en` and is **not** read from
`Options.DefaultLocale`. An app that sets a different default must pass
the matching value here, or the check compares every catalog against the
wrong one and passes while saying nothing useful.

## Two honest caveats

**A locale code cannot also be a first path segment.** An app that
declares locale `en` cannot serve an app route at `/en/...` — the prefix
is stripped before the mux sees it. This is inherent to prefix routing,
not a bug awaiting a fix.

**A trailing-slash redirect under a locale prefix drops the locale.**
When `ServeMux` issues its own trailing-slash redirect for a path under
a locale prefix, it emits the unprefixed path. Known limitation, and it
affects that one redirect only.
