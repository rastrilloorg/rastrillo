# Mobile ergonomics: phone-sized type, 44px targets, whole-row links, and shells without a hamburger

Status: design, 2026-09-30. The operator chose each look from a
throwaway prototype; this spec turns those choices into the framework
and decides what the choices left open. Revised after adversarial
review round 1 (see "Review log") with the operator's answers on
prerender, the gallery and copy review.

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
- **The gallery's own frame is not in H (operator, after round 1).** Its
  hand-written frame, hamburger included, converts with B
  (gallery-usability), which already reworks that frame. H converts the
  gallery's demo application and shell previews only. §4.9.
- **Chosen here: the row menu is a standalone partial, `row-menu`,** and
  `list-row-action` gains an optional `Menu` key that calls it. On
  phones it stays an anchored card, not a bottom sheet. §3.
- **Prerender ships in H (operator, after round 1).** The prototype's
  inline `<script type="speculationrules">` (P/c3/index.html:15) is
  refused by the default CSP, so `Serve` sends a `Speculation-Rules`
  header naming a rules file it serves itself, on by default, with an
  `Options` switch to turn it off. §4.6.
- **Copy review follows the adversarial review (operator).** The draft
  strings in §11 go to the operator once review is satisfied, before
  any is written into a file.
- **Chosen here, after rounds 1 and 2: on small or touch screens the
  calendar docks to the bottom of the viewport, and its days are 44px
  where seven fit.** The panel is a 326px border box derived from
  `--rst-tap`; seven 44px days fit from a 342px viewport. Below that (a
  320px phone, or a zoomed page) days narrow to about 41px, still 44px
  tall: a stated design exception to the house rule, above WCAG 2.5.8's
  24px, and not claimed as 2.5.5 conformance. §1.4.

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

**What the scale does not move.** The root font size is unchanged, so
rem spacing (`--rst-sp-*`, paddings written in rem) stays as it is; so
does geometry written in px (the 16px checkbox, the 26px kebab, the
28px avatar, the seg-tabs' `5px 16px` padding at ui/tokens.css:1331).
Controls grow because their text grows and because §1.4 gives them a
minimum box, not because the page is scaled. Paddings in `em` (none of
the targets in §1.4 rely on one) would follow the text.

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
   - **Each component selector at its own specificity, placed after the
     rule that resets its font and before any rule that sizes it on
     purpose.** Three small queries replace the block at
     ui/tokens.css:2085:
     - `.rst-input, [rst-input]` directly after its base rule
       (ui/tokens.css:1262, which sets `font: inherit`) and before
       `[rst-input~="primary"]` (1279);
     - `.rst-textarea, [rst-textarea]` directly after its base rule
       (ui/tokens.css:1285, which also sets `font: inherit`; a floor
       placed before it would be reset, which is the round-1 finding);
     - `.rst-search input[type="search"], [rst-search] input[type="search"]`
       directly after the rule that sets it to `var(--rst-fs-sm)`
       (ui/tokens.css:569-574); under variant iii that is 14px, so this
       is the one floor that fires inside the rastrillo idioms at all.

     Equal specificity, and each floor is the last word on its
     component's size except for the rules that mean it: the primary
     rule wins, and so does an app stylesheet loaded after tokens.css. A
     unit test reads tokens.css and checks each floor's position against
     its reset and its sizing rules, so a later edit that moves one
     fails.

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

Inside the query, every interactive idiom is at least `var(--rst-tap)`
on both axes, measured as its **activation area**. Every row of the
table below that says "min block size" also gets
`min-inline-size: var(--rst-tap)` unless the element is already as wide
as its container (a full-width menu row, a stretched input), and every
element that is `display: inline` today gets `inline-flex` or `flex` in
the same rule, since min sizes do nothing on an inline box. The
activation area is the box a tap
activates, which for a label-backed control is the label and for a
stretched link is the row. The prototype's list (P/touch-targets.css:
20-52) was the starting set; round 1 found it incomplete, so this is the
full inventory, taken from every interactive rule in tokens.css (every
`cursor: pointer`, every link and control rule):

| Idiom | Rule today | Touch treatment |
|---|---|---|
| Buttons, all sizes | ui/tokens.css:173-190 | min block and inline size |
| Inputs, selects, textareas | 1262, 1285 | min block size |
| Search box, its clear link | 545-614 | the box gets min block size and `padding-block: 0`, and the input `align-self: stretch` so the input itself, which is what a tap focuses, fills the 44px box (P/touch-targets.css:22-23); clear link 44×44 |
| Filter chip's remove link | 1235 (24×24) | 44×44, the chip's padding absorbs it |
| Help link | 1483 (28×28) | 44×44 |
| Dropdown summaries (list-bar, header, account, locale) | 1087 | min block size, `align-items: center` |
| Nested menu-group summaries | 1114 | min block size |
| Menu items in every panel: dropdown, row menu, locale | 1065, 1091, 1098, 1105 | full-width rows, min block size |
| Combobox options, date-picker rows | 1552, 1608 | min block size |
| Date-picker pick button, calendar nav | 1604, 1646 | 44×44; the date input's inline-end padding grows to 3rem so text never runs under the bigger button (P/touch-targets.css:25) |
| Calendar days | 1660 | below |
| Row action pill, kebab, row checkbox | 693, 1061, 1499 | §2.3 and §3 |
| Standalone person link | 969 | min block size |
| Pagination chips | 788-800 | 44×44 |
| Segmented tabs | 1331 | min block size |
| Switch, choice cards, tblock head | 1304, 1317, 1436 | the label: min block size |
| Bulk bar: close, escalate link, Actions summary | 1491, 1495, 1498 | close 44×44; escalate and Actions min block size |
| Modal close, modal panel nav links | 1472, 1468 | close 44×44; nav links min block size |
| Back-nav link | 1504 | `display: inline-flex; align-items: center`, then min block and inline size (it is inline today, where min sizes do nothing) |
| Shell: brand, nav links, Menu summary, back control | 1700, 1702, 1737, 1868 | `inline-flex` or `flex`, min block and inline size; back control §4 |
| Legacy sidebar drawer summary (kept for old layouts, §4.3) | 1793 | min block size; it is full width |

A test (§10.1) enumerates every rule in tokens.css that carries
`cursor: pointer` or styles an `a`, `button`, `summary` or `label`, and
fails if one is not in this table's fixture, so a new control cannot
arrive without a decision.

