# 🤖 Templates and the UI vocabulary

You render with `html/template`. There is no template language of its
own, and `ui` is a component library — nothing in it generates a screen,
decides a route, or owns rendering.

## One template per page

```go
tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
	ParseFS(ui.Templates(), "*.html"))
tmpl = template.Must(tmpl.ParseFS(appTemplateFS, "templates/*.html"))
```

Parse layout plus one page per template, rather than one tree containing
everything. Two pages can then both define `"content"`; in one tree the
second `{{define "content"}}` would win for both.

`render.go` is also where `flash.Take(w, r)` gets called, once per page,
so the layout can render a notice. See [Forms](/docs/forms).

## Template functions

`ui.Funcs()` registers `dict`, `list`, `icon`, `iconAssets`, `T`, `Tf`
and `dateWords`.

Each partial takes exactly one data value, and `dict` is how you build
it at the call site:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

If you scaffolded your own icon set, point both icon seams at it:

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

`{{iconAssets}}` goes in the layout's `<head>`. It renders empty for the
vendored-inline default, so you can call it unconditionally and never
edit the layout when the delivery mode changes — see
[Icons](/docs/icons).

`ui.WithT` and `ui.FuncsWith` rebind `T` to a request-scoped lookup, so
a partial's built-in strings resolve in the request's locale. See
[Localization](/docs/localization).

## The partials

They span the list-screen, display, form and route families:

```text
back-nav      error-page       field-time          meter
badge         field            form-error          notice
bulk-bar      field-check      form-foot           page-header
callout       field-date       job-status          pagination
choice-field  field-daterange  list-bar            person
confirm-form  field-datetime   list-bar-search     seg-tabs
detail-list   field-select     list-row-action     status-pill
dropdown      field-text       list-search-submit
empty-state   field-textarea   locale-menu
```

`locale-menu` is the language switcher; see
[Localization](/docs/localization).

Each partial's file carries its data contract in a comment above the
`{{define}}`, and `ui_test.go`'s `TestAllPartialsAreDefined` is the
authoritative list.

### Three containers the partials assume

They belong to your page markup, so the library does not emit them:

```html
<div class="rst-page">   <!-- the centred content column every screen sits in -->
<div class="rst-list">   <!-- the card wrapping a list-bar and a run of rows -->
<form class="rst-form">  <!-- the column a run of fields and a form-foot sit in -->
```

There is also a class idiom vocabulary — section box, list grid,
dropdown, filter tokens, help tooltip, selection checkbox — that is CSS
rather than a Go partial. The `ui` package's doc comment has the full
list.

### Which card is which

Two of those containers look like cards and are not the card you want
for ordinary content. `rst-list` and `rst-card` have **no padding by
design**: they hold a run of rows, and each row pads itself. Put a form,
a paragraph, a strip of links or anything else that is not a row straight
into one and it renders flush against the border — the text touching the
edge is the tell.

The padded card for arbitrary content is `rst-box`, with its heading as
a sibling `rst-box-head` before it:

```html
<div class="rst-box-head"><h2>Sign in</h2></div>
<section class="rst-box">
  <form class="rst-form" method="post" action="/signin">…</form>
</section>
```

`rst-form` is a hook the form partials assume, not a container: it draws
nothing on its own, so it needs a `rst-box` (or the bare page) around it.

### Screens stack vertically

A screen is a column: page-header, then section-header + card, then the
next section-header + card, in reading order. Do not compose a heading, a
paragraph and a button side by side in a flex row — a three-word heading
wrapped onto three lines beside a full-width paragraph and a tall narrow
button is what that produces at any real width. A notice that needs a
call to action is either a `callout` whose body ends in a link, or a
`rst-box-head` (the `<h2>` plus one compact `rst-btn`) over a `rst-box`
holding the explanation. Horizontal arrangement is reserved for the
idioms that ship it: `rst-box-head`, `rst-field-row`, `rst-lbar`,
`rst-lrow` cells, `rst-seg-tabs`.

## Date and time fields

Four partials, each `field-text`'s envelope — label, hint, error, the
same `aria-describedby` wiring — around a native input:

| Partial | Input | Posts |
|---|---|---|
| `field-date` | `<input type="date">` | `2006-01-02` |
| `field-time` | `<input type="time">` | `15:04` |
| `field-datetime` | `<input type="datetime-local">` | `2006-01-02T15:04` |
| `field-daterange` | two of the above in a `rst-field-row` | both halves |

