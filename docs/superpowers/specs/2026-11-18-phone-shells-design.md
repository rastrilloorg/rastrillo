# Phone shells: one hierarchy, one header row, an icon scheme switch

Date: 2026-11-18. Branch `phone-shells` (rastrillo). Work lands in
Fiesta first (a 1.4.0 release; 1.3.0 and 1.3.1 already shipped), then Rastrillo vendors it.

## Why

Paul keeps seeing Rastrillo apps whose phone layout has a "Menu" item in
the header instead of Fiesta's phone navigation. Two causes:

1. The `topbar` shell, the obvious pick for "links in a header", hides its
   nav behind a Menu card below 800px. Links in a header make sense on a
   desktop; on a phone they break the hierarchy, because every page
   floats the sections above itself instead of sitting one level below
   them.
2. The skill never says which shell to pick, and describes the Menu card
   as normal behaviour (`skill/design.md`, "Shells on a phone").

Separately, apps keep ending up with an unattractive light/dark switcher:
`scheme-menu` is a dropdown holding three words, sitting loose in a bar
or footer.

## Goals

- Every shell with navigation shows one level of the hierarchy at a time
  on a phone. The topbar adopts it; sidebar and console already have it.
- On a phone there is never more than one header row.
- The colour scheme is a three-icon segmented control (sun, moon,
  monitor), inside the account card wherever a shell has one.
- The skill tells agents which shell to pick and never to add a Menu
  button, hamburger or drawer.

## Non-goals

- No change to any shell at 800px and up, except where the scheme
  control moves (below).
- No change to `stage`.
- No new navigation patterns: no bottom tab bar, no drawer.

## Anti-goals

- A "Menu" button that holds navigation, in any shell, on a phone.
- Two stacked header rows on a phone (today's console: bar, then back
  strip).
- Breaking an existing app's own `layout.html`. Old topbar and console
  markup must keep rendering exactly as today; the new behaviour is opted
  into by the new markup, as the sidebar's phone index was in its
  release ("Upgrading to the phone index", `docs/site/templates.md`).
- Changing Fiesta's frozen 1.0 pixel tree. Old markup renders as before;
  any rendering change to markup 1.0 already wrote needs a named, scoped
  exception with its own regression test, as 1.2.0 did
  (`hack/catch-up-1.2.md`, `check/baseline_test.go` `newOnOldMarkup`).

## The design

### 1. The hierarchy on a phone (topbar)

Below 800px a topbar app is a tree: **Menu › top-level pages › their
children.** The app opens on its real home page; the list of sections is
one level up.

```
/  (home)                  /menu                      /posts/42
┌───────────────────────┐  ┌───────────────────────┐  ┌───────────────────────┐
│ ‹ Menu   Blog     (◉) │  │          Blog     (◉) │  │ ‹ Posts  Blog     (◉) │
├───────────────────────┤  ├───────────────────────┤  ├───────────────────────┤
│ Latest posts          │  │ Home                › │  │ Post title            │
│ …                     │  │ Archive             › │  │ …                     │
│                       │  │ About               › │  │                       │
```

- **The Menu page is a real page at `/menu`**, marked
  `{{define "view"}}index{{end}}`, exactly as the sidebar's index is. Its
  rows are the topbar's `nav` links, laid out as the sidebar/console
  phone index lays out the rail (full-width rows, chevrons, ids for
  focus return). `rastrillo new --shell=topbar` scaffolds it, with its
  route. At 800px and up `/menu` renders the same links as a plain list
  inside the normal topbar page; few people will see it, and it must not
  look broken.
- **Every other page is a content page** with an `up` block, as in
  sidebar and console. The topbar's default `up` is the Menu page with
  the page's own nav row as the fragment (`/menu#nav-home`), so a page
  that forgets still has a way back.
- **The back link carries a label.** New block `upLabel` (name is the
  builder's to settle, see below). Default in topbar: "Menu"
  (`rastrillo.ui.shell_menu`, already translated). A deeper page names
  its parent: `{{define "upLabel"}}Posts{{end}}`. Sidebar and console keep
  their default, "Sections" (`rastrillo.ui.shell_up_label`), and gain
  the same block.
- `shell.js` and `shell.css` now apply to topbar too: the slide between
  index and page, Back reusing history, focus returned to the row. The
  topbar layout links them, as sidebar and console do. Speculation-rules
  prerendering of nav links (today sidebar/console only, `Serve`) extends
  to topbar.
- **Chosen over** a `#menu` fragment that covers the current page with
  the nav already in its HTML: no route and free for existing apps, but
  a second mechanism beside the sidebar's index page, without the slide,
  focus return or prerender. Paul accepted the real page.

### 2. One header row on a phone (topbar, console, sidebar)

Below 800px every shell with chrome shows exactly one header row:

- **inline start:** the back link (`‹ Menu`, `‹ Posts`, `‹ Sections`), absent
  on the index;
- **centre:** the app's name (the `brand` block);
- **inline end:** the account button (the avatar, below).

When the name does not fit beside the other two it is hidden (not
truncated mid-word, not wrapped to a second row); the back link and the
avatar always stay. The centring is true centring on the row, not centre
of the leftover space, when it fits. RTL mirrors it.

- **Console:** today a bar row plus a back-strip row; becomes one row.
- **Sidebar:** today a back strip only on content pages and the
  heading-plus-avatar on the index; it gets the same row, so all three
  shells look alike on a phone. On the index the row's centre is the app
  name and the index's existing `<h1 rst-shell-title>` is that name: one
  heading, not a bar plus a heading.
- **Topbar:** the bar becomes this row.
- The page's own heading (`<h1>` in content) follows as content, not as a
  second header.
- Existing tests that pin today's phone strip (for example
  `TestThePhoneHeaderStripIs56Pixels`) are updated to the new row; its
  height stays 56px unless the builder finds a reason, recorded.