**Calendar days.** The panel's content box is `18rem` (it is a content
box: tokens.css sets `box-sizing` per component and has no universal
reset, ui/tokens.css:1643, 1253), holding a seven-column fixed table
(1658), so a day is about 41px wide on desktop; seven 44px days need
308px of content. Inside the query days get
`block-size: var(--rst-tap)` (the prototype changed only the line
height, P/touch-targets.css:28, which grows height alone), and the
panel is **docked to the bottom of the viewport on every small or touch
screen**:

```css
:is([rst-cal], [rst-dtp] [rst-cal], [rst-dtp].is-above [rst-cal]) {
  box-sizing: border-box;
  position: fixed;
  top: auto;
  inset-block-start: auto;
  inset-block-end: var(--rst-sp-2);
  inset-inline: 0;
  margin: 0 auto;
  inline-size: min(calc(7 * var(--rst-tap) + 2 * var(--rst-sp-2) + 2px), calc(100vw - 2 * var(--rst-sp-2)));
}
```

(both spellings; the `:is()` takes the weight of its heaviest argument,
(0,3,0), so it overrides the `is-above` rule at ui/tokens.css:1622 as
well as the base rule.) Every declaration earns its place, because
round 2 found each missing one: `border-box` so the width formula is
the outer box (the border box is 326px at the default root, the tap
plus padding plus borders, derived from `--rst-tap` rather than a second
constant); `top: auto` because the base rule's physical `top: 100%`
(1643) would otherwise be measured against the viewport once the panel
is fixed; `inset-block-start` and the margins reset because `is-above`
sets them. datetime.js still measures and toggles `is-above`
(datetime.js:1405-1419); inside the query the class changes nothing.

Why dock rather than anchor, on a tablet too: an anchored panel is
placed from the field's inline start (`inset-inline-start: 0`), and a
date field can sit at the trailing end of a wrapping field row
(ui/tokens.css:1357-1360) at any width; datetime.js checks vertical
room only (1412). Docked, the panel is always inside the viewport, and
one presentation for every touch screen is one thing to test. Desktop
under a fine pointer keeps today's anchored 18rem panel.

**Below a 342px viewport** (a 320px phone, or a zoomed page) the docked
panel is narrower than 326px and days come out at about 41px wide, still
44px tall. This is a **design exception to the house 44px rule**, not a
claim of WCAG 2.5.5 conformance: the date field beside the calendar
takes a typed date, which gives a keyboard and a thumb an alternative
way in, but a text field is not an equivalent target for "pick this
day", so the spec does not lean on 2.5.5's equivalent exception. WCAG
2.5.8's 24px AA floor holds by a wide margin. The 320px reflow test
covers the panel.

**Exempt:** a link inside running text (inside `p` or `[rst-field-help]`,
not inside `nav`), per WCAG 2.5.8's inline exception. The prototype's
measuring script draws the same line (P/measure.js:24-27).

### 1.5 What does not change

