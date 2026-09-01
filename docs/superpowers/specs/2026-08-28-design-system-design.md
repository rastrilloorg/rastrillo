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

**Superseded on 2026-08-30 by §6-v2.5 in the one respect that matters:
the tree is no longer committed, and `dsgen` is no longer internal.**
Everything below about what is rendered and how it is gated still holds;
`docs/design-system/` as a location, `TestDesignSystemIsCurrent`, the
`guardRoot` output-root check and `TestTreeStaysUnderTheSizeGate` do not.

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
`amadan.net/rastrillo/rastrillo` — a guard added after a
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

`.rst-page` was capped at 52rem, not the 800px Paul estimated — a
reading measure on a column that holds list grids and side-by-side
fields. Widened to 64rem.

The desktop preview's scale factor moves with it, and is measured
rather than written down: it is `min(1, 100cqw / 1200px)`, so nothing
in the CSS needed editing, and `TestPreviewWidgetDrivesTheWholeJourney`
now logs what the engine computed. Read on a 1280px window, the
1200px virtual page went from painted 768px wide (scale 0.640) to
960px (scale 0.800). `previewHeights` did NOT move, and was
re-measured to confirm it: every frame lays out at the virtual 1200px
whatever the column is, so the table is independent of this change.

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

---

## 6-v2.2. Remixability (2026-08-30) — RULED, not yet started

Two changes with one motive: the system currently prescribes where it
should offer. Paul's observation is the frame for both — *"Every
Rastrillo app so far built has had it"* — and neither change is worth
making if the result is a different single answer imposed just as hard.

### The rake line is retired

`.rst-page-header::after` draws a 2.5rem accent stroke over the header's
1px rule, flush to the inline start. It is the library's one flourish
and the reason every app looks like a rastrillo app. It also reads as a
determinate progress bar at 12%, and not by resemblance: it is built
from the same parts in the same order — a thin full-width track in the
line colour with a shorter saturated segment filling it from the leading
edge. The same markup would serve both.

**RULED 2026-08-30 by Paul: the tinted hairline.** The header rule moves
onto the theme axis as one derived token, and the `::after` goes away:

```css
/* tokens.css — structure, identical on every theme */
.rst-page-header { border-bottom: 1px solid var(--rst-header-rule); }
.rst-page-header::after { content: none; }

/* themes/day.css */
--rst-header-rule: color-mix(in oklab, var(--rst-accent) 18%, var(--rst-line));
/* themes/plain.css */
--rst-header-rule: var(--rst-line);
/* themes/signal.css */
--rst-header-rule: color-mix(in oklab, var(--rst-accent) 45%, var(--rst-line));
```

Derived rather than authored, so a theme that changes its accent gets a
matching rule with no second value to keep in step — that drift is
exactly what a fourth hand-authored colour per theme per scheme would
produce.

**The rule is decorative and carries no contrast floor.** The heading's
size, weight and spacing carry the structure; the rule does not. Do not
add it to the 26-pair contrast gate, and do not let a later pass argue
it back up to meet a 3:1 floor it was never subject to. This sentence
exists so that argument has already been answered.

An app that wants its own sets one custom property, instead of fighting
an `::after` it cannot remove. That is the whole point of the change.

### The palette generator

**RULED 2026-08-30 by Paul: a mood-driven generator constrained BY the
contrast gate, not a shuffle.** A mood (calm, warm, technical,
editorial) produces a palette *by construction* in OKLCH — lightness
ladders derived so the documented pairs clear their floors — and
anything that fails is rejected rather than shipped with a warning.

The floor to hold is the one the three shipped themes hold: 26 pairs ×
2 schemes, 4.5:1 for text and 3:1 for control borders. A generator whose
output is not held to it turns the system's accessibility claim into a
claim about three files rather than about the system, which is worse
than not generating at all.

Both changes ship after v2.1.

---

## 6-v2.1b. Menus that fit the viewport (2026-08-30) — RULED, not yet started

Paul: *"dropdowns should position themselves correctly no matter where
they are on the screen ... a dropdown at the bottom right should
position correctly to fit the viewport, bottom left etc. or an inline
dropdown where there's not enough scroll room to show should drop up
instead. If there's not enough viewport, the dropdown should scroll
rather than overflow."*

Three behaviours, and they do not cost the same.

### 1. Scroll rather than overflow — a plain bug, fix it everywhere

`.rst-combo__list` and `.rst-dtp__list` already carry `max-block-size`
plus `overflow-y: auto`. `.rst-dropdown__menu` and
`.rst-row-menu__panel` carry **neither** — no cap, no scroll. The
twelve-locale language menu measures 388px, so on a short viewport its
last entries are unreachable. Cap the menu surfaces against the space
actually available and let them scroll. No new technology, no script,
every browser.

### 2. Flip — CSS anchor positioning, Chromium-first

**RULED 2026-08-30 by Paul.** `position-try-fallbacks: flip-block,
flip-inline` (with `position-area`) does exactly what he described,
including the inline flip near the trailing edge, with zero script.

It is Chromium-only today. That is accepted **with the reason recorded
so nobody treats it as an oversight**: unsupported browsers fall back to
today's fixed position, which is what they already do, so nothing
regresses; and when Firefox and Safari ship it the behaviour arrives
with no code change. The alternative — script — costs a shim split
(`rastrillo.js` has 6 bytes), adds resize and scroll listeners to every
page carrying a menu, and puts positioning *behind* JavaScript, so with
script off it would be worse than the fixed position we have now. That
trade is the wrong way round for a framework whose doctrine is that the
scriptless path is the real one.

Gate it honestly: the drive must assert the flip **where the engine
supports it**, and must not silently pass by finding no support. A
capability probe that fails when the probe itself stops working is the
shape to use — this branch has shipped four gates that gated nothing.

### 3. Top layer — NOT chosen, recorded so it is not re-proposed as new

`popover` + anchor positioning would put menus in the top layer, where
no ancestor can clip them (the `.rst-list` `overflow: hidden` bug this
morning was exactly that class), and would bring native light dismiss,
Escape and one-at-a-time — deleting shim code rather than adding it.

Not chosen now because it replaces `<details>` as the baseline, and a
browser without `popover` leaves the button inert, where `<details>`
always works. It remains the most interesting long-term shape, and it is
the natural companion to §6-v3's markup migration. Revisit it there,
deliberately, not by accident.

### 4. The scrollbar must not move the layout — RULED 2026-08-30 by Paul

Paul: *"clicking between sections causes the page to jump depending on
whether the scroll bar is present or not. Should we default to having it
so that scrollbar doesn't shift the layout, with an opt-out?"* — yes,
defaulted, with a token opt-out.

```css
:root { --rst-scrollbar-gutter: stable; }
html  { scrollbar-gutter: var(--rst-scrollbar-gutter); }
```

A token rather than a class, for three reasons: the opt-out is one line
in the app's own stylesheet (`--rst-scrollbar-gutter: auto`), it layers
like every other token, and custom properties are untouched by §6-v3's
markup migration, so this survives it without edit.

**It fixes a second instance of the same bug.** `tokens.css:968` is
`body:has(.rst-backdrop) { overflow: hidden }` — the modal's scroll
lock. That removes the scrollbar the moment a modal opens, shifting the
whole page sideways while the reader watches. One declaration fixes both.

**The cost, chosen knowingly:** a page too short to scroll now reserves
the gutter as well, so there is a thin empty strip at the trailing edge
where there was none. That is what never shifting costs. `both-edges`
was considered and not taken — it doubles the strip to keep centred
content exactly centred, and this system's pages are already a
max-width column inside a larger ground, so the asymmetry is not visible
where it would matter.

Support: Chrome 94+, Firefox 97+, Safari 18.2+; older engines ignore the
declaration and land on today's behaviour — the same degradation shape
as the anchor-positioning ruling above, and for the same reason.

**Do not over-claim what this fixes.** macOS overlay scrollbars take no
layout space, so on a default Mac there is nothing to shift. This is
real on Windows and Linux, on macOS with "always show scrollbars", and
inside the gallery's own preview iframes. Say that in the docs rather
than implying it explains every jump anyone has seen.

### 5. The optional flip helper — RULED 2026-08-30 by Paul, with amendments

Paul asked for an optional script providing anchor positioning to Safari
and Firefox, opt-out for anyone staying CSS-only. Accepted, with three
amendments that all make it smaller.

**Not named "shims".** "The shim" already means `rastrillo.js` here —
`ShimJS()`, `TestShimIsSmall`, five uses inside the file, plus SKILL.md
and templates.md. A `rastrillo-shims.js` would make every existing
sentence about the shim ambiguous. The convention is one file per
capability named for the capability (`select.js`, `datetime.js`), so
this is `menufit.js`.

A category name also invites a bucket: a file called shims grows until
every app pays for bytes few of them need. One capability, one file, one
legible cost.

**Not a general polyfill.** A general anchor-positioning polyfill
reimplements a layout algorithm by parsing stylesheets — a large
dependency with real edge cases, bought to get something narrower than
what it provides. What is actually needed is: if the menu does not fit
below, put it above; if it does not fit inline, flip it. That is a short
routine over `getBoundingClientRect`, run on the `<details>` toggle. No
scroll or resize listeners — it only has to be correct at the moment of
opening.

**Self-disabling where the CSS works:**

```js
if (CSS.supports("position-try-fallbacks: flip-block")) return;
```

It therefore costs nothing in Chromium, cannot fight the CSS, and
switches itself off in Safari and Firefox the day they ship anchor
positioning — with no release from us. That property is what makes it
worth having rather than a maintenance liability.

**The opt-out is the absence of a `<script>` tag**, not a config flag.
The scaffold writes the tag; deleting one line opts out, and is
self-documenting in a way a flag is not. Its own byte budget, separate
from `rastrillo.js` (16,378 of 16,384).

The doctrine holds throughout: with no JavaScript at all, menus keep
today's fixed position, which is what every browser does today. This
file only ever moves an engine closer to the CSS we already wrote.

### 6. Clearing a search must clear the search — RULED 2026-08-30 by Paul

Paul, with a screenshot of a search field holding "sere" and a native ✕:
*"clearing search doesn't actually clear the search ... can we provide a
sensible default for what happens when the search is cleared ...
presumably returning to a URL where there's no search? Some kind of hook
anyway."*

The ✕ in that screenshot is `::-webkit-search-cancel-button`, which does
exactly what it is specified to do: it clears the input's VALUE. A form
submits on submit, so nothing else happens — the results stand and the
URL still carries `?q=sere`. The affordance looks like it worked.

**The default is a link, not script.** When `Query` is non-empty,
`list-bar-search` renders an `<a>` to the same `Action` carrying the
`Hidden` pairs with `q` omitted. A real navigation: it works with
JavaScript off, it is bookmarkable, and Back behaves. `ClearHref` is the
hook — an app passing it wins over the computed default.

**Suppress the native cancel button.** Leaving it beside the link is the
bug rather than a redundancy: two affordances, one of which lies.

**What clearing means, decided rather than left to fall out:**

- Filters and sort survive. They ride in `Hidden`; the default carries
  them and omits only `q`. "Clear the search" is not "reset the screen".
- **Pagination is the case the framework cannot decide.** `Hidden` is
  opaque name/value pairs — nothing tells the partial which one is the
  page. A page number from a filtered result set is meaningless once the
  filter is gone, but the partial cannot know to drop it, so the default
  carries everything and `ClearHref` is how an app says "and reset to
  page 1". Document that on the key, because otherwise every app meets
  it once, in production.

**Two constraints on the implementation:**

- The clear target must be **at least 24×24 CSS px**. WCAG 2.2 AA target
  size is gated in CI and already caught a 17px close chip on this
  branch; an ✕ tucked inside a field is exactly that shape.
- It needs a new catalog key for its accessible name — twelve locales,
  and a trip through the copy gate with the rest of the new prose.

### 7. The rail overflows the viewport by its own padding — REGRESSION, 2026-08-30 — AS BUILT (2026-08-30)

Paul, with the live sidebar shell: *"Sidebar shell left nav is going off
the edge of the viewport"* — the person at the rail's foot is clipped.

Shipped this morning with the rail-foot change. `tokens.css:1133` gives
the rail `padding: var(--rst-sp-4)` (1rem); `:1163` gives it
`block-size: 100dvh`; and box-sizing is **deliberately not global** in
this file (`:787` records why — it is set per component rather than in a
`*` reset). So the rail is content-box: its border box is 100dvh + 32px,
`position: sticky` at `inset-block-start: 0`, and the last 32px hang
below the window. `overflow-y: auto` cannot help — the content fits the
content box, so no scrollbar appears; the box is simply taller than the
viewport.

