# Mobile ergonomics: phone-sized type, 44px targets, whole-row links, and shells without a hamburger

Status: design, 2026-09-30. The operator chose each look from a
throwaway prototype; this spec turns those choices into the framework
and decides what the choices left open. Not yet reviewed.

Part H of the design-system iteration (F is the sign-in screen,
`2026-09-27-signin-screen-design.md`).

Citations: `P/` is the prototype the operator approved,
`/home/paulca/.cache/tmp/mobile-proto/` (generator `P/gen.py`, screenshots
in `P/shots/`). Everything else is this repository at `a7a24dd`. The
prototype's copy of `rastrillo.js` (16,364 bytes) predates the busy.js
split and is not the file this spec measures against.

## Why

On a phone, a rastrillo app reads like a desktop page someone shrank.

- **Type is 14px and targets are about 28px.** `tokens.css` sets
  `--rst-fs-base` to 14px on purpose (ui/tokens.css:78-81) and states its
  target sizes against WCAG 2.2's 24px floor (ui/tokens.css:63-68): row
  pills about 27px, buttons 28/34/44px, the row kebab 26px
  (ui/tokens.css:1061), a row checkbox 16px (ui/tokens.css:1500). Those
  are fine under a mouse and fiddly under a thumb.
- **The zoom fix shrinks the one field that was meant to be big.** The
  coarse-pointer floor (ui/tokens.css:2085-2092) sets
  `font-size: max(1rem, 1em)` on `[rst-input]` at the same specificity as
  `[rst-input~="primary"]` (ui/tokens.css:1279) and later in the file, so
  it wins. In `font-size`, `1em` is the *parent's* size (14px), so the
  17px primary field comes out 16px. The spike measured it.
- **Only one row idiom is clickable across its width.** `list-row-action`
  stretches its name link over the row (ui/tokens.css:672-683); the list
  grid, which most list screens use, is clickable only on the name text,
  though it already fills on hover as if the whole row were a link
  (ui/tokens.css:1045).
- **Two of the three navigation shells hide navigation behind a
  hamburger.** The sidebar folds its rail behind
  `<details rst-shell-chrome>` (ui/layouts/sidebar.html:30), and the
  topbar and console open their tails *into the page flow*, shoving the
  content down (ui/tokens.css:1747, 1889).
- **Phones still zoom in apps that have the fix in the library.** The
  zoom floor shipped in v0.26.0 (CHANGELOG.md:394-404) without a
  re-vendor line. §7 shows why several apps never took it.

## Decisions

Binding decisions from the operator, then the choices this spec made
where those left room. Each "chosen here" item says what it rejected.

- **Sizing is variant iii.** On small or touch screens the whole type
  scale moves up one step (base 16px) and every tap target is at least
  44×44px. Desktop density is unchanged. (P/v-base16.css,
  P/touch-targets.css; rejected: variant ii, controls only.)
- **The query is `(pointer: coarse), (max-width: 40rem)`**, as in the
  prototype (P/touch-targets.css:1-7). Justified in §1.1.
- **Sidebar shells use c3.** At narrow widths the rail is a full-screen
  index at a real URL; content pages carry a back control at the top
  left; a small script adds a slide, history reuse, focus return.
  Desktop is unchanged. (P/c3/, P/side.css, P/side-c3.css, P/side-c3.js;
  rejected: c1 without the script, c2's single-URL `:target` index.)
- **Topbar menu is the right-aligned floating card (d-right).** It
  overlays the page and never pushes it down; it closes on an outside tap
  with and without JavaScript and on Escape through rastrillo.js. The
  active item is marked by a blue background only; the prototype's left
  border (`box-shadow: inset 3px 0 0`, P/topmenu.css:52-56) is dropped.
- **Guidance discourages hamburger drawers.** Shells with a header use
  the dropdown card; sidebar shells use index and back.
- **Whole-row targets (operator addition).** A list item with a primary
  destination is clickable anywhere on the item, on desktop and mobile.
  Secondary actions sit above the target and follow the 44px rule.
- **A shipped row-actions menu (operator addition).** §3.
- **Chosen here: a page says it is the index with a block, not a data
  field or a route.** `{{define "view"}}index{{end}}`, and a content
  page names its way back with `{{define "up"}}/#nav-invoices{{end}}`.
  Rejected: a data field (a shell must render with nil data;
  ui/ui.go:419-422, and TestLayoutsParseAndRender executes every layout
  with nil); a route convention (no template can see the request path;
  there is no path func in ui/funcs.go, and "/" need not be the index).
  Blocks are how a page already talks to its shell: every page overrides
  `nav` to mark `aria-current` (docs/site/templates.md). §4.1.
- **Chosen here: the sidebar script is a new vendored file, `shell.js`,
  with its transition in `shell.css`; neither goes in rastrillo.js.**
  rastrillo.js is 9,784 bytes against its 16 KiB cap (ui/shim_test.go:
  262), not at the cap: the busy.js split freed 6.6 KB. It would fit.
  It goes in its own file anyway, because only two shells need it, and
  because the transition cannot live in tokens.css (§4.5). The
  light-dismiss change for the topbar card *does* go in rastrillo.js:
  it extends a section that is already there.
- **Chosen here: the console follows both patterns, one per chrome.** Its
  bar's tail becomes the floating card (it is a header); its rail
  becomes the index with a back control (it is a sidebar). §4.8.
- **Chosen here: the gallery's own frame keeps its drawer in this cut.**
  Its demo application and its shell previews, which render through
  `ui.Layout`, move to the new pattern. §4.9 says why, and it is the one
  item the operator may want to overrule.
- **Chosen here: the row menu is a standalone partial, `row-menu`,** and
  `list-row-action` gains an optional `Menu` key that calls it. On
  phones it stays an anchored card, not a bottom sheet. §3.
- **Prerender needs an operator call.** The prototype's inline
  `<script type="speculationrules">` (P/c3/index.html:15) is refused by
  the default CSP. §4.6 gives the CSP-clean design and asks.

## 1. Type and tap targets on small or touch screens

### 1.1 The query

```css
@media (pointer: coarse), (max-width: 40rem) { … }
```

- **`pointer: coarse`** is a phone or tablet at any width, including a
  phone in landscape (up to about 932px) and a tablet at 1024px or more.
  It is the *primary* pointer, so a touchscreen laptop driven by its
  trackpad keeps desktop density. `any-pointer: coarse` was rejected for
  that reason: one touchscreen would switch a mouse user's whole UI.
- **`max-width: 40rem`** (640px; a `rem` in a media query is the
  browser's initial font size, not the root's) catches a narrow desktop
  window and, above all, the gallery's Mobile tab: a 390px iframe on a
  desktop's fine pointer (internal/designsystem/gallery.css:328-342).
  Without this half the gallery would show desktop density on its Mobile
  tab, which is what the prototype found.
- **Why 40rem and not the shells' 800px.** Layout and density are
  different axes. Between 640 and 800px on a fine pointer is a split
  desktop window or a tablet with a mouse: it gets the narrow *layout*
  (one column, the index) but keeps the mouse's density. A phone never
  falls in that band in portrait, and in landscape the pointer half
  catches it.

One query, stated once in tokens.css. Every rule in §1, §2 and §3 that is
"touch only" sits inside it.

### 1.2 The scale

Inside the query the four type tokens move up one step
(P/v-base16.css):

| Token | Desktop (unchanged) | Small or touch |
|---|---|---|
| `--rst-fs-lg` | 1.0625rem, 17px | 1.1875rem, 19px |
| `--rst-fs-base` | 0.875rem, 14px | 1rem, 16px |
| `--rst-fs-sm` | 0.78125rem, 12.5px | 0.875rem, 14px |
| `--rst-fs-xs` | 0.71875rem, 11.5px | 0.8125rem, 13px |

Every component paints with these tokens, with four exceptions that
spell a token's value as a literal. Two are the xs step and move to
`var(--rst-fs-xs)`: the list-grid head row (ui/tokens.css:1044) and the
tooltip (ui/tokens.css:1486). Desktop output is identical because the
value is the same. The display sizes stay literal on purpose: page
titles at 1.375rem (ui/tokens.css:433, 1976, 2059), stat numbers
(ui/tokens.css:935, 941), avatar initials (ui/tokens.css:829, 970, 976).
On a phone the title is then 22px over 16px body text, which is the
hierarchy the approved screenshots show (P/shots/ab-sizing-base16-phone.png).

`--rst-tap: 2.75rem` (44px at the default root, and it tracks a raised
default) joins the scale block at :root (ui/tokens.css:77-111), so an
app's own CSS can use it. Only rules inside the query read it.

### 1.3 The zoom floor, and the bug in it

The floor stays: every text-entry control is at least 16px where a phone
would otherwise zoom on focus. Variant iii makes body text 16px, but
inputs inherit their size (`[rst-input]` sets `font: inherit`,
ui/tokens.css:1262), and some sit in a smaller parent: a bulk bar, a
menu panel, a date picker. So the floor is still needed. Two changes:

1. **The query widens** from `(pointer: coarse)` to §1.1's, so a narrow
   window and the gallery's Mobile tab show the floor too.
2. **The floor can no longer beat a field that is big on purpose.** It
   splits into two rules, because one spelling cannot serve both halves:
   - **Bare elements, at zero specificity.** `:where(input:not([type=checkbox]):not([type=radio]):not([type=range]), select, textarea)`
     with `font-size: max(1rem, 1em)`. An app's own input with no rst-
     attribute is sized by the browser's stylesheet, which any author
     rule beats, so `:where()` still lifts it; and any app rule that
     sizes it wins.
   - **The component selectors, at their own specificity, placed before
     the rules that size a field on purpose.** `.rst-input, [rst-input]`,
     `.rst-textarea, [rst-textarea]` and the search input move out of the
     block at ui/tokens.css:2085 into a query that sits directly after
     `.rst-input` (ui/tokens.css:1262) and before `[rst-input~="primary"]`
     (1279). Equal specificity, earlier in the file: the primary rule
     wins, and so does an app stylesheet loaded after tokens.css.

   Why not wrap the component selectors in `:where()` too, as the
   operator suggested: at zero specificity the floor loses to
   `.rst-input { font: inherit }` (0,1,0), because `font` resets
   `font-size`. The floor would switch itself off for every rastrillo
   input and phones would zoom again. Source order at equal specificity
   is the spelling that keeps the floor on and the primary field big.

On a phone the primary field is then `var(--rst-fs-lg)`, 19px. The
prototype's restatement (`max(1rem, var(--rst-fs-lg))`,
P/touch-targets.css:15-18) is not needed once the floor no longer
overrides it. The comment at ui/tokens.css:2064-2080 is rewritten to
name the 1em trap, since that is the bug the next reader would
reintroduce.