Above 40rem under a fine pointer, desktop **density** is today's: the
type tokens, every control's size and every spacing value. Three
desktop changes are deliberate and are pinned by the same test (§10.1)
at their new values: the whole-row target and its focus ring (§2), and
the row checkbox's label growing to a 24×24 target around an unchanged
16px box (§2.3).

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
:where(:is([rst-row], [rst-lrow]) :is(a[href], button, summary, label, input, select, textarea):not([rst-row-main] > a, [rst-lrow] > a.rst-nm, [rst-lrow] > a[rst-person])) {
  position: relative;
  z-index: 1;
}
```

(written in both spellings; §8). The `:not()` is attached to the
`:is()` with no space: it filters the controls themselves. With a space
it would be a descendant combinator and lift things *inside* the
controls, leaving a bare checkbox or summary covered (round 1, finding
2). The browser test hit-tests the controls themselves (§10.2).

**The whole selector is inside `:where()`, so it weighs nothing.** Its
job is to give an otherwise static control a position and a z-index, and
never to replace a position a component chose. Round 2 found three that
it would have replaced at any positive weight: the switch's input,
absolutely placed over its track (ui/tokens.css:1305); the date
picker's pick button, absolutely placed inside the field (1604); and a
native select that select.js hides out of flow (2118). At zero weight
every one of those rules wins `position`, and `z-index: 1` still
applies to them, since an absolutely placed element is positioned. An
app rule that positions a control in a row wins the same way. The
browser fixture puts a switch, a date field and an enhanced select in a
list-grid row and checks each keeps today's box (§10.2).

Four things about it:

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
- **A menu panel opened over later rows stays above them.** The lifted
  summaries of later rows are at `z-index: 1` in the page's stacking
  context; the open panel is `z-index: 40` (ui/tokens.css:1064), or
  `position: fixed` under anchor positioning (1206-1218), and its
  `<details>` is not positioned-with-z-index, so it creates no stacking
  context to trap it. A test opens the first row's menu over the second
  row's kebab and checkbox and hit-tests the panel's items there.

Sizes of the lifted controls, on small or touch screens: at least
`var(--rst-tap)` each way. The kebab summary is 26×26 today
(ui/tokens.css:1061); the row checkbox's `<label rst-selbox>` grows by
padding so the 16px box stays 16px and the label, which is what a tap
activates, is 44×44; the action pill gets `min-block-size`.

On desktop the kebab keeps 26×26, which clears WCAG 2.5.8's 24px. The
checkbox does not: it is 16×16 in a label with no padding
(ui/tokens.css:1499-1500), and the spacing exception cannot rescue it
inside a row whose overlay is itself a target. So on desktop too the
label gets 4px of padding: a 24×24 target around the unchanged 16px box.
That is one of the three deliberate desktop changes (§1.5). An invisible
hit area larger than the control on desktop was considered and not
taken: a pseudo-element reaching past the control's box would overlap
the row's own overlay and take clicks meant for the row.

**The kebab's column on wide touch screens.** The narrow override sets
the list grid's trailing column (ui/tokens.css:1053-1056), but above
800px the columns come from the app's own `--rst-cols`, usually with a
literal trailing `32px` (the doc sample, ui/ui.go:108; the gallery,
internal/designsystem/page.go:2562). tokens.css gains
`--rst-col-menu`, `32px` at :root and `var(--rst-tap)` inside the query,
and the samples, the docs and the scaffold write
`--rst-cols: … var(--rst-col-menu)`. An app that keeps a literal `32px`
still works: the 44px summary is `justify-self: end` (1060), so it
overflows its track toward the inline start by 12px, into the grid's
0.85rem (13.6px) gap, without reaching the neighbouring cell. The
upgrade notes say to switch to the variable.

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
cell or anywhere else a row needs one. Rejected: a `Menu` key on
`list-row-action` alone, because the list grid has no partial and is
where most kebabs live.

`list-row-action` also gains an optional `Menu` key, read through `opt`
(ui/funcs.go:389) so a struct caller without the field does not fail at
Execute (the reason `MenuGroup` is read through `menuGroup`). `Menu` is
the `Items` list alone; the partial supplies `Name` from the row's
`Main`, so the trigger is "Actions for {Main}" with no second copy of
the name to drift. It renders after the status pill and after the
action pill, when both are present: pill for the one frequent action,
kebab for the rest, in that order in the DOM and on screen. Without
`Menu` the row's output is byte for byte today's.

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
  accessible name through `Tf "rastrillo.ui.row_menu" "name" .Name`. `Tf`, not `T`: the
  default `T` ignores its extra arguments (ui/funcs.go:163) and would
  render "Actions for {name}"; `Tf` substitutes (ui/funcs.go:183), as
  the sign-in partial does (ui/partials/signin.html:45).
- `Items` list, required. Each item: `Label` required; exactly one of
  `Href` (a GET link) or `Action` (a POST); `Hidden [][2]string`
  optional, POST only, in caller order (the shape `confirm-form` settled
  on); `Danger` bool.
- `MenuGroup` optional, read through `menuGroup`, default `rst-menus`.

**Destructive items are links, always.** SKILL.md §7 says destructive
actions are `confirm-form` on their own URL, never a modal fired from
the row (SKILL.md:402-403). So a `Danger` item requires `Href`, the
confirm page, and renders as `<a class="rst-danger" href>`; `Danger`
with `Action` is an error (below). The partial draws an
`<hr>` before the first `Danger` item that follows a non-danger item;
items render in caller order, and the docs say to put the destructive
one last with a label ending in "…" (the existing convention,
ui/tokens.css:1058-1059).

**POST items are a one-button form.** CSRF is `csrf.Protect`, an
origin check mounted app-wide (SKILL.md:248; csrf/csrf.go), so no token
field is needed. busy.js covers every submit button (busy.js:120-131):
it inserts a `[rst-spin]` before the label. The spinner-*replaces*-label
presentation is scoped to `[rst-btn]` (ui/tokens.css:366-380), and a
menu item is not one, so a POST item shows the spinner **beside** its
label, the look every non-`rst-btn` button already gets. That is right
for a full-width, start-aligned row. The round-1 draft promised the
replacement look; it would need the `rst-btn` rules widened for no gain.

The spinner gets a **reserved slot** so the item's box cannot move.
Being full width does not guarantee that: the panel has only a
`min-width` and otherwise sizes to its content (ui/tokens.css:1064), so
an inline spinner added to a long label could widen the panel or wrap
the label. POST buttons in a row menu are `position: relative` with
`padding-inline-end: calc(0.65rem + 1rem + var(--rst-sp-2))` (the
item's own padding, the spinner's 1rem, a gap), and
`[rst-row-menu-panel] button > [rst-spin]` is absolutely placed in that
padding at the inline end, vertically centred. The slot is reserved
whether or not the item is busy, which widens POST items by 1.5rem of
empty padding on desktop: a cost paid so a long translated label never
reflows mid-submit. Under reduced motion the ring is still, as
everywhere else (ui/tokens.css:1509).

**Validation.** Invalid items fail at Execute, loudly, through a new
template func `rowMenuItems` that the partial ranges over, the way
`dict` returns an error for an odd argument count (ui/funcs.go:252).
It returns an error when an item has no `Label`; has both or neither of
`Href` and `Action`; is `Danger` without `Href` (a destructive item is a
link to its confirm page, so `Action` alone has nowhere to go); or
carries `Hidden` without `Action`. Silently dropping an item, or
rendering a destructive POST, are the two failures this rules out.

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
- Clicking an item does not activate the row. Only the summary is
  lifted (§2.3; the `<details>` is not, so it makes no stacking
  context); the open panel paints above the row's overlay because it is
  itself positioned at `z-index: 40`, or `position: fixed` under anchor
  positioning. A click on an item hits the item, and the item is not
  inside the primary link, so the link cannot receive it.

### 3.5 Gallery

A `row-menu` sample in the List family (internal/designsystem/samples.go
beside `list-row-action`, samples.go:196-214), with its Code tab, and
the demo application's Requests list (page.go:1988-2001) gains a kebab
per row.

The styleguide's list-grid sample (ui/styleguide.go:26-37) **stays
hand-written markup**, and gains the kebab exactly as the partial
renders it. Styleguide samples are raw HTML, not templates: the gallery
hands them to `newPreview` unexecuted (internal/designsystem/page.go:
1517, 1639) and their own test parses no partials (ui/ui_test.go:1633),
so a `{{template "row-menu"}}` call there would render as its source
text. The raw-source gates that read the sample, the menu group and
`rst-danger` (ui/ui_test.go:1748, 1981), keep passing because the
markup still carries both. A new unit test renders `row-menu` with the
sample's data and asserts the sample's `<details rst-row-menu>` is that
output, byte for byte after whitespace normalisation, so the two cannot
drift.

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
<div rst-shell-back><a href="{{block "up" .}}/{{end}}" rel="up" aria-label="{{Tf "rastrillo.ui.shell_up" "name" (T "rastrillo.ui.shell_up_label")}}">{{T "rastrillo.ui.shell_up_label"}}</a></div>
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
  (P/side.css:89), with `animation: none` on the same selector under
  `prefers-reduced-motion: reduce` for the motion gate.
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
   `back` or `forward`. (For a prerendered page `pagereveal` fires at
   activation, after the storage swap, so it sees the flag below.) `back` when the Navigation API reports a traverse
   to an earlier entry, or when the page being left set a one-shot
   `sessionStorage` flag because its back control was used; else
   `forward`. The prototype decided by URL regex (P/side-c3.js:5-8); the
   flag replaces it because the new page cannot know the index URL
   before its body is parsed, and a URL pattern is app-specific.
2. **Back reuses history, only when it can prove what is behind.** A
   click on `[rst-shell-back] a` calls `history.back()` instead of
   following the link when all of these hold: it is a plain primary
   click (button 0, no Ctrl, Meta, Shift or Alt, not already
   `defaultPrevented`); the Navigation API exists; and the entry before
   `navigation.currentEntry` is a **different document** whose URL,
   path and query, fragment ignored, equals the back link's resolved
   `href`. "Different document" is the entry's `sameDocument` relation
   to the current one being false, so index, page, `#section` on the
   same page, back does not return to the fragment (round 1, finding 9).
   A redirect behind `up` fails the URL test and follows the link, which
   is correct. Anything unproven follows the link, so history grows by
   one entry: that is the scriptless behaviour, not a failure. The
   prototype's `document.referrer` fallback (P/side-c3.js:29) is
   dropped: the referrer names the document's referrer, not the entry
   before the current one.
