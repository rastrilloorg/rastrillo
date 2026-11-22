# Design system, locales and markup

**Use the shipped design system by default.** Its look is Fiesta's
(fiesta.rastrillo.org), pinned by the module; the `ui` partials write
its markup. Start with the shells, `ui` partials (skill/partials.md),
`rst-` attributes, tokens and a shipped theme. Customise with a thin
app-owned CSS layer loaded after `tokens.css` and `theme.css` through
the shell's `head` block. Prefer
`--rst-*` token overrides for colour, type and shape; use app-specific
classes for layout or components the system does not cover. Keep base
styles intact so framework fixes stay easy to adopt. An explicit user
design requirement wins. Every partial with its Go call: Rastrillo
components, rastrillo.org/components (built by `cmd/dsgen`, not
committed); `go generate ./...` renders a local copy into
`.design-system/`. Tokens and hand-written markup:
fiesta.rastrillo.org/design-system. docs/site/templates.md

## Markup rules

- **The vocabulary is attributes, not classes.** `<div rst-box>`,
  `<a rst-btn="primary">`, `<div rst-callout-body>`; `class` carries
  only `rst-sr-only`, `rst-mono`, `rst-m-hide`, `rst-grow`, `rst-nm`,
  `rst-danger`, `rst-cell-mut`. `rastrillo markup --fix` converts an app
  written the old way. `rst-list`/`rst-card` hold rows only (unpadded
  by design). Forms, prose, links go in `rst-box` with a sibling
  `rst-box-head`. Button sizes `sm`/default/`lg` compose with the
  variant.
- **No `style` attributes and no `<style>` blocks:** the baseline CSP
  blocks them, and the browser drops them without an error. For a grid
  card, give `rst-card` a class and set `--rst-cols` in your stylesheet.
  `make csp` finds them (an app made before it: add a `csp` target
  running `rastrillo csp` to `ci`, and `.amadan/ci.d/18-csp`).
- Screens stack vertically — never heading, paragraph and button in one
  flex row; a notice with a CTA is a `callout` ending in a link. State
  is never colour alone: a `status-pill`'s label and a `meter`'s
  fraction are always visible text; `Alert` (`role="alert"`) is for
  live problems, not ambient notes. A dashboard opens with `rst-stats`
  holding `stat` cells, one `rst-stat="lead"`. Put the sign in `Delta`
  and pass `Tone` yourself — a fall is good news about half the time.
- A name inline with other content needs `<bdi>`, or an RTL name draws
  the number beside it to its LEFT.
- Every menu is a `<details name="rst-menus">` (opening one closes the
  rest); `fiesta.js` closes any on outside click or Escape;
  `MenuGroup` names another group, and a nested `rst-menu-group` MUST
  name a different one or it closes its parent.
- A row that stands for an item is a link across its whole width
  through its one name link; never link only the name. Row actions use
  `row-menu`, and a destructive one is a link to its confirm page.
  Inside a bulk-selection form, row-menu items are links only.
- A page of pictures is `media-grid` (4:3 frames; `Fit "contain"` for
  logos and banners; `Select` for the bulk-bar's form; `Menu` for a
  kebab). `detail-list` takes `DateTime` beside `Value` for a moment
  (`<time>`).

## Localizer

`ui.Pages` is strict: start each body with `{{$L := localizer $}}` and
pass `$L` first to every locale function (`T`, `number`, `money`...)
and to partials as `"L" $L`, or the page fails; `render.go` takes `L`
from `ui.ForRequest(r)`. Format every displayed quantity with
`{{number $L .Count}}`, including stats, tables, totals and chart
labels. `stat.Value` formats numbers and plain integer strings
automatically; `detail-list.Value` formats numeric types. Pass
quantities as numbers, identifiers/years/versions as strings; never
group IDs, input values, URLs or machine attributes. `number` writes
from the locale's CLDR pack, exactly, never rounding: a decimal goes in
as `number.Decimal("1200.5")` (a plain string stays as given), and
words after it set the format: `"plain"` (no group marks), `"places" 2`
(pad the fraction), `"percent"` (50 is 50 %). In Go, `number.Format`.
Dates: `{{date $L .At "dayYear"}}` writes CLDR's pattern for the locale
(kinds `day`, `dayYear`, `short`, `date`, `dateYear`, `monthYear`,
`month`, `year`, `time`, `stamp`), in `.At`'s own zone. In Go,
`dates.For(locale)`: `Format`, `FirstDay`, `Heads`, `Order`; the browser
twin is `dates.Assets()`'s `dates.mjs`, fed `Client()` as JSON.
Write an app's own text as
English literals in the template; `{{T $L "key"}}` looks a catalog key
up (the `rastrillo.ui.*` keys a form error carries, say) and
`{{Tf $L "key" "name" .Name}}` fills `{name}` in it from name/value
pairs (or one map).