```html
{{template "field-datetime" dict "Name" "Starts" "Label" "Starts"
	"Value" .Fields.Starts "Required" true
	"Error" (T (index .Errors "Starts"))}}
```

The keys are the ones `field-text` takes — `Name`, `Label`, `Value`,
`Required`, `Hint`, `Error` — plus `Min` and `Max` in the same wire
format the field posts, and `Plain` to emit the bare native input with
no enhancement attributes at all.

`field-daterange` wraps two of those. `Start` and `End` are sub-dicts,
each carrying a whole single-field contract of its own. `Legend` names
the pair, and `LegendHidden` keeps that name for a screen reader while
dropping the visible heading. `Kind` picks the input both halves get —
`"datetime"` by default, or `"date"` — and `Seed` is described below.

**The two halves must have different `Name`s.** Each derives its input
id and its hint and error ids from its own `Name`, so a shared one
duplicates every id on the page and points both halves'
`aria-describedby` at the wrong messages.

`Seed` is a browser-side convenience, not a default: `"session"` moves
an empty or backwards end to an hour after the start, once the start is
committed. Nothing is seeded server-side, so a submission with scripts
off is exactly what the person typed.

Parse the other end with `form.Date`, `form.Time` or `form.DateTime`,
and check a range with `form.Range` — see [Forms](/docs/forms).

### What the enhancement adds

`datetime.js` turns an armed input into a combobox that reads
"tomorrow", "next fri 9am", "25 Dec 6pm" or "in 2 weeks". The native
input never leaves the DOM: it keeps its name and its wire value, so the
POST is byte-identical to the un-enhanced form and the server parses it
with the same code either way. With scripts off the field is an ordinary
date input, and the browser's own picker still opens.

It holds no English vocabulary. Not one word the parser matches on is
written in the file:

- **The relative words come from the catalog.** `{{dateWords}}`
  resolves all seventeen — today, tomorrow, next, in, ago, at, noon, am
  and the rest — through the bound `T` and encodes them as one JSON
  object on `data-rst-date-words`. One attribute, not seventeen. Because
  the helper is bound the same way `T` is, an app that rebinds `T` per
  request gets the request's language in the vocabulary too: a field
  enhanced in Japanese parses Japanese. A key may list several accepted
  spellings separated by `|`, and matching is case- and accent-folded.
- **Weekday and month names come from `Intl`**, in the page's own
  `lang`, along with the locale's digits folded to ASCII. So "3 mars",
  "3月3日" and "٣ مارس" all parse with no catalog entry behind them.
- **The visible strings ride out on `data-rst-date-*` attributes**, each
  resolved through `T` at render, because this markup is built in the
  browser where the catalog is out of reach. The file keeps English
  fallbacks for the eleven labels it puts on screen, the way `select.js`
  does, so a field that arrives without an attribute still works.
- **`{example}` and `{n}` are substituted in the browser**, not at
  render. The hint travels as its raw template so the example date is
  formatted by the locale's own formatter; the results line counts rows
  that only exist once the list is built.

Unreadable text is not a value. Type something the parser cannot read
and the field puts the old value back rather than guessing, so nothing
is committed that nobody chose. The picker button is a real labelled
button calling `showPicker()` — the browser's own calendar or clock
grid, rather than a hand-built one to keep accessible.

A range pairs by DOM order: two armed inputs inside one
`[data-rst-range]` wrapper are the start and the end, in that order. The
end's quick picks are relative to the start, and a time typed into the
end with no date lands on the start's day.

### The searchable select

`field-select` carries `data-rst-select` at ten options or more, and
`select.js` mirrors a filterable ARIA combobox onto it. Below ten,
search over a handful of items is furniture rather than help, so nothing
is emitted and the script finds nothing to enhance. `Plain` opts out at
any size.

`Options` is flat here, so `<optgroup>` is a hand-written-markup thing —
and `select.js` renders those groups rather than flattening them: each
optgroup becomes a `role="group"` with its label as the group's
accessible name, and loose options sit at the top level. A group
filtered down to nothing takes its heading with it.

A hand-written select opts out from the markup side with
`data-rst-select="false"`, which is never enhanced whatever its size.
This partial never emits it — `Plain` simply emits nothing — but
`select.js` honours it, so a select you wrote yourself can say no.