3. **Focus return, by a return record.**
   - **Written when a content page is left:** on `pageswap` (fired on
     the outgoing document for every navigation, including the
     activation of a prerendered page, before the new one is revealed),
     and on `pagehide` in engines without `pageswap`, shell.js writes
     `rst-shell-return` = the page's own path and query to
     `sessionStorage` (per tab).
   - **Read when an index is revealed, never while prerendering:** on
     `pagereveal` (which fires on first render, on activation of a
     prerendered page and on a back/forward-cache restore), or on
     `pageshow` in engines without it, and only when
     `document.prerendering` is false and the index view is showing at
     narrow width. A prerendered index runs its first scripts with a
     cloned storage before the reader has left the source page, so
     reading earlier would miss the fresh record (round 2, finding 9);
     `pagereveal` at activation is the first moment the record is
     there. It takes the record (reads and removes it).
   - **Focus, in order:** the nav link whose resolved `href` path and
     query equal the record; else the fragment's target, if the URL has
     one and it is a nav link; else nothing, leaving the browser's own
     focus. **Record first**, because the record is always fresher than
     the fragment: after returning to `/#nav-invoices` and then visiting
     Orders, a history-backed return restores the URL with the old
     `#nav-invoices` fragment, and fragment-first would focus Invoices
     (round 2, finding 8). The fragment is the scriptless mechanism and
     the scripted fallback when there is no record.
   - Writing when the page is left, rather than on a nav click, covers
     every way back: the back control, the browser's back button, and a
     deep link (a content page opened directly, then its back control),
     which the round-1 draft promised and did not implement. Storing a
     path rather than an `id` means an app need not give its nav links
     ids for the scripted path; the scriptless path still needs
     `#nav-…`. `sessionStorage` is used through a `try` that treats any
     throw (storage disabled, quota) as "no record", so the fragment
     rule still applies. The record is one-shot, so an index reached
     later in the same tab by other means consumes it at most once.

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
- Reduced motion: no opt-in, so navigation is instant; and, for the
  motion gate, a `@media (prefers-reduced-motion: reduce)` block that
  sets `animation: none` on every animated selector by exact selector.
  The gate (TestReducedMotionDisablesEveryTransition, ui/ui_test.go:
  367-394) reads only `TokensCSS()` today; it is extended to run over
  `ShellCSS()` too, unchanged in what it accepts, so shell.css cannot
  escape it. The class/attribute twin rules (ui/markup_v3_test.go:329,
  375) apply to shell.css too, but not the existing test as it stands:
  it carries a floor of 300 pairs (markup_v3_test.go:362) that a small
  file cannot meet. The pairing check is factored into a helper; tokens.
  css keeps its 300 floor, and shell.css gets its own test with a floor
  equal to the pairs it is written with (the console bar's
  `view-transition-name` rule and the type rules), so the check cannot
  pass vacuously on either file (round 2, finding 14).

A system back gesture that the browser animates itself (iOS Safari's
swipe) must not animate twice. The view-transition spec skips a
transition when the navigation already has a UA visual transition; the
by-hand check (§10.8) confirms it in Safari.

**Budget.** shell.js has its own contract test with an 8 KiB cap,
following busy.js's (ui/shim_test.go:405). rastrillo.js is untouched by
§4 (its §5 change is measured there).

**Vendoring.** `shell.js` and `shell.css` join `ui/vendored.go:19`, get
`ShellJS()`/`ShellCSS()`, are written by `rastrillo new` for every shell
like the rest of the vendored set (select.js and calendar.js already
ship to stage apps that never link them), and are checked by doctor.

Two consequences for an existing app, both in the changelog and the
upgrade notes:

- **Upgrading the module adds two files to the app's own vendoring
  test.** `TestVendoredAssetsMatchTheLibrary` ranges over
  `ui.VendoredAssets` and fails on a file it cannot read
  (cmd/rastrillo/new.go:764-779), so an app that upgrades without the
  new files goes red. `rastrillo doctor --fix` writes missing files
  ("wrote … was missing", doctor.go:607-664). This is the same step the
  busy.js split asked for (CHANGELOG.md, "vendor it and link it").
- **Deleting them needs a line.** Doctor accepts an absent file
  (doctor.go:175-189); the generated test does not, unless the name is
  in `vendoredIsMine`. An app on topbar, column or stage that deletes
  them adds `"shell.js": true, "shell.css": true` there. The docs say so
  beside the deletion advice, and the scaffold's comment on
  `vendoredIsMine` names the case.

The test (§10.6) scaffolds with the previous release's file set, removes
the two files, and asserts the vendoring test fails with the doctor
message, then passes after `doctor --fix`.

**The gallery serves them too.** The gallery's output map lists its
assets by hand (internal/designsystem/designsystem.go:136-142), so
adding to `VendoredAssets` does not publish them; `shell.js` and
`shell.css` are added there, and to the Getting Started page's asset
list (page.go:2177), whose test derives its expectation from
`VendoredAssets` (TestTheGettingStartedPageWeighsTheRealAssets,
designsystem_test.go:2631) and so fails until the page lists them. The
browser tests load the new demo documents with request interception
off and assert no 404 for either file.

### 4.6 Prerender, through a response header

The prototype prerenders the next section with inline speculation rules
(P/c3/index.html:15). The default CSP (serve.go:428-430) has no
`script-src`, so scripts fall back to `default-src 'self'` and an inline
rules block is refused; injecting one from shell.js is inline too. The
operator's ruling: ship it in H, on by default, with a switch to turn it
off.

**The header.** `securityHeaders` (serve.go:437-457), which already sets
the CSP on every response, also sets

```
Speculation-Rules: "/_speculation-rules"
```

(a structured-field list of one string, the form the header takes).
Browsers act on it for documents and ignore it elsewhere, so it goes on
every response rather than sniffing for HTML. Set there, it follows the
file's existing rule: an app's handler or `Options.Wrap` can replace or
`Del` it.

**The rules file.** `buildHandler` registers `GET /_speculation-rules`
on its own mux beside `GET /api/next-due` (serve.go:521-523), before
`mux.Handle("/", app)` (530) and so before the early return for an app
with no locales (532-534): both return paths serve it. The method-and-path
pattern is more specific than the app's `/`, so the framework answers it
even when the app has a catch-all. With locales, the locale middleware
wraps this mux (558); the unprefixed path still works because the
middleware negotiates a locale from the cookie or header when the path
has no prefix, rather than requiring one (localemw.go:36-45). The path
is exported as `rastrillo.SpeculationRulesPath`. The response is
`Content-Type: application/speculationrules+json` (a static file would
go out as `application/json`, which the browser refuses),
`Cache-Control: public, max-age=86400`, and a constant body:

```json
{"prerender":[{"source":"document","where":{"selector_matches":":is([rst-shell-sidebar],[rst-shell-console],.rst-shell-sidebar,.rst-shell-console) :is([rst-shell-nav],[rst-shell-back],.rst-shell__nav,.rst-shell__back) a[href]"},"eagerness":"moderate"}]}
```

Document rules scoped by selector, in both spellings, so they only ever
match sidebar and console navigation and the back control; on every
other page they match nothing. `moderate` prerenders on a 200ms hover
on desktop and on pointer-down on a phone. Prerender is same-origin by
default and every matched link is a GET; SKILL.md already says a GET
never mutates, and a handler can tell a prerender from its
`Sec-Purpose: prefetch;prerender` request header.

**The switch.** `Options.NoSpeculationRules bool`: true sends no header
and registers no route, so the path falls through to the app like any
other (a 404 from an app with a 404 fallback, whatever a catch-all
answers otherwise). `securityHeaders` gains the flag as a parameter. Named as a negative because the default is on
and a zero `Options` must mean on; `Options.CSP` is the precedent for a
field whose zero value is the framework default (serve.go:146-156).

**CSP, in two stages.** *Loading the rules:* the HTML standard exempts
a ruleset delivered by the `Speculation-Rules` header from CSP (it is
not a script, and `'inline-speculation-rules'` governs only inline
`<script type="speculationrules">`), so no CSP, the default or an app's
own `Options.CSP`, stops the file loading, and the default CSP needs no
change. *Acting on them:* each prerender is a same-origin navigation to
the app, and the prerendered document carries the app's own CSP like any
page. So CSP is **not** an opt-out, and the docs must not suggest it:
the opt-outs are `Options.NoSpeculationRules`, or `Del` of the header in
a handler or `Options.Wrap` (round 2, finding 12).

Rejected: `'inline-speculation-rules'` in the default CSP (widens every
app's policy and misses apps with their own `Options.CSP`); rules in a
vendored static file linked from the layout (there is no `<link>` form
for speculation rules; it is a `<script>` or the header).

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

- **The gallery's own frame is not in H.** It converts with B
  (gallery-usability), which already reworks that frame (operator,
  after round 1). The legacy CSS stays (§4.3), so it keeps working
  exactly as today, drawer included. Converting it is also more than a
  markup swap: about a hundred anchored rail entries in collapsible
  groups with a filter box, switchers inside `main` (which the index
  hides), and an Overview that would need a page of its own.
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
- A 0.14s drop-in, with `animation: none` on the same selector in a
  `prefers-reduced-motion: reduce` block, which is the form the motion
  gate requires (ui/ui_test.go:354-394).
- The account and language menus inside the card **expand in place**
  (static panel, no shadow), because a popover inside a popover has
  nowhere good to go on a phone (P/topmenu.css:59-81).
- **Every card rule is scoped to `@media (max-width: 799.98px)`**: the
  bar's position, the tail's box, the nav rows and their current-item
  style, the account and language panels made static, the summary's
  `::before` layer, its pressed fill and the drop-in. The round-1 draft
  leaned on the wide block's `display: contents` (ui/tokens.css:
  1758-1772) to undo them, and it cannot: `display: contents` removes
  the tail's own box and nothing else, so the account panel would stay
  static, the nav would lose its underline and the fixed layer would
  still swallow clicks on a desktop someone widened with the menu open
  (finding 11). Scoped, nothing needs undoing and the wide rules are
  untouched. The existing narrow rules stay as they are (unscoped,
  undone at 800px); the card rules come after them in the file and win
  at equal specificity inside the query.

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

The shell menu joins the light-dismiss list (ui/rastrillo.js:164-165),
and the tail sibling counts as inside it. The tail is the menu's content
but not its DOM child, so without that rule a click on the account menu
inside the card would close the card, and Escape from a plain nav link
in the card would find no menu at all. The prototype spelled this as a
separate file (P/topmenu.js); here rastrillo.js learns it in three
places (round 1, finding 12):

- **The selectors, in both spellings, written out.** The existing line
  derives class selectors by rewriting `[rst-x]` to `.rst-x`
  (ui/rastrillo.js:165), which would give `.rst-shell-menu`; the class
  is `.rst-shell__menu` (ui/tokens.css:1737). So after that line:
  `MENUS += ",[rst-shell-menu][open],.rst-shell__menu[open]"`, and
  `TAIL = "[rst-shell-tail],.rst-shell__tail"`.
- **A logical parent, only while the card is showing.** `menuAround(node)`
  is `node.closest(MENUS)`, or, when that is null, the open shell menu
  whose tail contains the node: `(t = node.closest(TAIL)) &&
  t.previousElementSibling` if that matches MENUS **and its summary is
  rendered** (`summary.getClientRects().length > 0`). At 800px and above
  the summary is `display: none` (ui/tokens.css:1759) while a
  `<details>` opened at 390 and then widened keeps `open`; without the
  condition, Escape from the account menu would climb to that hidden
  menu and focus a summary nobody can see (round 2, finding 10). With
  it, the wide bar behaves exactly as today: Escape focuses the account
  summary. The open, hidden shell menu is still closed by the next
  outside click or Escape, which is harmless and means a reader who
  narrows the window again does not find the card open.
- **Containment and the climb both use it.** `closeMenus(except)` keeps a
  menu open when it contains `except` **or** is `menuAround` of it,
  directly or through the climb. The Escape handler's host is
  `menuAround(activeElement)`, and its climb
  (ui/rastrillo.js:189-191) steps with `menuAround(host.parentElement)`
  instead of `host.parentElement.closest(MENUS)`. From a nav link in
  the card the host is the shell menu; from an item in the account menu
  inside the card the climb goes account menu, then shell menu. Either
  way focus lands on the Menu summary, which stays rendered.

Tests drive Escape from a plain nav link, from inside the open account
menu, and with the class spelling of the whole topbar (§10.4).

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

Measured: rastrillo.js is 9,784 bytes. The change is two selector
constants, a five-line `menuAround`, two call sites and their comment,
about 1 KB with the house's comment density; the contract test records
the before and after, as it has for every change (ui/shim_test.go:
201-261). It must stay under 16,384.

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

### 8.1 Gate matrix

Every existing gate H touches, and what happens to it. "Extended" means
the same assertion over more input, never a weaker one.

