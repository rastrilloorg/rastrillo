# 🤖 The design system: rastrillo.org/design-system, themes, shells, base languages, date and time

Design, 2026-08-28. Direction approved by Paul in conversation; this is
the written form for review.

Three screenshots started this. A sign-in form and a strip of links
sitting flush against a card border, and a notice composed as heading |
paragraph | button in one flex row. PR #98 wrote down the rules those
broke. But the rules were written because nobody could *see* the
vocabulary: the partials are documented in template comments, the
class idioms in a package doc, the tokens in a CSS header, and none of
it renders anywhere a person or an agent can look at before choosing.

This builds the place to look, and the three choices a new app makes
before it has a single screen: a theme, a shell, and the languages it
speaks. Then it adds the two form controls that every one of the six
CARLOS apps has had to hand-roll — a date/time field and (already
shipped, now finished) a filterable select — on the terms the rest of
the library already honours: native control as the value carrier, one
opt-in attribute, no dependencies, every string through the catalog.

## 0. The proviso

Everything here is localisable into every language a rastrillo app can
declare. Concretely:

- No user-visible string in a partial, a shell, or a script is
  hardcoded. Every one is a `rastrillo.ui.*` key resolved through `T`
  at render time, or carried to the browser on a `data-` attribute
  when the markup is built there.
- The date parser has **no English vocabulary in it**. Its relative-word
  vocabulary comes from the catalog; its weekday and month names come
  from `Intl.DateTimeFormat` for the page's `lang`. "No English
  vocabulary", not "no English": as built the file keeps English
  fallbacks for the eleven labels it puts on screen, the convention
  `select.js` already set, so a field that reaches it without its
  attributes still works — not one word the PARSER matches on is
  written there (as built, 2026-08-29).
- The framework ships twelve base catalogs (§3) so a single-locale app
  in any of them gets correctly-worded components without writing a
  catalog, and a multi-locale app gets a language switcher (§2.4) it
  did not build.
- `rastrillo generate --check` fails when an app's non-default catalog
  is missing a translation for any `rastrillo.ui.*` key (§3.4). This
  is what turns "localisable" from possible into enforced.
- Layout uses CSS logical properties so `dir="rtl"` mirrors without a
  second stylesheet, and the design-system page renders in Arabic to
  prove it.

## 1. Themes

### 1.1 What a theme is

AS BUILT (v2, 2026-08-29). A theme is colour, type family and shape:
one `:root` block setting `color-scheme: light dark`, every colour
declared once as `light-dark(<light>, <dark>)`, single-valued tokens
(`--rst-font`, the radii) written plain, and exactly two toggle rules —
`:root[data-theme="light"] { color-scheme: light; }` and
`:root[data-theme="dark"] { color-scheme: dark; }` — which re-resolve
every `light-dark()` in the file at once, in both directions.
`light-dark()` is a colour function, so a shadow token wraps only its
colour and writes the geometry outside the call. Nothing structural.
Those blocks used to live inside `ui/tokens.css`; they moved out in v1,
and the shape tokens (`--rst-radius`, `--rst-radius-sm`,
`--rst-radius-pill`, `--rst-shadow-pop`, `--rst-shadow-knob`,
`--rst-shadow-lift`, `--rst-overlay`) followed them in v2.

```
ui/tokens.css           structure only: spacing, the type scale, every component class
ui/themes/day.css       the default — an everyday blue (#2464e0) on white and grey, 8/6px radii
ui/themes/plain.css     the skeleton — greyscale, accent = text colour, 4/3px radii, hairline shadows
ui/themes/signal.css    graphite neutrals, one electric cobalt (#1a56ff), 4/2px radii, short dense shadows
```

(v1 shipped `ink`, `teal` and `warm`; §6-v2 replaced all three.)

Each theme file carries the same custom-property set — a gate asserts
the three declare identical property names — and its own WCAG 2.2 AA
table in the header, in BOTH schemes, computed from its hex values.
`ui/contrast_test.go` parameterises over `ThemeNames()` × {light, dark},
splitting each `light-dark()` back into two per-scheme tables, and
asserts every documented pair; a new theme that fails a pair does not
ship.

### 1.2 API

```go
func ThemeNames() []string            // "day", "plain", "signal" — day first, the default
func ThemeCSS(name string) ([]byte, bool)
```

### 1.3 Delivery

`rastrillo new --theme=day` (default `day`) writes `static/tokens.css`
and `static/theme.css`. The file is always named `theme.css`, whatever
theme it holds: the layout links one fixed name, and switching theme
later is replacing one small file. The scaffold's vendored-pin test
gains a line for `theme.css`, keyed to the chosen theme name recorded
in the test's map.

The examples keep the default theme and their pins (`day` since v2).

## 2. Shells

### 2.1 What a shell is

A `layout.html` plus the class idioms it uses. Three, drawn from the
six apps:

| Shell     | Drawn from                       | Chrome                                                                         |
|-----------|----------------------------------|--------------------------------------------------------------------------------|
| `column`  | birthday-alarm                   | none — today's scaffold layout: `<main class="rst-page">`, made explicit       |
| `topbar`  | vitogo, amadan                   | brand, nav links, `<details>` account menu, locale menu, footer                 |
| `sidebar` | seapointish, messenger, keymail  | fixed left rail with nav groups ≥800px; a `<details>` chrome bar below that     |

### 2.2 The template contract

```go
func LayoutNames() []string
func Layout(name string) ([]byte, bool)
```

Every shell defines `layout` and renders `{{template "content" .}}`.
It also declares blocks the app may override, all with working
defaults:

```
{{block "title" .}}     — page <title>
{{block "brand" .}}     — the mark/name link in the chrome
{{block "nav" .}}       — the primary links (topbar: inline; sidebar: grouped)
{{block "account" .}}   — the <details> account menu
{{block "locale" .}}    — the language switcher (§2.4); empty for a one-locale app
{{block "foot" .}}      — topbar only
```

`column` defines only `title` and `locale`, and renders the switcher
as a single line above `<main>` when there is more than one locale.
As built, `column` defines `title`, `lang` and `dir` and no `locale`
block at all (as built, 2026-08-28).

All three shells also carry `{{block "head" .}}{{end}}` as the last
thing in `<head>` — an empty slot for a favicon, a meta tag, an extra
stylesheet or a script that must run before the body. It was added for
the gallery's shell demos, which need `gallery.js` inside an iframe that
inherits nothing from the page around it, and kept because an app wants
the same hole; being last is what lets an app's own CSS win the ties it
should against `tokens.css` and the theme (as built, 2026-08-29).

The shell classes — `rst-shell-topbar`, `rst-shell-sidebar`,
`rst-shell__rail`, `rst-shell__chrome`, ~~`rst-shell__menu`~~ (never
built; see §2.4) — live in `tokens.css` beside the other class idioms, with `styleguideSamples`
entries so `TestIdiomClassesAreStyled` covers them. Mobile collapse of
the sidebar is `<details class="rst-shell__chrome">` — no JavaScript.

Built-in strings — skip link, "Menu", "Account", "Language" — are
`{{T "rastrillo.ui.shell_skip"}}` etc. The `<html>` element carries
`lang` and `dir` from the request: the scaffold's render helper passes
them in `data`; `ui` exports `Dir(locale) string` (`"rtl"` for `ar`,
`fa`, `he`, `ur`; `"ltr"` otherwise) so an app never guesses.
`Dir` shipped in the root `rastrillo` package instead, beside the rest
of the locale surface, so `ui` need not import it (as built, 2026-08-28).

### 2.3 Delivery

`rastrillo new --shell=column` (default `column`) writes the chosen
`templates/layout.html`. It is a template, so it is app-owned and
unpinned — an app edits its layout on day one. `--theme` and `--shell`
join `--icons`, `--icon-delivery` and `--ux` in the usage line and are
validated before any file is written.

### 2.4 The language switcher

A `<details class="rst-shell__menu rst-locale">` listing the app's
declared locales. The switcher rides `rst-dropdown rst-locale`, reusing
the ordinary dropdown vocabulary rather than a `rst-shell__menu` class
that never needed to exist (as built, 2026-08-28). Each item is a link to the **same path under that
locale's prefix** (`/ga/orders` for `/orders`), labelled by its
autonym (`rastrillo.ui.locale_name` in that locale's catalog:
"Gaeilge", "日本語", "العربية"), the current one `aria-current="true"`.
Hidden when the app declares one locale.