### 1.4 Tap targets

Inside the query, every control that is not a link in a sentence is at
least `var(--rst-tap)` on its smaller axis. The prototype's list
(P/touch-targets.css:20-52) is the starting set, written in both
spellings (§8) and extended by §2 and §3:

- Buttons (`[rst-btn]`, all sizes): `min-block-size` and `min-inline-size`.
- Inputs and selects: `min-block-size`; the search box and its clear
  link; the date picker's pick button, calendar nav and day cells.
- Menu items in every panel (`[rst-dropdown-menu]`, `[rst-row-menu-panel]`,
  `[rst-locale]`), combobox options, date-picker rows: `display: flex;
  align-items: center; min-block-size`.
- Row actions: the action pill, the kebab (§3), the row checkbox (§2.3),
  and the list grid's kebab column (`32px` → `var(--rst-tap)`,
  ui/tokens.css:1053-1056).
- Pagination chips, segmented tabs, the switch, the bulk bar's close
  and Actions controls (ui/tokens.css:1491, 1498).
- Shell controls: nav links, the Menu summary, the brand, the back
  control (§4).

**Exempt:** a link inside running text (inside `p` or `[rst-field-help]`,
not inside `nav`), per WCAG 2.5.8's inline exception. The prototype's
measuring script draws the same line (P/measure.js:24-27).

### 1.5 What does not change

Above 40rem under a fine pointer, every computed size is today's. That
is a test (§10.1), not a promise.

## 2. Whole-row targets

### 2.1 The rule

A row that represents an item with a destination has one **primary
link**, and that link's `::after` covers the row. Everything else in the
row that you can operate sits above the overlay. A row with no primary
link gets no overlay and no hover fill, so it never looks clickable.

This is the rule `list-row-action` already follows
(ui/partials/list-row-action.html:5-9, ui/tokens.css:672-683), extended
to every row idiom. Nested anchors stay impossible by construction: the
row is not a link, it contains one.

### 2.2 The idioms

| Idiom | Primary link | Today | Change |
|---|---|---|---|
| `list-row-action` (`[rst-row]`) | `[rst-row-main] > a` | Stretched | Status pill stops being a dead spot (below). Hover requires the link. |
| List grid (`[rst-lrow]`) | `> a.rst-nm`, or `> a[rst-person]` when a person is the identity cell | Name text only; row already fills on hover (ui/tokens.css:1045) | Gains the overlay. |
| `rst-card` holding `rst-row` or `rst-lrow` | As the row | As the row | Nothing of its own. |
| `person` partial, alone | The whole `a[rst-person]` | Whole element | None. |
| Choice cards, tblock | The `<label>` | Whole card | None. |
| Sidebar index rows (§4) | The nav link, `display: flex` | n/a | Rows are the links. |
| Topbar card rows (§5) | The nav link | n/a | Full-width rows. |
| detail-list, stat, job-status, status pill, badge | None | No link | None: not items with a destination. |
| list-bar, bulk-bar, pagination, seg-tabs | None | Toolbars | Tap floor only (§1.4). |

The gallery's demo lists (internal/designsystem/page.go:1980-2001), the
shell demo content (page.go:2562-2566), the formats page people list
(internal/designsystem/formats.go:135-140), the generator's list
template (internal/generate/templates.go:214-221) and the examples all
use these idioms, so they pick the rule up from tokens.css with no
markup change.

**One identity cell per row.** A list-grid row whose direct children
include both `a.rst-nm` and `a[rst-person]` would get two overlays and
the later one wins. The docs say one identity cell; a unit test checks
the styleguide samples hold to it.

**The status pill joins the target.** `[rst-row] [rst-status]` is lifted
above the overlay today (ui/tokens.css:709-715), following the 2026-08-03
spec's "anything clickable or readable on the right sits above it"
(docs/superpowers/specs/2026-08-03-f1-f6-f8-cleanup-design.md:36-43).
Under this rule a pill is not a control, and a dead spot inside a row
you can tap is a mis-tap on a phone. The lift is removed. In the list
grid, status pills were never lifted and stay under the overlay.

### 2.3 Controls above the overlay

```css
:where([rst-row], [rst-lrow]) :is(a[href], button, summary, label, input, select, textarea)
  :not([rst-row-main] > a, [rst-lrow] > a.rst-nm, [rst-lrow] > a[rst-person])
  { position: relative; z-index: 1; }
```

(written in both spellings; §8). Three things about it:

- **It lifts the controls, not their containers.** Lifting the row menu's
  `<details>` would make it a stacking context and trap its panel
  (`z-index: 40`, ui/tokens.css:1064) under the next row's lifted
  controls. Lifting only the `<summary>` leaves the panel in the page's
  context, above every row.