| Gate | Where | In H |
|---|---|---|
| Class/attribute twins, no orphans | ui/markup_v3_test.go:329, 375 | helper shared; tokens.css keeps its 300-pair floor (362); shell.css gets its own test and floor (§4.5) |
| Both spellings compute the same | ui/markup_v3_browser_test.go:330 | fixture gains every new class and the row, row-menu and card states |
| Styleguide samples are styled, in the group, carry `rst-danger` | ui/ui_test.go:1729, 1748, 1981 | pass unchanged: the list-grid sample stays raw markup, now kept equal to the partial's output by a new test (§3.5) |
| Reduced motion disables motion | ui/ui_test.go:367 | extended to `ShellCSS()`; tokens.css's new drop-in and flash carry their `none` |
| No inline styles in partials and layouts | ui/ui_test.go:3379 | covers `row-menu` and the changed layouts, unchanged |
| Layouts parse and render with nil data | ui/ui_test.go:2845 | unchanged; the new blocks have defaults |
| Shim size | ui/shim_test.go:262 | unchanged cap; new before/after note |
| shell.js contract | new, beside TestBusyContract (shim_test.go:380) | 8 KiB cap |
| Vendored set in scaffold and doctor | cmd/rastrillo/new_test.go, doctor_test.go:315, 555 | two more files |
| Examples' tokens.css byte copies | examples/blog/internal/blogtest/tokens_test.go:21, and tickets | re-copied in every step that touches tokens.css (Rollout) |
| Gallery page budget, 128 KiB | internal/designsystem/designsystem_test.go:76, 396 | unchanged; the demo split into documents makes each lighter; the new shell previews are held to it |
| Header rule on every page with a header | internal/designsystem/header_rule_test.go:23 | unchanged; covers the new demo documents and shell previews because it walks `Render()` |
| Getting Started lists the real assets | designsystem_test.go:2631 | fails until the page lists the shell files (§4.5) |
| Contrast | ui/contrast_test.go:295 | no new pair: accent text on accent-soft (the card's current item) is already held to 4.5:1 |
| axe WCAG 2.2 AA, 320px reflow | internal/designsystem/a11y_test.go:477, 824 | extended to the index view and the new demo documents |
| gallery.js size | designsystem_test.go:1742 | untouched: H adds no gallery script |

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
  examples/blog/internal/blogtest/tokens_test.go:21) and are re-copied
  in the same step as every tokens.css change (Rollout). Their layouts
  are hand-written copies of the old shells and keep working on the
  legacy rules; converting them is not in H.

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
  TEXTY set, P/measure.js:5) computes at least 16px, including a textarea
  and a search input placed inside a bulk bar and a menu panel (small
  parents), which is where the round-1 draft would have failed. The
  primary input computes exactly `--rst-fs-lg`, 19px (the 1em bug would
  give 16).
- **Targets, measured as activation areas.** Two separate steps.
  *Visibility filter:* an element is skipped only if it is not rendered
  (`checkVisibility()` false, or a zero-size box, or clipped as
  `rst-sr-only`). *Measurement and occlusion,* for everything that
  passes: the test takes the element a tap activates, not the element
  drawn (a checkbox or radio inside a label is measured as the label, a
  stretched link as its row, P/measure.js:61-63), and
  `elementFromPoint` at that area's centre must return the control, its
  label, or a descendant of either (a switch's track, a summary's icon);
  anything else is an **occlusion failure**, not a skip (round 2,
  finding 11). Every area is at least 44×44, except links in running
  text and calendar days below a 342px viewport (§1.4).
- **Inventory completeness (unit, no browser).** The test lists every
  rule in tokens.css that has `cursor: pointer` or styles an `a`,
  `button`, `summary` or `label`, and fails if one names no idiom that
  the fixture renders, so a new control needs a row in §1.4's table.
- **Calendar:** at 390 with touch, the open calendar is docked inside
  the viewport and every day cell is at least 44×44; at 320, every day
  is at least 24px wide and 44px tall and the panel does not overflow;
  with the date field at the inline end of a field row, the same.
- **1024×768, touch** (the pointer half alone) and **600×800, mouse**
  (the width half alone): the same assertions.
- **1280×900, mouse:** desktop density pinned. Body 14px; the four
  tokens at 17/14/12.5/11.5px; primary input 17px; button heights
  28/34/44px (ui/tokens.css:63-68); kebab 26×26; checkbox box 16px in a
  24×24 label; calendar 18rem. Any change fails.
- **Unit (no browser):** in tokens.css the bare-element floor's selector
  is wholly inside `:where()`; the input floor sits after `.rst-input`
  and before `[rst-input~="primary"]`; the textarea floor after
  `.rst-textarea`; the search floor after the search input's font-size
  rule.

### 10.2 Whole rows

For each idiom in §2.2 with a primary link (list-row-action; list grid
with `rst-nm`; list grid with a person link; list grid with checkbox,
status pill and kebab; the demo's Requests list), at 1280 and at 390
with touch:

- A click at the row's far empty area (inline end minus 12px, vertical
  centre, and a point inside an empty cell) navigates to the primary
  href. `document.elementFromPoint` at that point is the primary link.
- A click on the action pill goes to the pill's href; on the kebab opens
  the menu and does not navigate; on the checkbox itself, and on its
  label's padding, toggles it and does not navigate. `elementFromPoint`
  at each control's centre is that control. Each is at least 44×44 at
  390; at 1280 the checkbox label is 24×24.
- With the first row's menu open over the second row, a click on a panel
  item that lies over the second row's kebab or checkbox hits the item.
- A list grid whose `--rst-cols` ends in a literal `32px`, at 1024 with
  touch: the 44px kebab does not overlap the previous cell's box.
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
  `document.activeElement` is the nav link that was followed. Then:
  - index, page, a same-page `#fragment` link, back control: the link is
    followed (the entry before is the same document), and the index
    loads with focus on the right nav link;
  - a deep link (content page opened directly), back control: the link
    is followed, and focus lands on the nav link for that page, from
    the return record;
  - the same with nav links that carry no `id` and a back link with no
    fragment: focus still lands, from the record;
  - with `sessionStorage` throwing (the test overrides it to throw),
    and a fragment: focus lands on the fragment's link; with neither,
    focus is the browser's default and nothing throws;
  - Ctrl, Meta and Shift clicks and a middle click on the back control
    are not intercepted;
  - an `up` that redirects: the link is followed.
  A recorded `pagereveal` sees type `forward` going in and `back`
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
- Scripts on: Escape from a plain nav link in the card closes it and
  focuses the Menu summary; with the account menu open inside the card
  and focus on one of its items, one Escape closes both and focuses the
  Menu summary; opening the account menu inside the card keeps the card
  open. The same three with the whole topbar in the class spelling
  (`.rst-shell__menu`, `.rst-shell__tail`).