The choice has to survive leaving the prefix. `docs/site/localization.md`
says: "Nothing writes that cookie yet. Persisting a user's locale
choice across requests is your job for now." This closes that. Each
switcher link goes through `POST /_locale` (a form per item, the
Locale in a hidden field, `return` carrying the current path) which
sets `rastrillo_locale` and 303s to the prefixed path. The route is
mounted by `rastrillo.Serve` like the asset handler, is CSRF-checked by
the same origin check every POST gets, and refuses a locale that is not
declared. The switcher's data (`Locales []LocaleItem{Code, Name, Href,
Current}`) comes from a new `rastrillo.LocaleItems(r)` helper.

## 3. Base languages

### 3.1 The set

Ethnologue's ten largest first-language populations, plus Irish, plus
Arabic for right-to-left coverage — twelve catalogs:

| Code      | Language                 | Why                              |
|-----------|--------------------------|----------------------------------|
| `en`      | English                  | the source of truth              |
| `ga`      | Irish                    | ours                             |
| `zh-Hans` | Mandarin, simplified     | Ethnologue L1 #1                 |
| `es`      | Spanish                  | #2                               |
| `hi`      | Hindi                    | #4                               |
| `pt`      | Portuguese               | #5                               |
| `bn`      | Bengali                  | #6                               |
| `ru`      | Russian                  | #7                               |
| `ja`      | Japanese                 | #8                               |
| `yue`     | Cantonese, traditional   | #9                               |
| `vi`      | Vietnamese               | #10                              |
| `ar`      | Arabic                   | RTL proof; the shells must mirror|

Shipped at `locales/<code>.toml` in the framework module, embedded.
Each is machine-drafted and says so in a header comment, so a native
speaker knows it is correctable. The parser fixtures (§4.3) are the
concrete check that at least the date vocabulary round-trips.

### 3.2 The key set

Every catalog holds exactly the `en` key set — a gate diffs them.
Today's keys (`pagination`, `search_submit`, `cancel`, `done`,
`select_*`) plus:

```
rastrillo.ui.locale_name           the autonym
rastrillo.ui.shell_skip            "Skip to content"
rastrillo.ui.shell_menu            "Menu"
rastrillo.ui.shell_account         "Account"
rastrillo.ui.shell_language        "Language"
rastrillo.ui.date_*                §4.2 — vocabulary, quick picks, hint, status
rastrillo.ui.date_end_before_start the range error
```

### 3.3 Lookup order

`BaseCatalog()` becomes `BaseCatalogs() map[string]Catalog`. The
fallback chain grows one level:

```
app catalog (request locale) → app catalog (default) → framework catalog (request locale) → framework en → key
```

`NewLocales`'s signature is unchanged: it reads the framework catalogs
internally rather than taking the map from its caller (as built,
2026-08-28). A declared locale with no framework catalog
(`fr`, say) skips the third level and lands on `en`, which is today's
behaviour exactly. The framework catalog is looked up by the app's
*declared* code, and only that: an app declaring `zh` gets no framework
Chinese unless it declares `zh-Hans`, and the `--check` message says so.
Request-to-declared matching (`Accept-Language` `fr-CA` → declared `fr`)
is unchanged and happens before any catalog is consulted.

### 3.4 The gate

`rastrillo generate --check` grows one rule: a non-default app catalog
must carry every `rastrillo.ui.*` key **unless** the framework ships a
catalog for that locale. So an app declaring `fr` is told its
components will be English until it translates ~40 keys; an app
declaring `ja` is not, because they will be Japanese. The message
lists the missing keys and points at `locales/en.toml` as the template.

## 4. Date and time

### 4.1 Partials

Four, each `field-text`'s envelope (label, hint, help, error, the same
`aria-describedby` wiring) around native inputs:

```
field-date       <input type="date">                        POSTs 2006-01-02
field-time       <input type="time">                        POSTs 15:04
field-datetime   <input type="datetime-local">              POSTs 2006-01-02T15:04
field-daterange  two field-datetime (or date+time pairs) in a rst-field-row, wrapped in data-rst-range
```

Each emits `data-rst-date` (range: `data-rst-range="start"|"end"`)
unless `Plain`, and with it every string the script needs as
`data-rst-date-*` attributes resolved through `T`. Keys: `Name`,
`Label`, `Value`, `Required`, `Hint`, `Help`, `Error`, `Min`, `Max`,
`Plain`; range adds `Start`/`End` sub-dicts and `Seed` (`"session"`
seeds end = start + 1h in the browser, as Tito Go's does).

The key list shipped without `Help`: the date partials carry `Hint`
alone (the muted line under the input), matching `field-text`'s envelope
rather than `field-select`'s, and a range is both-`date` or
both-`datetime` — `field-daterange`'s `Kind` picks the input type for
both halves at once, so a mixed pair is not expressible (as built,
2026-08-29).

`data-rst-range` is wrapper-scoped rather than per-half: it sits on the
`rst-field-row` and carries `Seed` as its own value (`data-rst-range` or
`data-rst-range="session"`), the two armed inputs inside pairing up by
DOM order — first is the start, second the end — so neither half needs a
`"start"`/`"end"` marker and the two halves stay byte-identical to the
singular partials they compose (as built, 2026-08-29).

Timezone is not a date field's concern: an app that needs one renders
a `field-select` of zones beside it and parses in that location (§4.4),
which is what Tito Go does and what the events example will show.

### 4.2 `ui/datetime.js`

A port of Tito Go's `smart-datetime.js` with its vocabulary layer
replaced. Same contract as `select.js`: first-party, dependency-free,
inert without the attribute, idempotent on re-scan, native input stays
in the DOM as the value carrier so the POST is byte-identical with
scripts off. ~25 KB; its own file, its own vendored-pin line, linked by
every shell after `select.js`.

What it does, unchanged from Tito Go: a WAI-ARIA 1.2 combobox over the
native input; typed text is parsed into a preview row ("Set ↵") and
committed on Enter; a quick-pick list (today, tomorrow, next Monday, in
a week; for a range end: +1h, +2h, end of that day, same time next
day); a real labelled button that calls `showPicker()` for the
browser's own calendar or clock; unparsed text arms nothing, so Enter
refuses rather than guessing; display via `Intl.DateTimeFormat`.

What changes:

- **Vocabulary from the catalog.** Relative words and connectives ride
  on attributes: `date_today`, `date_tomorrow`, `date_yesterday`,
  `date_next`, `date_last`, `date_in`, `date_ago`, `date_at`,
  `date_day`/`date_days`, `date_week`/`date_weeks`, `date_month`/
  `date_months`, `date_hour`/`date_hours`, `date_minute`/`date_minutes`,
  `date_noon`, `date_midnight`, `date_am`, `date_pm`. The unit words
  shipped as seventeen SINGULAR keys whose values carry the plurals on
  the same `|` the spellings use (`date_day = "day|days"`,
  `date_minute = "minute|minutes|min|mins|m"`), not as the
  `date_day`/`date_days` pairs listed here — one key per concept, with
  the language's own forms inside it (as built, 2026-08-29). A key may
  hold several accepted spellings separated by `|` ("tomorrow|tmrw"),
  and
  the matcher is accent- and case-folded (`NFD`, strip marks, lower),
  so "amárach" and "amarach" both parse. The whole vocabulary rides on
  ONE attribute, not one per word: `{{dateWords}}` (bound to the same
  translator `T` is, so it localises per request) resolves the
  seventeen keys and JSON-encodes them into `data-rst-date-words` (as
  built, 2026-08-29).
- **Weekdays and months from Intl.** Long and short forms for the
  page's `lang`, via `formatToParts`, which Tito Go already does for
  read-back. So "mar 3", "3 mars", "3月3日" and "٣ مارس" all parse with
  zero catalog entries.
- **Numerals.** Digits are folded from the locale's numbering system
  to ASCII before parsing (`Intl.NumberFormat(lang).formatToParts` gives
  the digit set), so Arabic-Indic and Devanagari digits work.
- **Word order.** The grammar is word-set based, not positional: "in 2
  weeks", "en 2 semanas", "2 週間後" all reduce to {`in`, 2, `weeks`}.
  Where a language marks the relation with a suffix (Japanese 後,
  Vietnamese "nữa"), the catalog value for `date_in` is that suffix and
  the matcher accepts it on either side of the quantity.
- **Locale** is `document.documentElement.lang`; a field may override
  with `data-rst-date-lang`.

Strings the user hears — the set-prompt, the hint row, the live-region
results — come from `date_set`, `date_hint`, `date_results`,
`date_result_one`, `date_pick` (the picker button's label), and the
quick-pick labels `date_quick_today`, `date_quick_tomorrow`,
~~`date_quick_next_monday`~~, ~~`date_quick_week`~~, `date_quick_plus_1h`,
`date_quick_plus_2h`, `date_quick_end_of_day`, `date_quick_next_day`.

`{example}` in `date_hint` and `{n}` in `date_results` are substituted
in the BROWSER, not at render: the hint travels as its raw template so
the example date is written by the locale's own formatter, and an app
that rebinds `T` to `rastrillo.T(r, key)` — which ignores arguments —
would otherwise print "{example}" at a person (as built, 2026-08-29).
The quick picks shipped as seven, not eight: `date_quick_next_week`
replaced `date_quick_next_monday` and `date_quick_week`, and
`field-time` shares `date_pick` with the calendar fields, so a time
input's picker button reads "Open the calendar" (as built,
2026-08-29).

### 4.3 Testing the parser

Tito Go's Node harness comes across: `ui/datetime_node.mjs` loads the
parser without a DOM and `ui/datetime_test.go` runs it under `node`
(skipped, loudly, when `node` is absent — the browser rig has the same
rule). Fixtures are one TOML table per shipped locale, each ≥20 cases
("tomorrow 9am", "next fri", "in 2 weeks", "25 dec 6pm", a bare year,
an unparsable string that must yield nothing) with the catalog's own
vocabulary. Twelve fixture files; a locale without one fails the gate.
The `en` fixtures include Tito Go's regression cases verbatim.

The fixtures shipped as JSON, not TOML: one
`ui/testdata/datetime/<locale>.json` per shipped locale, read by a Node
harness that has `JSON.parse` and no TOML parser, plus a thirteenth
file, `regressions.json`, which is one flat list of cross-locale
regressions with each case naming its own `lang` (as built,
2026-08-29).

### 4.4 `form` kinds

```go
const (
	Date     Kind = "date"       // 2006-01-02
	Time     Kind = "time"       // 15:04
	DateTime Kind = "datetime"   // 2006-01-02T15:04
)