- **It is required, not tidy-up.** A positioned overlay paints above
  every non-positioned element whatever the DOM order, so a checkbox
  *before* the name link (`[rst-selbox]`, ui/tokens.css:1499) is covered
  today the moment the list grid gains an overlay. A tap on it would
  open the item.
- **The primary link is excluded** because positioning it would make
  its own `::after` measure against the link instead of the row.

Sizes of the lifted controls, on small or touch screens: at least
`var(--rst-tap)` each way. The kebab summary is 26×26 today
(ui/tokens.css:1061); the row checkbox's `<label>` grows by padding so
the 16px box stays 16px and the target around it is 44px; the action
pill gets `min-block-size`. On desktop the kebab and checkbox keep their
size (both clear 24px, WCAG 2.5.8 AA, for a mouse). An invisible larger
hit area on desktop was considered and not taken: a pseudo-element
reaching past the control's box would overlap the row's own overlay and
take clicks meant for the row.

### 2.4 Hover and focus

- **Hover** fills the whole row, as both idioms already do; the rule
  gains the primary-link condition. The list grid's hover selector also
  matches a non-link `<span rst-person>` today (ui/tokens.css:1045; the
  v3 fixture at ui/markup_v3_browser_test.go:103-104 has one), which
  makes a display-only row look clickable. It becomes `> a[rst-person]`.
- **Focus** draws the ring on the overlay, not on the text: the primary
  link's `:focus-visible` outline moves to its `::after`, inset by 2px
  so a card's clipped corner does not cut it. The focus rule at
  ui/tokens.css:162 stays for everything else. No `:has()` is needed.
- **Pointer:** the overlay is part of the link, so the cursor, the
  status-bar URL, middle-click and "Open in new tab" work across the
  whole row, which is a gain the name-only link never had.

### 2.5 Text selection

The overlay covers the row's text, so dragging across it drags the link
instead of selecting text, and a long press on a phone opens the link
menu. That is the accepted cost. Copying belongs on the item's own page;
a value someone copies often (an email, a reference) gets a real control
in its cell, which is lifted like any other. No opt-out utility ships in
this cut.

## 3. The row menu, a shipped component

Today the per-row kebab is a markup idiom only (ui/tokens.css:1058-1081,
the sample at ui/styleguide.go:26-37). It becomes a partial.

### 3.1 Shape

**A standalone partial, `row-menu`**, rendered in the list grid's last
cell or anywhere else a row needs one. `list-row-action` gains an
optional `Menu` key read through `opt` (ui/funcs.go:389), so adding it
does not break a struct caller the way a plain `.Menu` would (the same
reason `MenuGroup` is read through `menuGroup`, ui/ui.go doc comment).
Rejected: a `Menu` key on `list-row-action` alone, because the list grid
has no partial and is where most kebabs live.

```
{{template "row-menu" dict "Name" .Name "Items" (list
  (dict "Label" "Edit" "Href" (printf "/orders/%s/edit" .ID))
  (dict "Label" "Archive" "Action" (printf "/orders/%s/archive" .ID))
  (dict "Label" "Delete order…" "Href" (printf "/orders/%s/delete" .ID) "Danger" true))}}
```

renders

```html
<details rst-row-menu name="rst-menus">
  <summary aria-label="Actions for Grace Hopper">{{icon "kebab"}}</summary>
  <div rst-row-menu-panel>
    <a href="/orders/AB3PX/edit">Edit</a>
    <form method="post" action="/orders/AB3PX/archive"><button type="submit">Archive</button></form>
    <hr>
    <a class="rst-danger" href="/orders/AB3PX/delete">Delete order…</a>
  </div>
</details>
```

Keys, in the partial's contract comment:

- `Name` string, required: the row's name, used only in the trigger's
  accessible name through `T "rastrillo.ui.row_menu" "name" .Name`.
- `Items` list, required. Each item: `Label` required; exactly one of
  `Href` (a GET link) or `Action` (a POST); `Hidden [][2]string`
  optional, POST only, in caller order (the shape `confirm-form` settled
  on); `Danger` bool.
- `MenuGroup` optional, read through `menuGroup`, default `rst-menus`.

**Destructive items are links, always.** SKILL.md §7 says destructive
actions are `confirm-form` on their own URL, never a modal fired from
the row (SKILL.md:402-403). So a `Danger` item renders as
`<a class="rst-danger" href>` to that confirm page and never as a form,
whatever else the item carries. A test pins it. The partial draws an
`<hr>` before the first `Danger` item that follows a non-danger item;
items render in caller order, and the docs say to put the destructive
one last with a label ending in "…" (the existing convention,
ui/tokens.css:1058-1059).

**POST items are a one-button form.** CSRF is the `Sec-Fetch-Site` and
`Origin` check (csrf/csrf.go), so no token field is needed. busy.js
already covers every submit button, so a POST item shows the spinner in
place of its label with no opt-in; the item keeps its width.

### 3.2 Trigger

- The kebab icon in a `<summary>`, named `Actions for {name}` by
  `aria-label` (draft; §11). The summary is lifted above the row's
  overlay (§2.3), so tapping it never opens the row.
- Desktop: 26×26, unchanged. Small or touch: 44×44, and the list grid's
  kebab column widens to match (§1.4).

### 3.3 Panel

- The menu surface every panel already shares: `--rst-surface`, a
  `--rst-line` border, `--rst-shadow-pop`, the 9px radius
  (ui/tokens.css:1064). Hover and focus are the `--rst-accent-soft`
  background only, no border; a danger item's hover is
  `--rst-tone-negative-bg` (ui/tokens.css:1081).
- Items are full-width rows, at least `var(--rst-tap)` tall on small or
  touch screens. `[rst-row-menu-panel] form { margin: 0 }` is added so a
  POST item lines up with a link item.
- **Placement is the existing viewport-fit rule, unchanged.** "Menus
  that fit the viewport" (ui/tokens.css:1117-1219) already includes
  `[rst-row-menu-panel]`: a height cap with its own scroll, and under
  anchor positioning `position-area: block-end span-inline-start` (right
  aligned under the trigger in LTR, left aligned in RTL, from logical
  properties) with `flip-block` when there is no room below. An engine
  without anchor positioning keeps the fixed inset (opens downward),
  which is today's behaviour.
- **No bottom sheet on phones.** An anchored card keeps the row it acts
  on in view, so "Archive" is visibly about *this* row; a sheet covers
  the list and would need the row's name repeated as its title (more
  markup, one more string, and a second presentation to test). It also
  keeps one menu surface for the topbar card (§5), the account menu and
  this. The flip already solves the one thing a sheet is usually for,
  a menu near the bottom of the screen.

### 3.4 Behaviour

- Native `<details name="rst-menus">`: opening one closes the others
  with no script (SKILL.md:445-448).
- rastrillo.js light dismiss already lists `[rst-row-menu]`
  (ui/rastrillo.js:164): an outside click closes it, Escape closes it and
  returns focus to its summary. Nothing new.
- Keyboard: Tab through the items. **No arrow keys.** The library's menus
  are disclosures of plain links and buttons, not ARIA `role="menu"`
  widgets, and arrow keys on one menu and not the others would be
  inconsistent. Adding them to every menu is its own change.
- Clicking an item does not activate the row: the panel is inside the
  lifted details, and the overlay is a pseudo-element of a sibling link,
  so there is nothing for the click to reach.

### 3.5 Gallery

A `row-menu` sample in the List family (internal/designsystem/samples.go
beside `list-row-action`, samples.go:196-214), with its Code tab, and
the demo application's Requests list (page.go:1988-2001) gains a kebab
per row. The styleguide's list-grid sample (ui/styleguide.go:26-37)
switches from hand-written markup to the partial.

## 4. Sidebar shell: the index and the back control

### 4.1 How a page says what it is