### 3. The account button and card

- On a phone, in every shell, the header row's end is the **person's
  avatar**: the `profile` block, as the console's index already shows
  and the sidebar's rail foot already is. Empty avatar with accessible
  name "Account" when no one is signed in (the existing placeholder).
- **No visible "Menu" label and no hamburger.** Paul: the console's Menu
  is not navigation. Fiesta reserves the hamburger (`menu` icon) for
  navigation and the kebab for row actions
  (`internal/gallery/copy.go`, `gallery.icons.kebab`), so the avatar is
  the honest icon for "you and your settings". Paul did not object to
  the avatar when offered avatar vs kebab.
- Topbar gains the `profile` block (sidebar and console have it). The
  `account` block keeps its meaning in every shell: the links only.
- The card's contents, in order: the person (name, email), Appearance
  (the scheme control), Language, then the account's links. On a desktop
  the topbar and console keep an account dropdown at the bar's end with
  the same card contents; the sidebar keeps its rail-foot dropup.

### 4. The scheme control

Replace the dropdown-of-words with a **three-icon segmented control**:
sun (Light), moon (Dark), monitor (System), side by side, the current one
pressed.

```
┌──────────────────────────────┐
│ Grace Hopper                 │
│ grace@example.com            │
├──────────────────────────────┤
│ Appearance  [sun|moon|mon]   │
│ Language    English       ▾  │
├──────────────────────────────┤
│ Settings                     │
│ Sign out                     │
└──────────────────────────────┘
```

- **Where:** inside the account card / dropup in topbar, sidebar and
  console, at every width. It leaves the bar and the rail foot's top
  level. In `column` (no account menu) the same control sits in the
  footer row (`rst-page-foot`), replacing the dropdown there.
- **Markup:** still `[rst-scheme]` holding one `<form method="post">` per
  choice with a hidden `scheme` input, so it works with scripts off and
  `fiesta.js`'s in-place switch, `aria-current` marking and `rst-scheme`
  event keep working unchanged, as does Rastrillo's cookie write in
  `rastrillo.js`. Each button is icon-only with the choice's name as its
  accessible name (`rastrillo.ui.scheme_light` etc., already
  translated), and a visible tooltip is not required. The group is
  labelled "Appearance" (new key; reuse `rastrillo.ui.shell_scheme` if
  its wording fits). Target size meets WCAG 2.2 2.5.8 like the
  `seg-tabs` buttons.
- Uses Lucide `sun`, `moon`, `monitor` from Fiesta's icon set.
- `fiesta.js` today "closes the menu" after a switch. Inside the account
  card that would close the card; the builder decides whether the card
  stays open (likely better: the reader sees the change and can switch
  back) and records why.
- The old dropdown markup keeps rendering for old layouts. The
  `scheme-menu` partial in Rastrillo writes the new control; whether it
  keeps a `"Dropdown"` option for old callers is the builder's call.

### 5. Examples show only the new shape

Paul's example of the transgression is Fiesta's own gallery:
`layout-console`, Mobile, shows "Party planner" with a "≡ Menu" button,
then a second row "‹ Sections", then the page. Every example we publish
must show one header row and no Menu label. Old markup keeps working for
apps that have it, but no example teaches it.

- Fiesta: `examples/layout-console`, `layout-console-page`,
  `layout-sidebar`, `layout-sidebar-page`, `layout-topbar` (plus the new
  topbar index and page examples), `shell-sidebar`, `scheme-menu/*`,
  and every other example whose markup carries `rst-shell-back`,
  `rst-shell-menu` or the scheme dropdown. Their blurbs in
  `examples/prose.json` ("On a phone, the bar's menu opens as a card…")
  are rewritten, in every locale the gallery ships.
- Fiesta's gallery chrome itself (`internal/gallery/page_tmpl.go`, its
  own back strip, and the demo app in `demo.go`/`frame.go`) follows the
  same rule on a phone.
