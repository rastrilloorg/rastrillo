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
- The date parser has **no English in it**. Its relative-word
  vocabulary comes from the catalog; its weekday and month names come
  from `Intl.DateTimeFormat` for the page's `lang`.
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

The shell classes — `rst-shell-topbar`, `rst-shell-sidebar`,
`rst-shell__rail`, `rst-shell__chrome`, `rst-shell__menu` — live in
`tokens.css` beside the other class idioms, with `styleguideSamples`
entries so `TestIdiomClassesAreStyled` covers them. Mobile collapse of
the sidebar is `<details class="rst-shell__chrome">` — no JavaScript.

Built-in strings — skip link, "Menu", "Account", "Language" — are
`{{T "rastrillo.ui.shell_skip"}}` etc. The `<html>` element carries
`lang` and `dir` from the request: the scaffold's render helper passes
them in `data`; `ui` exports `Dir(locale) string` (`"rtl"` for `ar`,
`fa`, `he`, `ur`; `"ltr"` otherwise) so an app never guesses.

### 2.3 Delivery

`rastrillo new --shell=column` (default `column`) writes the chosen
`templates/layout.html`. It is a template, so it is app-owned and
unpinned — an app edits its layout on day one. `--theme` and `--shell`
join `--icons`, `--icon-delivery` and `--ux` in the usage line and are
validated before any file is written.

### 2.4 The language switcher

A `<details class="rst-shell__menu rst-locale">` listing the app's
declared locales. Each item is a link to the **same path under that
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

`NewLocales` takes the map; a declared locale with no framework catalog
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
  `date_noon`, `date_midnight`, `date_am`, `date_pm`. A key may hold
  several accepted spellings separated by `|` ("tomorrow|tmrw"), and
  the matcher is accent- and case-folded (`NFD`, strip marks, lower),
  so "amárach" and "amarach" both parse.
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
`date_quick_next_monday`, `date_quick_week`, `date_quick_plus_1h`,
`date_quick_plus_2h`, `date_quick_end_of_day`, `date_quick_next_day`.

### 4.3 Testing the parser

Tito Go's Node harness comes across: `ui/datetime_node.mjs` loads the
parser without a DOM and `ui/datetime_test.go` runs it under `node`
(skipped, loudly, when `node` is absent — the browser rig has the same
rule). Fixtures are one TOML table per shipped locale, each ≥20 cases
("tomorrow 9am", "next fri", "in 2 weeks", "25 dec 6pm", a bare year,
an unparsable string that must yield nothing) with the catalog's own
vocabulary. Twelve fixture files; a locale without one fails the gate.
The `en` fixtures include Tito Go's regression cases verbatim.

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

### 4.5 The filterable select

Already shipped: `field-select` past ten options carries
`data-rst-select`, and `select.js` mirrors a filterable ARIA combobox
onto it. Two things from Tito Go's `searchable-select.js` come across:

- Today `select.js` flattens a select's `<optgroup>`s, silently
  dropping headings the author wrote — exactly when the list is long
  enough for them to matter. Tito Go refuses to enhance such a select;
  rastrillo will instead render the listbox with a `role="group"` and
  a labelled heading per optgroup, so grouped selects are enhanced
  rather than skipped.
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
hidden) and **Start page** (`HomeHref`). A 500 shows `Ref` in a muted
monospace line ("Reference: k7f2q9") — the same id the server logged,
so support can find the log line from what the user quotes. Nothing
about the error itself is ever shown: no stack, no message, no path.

Keys: `error_<status>_title`, `error_<status>_body` for the five
statuses above, `error_generic_title`/`_body` for any other, `error_back`,
`error_home`, `error_ref` ("Reference: {ref}").

### 4b.2 The plumbing

- `rastrillo.Ctx` gains `ErrorPage func(w http.ResponseWriter, r *http.Request, status int, ref string)` — set by the scaffold's render helper to render `error-page` inside the layout. Nil falls back to today's text.
- `view.Fail` mints a `ref` (6 chars, base32 of 4 random bytes), logs it beside the error, and calls `ctx.ErrorPage(w, r, 500, ref)`.
- `view.NotFound(ctx, w, r)` and `view.Forbidden(ctx, w, r)` replace the bare `http.NotFound`/`http.Error` calls in the generated actions and the auth/password/passkey packages where a `Ctx` is in reach. Sites without a `Ctx` (the framework's own `/healthz`-tier routes) stay plain — they are never a user's screen.
- `rastrillo.Serve` gains panic recovery: a recovered panic logs the stack with a ref and renders the 500 page through `Options.ErrorPage` (the same function, hoisted to Options so the recovery wrapper outside any `Ctx` can reach it). Today a panic is a dropped connection.
- The scaffold's `templates/errors.html` defines `content` for `error-page` so an app can restyle it by editing a file it owns. The three shells render it at full page; the design-system page shows all five statuses in every theme.
- `Accept: application/json` requests get `{"status":404,"ref":"…"}` — the generated actions' JSON paths already exist and just gain the ref.

## 5. rastrillo.org/design-system

### 5.1 What is on it

One page per theme × locale, `index.html` for `ink`/`en`, showing:

- **Tokens** — every custom property as a swatch, with the contrast
  table rendered from the theme's header, and the type scale.
- **Every partial**, rendered with sample data, in every state it has
  (tones, required, error, help, disabled, enhanced and `Plain`),
  including the five error pages.
- **Every class idiom** from `styleguideSamples` — box, list grid,
  dropdown, form layout, toggle block, modal, help, selection box,
  and the three shells' chrome.
- **The three shells** at full page, each its own URL
  (`shells/topbar.html`), shown inline in an `<iframe>` and linked.
- **The date and time fields and the filterable select** live — this
  is the one place rastrillo.org serves JavaScript, and only here.
- **The rules** from PR #98 — which card is padded, screens stack
  vertically — beside the components they govern, with the wrong
  version shown struck through.
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

```
docs/design-system/
  index.html                 ink, en
  <theme>/<locale>/index.html
  <theme>/<locale>/shells/{column,topbar,sidebar}.html
  tokens.css  theme-{ink,teal,warm}.css  select.js  datetime.js  rastrillo.js
```

36 pages plus 108 shell pages; each is ~100 KB; the tree is ~15 MB and
committed. That is the price of no-Go-on-the-website, and it is
reviewable: a partial change shows as a diff in every page that renders
it, which is the point.

Gate `TestDesignSystemIsCurrent` renders to memory and diffs against
the committed tree, failing with the first differing path. `go
generate` is the fix. The existing docsite gates (links, anchors,
fences) are extended to the HTML pages for `/docs/` links only.

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