**Why the gate passed, which is the part worth keeping.** The drive
asserts the person sits at the foot OF THE RAIL, and measured the rail
at **932px in a 900px viewport**. That number is in the passing test's
own output: 932 = 900 + 2×16. The assertion was true and the frame was
wrong. Re-frame it against the viewport — the rail's border box must not
exceed it — and the same drive would have failed on the day it landed.

Fix per this file's convention: `box-sizing: border-box` on the rail
itself, beside the components that already declare it, not a `*` reset.

### 8. The collapsed rail needs a menu icon — RULED 2026-08-30 by Paul — AS BUILT (2026-08-30)

*"The sidebar shell should also provide a kebab / hamburger icon beside
'Menu'."* — the `<summary>` of the disclosed rail below 800px, which is
text-only today.

**Vendor Lucide `menu`, do not reuse `kebab`.** The set is eleven slugs
and has no hamburger. `kebab` means "more actions on this row"
everywhere else in this system, and spending it on navigation blurs a
distinction the vocabulary currently keeps. Add `menu` to `icons.go` and
`IconSlugs()`; the Icons page (§6-v2.1 Task 5) derives from `IconSlugs()`
rather than a literal list, so it picks the new icon up with no edit.

`aria-hidden`, since it sits beside a visible text label.

### 9. The topbar has no narrow layout — RULED 2026-08-30 by Paul — AS BUILT (2026-08-30)

*"The topbar shell should compact the menu and the dropdowns into a
single dropdown, or some kind of overlay."*

`.rst-shell__bar` (tokens.css:1112) is `display: flex; flex-wrap: wrap`
and `.rst-shell__account` (:1118) carries `margin-inline-start: auto`.
There is **no collapse breakpoint for the topbar at all** — so as the
window narrows, the account and locale menus wrap onto a second row and
the auto margin shoves them to the trailing edge, which is the state in
Paul's screenshot. Nothing is broken; nothing was ever written.

**Use the sidebar's mechanism, not a second one.** Below the same
breakpoint the sidebar already uses (800px), the topbar's tail — nav,
account, locale — collapses into ONE `<details>` disclosure behind the
Lucide `menu` icon added in §8. Above it, CSS hides the `<summary>` and
the tail lays out inline exactly as today, which is the same shape
`.rst-shell__chrome` already uses in reverse. Two shells, one collapse
idiom, one icon: that is the thing a design system is supposed to
demonstrate.