Two new blocks in `ui/layouts/sidebar.html` (and console, §4.8):

- **`view`**, default `page`. The index page defines
  `{{define "view"}}index{{end}}`. The layout writes it into the shell
  root, `<div rst-shell-sidebar="{{block "view" .}}page{{end}}">`, so CSS
  selects `[rst-shell-sidebar~="index"]` (class twin
  `.rst-shell-sidebar--index`) and `~="page"` (`--page`).
- **`up`**, default `/`, the brand's default href
  (ui/layouts/sidebar.html:32). A content page defines
  `{{define "up"}}/#nav-invoices{{end}}` to return to its own row on the
  index. Without the fragment the page still works; focus just returns
  to the top of the index when scripts are off.

Why the default is `page` and not empty: an old layout renders
`<div rst-shell-sidebar>`, whose value is empty. The new rules key on the
two named values, so a new tokens.css on an old layout matches neither
and falls through to today's rules (§4.3). A page that forgets `view`
is a content page, which is the safe failure: its content shows and its
back control leads to the index. The other default would hide the
content of every unconverted page on a phone.

Why two blocks and not one: the layout cannot capture a block's output
to branch on it, so "an empty `up` means index" would have to be read
by CSS from an empty `href`, through `:has()`, and without `:has()` the
index would render as a content page with a back link to itself and no
navigation at all.

`view` is a generic name and an app could already have a template
called that; the upgrade notes say to rename it. `up` matches the
back control's `rel="up"`.

### 4.2 Markup

```html
<div rst-shell-sidebar="{{block "view" .}}page{{end}}">
<a rst-skip href="#main">…</a>
<div rst-shell-back><a href="{{block "up" .}}/{{end}}" rel="up" aria-label="{{T "rastrillo.ui.shell_up" "name" (T "rastrillo.ui.shell_up_label")}}">{{T "rastrillo.ui.shell_up_label"}}</a></div>
<aside rst-shell-rail>
<h1 rst-shell-title>{{template "title" .}}</h1>
{{block "brand" .}}…{{end}}
<nav rst-shell-nav>{{block "nav" .}}{{end}}</nav>
<div rst-shell-rail-foot>…</div>
</aside>
<main rst-shell-main id="main">…</main>
</div>
```

- `<details rst-shell-chrome>` is removed from the layout. The hamburger
  goes with it.
- The back control is a link, top left (logical inline start), sticky,
  in a strip of its own (P/side.css:18-47). It is the first thing after
  the skip link, so focus order matches visual order.
- The chevron is drawn in CSS (a rotated border on the link's
  `::before`), the same way as the index rows' (§4.3), mirrored under
  `[dir="rtl"]`. icons.go has no `chevron-left` (icons.go:119 has only
  the down caret) and no general icon mirroring; the one mirrored glyph
  in tokens.css is the calendar's (ui/tokens.css:1657). A CSS chevron
  needs neither, and keeps the icon out of the accessible name.
- The index heading reuses the page's own `title` block through
  `{{template}}` (a second `{{block}}` would redefine it). On the index
  the document title is the app's name, so the heading is too, with no
  new string.
- Apps that want their name on the back control (the prototype's
  "‹ Ledger") edit the label in their own layout.html; it is the same on
  every page, and the layout is app-owned.

### 4.3 CSS

Narrow is the default and the existing `@media (min-width: 800px)` block
(ui/tokens.css:1819) undoes it, as the rest of the shell CSS does.

- **Content view** (`~="page"`): rail hidden (as today,
  ui/tokens.css:1792); back strip shown.
- **Index view** (`~="index"`): `main` and the skip link hidden; rail
  shown at `min-block-size: 100dvh`; brand hidden (on the index it links
  to itself) and `[rst-shell-title]` shown in its place at the
  prototype's 1.75rem / 700 (P/side.css:66); back strip hidden.
- **Index rows** (P/side.css:61-91): each direct `a` of the nav is a
  full-width row at least 3rem tall, 1rem text, with a chevron. The
  prototype drew grouped cards with wrapper `<div class="c-group">`
  elements that app nav markup does not have. Here the grouping comes
  from the markup apps already write (`<p rst-shell-group>` then links):
  borders on each link, rounded top corners on the first link and on
  `[rst-shell-group] + a`, rounded bottom corners on `a:last-child` and
  on `a:has(+ [rst-shell-group])`. Without `:has()` a group's last
  corner is square, which is cosmetic. The chevron is drawn in CSS (a
  rotated border on `::after`, mirrored under `[dir="rtl"]`), so nav
  markup needs no icon. `a:target` flashes `--rst-accent-soft` once
  (P/side.css:89), reduced motion excepted.
- **The rail foot on the index follows the nav** with
  `margin-block-start: var(--rst-sp-6)`, not `auto`. The comment at
  ui/tokens.css:1801-1815 warns that a rail with a min-height floats its
  foot to the bottom, where the language menu would open off the screen.
  The rule says so in its own comment.
- **Wide** (≥ 800px): both views are today's layout. `main` and the skip
  link are shown in the index view; the back strip and the title are
  hidden. The index URL on desktop shows the rail beside the index
  page's own content, as the prototype's does (P/shots/c-c3-desktop-index.png).
- **Compatibility.** The `[rst-shell-chrome]` rules
  (ui/tokens.css:1793-1795, 1821) stay, so an app that re-vendors
  tokens.css and keeps its old layout keeps its drawer. None of the new
  rules match an unmarked root.

### 4.4 Accessibility

- **One h1 on the phone index**, from the title block. It is
  `display: none` everywhere else, so desktop and content pages never
  expose two.
- **Back control name.** Visible text is the label; the accessible name
  is "Back to {label}" (drafts in §11). The visible label is inside the
  name, as WCAG 2.5.3 asks. `rel="up"` says the relationship to tools
  that read it.
- **Landmarks.** On the phone index `main` is hidden, so the page has a
  complementary rail with a navigation inside it and no main landmark.
  That is accepted: the page *is* navigation, and the gallery's axe run
  uses WCAG tags only (internal/designsystem/a11y_test.go:63-67), where
  "one main" is a best-practice rule. The skip link is hidden with
  `main`, so it never points at nothing.
- **Focus on return.** Without scripts, `/#nav-invoices` makes that link
  the `:target` and the sequential focus starting point, so the next Tab
  goes to the item after it; whether it also becomes `activeElement`
  varies by engine, so the test asserts the starting point, not focus.
  With shell.js the link is focused explicitly (§4.5).
- **Reduced motion:** no transition and no flash (§4.5).

### 4.5 shell.js and shell.css

The layout links both, sidebar and console only:

```html
<link rel="stylesheet" href="{{asset "static/shell.css"}}">
<script defer blocking="render" src="{{asset "static/shell.js"}}"></script>
```

**Why shell.css exists.** A cross-document view transition is opted
into with `@view-transition { navigation: auto }`, and that rule cannot
be scoped to a shell: in tokens.css it would opt every page of every
shell into transitions at narrow widths, sign-in included. Both the old
and the new document must opt in, so a stylesheet that only sidebar and
console pages link scopes it exactly.

**Why `blocking="render"`.** The direction of the slide is set in
`pagereveal`, which fires at the new page's first render; a plain
deferred script can run after it. An engine that ignores `blocking`
gets an untyped transition, which shell.css draws as a plain cross-fade.

shell.js, one IIFE, the house contract (a header comment that states
the vocabulary, two-space indent, no `eval`, CSP-clean), about 50 lines
of code as in the prototype (P/side-c3.js, 2,524 bytes with terse
comments):

1. **Direction.** In `pagereveal`, if there is a transition, add type
   `back` or `forward`. `back` when the Navigation API reports a traverse
   to an earlier entry, or when the page being left set a one-shot
   `sessionStorage` flag because its back control was used; else
   `forward`. The prototype decided by URL regex (P/side-c3.js:5-8); the
   flag replaces it because the new page cannot know the index URL
   before its body is parsed, and a URL pattern is app-specific.
