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

A theme is colour and type: the light `:root` block, the
`@media (prefers-color-scheme: dark)` block, the two `[data-theme]`
override blocks, and `--rst-font`. Nothing structural. Today those
blocks live inside `ui/tokens.css`; they move out.

```
ui/tokens.css           structure only: spacing, radii, every component class
ui/themes/ink.css       the current palette, unchanged — iron-gall violet on cool-violet neutrals
ui/themes/teal.css      amadan's teal (#0b6e63) on green-grey neutrals, monospace-first --rst-font
ui/themes/warm.css      messenger's paper (#efe3d6 / #fbf6ee) neutrals, rust accent (~#a3452a), warm charcoal dark
```

Each theme file carries the same custom-property set — a gate asserts
the three declare identical property names — and its own WCAG 2.2 AA
table in the header, computed from its hex values.
`ui/contrast_test.go` parameterises over `ThemeNames()` and asserts
every documented pair; a new theme that fails a pair does not ship.

### 1.2 API

```go
func ThemeNames() []string            // "ink", "teal", "warm" — ink first, the default
func ThemeCSS(name string) ([]byte, bool)
```

### 1.3 Delivery

`rastrillo new --theme=ink` (default `ink`) writes `static/tokens.css`
and `static/theme.css`. The file is always named `theme.css`, whatever
theme it holds: the layout links one fixed name, and switching theme
later is replacing one small file. The scaffold's vendored-pin test
gains a line for `theme.css`, keyed to the chosen theme name recorded
in the test's map.

The examples keep `ink` and their pins.

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

One page per theme × locale, `index.html` for `ink`/`en`, showing:

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
  the page is itself the RTL and CJK proof.

Page prose is English; the components on it render in the selected
locale from the framework catalogs.

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
depth prefixes left, the root `index.html` and `ink/en/index.html` are
the same bytes; `TestRootIndexIsInkEnglishAtTheTreeRoot` asserts that
byte-identity outright rather than blanking hrefs to compare the rest
(as built, 2026-08-29).

```
docs/design-system/
  index.html                 ink, en
  <theme>/<locale>/index.html
  <theme>/<locale>/modal.html
  <theme>/<locale>/shells/{column,topbar,sidebar}.html
  tokens.css  theme-{ink,teal,warm}.css  select.js  datetime.js  rastrillo.js
```

~~36 pages plus 108 shell pages; each is ~100 KB; the tree is ~15 MB and
committed.~~ As built: 188 files (36 index pages + 36 modal demos + 108
shell pages + the root index + 7 shared assets), 4,427,216 bytes —
4.22 MiB, well under the ~15 MB estimate. `TestTreeStaysUnderTheSizeGate` holds the whole
rendered tree to a 20 MB ceiling, logging the exact byte count each run
(as built, 2026-08-29). Committed regardless of size; that is the price
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
17,836 today, so §7's bullets are trimmed to pay.

## 7. Out of scope

- A calendar-grid picker. `showPicker()` hands that to the browser,
  which does it better than a first-party grid would, in every locale.
- Themes beyond three, or a theme editor. A theme is a 90-line file;
  the page shows what one looks like.
- Translating the docs prose or the design-system page copy.
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
