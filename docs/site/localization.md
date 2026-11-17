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

### Formats for the browser, with no prefix or cookie

An app with one language and readers in many countries writes numbers,
money and dates in each reader's own formats, and writes no `/xx/`
prefix and no cookie. Ask for the header's locale alone:

```go
code := locales.NegotiateHeader(r.Header.Get("Accept-Language"))
```

Build `locales` with `rastrillo.NewLocales` over the formats you offer,
your default first. `NegotiateHeader` matches the header as the
middleware does, region to language included (`de-AT` finds `de`), and
returns the default when nothing matches. It ignores the path and the
`rastrillo_locale` cookie, so a stale cookie cannot put half a page in
another locale. `Middleware` and `Negotiate` keep their precedence.

The colour scheme has the same kind of switch beside it. Every shell
offers Light, Dark and System in a `scheme-menu` that posts to the
framework's `POST /_theme`; the choice is stored in the
`rastrillo_scheme` cookie, cleared for System, and `ui.ForRequest`
reads it as `Scheme` so the layout writes `data-theme` on `<html>`
before the first paint. With JavaScript on, `fiesta.js` applies the
choice in place and `rastrillo.js` writes the same cookie. The switch
picks the scheme only; the theme (`day`, `plain`, `signal`) stays your
choice.
See [Themes](/docs/reference/ui#themes).

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

## Numbers

`{{number $L .Count}}` writes a quantity in the page's locale, from the
same CLDR pack money uses: its digits, decimal mark, group mark and
grouping, and its minus sign. Spanish leaves four digits ungrouped
(`1234`, `12.345`); French groups with a narrow no-break space;
Hindi groups in lakhs (`12,34,567`).

Integers and floats are written exactly, as are integer strings such as
`"1234567"`. Any other string is shown as given, so a version like
`"1.10"` is never regrouped. For an exact decimal, pass a
`number.Decimal`:

```go
data["Total"] = number.Decimal("1200.5") // {{number $L .Total}}: 1.200,5 in German
```

Words after the value set the format:

| Template | German |
|---|---|
| `{{number $L .Total}}` | `1.200,5` |
| `{{number $L .Total "plain"}}` | `1200,5` |
| `{{number $L .Total "places" 2}}` | `1.200,50` |
| `{{number $L .Share "percent"}}` | `50 %`, for a `.Share` of 50 |

Nothing is rounded: `"places" 2` pads a shorter fraction and leaves a
longer one whole. A percent writes the value as shown, so store 50 for
50 %. In Go, `number.Format(locale, value, number.Options{...})` does the
same, and `number.InputValue` is the text an input starts from: no group
marks, so no reading in the page's locale takes it for a thousand
times more. A seed such as
German `1,234` (one point two three four) still reads two ways when
read fresh, so a `form.Number` field keeps an unchanged seed unread.

### Reading a number someone typed

`number.ParseNormalized(locale, text)` reads a number the way a money
field reads an amount, with no currency. Text with one plausible
reading is taken, in the locale's marks or the other common ones:

| Locale | Text | Reads as |
|---|---|---|
| `en` | `1,200.5`, `1200,5` | 1200.5 |
| `de` | `1.200,5`, `1200,5` | 1200.5 |
| `fr` | `1 200,5` | 1200.5 |
| any | `1,20,000` | 120000 |
| `en` | `1,500` | 1500: the page's own group mark wins |
| `de` | `1.500` | 1500 |
| `en` | `1.500` | refused: 1.5 or 1500 |
| `de`, `fr` | `1,500` | refused: 1,5 or 1500 |

A refusal is a `*number.AmbiguousError`; its `Choices(locale)` writes
both readings so neither can be misread. A leading `+`, `-` or `−`, a
trailing `-`, and parentheses are signs. Exponents, `NaN`, letters and
malformed groups are `number.ErrInvalid`; more than 30 digits as typed,
zeros included, is
`number.ErrTooLong`. `number.Parse` is the strict reading: the locale's
own marks only.

In a form, declare the field and draw `field-number`:

```go
parsed := form.Parse(r, form.Field{Name: "Seats", Kind: form.Number,
	LocaleEcho: true, Required: true})
seats, present := parsed.Number("Seats") // number.Decimal
```

```html
{{template "field-number" dict "L" $L "Name" "Seats" "Label" "Seats"
  "Value" .Seats.Value "Locale" .Seats.Locale}}
```

Fill `.Seats` from `form.SeedNumber(locale, &value)`, or after a refusal
from `parsed.NumberInput("Seats", locale)`. The field posts the locale
it was drawn in, so `1.500` is never read the other way. On an edit
form, set `StoredNumber` to the value drawn: text left as it was keeps
that value unread, even when it reads two ways.

## Dates

`{{date $L .At "kind"}}` writes a `time.Time` in the page's language,
from CLDR's patterns, which each pack carries beside its number data.
It writes the moment in its own zone, so convert it with `In` first.

| Template | `en-GB` | `en-US` | `de` |
|---|---|---|---|
| `{{date $L .At "dayYear"}}` | `Friday, 20 November 2026` | `Friday, November 20, 2026` | `Freitag, 20. November 2026` |
| `{{date $L .At "short"}}` | `Fri 20 Nov` | `Fri, Nov 20` | `Fr., 20. Nov.` |
| `{{date $L .At "time"}}` | `21:09` | `9:09 PM` | `21:09` |

The kinds are `day`, `dayYear`, `short`, `date`, `dateYear`,
`monthYear`, `month`, `year`, `time` and `stamp`. A nil `*time.Time`
writes nothing. The words around a date stay in your app's language;
only the date's own form follows the locale.

In Go, `dates.For(locale)` gives the same: `Format(kind, t)`, the first
day of the week (`FirstDay`, `WeekStart`, `Heads` for a calendar's
columns), and how a date typed in numbers is ordered and separated
(`Order` and `Separator`: `dmy` and `/` in `en-GB`, `mdy` in `en-US`,
`.` in German). For a locale whose dates are not drawn, such as Thai,
whose calendar is the Buddhist one, it writes ISO dates.

The browser writes the same dates with `dates.mjs`, served from the
module by `dates.Assets()`. Give it `dates.For(locale).Client()` as
JSON. See [dates](/docs/reference/dates).

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