## The design system

Every partial, every state, every class idiom and all three shells,
rendered live for all three themes and all twelve base locales — one
page per theme × locale, plus a full-page demo for each shell. It lives
in-repo at `docs/design-system/`, generated by `internal/designsystem`
and committed like the rest of the docs; run `go generate ./...` after
any change to `ui` or its templates, and `TestDesignSystemIsCurrent`
fails the build if the tree drifts from what's committed. It will be
published at rastrillo.org/design-system once the website vendors it.

## Styling

`ui.TokensCSS()` is the design-token stylesheet, and `rastrillo new`
writes it once into your `static/` directory. From that moment it is
yours: edit it freely, and nothing in the framework will overwrite it.

The scaffold ships a `vendored_test.go` pinning the delivered copy
byte-identical to the library's, so you find out you have drifted when
you meant to, rather than at an upgrade. Delete or update that test when
you intend to diverge. See [Assets](/docs/assets).

Two stylesheets, not one. `tokens.css` is structure — layout, spacing,
radius, the type scale, and every `rst-` component class. A theme,
written beside it as `static/theme.css`, is the colour and the type
family those classes paint themselves with. The split is what makes a
restyle cheap: swapping one file changes how everything looks, and
nothing about how anything is laid out.

## Themes

Three ship, and `rastrillo new --theme=<name>` writes the one you pick:

| Theme  | The look                                                            |
|--------|---------------------------------------------------------------------|
| `ink`  | iron-gall violet on cool-violet neutrals (default)                  |
| `teal` | workbench teal on green-grey neutrals, monospace-leaning type       |
| `warm` | rust on cream paper neutrals — closer to letters than to a dashboard |

A theme file holds custom properties and a `color-scheme`, declared
three times: once for light, once under `prefers-color-scheme: dark`,
and once more under `[data-theme]` so an explicit toggle beats the OS in
both directions. Both modes are authored — the dark set is not the light
set inverted.

Each file carries its own contrast table in the header comment: every
text-on-background and border-on-background pair with the measured
ratio beside the WCAG 2.2 AA requirement it has to clear. `ui`'s
contrast gate recomputes every pair from the hex values in the file and
fails if one has dropped under its AA floor — 4.5:1 for text, 3:1 for a
control border. What it does not check is the printed number. A row can
go stale and the build stays green, so if you edit a colour, edit the
row: nothing else will.

Swapping in a theme of your own is replacing `static/theme.css`. The
only contract is the token set: declare every colour name `ink` declares
in each of the three blocks — `--rst-font` is declared once, in its own
`:root` — and every component class already knows what to do with it.
The scaffold's `vendored_test.go` pins `theme.css` to the library copy
exactly as it pins `tokens.css`, so delete its line there when the edit
is deliberate.

## Shells

The shell is the page frame — `templates/layout.html`, written once by
`rastrillo new --shell=<name>` and yours from then on:

| Shell     | The frame                                                                  |
|-----------|----------------------------------------------------------------------------|
| `column`  | a centred content column, no chrome (default)                              |
| `topbar`  | header bar: brand, nav, account menu, locale switcher, footer              |
| `sidebar` | a left rail of nav groups, collapsing to a `<details>` chrome bar below 800px |

Every shell defines `layout`, renders `{{template "content" .}}` for the
page's own body, and puts each piece of chrome in a block with a working
default. A page overrides only what it cares about, by redefining the
block:

```html
{{define "brand"}}<a class="rst-shell__brand" href="/">Notes</a>{{end}}
{{define "nav"}}
  <a href="/" aria-current="page">Notes</a>
  <a href="/archive">Archive</a>
{{end}}
{{define "account"}}
  <a href="/settings">Settings</a>
  <form method="post" action="/signout"><button type="submit">Sign out</button></form>
{{end}}
{{define "content"}}<h1>Your notes</h1>{{end}}
```

The blocks are `title`, `lang` and `dir` in all three shells, plus
`brand`, `nav`, `account` and `locale` in `topbar` and `sidebar`, and
`foot` in `topbar` only. None of them reads a field off the data, so a
shell renders whether your handler passes a struct, a `dict`-built map
or nil — a shell can never break because a page's view model changed
shape.