- **Resize while open:** open the card at 390, resize to 1280: the bar
  is today's (tail inline, account panel absolute and closed state as
  before, nav underline on the current item), and a click on a link in
  `main` follows it (no leftover fixed layer). Back to 390: the card
  again, closed or open as the `<details>` left it.
- The current item's computed `background-color` is
  `--rst-accent-soft`; its `box-shadow` is `none` and its
  `border-inline-start-width` and `border-block-end-width` are 0.
- 800px and above: today's bar, unchanged.
- Console: the same, plus the index and back legs of §10.3.

### 10.5 Row menu

- **Unit:** every valid key combination renders; the summary's name is
  "Actions for {name}"; a `Danger` item is `<a class="rst-danger">`;
  Execute fails, naming the item, for no `Label`, both or neither of
  `Href` and `Action`, `Danger` without `Href`, and `Hidden` without
  `Action`; the `<hr>` appears before the first danger item after a
  non-danger one and nowhere else; POST items are `<form method="post">`
  with one submit button and the `Hidden` pairs in order; `MenuGroup`
  defaults to `rst-menus`; `list-row-action` with no `Menu` renders byte
  for byte today's output, and with `Menu` and `ActionHref` renders the
  pill, then the kebab named for `Main`; a struct caller without a
  `Menu` field still executes.
- **Browser**, scripts off and on: opening one row's menu closes
  another's (scripts off, native group); outside click and Escape close
  it (scripts on) and Escape returns focus to its summary; near the
  viewport's bottom the panel opens upward in an engine with anchor
  positioning; clicking an item never navigates to the row's href; a
  POST item shows the busy spinner beside its label and the item's
  box does not change size, with a still ring under reduced motion;
  items and trigger are 44px at 390 with touch.
- **a11y:** axe on the gallery sample in every theme and scheme; every
  control named (TestEveryControlHasAnAccessibleName, ui_test.go:1301).

### 10.6 Scaffold and vendoring

`rastrillo new --shell=sidebar` and `--shell=console` write the two pages
and the second route, and the scaffolded app builds and passes its own
tests; the other shells are unchanged. `shell.js` and `shell.css` are
written for every shell and match `ui.ShellJS()`/`ShellCSS()`. Doctor
checks them, and prints its layout advisory for an old layout (§7) with
exit code 0.