**The trap, named before anyone meets it.** The account menu is
`<details class="rst-dropdown" name="rst-menus">`. Nesting it inside an
outer `<details name="rst-menus">` means opening the account **closes
its own parent** — the exclusivity that makes sibling menus behave is
the thing that breaks nested ones. This system already documents that
rule for `rst-menu-group` ("a nested group MUST name a different one or
it closes its parent"); this is the first place in the framework's own
shells where it applies. The outer disclosure takes a different group
name, and the inner menus become nested groups.

Zero JavaScript throughout: it is `<details>`, a media query and a pair
of display rules.

**Block names must survive.** Apps override `nav`, `account` and
`locale`. Wrapping them in a disclosure is a layout change, not a
contract change — the block names stay exactly as they are, or every
app that overrides one breaks silently on upgrade.

### 10. Prev/next at the foot of every page — RULED 2026-08-30 by Paul

*"since we're splitting across pages now, we need a 'Prev' and 'Next'
navigation at the bottom of each page."*

The split gave the gallery five pages and three ways between them: the
rail, the tab strip, and nothing at the bottom. A reader who has just
finished a page has to go back up to leave it, and below 800px the rail
folds, so the tab strip is carrying navigation alone.

A pair of links at the foot of every page, in `pageKinds()` order:
previous on the inline start, next on the inline end, each naming the
page it goes to rather than saying "Prev" and "Next" alone — the name is
what tells a reader whether they want it. Overview has no previous and
Shells has no next; the missing side leaves its space rather than
shifting the other one across.

Derived from `pageKinds()`, never a literal list, so a sixth page kind
joins the sequence with no edit — and **gate that**, the way the tab
strip and the chrome gates now do, because this is the third navigation
surface in the same file and the first two both shipped ungated.

Both link labels are prose keys: two new English strings, eleven
translations each.

### 11. Overview shipped blank — process note, 2026-08-30

Task 3 built Overview as a deliberate stub: the page exists and is
reachable, its content belongs to Task 4. That was a reasonable way to
split the work and an unreasonable thing to deploy. It was written in
the pull request and shipped anyway, and Paul found a blank page at the
address every visitor lands on first.

The rule this earns: **a stub may be merged, but the deploy that carries
it needs either its content or a placeholder that reads as deliberate.**
An empty `<h1>Overview</h1>` reads as a broken build, not as work in
progress. Nothing about the gates would have caught it — every gate
passed, because a page that renders its own heading and nothing else is
structurally perfect and substantively empty.

### 12. Every nav section needs its own overview link — RULED 2026-08-30 by Paul

*"each major section now needs an overview too as the first clickable
item in each nav section."*

The split left a gap in the rail: expanding TOKENS shows nine anchors
and **no route to `tokens.html` itself**. The section title is a
`<summary>`, so it discloses rather than navigates, and the only ways to
a section's top are the tab strip and the prev/next links of §10, which
do not exist yet.

First item under every section, before its anchors: a link to that
page's top, labelled **Overview** — Paul's word, kept.

**The name collides on purpose, so disambiguate it in the accessible
name.** The top-level page is also Overview, so the rail reads Overview,
then TOKENS → Overview, then COMPONENTS → Overview. Nesting makes that
unambiguous to the eye and ambiguous to a screen reader, which hears
"Overview" five times in one navigation landmark.

Give each link an accessible name carrying its section — "Tokens
overview" — via a `{section} overview` prose key. That satisfies WCAG
2.5.3 Label in Name, which requires the accessible name to *contain* the
visible label, and it does. Do not change the visible word to
disambiguate: the visible label is the one Paul asked for and the one
the filter matches on.

Derived from `pageKinds()` like §10's prev/next, and gated the same way:
first item in its section, exactly one per section, present for a sixth
page kind with no edit.

Two prose keys — the visible label and the accessible-name pattern —
eleven translations each.

### §7–§9 as built (2026-08-30)

Built as ruled, with one deviation in §9 and one addition to all three.

**§7.** `box-sizing: border-box` on `.rst-shell-sidebar >
.rst-shell__rail`, beside the components that already declare it. The
gate is re-framed: `railReading` now carries `Viewport`, `RailOverhang`
and `PersonOverhang`, and the drive asserts the rail's border box does
not exceed the window, that the sticky rail's bottom edge is not below
it, and that the person at its foot is not off-screen. Mutation-verified
— restored to `content-box`, the old frame passes reporting "rail 932px
in a 900px viewport" and the new frame fails on all three assertions
with the same numbers. A third leg drives 1280×420, where the honest
claim is weaker and is stated as such: the rail must fit the window, and
anything that does not fit inside the rail must be reachable by
scrolling it.

That third leg was reviewed and found vacuous, and is fixed (see the fix
round below). It ran on a two-link fixture that fits 420px, so the
overflow case never arose; and it read `scrollHeight - clientHeight`,
which is overflow and not scrollability — `overflow: visible` reports it
too. Removing `overflow-y: auto` from the rail passed all three legs
green. It now runs on a twenty-link page of its own, treats the overflow
reading as the PREMISE and fails if the fixture stops overflowing, and
proves the claim by round trip: what the engine computed for
`overflow-y`, where `scrollTop` actually landed when the box was asked
to go to its own bottom, and whether the person came into view when it
did.

**§8.** Lucide `menu` as a twelfth slug in `icons.go`, `IconSlugs()` and
both scaffoldable sets (`internal/iconsets`; Font Awesome's is `bars`).
`kebab` untouched, and `TestMenuAndKebabAreDifferentGlyphs` fails if a
later edit points one at the other. Both shells' collapse summaries
carry it, aria-hidden beside their own visible label; so does the
gallery's own chrome strip.

**§9, and the deviation.** The tail is an adjacent SIBLING of the
disclosure — `.rst-shell__menu[open] + .rst-shell__tail` — rather than
its content. The ruling described the tail as the disclosure's content;
that shape cannot produce the wide layout, because a closed `<details>`
hides its own content and no rule reliably un-hides it across engines
(`::details-content` is too new to be the floor a shell stands on). The
sibling shape is the one `.rst-shell__chrome` already uses, which is
what the ruling pointed at, and above 800px `display: contents` on the
tail promotes nav, account and locale back to direct flex items of the
bar, so the wide layout is unchanged down to the auto margin.

The trap survives the deviation and is worth restating for that reason:
`<details name>` exclusivity is **document-wide, not sibling-scoped**,
so keeping the account menu out of the disclosure's subtree buys nothing
on its own. The disclosure takes `name="rst-shell-menu"`. Two gates:
`TestEveryMenuDefaultsToTheSharedExclusivityGroup` reads the name off
the layout, and the browser drive opens the account menu inside the
collapsed tail and asserts the disclosure is still open. Both were
mutation-verified against `name="rst-menus"`.

**Common to all three: the shells are now driven against viewports.**
`TestTheTopbarCollapsesItsTailBehindOneDisclosure` drives 1280, 800
(the breakpoint matches at its own width), 390 and 320 with no script on
the page, and `TestA11yScansTheShellsCollapsed` scans both shells at
390px with the disclosure open, in light and dark, and measures the
summary against WCAG 2.2 SC 2.5.8's 24×24 — a target size axe's default
tag set does not check. That gap, not the three bugs, was the finding:
every scan in the tree ran at a width where neither collapse control
exists.

### §10–§12 as built (2026-08-30)

Built as ruled. Overview opens with Paul's paragraph, word for word,
translated into the other eleven; under it, a route into each of the
other pages read off `pageKinds()`, each row carrying its own one-line
`Blurb`. Adding a page kind is still one row — the row is one field
longer.

The prev/next pair is a two-column grid rather than a flex row with
`space-between`, because the ends of the sequence are missing a link
and not missing a column: Overview's Next has to stay on the inline end
rather than sliding across to where Previous would have been.

The section overview link is the first item of every rail section that
*discloses*. A section with nothing anchored on it yet already renders
as a plain link to its own page — the Overview is the only one — so it
does not get a second route under itself, and Demos, which is not a
page of this gallery, does not get one at all. The visible label is
Paul's word on every section and the accessible name carries the
section (`aria-label="Tokens overview"`); the gate asserts the
containment SC 2.5.3 asks for rather than trusting the prose table to
keep the shape.

All three surfaces are gated, and all three gates were mutation-verified
in both directions — the link pinned to a fixed page, and the surface
removed outright — which is what §10 asked for after the first two
navigation surfaces in the same file shipped ungated. The gates walk
`pageKinds()`, so a sixth page kind is expected on all three the day its
row lands.

### The demo application as built (2026-08-30)

`<theme>/<locale>/demo.html`, 36 copies, built in the sidebar shell —
named rather than derived, because a demo has to pick one and the
sidebar is the richest of the three. It is framed at the top of the
Overview under Paul's paragraph, linked full-page from there and from
the rail's Demos section on every page.

Three views in one document, as Paul chose: a dashboard, the request
list, and one request open. The switching is `:target` — each view has
an address of its own, so the back button walks them and a reader can
copy a link to the detail view out of the frame. That is the
URL-per-view idiom in the only currency a static file has, and the
demo's own callout says so rather than implying a route it does not
have.

The rules are written so the DASHBOARD is what hides, not what shows:
`#view-requests` and `#view-request` are hidden until targeted, and the
dashboard is hidden only when one of them is. Where `:has()` is missing
the last rule drops and the page degrades to every view stacked and
readable, rather than to a blank screen. The rail's current item is the
same trick, and it is honest about being visual only: a real app renders
each view at its own route and puts `aria-current` on the link
server-side.

The framework's three scripts load, because a real app gets them, and
none of them does any of the switching.
`TestTheDemoApplicationSwitchesViewsWithNoScript` drives the whole
journey — land, follow the rail to the list, follow a row into the
record, follow the back link out — with script execution disabled in the
engine, and asserts exactly one view is painted at every stop.
Mutation-verified by deleting the rule that hides the dashboard: the
drive then reports two views painted at three of the four stops.

The app's own words are prose keys and translated; its records — the
names, the subjects, the dates, the queue, the brand — stay English on
every copy, which is the boundary `templateFixtures` and
`proseFixtureCollisions` already draw for the shell and modal demos.

### Fix round 1 (2026-08-30)

Six findings from the review of both batches on this branch.

**The topbar comment argued for the bug it exists to prevent.** It
opened `name="rst-menus", and the rst-menus group is exactly what it
must not be`, three lines above an element carrying
`name="rst-shell-menu"`. The first four words asserted the value the
rest of the paragraph forbids, in the one comment a maintainer editing
that layout is standing in. Rewritten to state the attribute the element
carries, why it is not `rst-menus`, and that the paragraph is the reason
for the two gates rather than advice to change the element.

**§10's placement clause was ungated.** Two `grid-column` declarations
are the whole of "previous at the inline start, next at the inline end"
and of "the missing side leaves its space", and nothing read geometry —
deleting them and swapping them both passed the entire suite,
`-tags browser` included, because the only gate that noticed was the
tree's freshness check and the answer to that is `go generate`.
`TestThePrevNextPairSitsAtTheEndsOfItsRow` now measures: the two ends of
the sequence and a middle page, in both writing directions, against the
strip's own box. Deletion is the shape worth having it for — with both
links present, auto-placement puts them in columns 1 and 2 anyway, so
the bug is invisible until the Overview's lone Next auto-places into the
first column and lands exactly where Previous would have been.

**The demo application's grid headers were untranslated.** `Subject`,
`Status` and `Updated` were allowlisted as fixtures under a comment
whose own rule — everything the application says is a prose key — put
them on the other side of the line, so a Japanese or Arabic reader met
an English table header inside the fully localised frame that is their
first sight of the system. Three prose keys, eleven translations each.
The line is now stated where it actually runs: a ROW is data and stays
English, the HEADER over it is the application naming its own columns.
`Status` remains a fixture as well, for the shell demos' sample screen,
which writes its column headers literally.

**Two sentences made true.** `demoCSS` claimed `:has()`-less
degradation stacks "every view"; it is at most two, and with no fragment
exactly one. `internal/iconsets` listed `menu` as Font Awesome's `bars`
inside a run of Lucide divergences; `menu` is Lucide's own slug, and the
five that differ are the five `docs/site/icons.md` names.

**And one thing that was not a finding.** The topbar's `nav`, `account`
and `locale` are a level deeper than they were: `display: contents`
gives their boxes back above 800px but not their selectors, so an app
whose CSS says `.rst-shell__bar > .rst-shell__nav` stops matching at
every width. Nothing in this repo does, but apps upgrade — so it is
written down in `docs/site/templates.md` under Shells, where an upgrader
will meet it, and the layout's own comment no longer says the three
blocks "are direct children of the bar again".

---

## 0. What is shipped, what is merged, what is only ruled

**Read this before building on anything below.** Two CARLOS apps have
now written specs against this document, and one of them built on a
section that is a ruling rather than an API — because nothing here said
which was which. That is this document's fault, not theirs.

Three states, and the difference between the first two matters more than
it looks:

| State | Means | How to check |
|---|---|---|
| **RELEASED** | in the latest tag; `go get` gives it to you | `git show v0.19.0:ui/ui.go` |
| **ON MAIN** | merged, unreleased — you get it only by tracking `main` | `git show origin/main:ui/ui.go` |
| **RULED** | decided and written down. **No code exists.** | it is only in this file |

As of 2026-08-30, with **17 commits on main since v0.19.0 was tagged on
2026-08-24**:

- `ui.TokensCSS()` — **RELEASED** (v0.19.0, `ui/ui.go:192`).
- `ui.ThemeCSS(name)`, `ui.ThemeNames()`, `ui.Layout(name)`,
  `ui.LayoutNames()`, the three themes, the twelve locales, the busy
  rule, the gallery — **ON MAIN, not released**. An app that runs
  `go get` today gets a framework with none of the themes this document
  describes.
- The bare-attribute grammar of §6-v3 — **RULED**. It exists in no Go
  source and on no branch. The shipped and merged `ui` package styles
  `class="rst-*"` and nothing else. An app writing `<div rst-list>`
  today renders unstyled.
- `Pair`, the allocation entry point, `rastrillo doctor`, the semantic
  elements of §6-v2.4, the tinted header rule — **RULED**.

### Two ways to check this and get a confident wrong answer

Both were hit for real on 2026-08-30, and neither errors — they answer,
and the answer disagrees with reality. That is the failure shape worth
naming, because a tool that fails loudly teaches you something and a
tool that lies does not.

**Never cite your working tree.** A downstream app reported this
package's state from a checkout sitting on a feature branch
(`vault-client`, tip `ae9a1c0` — which is not an ancestor of `main` and
never was), and quoted a real line number from it. Everything it said
was true of the tree it was standing on. Cite `origin/main` after a
fetch, or a tag, and say which.

**`git branch -r --merged origin/main` is blind here.** This repo
squash-merges, so a merged branch is not an ancestor of `main` and the
command reports nothing for it. Reaching for `--merged` to check whether
`design-system-v2` landed returns an empty list and reads as "it never
merged", while `main` carries every line of it. Check for the content,
not the ancestry.

**The gap between RELEASED and ON MAIN is the one that will bite.**
**18 commits ahead of v0.19.0, counted at `ac287b5` on 2026-08-30** —
stamped rather than stated, because a bare number in a spec ages into a
wrong fact, and this one aged within the hour of being written. The
count moves; the gap is the point. A downstream app
reading this document and running `go get` gets neither the themes nor
the shells. That is an argument for releasing, not for footnotes.

---

## 6-v2.2b. The colour engine (2026-08-30) — designed with two downstream callers

§6-v2.2 ruled a mood-driven palette generator constrained by the contrast
gate. Two CARLOS apps then arrived needing the same machinery for a
different purpose, and their requirements changed the design before any
of it was written. Both are recorded here with attribution, because the
reasoning is what has to survive: someone will later try to simplify
each of these back, and every one of them is load-bearing.

The apps: **Sheets** (`amadan.net/carlos/sheets`) — cell fills,
conditional formatting, presence cursors, XLSX round-trip. **Docs** —
text highlights, comment-thread author colours, presence cursors,
collaborator avatars.

### Two entry points, not one

```
Pair(hue, chroma, background) -> {fill, on-fill}   // contrast-correct by construction
Allocate(keys, avoid)         -> ([]intent, separated bool)
```

Contrast-correctness and mutual distinguishability are **different
guarantees**. Merging them gives a function that means different things
depending on its arguments, which is the shape of API nobody can gate.
An allocated intent still resolves through `Pair` against whatever
background it lands on.

### The background is a literal colour, never a scheme

The first draft took `(hue, chroma, scheme)`. Docs killed it: their
canvas is a white page on a neutral ground **in every theme, including
dark**, so a highlight must be contrast-correct against paper white, not
against the theme's surface token. A scheme enum cannot express that —
it can only mean "the theme surface for this scheme".

Sheets then showed it was worse than a missing case. Their fills
frequently do not sit on the sheet surface at all: conditional
formatting paints *under* a user fill, a selected cell sits on the
selection tint, a frozen header sits on something else again. Under a
scheme parameter their own contrast gate would have asserted the wrong
pair **while passing**. Passing the literal background is therefore a
bug removed from a downstream app, not a generalisation of ours.

The sequence is recorded exactly, at Sheets' own request, because it is
the argument for review rather than for anyone's discipline: the
correction came first, from a second caller's unrelated requirement, and
the consequence was found second, when Sheets sat down to write what
their contrast gate would actually assert. Nobody caught it before
writing. A second pair of eyes moved a bug from shipped to unwritten,
which is a different and more repeatable thing than someone being
careful.

Scheme becomes the caller's business: resolved from the theme token in
the ordinary case, from paper white in Docs', from whatever a cell
actually sits on in Sheets'.

### Allocation: separation is hard, stability is best-effort

You cannot guarantee both. Two people in one document can hash adjacent,
so a hash that gives cross-document stability cannot also give
in-document separation. **Separation is the guarantee**, because it is
the one carrying meaning; stability is documented as best-effort. Hash
for the preferred allocation, displace the later arrival on collision.

**Determinism is a correctness requirement, not a nicety.** Allocation
must be a pure function of a canonically ordered key set, never of
arrival order — otherwise two clients rendering the same document
disagree about who is which colour, which is worse than no colour at
all. "Displace the later arrival" means later in the canonical order,
not later in wall-clock time at one client.

`Allocate` takes an `avoid` set (Sheets' requirement) so a caller can
keep already-allocated hues out of a new allocation; collisions then
only happen past the set's capacity, which is the honest limit rather
than an accident.

**The whole algorithm, because naming the loser is not enough.** Docs
proposed the total order — displace whichever opaque key sorts later
lexicographically over its bytes — which is client-independent and
correct as far as it goes. It stops one level short: it says who moves
and not where they move to, and two implementations that agree on the
loser and disagree on its destination produce exactly the bug the order
was introduced to prevent, one level down.

So the displacement target is specified too, and the allocation is
deterministic open addressing over the sorted set:

1. Sort the key set lexicographically over the keys' bytes.
2. For each key in that order, probe `(hash(key) + i) mod N` for
   increasing `i`, taking the first hue that is neither already taken in
   this pass nor in `avoid`.

The first key to want a hue keeps it, which preserves the globally
stable allocation for the lexicographically earliest holder; every
displacement afterwards is a pure function of the sorted set. Chains
resolve because the probe is deterministic — with A, B and C colliding
in sequence, C's destination cannot depend on the order it happened to
be observed in.

`avoid` is applied inside the probe rather than before it, so an avoided
hue displaces exactly like a taken one and the two cannot disagree.

### The key is opaque and the framework knows nothing about users

Callers pass a stable opaque key and own what it means. Docs has
commenters with no account — a guest becomes a member at the moment they
write, so their key is the membership row; Sheets uses a member id. `ui`
has no business knowing what an identity is, and this keeps it out.

### Past capacity, say so rather than lie

`Allocate` returns the allocation **plus a flag that separation is no
longer guaranteed**. Silently reusing hues would be the lie. This needs
no other mechanism because the framework's existing rule already does
the work: colour never carries meaning alone, so every author carries a
name or initials regardless — past capacity, colour degrades to
decoration and the label carries identity by itself. The flag exists so
a caller leaning on colour can stop.

### Why a bounded offered set

A free colour wheel cannot be proven; a fixed set of offered hues can be
proven at build time exactly the way the 26 documented pairs already
are. That is the distinction between a generator that is gated and one
that is hoped for — the same distinction that ruled out a palette
randomiser in §6-v2.2.

**The set must be proven against every background it can be rendered
on, not one.** Paul ruled Docs' canvas as light by default with a dark
canvas as a *per-person* preference, which means the same stored
highlight is read against paper white by one person and dark paper by
another, in the same document, at the same time. So an offered intent
is only sound if `Pair` clears the floor for it against **each** of the
backgrounds the suite can render: paper white, dark paper, and the theme
surfaces. An intent that works on white and fails on dark paper is not
an offered intent; it is a trap with a build gate that agreed with it.

That multiplies the build-time proof by the number of backgrounds, which
is the correct cost — and it is another thing a `scheme` parameter could
not have expressed, because two of those backgrounds exist inside the
same scheme.

**Export resolves against the print background, structurally.** Paul
also ruled that print and export always render true colours regardless
of the reading preference. That is a caller decision, and Docs is making
it structural rather than disciplinary — their export renderer will have
no access to the preference at all, so it cannot pick up the dark
resolution. Worth copying: a rule enforced by what a component can
reach beats a rule enforced by remembering.

### The resolved hex is part of the API

XLSX round-trips fills as hex in both directions, so a caller needs the
concrete colour per background, not only the intent. Storing intent and
never a colour is insufficient for a real caller. Import mapping hex to
the nearest offered intent stays the app's problem.

### Declared consumers

Recorded because an API with named consumers changes differently from
one without: these are who breaks if a signature moves, and the list is
the reason several of the decisions above are not negotiable.

**CARLOS Docs** (`carlos/docs`) — `Pair` for text highlight and comment
author colours, against **four** backgrounds (paper white, dark paper,
and the theme surfaces; the first two are Docs-defined literals it will
supply with its intent set). The allocation entry point for comment
threads, presence cursors and collaborator avatars, with a membership
row as the opaque key for guests who have no account. `rastrillo doctor`,
replacing a manual re-copy step it carries in its upgrade runbook today.

**CARLOS Sheets** (`carlos/sheets`) — `Pair` for cell fills and
conditional formatting, against backgrounds that are frequently not the
sheet surface (a conditional format under a user fill, the selection
tint, a frozen header). The allocation entry point for presence cursors.
The resolved hex for XLSX round-trip. `rastrillo doctor`, same reason.

### Sequencing

Sheets needs this before v2.2 can land, and is building a deliberately
crippled shim: a hand-computed table of six to eight intents behind this
exact signature, gated by a contrast test using **the same floors and
the same arithmetic** as `ui/contrast_test.go`, with a deletion trigger
linking this section. There is no algorithm in it to diverge from. Their
chosen intents and hand-computed pairs come back here as input to the
generator's offered set.

---

## 6-v2.3. `rastrillo doctor` (2026-08-30) — AS BUILT (2026-08-30)

Compares an app's frozen `static/*` — `tokens.css` above all — against
the module's embedded copies, reports drift, and offers to re-copy.

**Two reasons, and it needs both to be worth building.** First:
`tokens.css` is written into `static/` at scaffold time and frozen
there, while partials upgrade with the module, so an app silently runs
new markup against old CSS. That is currently a manual step in a
runbook, which means a step people skip. Second: §6-v3's markup
migration is staged *because* of this trap, and its middle stage needs
exactly this comparison to tell an app whether it is safe to flip
spellings.

Named downstream consumer: the Sheets app, which asked for it and said
it would adopt it the day it ships. That is what moved it from an idea
to work.

### As built

`cmd/rastrillo/doctor.go`, `rastrillo doctor [--fix] [--force] [--theme
<name>] [dir]`, documented at docs/site/cli.md.

**The version question was the design.** The CLI carries its own
compiled-in `ui`; the app has its own required version in `go.mod`; the
difference is not drift. Doctor reads both, names the one it compared
against on the first line, and **refuses `--fix` across a mismatch**
without `--force` — copying this binary's assets into an app that
compiles against an older module manufactures the exact fault the tool
detects and then reports it clean. A `replace` directive is not a
mismatch: there is no second version to disagree with, only the question
of whether this binary was built from that checkout, which doctor says
out loud rather than guessing.

**One list, three readers.** `ui.VendoredAssets(theme)` is now the
single definition of the vendored set. The scaffold writes those bytes,
the `vendored_test.go` it generates compares against them, and doctor
reports the difference — replacing the list that had been written twice,
once in `new.go`'s file map and once inside its generated-test template.

**Three things it will not call drift**, because one false positive
costs the tool everything: a `theme.css` matching no shipped theme
("custom or drifted", never diffed against a guess), a file named in the
generated test's new `vendoredIsMine` map, and a file whose pin line an
older scaffold's test had deleted — which meant the same thing before
the map existed, and whose apps are exactly the population doctor is
for.

The third of those carries a condition the first implementation missed
(found in review, fixed before merge): **the file must still exist**.
The original pin (`36ee472`, #73) listed three files; `theme.css` and
`datetime.js` joined the vendored set later (#104, #106), so for an app
scaffolded in that window a name its pin never mentioned means
"predates", not "deleted on purpose". Reading it as a claim told the
oldest apps — the ones most likely to be drifting — something false
about their own history, and made `--fix` withhold a file they needed.
The file itself separates the two readings, and it is the only evidence
that can. You delete a pin line to protect an *edit*, so: absent →
`absent` (true under either reading, and the one state `--fix` acts on
without `--force`); present and identical to the library → compared
normally, because there is no edit to protect; present and differing →
`yours`, the reading that never overwrites a person's work.

Existence alone — the obvious rule, and the one first written — has a
trap one step on: `--fix` delivers `datetime.js` to a 2025 app, the file
now exists, the old pin still does not name it, and the next run calls
it a deliberate edit. Doctor would have permanently exempted a file it
installed itself. Reading the content closes that.

**A fourth reader of the one list**, added in the same round: the
gallery's Getting started page. Its `AppBytes` total and its file rows
were enumerated by hand, bound to the library only by the
`len(scripts) == 3` canary that §6-v2.1's cleanup retired. Both now come
off `ui.VendoredAssets`, so a sixth vendored file cannot reach an app
while the page that weighs the set stays quiet.

Exit codes 0 clean, 1 error, 2 usage, 3 drift, 4 version mismatch. Drift
and mismatch are separate because they call for opposite actions: one
means "re-copy these", the other means "re-copy nothing yet".

---

## 6-v2.4. Semantic elements and common data formats (2026-08-30)

**RULED by Paul:** native semantic elements now; schema.org microdata
later **only if a real machine consumer appears**; and both the partial
changes and a new "Common data formats" gallery page.

**Microformats were considered and not taken.** h-card/p-name/u-url are
classes, and §6-v3 reserves `class` for utilities and the app's own CSS
— adopting them would reintroduce class-as-semantics in the same release
that removes it, and every doc sentence explaining the rule would gain
an exception. If a machine-readable vocabulary is ever needed, microdata
is attribute-based and agrees with the grammar instead of fighting it.

**Nothing in this list is used anywhere in the framework today.**
Verified: no `<time>`, `<data>`, `<meter>`, `<progress>`, `<address>`,
`<abbr>`, `<bdi>`, `<figure>` in any partial or layout. The `meter`
partial is spans and an `<i>`; `job-status` is a div.

| Element | Where | Why |
|---|---|---|
| `<bdi>` | any user-supplied name in running text | Twelve locales including Arabic. A Hebrew or Arabic name inside an English sentence reorders the line without it. Non-obvious, and currently wrong |
| `<time>` | list rows, detail lists, job status | Machine-readable dates the framework already parses with `Intl` |
| `<progress>` | `job-status` while running | The native element for exactly this |
| `<meter>` | the `meter` partial | Named after an element it does not use |
| `<data>` | stat headlines, identifiers, quantities | See below |
| `<abbr>` | WCAG, AA, RTL in the gallery's prose | Free, and the gallery is where a reader meets them |
| `<figure>`/`<figcaption>` | the preview frames | A frame with a caption is literally this |
| `<output>` | computed form values | Where a form shows a derived result |
| `<address>` | shell footer and article byline ONLY | See the correction below |

**`<address>` is the one most likely to be got wrong**, and it was named
in the request. It marks contact information *for its nearest `<article>`
or `<body>` ancestor* — the author of that content. It is **not** for
postal addresses generally and **not** for a list of people. The
`person` partial must not become `<address>`: a user row is data about a
person, not the document's authorship.

**`<data>` buys machine-readability, not accessibility.**
`<data value="4120">4.1k</data>` gives the exact figure to a machine
while a person reads the abbreviation. But `value` is **not exposed to
assistive technology** — a screen reader announces the text. If an
abbreviation loses something a person needs, the fix is visible text or
an accessible name, never `value`. And time and duration take `<time>`,
not `<data>`; that is the distinction that gets got wrong.

**Dashboard stats.** Paul: "headline dashboard stats, secondary stats,
then maybe some widget variants, possibly inspired by titogo". The
titogo precedent, quoted from `internal/instance/styles/admin/dashboard_page.css`:

> stat-band — the joined instrument strip that opens a dashboard or a
> report: the lead reading (usually money) oversized, companion counts
> divided by hairlines in the same card. Labels ride above their number,
> one eyebrow grammar for every cell. Flex so any cell count works.

That is the headline/secondary split already solved once by a real
dashboard: **one component with a lead cell, not two components**. Under
§6-v3's grammar, `<div rst-stat="lead">` beside `<div rst-stat>`.

The framework's existing rules already decide the hard part: a delta
(+12%, −4%) may never be green-or-red alone, and the number is always
visible text — the same rule `meter` follows today.

**The Common data formats page** collects dates, durations, numbers,
currency, percentages, file sizes, identifiers, people and addresses,
each with the element that carries it and how it renders across the
twelve catalogs. It connects to work already done: `datetime.js` derives
its whole vocabulary from `Intl`.

---

## 6-v2.5. The tree stops being committed (2026-08-30) — RULED by Paul

The generated gallery is committed at `docs/design-system/`, vendored
into the website by `sync-docs.mjs`, and served as static files. Paul
ruled it should be generated **during the website build** instead.

### Why, with the numbers that made the case

The tree is **20 MB against 14 MB for the entire rest of the
repository** — the framework, three example apps and all the prose. It
is rewritten whole on every change that touches `ui`: eight commits in
three days. `.git` is 43 MB, and most of that is this artifact's
history rather than the artifact.

So the largest thing in the repo is machine output, and it is also the
noisiest thing in every diff. `maxTreeBytes` was a brake on that ratio,
and the question it was really standing in for is this one.

### What this buys, and it is more than the megabytes

**Freshness stops being a gate and becomes structural.**
`TestDesignSystemIsCurrent` exists only because a committed copy can
drift from its generator. Delete the copy and the drift is
unrepresentable — there is no second thing to disagree. A gate deleted
because its failure mode cannot occur is the best kind of deletion; it
is not the same as a gate deleted because nobody wanted to fix it.

The orphan walk goes with it, for the same reason.

### The mechanism: a public `cmd/dsgen`

Go's `internal/` rule means the website cannot run
`go run …/internal/designsystem/cmd/dsgen@<sha>` — internal packages are
unreachable from outside the module. So the command moves to
**`cmd/dsgen`** at the repo root and becomes part of rastrillo's
published surface; `internal/designsystem` stays internal and the
public command calls it.

**That is a real commitment and is recorded as one.** The generator
gains version compatibility expectations it did not have while it was
internal. It takes an output directory and a mount path; it must not
grow flags that encode the website's opinions.

The website runs it at build time against a pinned sha. The pin already
exists: `src/_data/docsversion.json` records the rastrillo sha the docs
were vendored from, and that same sha generates the gallery.

**Cost:** the website build gains a Go toolchain and a module fetch,
taking its `build` check from ~13s to roughly a minute. Accepted.

### The size gate changes meaning, so it changes shape

A whole-tree ceiling was a proxy for *repo* reviewability. Once the tree
is not in the repo, the thing that matters is what a reader waits for —
which is **per-page weight**, and is what Paul actually complained
about. Replace `maxTreeBytes` with a per-page budget. That is a better
gate on its own merits: it fails on the page that got heavy rather than
on the total, and `components.html` at 325 KB would have tripped it long
before the total came near any ceiling.

### What is honestly lost

The rendered output stops being diffable. That has had real value today:
seeing 37 files change in a commit is what surfaced a controller mistake
that swept an implementer's in-flight tree into an unrelated commit.
`dsgen` writing to a local, git-ignored directory keeps that available
for inspection — it just stops being a thing the repo carries.

### Consequential changes

- The a11y scan serves the tree from disk today
  (`a11y_test.go:139`, `http.FileServer(http.Dir(treeDir))`). It serves
  `Render()`'s output from an in-memory FS instead — strictly more
  direct, because it then scans exactly what would be published rather
  than what was committed.
- `sync-docs.mjs` keeps vendoring the 49 markdown pages byte-for-byte;
  only the gallery moves.
- The website's file-count guard changes shape: it can no longer compare
  a vendored count, so it verifies the generator ran and produced the
  page kinds it expects.

#### AS BUILT (2026-08-30) — framework side

Six commits on `dsgen-at-build-time` (five building it, one closing a
review). The website change is not in them and is still to do.

**`cmd/dsgen` is public, and the mount is a real argument.** The command
moved out of `internal/designsystem/cmd/dsgen`. It takes `-out` and
`-mount` and nothing else. The mount is threaded through
`designsystem.Render(mount)` and the twenty-seven page-building
functions under it rather than read from a constant, so it is a
parameter that is actually honoured: rendering at `/ui/gallery/`
produces the same 369-file set with no `href=` or `src=` under
`/design-system` anywhere in it, and — since the locale-menu fixture in
`samples.go` stopped spelling the gallery's own mount in its `Return`
value — no occurrence of the string in any page at all. (It survives in
one place, `gallery.js`'s header comment, where it is the project's name
and not a URL.) `designsystem.CleanMount` normalises a trailing slash or a
missing leading one and refuses the site root, because the tree writes
`tokens.css` beside its theme directories.

The command the website runs:

```
go run amadan.net/rastrillo/rastrillo/cmd/dsgen@<sha> \
    -out src/design-system -mount /design-system
```

`guardRoot` is gone. It could only ever protect one hardcoded path, and
the 152-file incident behind it needs a rule that generalises to a
directory an outside caller names. **dsgen owns `-out`**: it empties the
directory before writing, and it will only take ownership of one that is
absent, empty, or already carries its own `.dsgen` stamp. Anything else
is refused with no filesystem change.

Both halves are load-bearing, and the first attempt had only one of them.
Removing just the top-level paths the current render produces is safe and
incomplete: a theme dropped or renamed between versions — this project
renamed `ink` to `day` — keeps its whole directory and its stylesheet in
a build directory that persists, published and linked, and the site's
shape guard checks which files are present, never which are extra. That
is the deleted freshness gate's failure mode one directory over, because
an output directory that outlives a render IS a second copy. Emptying is
what makes the output the render and nothing else;
`TestWriteLeavesNoTraceOfAnEarlierRender` seeds exactly the rename's
residue and `TestWriteRefusesADirectoryItDoesNotOwn` holds the other
half.

**The tree is deleted**: 369 files, 19,038,929 bytes. `docs/design-system/`
and `.design-system/` are both git-ignored — the second is where
`go generate ./...` now writes, for reading locally.

**`TestDesignSystemIsCurrent`, its orphan walk, `treeCommitted` and
`treeDir` are deleted**, and so is the disk half of
`TestVendoredAxeStaysOutOfTheTree`. The a11y scan's `committedTree` is
deleted with them: `a11y_test.go` now uses `browser_test.go`'s
`treeHandler`, which serves `Render()`'s output from memory, so CI scans
the bytes dsgen would publish rather than a copy of them. Nothing in the
package reads the filesystem for the tree any more.

**`maxTreeBytes` → `maxPageBytes`, 128 KiB per HTML page.** A little
under twice the heaviest page that is not `components.html`
(`primitives.html`, 67,507 bytes in its widest locale), so a new section
can land without an argument.

**`components.html` does not pass it, and this is the finding.** 332,827
bytes in `day/en`, 387,029 in `signal/hi` — 3× the budget and 5.7× the
next heaviest page. It is in a `pageBudgetDebt` table with its own
ceiling, the shape `axeExempt` and `colorMixSkip` already use here. An
entry counts as needed only while a page of that name is **actually over
`maxPageBytes`** — not merely while a page of that name exists, which is
what the first attempt checked and which would have let a fixed
`components.html` keep a 3× permission slip for ever. So the table
shrinks the moment the page is fixed, and
`TestTheDebtTableCannotOutliveTheDebt` is the gate on that, because it is
the only property that makes an exemption table worth having.
The weight is the Code tabs: every sample is written into the
page a second time, escaped, for a tab most readers never open. Fixing
that is a change to what the page contains and has not been made.

Repo size: 34 MB → 14 MB in the working tree. `.git` is unchanged at
46 MB — deleting a file does not delete its history, and only a rewrite
or a fresh clone with `--filter` would recover that.

---

## 6-v2.6. Variants (2026-08-30) — RULED by Paul

The question was whether the design system should become its own repo,
and the case for it was that two CARLOS apps had begun proposing
components into it. **Paul ruled: the design system stays rastrillo's.
The CARLOS suite gets its own, even if it is only a variant.** And
further: *"maybe at some point we support a variant framework so that
Rastrillo apps all get their own system out of the box."*

### What this settles

The repo question is closed, and for a better reason than the one I
gave. My argument was mechanical — the gallery is `ui`'s test suite, and
splitting it turns every coverage gate into a pinned gate that lags.
That still holds. But the ruling answers the pressure underneath it,
which was never really about repositories: two apps wanted components
that are not rastrillo's, and the only shape on offer was "upstream it".

There is now a third answer. A suite's components live in the suite's
variant.

### What a variant is

A variant depends on `ui`, keeps its bones — the tokens, the partial
vocabulary, the accessibility floors — and adds or replaces on top: its
own components, its own themes, its own gallery. It is not a fork and it
is not a theme; a theme changes colour, type and shape, and a variant
can add a component a theme cannot.

**`cmd/dsgen` going public is NOT the mechanism a variant's gallery
needs. Corrected 2026-08-31.** This section originally said it "happens
to be the mechanism a variant's gallery needs". That is false, and I
repeated it to sheets-03, who wrote it into estilo's `AGENTS.md` and
`README` as settled fact before their implementer opened the source and
found otherwise.

What is true: `dsgen` is public, and any module can run it. What is
false: that running it against another module documents *that module's*
vocabulary. Its entire public surface is `-out` and `-mount`;
`internal/designsystem` imports `amadan.net/rastrillo/rastrillo/ui`
directly, and its page kinds, samples and prose are rastrillo's. Pointed
at estilo it reproduces rastrillo.org's gallery. The tool's own usage
string was honest throughout — "dsgen writes **rastrillo's**
design-system gallery" — so the error was mine and never the code's.

**The underlying want is real and remains open.** A variant with its own
components needs to document them, and the two available paths are both
refused by estilo's own rules: fork the tool, or hand-write a gallery.
So the question is whether `dsgen` grows a seam — a way to point `Render`
at another module's vocabulary — or whether variants are expected to
solve it themselves. Not decided, and not urgent: estilo has no
components yet and will not until an app needs one.

Recorded rather than quietly edited, because the claim is in circulation
and the next person will hear it from the same place sheets-03 did.

### Why this is the same problem as the rake line

The original complaint was that every rastrillo app looks like a
rastrillo app — one accent stroke, hardcoded, that no theme could
remove. The answers to that keep arriving at the same shape: put the
flourish on the theme axis (§6-v2.2), generate palettes rather than
prescribe three (§6-v2.2), and now let an app or a suite carry its own
variant rather than inherit one look. **A framework that ships one
appearance produces apps that all look alike, and each of these rulings
is the same correction at a different scale.**

"Out of the box" in Paul's phrasing is the demanding part: a variant
should be something `rastrillo new` can offer, not a thing an expert
assembles. That is design work, not yet started.

### Correction owed downstream

Docs and Sheets were told the inspector panel was "the shared piece, and
that is what gets proposed to the library". **Under this ruling that is
wrong.** An inspector panel shared by a document editor and a spreadsheet
is the CARLOS suite's component, not the framework's. Both sessions were
corrected on 2026-08-30.

The bar that made it look like a library component — two callers at
design time — is the right bar for *extraction*. It says nothing about
*where to*, and I collapsed the two.

### Extraction and destination are different questions

Worth stating as a rule, because every piece a downstream app hands
upward will raise both and they have different answers:

- **Should this be extracted?** Two independent callers need it. That is
  the bar, and it is about whether a shared thing exists.
- **Where does it go?** Whose vocabulary is it in? A component two
  CARLOS apps need is the suite's. A component two *rastrillo* apps of
  any kind would need is the framework's. Being shared says nothing
  about which.

**And "two callers at design time" is weaker evidence than it sounds** —
that was my phrase and it overstates. Two specs that both describe
wanting a thing are two guesses about usage; two working
implementations are two facts. Extraction from working code shows you
the shape both callers actually needed, including the parts neither
spec predicted. Prefer app-local until a second app genuinely reaches
for the first one's component.

That is the answer to "where does the variant live": **nowhere yet.**
Build the primitives app-local in Sheets and Docs, and let the variant
be created when a real second consumer appears rather than because a
ruling made room for one. A variant repo with one consumer is exactly
what the extraction bar exists to prevent.

### Which spelling a variant writes

A variant depends on `ui`, and `ui`'s shipped `tokens.css` styles
`class="rst-*"`. So a variant written today writes **classes**, and
migrates alongside the framework's own 585 call sites when §6-v3 lands.

Not because classes are right, but because a page carrying `ui`'s
classes and a variant's attributes has two spellings of the same
vocabulary in one document — precisely what §6-v3 exists to prevent, and
worse for arriving as a mixture rather than as a migration. The
migration is mechanical and one codemod covers both. Being early is not
worth being inconsistent, and it is certainly not worth shipping
unstyled.

### Where the variant lives — RULED 2026-08-30 by Paul, on Fable's recommendation

**No variant repo today. Primitives go app-local in each app. The
variant becomes its own module the day the second app actually reaches
for the first app's primitive — extraction day and creation day are the
same day.**

Fable 5 recommended it; the full reasoning is in
`.superpowers/sdd/2026-08-30-design-system-v2-1/fable-variant-recommendation.md`.
Three findings changed the argument:

**The Go module objection was overstated.** Docs argued that sharing a
package from inside an app module drags the whole application in. Fable
built the scenario: with module graph pruning (Go ≥1.17) the heavy
dependency does not reach the consumer's `go.mod` even as indirect,
nothing from it compiles, and the build succeeds *with that dependency's
source deleted* — only its `go.mod` is read. What is real is **version
coupling**: the consumer would pin the application's release tags. That
still rules out sharing from inside an app module, for a different
reason than the one given.

**And both apps skipped a step.** Neither ever needs to `go get` the
other, because adoption day is the day the code moves out to a fresh
module. The module boundary settles what shape the shared thing takes;
it says nothing about when to create it.

**Paul's larger intent argues for later.** A variant framework that
`rastrillo new` can offer is a framework feature, and a bespoke repo
built before any primitive exists is a contract designed from guesses
whose accidents the framework feature would then have to accommodate —
this section's own extraction lesson, one level up. App-local primitives
teach what a variant contains; the first real extraction produces the
CARLOS variant; that working variant shapes the framework feature as its
first user.

There is also a timing argument neither app raised: §6-v3 is ratified
and unstarted, so a module created today is a versioned public surface
born in the losing spelling with a breaking migration guaranteed in its
first months. App-local code migrates by codemod, with no release.

### A copy is the trigger

The failure Fable expects from its own recommendation, and it is this
project's native one: **extraction day never gets scheduled.** Under
deadline one app copies the other's CSS into `static/` and drift begins.
The frozen `tokens.css` is the proof that this is what actually happens.

So the trigger is named as a cheap, observable act rather than a
judgement: **the day either app copies or imports the other's primitive,
the shared module is created that week and the copy deleted in the same
change.** "A second consumer appears" is too abstract to ever fire.

This is now a rule in `SKILL.md`, because it is not suite-specific —
every rastrillo app has components that are the app's rather than the
framework's.

### The two-pin cost, contained

A variant pins `ui`; apps pin the variant. Real, not decisive, and zero
until the variant exists. MVS closes half automatically; the dangerous
direction is an app upgrading `ui` past the variant's CSS, which fails
**visually rather than at compile time**. Containment: the variant
embeds its CSS from the module rather than being scaffold-copied — one
pin, no frozen file — and declares its tested `ui` range for
`rastrillo doctor` (§6-v2.3) to check, which is an extension of a
mechanism already approved rather than a new one.

---

## 6-v2.7. Dashboards and a fourth shell (2026-08-30) — RULED by Paul

Two additions, with Stripe's dashboard as the reference Paul supplied.

### A shell that is topbar AND sidebar

`ui.Layout` ships `column`, `topbar` and `sidebar`. It cannot do the
commonest admin shape of all: a brand-and-account bar across the top
with a navigation rail beneath it down the side. Stripe, GitHub and most
consoles are this, and an app that wants it today has to hand-write a
layout the framework nearly provides.

Proposed name: **`console`**. Same `nav`, `account`, `locale`, `brand`
and `content` blocks as the other three, so a screen moves between
shells without an edit. It collapses like the others below 800px — and
it has two things to fold rather than one, which is the design work.

### Dashboard components

The vocabulary from the reference, named for what each one is:

- **stat** — a figure with an optional **delta** (`+1,900%`) and a
  comparison label (`1 previous period`). §6-v2.4 already ruled `<data>`
  for the figure and titogo's `stat-band` as the lead/companion shape.
- **card footer** — `Updated 22 seconds ago` on one side, `More details`
  on the other. It recurs on every card in the reference and is a
  component, not a one-off.
- **leader list** — name and meta on the left, a right-aligned figure on
  the right, ranked. Distinct from `list-row`, which is a record you
  navigate to; this is a reading of a top-N.
- **series** — the chart itself.
- and the combinations: stat over series in one card, stat beside stat,
  series spanning a full-width block.

### The chart problem, and the answer

A chart cannot be a charting library here — the doctrine is that the
scriptless path is the real one, and a dashboard that renders nothing
without JavaScript fails it at the first screen.

**Server-rendered SVG.** A Go helper takes the points and emits a
`<polyline>` or `<path>`. Zero JavaScript, prints, works in email, and
degrades to nothing worse than itself.

Three rules the reference already follows, and which this system holds
anyway:

1. **Geometry in SVG, labels in HTML.** Axis values, the title and the
   footer are HTML around the SVG, not `<text>` inside it. They then
   translate through the twelve catalogs, scale with the type tokens,
   stay selectable, and reflow. Text baked into SVG does none of that
   and would quietly break every non-English rendering.
2. **A series is distinguished by line style, not only colour.** The
   reference draws this period solid and the previous dotted. That is
   the framework's existing rule — colour never carries meaning alone —
   arriving at the same answer, and it must be the default rather than
   an option.
3. **The chart is a picture of data, so the data must be reachable.** A
   `role="img"` with an accessible name summarising the trend, and the
   series available as a real table — visually hidden, or in a
   `<details>`. That satisfies the text-alternative requirement *and*
   gives a keyboard or screen-reader user the actual numbers, which a
   summary sentence cannot.

Do not accept a chart that is only an image with a label. The numbers
are the content.

### Sequencing

After §6-v3 stage 2. These are new components in the class spelling
today and would otherwise be written twice.

---

## 7-v2. Calibrate the instrument (2026-08-31) — a testing rule this project earned

Twelve gates on this project have gated nothing. The failures looked
different every time — a probe reading the IDL property it set, a
sentinel checking a string nobody says, an assertion measuring the right
thing in the wrong frame, a test submitting a form on every run while
reporting green — and they were one failure.

Stage 2 produced the cleanest instance, and it is worth the space
because it defeated the strongest evidence a process normally has.

A report claimed 577 pages, 236,495 elements, **zero differences**
between two renders. **A reviewer reproduced it exactly**, and proved
the drive could fail by planting a mutation. Both were wrong. The drive
had no control: run against a *single* tree it should return zero by
construction, and it did not — a fixed sleep, `readyState` answering
the wrong question for a `srcdoc` frame, pages swinging between
readings. The zero was luckier than it was sound.

**Two parties agreeing about the output of an uncalibrated instrument
is not two pieces of evidence. It is one piece, twice.** (sheets-03's
formulation, and better than mine.)

### The rule

**Run the check against a case whose answer you already know, and see
whether it says so.** A comparison of a thing with itself must be zero.
A gate over an empty set must fail rather than pass. A drive that finds
no violations must be shown finding one.

Mutation testing — which this project already does — proves a gate can
fail. It does not prove the gate is *measuring* rather than guessing.
The control is the other half, and it is the half nobody remembers.

### The corollary, which is about reviewing

Reproduction with the **same instrument** is one measurement twice.
Reproduction with a **different instrument** is two. Stage 2's reviewer
did both: it re-ran the implementer's drive (one measurement, twice) and
then wrote its own — state pseudo-classes stripped, `@media` entered,
30,162 comparisons — which is genuinely independent and is what makes
the final number trustworthy.

When a review's job is to confirm a measurement, build a second
instrument rather than running the first one again.

### And the honest number was the useful one

The calibrated drive reports **80 elements on two pages**, traceable to
a Vietnamese copy redraft in the same branch, with the markup flip
itself at zero. That is a result someone can act on. "Zero differences"
was not, because it could not be told apart from a broken harness.

---

## 6-v2.8. What a data attribute is for (2026-08-31) — RULED by Paul

Paul, looking at a date input carrying thirteen of them:

> "do all of the data attributes on the date input need to be inline? Since they're for
> the JS enhancement, shouldn't they be in JS? ... The goal is to keep the HTML clean.
> **data attributes should allow for changing the behaviour of the JS, less for
> containing initial config that it requires.**"

That last sentence is the rule, and it generalises past the instance that produced it.
An attribute earns its place in the markup when it says something *about this element*
that the script could not otherwise know — armed or not, this variant rather than that
one, this override. It does not earn its place by carrying data the script needs on
every instance, identically.

### What it was costing

`ui/partials/field-date.html` put thirteen attributes on one `<input>`: the arming flag,
the whole parser vocabulary as a JSON object, and eleven UI strings. field-select,
field-time, field-datetime and field-daterange had the same shape. Measured on the live
`date-and-time.html`: **340 `data-rst-*` attributes, 15,146 bytes, 15% of the page.**
One field's run is 579 bytes; the entire 34-string vocabulary for one locale is 476
bytes gzipped. The markup was carrying, per element, more than the whole catalog weighs
once.

### The constraint that produced it, which is real

Translation happens server-side in Go against TOML catalogs, and `datetime.js`
deliberately matches on no English word of its own — that is what makes twelve locales
work. The JS cannot look a string up; it has no catalog. So the strings must reach it
from the server somehow. Choosing "on every element" as the somehow is the defect; the
requirement behind it is not.

### Ruled: a loader and a served catalog, not vendored files

`ui/i18n.js` loads one catalog per locale from a framework-owned route rendered from the
same TOML through the same translator the HTML uses.

**Twelve vendored `en.js` / `ga.js` files were considered and refused.** They would
recreate, twelve times over, the exact trap `rastrillo doctor` was built for in §6-v2.3:
a file copied into `static/` at scaffold time that drifts from the module that reads it.
And they cannot work at all for an app that adds its own locale, because the framework
cannot vendor a catalog it has never seen. Serving keeps one source of truth, covers
app-added locales for free, and adds nothing to the vendored surface.

A controller counter-proposal — one inline JSON payload per document — was measured and
**refused on its own numbers**: at 1,482 raw bytes it is *worse than the status quo*
until a page carries about three enhanced fields. It is recorded here because it looked
obviously right and was obviously wrong, and the thing that settled it was arithmetic
rather than argument.

### The cost, stated rather than buried

A failure mode that does not exist today: the enhancement arms, the catalog fetch fails,
and the reader gets the English fallbacks that `datetime.js` and `select.js` already
carry. It degrades rather than breaks, and Paul accepted it explicitly — *"we can add
even more resilience later if we need to"* — but it is new, and **the gate for it is a
drive with the catalog request blocked**, with a control that runs the same drive with
the catalog allowed. A drive that passes both ways is measuring nothing.

The scriptless path is untouched: these strings are only ever used by the enhancement,
and the native `<input type="date">` remains the value carrier that posts `2006-01-02`
with scripts off.

---

## 6-v2.9. What the weights table should say (2026-08-31) — RULED by Paul

The Getting started page reports each vendored file's weight, computed from the embedded
bytes at render time so it cannot rot. It reported raw bytes, and Paul read it as
overstated:

> "The file-sizes ... include lots and lots of comments, and not gzipped ... should we
> offer versions without the comments and show folks the gzipped cost?"

then

> "let's show kb rather than bytes, so instead of saying 107279bytes, do 107kb"

### Measured against what the edge actually serves

`content-encoding: gzip` and `vary: Accept-Encoding` are present on rastrillo.org —
verified, not assumed.

| file | raw | gzipped | comments stripped, gzipped |
|---|---|---|---|
| `tokens.css` | 107,279 | 28,998 | 8,832 |
| `theme-day.css` | 10,828 | 3,293 | 634 |
| `rastrillo.js` | 16,364 | 6,165 | — |
| `select.js` | 11,919 | 4,347 | — |
| `datetime.js` | 59,438 | 18,997 | — |

Two findings, and the second is the surprising one.

**The page overstated by 3.3×.** 205,828 raw against 61,800 over the wire. A reader
deciding whether to adopt this was reading a number no visitor ever pays.

**Gzip does not make comments free.** Comments are 47% of `tokens.css` raw and **70% of
it compressed** — 20,166 gzipped bytes, four times the weight of the shim. `theme-day.css`
is 85% comments. Every app vendoring these ships ~20 KB gzipped of design-doc commentary
to every visitor on every cold load. This is worth writing down because the intuition
that "gzip handles comments" is wrong at exactly the magnitude that matters.

### Ruled: show both figures, in KiB

- Raw **and** compressed, both computed at render time from the embedded bytes. Never
  typed into prose — the rule already governing this page still governs it.
- **KiB, 1024 bytes**, matching `maxPageBytes = 128 KiB` and the budget comments.
  Lowercase "kb" is kilobits, eight times wrong on a page whose point is exact sizes.
  `docs/site/compare.md` says "KB" once and comes into line.
- **One decimal below 10 KiB, whole numbers above.** Rounding everything to whole KiB
  turns a 634-byte file into "1" or "0", both lies.
- **Do not pin the compressed figure in an expected-value test.** Compressor output can
  shift between Go versions. The determinism gate is two `Render()` calls in one
  process and is unaffected; a golden file holding "28,998" would churn on a toolchain
  bump for no reason. Assert the relationship, not the literal.

### Not ruled: shipping comment-stripped assets

Left open deliberately. It collides with a doctrine already set — the comments in
`tokens.css` are *why* an app can own the file, and a stripped copy is one an app can
serve but not read. Vendoring both doubles what `doctor` reasons about and gives every
app a "which one did I edit?" question. The numbers above say the prize is real (20 KB
gzipped per cold load); the design is not decided.

Related, and noted because it is the larger number: **`datetime.js` is 59,438 raw /
18,997 gzipped — a third of the total wire cost on its own** — and it is an optional
enhancement. §6-v2.8 moves its strings out of the markup; it does not make the file
smaller.

---

## 6-v2.2c. `Wash`: the third entry point (2026-08-31) — from Sheets, on round-trip grounds

§6-v2.2b records that two downstream callers changed the colour engine's design before a
line was written. This is the third time it happened, and it happened *after* the engine
shipped — which is the case that section was written to make cheap.

### The controller's ruling, and why it was wrong

`Pair` returns a fill and an on-fill. Asked whether it should instead return a **pale
wash leaving the caller's own ink alone**, I ruled on the API's shape: a pair only means
something if we choose the ink, and a wash that the author's own text sits on needs no
contrast function at all. That reasoning is sound and the conclusion was still wrong,
because it reasoned about the return value and not about what a caller must then do with
it.

### Sheets' correction: applying an on-fill means persisting it

When someone highlights a cell yellow, they are choosing a **background wash and keeping
their own ink**. They did not ask for their font colour to change. If `Pair` hands back
an on-fill and the app applies it, the app must store it — and on export that becomes **a
font colour written into the XLSX that the author never set**. Import a file, highlight
one cell, export, and it returns with font colours throughout.

That is round-trip corruption. The same observation as the controller's — the on-fill is
surplus when the caller keeps their ink — with the consequence attached: it is not
surplus, it is destructive.

`Pair` remains correct for callers that own both halves: presence cursors, comment author
colours, and conditional formatting, where a *rule* picks the colour and the app writes
both fill and text.

### The wash constraint runs the other way

Not *"given a fill, what ink survives on it"* but **"given the ink the author already
has, what is the palest fill of this hue that their ink still clears 4.5:1 on — and that
they can still see they applied"**.

    Wash(hue, chroma, ink, background) -> Swatch

Two floors, and the second is the load-bearing one:

1. The caller's existing `ink` clears 4.5:1 against the returned fill.
2. The fill stays **perceptibly different from `background`**, or the user clicks yellow
   and nothing appears to happen.

**Floor 2 uses ΔE_OK, not a contrast ratio.** Sheets' point, and it is right: contrast
ratio is the wrong instrument for "can you tell this cell is filled". That is a
perceptual-distance question, and the engine already computes ΔE_OK for separation, so
it reuses that machinery rather than growing a second notion of perceptual distance. The
two floors pull against each other, which is what makes floor 2 real rather than
decorative — remove it and `Wash` returns something invisible.

**Failing is the right answer past the floors.** "This hue cannot be a readable wash
under your text colour" is a true statement, not an edge case to paper over. The
precedent is worth recording because it is an accessibility improvement rather than a
copy of the incumbent: Excel permits a dark navy fill under black text, unreadable, with
no warning. A wash function whose offered set **cannot** produce an unreadable pairing is
strictly better — and better *because* it constrains what is offered rather than
overriding what the author chose.

### Even coverage beats any particular hue

Sheets never delivered the hand-computed hue table they promised, and supplied something
more useful instead: **XLSX import maps arbitrary incoming hex to the nearest offered
intent.** So even coverage of the circle is the property that matters, and a gap means
every imported fill in that region snaps visibly sideways on a file the user never
edited. Twelve at 30° is even coverage.

The check that follows, and it is a measurement rather than an assumption: the
nearest-offered-hue distance for the five fills people actually use — **yellow, green,
red, orange, light blue**. Even spacing does not guarantee any of them lands well.

### The open question this raises for Docs

A text highlight is the same wash shape. But Paul ruled Docs' canvas light by default
with dark as a *per-person* preference, so one stored highlight is read against paper
white by one reader and dark paper by another, in the same document, at the same time. A
wash perceptible on white can be imperceptible on dark. Docs therefore stores an intent
and resolves per canvas — and if the offered set cannot satisfy floor 2 on both, that
constrains the set for every caller, not only theirs.

### §7-v2 addendum (2026-08-31): measure freely, publish only from the shipped path

sheets-03's sharpening of the calibration rule, and better than the version it refines.

The rule already said that two parties agreeing about an uncalibrated instrument is one
piece of evidence twice. The question it left open is what to do when you *want* a
second implementation — and the answer turns on what the number is for.

**Measuring to discover is free. Publishing is not.** A second implementation is the
right tool for finding a defect: this branch used one to establish that Excel's own
"Yellow Fill with Dark Yellow Text" preset measures 4.12:1 and fails AA, and that
`#0000FF` cannot carry black ink at 2.44:1. Neither finding needed the shipped code, and
waiting for it would have cost a round.

But a number that reaches documentation as *the scale for a shipped function* must come
from the code that ships. Otherwise the two can drift — a different white point, a
different ΔE variant, a rounding difference — and the published guidance quietly stops
describing the instrument the caller actually holds, with nothing to catch it.

Sheets applied this to themselves before anyone asked: invited to send measured
separations for the classic spreadsheet fills, they sent **hexes instead**, on the
grounds that numbers from their OKLCH implementation, published as the scale for
rastrillo's `Wash`, would be two instruments agreeing by assumption.

### And the corollary about what a guarantee covers

A generator's guarantee is about what it generates, not about what the product displays.

`Wash` cannot produce an unreadable pairing. A spreadsheet built on it **can still show
one**, because an imported file's original fill and font colour are retained verbatim —
which is what makes import→export lossless, and which is right: faithfully displaying a
document someone else authored is a different act from generating a colour, and silently
"correcting" imported formatting would lie about the file.

So the doc comment must claim *"a wash this function produces cannot be unreadable"* and
never *"a cell in this app cannot be unreadable"*. Someone will otherwise find a failing
cell and conclude the guarantee is broken while it is working exactly as specified. The
product's obligation is to **surface** the failing pairing, not to hide it and not to
override it — and that obligation lives in the app, not in this library.

---

## 6-v2.2d. The ink-unknown wash (2026-08-31) — Docs' requirement, specified not built

Docs confirmed `Pair` is wrong for text highlights, for the same reason Sheets did. But
their requirement is **not** Sheets' with a different weight, and the difference is the
whole of this section: **the caller does not control the ink.**

A highlight sits behind whatever colour the author already chose. The app cannot see it
and must not change it. So the guarantee is not "give me contrast-correct ink for this
fill" but *"give me a fill that does not break contrast for ink I did not choose."* That
is a bound on how far the wash may move from the background's luminance, computable from
the background and two floors alone — and it takes **no ink argument**.

### The stated requirement was impossible, and the arithmetic is why

Docs first specified "any ink that met 4.5:1 against the background still meets 4.5:1
against the wash". On paper white that has exactly one solution and it is white.

The worst case is the *lightest* legible ink, sitting exactly at threshold: luminance
0.1833 on white. For it to keep 4.5:1 on the wash, the wash needs luminance ≥ 1.0000.

    retained 4.5:1  ->  wash luminance >= 1.0000   impossible
    retained 4.0:1  ->  wash luminance >= 0.8833
    retained 3.5:1  ->  wash luminance >= 0.7667
    retained 3.0:1  ->  wash luminance >= 0.6500

So the retained floor must be **lower than the admission floor**. That is not a
compromise smuggled in; it is forced.

### RULED by Docs: 3:1 retained

The knob decides which real highlights are legal, which is what made the choice easy once
it was visible:

    #FFFF00  classic highlighter yellow   L=0.9278
    #FFEB9C  Excel light yellow           L=0.8308
    #C6EFCE  Excel light green            L=0.7820
    #FFC7CE  Excel light red              L=0.6656

At 3:1 all four are legal. At 3.5:1 Excel's light red drops out. At 4:1 Excel's light
yellow is already illegal. Docs' reasoning: **a highlight function that outlaws yellow
highlighter has failed at the only job it has.** Shipping a feature that cannot produce
the thing it is named after is worse than a slightly weaker guarantee.

### Two guarantees that compose, and neither is sufficient alone

Docs' second observation is the more reusable one. The impossibility bites hardest for
ink sitting exactly at the admission threshold — and **that ink only exists in a document
if the product offers it.**

So the retained floor is one knob and the offered ink palette is the other, and they
belong to different owners: the library guarantees 3:1 retained; **Docs constrains its own
offered text colours well clear of threshold**, so the theoretical worst case is not
reachable through the product's own controls. Docs is writing that into their spec
explicitly, so nobody later widens the ink palette without noticing it silently weakens
every highlight in every document.

**The residual, stated rather than hidden:** DOCX import brings arbitrary inline colours
from Word, and a pasted near-threshold grey under a light wash can land below AA with
neither knob preventing it. Docs leans toward reporting it in the import report rather
than silently restyling the document — the degrade-loudly rule — and will decide when
import is built.

### Not built

The known-ink `Wash` lands first, for Sheets. Docs has no highlight UI and asked for it
right rather than fast. `Wash`'s doc comment must say plainly that **it requires a known
ink and is the wrong function if you do not control the text colour** — otherwise someone
passes a guessed ink and quietly restyles an author's text, which is the same class of
harm as the XLSX font-colour leak that created `Wash` in the first place.

### One shape, reached twice, independently

Both apps arrived at storing an `Intent` and resolving per viewer, for unrelated reasons —
Sheets from export, Docs from a per-person dark canvas. Which also settles the check's
shape: **"for every hue there exists a wash on white and one on dark"**, never a
conjunction over a single hex. The controller had that wrong and was corrected.

---

### §7-v2 addendum (2026-08-31, second): calibration is necessary, not sufficient

The rule says: run the check against a case whose answer you already know. A gate on this
branch obeyed it fully and still gated nothing, and that gap is worth naming.

The preview-widget gate asserted that every preview box clears a 64px floor at a phone
viewport. It was calibrated — its control held on the pre-fix build, where it failed
naming the 20px boxes; three mutations of the finished CSS were caught; a fourth was
caught only after the gate was strengthened. By every test in §7-v2 it was a good gate.

**It was measuring the wrong quantity.** The bug was that a 1200px page rendered at 26%
made the *sample* an 18px sliver. The gate measured the *box*. A build where every box is
400px tall and every sample a 14px sliver passes it cleanly — and that was approximately
the state it certified, because the floor it verified added blank space rather than
legibility. A reviewer found it by reading **inside** the srcdoc frame, which neither
earlier instrument had done.

**So the rule needs a second half. Before calibrating a gate, check that the quantity it
measures is the quantity that can break.** A proxy that correlates with the fault in the
cases you tried will pass every calibration you can devise, because calibration tests the
instrument against the quantity you chose — it cannot tell you the choice was wrong.

Passing a well-run gate on the wrong quantity is worse than having no gate: a red gate
starts an investigation, and a green one ends it. The tell here was available in advance —
the gate asserted a *container* while the defect was in the *content*.

### §6-v2.2c addendum (2026-08-31): the weight scale, and the argument for a parameter

Measured through the shipped `Swatch.Separation`, against a white canvas:

    Excel light green  #C6EFCE  0.1056     flat yellow  #FFFF00  0.2134
    Excel light yellow #FFEB9C  0.1179     solid green  #00B050  0.3846
    Excel light red    #FFC7CE  0.1352     flat red     #FF0000  0.4526
                                           flat blue    #0000FF  0.6312

The four saturated hexes are verified in Sheets' repository and exercised by their tests;
the three presets were verified here against XlsxWriter's documentation, independently of
both teams. The floor sits around 0.030 — about a third below even the lightest preset.

**Sheets' perceptual check on the ordering, which is the cheapest calibration available
and worth imitating:** blue is furthest from white, then red, then green, with bright
yellow lowest of the saturated four — because yellow *is* nearly as light as paper. "If
yellow had come out above red I would have distrusted the instrument." A result that
matches a physical intuition you held before you measured is weak evidence; one that
contradicts it is strong evidence something is wrong.

### Two bands, and why weight is a parameter rather than a floor

The strongest argument for the target-separation parameter is not that two apps differ.
It is that **two callers inside one app differ**:

- **Rule-driven fills** — conditional formatting, where the app owns both halves and users
  expect Excel's register. The **0.10–0.14** band; Sheets defaults near **0.12**.
- **Hand-picked fills** — a person choosing from an offered set, who expects what they
  chose to *look* chosen. The **0.21+** register, where flat yellow sits.

One product, one API, two bands, decided by who is doing the choosing. A global floor
could serve neither without failing the other, which is the general form of "a floor is
the wrong instrument for a preference".

Document the two bands as the guidance rather than publishing a bare table. A table tells
a caller what colours measure; the bands tell them which number to pass.

### `SeparationMet` is a one-directional signal, deliberately

`requested >= floor && delivered >= requested - dust`: it reports **paler than you asked
for, never darker**. That is the direction that needs surfacing — a fill weaker than
intended is invisible in a scan of a thousand rows, which is the failure a user actually
suffers, while a fill heavier than requested is merely emphatic.

Proximity was tried first and abandoned on a real case (asked 0.08, got 0.0856). No
tolerance can work: **the achievable weights are not evenly spaced**, because the ink
floor cuts gaps out of the middle of the range, and any tolerance wide enough to swallow
those would report every constrained answer as honoured — turning the one signal the
caller needs into noise.

**Corrected twice, 2026-08-31, and the sequence is more instructive than the answer.**

The section first said "gaps of up to 0.067", relayed from a report; I published it and
passed it to Sheets. A review then measured **0.0580** on the shipped canvases, **0.0608**
over a sweep, and **0.9588** at an extreme, so I corrected the section to say the original
figure did not reproduce. A third measurement corrected *that*: the largest gap is
**0.0649** on the shipped canvases and **0.0666** over the wide sweep, and the 0.9588
extreme does not exist — ink `#757573` has a *negative* dark bound (−0.000017), so only
light fills are feasible and there is at most one achievable weight per hue, hence no
second island.

So the original 0.067 was approximately right, and my confident retraction of it was
wrong in the opposite direction.

**What resolved it was a method correction, not more sampling.** Gaps must be read off the
**sorted set of achievable weights**, not off the lightness walk, because separation is
V-shaped about the background — a walk crosses the background and reads a spurious span.
Two of the three measurements made that mistake, which is almost certainly where 0.9588
came from.

The lesson survives all three passes intact, and is the reason the disagreement was cheap
rather than expensive: **a number supporting a design decision belongs in an assertion,
not in prose.** The conclusion is now gated as `maxGap > 10 × washTolerance` and no figure
is quoted anywhere a test does not hold it. Had that been true at the start, none of these
three numbers would have needed publishing, correcting, or correcting again.

### The slack constant: the pattern in one number

`washSlack` shipped at 0.02. The bound its own premise required is **0.0525** — a
quantisation plateau where every lightness from 0.02 to 0.0525 renders `#000000`. So the
constant sat below the bound it existed to enforce. No test caught it. **No output
differed anywhere**, because the cases that would have exposed it never arose: raising it
to 0.08 changed no colour across 58,968 resolutions, while setting it to zero changes 30.

Honest code, an unexamined premise, and no wrong answer to point at. It was findable only
by asking what the number is *supposed* to bound and measuring that — which is the same
move as running a check against a case whose answer you already know, applied to a
constant instead of to a test.

### §7-v2 addendum (2026-08-31, third): mark provenance at the moment of sending

Two figures went out of this session in one message, presented identically: `0.0525`,
which had been independently derived twice and held exactly, and `0.067`, which had been
relayed from a report and held nothing. The reader could not tell them apart **because
the sender had not marked them apart.**

The obvious rule — verify before relaying — is the wrong one. Verification is expensive,
sometimes impossible, and the pressure to skip it is highest exactly when a result is
interesting. sheets-03's formulation is the one that works:

> **Mark provenance at the moment of sending, because that is the only moment you still
> remember which is which.**

They avoided the same error by accident rather than by discipline: sending seven hexes,
they split four verified-in-their-repo from three recalled from memory, because they
happened to know which was which as they typed. Gathered a day earlier and sent from
notes, all seven would have gone out looking equally solid.

Marking is free and takes a second. It also converts a private uncertainty into a public
one, which is the only form anybody else can act on — the three recalled hexes were then
verified against an independent source *because they were flagged*, and turned out to
carry the font colours that produced two findings neither team had.

**The corollary for retractions:** report the direction, not only the fact. The `0.067`
correction made its own argument *stronger* — the real gaps are larger, by an order of
magnitude at the extreme, so "no tolerance can work" became more true. A retraction that
weakens nothing is easy to under-report, and under-reporting it invites someone to
re-litigate a settled decision on the grounds that its evidence moved.

---

## 6-v2.10. Sign-in screens (2026-08-31) — requested by Paul

> "Another thing to add to the design system: sign in screens, including
> magic-link, username/password, and social signin ... `~/github.com/elevenmessenger/messenger`
> does this pretty well, and these are screens that pretty much every app will need."

He is right that every app needs them, and the gap is real: **`rastrillo/auth` renders no
HTML at all.** It is pure mechanism — `Begin`, `Callback`, `Verify`, `Signout`,
`RequireSession` — so today every app hand-writes its own sign-in screen against it, and
the framework has opinions about magic links, passkeys and passwords in `docs/site/` with
nothing to render them.

### What the reference actually does well, read from the source

Six things, and most of them are doctrine rather than markup.

1. **One primary door, not a wall of buttons.** The card composes to a single primary
   action — the last-used method where the browser remembers one, otherwise the platform
   default — with a second door beneath it and a quiet "Other ways to sign in" leading to
   the rest. Demoted doors do not render on the main card at all.
2. **Find-or-create, one button each. No sign-in versus sign-up split.** "Continue with
   Apple" is one door whether or not you have an account.
3. **Provider buttons built to each provider's own spec, by hand.** Apple's black,
   Google's white with the hairline and the official four-colour G, Microsoft's four
   squares, GitHub's dark. All inline SVG and no vendor JavaScript — their rendered
   buttons need their SDKs, and the CSP rightly blocks them. That constraint is
   rastrillo's too, and arriving at the same answer from a different rule.
4. **Steps swap in place.** The email door is a screen of its own: address → code →
   session, as forms, so Enter submits and native validation gates.
5. **A working button wears a face.** "The wait needs a face even when the server is
   instant", with a static ring under `prefers-reduced-motion`.
6. **Failure copy that serves both readings of an ambiguous failure.** A cancelled passkey
   ceremony and having no passkey are indistinguishable — `NotAllowedError` either way —
   so one explainer addresses a returning member who picked the wrong key *and* someone
   genuinely new. That is the sharpest thing on the page and it is entirely copy.

### The tension, and where it resolves

The reference composes the card **in JavaScript**, from a per-browser memory of the
last-used method. Rastrillo's doctrine is that the scriptless path is the real one. These
are compatible if the split is drawn correctly:

- **Server-rendered:** the card, the doors, the order, the step-through, every form, every
  error state. The app declares its default primary door; that renders with no script.
- **Enhancement:** remembering the last-used method for this browser, and promoting it.
  Per-viewer, non-essential, degrades to the app's configured default. This is exactly the
  shape `select.js` and `datetime.js` already have.

### The honesty constraint, which is Paul's own rule

`rastrillo/auth` implements magic links and the keymail OAuth ceremony. **It does not
implement Google, Apple, Microsoft or GitHub sign-in.** So the design system ships the
*buttons* — correctly branded, accessible, scriptless, pointing at whatever route the app
provides — and must not imply the framework implements the flows behind them.

That is §6-v2.3's rule applied before the fact rather than after: *published documentation
describes what exists.* A "Continue with Google" button in the gallery with no Google
support in `auth` is fine; a Getting-started sentence implying the framework signs you in
with Google is not.

### Open, for Paul

- **Do the screens live in `ui` as partials, or in `auth` as renderable pages?** Partials
  keep `auth` free of HTML and let an app compose its own screen; renderable pages are
  turnkey and match how `auth` already owns its routes. The tension is the same one
  `magic-links.md` calls "the framework's turnkey option".
- **Password sign-in is the one where shipping a screen is a position.** The framework
  documents passwords, and a design system with a polished username/password card
  encourages it. Worth being deliberate rather than complete.

---

## 6-v2.11. The preview width classes (2026-09-01) — AS BUILT

> Paul, with a screenshot of the Bare/field-text example: "the form
> element examples shouldn't be scaled .. only the shells ... the scaled
> form element examples make the examples look like they're teeny tiny.
> I think they should be scaled only if the iframe is smaller than the
> content, which for these examples is a much smaller breakpoint"

### The measurement that settles it

Every example on the site was laid out at a virtual 1200px. **This
gallery's own stage caps at 958px at every window width** — `[rst-page]`
has a `max-width` and the rail is a fixed 240px, so widening the window
past about 1280px adds nothing to the column. Measured in a real engine
across 700–1920px: the stage reads 958px at 1280, 1440, 1600 and 1920.

So `--ds-k` was `958/1200 = 0.798` on every desktop, permanently, for
every component in the gallery. Nothing was broken and nothing looked
broken. It looked small — 12.5px type rendered at 10px — which is a
thing a gallery of a design system must not do.

### Two width classes

`--ds-wd` is the width an example's desktop rendering is laid out at.

| class | `--ds-wd` | threshold | who |
|---|---|---|---|
| `.ds-view--page` | 1200px | 54rem | the four shell demos, the two shell idioms, the modal, the demo application |
| `.ds-view` | 900px | 40.5rem | everything else — 105 of the 114 examples |

The component class is the default, because it is right 92% of the time.
`page.go`'s `pageFrame()` sorts by anchor-id prefix (`shell-`,
`idiom-shell-`) plus two names no rule could derive (`demo-app`,
`idiom-modal`), so a fifth shell lands in the right class with no edit.

**900px, and not the 958px that would fill the column.** `tokens.css`
folds `.rst-m-hide` away and switches the shells to their narrow layout
at **800px**, so a "desktop" rendering under that width would quietly be
the MOBILE layout under a lit Desktop tab — the worst failure this
widget has. 900px clears that line by 100px and leaves 58px of slack the
gallery's own layout can spend without dragging every preview back under
a scale of 1. Coupling the number to 958 would have meant re-measuring
every entry in `previewHeights` the next time the rail or the column
moved.

### The threshold had to split with it

The stage width at which a rendering stops shrinking and starts panning
is `--ds-wd × --ds-kmin`, and `--ds-kmin` (0.72) protects a type size
rather than a width, so it stays one number for both classes. The
threshold does not: 1200 × 0.72 = 864px = 54rem, and 900 × 0.72 = 648px
= 40.5rem. A CSS query condition cannot name a custom property, so this
costs a second pair of `@container` queries — the one real price of the
change. The component threshold is *lower*, which is the point rather
than a side effect: a 900px page is still legible in a column where a
1200px one is not, so a component holds its desktop rendering for 216px
of stage longer than a shell does. One shared threshold would have had
to pick which class to be wrong for.

### What it cost the gates, and one that had to move

Every drive that named 1200px now reads the widget's own `--ds-wd`, so
the identity is asserted per class and a third class would need no edit.
`TestThePreviewDefaultIsMonotoneInStageWidth` sweeps both the Display
page (components) and the Overview (the demo application) and *fails if
it did not see both classes*, because each has its own query pair and a
one-page sweep would leave the other ungated.

**The calibration had to be hoisted, and the reason is worth keeping.**
That drive proved the `@supports` trig branch was live by finding a
frame the engine had scaled — and `--ds-k` resolves to a plain `1` in
the fallback branch too, so a reading of 1 proves nothing. The component
class made every widget on a component page read exactly 1, which is
simultaneously the whole point of the class and precisely the reading
that cannot calibrate anything. It now calibrates once, on the Overview,
where a 1200px page in a 958px stage is still scaled to 0.798.

`previewHeights` did **not** move. Re-measured at 900px to confirm it:
the slack already in the table covered the extra wrapping a narrower
page causes, and no frame is smaller than its content.

### Result

At 1280px and wider the Display page's thirty examples render at
`--ds-k = 1.000`: a field is a field, at its own size. Below 1184px they
scale as before, and under 648px of stage they clamp and pan.

---

## 6-v2.12. The stat band (2026-09-01) — AS BUILT, §6-v2.4's dashboard stats

§6-v2.4 ruled the shape and this builds it: **one component with a lead
cell, not two components.** `stat` is a partial (fixed keys) and
`rst-stats` is a class idiom (it wraps a body only the caller knows —
the same footing as `rst-box` and `rst-card`).

### What the shape decides

| | |
|---|---|
| `[rst-stats]` | the band: one card, `display: flex`, `flex-wrap` |
| `[rst-stat]` | a cell: `flex: 1 1 9rem`, hairline on the inline start of all but the first |
| `[rst-stat~="lead"]` | `flex-basis: 14rem`, number at 2.1rem instead of 1.5rem |
| `[rst-stat-label]` | the eyebrow, uppercase and tracked |
| `[rst-stat-num]` | tabular-nums, so a polled number does not jitter its neighbours |
| `[rst-stat-delta]` | the change, tinted by `rst-tone` |
| `[rst-stat-note]` | the comparison the delta is against |

Flex and not a grid, because a grid needs the caller to say how many
cells there are and any number has to work. The hairline is
`border-inline-start`, so it lands on the right in Arabic. The one
visual compromise is a wrapped row's first cell drawing a leading
hairline against the card edge — accepted, because the alternative is
`:nth-child` arithmetic against a column count nobody declared.

### The two rules that are the component's whole point

**The sign is in the text, and the partial does not add it.** A caller
passing `"12%"` for a fall would get a plus, so `Delta` carries its own
sign and a missing one is visibly wrong to everyone rather than
invisibly wrong to a colour-blind reader.

**`Tone` is never derived from the sign.** A fall is good news about
half the time a dashboard shows one — a shorter wait, fewer errors, a
smaller backlog — so a partial painting every minus red would be
confidently wrong about half its uses. Which direction is good is
product knowledge, and the product is the caller. Unset renders the
quiet muted grey, which is the honest answer where there is no opinion.

### Three things the build had to settle

**The contrast gate grew by two pairs.** The delta is the first place
in this system where a tone colours TEXT on a plain card rather than
filling a pill behind it, and `--rst-tone-positive-fg` was chosen to
clear 4.5:1 against `--rst-tone-positive-bg`, which is not
`--rst-surface`. Both new pairs are in the gate and all three themes
clear them in both schemes. **It owes the full 4.5:1 even though the
sign already carries the meaning:** 1.4.1 is about meaning, 1.4.3 is
about text, and this is text.

**`previewStyle` gained a mobile exception, and the band earned it.**
Mobile heights were one factor — 1.25× desktop — on the measured claim
that no sample grows more than 1.17×. That claim holds for components
with one column at both widths and cannot hold for one whose whole
shape is a ROW that becomes a COLUMN: a four-cell band is 170px of
strip and 439px of stack, 2.6×. Raising the desktop number until the
derived mobile one fitted would have put 180px of empty box under every
desktop rendering, paying for the phone with the page most readers are
on. `previewMobileHeights` overrides the factor for examples that
change axis, and for nothing else.

**A gate had to learn about qualified attributes.**
`TestIdiomClassesAreStyled` requires every attribute a styleguide sample
writes to have a selector in tokens.css, and the band is the first
sample to carry `rst-tone`. There is no bare `[rst-tone]` rule and there
should not be — every rule names the kind too,
`[rst-status][rst-tone~="positive"]` — so `rst-tone` is exempted by
name as an attribute that never appears alone. Its variants are still
checked, so `rst-tone="lavender"` still fails.

### And a trap in samples.go worth knowing about

The leak gate reads rendered bytes and cannot tell a sample's DATA from
the page's own voice. A stat labelled "Open requests" failed the whole
build — not because the label was wrong, but because the demo
application already says that sentence through `P`, which makes the
string a prose key that must not appear in English on a Japanese page.
Sample data now has to avoid wording the page says elsewhere; the rule
is recorded at the head of samples.go.

### The demo application lost four CSS rules

Its dashboard hand-rolled a three-up grid and two type rules for its
numbers. They are gone, replaced by the band. That is the point rather
than a tidy-up: a demo that hand-rolls a component the framework ships
is a demo quietly saying the framework does not ship it.

### Copy owed

Thirteen new English strings (twelve gallery, one demo) are drafted and
carry their eleven translations. **They have not been through the copy
review gate.** The English is the lookup key, so an edit costs eleven
redrafted translations — review before merge, not after.