`account` is the one asymmetric block, and it is worth knowing which
shell you are in. In `topbar` the layout owns the `<details
class="rst-dropdown rst-shell__account">` and its summary, so your
`account` block is the **menu body only** — the links that go inside
`.rst-dropdown__menu`. In `sidebar` there is no dropdown: `account` is a
bare slot in the rail, and you supply the whole thing. Move a block
between shells and this is the edit you will need.

The chrome classes live in `tokens.css` like every other idiom:
`rst-shell-topbar`, `rst-shell__bar`, `rst-shell__brand`,
`rst-shell__nav`, `rst-shell__account` and `rst-shell__foot` for the
topbar; `rst-shell-sidebar`, `rst-shell__rail`, `rst-shell__chrome`,
`rst-shell__group` and `rst-shell__main` for the sidebar; and
`rst-skip`, the skip link, which all three shells carry — `column`
included. The sidebar's mobile collapse is that
`<details class="rst-shell__chrome">` and nothing else — no JavaScript,
like every other idiom here.

The `locale` block is where the language switcher goes, and
`locale-menu` is the partial that fills it:

```html
{{define "locale"}}{{template "locale-menu" dict "Items" .Locales "Return" .Path}}{{end}}
```

It renders nothing when `Items` is empty, so a one-locale app can wire
it and forget it. It sits on `rst-dropdown rst-locale` — the ordinary
dropdown vocabulary, not a shell-specific class — so it looks and
behaves the same in either shell. See
[Localization](/docs/localization).

## Error pages

`ui`'s `error-page` partial is the whole body of an error response: the
status, one honest sentence, a way back to somewhere real, and — for a
500 — the reference the operator will grep for. It renders *inside your
shell*, so the nav and the account menu are still there and the user is
not stranded on a bare page.

`rastrillo new` scaffolds it as `templates/errors.html`:

```html
{{define "content"}}{{template "error-page" dict "Status" .Status "Ref" .Ref}}{{end}}
```

and points `Options.ErrorPage` at the `render.go` helper that renders
it. Wire the same function to `Ctx.ErrorPage` and the 500 a handler
answers looks identical to the 500 a panic answers:

```go
func ErrorPage(w http.ResponseWriter, r *http.Request, status int, ref string) {
	w.WriteHeader(status)
	render(w, "errors", map[string]any{"Status": status, "Ref": ref})
}
```

The callback owns the status code as well as the body, so it calls
`WriteHeader` itself.

The partial words five statuses from the framework catalog — 404, 403,
422, 500 and 503 — in all twelve base languages. Any other status falls
back to a generic title and sentence rather than rendering a missing
key's name, so handing it a 418 produces a real page. `Title` and `Body`
override the catalog when you want your own wording, and `HomeHref`
moves the "Start page" link.

`Ref` renders only when it is set. A 500 has one — six lowercase base32
characters, minted by [`rastrillo.NewRef`](/docs/reference/rastrillo),
written to the log line under `ref` and shown on the page — because that
is the string a person quotes down a phone line and you grep for. A 404
has nothing to look up later, so it shows no reference.

`BackHref` renders a second, secondary link, and the rule is that **you
supply it and only from a `Referer` you have checked is same-site**.
There is deliberately no `javascript:history.back()` in the partial:
nothing here needs JavaScript to be usable, and an unvalidated `Referer`
is an open redirect with better manners. Leave it unset and the page
still has its "Start page" link.

Not everyone gets the page. A client that sent `Accept:
application/json` gets `{"status":500,"ref":"k3f9tq"}` from the view
helpers instead — `ref` is omitted when there is none, so a 404 answers
`{"status":404}`. Your `ErrorPage` callback is not consulted at all on
that path: it renders HTML, and the caller asked for something it can
parse. The panic-recovery path is the exception, and serves the HTML
page whatever the `Accept` header says. The callback receives `r`, so an
app that wants to sniff it can.

## The view helpers

For generated actions working against a `*rastrillo.Ctx`:

```go
func Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any)
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, what string, err error)
func NotFound(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
func Forbidden(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
func ParseID(r *http.Request) (int64, bool)
```

`Fail` logs the real error and answers a safe 500, so the detail reaches
your logs and never the response body; `NotFound` and `Forbidden` are
the same answer at 404 and 403. All three render through
`Ctx.ErrorPage` when it is wired, which is what puts the error page
above on the screen. `ParseID` reads the `{id}` path value; a malformed
one answers `false`, which your handler should turn into a 404 rather
than a 400. See [Scoping](/docs/scoping).