type Field struct {
	// …existing…
	Location *time.Location // Date/DateTime: parse in this zone; nil = time.UTC
}

func (p *Parsed) Date(name string) time.Time      // zero when empty or invalid
func (p *Parsed) Time(name string) (h, m int, ok bool)
func (p *Parsed) DateTime(name string) time.Time
```

Parsing is `time.ParseInLocation` on the wire format, nothing looser —
the browser normalises. An unparseable value re-echoes as typed and is
a field error (`rastrillo.ui.date_invalid`); an empty optional value is
the zero time. `form.Range(p, "starts", "ends")` adds an error on
`ends` when it precedes `starts` (`rastrillo.ui.date_end_before_start`),
and is a separate call because the check spans two fields and `Parse`
is one-declaration-per-field by design.

Errors are keys, not English: `Errors` gains `Key` beside `Message`,
and the partials resolve through `T`. Today's `Required` message moves
to `rastrillo.ui.field_required` on the same terms.

`Errors` kept its `map[string]string` shape — the VALUE is the key, and
the calling template resolves it, `"Error" (T (index .Errors "Starts"))`
in the generated forms — because `T` returns an unrecognised string
verbatim, which makes the wrapping safe on every field and lets a
hand-written caller's finished sentence pass straight through; the
`Required`-message change is scoped to `Date`, `Time` and `DateTime`
only, so `Text`, `Textarea` and `Money` keep their humanised English
until a later sweep routes all rendering through `T` (as built,
2026-08-29).

### 4.5 The filterable select

Already shipped: `field-select` past ten options carries
`data-rst-select`, and `select.js` mirrors a filterable ARIA combobox
onto it. Two things from Tito Go's `searchable-select.js` come across:

- Today `select.js` flattens a select's `<optgroup>`s, silently
  dropping headings the author wrote — exactly when the list is long
  enough for them to matter. Tito Go refuses to enhance such a select;
  rastrillo will instead render the listbox with a `role="group"` and
  a labelled heading per optgroup, so grouped selects are enhanced
  rather than skipped. Built as specified: each `<optgroup>` renders as
  a `role="group"` whose `aria-label` is the group's own label (the
  visible heading is `aria-hidden` furniture), loose options stay at
  the top level with no wrapper at all, and a group filtered down to
  nothing takes its heading with it — where Tito Go refused the select
  outright, rastrillo renders it (as built, 2026-08-29).
- `data-rst-select="false"` opts a large select out from the markup
  side, as `Plain` does from the Go side, for hand-written selects.

The threshold stays at ten. Its strings are already catalog keys.

## 4b. Error pages

Today a 404 is `http.NotFound`'s "404 page not found", a failed render
is `view.Fail`'s "Something went wrong." as `text/plain`, a bad request
is "Bad request.", and a panic is whatever `net/http` prints. There are
~150 `http.Error`/`http.NotFound` call sites across the framework and
the generated actions, none styled, none localised, none in the app's
shell. That is the one screen a user is guaranteed to meet on a bad
day, and it is the ugliest one in the system.

### 4b.1 The partial

`error-page` — rendered *inside the app's shell* (so the chrome, the
switcher and the account menu are still there; the user is not lost):

```
Status   int, required        404 | 403 | 422 | 500 | 503 …
Title    string, optional     default from rastrillo.ui.error_<status>_title
Body     string, optional     default from rastrillo.ui.error_<status>_body
Ref      string, optional     a short request id the user can quote
HomeHref string, optional     default "/"
```

The default copy explains, plainly, what happened and what to do —
localised through the catalog, so all twelve base languages ship it:

| Status | Title                          | Body                                                                                                        |
|--------|--------------------------------|-------------------------------------------------------------------------------------------------------------|
| 404    | We can't find that page        | The link may be out of date, or the page may have moved. Check the address, or go back to the start.        |
| 403    | You can't see this             | Your account doesn't have access here. If you think it should, ask whoever runs this site.                  |
| 422    | That didn't go through         | Something in what was sent wasn't right. Go back and try again; nothing was saved.                          |
| 500    | Something went wrong on our side | It's not you. The problem has been recorded. Try again in a moment; if it keeps happening, quote the reference below. |
| 503    | We're briefly unavailable      | The site is being updated or is busy. Try again in a minute.                                                |

Each page has exactly two actions: **Go back** (`history.back()` when
JS is present, otherwise the `Referer` when same-origin, otherwise
hidden) and **Start page** (`HomeHref`). Back renders only from a
caller-validated `BackHref` — no `history.back()` and no `Referer`
sniffing inside the partial (as built, 2026-08-28). A 500 shows `Ref` in a muted
monospace line ("Reference: k7f2q9") — the same id the server logged,
so support can find the log line from what the user quotes. Nothing
about the error itself is ever shown: no stack, no message, no path.

Keys: `error_<status>_title`, `error_<status>_body` for the five
statuses above, `error_generic_title`/`_body` for any other, `error_back`,
`error_home`, `error_ref` ("Reference: {ref}").

### 4b.2 The plumbing

- `rastrillo.Ctx` gains `ErrorPage func(w http.ResponseWriter, r *http.Request, status int, ref string)` — set by the scaffold's render helper to render `error-page` inside the layout. Nil falls back to today's text. The scaffold wires `Options.ErrorPage` only; `Ctx.ErrorPage` is the app's own to set, because the mux scaffold has no ctx factory to hang it off (as built, 2026-08-28).
- `view.Fail` mints a `ref` (6 chars, base32 of 4 random bytes), logs it beside the error, and calls `ctx.ErrorPage(w, r, 500, ref)`.
- `view.NotFound(ctx, w, r)` and `view.Forbidden(ctx, w, r)` replace the bare `http.NotFound`/`http.Error` calls in the generated actions and the auth/password/passkey packages where a `Ctx` is in reach. The generated-actions and identity-plugin adoption of the two helpers moves to PR 3; only `Fail`'s signature sweep landed with this PR (as built, 2026-08-28). The generated actions adopted both in PR 3, but the identity plugins keep their plain `http.Error`/`http.NotFound` responses: `auth`, `password` and `passkey` are configured plugins with no `Ctx` in reach, so styling their errors needs a per-plugin `ErrorPage` seam of their own — deferred, not forgotten (as built, 2026-08-29). Sites without a `Ctx` (the framework's own `/healthz`-tier routes) stay plain — they are never a user's screen.
- `rastrillo.Serve` gains panic recovery: a recovered panic logs the stack with a ref and renders the 500 page through `Options.ErrorPage` (the same function, hoisted to Options so the recovery wrapper outside any `Ctx` can reach it). Today a panic is a dropped connection.
- The scaffold's `templates/errors.html` defines `content` for `error-page` so an app can restyle it by editing a file it owns. The three shells render it at full page; the design-system page shows all five statuses in every theme.
- `Accept: application/json` requests get `{"status":404,"ref":"…"}` — the generated actions' JSON paths already exist and just gain the ref.

## 5. rastrillo.org/design-system

### 5.1 What is on it

One page per theme × locale, `index.html` for ~~`ink`~~ `day`/`en`
(v2 renamed the themes; §6-v2.2), showing:

- **Tokens** — every custom property as a swatch, with the contrast
  table rendered from the theme's header, and the type scale. As built,
  the swatches are the **light** block's values only; a sentence beside
  them says so, names `ui/contrast_test.go` as the gate holding every
  documented pair to the AA floors, and warns that a reader in dark mode
  will see the chips painted in their own scheme while the printed
  values stay the light ones — the dark set is authored by hand in the
  same theme file and isn't rendered a second time here (as built,
  2026-08-29).
- **Every partial**, rendered with sample data, in every state it has
  (tones, required, error, help, disabled, enhanced and `Plain`),
  including the five error pages. As built, coverage is a gate, not an
  eyeball check: every partial section carries a `<!-- partial: NAME
  -->` marker (built in Go and injected as `template.HTML`, since
  `html/template` strips literal HTML comments during escaping), and
  `TestEveryPartialAppearsOnThePage` fails by name if one goes missing
  — the same mechanism covers the class idioms below via `<!-- idiom:
  NAME -->` (as built, 2026-08-29).
- **Every class idiom** from ~~`styleguideSamples`~~ `ui.Styleguide()`
  (moved from a test-only var to exported package API in the same PR —
  as built, 2026-08-29) — box, list grid, dropdown, form layout, toggle
  block, modal, help, selection box, ~~and the three shells' chrome~~
  and two of the three shells' chrome: `shell-topbar` and
  `shell-sidebar` are idiom entries, `column` is not, because its shell
  has no bespoke chrome classes beyond what the full-page demo already
  shows (as built, 2026-08-29). Three idioms render as escaped source in
  a `<pre>`, not live markup, each beside a link to the page where the
  markup is real. The two shell idioms nested a second `<main>` landmark
  inside the page's own; the `modal` idiom was worse — `position: fixed;
  inset: 0` plus `body:has(.rst-backdrop) { overflow: hidden }` meant
  every index page loaded with an open modal covering the gallery, its
  Close link the sample's own `/settings`, a 404. So the modal renders
  as source with a live demo at its own URL, which is the doctrine the
  idiom teaches: a modal is its own URL, and closing it is a plain link
  back (as built, 2026-08-29).
- **The three shells** at full page, each its own URL
  (`shells/topbar.html`), shown inline in an `<iframe>` and linked.
- **The modal route** at full page, one URL per theme × locale
  (`modal.html`): a small settings screen inside the inert backdrop,
  the overlay and panel over it, and every link on it real — Close and
  the panel's Back button return to that page's own index, the panel's
  nav tabs self-link. Linked from the idiom, not embedded (as built,
  2026-08-29).
- **The date and time fields and the filterable select** live — this
  is the one place rastrillo.org serves JavaScript, and only here.
- **The rules** from PR #98 — which card is padded, screens stack
  vertically — beside the components they govern, ~~with the wrong
  version shown struck through~~. As built, each rule is an info
  `callout` quoting the rule's own words from `docs/site/templates.md`
  verbatim, placed beside the idiom it governs; no deliberately-broken
  markup is rendered on the page (as built, 2026-08-29).
- A **theme** switcher (three links) and the **language** switcher
  (§2.4's markup, links only — no cookie route on a static site), so
  the page is itself the RTL and CJK proof. As built there are three
  switchers, clustered top-right in the header, and a searchable sidebar
  besides — §6-v2.3 (as built, 2026-08-29).

~~Page prose is English; the components on it render in the selected
locale from the framework catalogs.~~ Amended by §6-v2 requirement (1):
every word on the page is translated into all twelve base locales, page
prose included, with the deliberate exception of the sample fixtures.
§6-v2.3 has the boundary (as built, 2026-08-29).

### 5.2 How it is built

`internal/designsystem` in the framework repo. `go generate ./...`
renders the whole tree into `docs/design-system/` — static HTML, the
three theme files, `tokens.css`, `select.js`, `datetime.js`,
`rastrillo.js`. No build step on the site side; the site vendors the
tree as it vendors markdown.

~~As built, the root `index.html` is not a copy of `ink/en/index.html`:
`dsgen` calls the same page renderer a second time with a different
pair of path prefixes (`Root`, to the tree root, and `Self`, to the
page's own theme/locale directory), so the two files are independently
generated and only incidentally identical.~~ Withdrawn, and with it the
claim that the tree works at any mount path. **Every URL the renderer
emits is an absolute path under `/design-system/`**, from one
`mountPath` constant in `internal/designsystem`: stylesheets, scripts,
iframe sources, the theme and language switchers, the shell demo links
and the shells' back-links. The CARLOS static edge serves a directory
index at its slash-less URL as a 200 with no redirect — `/design-system`
and `/design-system/` both return the same document — so a relative
href resolved against a different base on each, and the slash-less
visit, which is the one a person types, loaded no stylesheet and
carried a navigation pointing one directory too high. The tree is now
bound to the path the site serves it from, which is the trade. With no
depth prefixes left, the root `index.html` and `day/en/index.html` (was
`ink/en`) are the same bytes;
`TestRootIndexIsTheDefaultThemeInEnglishAtTheTreeRoot` — renamed in v2
so it names the default theme rather than one theme's name — asserts that
byte-identity outright rather than blanking hrefs to compare the rest
(as built, 2026-08-29).

```
docs/design-system/
  index.html                 day, en
  <theme>/<locale>/index.html
  <theme>/<locale>/modal.html
  <theme>/<locale>/shells/{column,topbar,sidebar}.html
  tokens.css  theme-{day,plain,signal}.css
  select.js  datetime.js  rastrillo.js  gallery.js