**Upgrade from an existing scaffold:** a scaffold with the two shell
files removed (the previous release's set) fails its own
`TestVendoredAssetsMatchTheLibrary` with the existing missing-file
diagnostic, `read vendored shell.js: …` (cmd/rastrillo/new.go:773; the
doctor instruction is only on the byte-mismatch branch, 779, and an
existing app's test file is not regenerated by a module upgrade); after
`rastrillo doctor --fix` it passes. A scaffold that deletes them and
lists both in `vendoredIsMine` passes.

### 10.6a Prerender header

- **Unit (serve_test):** every response carries
  `Speculation-Rules: "/_speculation-rules"`; `GET /_speculation-rules`
  answers 200, `application/speculationrules+json`, and a body that
  parses as JSON whose one rule is `source: document` with the selector
  in both spellings; with `Locales` set the path is still served
  unprefixed, and it is served on the no-locales path too; with an app
  mux holding a catch-all `/`, the framework still answers the rules
  path; with `Options.NoSpeculationRules` no header is sent and the path
  reaches the app (a fixture app with a 404 fallback answers 404, one
  with a catch-all answers its own body); a handler that `Del`s the
  header removes it; the CSP header is byte for byte `defaultCSP`.
- **Browser:** with CDP's `Preload` domain enabled, on a sidebar page at
  390 with touch, a pointer-down on a nav link starts a prerender of
  that URL and no CSP violation is reported; a link outside the shell's
  nav and back control starts none; on a stage (sign-in) page nothing
  is prerendered.

### 10.7 Contracts

shell.js: an IIFE with its contract comment, no `eval`/`new Function`,
two-space indent, under 8 KiB, and names the three behaviours.
rastrillo.js: still under 16 KiB, and mentions both spellings of the
shell-menu selector, `TAIL` and `menuAround`.

### 10.8 By hand before merge

In the branch description: iOS Safari and Android Chrome on a real
phone: no zoom on focus of any field; the slide both ways; the system
back swipe does not animate twice; VoiceOver and TalkBack on the index
(h1, rows), the back control's name, and the card.

## 11. Strings for copy review

All drafts, none written into a file until reviewed. No em dashes. The
operator reviews them once the adversarial review is satisfied.

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

One branch, in this order, each step green under `make ci` (which
tests the example modules, Makefile:31, 98-105) before the next. Every
step that changes `ui/tokens.css` re-copies it into
`examples/blog/static/` and `examples/tickets/static/` in the same
commit, because their byte-equality tests would otherwise fail in
between (round 1, finding 16).

1. Sizing (§1), with its tests.
2. Whole rows (§2).
3. `row-menu` (§3) and the gallery sample.
4. The topbar card and rastrillo.js (§5).
5. The sidebar and console index and back, shell.js and shell.css, the
   scaffold, the gallery's asset list, **and in the same step** the
   gallery demo and shell previews with the gallery tests that drive
   them (§4, §4.9). They cannot be separate steps: the demo and the
   previews render through `ui.Layout` (internal/designsystem/page.go:
   1857), so the layout change reaches them at once, and the narrow
   gallery tests wait for the drawer this step removes
   (internal/designsystem/a11y_test.go:515; round 2, finding 13). The
   gallery's own hand-written frame is untouched until B.
6. The speculation-rules header (§4.6).
7. Docs, SKILL.md, doctor's advisory, changelog (§6, §7).

## Out of scope

- Converting the gallery's own frame: it converts with B,
  gallery-usability (§4.9).
- Converting the examples' hand-written layouts (§9).
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
- **The drawer's CSS lives on as legacy** for old layouts, the examples
  and the gallery until B. It is dead weight in tokens.css until both are gone; the
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
- **Prerender costs server work on hover.** `moderate` eagerness fetches
  a page a desktop reader hovers for 200ms and may never open. It is
  scoped to shell navigation, which is few links; an app with expensive
  pages turns it off with `Options.NoSpeculationRules`.
- **The calendar docks to the bottom on touch screens**, away from its
  field.
  A field in the bottom 20rem of a short viewport is covered while the
  calendar is open, which is how a phone's own date picker behaves.

## Review log

### Round 1 (Astra, 2026-09-30): not ready; 3 Blockers, 13 Important, 2 Minor

1. Blocker, the textarea floor is reset by `[rst-textarea] { font: inherit }`
   after it. Resolved: each floor sits after its own component's reset,
   with a unit test on position and browser legs in small parents (§1.3,
   §10.1).
2. Blocker, the lifting selector's `:not()` was a descendant combinator.
   Resolved: attached to `:is()`, and the tests hit-test the controls
   themselves (§2.3, §10.2).
3. Blocker, seven 44px days do not fit an 18rem calendar. Resolved: the
   panel is 326px on touch, docked to the viewport's bottom below 40rem,
   with days exempt below a 342px viewport under 2.5.5's equivalent
   exception (§1.4, Decisions).
4. Important, the target inventory was incomplete and measured the
   wrong box. Resolved: a full inventory from tokens.css, a unit test
   that keeps it complete, and activation-area measurement (§1.4, §10.1).
5. Important, wide touch grids keep a 32px kebab track. Resolved:
   `--rst-col-menu`, with the literal case shown to degrade into the gap
   (§2.3).
6. Important, §3.4 relied on lifting the `<details>`. Resolved: summary
   only, restated in §3.4, with a test of a panel over later rows (§2.3,
   §10.2).
7. Important, `Danger` with `Action` had no destination, and `Menu` was
   unspecified. Resolved: `Danger` requires `Href`, invalid items fail at
   Execute through `rowMenuItems`, and `Menu`'s shape and order beside
   the pill are defined (§3.1).
8. Important, the spinner-replaces-label look is `[rst-btn]` only.
   Resolved: menu items show the spinner beside the label, as every
   other button does, and the test checks the box does not move (§3.1,
   §10.5).
9. Important, the referrer fallback could go to the wrong entry.
   Resolved: history is reused only when the Navigation API proves the
   previous entry is the `up` document; otherwise the link is followed
   (§4.5, §10.3).
10. Important, deep-link focus return was not implemented. Resolved: a
    return record written on `pagehide`, with a defined fallback order
    and storage failure handled (§4.5, §10.3).
11. Important, `display: contents` does not undo the card's descendant
    rules. Resolved: every card rule is scoped below 800px, and a resize
    test (§5.1, §10.4).
12. Important, Escape could not find the shell menu from the tail, and
    the class spelling was wrong. Resolved: explicit selectors in both
    spellings and `menuAround` for containment and the climb (§5.3,
    §10.4).
13. Important, deleting the shell files breaks the scaffolded vendoring
    test, and upgrades add two files. Resolved: `vendoredIsMine` for
    deletion, `doctor --fix` for upgrades, and an upgrade test (§4.5,
    §10.6).
14. Important, the gallery does not serve new vendored files by itself.
    Resolved: its output map and Getting Started list gain them, with a
    load test (§4.5).
15. Important, the motion gate and the twin gates read tokens.css only.
    Resolved: both are extended to shell.css and every new animation
    carries its exact-selector `none` (§4.3, §4.5, §5.1, §8.1).
16. Important, examples' copies deferred to the last step would break
    every step before it. Resolved: re-copied with every tokens.css
    change (Rollout).
17. Minor, gates named incompletely. Resolved: §8.1's matrix.
18. Minor, over-broad claims about scaling, "every size unchanged", and
    the desktop checkbox. Resolved: §1.2 states what does not scale,
    §1.5 says density and names the three deliberate desktop changes,
    and the desktop checkbox label grows to 24×24 (§2.3).

Operator, after round 1: prerender ships in H (§4.6); the gallery's own
frame converts with B, not H (§4.9); copy review after this review (§11).

### Round 2 (Astra, 2026-09-30): not ready; 1 Blocker, 12 Important, 3 Minor

Round-1 findings re-verdicted: 1, 2, 5, 6, 7, 9, 11, 13, 14, 16 and 18
resolved; 3, 4, 8, 10, 12, 15 and 17 partly, each completed by a round-2
finding below.

1. Blocker, the docked calendar's width assumed a border box on a
   content box, and the base rule's physical `top: 100%` survived
   `position: fixed`. Resolved: `border-box`, `top: auto`, every inset
   and margin reset, the width derived from `--rst-tap`, at a weight that
   beats `is-above` (§1.4).
2. Important, a wide touch screen could still overflow the calendar
   inline, and the 2.5.5 claim was unsupported. Resolved: docked on every
   small or touch screen; sub-44px days below 342px are a stated design
   exception, not a 2.5.5 claim (§1.4, Decisions).
3. Important, several treatments sized containers, not activation
   areas. Resolved: the search input stretches, inline elements become
   flex, both axes are stated, and the legacy drawer summary is in the
   inventory (§1.4).
4. Important, the lifting rule would override components' own
   positioning. Resolved: the whole selector is inside `:where()`, and
   a fixture puts a switch, a date field and an enhanced select in a row
   (§2.3).
5. Important, `T` ignores interpolation. Resolved: `Tf` for both names
   (§3.1, §4.2).
6. Important, the styleguide sample cannot call a partial. Resolved: it
   stays raw markup, held equal to the partial's output by a test (§3.5).
7. Important, an inline spinner can resize the panel. Resolved: a
   reserved spinner slot on row-menu POST buttons (§3.1).
8. Important, fragment-first focus restored a stale section. Resolved:
   the return record wins over the fragment (§4.5).
9. Important, focus restoration and prerender activation. Resolved:
   the record is written on `pageswap` and read on `pagereveal`, never
   while `document.prerendering` (§4.5).
10. Important, `menuAround` could focus a hidden summary after a resize.
    Resolved: the shell menu is a logical parent only while its summary
    is rendered (§5.3).
11. Important, the hit test skipped occluded controls. Resolved:
    visibility filter and occlusion failure are separate steps, with
    descendants accepted (§10.1).
12. Important, CSP was described as an opt-out. Resolved: the two
    stages are described separately; the opt-outs are the switch and
    the header (§4.6).
13. Important, the rollout split the shell change from the gallery
    conversion it breaks. Resolved: one step (Rollout).
14. Minor, the twin gate's 300-pair floor. Resolved: a shared helper,
    per-file floors (§4.5, §8.1).
15. Minor, the upgrade test's expected message. Resolved: the existing
    missing-file diagnostic (§10.6).
16. Minor, "404" and the locale wording for the rules route. Resolved:
    registration before both returns, fall-through when disabled, and
    the middleware's negotiation stated (§4.6, §10.6a).
