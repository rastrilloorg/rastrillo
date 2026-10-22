# Design system, locales and markup

**Use Rastrillo's design system by default.** Start with its shells,
`ui` partials (skill/partials.md), `rst-` attributes, tokens and a
shipped theme. Customise with a thin app-owned CSS layer loaded after
`tokens.css` and `theme.css` through the shell's `head` block. Prefer
`--rst-*` token overrides for colour, type and shape; use app-specific
classes for layout or components the system does not cover. Keep base
styles intact so framework fixes stay easy to adopt. An explicit user
design requirement wins. Full vocabulary: rastrillo.org/design-system
(built from `ui` by `cmd/dsgen`, not committed); `go generate ./...`
renders a local copy into `.design-system/`. docs/site/templates.md

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
  rest); `rastrillo.js` closes any on outside click or Escape;
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
group IDs, input values, URLs or machine attributes. Keep percent
formatting explicit and locale-aware. Write an app's own text as
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

`rastrillo new --theme=day|plain|signal
--shell=column|topbar|sidebar|console|stage`: the theme lands as
`static/theme.css`, the shell as `templates/layout.html`, both
app-owned from then on. Shells on a phone: `topbar` and `console` put
their narrow chrome in the Menu card. `sidebar` and `console` rails
become an index page: mark it `{{define "view"}}index{{end}}`; every
other page names its way back with `{{define "up"}}/#nav-x{{end}}` and
the nav link gets `id="nav-x"`. Never build a hamburger drawer.
`profile` holds who is signed in (avatar, name, email); `account` holds
only links.

Colour scheme: every shell offers Light, Dark and System beside the
language menu (`scheme-menu` posts to `/_theme`; cookie
`rastrillo_scheme`; `ui.ForRequest` carries `Scheme`; the layout
writes `data-theme`). Do not hand-roll a toggle. docs/site/reference/ui.md

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