```

~~36 pages plus 108 shell pages; each is ~100 KB; the tree is ~15 MB and
committed.~~ ~~As built: 188 files … 4,427,216 bytes — 4.22 MiB, well
under the ~15 MB estimate.~~ As built after v2: **189 files** (36 index
pages + 36 modal demos + 108 shell pages + the root index + 8 shared
assets — `gallery.js` is the eighth), **15,356,180 bytes / 14.64 MiB**
(15,335,806 before the menu previews gained the shim).
The estimate was right after all; the previews are what closed the gap,
since every example on an index page carries two `srcdoc` documents of
its own. `TestTreeStaysUnderTheSizeGate` holds the whole rendered tree
to a 20 MB ceiling, logging the exact byte count each run (as built,
2026-08-30). Committed regardless of size; that is the price
of no-Go-on-the-website, and it is reviewable: a partial change shows as
a diff in every page that renders it, which is the point.

`dsgen` (`internal/designsystem/cmd/dsgen`) is the generator `go
generate` invokes. Before it deletes anything it refuses to run unless
its output root is an absolute path ending `docs/design-system` whose
repo root two levels up holds a `go.mod` declaring
`github.com/carlosframework/rastrillo` — a guard added after a
throwaway debug run once wrote the whole tree into
`internal/designsystem/` itself (as built, 2026-08-29).

Gate `TestDesignSystemIsCurrent` renders to memory and diffs against
the committed tree, failing with the first differing path. `go
generate` is the fix. ~~The existing docsite gates (links, anchors,
fences) are extended to the HTML pages for `/docs/` links only.~~ Not
built: `internal/docsite`'s gates read only `docs/site/`'s Markdown and
`ui.Templates()`; nothing in tasks 2–4 points them at
`docs/design-system/`'s HTML. `TestEveryPageIsAWholeLocalisedDocument`
covers the rendered pages' own structural integrity instead: one
doctype, the right `lang`/`dir`, no leaked catalog keys, and — since
the absolute-path fix — every `<link>`, `<script src>`, `<iframe src>`
and every anchor pointing into the tree checked to be an absolute path
under `/design-system/` that names a file the tree actually renders.
That last clause is the cross-page link pass PR 5 deferred, arriving
early and from the renderer's own file map rather than from a crawl;
the sample hrefs inside the partials (`/orders/AB3PX` and friends) are
content, not chrome, and are exempt by not starting with the mount
prefix (as built, 2026-08-29).
`TestNoIndexPageOpensAModalOverTheGallery` holds the modal to source:
no index page may carry a live `rst-modal-overlay` or `rst-backdrop`
element, and every one must carry the escaped source and a link to its
own `modal.html`. A plain string match is the right instrument because
escaped source cannot trip it — `html/template` writes the sample's
quotes as `&#34;` inside the `<code>`, so `class="rst-modal-overlay"`
occurs only where a browser would lay the overlay out (as built,
2026-08-29).

### 5.3 The site

`rastrillo-website`'s `hack/sync-docs.mjs` gains a second source,
`docs/design-system/` → `src/design-system/`, passthrough-copied to
`/design-system/`. `check-docs.mjs` counts it. A "Design system" link
joins the docs sidebar and the site header; `templates.md` links to it
from the partials list. Deploy is the existing ship/promote runbook.

## 6. Delivery

Five PRs, in this order, each green and reviewable alone:

1. **Base languages and the switcher.** `locales/*.toml` × 12, per-locale
   `BaseCatalogs`, the fallback level, the `--check` rule, `Dir`,
   `LocaleItems`, `POST /_locale`, `shell_*`/`locale_name` keys, error
   keys. Docs: `localization.md` loses its "nothing writes that cookie"
   caveat.
2. **Themes, shells and error pages.** Theme split, three themes,
   contrast gate, three layouts, shell classes and samples,
   `--theme`/`--shell`, vendored pin, `Dir` wired into the scaffold
   layout, logical properties pass over `tokens.css`; `error-page`,
   `Ctx.ErrorPage`, `view.NotFound`/`Forbidden`, panic recovery,
   `error_*` keys in all twelve catalogs. Docs: `templates.md`,
   `cli.md`, `getting-started.md`, `app-shape.md`.
3. **Date and time, and the select.** Partials, `form` kinds,
   `datetime.js`, Node harness and twelve fixtures, `date_*` keys in all
   twelve catalogs, optgroup and opt-out in `select.js`. Docs: `forms.md`.
4. **The design system tree.** `internal/designsystem`, `go generate`,
   the gate, `docs/design-system/`.
5. **The website.** Sync, passthrough, nav, deploy.

SKILL.md gets one line for `--theme`/`--shell`, one for the date kinds
and `form.Range`, one for the twelve base locales. Budget 18,000;
17,836 today, so §7's bullets are trimmed to pay. As built through v2,
SKILL.md is **17,422 bytes** and also carries the busy-button default,
the `<details name>` menu group with its nested-group trap, and the
design-system URL in the future tense until the site vendors the tree
(as built, 2026-08-30).

## 7. Out of scope

- A calendar-grid picker. `showPicker()` hands that to the browser,
  which does it better than a first-party grid would, in every locale.
- Themes beyond three, or a theme editor. A theme is a 90-line file;
  the page shows what one looks like.
- Translating the docs prose ~~or the design-system page copy~~. The
  design-system page copy came back in scope as §6-v2 requirement (1)
  and is translated into all twelve base locales; `docs/site/*.md` is
  still English only (as built, 2026-08-29).
- Persisting locale without JavaScript on the *static* site (there is
  no server there; links suffice).