Locales: `locales`/`default_locale` in `manifest/app.toml`; `rastrillo
generate` writes `gen/locales`, linking their packs: pass its
`Locales`/`DefaultLocale` to `Resolve`. Boot refuses a locale whose
pack is unlinked; with `RASTRILLO_CHECK_CONFIG=1`, `Resolve` prints the
check and exits. Catalogs: `Options.LocaleFS`, flat TOML per code;
twelve ship translated. **Any locale you add must translate the
`rastrillo.ui.*` keys or `generate --check` fails.**
docs/site/localization.md

## Shells and colour scheme

**Pick the shell by the app's shape.** A single list and its detail
pages: `column`. Sections a person moves between: `sidebar`, `topbar`
or `console` (`topbar` for a few sections and a home page of its own,
`sidebar` for more or grouped sections, `console` for a tool with a bar
over its rail). A sign-in page: `stage`.

`rastrillo new --theme=day|plain|signal --shell=<shell>` writes
`static/theme.css` and `templates/layout.html`, app-owned from then on.
To change an existing app's shell, start from
`$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo)/ui/layouts/<shell>.html`
and carry the app's own edits in `layout.html` across; copied over
unread, they are lost. A `--auth=signin` app's Sign in/Sign out control
(`{{if not (opt $ "Base")}}…{{end}}`) moves into the account card,
right after `{{block "account" .}}{{end}}`, as its last item, with
`rst-btn` dropped and `data-busy="false"` on the sign-out form; in
`column` it follows the scheme block. The rest of the move (the Menu
page, `view`, `up`): "Upgrading to one header row" in
docs/site/templates.md. Never scaffold one with
a `rastrillo` from your PATH: an older one writes the old Menu card and
back strip.

**On a phone all three show the sections as an index page**, under one
header row: the way up, the app's name, the person's avatar. The index
is the home page in `sidebar` and `console`, and `/menu` in `topbar`
(`menu.html` with the `nav` and, for a desktop, a list of the same
links; a `GET /menu` route and action; `"menu"` in `render.go`'s list).
Mark it `{{define "view"}}index{{end}}`. Each section's nav link gets
`id="nav-x"` (on a phone the index's rows are the nav), and every other
page names its way back: `{{define "up"}}{{path $ "/#nav-x"}}{{end}}`
(`topbar`: `/menu#nav-x`). A page whose parent is not the index points
`up` at that parent and names it, `{{define "upLabel"}}Events{{end}}`,
plain text. **Never add a Menu button, hamburger or drawer**, and never
a second header row: the index is the phone's navigation.

`profile` holds who is signed in (avatar, name, email); `account` holds
only the card's links.

Colour scheme: the shells already offer Light, Dark and System
(`scheme-menu`, three icon buttons posting to `/_theme`; cookie
`rastrillo_scheme`; `ui.ForRequest` carries `Scheme`; the layout writes
`data-theme`), in the account card beside the language row (`column`:
beside the language menu). Never hand-roll a scheme control or move it
out of the card. docs/site/templates.md

## The CSS floor

**Modern CSS is the floor, not a hazard.** `tokens.css` plus a shipped
theme already require Chrome 123, Safari 17.5, Firefox 121, because
every theme colour is a `light-dark()`. Anything that shipped at or
below that floor is free, `oklch()` and `color-mix()` included, with
no hex twin and no `@supports`: an engine too old for `oklch()`
dropped the whole `light-dark()` palette several declarations earlier.
Self-contained means the CSS fetches nothing — no imports, no remote
assets, no webfont, held by `ui_test.go` — never that old engines are
supported. Go above the floor with `@supports`, or move the floor and
say so. The bar is a year in all three engines; `cssfloor_test.go`
fails once the floor has gone nine months unreviewed.
docs/site/templates.md