- Rastrillo: `examples/blog`, `examples/notes` and any other example
  app, `ui/testdata` pages, the design-system pages
  (`internal/designsystem`), and the rastrillo.org site's shell samples.
- Test fixtures that exist to prove old markup still renders keep it,
  named as legacy (as `sizing.html`'s `legacy-drawer` is).
- A gate in Fiesta fails a published example (not a legacy fixture) that
  renders two header rows or a visible "Menu" label below 800px, so it
  cannot come back.

### 6. Guidance (Rastrillo skill and docs)

- `skill/design.md` "Shells and colour scheme": pick the shell by the
  app's shape. A single list and its detail pages: `column`. Sections a
  person moves between: `sidebar`, `topbar` or `console`; all three show
  the sections as an index on a phone. Mark the index with `view`, give
  every other page `up` (and `upLabel` when its parent is not the
  index). Never add a Menu button, hamburger or drawer. The scheme
  control ships in the account card; never hand-roll one.
- `skill/screens.md` step 4 and the SKILL.md index row point to that.
- `docs/site/templates.md` "Shells" and "On a phone" sections, plus an
  "Upgrading" section for topbar and console layouts, written like the
  existing "Upgrading to the phone index".
- Fiesta's `DESIGN.md` Navigation paragraph, which still describes the
  old drawer, is rewritten to describe the index, the one header row and
  the account card. Fiesta `CHANGELOG.md` and the gallery's shell demos
  show the new behaviour.
- A skilleval task (`hack/skilleval/tasks/`): an app with three
  sections; the check fails on a nav inside `[rst-shell-menu]`, requires
  a `view` index page, and requires the scheme control inside the
  account card.

## Order of work

1. **Fiesta 1.4.0:** `css/tokens.css`, `css/shell.css`, `js/shell.js`,
   `js/fiesta.js` if needed, `examples/layout-*` (topbar gains
   `layout-topbar-page` and an index example, like sidebar/console),
   `examples/scheme-menu`, gallery demos, browser checks, `DESIGN.md`,
   `CHANGELOG.md`, release (see the Fiesta release recipe).
2. **Rastrillo:** bump the Fiesta pin, `ui/layouts/{topbar,console,
   sidebar,column}.html`, `ui/partials/scheme-menu.html`, the
   `rastrillo new` scaffold for topbar (index + one section + `/menu`
   route), `rastrillo doctor` hints for old topbar/console layouts,
   docs, skill, skilleval task. `examples/blog` (a topbar app) moves to
   the new layout as the worked example of a content home page under
   `‹ Menu`.

## Constraints

- Strict CSP: no inline styles or scripts.
- Works with scripts off: the index and back link are plain links, the
  card is `<details>`, the scheme control is forms.
- 320px with no horizontal scroll; RTL in every language Fiesta ships.
- Focus order equals visual order at every width (the console's
  "nothing moves" rule); no `order` or grid reshuffles that break it.
- Page weight: each page's framework CSS and JS stays within 16 kB
  gzipped (`make perf`); topbar pages now load `shell.js`/`shell.css`.
- Fiesta CI runs on the amadan fleet; agents gate on hub CI, not long
  local suites.

## Left to the builder

- The `upLabel` block's name, and whether it can be derived instead of
  written (it cannot be read from `up` in a template; see the sidebar
  layout's comment on why `view` and `up` are two blocks).
- How the old topbar/console markup is told apart from the new (an
  attribute value on the root, as the sidebar used `view`).
- Whether the card stays open after a scheme switch.
- Exact sizes, spacing and the name-hiding mechanism (container query,
  grid with `minmax`, or similar), within the rules above.

## Calls made after the builder check

Astra read this document against both codebases and raised three
questions. Each is settled here as the author's call, for Paul to check.

1. **Old layouts get the icon scheme control automatically.** Existing
   layouts call `scheme-menu` with no option (`ui/layouts/topbar.html`),
   and the partial is the framework's, so changing it changes every app
   on upgrade. That is wanted: the goal is that apps stop shipping the
   dropdown of words. An old layout shows the three icons where its
   dropdown was (bar, footer or rail-foot card); moving it into the
   account card takes the new layout. No `Dropdown` option.
2. **The sidebar's avatar keeps its one element and its reading order.**
   On a phone the avatar sits at the end of the header row but stays
   last in the DOM, after the nav, as it ships today (`tokens.css`
   lifts the rail foot; `ui/layouts/sidebar.html` explains why there is
   never a second copy: app blocks can carry ids). This is the one
   recorded exception to "focus order equals visual order"; desktop
   keeps the rail-foot dropup. The topbar's `/menu` page has the same
   exception (its rows are the bar's nav, before the account button);
   every content page, and the console everywhere, reads in visual order.
3. **A topbar page with no `up` goes back to `/menu`, no fragment.** The
   scaffold pairs each nav row's id with its page's `up`
   (`/menu#nav-x`), and `shell.js` returns focus to the matching row
   when scripts run. No server-side page-to-section contract.