- Writing the cookie for apps that route their own locale prefix.

## 6-v2. Second iteration (2026-08-29, Paul's review of the live page)

Requirements, verbatim in intent: (1) the language switcher moves top-right
and changes ALL text on the page, page prose included; (2) a searchable
sidebar nav with collapsible sections linking to every item; (3) the
default theme becomes generic-personable — white background, greys, an
everyday palette — renamed **day**; (4) the second theme is **plain**, a
minimal skeleton to build on; (5) the third is personality-led,
impeccable-style, modern/slick/assertive (**signal**), and every theme is
authored light+dark with a scheme switcher in the gallery; (6) semantic
elements for the main interactive idioms (`<nav>`, `<dialog>`, `<search>`);
(7) the visual bugs Paul screenshotted (field-row alignment with errors,
grow/short proportions, date-button placement, dead space); (8) every
example gets Desktop/Mobile/Code previews — desktop always renders the
desktop layout via a scaled iframe, the technique also covering modals;
full examples stay linked, in new tabs; (9) tito CSS.md principles adopted
where they fit: `light-dark()` single-declaration theming, descriptive
variable layering, semantic-first markup, alphabetised rules — custom
elements replacing the `rst-*` vocabulary are explicitly DEFERRED, a
doctrine question for another day; (10) sample links go nowhere (`#`)
instead of 404ing; (11) opening a dropdown closes other open dropdowns by
default (native `<details name>`, nested groups excepted). Theme axis
grows to colour + type + shape (radius/shadow tokens move into themes).
Plan: docs/superpowers/plans/2026-08-29-design-system-v2.md.

As built, by requirement: (6) and (11) in §6-v2.1; (3)(4)(5) in
§6-v2.2; (1) and (2) in §6-v2.3; (7) in §6-v2.4; (8) and (10) in
§6-v2.5; (9) in §6-v2.6. Two requirements arrived after this list and
have addenda of their own below: (12) the busy-button rule, and (13) the
accessibility gate.

### 6-v2.1 Semantic elements and menu exclusivity — AS BUILT (2026-08-29)

Requirements (6) and (11), as they landed.

**Elements.** `list-bar-search` renders `<search>` around its
`method="get"` form, and the form's `role="search"` is gone with it —
`<search>` carries that role, and both would announce two nested search
landmarks for one box. `tokens.css` sizes `.rst-lbar > search` and
`.rst-list > search` alongside the `.rst-search` selectors they replace,
so hand-written bare forms keep their layout. `back-nav` became a
`<nav>`; `pagination` and `seg-tabs` already were, and the shells' link
clusters already used `<nav class="rst-shell__nav">`. The modal panel
became `<dialog class="rst-modal-panel" open>`: a rendered-open,
non-modal dialog is exactly what a modal-as-a-URL already was, nothing
calls `showModal()`, nothing reaches the top layer, `::backdrop` never
paints, and the `rst-modal-overlay` div stays the scrim. `tokens.css`
gains one scoped reset (`dialog.rst-modal-panel`) undoing the UA dialog
block — absolute positioning, auto margins, `1em` padding, the Canvas
colour pair.

**Deliberate non-changes.** The `rst-lrow` grid stays divs: its columns
come from one `--rst-cols` custom property on the card, and CSS grid over
real `<table>` markup means `display: grid` on the table and its rows,
which discards the table semantics the conversion was for. A list row
(`rst-row`) stays a div for a related reason — it lives inside a
`rst-list` card the app's own page markup writes, so a `<li>` would need
a list the partial does not own. `job-status`'s `rst-job` div is not
given a live-region role here: the shim replaces that element wholesale
on every poll, and whether a replaced host announces reliably is a
behaviour question, not an element one.

