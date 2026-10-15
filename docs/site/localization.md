# 🤖 Localization

You declare your locale set and supply catalogs from an `embed.FS`. Each
request resolves one locale, and your actions look strings up through a
request-scoped function.

## Declaring

Declare the languages your app offers in `manifest/app.toml`.
`rastrillo new` writes one:

```toml
# The locales this app offers. Add a code to offer another language,
# then run rastrillo generate; with two or more, URLs gain /xx/
# prefixes and pages a language switcher.
locales = ["en"]
default_locale = "en"
```

A new app offers one language: English, or the one you pass to
`rastrillo new --locale`. It has no `/xx/` prefixes and no switcher.

`rastrillo generate` writes the two settings to `gen/locales`, and
`main.go` passes them to `rastrillo.Resolve`:

```go
	opts, err := rastrillo.Resolve(rastrillo.Options{
		DBPath: "shop.db",
		Logger: logger,
		// Locales and DefaultLocale come from manifest/app.toml, through
		// gen/locales. Boot refuses a configuration whose packs this
		// binary does not link.
		Locales:       genlocales.Locales,
		DefaultLocale: genlocales.DefaultLocale,
	})
```

To offer another language, add its code to `locales` in
`manifest/app.toml` and run `rastrillo generate`. That links its
formatting pack. Then add `locales/<code>.toml`. If the framework does
not ship the language, translate every `rastrillo.ui.*` key in it.

Write each code the way CLDR does: `en-US`, `zh-Hant`, `pt-PT`. The
app refuses to start on a code it cannot use, and says what to write
instead. See [locales](/docs/reference/locales).

`Options.LocaleFS` carries your catalogs: an `embed.FS` holding
`locales/<code>.toml`. Catalogs are flat TOML, nothing nested:

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
which `rastrillo.Serve` mounts whenever `Options.Locales` is set. Every
shell has a language switcher that posts to it. It reads `Locales` and
`Return` from the page's data, and `ui.ForRequest(r)` gives you both,
so there is nothing to wire. A one-locale app has no items, and the
switcher renders nothing. Each item lands on the same path under the new
prefix, and the cookie makes the choice stick on unprefixed paths too.

## Looking strings up

```go
rastrillo.T(r, "orders.title")
rastrillo.Tf(r, "orders.greeting", "name", user.Name)
```

`Tf` interpolates `{name}`-style placeholders.

In a template, pass the page's localizer first:
`{{T $L "orders.title"}}`. See [Templates](/docs/templates#template-functions).

A missing translation stays visible on the page as `orders.title` —
never blank, never a crash. Lookup falls back through five levels,
listed in the next section.

## The framework base catalog

The framework ships its own strings, `rastrillo/ui`'s `rastrillo.ui.*`
keys, in twelve locales: `en`, `ga`, `zh-Hans`, `es`, `hi`, `pt-PT`,
`bn`, `ru`, `ja`, `yue`, `vi`, `ar`. Declare one of those and the
built-in components speak it with no catalog of your own. Matching is by
the code you declare, exactly: `zh` does not find `zh-Hans`.

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
The defaults come from the localizer you pass as `"L" $L`, so they are
in the page's language.

### Portuguese is pt-PT

The framework's Portuguese is European Portuguese, and it now ships as
`pt-PT`, not `pt`. Money on a `pt-PT` page reads `1284,50 €`. In an app
that offers `pt-PT`, what still says `pt` keeps working:

- A request under `/pt/` redirects to the same path under `/pt-PT/`:
  301 for GET and HEAD, and 308 for anything else, so a posted form
  keeps its body.
- A `rastrillo_locale=pt` cookie, and a switcher post of `pt`, pick
  `pt-PT`.
- Your own `locales/pt.toml` is still read, for `pt-PT`.

`rastrillo new --locale pt` scaffolds `pt-PT`. Links to the gallery's
old `/pt/` pages redirect too, to the top of the page.

## The pre-ship gate

```sh
rastrillo generate --check
```

Fails when a catalog is missing keys your default language's catalog
has. The default is `default_locale` in `manifest/app.toml`.

It also fails when a language you offer needs a `locales/<code>.toml`
you have not written. That is any language the framework does not ship,
and any language but English when your app has strings of its own. The
message names the file and the reason. For a language the framework does
not ship, copy the keys from the module's `locales/en.toml` and
translate every one. `rastrillo doctor` runs the same check.

This runs under `--check` only. Plain `rastrillo generate`, and so
`rastrillo dev` and `rastrillo new`, never fails on an incomplete
catalog: you get the fallback while you work, and the failure before
you ship.

`--default-locale` was removed. Passing it fails with
`--default-locale was removed: the default comes from default_locale in manifest/app.toml`.

## Two caveats

A locale code cannot also be a first path segment. An app declaring
locale `en` cannot serve an app route at `/en/...`, because the prefix is
stripped before the mux sees it. That is inherent to prefix routing.

A trailing-slash redirect under a locale prefix drops the locale. When
`ServeMux` issues its own trailing-slash redirect for a path under a
prefix, it emits the unprefixed path. Known limitation, affecting that
one redirect.
