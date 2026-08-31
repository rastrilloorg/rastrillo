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

`ui.Funcs()` registers `dict`, `list`, `menuGroup`, `searchClear`,
`icon`, `iconAssets`, `T`, `Tf` and `dateWords`.

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

### The elements they use

Where HTML has an element for what a component is, the component is that
element rather than a div wearing a role:

| Idiom | Element |
|---|---|
| `list-bar-search` | `<search>` around the `method="get"` form |
| `pagination`, `seg-tabs`, `back-nav` | `<nav>` |
| the modal panel | `<dialog open>` |
| every menu | `<details>` / `<summary>` |

`<search>` carries the search role itself, so the form inside it no
longer sets `role="search"` — two nested search landmarks for one search
box helps nobody. The strip sizes the landmark (`[rst-lbar] > search`) as
well as a bare form, so hand-written markup keeps the layout it had.

### Clearing a search

When `list-bar-search` has a `Query`, it also renders a ✕ beside the
field, and that ✕ is a **link**: the same `Action`, carrying every
`Hidden` pair, with `q` dropped. A real navigation, so it works with
JavaScript off, it is bookmarkable, and Back behaves.

The browser's own ✕ is suppressed in `tokens.css`, on purpose. It is
`::-webkit-search-cancel-button` and it does exactly what it is
specified to do — it clears the input's *value*. A `method="get"` form
submits on submit, so nothing else happens: the results stand and the
address bar still says `?q=`. Leaving it beside a link that really does
clear the search would be two affordances, one of which lies.

Filters and sort survive a clear. They ride in `Hidden`, and clearing
the search is not resetting the screen.

One pair it *can* decide about is `q` itself. An app that carries its
whole query string across the GET hands `q` back in `Hidden`, and
carrying that into the clear link would hand the reader a ✕ that puts
the search back — the exact lie this replaced. It is dropped.

**Pagination is the one the framework cannot decide.** `Hidden` is
opaque name/value pairs — nothing in it says which pair is the page — so
the default carries all of them, page number included, and a page number
from a searched result set is usually meaningless once the search is
gone. `ClearHref` is the hook, on both `list-bar-search` and `list-bar`:

```html
{{template "list-bar" dict
    "SearchAction" "/posts" "Query" .Query "Hidden" .Carry
    "ClearHref" "/posts"}}
```

Anything you pass wins over the computed default.

The modal panel is a `<dialog open>`, which is not a change of idiom: a
rendered-open, non-modal dialog is exactly what a modal-as-a-URL already
was. Nothing calls `showModal()`, so nothing enters the top layer,
`::backdrop` never paints, and the `rst-modal-overlay` div stays the
scrim. `tokens.css` undoes the browser's own dialog block — absolute
positioning, auto margins, `1em` of padding — so the panel lays out as
before.