**Exclusivity.** Every menu the library emits defaults to the
`<details name>` group `rst-menus` — `dropdown`, `locale-menu`,
`bulk-bar`, the row menus in the list grid, the generated filter
dropdown, and the topbar shell's account menu. The `dropdown`,
`locale-menu` and `bulk-bar` partials take a `MenuGroup` key to override
it, resolved through a new `menuGroup` template func rather than
`{{if .MenuGroup}}`: the partials accept a Go struct as well as a dict,
and a template action reading a field a struct does not have is an
Execute error, so the inline form would have 500'd every existing struct
caller (`examples/blog`'s `blog.Filter`, which it did, loudly, in that
example's own suite). A nested `rst-menu-group` MUST carry a different
name: `<details name>` exclusivity is document-wide, not sibling-scoped.
The sidebar's `rst-shell__chrome` strip and the toggle-block stay out of
the group.

**Light dismiss.** Closing an open menu on an outside click or on Escape
is beyond the native disclosure, so `rastrillo.js` does it: two
delegated capture-phase listeners on the document, no per-element
binding, so menus arriving in a polled fragment are covered. Escape
returns focus to the summary that opened the menu. Scriptless behaviour
is unchanged — toggling and the name group still work with the file
removed. The shim's size cap rose from 8KB to 12KB, matching select.js;
the arithmetic is in `TestShimIsSmall`'s comment.

**Gates.** `TestMenuExclusivityAndDropdownDismissDrive` and
`TestModalDialogPanelDrive` (`-tags browser`) drive a real engine: the
shared group closing a header dropdown when a row menu opens, a submenu
NOT closing its parent, outside-click and Escape dismissal, chrome and
tblock untouched, and `:modal` false on the panel — the assertion that
the zero-JS promise holds.

### 6-v2.2 The three themes — AS BUILT (2026-08-29)

Requirements (3), (4), (5), and the theme-axis ruling. The format and
the palettes are recorded in §1.1 and §1.3, which were rewritten rather
than annotated because v1's `ink`/`teal`/`warm` had no surviving reader.
What belongs here is what the rewrite cost.

**Shape joined the theme axis** — `--rst-radius`, `--rst-radius-sm`,
`--rst-radius-pill` and the four depth tokens moved out of `tokens.css`
into the theme files. Ruled up front, and it is what lets `plain` be
nearly square and `signal` be milled while the structure stylesheet
stays the same bytes. `TestThemesDeclareIdenticalTokenSets` is what
keeps the wider axis honest.

**`light-dark()` is a colour function.** A shadow token wraps only its
colour and writes the geometry outside the call. The first draft wrapped
the whole shadow, which parses as a custom property and is invalid the
moment it substitutes into `box-shadow` — every shadow in every theme
would have vanished in a real browser with the Go gate still green.

**The browser floor moved, and the file says so.** `light-dark()` is
Chrome 123, Safari 17.5, Firefox 120, and its failure is ungraceful: an
older engine cannot parse the function, so every declaration using it is
dropped and the app renders with no palette at all rather than a
monochrome one. `tokens.css` has a lower floor of its own — `:has()`
(Chrome 105, Safari 15.4, Firefox 121) and the `lh` unit (Chrome 109,
Safari 16.4, Firefox 120), which is why the field row's phantom label
writes a `calc()` fallback ahead of its `1lh` for the Chrome 105–108 and
Safari 15.4–16.3 window. Note that `lh` is NEWER than `:has()` on Chrome
and Safari and older only on Firefox; an early note in `tokens.css`
calling `lh` "below the floor we already set" was true of Firefox alone
and has been corrected. The combined floor is Chrome 123, Safari 17.5,
Firefox 121, and `docs/site/templates.md` prints all three numbers with
the escape hatch (a plain `:root` palette plus a `prefers-color-scheme`
block; the token names are the only contract).

**`plain`'s shadows are real, not absent.** Ruled during the pre-flight
scan: "no shadow" would have made the contrast parser non-uniform across
themes, so `plain` uses 1px hairline-style shadows and its header states
the skeleton intent. `signal`'s direction was controller-pinned rather
than rolled from a `PRODUCT.md` that does not exist; its header reads as
an expansion of the pin.

### 6-v2.3 The gallery: localised prose, three switchers, a searchable rail — AS BUILT (2026-08-29)

Requirements (1) and (2).

**Every word on the page is translated**, not only the components on it.
The renderer's own headings, leads, notes and control labels — 207 prose
keys — go through a `P` helper against a catalog of the gallery's own,
in the same twelve base locales the framework ships.
`TestEveryProseKeyIsTranslated` fails on a missing entry;
`TestNoEnglishProseReachesATranslatedPage` sweeps all 165 non-`en` pages
for English that slipped through, and it earned its keep twice: the
first version could not see strings passed as `dict` arguments, so
"Write a post", "Published" and "Draft" shipped untranslated on 99 shell
pages until `dictArguments` was added.

**Keys are the English sentence itself**, not a slug. Ruled after a
challenge: a copy-edit to the sentence fires BOTH arms of the gate — no
catalog entry for the new text, and an orphaned row for the old — which
is strictly stronger than a slug. The residual (renaming a key silently
keeps eleven stale translations) is not fixable by any scheme available
here, and is named rather than chased.

**The sample-data boundary (RULED, escalated).** Demo screens localise;
component-sample fixtures stay English. The names, routes and labels in
a component sample are stand-ins, and translating them would imply the
framework ships those words — the same class of thing as "Grace
Hopper". The shell and modal demos are the other way round: they
impersonate a real application, so their chrome speaks the reader's
language. Where one string is both — "Write a post" is the page's own
chrome on a shell demo and a fixture on an index — `proseFixtureCollisions`
exempts the key on that page kind ONLY and keeps checking it everywhere
else. Rejected alternative: widening `proseLeakFloor`, which would
exempt every short key at once. The boundary is written in three places
on purpose — the gallery's own prose under Partials in all twelve
languages, the comment above `proseFixtureCollisions` where a maintainer
meets it, and `docs/site/templates.md` for a reader who never opens
either.

**Three switchers, clustered top-right** in the page header: theme
(three links), colour scheme (System / Light / Dark), language (the
`locale-menu` dropdown, twelve entries). Theme and language keep you in
the tab you are reading; the scheme toggle is the only one that needs
JavaScript. `TestTheChromeCarriesTheThreeSwitchers` holds the cluster.

**The rail is the `sidebar` shell**, deliberately — a gallery that
documented `rst-shell-sidebar` while being built out of something else
would be advertising rather than documentation, and the mobile collapse
comes free with it. Above the nav sits a search box; typing hides the
entries that do not match and any section left empty.
`TestTheSidebarLinksEverythingOnThePageExactlyOnce` derives the nav from
the same `<!-- partial: -->` / `<!-- idiom: -->` markers the coverage
gates read, in page order, so a new partial appears in the rail with no
nav code edited — verified by a reviewer adding a probe partial. Section
titles join the filter as well, because a reader searching "shells"
means the heading.

**Gallery-only JavaScript is permitted (RULED).** The framework's
zero-JS doctrine binds the partials, not the gallery's own chrome, so
`gallery.js` exists: the scheme toggle, the nav filter, and re-writing
`data-theme` onto each preview frame. It lives beside the renderer, not
in `ui`, and no scaffold ever writes it. Same rules as its three
neighbours otherwise — first-party, dependency-free, no network, and
inert-safe: both controls are `display: none` until the file sets
`data-rst-js`, so scripts-off gets the nav and the theme's own
`color-scheme: light dark` rather than two dead controls. It is a
blocking `<script>` in `<head>` rather than a deferred one, because
applying a remembered Dark after the body has parsed is a visible flash
and revealing the toggle after the body has parsed is a control popping
into a bar the reader is already looking at.
`TestGalleryScriptLoadsBeforeTheBody` and
`TestGalleryScriptStaysInertAndFirstParty` hold both halves. Budget
10 KiB; 10,208 bytes as shipped, and the ceiling's comment says the next
feature here is a conversation rather than a bump.

### 6-v2.4 The four visual bugs — AS BUILT (2026-08-29)

Requirement (7). Three of the four had one cause: `.rst-input` and
`.rst-textarea` set `width: 100%` with padding and a border and no
`box-sizing: border-box`, so `100%` was the CONTENT box — a plain input
rendered 21px wider than its own `.rst-field`, and an enhanced date
input 47px wider than its `.rst-dtp` wrapper. `box-sizing` is declared
on the two control classes rather than globally, because `tokens.css`
ships no `*` reset on purpose: the `rst-` prefix is its whole collision
surface with an app's own CSS.

- **Row misalignment.** `.rst-field-row` bottom-aligned its fields, so
  the one carrying an error had its control lifted clear of its
  sibling's and its message dropped across the field beside it.
  `align-items: start` aligns by the label row and therefore by the
  control row. A field with no label reserves the label's line with a
  `::before` that MEASURES it (`font-size: var(--rst-fs-sm)`,
  `block-size: 1lh`) rather than restating the 1.5. Messages carry
  `contain: inline-size` so a long error cannot buy its column extra
  width at its neighbour's expense. Judged against grid-with-subgrid,
  which aligns the tracks exactly but cannot express what this row is
  for: `grid-auto-flow: column` never wraps, and `grid-auto-columns` is
  one value for every column, so `rst-grow` cannot be said at all.
- **Grow/short proportions.** `.rst-grow` was `flex: 1` — that is
  `flex: 1 1 0%`, a grown field with no basis to wrap at — and
  `.rst-input--short` was a 6.5rem content box with no floor. Now
  `flex: 1 1 12rem`, `inline-size: 8rem; max-inline-size: 100%`, and
  every field in a row gets `min-inline-size: 8rem`.
- **Date picker button.** Absolutely positioned against a wrapper that
  was 47px narrower than the control inside it. The border-box fix makes
  the two the same box; `inset-block: 0` with `margin-block: auto`
  centres the button at any control height, in either writing mode.
- **Dead space.** `.rst-form` is a flex column, where child margins
  never collapse, and its children carried `margin: var(--rst-sp-4) 0`
  — so the form spaced its children twice and added 16px at each end
  where nothing was being separated from anything. Now one mechanism:
  `gap: var(--rst-sp-5)` and `.rst-form > *:not(.rst-form__foot,
  .rst-form-foot) { margin-block: 0 }`. **Stated as a rule, not a
  list**, after a review round proved a list cannot name `.rst-callout`'s
  successor and cannot name an app's own div at all — a child the list
  forgets lands at exactly the spacing the rule exists to prevent.
  Side effect worth knowing: two fields in a `.rst-form` now sit 24px
  apart rather than 40px, which is the rhythm `.rst-form-flow` already
  documented, so the two containers finally agree.
  `docs/site/forms.md` carries all of this for readers.

`TestFieldRowGeometryHoldsUnderAnError` (`-tags browser`) is the gate:
one fixture whose direction rides the query string, run as `ltr` and
`rtl` subtests, asserting shared control tops, messages that stay off
their neighbour, equal columns under unequal errors, no control
overflowing its field, and the picker button 0.35rem inside the
control's inline end. It fires 20 assertions against the pre-task
stylesheet.

### 6-v2.5 Desktop / Mobile / Code, dead links, new tabs — AS BUILT (2026-08-29)

Requirements (8) and (10).

**Nothing renders inline on the gallery any more.** All 110 examples per
index page are a `ds-view` widget: three radios sharing a name, a
`:has()` rule swapping the panels, and one `<iframe srcdoc>` holding a
whole document — the sample, the stylesheets, and nothing else. Radios
over id/`for` to avoid 330 extra ids per page. The frame lays out at a
virtual `--ds-w` (1200px Desktop, 390px Mobile) and is scaled by
`min(1, tan(atan2(100cqw, var(--ds-w))))` inside an `@supports` guard —
`tan(atan2(a, b))` being the one way CSS divides two lengths into a
number; without the trig functions the scale stays 1 and the frame is a
1200px page clipped to the column, smaller rather than broken.

Giving each sample its own document is what settles the awkward ones:
the modal renders LIVE, overlay and all, because the overlay is fixed to
its own viewport; the two shell chrome idioms render live because a
`<main>` inside its own document is not a nested landmark; and
`.rst-form-foot`'s `position: sticky` sticks to its own form's window,
which is what it does in an app. Frame heights are measured off the
engine, not guessed, and `TestPreviewFrameHeightsFitTheirContent` is
that measuring drive, committed, so the numbers cannot rot.

**The grip.** A preview is a window on its document rather than a fit to
it — a taller sample scrolls inside its box — so `.ds-view__box` carries
`resize: vertical` and the frame takes its height back off the box
(`block-size: calc(100% / var(--ds-k))`). The first version had it the
other way round, a fixed height on the frame, which gave the grip
nothing to move: the box grew and the rendering inside it stayed the
size it was, leaving ~300px of empty box under a 255px sample. Caught in
review as a control that did nothing, and now measured — box 251 → 561,
rendering 249 → 559, the framed document's own viewport 390 → 876 — with
a drive leg that fails if the CSS is reverted.

**Dead links (10).** `deaden()` runs over the live rendering only: every
`href` that is not already a fragment or a page of this tree becomes
`#`, and every `<form>` gains `target="ds-void"` with a hidden sink
iframe appended to the `srcdoc`. The submission a real app would make is
really made, into the sink, and the preview is still on screen
afterwards — and the busy rule's existing "skip a form whose target is
not `_self`" clause means nothing spins on its way nowhere. A form's
`action` is deliberately untouched, and the **Code tab is not deadened
at all**: it keeps `/posts/1/edit` and friends, which are the hrefs
somebody copying the markup wants. `samples.go` was left alone against
the brief's letter, because rewriting the data would have deadened the
source a reader copies.

**New tabs.** `target="_blank" rel="noopener"` plus an `rst-sr-only`
"(opens in a new tab)" on every renderer-owned link that leaves the
page — the rail's four demo entries, the modal and shell idiom links,
and the button under each shell demo. The theme and language switchers
deliberately stay in the tab you are reading, and
`TestEveryDemoLinkOpensInANewTab` says which is which and why.

**Cost.** 4.95 MiB → 14.64 MiB (5,185,476 bytes at `4eaeb39`), index
page 110,703 → 374,833 bytes, under
the unchanged 20 MiB ceiling, with the arithmetic in the gate's comment:
each sample is written twice (escaped `srcdoc` plus escaped source) and
attribute escaping costs ~40% because every quote becomes six
characters. A third copy would need the ceiling to move. Measured load
is ~1.0s for 362KB, and `loading="lazy"` does almost nothing here —
107 of 110 frames are constructed at first paint, since only the three
`src=` shell frames can defer.

**One assumption that was wrong, and the drive caught it.**
`color-scheme` does propagate into an iframe — but only into a document
that has not declared one, and every preview links a theme, and every
theme declares `color-scheme: light dark`. So `gallery.js` writes
`data-theme` onto each frame's own `<html>` instead, on load and on
every toggle.

### 6-v2.6 The CSS.md adoption boundary — AS BUILT (2026-08-29)

Requirement (9), ruled at planning and unchanged by delivery.

**Adopted:** `light-dark()` single-declaration theming (§6-v2.2);
descriptive two-layer variables inside the theme files; semantic-first
markup (§6-v2.1); alphabetised declarations within a rule, which
`tokens.css` and the themes both follow.

**Deferred, explicitly:** custom elements replacing the `rst-*` class
vocabulary. That is a doctrine question — it would change what an app
writes in every template, what `Styleguide()` returns, and what the
scaffold's vendored pins mean — and it is not smuggled in under a
styling pass. `rst-` prefixed classes remain the whole collision surface
`tokens.css` claims with an app's own CSS, and the absence of any `*`
reset in the file is part of that claim.

### §6-v2 addendum (2026-08-29): the busy-button rule

(12) A button that changes something gets a loading state — disabled-in-effect
plus a spinner — by default; a button that only reveals something (disclosure,
dropdown, tab) does not. The framework's `data-busy` form opt-in becomes the
default for submit buttons, with `data-busy="false"` as the opt-out and
`data-busy-label` unchanged. The guard against double submission is the
substance of the rule; the spinner is how it tells the truth to the person
waiting. Zero-JS: unchanged submit, no busy state — idempotency stays the
app's job. Plan: Task 5c.

#### AS BUILT (2026-08-29)

**Shape.** One delegated capture-phase `submit` listener on the document,
replacing the `form[data-busy]` scan — so a form arriving inside a polled
fragment is covered the moment it lands, and the attribute survives only as
an opt-out. The form gets `aria-busy="true"` and a `rstBusy` flag; the button
the browser submitted with (`SubmitEvent.submitter` — the one clicked, or the
default one implicit submission clicked) gets `aria-busy="true"`, a
`<span class="rst-spin rst-btn__spin" aria-hidden="true">` before its label,
the optional `data-busy-label` swap, and `disabled` one tick later.
`data-busy="false"` is read on the form (skip everything) and on the button
(skip the loading state, keep the form's guard). The bfcache restore now
walks `form[aria-busy]` rather than `form[data-busy]`.

**Ordering is the whole design.** Chrome really does drop a submit button's
name/value if `disabled` is set inside the submit handler — mutation-verified,
not assumed: the drive's payload assertion goes from `action=save&note=hello`
to `note=hello` the moment the hardening is hoisted out of the deferred
callback. So the synchronous half is only what cannot reach the payload
(`aria-busy`, the spinner, a `<button>`'s text, which is never submitted), and
the deferred half is `disabled` plus an `<input type="submit">`'s value, which
IS what it submits.

**Divergences from the sketch above, and why.** Only the CLICKED button goes
busy, not every submit button in the form — the others must keep their name
and value for a Save / Save-draft pair to still be distinguishable server-side.
A form with a `target` other than `_self` is skipped: the page it is on is not
going anywhere. A submit that something downstream cancels
(`e.defaultPrevented` at the tick) is handed back, guard included — a form the
browser never sent must not sit there looking busy, and an app handler that
took the job owns the feedback. An `<input type="submit">` gets the attributes
and the label but no spinner: it is void, with nowhere to put one. An engine
without `SubmitEvent.submitter` keeps the guard and skips the loading state.

**Trap 3 is structural, not defended.** Constraint validation fails BEFORE the
submit event fires, so an invalid form never reaches this code and cannot be
left stuck busy. `formnovalidate` skips validation and really does submit,
which should look like a submission. Both are driven.

**CSS.** `.rst-spin` already carried its `prefers-reduced-motion` branch
(`animation: none; opacity: 0.5`), so nothing was added for it. New in
`tokens.css`: `.rst-btn[aria-busy="true"], .rst-btn:disabled { cursor: default }`,
`.rst-btn:disabled { opacity: 0.8 }`, and `.rst-btn__spin { align-self: center;
flex: none }`. `.rst-btn`'s own `gap` spaces the ring from the label. 0.8 rather
than a heavier dim: a disabled control is incidental under WCAG 1.4.3 and exempt
from the contrast minimum, but this one is disabled while someone is waiting on
it, so the label stays comfortably readable.

**Shadowing.** `HTMLFormElement` is `[LegacyOverrideBuiltIns]`, so a control
named `target` — a target amount, a target date; an ordinary field name —
replaces `form.target` with the input element, which is truthy and is not
`"_self"`. Reading the property rather than the attribute makes the handler bail
out BEFORE it arms the guard, so that one form silently loses the whole rule and
double-submits. Every attribute the handler reads therefore goes through
`getAttribute`, and the guard is the form's own `aria-busy` rather than an
expando a control could shadow — and, under strict mode, throw on assigning to.
Driven with `<input name="target">`, premise asserted first.

**Budget.** The shim's cap rose 12KB → 16KB, once, with the arithmetic in
`TestShimIsSmall`: 11,689 → 16,177 bytes, of which 620 are code and 3,868 are
comment. Splitting was rejected on those numbers — 620 bytes of behaviour does
not earn a third scaffolded file and a third `<script>` tag on every page.
select.js keeps its own 12KB; the shared number was a coincidence of history.

**Gates.** `TestBusyRuleIsTheDefault` (Go) pins the delegated listener, the
absence of the opt-in scan, both readings of `data-busy="false"`, and the
sync/deferred split of the payload-affecting mutations.
`TestBusyButtonDrive` (`-tags browser`) drives a real engine through nine legs:
the refused form, the form-level opt-out, the button-level opt-out (form still
guarded, proven by behaviour rather than a flag), the form with a control named
`target`, the busy state with the payload the server actually received,
re-entrancy from three directions, the back/forward-cache restore,
`prefers-reduced-motion` (computed `animation-name: none`, ring still shown),
and scripts disabled. Most of it hangs on the endpoint answering **204 No
Content**, which is defined not to navigate — that is what keeps the busy state
on the page long enough to assert against instead of racing a response. The
back leg is the exception and needs a response that really does navigate, so it
posts to a second endpoint; it asserts `pageshow`'s `persisted` flag first and
fails fatally if the browser re-fetched the page, since a leg that reads a fresh
document proves nothing about the restore. Coming back has to leave the form not
merely clean but usable, so the leg submits it again and checks the server heard
it.

**Gallery.** `form-foot` gains an idle/working pair, the second a `Raw` sample
because it is not a state the partial can be asked for, with the rule and its
limits as the two notes — four new prose keys in twelve locales.

### §6-v2 addendum (2026-08-29): the accessibility gate

(13) From the user's question on the v2 page — "and everything is WCAG
2.2 AA, right?". The honest answer needed a gate rather than an
assertion, so v2 grew one: an axe-core scan in a real browser over the
committed gallery tree, plus the two criteria axe cannot see. Landed as
Task 6b, after the previews, so it scans the final markup.

#### AS BUILT (2026-08-29)

**Engine.** axe-core 4.10.3, vendored as `ui/testdata/axe/axe.min.js`
with its sha256, provenance and MPL-2.0 note in a README beside it —
self-contained, because a test that fetches from a CDN is a test that
fails on a train. Two untagged containment tests keep it honest:
`TestVendoredAxeIsThePinnedVersion` hashes the file against the pin, and
`TestVendoredAxeIsNotAShippedAsset` plus
`TestVendoredAxeStaysOutOfTheTree` prove it reaches neither `ui`'s
exported assets nor `docs/design-system/`.

**Ruleset.** `wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa` — all five,
since 2.2 AA is cumulative. `best-practice`, `experimental` and ACT tags
are excluded: useful advice, not the standard the question asked about.
`axeExempt` is EMPTY; no rule was weakened to make anything pass.

**Four browser-tagged tests** in `internal/designsystem/a11y_test.go`:
the gallery (6 pages × 2 schemes), the preview documents (8 srcdocs × 3
themes × 2 schemes plus an RTL page — 64 scans, axe injected into each
frame's own document), reflow at 320×640, and a 30-stop keyboard walk.
The tree served is the COMMITTED one, not a fresh `Render()`, since
`TestDesignSystemIsCurrent` already proves them equal — so what CI
publishes is what CI scanned.

**Findings: 9 violations → 0**, plus 2 reflow failures and 3 unringed
keyboard stops. `document-title` on all 8 preview documents (every
`srcdoc` now carries a `<title>`, the words its frame's `title` already
had); `target-size` on the filter chip's ✕ at 17×17px (now 24×24, the
chip measuring 26.0px against 22.4 before); `.rst-seg-tabs` overflowing
a 320px viewport (now wraps); `frame-title-unique`, reported as
incomplete only because the index is scanned with `iframes:false` but
real — 110 frames, 46 distinct titles — fixed by constructing titles
from existing prose keys, so no new string needed eleven translations.
Two of the three missing focus rings were a measurement bug in the
walk; the third is the browser's own scrollable-region stop on an
iframe, which no author CSS can paint (probed and proven), and is the
single entry in `focusRingExempt`.

**Two things about driving axe, both written into the test's comments.**
A clean first run is a lie until you prove the engine ran: axe's
cross-frame `postMessage` path went quiet on a page holding 110 frames
and returned an EMPTY result, which reads exactly like a pass, so
`scan()` returns the count of rules that completed and the floor is
`axeFloor = 5` (measured minima: 30 on an index, 15 on the modal, 13 on
a shell, 6 on the smallest preview). And an engine injected with
`AddScriptToEvaluateOnNewDocument` binds to the document that existed
then, so a lazy frame that reloaded produced phantom violations; axe is
now injected into each frame after its document has settled.

**A review round found the gate gating nothing.** The tests were in no
CI job while `docs/site/templates.md` claimed "every CI run".
`./internal/designsystem/` is now in the `browser-tagged tests` job with
`-p 1`, and that step's comment says removing it means editing the
templates.md paragraph in the same commit. The same round found the
focus-ring detector green-lighting an invisible ring (`outline-offset`
counted alone, and `outline-color: transparent` counted as a ring) — it
scored 29/30 on a page with every indicator removed. Every property is
now normalised to present-and-visible before comparison, and both
mutation shapes fail.

**Honest scope, and it is in the docs.** Automated scanning reaches
roughly half the WCAG success criteria; the other half is read by a
person. It is a sample: 6 pages of 180 and 8 previews of 110, chosen for
what they would catch rather than for coverage, each choice argued in
`a11yTargets()`. `heading-order` was found and fixed with the ruleset
temporarily widened, but it is tagged `best-practice`, so nothing gates
its regression — stated rather than quietly ignored. `color-contrast`
comes back INCOMPLETE on the modal's translucent overlay in both
schemes; `ui/contrast_test.go` holds every documented token pair, so the
gap is one composited overlay nobody has measured by hand.

---

## 6-v2.1. Third iteration (2026-08-30, Paul's review of the v2 page)

v2 shipped and Paul reviewed it live. This section records what he asked
for, and the three slices it was split into. **v2.1 is this section.**
The markup migration (§6-v3) and the palette generator (§6-v2.2) are
sequenced after it, deliberately: the repo-wide markup change should not
ride along with visible bug fixes.

### The bugs, with the causes found before planning

1. **A dropdown is clipped by the card it opens inside.** `.rst-list`
   sets `overflow: hidden` (tokens.css:255) to clip its rows' corners,
   which makes it a clipping context for the bulk bar's absolutely
   positioned Actions menu. Fix by rounding the first and last rows
   instead and dropping the `overflow`. Any card that holds a menu has
   this shape, so the fix is the rule, not the one instance.
2. **`<hr>` renders as a thick UA inset rule in the topbar account
   menu.** The style exists but is scoped to `.rst-row-menu__panel hr`
   (tokens.css:651); the account menu is `.rst-dropdown__menu`. Widen
   the selector to every menu surface.
3. **The sidebar nav discloses with text glyphs** (▸ ▾) where the rest
   of the system uses Lucide. `chevron-down` is already vendored — the
   set is eleven icons with `IconSlugs()` — so this is a swap.
4. **The sidebar shell puts the person mid-rail.** The profile belongs
   bottom-left, with the language switcher as a **dropup** above it.
   Zero JavaScript: a `<details>` menu positioned `bottom: 100%`.

### The page split

One 371 KB document carrying 110 iframes is the reason the page feels
slow; it is not the Eleventy build, which only passthrough-copies the
Go-generated tree. Split the gallery into pages, each with its own URL
and its own place in the nav:

**Overview** (the new landing page) · **Tokens** · **Components**
(renamed from Partials) · **UI primitives** (renamed from Class idioms)
· **Shells** · **Icons** (new) · **Getting started** (new, under
Overview).

The rename is to the READER-FACING label only. `ui.Templates()` keeps
returning partials and `docs/site/templates.md` keeps calling them
partials, because that is what they are in Go and what an app author
types. A page that calls them Components while the code calls them
partials would be a worse lie than the one it fixes — so the Components
page says, once, that these are the framework's template partials.

### Overview's content

Paul's paragraph, verbatim, is the page's opening:

> The Rastrillo design system aims to be a starter framework for any app
> to get a consistent, polished, accessible UI with no or minimal
> JavaScript dependence, available in multiple languages, and using
> clean, modern HTML and CSS. It's designed to be delightful to use with
> or without LLM assistance, and easily remixable.

Above or beside it, an **"everything" demo** in an iframe: one
self-contained page with dashboard, list and detail as clickable
sections, so a first-time reader sees what an app looks like before they
see a single token. One page with internal sections rather than a
multi-page app (Paul's choice), zero JavaScript, openable full-page.

### Getting started

A page showing how the CSS and JS are structured, what each file is for,
and **what it weighs** — measured at render, never typed:

| file | bytes today |
|---|---|
| `tokens.css` | 64,838 |
| `themes/<theme>.css` | ~10,000 |
| `rastrillo.js` | 16,378 |
| `select.js` | 10,799 |
| `datetime.js` | 59,077 |

It offers an independent download for someone not using the framework,
and says plainly that new rastrillo apps get all of this by default.

### The column

`--rst-page` is capped at 52rem (832px), not the 800 Paul estimated.
Widen to 64rem. The desktop preview scale factor is measured in gates;
they move with it.

### Sequenced after this section

- **§6-v2.2 — the palette generator.** Not a shuffle: a mood
  (calm/warm/technical/editorial) produces a palette BY CONSTRUCTION
  inside the AA contrast constraints, in OKLCH, rejecting what fails.
  Every generated palette must pass the same 26-pair × 2-scheme gate the
  three shipped themes pass, or the system's accessibility claim becomes
  a claim about three files rather than about the system.
- **§6-v3 — the markup migration.** Attributes for kind, classes for
  modifiers (`<div rst-list>`, `<button rst-btn="primary">`). Fable was
  asked for the design; its recommendation lands in
  `.superpowers/sdd/2026-08-29-design-system-v2/fable-markup-recommendation.md`.
  Not started until Paul has read it.

---

## 6-v3. The markup migration (2026-08-30) — RULED, not yet started

Fable's design recommendation, with the migration counts and a worked
side-by-side, is at
`.superpowers/sdd/2026-08-29-design-system-v2/fable-markup-recommendation.md`.
Read it before planning this. The grammar it recommends, and which is
hereby ratified:

- **Kind is an attribute.** `<div rst-list>`, `<details rst-dropdown>`.
- **Variant is the attribute's value, matched with `~=`.** `<button
  rst-btn="primary">`, and `~=` gives class-like space-separated tokens,
  so `rst-btn="primary compact"` composes with no new mechanism.
- **The value slot NEVER names a part.** `rst-dropdown="summary"` would
  make identical syntax mean "variant" on one attribute and "part-of" on
  another in the same tag. Parts are styled structurally
  (`[rst-dropdown] > summary`) or get a flat attribute
  (`rst-dropdown-menu`). Note that `rst-dropdown__summary` has no rule
  at all today, so it is deleted rather than renamed.
- **`class` is for utilities and the app's own CSS** — `rst-sr-only`,
  `rst-mono`, `rst-grow`.
- **`data-tone` becomes `rst-tone`**, which also unifies it with the
  four `rst-badge--*` classes that spell the same four tones a second
  way today. `data-*` stays for runtime state (`data-busy`,
  `data-theme`).
- **One spelling, not two.** No permanent class fallback. This
  vocabulary is taught to LLMs through `SKILL.md`; two spellings means
  every example picks one anyway, and what comes back is a mix.
- **No `<rst-list>` custom elements.** §7's deferral stands, and Fable
  independently agrees: the library's best pieces are native `details`,
  `summary` and `search` elements, which attributes annotate and custom
  elements would have to wrap or replace.

### RULED 2026-08-30 by Paul: bare attributes, not `data-rst-*`

> I think bare `rst-list` is fine. Later we could add tooling to prefix
> with `data-` if we wanted to.

Fable named this the one unrevisitable decision, on the grounds that
generated apps carry the attribute forever. Paul's answer holds, and the
reasoning is recorded here so nobody relitigates it: the escape hatch is
the same staged mechanism Fable already proposed for the class→attribute
flip — ship a `tokens.css` matching both spellings, run a codemod, drop
the old selectors. A mechanism that works once works twice. The decision
costs one extra staged release to reverse, not permanence.

### The risk that is real

`tokens.css` is written into each app's `static/` at scaffold time and
frozen there, while partials upgrade with the module. A straight flip
breaks apps mid-upgrade. The staged path is therefore mandatory, not
optional: ratify the grammar → ship a release whose `tokens.css` pairs
both selectors (non-breaking) → flip the partials atomically and
regenerate the gallery (the breaking release, whose notes mandate a
`tokens.css` refresh) → drop the class selectors before 1.0.

### Measured migration surface

~585 live `class="rst-` instances: partials 127, layouts 20,
`styleguide.go` 76, scaffold 17, `internal/designsystem` 82, examples
74, `docs/site` 16, `ui` test literals ~170. Plus 451 selector
occurrences of 145 distinct classes in `tokens.css`, 83 `rst-`
references across the three JS files, and 12,444 instances in the
189-file gallery, which regenerate for free. Every `--rst-*` custom
property is untouched.

The teaching surface — `SKILL.md` (17,602/18,000), `docs/site`, the
styleguide, the scaffold and the gates — must flip in the same commit as
the partials, or an LLM reading a half-migrated corpus emits a mix.