2. **Back reuses history.** A click on `[rst-shell-back] a` whose
   previous history entry is the back link's own URL (path and query,
   ignoring the fragment) calls `history.back()` instead of pushing, so
   history does not grow index, page, index, page, and the index
   returns from the back/forward cache at its scroll position. The
   previous entry comes from `navigation.entries()` where the Navigation
   API exists, else a same-origin `document.referrer` with
   `history.length > 1`, as the prototype does (P/side-c3.js:22-32).
   A deep link (nothing behind it) keeps the plain href.
3. **Focus return.** A click on a nav link stores its `href` in
   `sessionStorage`. On `pageshow` in the index view at narrow width,
   after a traverse or a bfcache restore or with a fragment, focus the
   fragment's target or the nav link with the stored href. Storing the
   href rather than an `id` means apps need not give nav links ids for
   the scripted path; the scriptless path still needs `#nav-…`.

shell.css (P/side-c3.css):

- Inside `@media (max-width: 799.98px) and (prefers-reduced-motion: no-preference)`:
  the opt-in; a 0.32s slide for `forward` and `back` on the root, the old
  page moving 25% and dimming, the new one sliding in from the inline
  end. Mirrored under `[dir="rtl"]`: the prototype's `translateX(100%)`
  is physical and would slide the wrong way.
- Console only: the bar gets `view-transition-name`, so it holds still
  while the page under it slides.