Three things stay divs on purpose. The `rst-lrow` grid is a **layout
grid**, not a table: its columns come from one `--rst-cols` custom
property on the card, and CSS grid on real `<table>` markup means
`display: grid` on the table and its rows, which throws away the table
semantics you converted for. Use a `<table>` when the content is a data
table you want announced as one; `rst-lrow` is for list screens whose
rows are links. A list row (`list-row-action`'s `rst-row`) is a div for
the same reason — it lives in a `rst-list` card your own page markup
writes, and a `<li>` needs a list around it that the partial does not
own.

`job-status`'s `rst-job` is the third, and the reason is behaviour
rather than structure: the shim replaces that element **wholesale** on
every poll, and whether a live region whose host node keeps being
swapped out still announces is a question for a browser, not a
judgement call. It stays a plain div until someone drives it.

### Menus close each other

Every `dropdown`, row menu and `locale-menu` the library emits carries
`name="rst-menus"`, so opening one closes whichever was open. That is
the native `<details name>` group and costs no JavaScript. Pass
`MenuGroup` to `dropdown`, `locale-menu` or `bulk-bar` to put a menu in
a group of its own.

A nested `rst-menu-group` **must** use a different name from the menu
around it. `<details name>` exclusivity is document-wide, not
sibling-scoped, so a submenu sharing its parent's group closes that
parent the moment it opens.

The sidebar shell's `rst-shell-chrome` strip and the toggle-block are
deliberately outside the group: neither is a menu, and closing the
narrow-screen nav rail because someone opened a filter would take the
navigation away.

Closing an open menu on a click elsewhere, or on Escape, is the one part
native `<details>` cannot express, so `rastrillo.js` does it — two
delegated listeners on the document, no per-element wiring, menus that
arrive in a polled fragment covered for free. Delete that section of the
file to opt out; with no script at all the menus still open, still close
on a second click of their own summary, and still keep one open at a
time.

### A button that changes something says so

Every submit button in every form gets a loading state on its way out:
`aria-busy="true"`, a spinner before the label, and — a tick later, once
the submission is under way — `disabled`. `rastrillo.js` does it by
default, with no attribute to remember. A button that only *reveals*
something gets nothing: a disclosure, a dropdown, a tab is not doing
work, and dressing it as though it were is a lie the reader has to learn
to ignore.

Only the button that was clicked. The others in the same form keep their
`name` and their `value`, so a Save / Save-draft pair still tells your
handler which one it was. The form is what carries the guard: a second
click, Enter in a field and a programmatic `requestSubmit()` all arrive
at the same place and are all refused while the first submission is out.

| Attribute | On | Effect |
|---|---|---|
| `data-busy="false"` | the `<form>` | the whole form opts out |
| `data-busy="false"` | one submit button | that button opts out; the form is still guarded |
| `data-busy-label="Saving…"` | either | replaces the button's text while it works |

Three things the rule deliberately does not do. It does not touch a form
whose `target` sends the result somewhere else — that page is not going
anywhere, so it has nothing to be busy about. It does not touch the
standard close button inside a `<dialog>` — `<form method="dialog">`, or
a `<button formmethod="dialog">` inside an ordinary form, since the
button's attribute beats the form's — for the same reason and a sharper
one: that submit closes the dialog and leaves the page exactly where it
stood, so a guard armed there would never be cleared and the dialog
could never be closed through its form again. And it hands the form back
if something downstream cancels the submit: an app handler that calls
`preventDefault()` to do the work itself owns the feedback too.

One state it cannot get itself out of. A submission that never navigates
— a `204` or `205`, or a response the browser hands straight to the
downloads shelf via `Content-Disposition` — leaves the page exactly as
the submit left it, so the button stays disabled with no timeout behind
it to rescue it. There is no honest general fix: a timer would either
fire while a slow save was still running or be so long it never helped.
`data-busy="false"` on that form is the escape hatch, and the same goes
for anything that posts and stays put. Worth knowing while you are
there: `disabled` on the focused button moves focus to `<body>`, so a
keyboard user tabs from the top of the document, which is another reason
a form that stays put should opt out.

Going back is handled. The back/forward cache restores a page exactly as
it was left, spinner and all, so the shim clears every busy form on
`pageshow` — button re-enabled, label restored, spinner removed, guard
released. That includes a submit button that belongs to the form through
`form="id"` while living somewhere else on the page, which is the shape
a sticky header's Save takes. A form you came back to is a form you can
submit again.

The spinner is `[rst-spin]`, the same ring `job-status` wears, and it
stops turning under `prefers-reduced-motion` — the ring stays, dimmed,
because the message is "working", not "look at this". An
`<input type="submit">` gets the attributes and the label swap but no
spinner: it is a void element with nowhere to put one. Prefer
`<button type="submit">`, which is what `form-foot` emits.

This is an enhancement and nothing more. See
[Forms](/docs/forms/#the-busy-button-is-not-a-guarantee) for what it
does not promise you.

### Three containers the partials assume

They belong to your page markup, so the library does not emit them:

```html
<div rst-page>   <!-- the centred content column every screen sits in -->
<div rst-list>   <!-- the card wrapping a list-bar and a run of rows -->
<form rst-form>  <!-- the column a run of fields and a form-foot sit in -->
```

There is also a markup idiom vocabulary — section box, list grid,
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
<div rst-box-head><h2>Sign in</h2></div>
<section rst-box>
  <form rst-form method="post" action="/signin">…</form>
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

### State is never colour alone

A tone tells you how to feel about a value; it never carries the value.
`status-pill` always renders its label, `meter` always prints its
fraction as text beside the bar, and `badge` is a word before it is a
colour — so a reader who cannot separate your positive green from your
negative red still reads the same screen you do. `callout` with `Alert`
adds `role="alert"`, which interrupts a screen reader mid-sentence:
reserve it for a problem happening now, and leave ambient notes as the
ordinary tones.

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

Every partial, every state, every markup idiom and all four shells,
rendered live for all three themes and all twelve base locales — five
pages per theme × locale, one per section, plus a full-page demo for
each shell and one for the modal route. It is live at
rastrillo.org/design-system.

The gallery is generated by `internal/designsystem` and is not kept in
the repository: it is 20 MB of machine output, rewritten whole every
time a `ui` template changes. The site builds it at deploy time by
running `cmd/dsgen` against the version of the framework the docs were
vendored from, so the gallery documents the version the prose describes:

```
go run github.com/carlosframework/rastrillo/cmd/dsgen@<version> \
    -out src/design-system -mount /design-system
```

Two arguments and no others, deliberately: `-out` is where the files go,
`-mount` is the URL path they will be served from. Every link and asset
URL in the output is an absolute path under `-mount`, so a tree
generated for one path cannot be served at another.

To read the rendered pages yourself, `go generate ./...` writes the same
tree into `.design-system/`, which is git-ignored. Nothing reads that
copy back and no gate holds it to anything — with no committed tree
there is nothing that can fall out of date, so there is no longer a
freshness gate to run.

Every page is laid out in the `sidebar` shell, because that shell is one
of the things the gallery exists to show. The rail is the same on all
five: a search box over a nav that links every section, every partial
and every markup idiom in the whole gallery, with the section you are
reading expanded and the rest folded away —
`TestTheSidebarLinksEverythingOnThePageExactlyOnce` derives that list
from the same markers the coverage gates read, so a new partial shows up
there without anyone touching it. Typing in the box hides the entries
that do not match, and any section that empties out. Below 800px the
rail folds into the shell's own `<details>` chrome strip, with no script
involved.

The reader-facing names for two of those sections are not the code's.
The gallery calls them **Components** and **UI primitives**; `ui`
returns partials and this page calls them partials, and the Components
page says so in a sentence of its own.

**Every word on the page is translated**, not only the components. The
gallery's headings, leads and notes come from a catalog of its own, in
the same twelve locales the framework ships, so the language switcher
changes the prose as well as the samples.
`TestEveryProseKeyIsTranslated` fails on a missing entry, and
`TestNoEnglishProseReachesATranslatedPage` fails on an English string
that reached a page in another language.

Sample content is the deliberate exception. The names, routes and labels
inside a component sample are stand-ins, and translating them would
suggest the framework ships those words, so they stay English on every
page. The shell and modal demos go the other way — they impersonate a
real application, so their chrome speaks the language you picked. The
page says so itself, under Partials, in all twelve languages.

Every example on the page is shown three ways behind one control:
**Desktop**, **Mobile** and **Code**. The two previews are one
`<iframe>` holding a document of its own — the sample, the stylesheets,
and nothing else — laid out at a virtual 1200px or 390px and scaled
into whatever width you are reading at, so the desktop rendering is the
desktop rendering on a phone. The tabs are radio inputs and `:has()`;
no JavaScript is involved in switching them. Each preview is a window on
its document rather than a fit to it, so a tall sample scrolls inside
the box — and the box has a resize grip on its bottom edge. Drag it and
the frame takes its new height, which means you see more of the
document rather than more of the box.

Giving each sample a document of its own is what makes the awkward ones
work. The two shell frames carry their own `<main>` and the gallery
page already has one; the modal's overlay is `position: fixed`, so
rendered in the gallery it covered the gallery; a form's save bar is
`position: sticky`, so it stuck to the bottom of the gallery rather
than to its own form. Each of those is correct inside its own frame.
The shells and the modal keep their full-page demos as well — a shell
wants a window, and a modal's whole claim is that it is a URL — and
those links, like every "open the demo" link on the page, open in a new
tab.

The links inside a sample go nowhere on purpose. Sample markup is
written to read like a real application (`/posts/1/edit`), and this
site serves none of those routes, so every link is rewritten to `#`
before it is framed and every form is aimed at a hidden sink. The Code
tab beside the preview keeps the routes the sample was written with,
which are the ones worth copying.

Three switchers sit in the header, top right. **Theme** is three links,
one per theme, landing on the same page in that theme's palette.
**Colour scheme** is System / Light / Dark, and it is the only one of
the three that needs JavaScript: it writes `data-theme` on `<html>`,
remembers the choice in `localStorage`, and puts the same attribute on
every preview frame, because a colour scheme does not reach into an
embedded document that declares one of its own. **Language** is the
`locale-menu` dropdown, twelve entries, each keeping you on the page you
were reading.

The script behind that toggle is `gallery.js`, the only JavaScript in
the tree that is not part of the framework — furniture for the page
rather than something an app is ever given, which is why it lives beside
the renderer instead of in `ui`. It follows the same rules as its three
neighbours anyway:
first-party, no dependencies, no network. The scheme toggle and the
filter box are both `display: none` until the file sets
`data-rst-js`, so with scripts off you get the nav and the theme's own
`color-scheme: light dark` rather than two controls that cannot do
anything.

Every link in that tree — stylesheets, scripts, the theme and language
switchers, the shell and modal demos — is an absolute path under
`/design-system/`, so the tree is served from that path and no other.
Relative links looked more portable and were wrong: the static edge
serves `/design-system` and `/design-system/` as the same page without
redirecting between them, and a relative href resolves differently on
each.

The gallery is scanned to WCAG 2.2 AA on every CI run — the
`browser-tagged tests` job runs `./internal/designsystem/`, and a
violation fails the build. Locally it is `go test -tags browser -p 1
./internal/designsystem/` (the `-p 1` matters: two Chromium-heavy
packages starting together contend badly enough to blow a drive's
deadline). It injects a pinned copy of axe-core into a real browser and
runs it over the tree as the renderer produces it — the same bytes
`dsgen` writes, served from memory: the index in each of the three themes
in both colour schemes, an RTL page, the modal and a shell demo, and the
preview documents the components actually live in — those in every theme
and scheme too. Plus two checks axe cannot make: that nothing scrolls
sideways in a 320px viewport, and that a Tab through the page shows a
focus ring at every stop and never gets stuck.

That is a floor, not a certificate. Automated scanning reaches roughly
half of the WCAG success criteria, and the other half — whether alt text
says something true, whether the reading order makes sense, whether a
label means what it says — is read by a person. And it is a sample: six
pages of a hundred and eighty, eight previews of a hundred and ten,
chosen for what they would catch rather than for coverage.

## Styling

`ui.TokensCSS()` is the design-token stylesheet, and `rastrillo new`
writes it once into your `static/` directory. From that moment it is
yours: edit it freely, and nothing in the framework will overwrite it.

The scaffold ships a `vendored_test.go` pinning the delivered copy
byte-identical to the library's, so you find out you have drifted when
you meant to, rather than at an upgrade. Name the file in that test's
`vendoredIsMine` when you intend to diverge, and `rastrillo doctor
--fix` will re-copy the ones you have not. See [Assets](/docs/assets)
and [The CLI](/docs/cli).

Two stylesheets, not one. `tokens.css` is structure — layout, spacing,
the type scale, and every `rst-` component. A theme, written beside it
as `static/theme.css`, is the colour, the type family and the shape
those components paint themselves with. The split is what makes a
restyle cheap: swapping one file changes how everything looks, and
nothing about how anything is laid out.

### Menus that fit the window

Every menu surface — `[rst-dropdown-menu]` and `[rst-row-menu-panel]` —
is capped at `min(20rem, 100dvh - 6rem)` and scrolls past that, so a
long menu on a short window can still be reached. A twelve-locale
language menu is 388px, which is where this came from.

They also flip. `position-area` with `position-try-fallbacks` opens a
menu upward when there is no room below it, and the other way inline
when it is against the trailing edge. No script: this is CSS anchor
positioning, which is **Chromium-only today**, and that is a choice
rather than an oversight. An engine without it lands on the fixed
position every engine has today, so nothing regresses, and Firefox and
Safari gain the behaviour with no release from us. The alternative was
script, which would put positioning behind JavaScript and leave the
scriptless path worse than it is now.

### The scrollbar gutter

`tokens.css` sets `scrollbar-gutter: stable` on `html`, through a token:

```css
:root { --rst-scrollbar-gutter: stable; }
html  { scrollbar-gutter: var(--rst-scrollbar-gutter); }
```

The width a scrollbar takes is reserved whether or not a scrollbar is in
it, so moving between a short screen and a long one no longer slides the
whole page sideways — and neither does opening a modal, whose scroll
lock (`body:has([rst-backdrop]) { overflow: hidden }`) takes the
scrollbar away the instant it lands.

The opt-out is one line in your own stylesheet:

```css
:root { --rst-scrollbar-gutter: auto; }
```

What it costs: a page too short to scroll now reserves the strip too, so
there is a thin empty band at the trailing edge where there was none.
`both-edges` would double that band to keep centred content exactly
centred, and was not taken — these pages are already a max-width column
inside a wider ground.

Do not over-claim what this fixes. macOS overlay scrollbars take no
layout space at all, so on a default Mac there was never anything to
shift. It is real on Windows and Linux, on a Mac set to always show
scrollbars, and inside an iframe. Chrome 94+, Firefox 97+, Safari 18.2+;
an older engine ignores the declaration and behaves as it does today.

## Themes

Three ship, and `rastrillo new --theme=<name>` writes the one you pick:

| Theme    | The look                                                           |
|----------|--------------------------------------------------------------------|
| `day`    | an everyday blue on white and grey; soft corners (default)         |
| `plain`  | the skeleton: greyscale, system type, almost no shape              |
| `signal` | graphite and one live cobalt; milled corners, short dense shadows  |

A theme file is one `:root` block. It sets `color-scheme: light dark`,
then declares every colour once as `light-dark(<light>, <dark>)`;
single-valued tokens — the font stack, the radii — are written plain.
Two rules at the foot of the file are the whole explicit toggle:

```css
:root[data-theme="light"] { color-scheme: light; }
:root[data-theme="dark"] { color-scheme: dark; }
```

Setting `color-scheme` re-resolves every `light-dark()` in the file at
once, in both directions, so a toggle beats the OS without restating a
single colour. Both schemes are authored — the dark set is not the light
set inverted — and there is no second copy of the palette to keep in
sync.

`light-dark()` needs a recent engine: Chrome and Edge 123, Safari 17.5,
Firefox 120, all of them 2024. Older ones do not fall back to the light
half — they cannot parse the function at all, so every declaration using
it is dropped and the app renders with no palette rather than a
monochrome one. If you have to support an engine below that floor, write
a theme with a plain `:root` palette and a `prefers-color-scheme` block
instead; the token names are the only contract, so nothing else changes.

`tokens.css` has a floor of its own, lower and far less brittle. It uses
`:has()` — Chrome 105, Safari 15.4, Firefox 121 — and the `lh` unit,
Chrome 109, Safari 16.4, Firefox 120. On Chrome and Safari `lh` is the
newer of the two, which is why the phantom label that reserves a line in
a field row writes a `calc()` fallback ahead of its `1lh`; on Firefox
`:has()` arrived last, so no engine there ever sees that fallback.
Neither degrades badly, so take the highest of all three and the floor
for `tokens.css` plus a shipped theme is Chrome 123, Safari 17.5,
Firefox 121: `light-dark()` sets it everywhere except Firefox, where
`:has()` does.

Below that floor, be precise about what "does not apply" means, because
it is more than the selector. A selector list is invalid **as a whole**
if any selector in it is invalid, so an engine that cannot parse
`:has()` drops the entire rule — every declaration in it, including the
ones that had nothing to do with `:has()`, and including the ones the
other selectors in the list were carrying. Keep a `:has()` selector in a
rule of its own, or wrap it in `:is()`, whose list is forgiving. The
console shell's rail does the first: one rule states the frame and
always applies, and a second, `:has()`-only rule fights for the single
declaration that the narrow layout contests. That is what makes its
degradation a choice — the rail stays visible, the wide frame is
untouched — rather than a coincidence.

One caveat worth knowing: `light-dark()` is a colour function, so it may
only stand where a colour is expected. A shadow token wraps its colour
and writes the geometry outside the call —
`0 8px 24px light-dark(a, b)`, never `light-dark(0 8px 24px a, …)`.

Shape is part of the theme, not the structure. `--rst-radius`,
`--rst-radius-sm`, `--rst-radius-pill` and the four depth tokens
(`--rst-shadow-pop`, `--rst-shadow-knob`, `--rst-shadow-lift`,
`--rst-overlay`) live in the theme file, which is why `plain` can be
nearly square and `signal` can be milled while `tokens.css` stays the
same bytes.

Each file carries its own contrast table in the header comment: every
text-on-background and border-on-background pair, in both schemes, with
the measured ratio beside the WCAG 2.2 AA requirement it has to clear.
`ui`'s contrast gate splits every `light-dark()` back into two tables and
recomputes every pair, failing if one has dropped under its AA floor —
4.5:1 for text, 3:1 for a control border. What it does not check is the
printed number. A row can go stale and the build stays green, so if you
edit a colour, edit the row: nothing else will.

Swapping in a theme of your own is replacing `static/theme.css`. The
only contract is the token set: declare every name `day` declares, and
every component already knows what to do with it. The scaffold's
`vendored_test.go` pins `theme.css` to the library copy exactly as it
pins `tokens.css`, so name it in `vendoredIsMine` when the edit is
deliberate. `rastrillo doctor` never diffs a theme it cannot identify —
a theme of your own reads as "custom or drifted", not as damage.

## Shells

The shell is the page frame — `templates/layout.html`, written once by
`rastrillo new --shell=<name>` and yours from then on:

| Shell     | The frame                                                                  |
|-----------|----------------------------------------------------------------------------|
| `column`  | a centred content column, no chrome (default)                              |
| `topbar`  | header bar: brand, nav, account menu, locale switcher, footer              |
| `sidebar` | a left rail of nav groups, collapsing to a `<details>` chrome bar below 800px |
| `console` | both at once: a brand-and-account bar across the top with the rail beneath it down the side, the two folding behind one disclosure below 800px |

Every shell defines `layout`, renders `{{template "content" .}}` for the
page's own body, and puts each piece of chrome in a block with a working
default. A page overrides only what it cares about, by redefining the
block:

```html
{{define "brand"}}<a rst-shell-brand href="/">Notes</a>{{end}}
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

The blocks are `title`, `lang`, `dir` and `head` in all four shells,
plus `brand`, `nav`, `account` and `locale` in `topbar`, `sidebar` and
`console`, and `foot` in `topbar` and `console`. None of them reads a field off the data, so
a shell renders whether your handler passes a struct, a `dict`-built map
or nil — a shell can never break because a page's view model changed
shape.

`head` is the odd one out: it is not chrome, it is your slot in
`<head>`. A favicon, an Open Graph tag, one more stylesheet, a script
that has to run before the body — override it and they go in, last in
the head, so your own CSS wins the ties it should win against
`tokens.css` and the theme.

`account` is the one asymmetric block, and it is worth knowing which
shell you are in. In `topbar` and `console` the layout owns the
`<details rst-dropdown rst-shell-account>` and its summary, so your
`account` block is the **menu body only** — the links that go inside
`[rst-dropdown-menu]`. In `sidebar` there is no dropdown: `account` is a
bare slot in the rail, and you supply the whole thing. Move a screen
between `topbar` and `console` and nothing changes; move it to or from
`sidebar` and this is the one edit you will need.

The chrome attributes live in `tokens.css` like every other idiom:
`rst-shell-topbar`, `rst-shell-bar`, `rst-shell-brand`,
`rst-shell-nav`, `rst-shell-account` and `rst-shell-foot` for the
topbar; `rst-shell-sidebar`, `rst-shell-rail`, `rst-shell-chrome`,
`rst-shell-group` and `rst-shell-main` for the sidebar;
`rst-shell-console` for the console, which reuses the bar, the rail and
the topbar's `rst-shell-menu` rather than naming anything of its own;
and `rst-skip`, the skip link, which all four shells carry — `column`
included. The sidebar's mobile collapse is that
`<details rst-shell-chrome>` and nothing else — no JavaScript,
like every other idiom here.

### The console folds two chromes behind one control

`console` is the only shell with two pieces of chrome to put away below
800px: the bar's tail and the rail. It puts them away with **one**
`<details rst-shell-menu>` rather than two, because two disclosures on a
phone is two things to learn. The disclosure gates its own next sibling
— the tail — with `+`, and gates the rail from the shell root with
`:has()`.

Both rules are written as *hide when closed* rather than *show when
open*, which is worth copying if you write chrome of your own. In a
browser without `:has()` the rail's rule never matches, so the rail
renders as a plain column of links under the bar: a longer page, and the
navigation still reachable. Written the other way round, the same
missing selector would be a phone with no way to navigate.

Nothing is reordered at any width. Grid places the bar and the rail by
named area, so the DOM order — bar, rail, page — is the reading order
and the focus order at 320px and at 1280px, in both directions of the
language.

### Upgrading: the topbar's tail is a level deeper

The topbar grew a narrow layout, and with it a wrapper. Below 800px its
`nav`, `account` and `locale` hide behind a `<details>`; above it,
`[rst-shell-tail]` is `display: contents`, so those three generate no
box of their own and lay out as flex items of `[rst-shell-bar]` exactly
as they did before — the rendering is unchanged at every width.

The DOM is not. They are now grandchildren of `[rst-shell-bar]` through
`[rst-shell-tail]`, and `display: contents` changes box generation, not
selector matching. If your own CSS or JavaScript reaches into the bar
with a child combinator:

```css
/* stops matching, at every width */
[rst-shell-bar] > [rst-shell-nav] { … }
```

use a descendant selector, or target the attribute on its own:

```css
[rst-shell-bar] [rst-shell-nav] { … }
[rst-shell-nav] { … }
```

The block names — `brand`, `nav`, `account`, `locale`, `foot` — did not
change, and a test holds them. Only the depth did.

The `locale` block is where the language switcher goes, and
`locale-menu` is the partial that fills it:

```html
{{define "locale"}}{{template "locale-menu" dict "Items" .Locales "Return" .Path}}{{end}}
```

It renders nothing when `Items` is empty, so a one-locale app can wire
it and forget it. It sits on `rst-dropdown rst-locale` — the ordinary
dropdown vocabulary, not a shell-specific name — so it looks and
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