- Each `:active-view-transition-type()` selector sits in a rule of its
  own. An engine that does not know the pseudo-class drops the whole
  rule, and co-listing would drop its neighbours with it (the trap the
  console's own CSS records at ui/tokens.css:1916-1928).
- Reduced motion: no opt-in at all, so navigation is instant.

A system back gesture that the browser animates itself (iOS Safari's
swipe) must not animate twice. The view-transition spec skips a
transition when the navigation already has a UA visual transition; the
by-hand check (§10.8) confirms it in Safari.

**Budget.** shell.js has its own contract test with an 8 KiB cap,
following busy.js's (ui/shim_test.go:405). rastrillo.js is untouched by
§4 (its §5 change is measured there).

**Vendoring.** `shell.js` and `shell.css` join `ui/vendored.go:19`, get
`ShellJS()`/`ShellCSS()`, are written by `rastrillo new` for every shell
like the rest of the vendored set, and are checked by doctor. An app
whose layout does not link them deletes them; doctor reports that as
"absent", which is supported (cmd/rastrillo/doctor.go:175-189).

### 4.6 Prerender: an operator call

The prototype prerenders the next section with inline speculation
rules (P/c3/index.html:15). The default CSP (serve.go:428-430) has no
`script-src`, so scripts fall back to `default-src 'self'` and inline
rules are refused. Injecting them from shell.js is inline too. Two
CSP-clean routes:

- **Recommended: a `Speculation-Rules` response header.** `Serve`
  answers a fixed path with the rules as
  `application/speculationrules+json` (a static file would go out as
  `application/json`, which the browser rejects) and adds the header to
  HTML responses. The rules are document rules scoped by selector, so
  they only ever match sidebar and console navigation:
  `{"prerender":[{"where":{"selector_matches":":is([rst-shell-sidebar],[rst-shell-console]) :is([rst-shell-nav], [rst-shell-back]) a[href]"},"eagerness":"moderate"}]}`
  (plus the class spelling). On every other page they match nothing.
- **Rejected: `'inline-speculation-rules'` in the default CSP.** It is
  narrow, but it widens the policy of every app, and apps with their own
  `Options.CSP` would not get it.

The question is whether H ships the header, on by default with an
`Options` switch to turn it off, or defers prerender to its own change.
Without it the slide still works: the old page stays up until the new
one is ready, then slides. Nothing else in this spec depends on the
answer.

### 4.7 What `rastrillo new --shell=sidebar` writes

Today every shell gets the same files and only `layout.html` differs
(cmd/rastrillo/new.go:119-121); the one page is `/`, `Hello, World`
(new.go:628-631). A sidebar app scaffolded that way would open on a
phone to a title and an empty list, because its only page is the index
and its content is hidden there.

For `--shell=sidebar` and `--shell=console` the scaffold writes:

- `templates/index.html`: `{{define "view"}}index{{end}}`, the nav with
  one item, `<a id="nav-overview" href="/overview">Overview</a>`, and
  the welcome as its content (what desktop shows at `/`).
- `templates/overview.html`: `{{define "up"}}/#nav-overview{{end}}`, the
  same nav with `aria-current="page"` on Overview, the same welcome.
- A second route, `r.Get("/overview", a.overview)`, beside `/`
  (new.go:426).

That is the c3 shape (P/c3/index.html marks Overview current on the
index, and P/c3/overview.html exists beside it): on desktop `/` is the
home page, on a phone it is the list of sections. Other shells keep
today's single page. cmd/rastrillo/new_test.go gains the two-page case.

### 4.8 The console shell

The console has a header *and* a rail (ui/layouts/console.html:55-110),
so it takes one pattern per chrome:

- **The bar's tail becomes the floating card** of §5. At narrow widths
  the tail holds the account and language menus (the nav is in the
  rail), so the card is short.
- **The rail becomes the index**, with `view` and `up` exactly as the
  sidebar's, and the back strip sits under the bar. The index view shows
  the bar, then the rail full screen; the foot stays below it.
- **One control stays one control.** `<details rst-shell-menu>` now gates
  only the tail. The rail no longer hides behind it, so the `:has()`
  gate (ui/tokens.css:1883 and its wide undo at 1929, the rules the
  degradation test exercises) no longer decides the rail in new-mode markup: the rail's
  narrow display comes from the server's marker, with no `:has()` at
  all. The gate rules are kept for old layouts and scoped to an unmarked
  root with `:not([rst-shell-console~="page"], [rst-shell-console~="index"])`
  (and the class twin), so they cannot contest the new rules. In the
  meantime an old console layout on new CSS gets the card for its tail
  and its old push-down rail, which works.

Rejected: putting the rail inside the card. The card is positioned in
the bar and the rail is a sibling of the bar, so stacking them in one
card needs the bar's height, which wraps and is not known.

### 4.9 The gallery

The gallery frames itself in the sidebar shell, but by hand: its markup
is a copy of the old layout, `<details rst-shell-chrome>` included
(internal/designsystem/page.go:2290-2306), pinned by
TestTheSidebarIsTheShellTheGalleryDocuments (designsystem_test.go:2013).

- **The gallery's own frame keeps its drawer in this cut.** The legacy
  CSS stays (§4.3), so it keeps working exactly as today. Converting it
  is not a markup swap: its rail is about a hundred anchored entries in
  collapsible groups with a filter box, every rail link is a fragment on
  one of five long pages, its theme and language switchers live in
  `main` (which the index hides), and its Overview is content that would
  need a page of its own. That is a follow-up with its own design; until
  it lands, the gallery is the one place a rastrillo drawer remains, and
  the docs say so.
- **The demo application moves.** It renders through
  `ui.Layout(demoShell)` (page.go:1835-1870), so it would change anyway,
  and today it is one document switching views with `:target`, which
  cannot express two server-side views. It becomes separate documents
  per view: `demo.html` is the index (desktop shows the dashboard),
  and the dashboard, requests and request views are content pages with
  `up` pointing at `demo.html#nav-…`. The Mobile tab then shows the real
  phone behaviour. TestTheDemoApplicationSwitchesViewsWithNoScript
  (browser_test.go:1115) follows the links instead of fragments.
- **The shells page's sidebar and console previews** render two
  documents each, index and content, with `up` joining them, so the
  Mobile preview can walk index, page, back inside its frame.

## 5. The topbar menu as a floating card

### 5.1 Markup and CSS

The topbar layout does not change: `<details rst-shell-menu>` and its
sibling `[rst-shell-tail]` (ui/layouts/topbar.html:61-74) are exactly
what the card needs. The change is in tokens.css's narrow rules
(ui/tokens.css:1712-1757), from P/topmenu.css:

- The bar is `position: relative`, so the open tail positions against
  it, with `z-index: 45`: above in-page menu panels (dropdown 30, row
  menu 40) so a row menu left open without scripts cannot paint over the
  card, and below the skip link (60).
- The open tail is a card: `position: absolute`, `inset-block-start:
  calc(100% + 6px)`, `inset-inline-end: 0.75rem`,
  `inline-size: min(20rem, calc(100vw - 1.5rem))`,
  `max-block-size: calc(100dvh - 5rem)` with its own scroll and
  `overscroll-behavior: contain`. Out of flow, so the page never moves.
  Logical insets, so RTL anchors it to the left.
- **One menu surface.** The card uses the panels' 9px radius, border and
  shadow (ui/tokens.css:1090), not the prototype's 12px. The prototype's
  radius is the one visual detail not carried: one surface for every
  menu is what makes them read as one system.
- A 0.14s drop-in under `prefers-reduced-motion: no-preference`.
- The account and language menus inside the card **expand in place**
  (static panel, no shadow), because a popover inside a popover has
  nowhere good to go on a phone (P/topmenu.css:59-81).
- The wide block (ui/tokens.css:1758-1772) already turns the tail into
  `display: contents`, which drops every box property the card adds. No
  new undo is needed beyond the bar's `position`.

### 5.2 Closing on an outside tap, with and without scripts

While open, the summary grows an invisible `::before` covering the
viewport (`position: fixed; inset: 0`), under the card and above the
page. A tap anywhere outside the card lands on it, which is a click on
the summary, which closes the `<details>` natively. The tap does not
reach the page underneath, which is what light dismiss means.

With rastrillo.js running, the same click targets the summary, which
the shell menu contains, so the script leaves it for the native toggle
and closes any other open menu. Nothing toggles twice.

The layer is `position: fixed` inside the bar, so it breaks if an app
gives the bar or an ancestor a `transform`, `filter` or
`backdrop-filter` (each makes a containing block for fixed
descendants). The rule's comment says so.

### 5.3 Escape, through rastrillo.js

`[rst-shell-menu][open]` (and `.rst-shell__menu[open]`) joins the
light-dismiss list (ui/rastrillo.js:164-165), with one rule the other
menus do not need: **its tail sibling counts as inside it.** The tail is
the menu's content but not its DOM child, so without that rule a click
on the account menu inside the card would close the card, and Escape
from inside the card would hand focus to the account menu's summary,
inside a card that is about to disappear. The prototype spelled this as
a separate file (P/topmenu.js); here it is `closeMenus` and the
Escape climb in rastrillo.js learning one relation:
`inside(menu, node) = menu.contains(node) || (menu is a shell menu &&
menu.nextElementSibling.contains(node))`.

**One Escape closes everything and returns focus to the Menu button.**
The prototype closed the inner account menu first and the card on a
second Escape. The library's existing rule is that Escape closes the
whole open stack and focuses the outermost summary
(ui/rastrillo.js:176-197, a submenu inside a dropdown); the card follows
it.

**Why it is in the light-dismiss list but not the `rst-menus` group.**
The two answer different questions (ui/rastrillo.js:149-158). The name
group decides which menus may be open together, and it is
document-wide: in `rst-menus`, opening the account menu inside the card
would close the card around it. The light-dismiss list decides what
closes on an outside click or Escape, and the card is now an overlay,
which is exactly what that list is for. The comment at
ui/rastrillo.js:160-163, which keeps shell chrome out because a drawer
is not a menu, is rewritten: the sidebar's drawer is gone and the
topbar's disclosure is now a menu.

Measured: rastrillo.js is 9,784 bytes. The change is a selector, a
four-line `inside` function used in two places, and its comment; the
contract test records the before and after, as it has for every change
(ui/shim_test.go:201-261). It must stay under 16,384.

Not included: closing the card when one of its links is followed. A
link to another page closes it by navigating; a same-page fragment link
leaving the card open is how every dropdown behaves today, and changing
it for one menu would make them differ.

### 5.4 Items

- Nav links in the card are full-width rows, `var(--rst-tap)` tall on
  small or touch screens, text colour `--rst-text`.
- **The current item: `--rst-accent-soft` background, `--rst-accent`
  text, weight 600. No border and no inset shadow.** The desktop
  underline (`border-block-end-color`, ui/tokens.css:1704) is removed
  inside the card. The prototype's `box-shadow: inset 3px 0 0` is
  dropped at the operator's request; it was also physical, and would
  have drawn on the wrong side in RTL.
- The open Menu summary is filled `--rst-accent-soft` with an
  `--rst-accent` border, so it reads as pressed.

## 6. Guidance: no hamburger drawers

- **SKILL.md** (26,577 bytes, cap 30,000, skillmd_test.go:124) gains
  about 700 bytes near the shells paragraph (SKILL.md:369-373) and the
  menus sentence (SKILL.md:445-448): shells with a header put their
  narrow chrome in the Menu card; sidebar and console rails are an
  index page on phones, the index marked with the `view` block and every
  other page naming its way back with `up`; never build a hamburger
  drawer. A row that represents an item links the whole row, and never
  only its name; row actions use `row-menu`, and a destructive one links
  to its confirm page. No existing fact is removed.
- **docs/site/templates.md**: the shells table (923-928), the attribute
  list (970-980, whose last sentence names the sidebar drawer), the
  console section (990-1009), the gallery note (610-620), the menu group
  note (167-170), and an "Upgrading" section giving the layout edit, the
  two blocks, the `view` name clash, and the scaffold's two-page shape.
  The list idioms section (137-146, 281-314) gets the whole-row rule and
  `row-menu`.
- **docs/site/icons.md:34** ("the shells use `menu` when they
  collapse"): only topbar and console now.
- **The gallery's shell blurbs** (page.go:906-908, 1561-1562) are
  translation keys (internal/designsystem/prose.go); the English goes to
  copy review first, then the eleven translations.
- **Comments** in the three layouts and tokens.css that describe the
  drawer or "two shells, one idiom" (ui/layouts/topbar.html:26-33,
  ui/layouts/sidebar.html:24-29) are rewritten.

## 7. Why phones still zoom

The fix shipped in v0.26.0. The apps that lack it are the apps that
never upgraded. Local checkouts on this machine, 2026-09-30:

| App | rastrillo pinned | Floor in its tokens.css |
|---|---|---|
| dineraya | v0.25.0 | no |
| oficina/calendar, memoria, sheets-core | v0.23.0 | no |
| paulca/seapointish | v0.17.0 | no |
| oficina/meet, slides; rutline; correomona | newer | yes |

Each app's generated `vendored_test.go` compares its copy with the
library *at the version its go.mod pins* (cmd/rastrillo/new.go:729-781),
so a stale pin passes its own CI with a stale copy. `rastrillo doctor`
compares byte for byte (cmd/rastrillo/doctor.go:257) and, run from a
newer CLI, reports version skew (exit 4) rather than drift
(doctor.go:168-170). And the v0.26.0 entry told nobody to re-vendor
(CHANGELOG.md:394-404).

What H does, and what it does not:

- **The changelog entry leads with the upgrade.** Its heading carries
  "re-vendor `tokens.css`, add `shell.js` and `shell.css`, and update
  your layout", and its first paragraph says that an app pinned below
  v0.26.0 zooms on every form on a phone, and that the fix is to upgrade
  the module, then `rastrillo doctor --fix`.
- **Doctor gains one advisory, not a new failure.** Doctor never reads
  `templates/layout.html` (it is app-edited, so bytes cannot be
  compared). It can recognise the old shell markup: a layout containing
  `rst-shell-chrome`, or a sidebar or console root with no `view` block.
  It prints one line naming the upgrade section of templates.md and
  keeps its exit code.
- **No doctor step in the scaffolded CI.** Inside an app's own CI,
  doctor runs at the app's pinned version, which is the comparison
  `vendored_test.go` already makes under `make test`. A second step would
  report the same thing twice and catch nothing new. The gap is being
  behind, and no check pinned to the app's own version can see that.
- **Out of scope, named:** a check that an app is behind the latest
  rastrillo release belongs to the forge's runner or a dependency bot,
  across every app, not to one app's CI.

## 8. Vocabulary, twins, budgets and the CSP

New selectors, each written with its class twin in the same rule at the
same weight (TestEveryClassSelectorHasAnAttributeTwin,
ui/markup_v3_test.go:329; TestNoAttributeSelectorIsAnOrphan, 375; the
grammar is internal/markup/markup.go:97-146):

| Attribute | Class |
|---|---|
| `[rst-shell-back]` | `.rst-shell__back` |
| `[rst-shell-title]` | `.rst-shell__title` |
| `[rst-shell-sidebar~="index"]`, `~="page"` | `.rst-shell-sidebar--index`, `--page` |
| `[rst-shell-console~="index"]`, `~="page"` | `.rst-shell-console--index`, `--page` |

The row rules use `a.rst-nm`, a utility that keeps its class spelling
in both (internal/markup Utilities, markup.go:55). Every new class goes
into the v3 browser fixture (ui/markup_v3_browser_test.go:60-168) so
both spellings are computed and compared.

| File | Today | After H | Cap |
|---|---|---|---|
| rastrillo.js | 9,784 | about 10.8 KB (§5.3) | 16,384 (shim_test.go:262) |
| shell.js | new | about 5 KB with comments | 8,192, new test |
| shell.css | new | about 1.5 KB | none (CSS) |
| gallery.js | 9,553 | unchanged | 10,240 (designsystem_test.go:1742) |

CSP: no inline styles or scripts anywhere. The only inline candidate in
the prototypes, speculation rules, is §4.6. View transitions, anchor
positioning and the tap floor are all in stylesheets.
TestPartialsAndLayoutsEmitNoInlineStyles (ui/ui_test.go:3379) covers the
new layouts and the `row-menu` partial.

CSS floor: `@view-transition` and `:active-view-transition-type()` are
enhancement only and do not raise the floor (cssfloor_test.go); an
engine without them navigates instantly. The index rows' corner rounding
uses `:has()`, which is already the floor's Firefox term.

## 9. Existing tests this changes

Each is rewritten to assert the new behaviour, not deleted.

- **TestTheShellsKeepTheirOverridableBlockNames** (ui/ui_test.go:2110):
  sidebar and console gain `view` and `up` in their source order.
- **TestEveryChromeShellCollapsesBehindTheMenuIcon** (ui_test.go:2053):
  topbar and console keep `<details rst-shell-menu>` with `menu`; the
  sidebar asserts *no* disclosure and a `[rst-shell-back]` link.
- **TestIdiomClassesAreStyled** (ui_test.go:1729, shells at 1800-1810):
  `rst-shell-chrome` leaves the must-have-a-sample list (it is legacy);
  `rst-shell-back` and `rst-shell-title` join. The `shell-sidebar`
  sample's pinned menu-icon path goes.
- **TestEveryMenuDefaultsToTheSharedExclusivityGroup** (ui_test.go:1940):
  unchanged in substance; it must still find no `rst-menus` in the
  sidebar and `rst-shell-menu` outside the group.
- **TestMenuExclusivityAndDropdownDismissDrive** (ui/browser_test.go:1082):
  its fixture's `rst-shell-chrome` stays open (legacy, not a menu); a
  new leg asserts an open `rst-shell-menu` *does* close on an outside
  click and Escape.
- **TestTheSidebarRailPutsThePersonAtItsFootAndTheLanguageMenuOpensUpward**
  (ui/shell_browser_test.go:288): the 390 leg renders the index view
  instead of clicking the drawer, and asserts the foot follows the nav
  and the language menu opens downward or flips (§4.3).
- **TestTheTopbarCollapsesItsTailBehindOneDisclosure**
  (shell_browser_test.go:579): at 390 the opened tail is a card that
  overlays (§10.4), not three stacked rows in flow.
- **TestTheConsoleFoldsBothChromesBehindOneControl**
  (ui/console_shell_browser_test.go:267): one click reveals the tail
  card only; the rail follows the view marker. "Exactly one visible
  summary" still holds, since the back control is a link.
- **TestTheConsoleDegradesTheWayItSaysItDoesWithoutHas**
  (console_shell_browser_test.go:645): runs against the old markup, where
  the gate still lives; a new-markup leg asserts the rail needs no
  `:has()` at all.
- **Gallery:** TestA11yScansTheShellsCollapsed (a11y_test.go:477; its
  sidebar leg scans the index and content views and drops the 24px
  summary floor for the sidebar); TestA11yReflowsAt320 (a11y_test.go:824)
  adds the index view; TestThePreviewWidgetIsUsableOnAPhone
  (browser_test.go:1761) and TestPreviewFrameHeightsFitTheirContent
  (browser_test.go:939) re-measured with the new demo documents; the
  gallery's `kMin` reasoning from a 12.5px `--rst-fs-sm`
  (browser_test.go:1342-1347) is rechecked, since the Mobile tab now
  renders 14px there.
- **TestListRowActionMinimalFixture/FullFixture** (ui_test.go:702-734):
  unchanged output without `Menu`; a new fixture with it.
- **Examples:** examples/blog and examples/tickets carry byte copies of
  tokens.css (TestVendoredTokensCSSMatchesTheLibrary,
  examples/blog/internal/blogtest/tokens_test.go:21) and are re-copied.

## 10. Tests to add

Browser tests use the harness (`-tags browser`, harness/rig.go:86).
Nothing in the suite emulates touch today; these legs use CDP's
`Emulation.setTouchEmulationEnabled` and assert
`matchMedia("(pointer: coarse)").matches` as a control before measuring,
so a harness that cannot produce a coarse pointer fails loudly instead
of passing on desktop sizes.

### 10.1 Sizing

A fixture page renders every partial and every styleguide sample.

- **390×844, touch.** Every visible text-entry control (the prototype's
  TEXTY set, P/measure.js:5) computes at least 16px. The primary input
  computes exactly `--rst-fs-lg`, 19px (the 1em bug would give 16).
  Every visible target (P/measure.js:6 plus `[rst-row-menu] > summary`,
  `[rst-selbox]`, `[rst-shell-back] a`) is at least 44px on its smaller
  axis, except links inside running text; a stretched link is measured
  as its row (P/measure.js:61-63).
- **1024×768, touch** (the pointer half alone) and **600×800, mouse**
  (the width half alone): the same two assertions.
- **1280×900, mouse:** desktop pinned. Body 14px; the four tokens at
  17/14/12.5/11.5px; primary input 17px; button heights 28/34/44px
  (ui/tokens.css:63-68); kebab 26×26; checkbox 16px. Any change fails.
- **Unit (no browser):** in tokens.css the bare-element floor's selector
  is wholly inside `:where()`, and the component floor's query sits
  after `.rst-input` and before `[rst-input~="primary"]`.

### 10.2 Whole rows

For each idiom in §2.2 with a primary link (list-row-action; list grid
with `rst-nm`; list grid with a person link; list grid with checkbox,
status pill and kebab; the demo's Requests list), at 1280 and at 390
with touch:

- A click at the row's far empty area (inline end minus 12px, vertical
  centre, and a point inside an empty cell) navigates to the primary
  href. `document.elementFromPoint` at that point is the primary link.
- A click on the action pill goes to the pill's href; on the kebab opens
  the menu and does not navigate; on the checkbox toggles it and does
  not navigate. Each of those controls is at least 44×44 at 390.
- A row with no primary link: a click navigates nowhere, and hovering
  it does not change its background.
- Focus on the primary link draws the ring around the whole row (the
  overlay's outline box equals the row's box, within 2px).

### 10.3 Sidebar index and back

Driven with scripts off and on, at 390 (touch) and 1280, in LTR and RTL.

- **Scripts off:** `/` shows the rail full screen, the title as a
  visible h1, no `main`, no skip link, no drawer control. Following a nav
  link shows the content page, the back control at the top inline-start,
  no rail. The back control goes to `/#nav-…`, where that link is
  `:target` and the next Tab lands on the following link.
  `history.length` grows by one per step (recorded, since that is the
  scriptless trade).
- **Scripts on:** index, then page, then back control: `history.length`
  is unchanged by the back step and `location` is the index;
  `document.activeElement` is the nav link that was followed. A deep
  link (content page opened directly) pushes the index and keeps focus
  logic. A recorded `pagereveal` sees type `forward` going in and `back`
  coming out, and no transition at all under
  `prefers-reduced-motion: reduce`.
- **1280:** both views show rail beside content, identical to today's
  layout (rail box and main box equal to a golden measured before the
  change); no back control and no second h1 visible.
- **Compatibility:** the previous `sidebar.html` (from git) with the new
  tokens.css still opens its drawer at 390.
- **axe** at 390 on both views, every theme and scheme; **320px reflow**
  on both.

### 10.4 Topbar and console card

Scripts off and on, at 390 with touch, LTR and RTL.

- Opening Menu leaves `main`'s bounding box unchanged to the pixel (the
  page never moves). The card's inline-end edge is within 0.75rem plus
  1px of the viewport's.
- A tap outside the card, at a point over a link in `main`, closes the
  card and does not follow the link, with scripts off and on.
- Scripts on: Escape closes it and focuses the Menu summary; with the
  account menu open inside the card, one Escape closes both and focuses
  the Menu summary; opening the account menu inside the card keeps the
  card open.
- The current item's computed `background-color` is
  `--rst-accent-soft`; its `box-shadow` is `none` and its
  `border-inline-start-width` and `border-block-end-width` are 0.
- 800px and above: today's bar, unchanged.
- Console: the same, plus the index and back legs of §10.3.

### 10.5 Row menu

- **Unit:** every key combination renders; the summary's name is
  "Actions for {name}"; a `Danger` item is always `<a class="rst-danger">`
  and never a `<form>`, even when `Action` is set; the `<hr>` appears
  before the first danger item after a non-danger one and nowhere else;
  POST items are `<form method="post">` with one submit button and the
  `Hidden` pairs in order; `MenuGroup` defaults to `rst-menus`;
  `list-row-action` with no `Menu` renders byte for byte today's output.
- **Browser**, scripts off and on: opening one row's menu closes
  another's (scripts off, native group); outside click and Escape close
  it (scripts on) and Escape returns focus to its summary; near the
  viewport's bottom the panel opens upward in an engine with anchor
  positioning; clicking an item never navigates to the row's href; a
  POST item shows the busy spinner; items and trigger are 44px at 390
  with touch.
- **a11y:** axe on the gallery sample in every theme and scheme; every
  control named (TestEveryControlHasAnAccessibleName, ui_test.go:1301).

### 10.6 Scaffold and vendoring

`rastrillo new --shell=sidebar` and `--shell=console` write the two pages
and the second route, and the scaffolded app builds and passes its own
tests; the other shells are unchanged. `shell.js` and `shell.css` are
written for every shell and match `ui.ShellJS()`/`ShellCSS()`. Doctor
checks them, and prints its layout advisory for an old layout (§7) with
exit code 0.

### 10.7 Contracts

shell.js: an IIFE with its contract comment, no `eval`/`new Function`,
two-space indent, under 8 KiB, and names the three behaviours.
rastrillo.js: still under 16 KiB, and mentions the shell-menu selector
and the tail relation.

### 10.8 By hand before merge

In the branch description: iOS Safari and Android Chrome on a real
phone: no zoom on focus of any field; the slide both ways; the system
back swipe does not animate twice; VoiceOver and TalkBack on the index
(h1, rows), the back control's name, and the card.

## 11. Strings for copy review

All drafts, none written into a file until reviewed. No em dashes.

| Key or place | Draft |
|---|---|
| `rastrillo.ui.shell_up_label` (back control, visible) | Sections |
| `rastrillo.ui.shell_up` (back control, accessible name) | Back to {name} |
| `rastrillo.ui.row_menu` (kebab name) | Actions for {name} |
| Scaffold nav item and page | Overview |
| Scaffold index and overview content | today's `Hello, World` line, unchanged |
| Doctor's layout advisory | This layout still has the old mobile menu. See "Upgrading" in the templates guide. |
| Changelog heading and first paragraph | drafted with the implementation, reviewed before merge |
| SKILL.md and templates.md additions (§6) | drafted with the implementation, reviewed before merge |
| Gallery shell blurbs (page.go:906-908, 1561-1562) | English first, then the eleven translations |

The index heading needs no string: it is the page's title (§4.2).

## Rollout

One branch, in this order, each step green before the next:

1. Sizing (§1), with its tests.
2. Whole rows (§2).
3. `row-menu` (§3) and the gallery sample.
4. The topbar card and rastrillo.js (§5).
5. The sidebar and console index and back, shell.js and shell.css,
   the scaffold (§4).
6. The gallery demo and shell previews (§4.9).
7. Docs, SKILL.md, doctor's advisory, changelog (§6, §7); the examples'
   tokens.css copies.

## Out of scope

- Converting the gallery's own frame (§4.9).
- Prerender, unless the operator says to ship §4.6's header in H.
- A check that apps are behind the latest release (§7).
- Arrow-key navigation in menus (§3.4).
- A text-selection opt-out for row cells (§2.5).
- `<table>` rows. A data table's rows are not items with a destination.
- Closing a menu when a same-page link inside it is followed (§5.3).

## Risks

- **Every phone page changes size at once.** Variant iii moves body text
  from 14 to 16px everywhere below 40rem or under touch. Layouts built
  to a 14px measure may wrap differently. The 320px reflow test and the
  sizing legs cover the library; apps will see it on their own screens.
- **The whole-row overlay changes clicks in existing apps.** A row that
  had a hand-written, unlifted control before the name (an app's own
  checkbox or link, not a rastrillo idiom) becomes covered. §2.3's rule
  lifts any `a`, `button`, `summary`, `label` or form control in a row,
  which covers the common cases; a control made of a `<div>` with a
  click handler is not covered. The changelog names it.
- **Block name `view` can collide** with an app template of that name.
  The upgrade notes say to rename.
- **The drawer's CSS lives on as legacy** for old layouts and the
  gallery. It is dead weight in tokens.css until both are gone; the
  legacy rules are grouped and commented so they can be deleted in one
  cut.
- **`blocking="render"` and cross-document view transitions are not in
  every engine.** Without the first, the slide may be untyped (a
  cross-fade); without the second, navigation is instant. Neither breaks
  navigation.
- **The outside-tap layer depends on `position: fixed` meaning the
  viewport.** An app style that gives the bar a transform or filter
  breaks it (§5.2).
- **Pixel pinning of desktop sizes** (§10.1) makes any later deliberate
  density change touch that test. That is the point of it.
