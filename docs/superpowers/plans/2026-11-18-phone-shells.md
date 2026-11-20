# Phone Shells Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On a phone, every shell with navigation shows one level of the hierarchy under one header row (back, app name, avatar), the topbar included, and the colour scheme is a three-icon control in the account card.

**Architecture:** Part A changes Fiesta (CSS, shell.js, examples, gallery, checks) and releases 1.4.0. New behaviour is keyed to new markup, so 1.0 to 1.3 markup draws as before. Part B pins Fiesta 1.4.0 in Rastrillo and moves its layouts, `scheme-menu` partial, scaffold, examples, docs and skill onto the new markup.

**Tech Stack:** HTML, CSS (`light-dark()` floor: Chrome 123, Safari 17.5, Firefox 121, so `:has()` is free), vanilla JS, Go `html/template`, chromedp browser checks (`-tags browser`).

**Spec:** `docs/superpowers/specs/2026-11-18-phone-shells-design.md` (rastrillo, branch `phone-shells`). Read it first; this plan argues from it.

## Global Constraints

- Phone means below 800px. Nothing at 800px and up changes except where the scheme control and language menu now sit (inside the account card).
- One header row on a phone, 56px tall, targets at least 44px, centred (the existing `TestThePhoneHeaderStripIs56Pixels` numbers).
- Old markup draws exactly as before. The 1.0 photographs (`check/oneoh_browser_test.go`) and declaration gates (`check/baseline_test.go`) must stay green with no new `renderingException` and no new `newOnOldMarkup` entry unless the task says so and names why.
- A shell root is new-style if and only if it contains `[rst-shell-up]`. Every new-style page emits that element, the index included (hidden there).
- No visible "Menu" label and no hamburger icon in any new-style example or layout.
- Strict CSP: no `style` attributes, no inline scripts.
- Works with scripts off: links, `<details>`, and forms only.
- 320px with no horizontal scroll; RTL mirrors; every Fiesta locale.
- Focus order equals visual order, except the two recorded exceptions: the avatar on the sidebar's phone index and on the topbar's `/menu` page is read after the nav rows.
- User-facing strings go through copy review by Luna (`codex exec -m gpt-6-luna`) before they land. Translations follow the approved English.
- Local runs are targeted (`go test -tags browser ./check -run TestX`). The full suites run on the amadan fleet (hub CI), one branch at a time.
- Commit subjects under 80 characters. Commit in the agent worktree identity (`claude`, `claude~paul@keymail.dev`).

## The new markup (the contract both parts share)

Part A defines it in Fiesta's examples; Part B's layouts emit it. `{…}` is app content.

**Account card** (topbar and console in the bar; sidebar in the rail foot as `[rst-shell-profile]`, which keeps its element and its `name`):

```html
<details rst-dropdown rst-shell-account name="rst-shell-menu">
  <summary><span rst-shell-avatar>{profile}</span><span rst-shell-account-label>Account</span>{chevron-down in rst-caret}</summary>
  <div rst-dropdown-menu>
    <div rst-shell-who>{profile}</div>
    <div rst-menu-row><span rst-menu-row-label>Appearance</span>{scheme control}</div>
    <div rst-menu-row><span rst-menu-row-label>Language</span>{locale-menu}</div>
    {account links}
  </div>
</details>
```

Below 800px only the avatar shows in the summary. At 800px and up the topbar and console show the label and caret, as "Account ▾" does today.

**Scheme control:**

```html
<div rst-scheme="icons" role="group" aria-label="Appearance">
  <form method="post" action="/_theme"><input type="hidden" name="scheme" value="light"><input type="hidden" name="return" value="{return}"><button type="submit" aria-label="Light">{sun}</button></form>
  <form …value="dark"…><button type="submit" aria-label="Dark" aria-current="true">{moon}</button></form>
  <form …value="system"…><button type="submit" aria-label="System">{monitor}</button></form>
</div>
```

`fiesta.js` is unchanged. Its submit handler matches `[rst-scheme]`, moves `aria-current` and sets `m.open = false`. On a `div`, that last line is a no-op, so the card stays open after a switch.

**Up link:** `<a rst-shell-up href="{up}" rel="up">{chevron-left icon, aria-hidden}{label}</a>`. Its accessible name must contain the visible label and say it goes back (WCAG 2.5.3), translated. The builder picks the mechanism.

**Roots:**

| Shell | Root | Bar children, in DOM order | Then |
|---|---|---|---|
| topbar | `rst-shell-topbar="page\|index"` | up, `h1 rst-shell-title`, brand, `nav rst-shell-nav`, account card | `main rst-page`, `footer rst-shell-foot` |
| console | `rst-shell-console="page\|index"` | up, `h1 rst-shell-title`, brand, account card | `aside rst-shell-rail > nav`, `main rst-shell-main`, `footer rst-shell-foot` |
| sidebar | `rst-shell-sidebar="page\|index"` | `header rst-shell-bar` holding only up (hidden at 800px and up) | `aside rst-shell-rail` (title, brand, nav, rail foot with the card), `main rst-shell-main` |

On the sidebar's phone row, the brand (or the title on the index) and the rail foot's avatar are placed into the bar's row by layout, never copied. No new-style root contains `rst-shell-menu`, `rst-shell-tail`, `rst-shell-back` or `rst-shell-chrome`.

## Review Focus

1. **A long brand at 320px**, with "‹ Menu" and the avatar. The name hides; the up link and avatar stay whole and stay in the row (Tasks A2–A4).
2. **An old layout on the new CSS:** a bare `rst-shell-topbar`, a console with `rst-shell-menu` and `rst-shell-back`, or a pre-index sidebar. Each must still open its Menu card or drawer on a phone (Task A5).
3. **Signed out:** the placeholder avatar is still a named, tappable button that opens a card holding the scheme and language controls. The sidebar's rule that hides an empty foot must not hide a card that now always has the scheme control (Task A2).
4. **Scripts off:** the up link, the index rows, the account card (`<details>`) and the scheme forms all work, and a scheme form posts to `/_theme` (Tasks A1, A4).
5. **RTL at 390px:** the chevron mirrors, the up link sits at the inline start and the avatar at the inline end (Tasks A2–A4).

---

## Part A: Fiesta 1.4.0

Work in a Fiesta worktree on branch `phone-shells`, made from `origin/main`. The local `main` is behind, so never base on it. Add new checks to `check/`. If `ci_test.go` asks for it, add a heavy test to `hack/test-weights.txt`.

### Task A1: The icon scheme control and the card's rows

**Files:**
- Create: `examples/scheme-menu/icons-dark-current.html`, `examples/scheme-menu/icons-in-a-card.html`
- Modify: `examples/examples.json` (states on `scheme-menu`; default becomes `icons-dark-current`; the old states stay as the dropdown's), `css/tokens.css` (`[rst-scheme="icons"]`, `[rst-menu-row]`, `[rst-menu-row-label]`), `i18n/*.json` (gallery copy for the new states), `examples/prose.json`
- Test: `check/scheme_icons_browser_test.go`

**Interfaces:**
- Produces: the scheme control and `rst-menu-row` markup above. The icons are Lucide `sun`, `moon` and `monitor` from `icons/`.

- [ ] **Step 1: Write the failing tests** in `check/scheme_icons_browser_test.go`:
  - `TestTheIconSchemeControlSitsInOneRow`: at 390px and 1280px, the three buttons share a top edge, each is at least 24×24px (WCAG 2.5.8), and the current one is visibly distinct. Read its computed background and border against its siblings; they must differ.
  - `TestTheIconSchemeControlSwitchesInPlace`: inside `icons-in-a-card` with the card open, clicking Light sets `data-theme="light"` on `<html>`, moves `aria-current` to Light, fires `rst-scheme` with `detail.scheme == "light"`, and leaves the card `open`.
  - `TestTheIconSchemeControlPostsWithoutScripts`: with JavaScript disabled, clicking Dark submits a POST to `/_theme` with `scheme=dark`. Use a test handler that records the request.
  - `TestEachSchemeButtonIsNamed`: each button's accessible name is Light, Dark or System, and the group's is Appearance (from the accessibility tree, as `axe_browser_test.go` reads it).
- [ ] **Step 2: Run them and confirm they fail.** Run `go test -tags browser ./check -run 'TestTheIconScheme|TestEachSchemeButton'`. Expected: FAIL, missing example.
- [ ] **Step 3: Write the two examples and the CSS.** The control is a segmented group: joined buttons, a shared border, and the current one raised like a `seg-tabs` current item, using existing tokens only. `[rst-menu-row]` is a flex row with the label at the start and the control at the end. The label is muted and 12.5px.
- [ ] **Step 4: Run the tests** (PASS), then `go test ./... -run 'Baseline|Coverage|Selectors|Spelling|Strings'`. Expected: PASS. New selectors are new markup, so `baseline_test.go` needs no exception.
- [ ] **Step 5: Copy review.** Send "Appearance" and the new state labels to Luna, then apply the approved English and translate it.
- [ ] **Step 6: Commit** with the subject `Scheme control: three icons in one row`.

### Task A2: The console on one row

**Files:**
- Modify: `examples/layout-console/default.html`, `examples/layout-console-page/default.html` (new markup per the table; the scheme and locale move into the card), `css/tokens.css` (rules under `[rst-shell-console]:has([rst-shell-up])`), `check/index_browser_test.go` (`TestThePhoneHeaderStripIs56Pixels` console legs), `check/console_shell_browser_test.go`, `check/profile_browser_test.go`
- Create: `check/testdata/fixtures/legacy/console-1.3.html` and `console-1.3-page.html` (byte copies of today's two console examples, used by A5)
- Test: `check/one_row_browser_test.go`

**Interfaces:**
- Consumes: A1's card rows and scheme control.
- Produces: `oneRow(t, ctx, root string) oneRowReading`, a helper in `check/one_row_browser_test.go` that returns the bar's height, the count of visible header rows above `main`, the up link's, name's and avatar's boxes, and whether any visible text in the chrome equals the localized "Menu" without being the up link. A3, A4 and A6 reuse it.

- [ ] **Step 1: Write the failing tests** in `check/one_row_browser_test.go`, table-driven over `{console, console-page}`, LTR and RTL, at 390px and 320px:
  - `TestThePhoneShowsOneHeaderRow`: exactly one visible header row above `main`, 56px tall.
  - `TestTheUpLinkNameAndAvatarShareTheRow`: on a page, the up link is at the inline start, the avatar at the inline end, and the name's centre within 1px of the row's centre when it fits. On the index there is no up link and the name is the `h1`.
  - `TestALongNameHidesBeforeAnythingElseMoves`: brand "Party planner for the whole extended family" at 320px. The name is hidden (`display: none` or zero width, not clipped mid-word), the up link and avatar keep their full boxes, and nothing scrolls sideways.
  - `TestNoMenuLabelOnAPhone`: `oneRow(...).MenuText == false`.
  - `TestTheSignedOutAvatarOpensACard`: with the placeholder profile, the summary is visible and named "Account", and tapping it opens a card holding `[rst-scheme="icons"]`.
- [ ] **Step 2: Run them and confirm they fail.** Run `go test -tags browser ./check -run 'TestThePhoneShows|TestTheUpLink|TestALongName|TestNoMenuLabel|TestTheSignedOut'`. Expected: FAIL against the old examples.
- [ ] **Step 3: Copy today's two console examples** into `check/testdata/fixtures/legacy/` before editing them.
- [ ] **Step 4: Rewrite both examples and add the CSS.** The bar is one grid row: `[start] up | name | avatar [end]`. Name hiding is the builder's choice of mechanism; it must meet the test. At 800px and up the bar matches 1.3's look: brand at the start, with the locale and the "Account ▾" summary at the end, the locale now inside the card.
- [ ] **Step 5: Update the console legs of `TestThePhoneHeaderStripIs56Pixels`** to the new row (`[rst-shell-bar]` with `[rst-shell-up]` and `[rst-shell-account] > summary` as targets). Update the console and profile tests that read `[rst-shell-menu]` to read the account card.
- [ ] **Step 6: Run the new tests and the touched files' tests.** Expected: PASS.
- [ ] **Step 7: Commit** with the subject `Console: one header row on a phone`.

### Task A3: The sidebar on one row

**Files:**
- Modify: `examples/layout-sidebar/default.html`, `examples/layout-sidebar-page/default.html`, `examples/shell-sidebar/default.html`, `css/tokens.css` (under `[rst-shell-sidebar]:has([rst-shell-up])`), the sidebar legs of `TestThePhoneHeaderStripIs56Pixels`
- Create: `check/testdata/fixtures/legacy/sidebar-1.3.html` and `sidebar-1.3-page.html` (byte copies first)
- Test: extend `check/one_row_browser_test.go` tables with `{sidebar, sidebar-page}`

**Interfaces:**
- Consumes: `oneRow`, and A1's card.

- [ ] **Step 1: Add sidebar rows to A2's five tests.** Also add `TestTheSidebarAvatarIsOneElement`: exactly one `[rst-shell-profile]` in the DOM, and on a phone content page its summary's box sits in the header row.
- [ ] **Step 2: Run them and confirm the sidebar rows fail.**
- [ ] **Step 3: Copy the legacy fixtures, rewrite the examples, and add the CSS.** On a phone the shell root is a grid whose first row holds the bar's up link, the brand (page) or `rst-shell-title` (index), and `[rst-shell-rail-foot]`. The rail itself is `display: contents` on a content page, with its nav hidden. On the index, the nav rows follow the row. At 800px and up the bar is hidden and the rail and dropup are unchanged.
- [ ] **Step 4: Run the one-row and sidebar tests**, plus `go test -tags browser ./check -run 'Index|Profile|ShellJS|Prerender'`. Expected: PASS.
- [ ] **Step 5: Commit** with the subject `Sidebar: one header row on a phone`.

### Task A4: The topbar's phone hierarchy

**Files:**
- Modify: `examples/layout-topbar/default.html` (becomes a content page: `rst-shell-topbar="page"`, up to `/menu#nav-home`, label "Menu"), `examples/examples.json` (adds `layout-topbar-index`, with sample URL `/menu`), `css/tokens.css` (under `[rst-shell-topbar]:has([rst-shell-up])`), `css/shell.css` (the slide applies to topbar), `js/shell.js` (recognise `[rst-shell-topbar]` as a root with an index), `README.md` (shell.js line)
- Create: `examples/layout-topbar-index/default.html` (`rst-shell-topbar="index"`, nav rows with ids `nav-home`, `nav-archive`, `nav-about`), `check/testdata/fixtures/legacy/topbar-1.3.html` (byte copy first)
- Test: extend `check/one_row_browser_test.go` with `{topbar, topbar-index}`, and add `check/topbar_index_browser_test.go`

**Interfaces:**
- Consumes: `oneRow`, A1's card.
- Produces: `shell.js` treats a topbar root exactly as it treats sidebar and console: slide type, Back via history, focus on return to `#nav-x`. Part B's `Serve` prerender list relies on this.

- [ ] **Step 1: Write the failing tests:**
  - Add the topbar rows to A2's five tests.
  - `TestTheTopbarIndexIsTheNavAsRows`: on `/menu` at 390px, every `[rst-shell-nav] a` is a full-width row at least 44px tall with a trailing chevron, `main` is hidden, and no up link shows.
  - `TestTheTopbarPageHidesItsNav`: on a page at 390px, no `[rst-shell-nav]` link is visible and `main` is.
  - `TestTheTopbarComesBackToItsRow`, with and without scripts: tap the up link on the page and land on `/menu#nav-home`, with focus (scripts) or the target (no scripts) on `#nav-home`. Mirror the sidebar's case in `shelljs_browser_test.go`.
  - `TestTheTopbarDesktopIsUnchanged`: at 1280px the new topbar page's bar has the brand at the start, the nav links inline and the account summary at the end, and the up link is hidden. On `/menu` at 1280px the nav links also show in `main`'s place as a plain list, with no empty page.
- [ ] **Step 2: Run them and confirm they fail.**
- [ ] **Step 3: Copy the legacy fixture, then write both examples, the CSS, and the `shell.js` and `shell.css` changes.** Old topbar markup (bare `rst-shell-topbar`, no `[rst-shell-up]`) keeps every 1.3 rule. `TestOneOhScriptsAreUnchanged` must stay green. If it pins `shell.js` bytes, the topbar recognition has to arrive by extending a selector list that test allows. Read the test first, and record in the commit body how it was satisfied.
- [ ] **Step 4: Run the tests**, plus `go test -tags browser ./check -run 'Shell|Index|OneOh'`. Expected: PASS.
- [ ] **Step 5: Commit** with the subject `Topbar: an index page and one header row on a phone`.

### Task A5: Old markup still works

**Files:**
- Test: `check/legacy_shells_browser_test.go`

**Interfaces:**
- Consumes: the legacy fixtures from A2 to A4, and `testdata/fixtures/sizing.html` (the pre-index drawer).

- [ ] **Step 1: Write `TestOldShellMarkupKeepsItsPhoneMenu`**, table-driven over the five legacy fixtures plus `sizing.html`, at 390px. The old Menu summary (or drawer) is visible, tapping it opens its card or rail, and the old back strip, where present, is its own row. These tests pass if the new rules are properly scoped.
- [ ] **Step 2: Run them.** Expected: PASS. If any fails, fix the scoping in `tokens.css`, never the fixture.
- [ ] **Step 3: Run the frozen gates:** `go test -tags browser ./check -run 'OneOh|RenderingException'` and `go test ./check -run Baseline`. Expected: PASS with no new exception.
- [ ] **Step 4: Commit** with the subject `Check that 1.3 shell markup keeps its phone menu`.

### Task A6: No published example teaches the old shape

**Files:**
- Modify: every remaining example that carries `rst-shell-back`, `rst-shell-menu`, `rst-shell-tail` or `<details rst-dropdown rst-scheme` (find them with `git grep -l -E 'rst-shell-(back|menu|tail)|rst-dropdown rst-scheme' examples/`), `examples/prose.json` and `i18n/*.json` blurbs (the "bar's menu opens as a card" text, every locale), `internal/gallery/page_tmpl.go`, `demo.go` and `frame.go` (the gallery's own chrome and demo app), `DESIGN.md` (the Navigation section, a new "One header row" rule, the scheme control under Components), `CHANGELOG.md` (Unreleased)
- Test: `check/no_old_shape_test.go` (plain), `check/no_old_shape_browser_test.go`

**Interfaces:**
- Consumes: `oneRow`.

- [ ] **Step 1: Write the failing tests:**
  - `TestNoExampleCarriesTheOldShellMarkup` (plain): no file under `examples/` matches `rst-shell-(back|menu|tail|chrome)` or `rst-dropdown rst-scheme`. Only `check/testdata/fixtures/legacy/` and `sizing.html` may.
  - `TestNoPublishedPageHasTwoHeaderRows` (browser): every `layout-*` example and the gallery's own pages (a sample of each page template the site builds), at 390px. `oneRow` reports at most one header row and `MenuText == false`.
- [ ] **Step 2: Run them and confirm they fail** on the remaining examples and the gallery chrome.
- [ ] **Step 3: Rewrite the examples, the gallery chrome and the blurbs.** Send every changed English string to Luna for copy review, apply the approved English, then translate.
- [ ] **Step 4: Rewrite `DESIGN.md`'s Navigation paragraph** to describe the index page, the one header row with the avatar at its end, and the account card's rows, and add the CHANGELOG entry.
- [ ] **Step 5: Run the tests**, plus `go test ./... -run 'Docs|Spelling|Strings|Coverage'`. Expected: PASS.
- [ ] **Step 6: Commit** with the subject `Examples and gallery show only the one-row phone header`.

### Task A7: Release Fiesta 1.4.0

- [ ] **Step 1: Push the branch and wait for hub CI to go green:** `amadan ci status rastrillo/fiesta -branch phone-shells`, exit 0.
- [ ] **Step 2: Show Paul the gallery's shells on a phone before releasing.** Run `make site` and serve it over Tailscale (`tailscale serve --https=443`). Link the Mobile view of `layout-topbar`, `layout-topbar-index`, `layout-console-page` and `layout-sidebar-page`, then wait for his go-ahead.
- [ ] **Step 3: Release.** Run `go run ./cmd/fiesta release -version 1.4.0 -candidate`, commit `releases/1.4.0/`, merge `origin/main` in if it moved, and get CI green again.
- [ ] **Step 4: Land, tag and ship** per the Fiesta recipe: `amadan branch merge rastrillo/fiesta phone-shells -squash -expect <sha> -message "<subject>\n\n<why>"`. Then annotate the tag `v1.4.0` on the merge, push it, run `make site-publish SITE_REF=v1.4.0`, and `carlos ship` / `carlos promote` to `canary/rehearsal`. Check the result live from the flagship.

---

## Part B: Rastrillo on Fiesta 1.4.0

Continue on the rastrillo branch `phone-shells`, merging `origin/main` in first. Every task ends with `GOFLAGS=-mod=mod` set for the local runs (see the memory on scratch modules).

### Task B1: Pin Fiesta 1.4.0

**Files:**
- Modify: `go.mod`, `go.sum`, the vendored copies `rastrillo doctor --fix` refreshes, every `testdata` module that replaces the root (tidy them after the bump)
- Test: the existing `vendored_test.go` and `ui` tests

- [ ] **Step 1:** Run `go get amadan.net/rastrillo/fiesta@v1.4.0 && go mod tidy`.
- [ ] **Step 2:** Run `go test ./ui/... -run 'Vendored|Tokens|Scripts'` and `go test ./cmd/rastrillo -run 'New|Doctor' -short`. Expected: PASS. Old layouts still render because Fiesta keeps the old rules.
- [ ] **Step 3: Commit** with the subject `Pin Fiesta 1.4.0`.

### Task B2: The partial and the three layouts

**Files:**
- Modify: `ui/partials/scheme-menu.html` (writes the icon control in a `rst-menu-row`, with no dropdown option), `ui/layouts/topbar.html`, `ui/layouts/console.html`, `ui/layouts/sidebar.html`, `ui/layouts/column.html` (footer gets the icon control; its row label is visually hidden there), `locales/*.toml` (adds `rastrillo.ui.shell_appearance`, plus `rastrillo.ui.shell_language` if the row label needs it, in all twelve locales), the `ui` tests that read `[rst-shell-menu]`, `[rst-shell-back]` or the scheme dropdown
- Test: `ui/phone_shells_browser_test.go`

**Interfaces:**
- Consumes: the markup contract above.
- Produces: new blocks in topbar, console and sidebar: `upLabel` (defaults to `T "rastrillo.ui.shell_menu"` in topbar and `T "rastrillo.ui.shell_up_label"` in the other two). `profile`, `view` and `up` also arrive in topbar, whose `up` defaults to `{{path $ "/menu"}}`. The block list in `docs/site/templates.md` changes to match.

- [ ] **Step 1: Write the failing tests** in `ui/phone_shells_browser_test.go`. Render each layout through `ui.Pages` with a page that defines `view`, `up` and `upLabel`, then check the rendered DOM: one `[rst-shell-up]`, no `rst-shell-(menu|back|tail)`, `[rst-scheme="icons"]` inside `[rst-shell-account]` or `[rst-shell-profile]`, and `TestThePhoneShowsOneHeaderRow` (the Fiesta assertion, ported) at 390px. Then `TestAnOldLayoutGetsTheIconControl`: a copy of today's `ui/layouts/topbar.html` saved as `ui/testdata/layouts/topbar-0.37.html` renders `[rst-scheme="icons"]` where its dropdown was.
- [ ] **Step 2: Run them and confirm they fail.** Run `go test -tags browser ./ui -run 'PhoneShells|OldLayout'`.
- [ ] **Step 3: Rewrite the partial and the layouts.** Keep each layout's long comments true. Rewrite the ones that describe the Menu card or the back strip to say why the row is one row.
- [ ] **Step 4: Copy review** of `shell_appearance` and any other new string via Luna. Then translate them into the twelve locales and run `rastrillo generate --check`.
- [ ] **Step 5: Run the tests**, plus `go test ./ui -run 'Layout|Shell|Scheme|Locale'`. Expected: PASS.
- [ ] **Step 6: Commit** with the subject `Layouts: one phone header row and the icon scheme control`.

### Task B3: Scaffold the topbar's Menu page; prerender topbar nav

**Files:**
- Modify: `cmd/rastrillo/new.go` (topbar joins the shells with a second page, in a different shape: `templates/menu.html` defines `view` index, `brand` and `nav` with `id="nav-home"`, and `templates/index.html` stays the home page with `up` `{{path $ "/menu#nav-home"}}` and the same nav. Add `menuPath = "\nconst menuPage action.Path = \"/menu\""`, the route `guarded.Get(menuPage, app.menu)`, the action `func (app *App) menu(visit *action.Visit)` rendering `"menu"`, and `"menu"` in the page list), the speculation rules in `Serve` (cover topbar layouts), and the comment on `twoPageShells`
- Test: `cmd/rastrillo/new_test.go` and the scaffold browser test that walks sidebar and console

- [ ] **Step 1: Write the failing tests.** `TestNewTopbarWritesAMenuPage` checks that the files exist, `make ci` passes in the scaffold (as the existing shell scaffold tests do), `GET /menu` answers 200, and its `[rst-shell-topbar="index"]` holds `#nav-home`. Extend the scaffold phone walk to the topbar: open on `/`, tap "‹ Menu", reach `/menu#nav-home`, tap Home, and return.
- [ ] **Step 2: Run them and confirm they fail.** Run `go test ./cmd/rastrillo -run 'TestNewTopbar|Scaffold.*Phone'`.
- [ ] **Step 3: Implement.** Copy review the Menu page's scaffold copy, if it has any.
- [ ] **Step 4: Run them and confirm they pass,** then run `make perf` in a topbar scaffold. Each page stays within 16 kB of framework CSS and JS now that it loads `shell.js` and `shell.css`.
- [ ] **Step 5: Commit** with the subject `Scaffold a Menu page for the topbar shell`.

### Task B4: Doctor names an old shell layout

**Files:**
- Modify: the `rastrillo doctor` layout check (find it with `git grep -n 'profile' cmd/rastrillo/doctor*.go`, next to the account-to-profile hint)
- Test: its `_test.go`

- [ ] **Step 1: Write the failing test** `TestDoctorNamesAnOldPhoneMenu`. A `templates/layout.html` containing `rst-shell-menu` or `rst-shell-back` gets one line naming the file and pointing to "Upgrading to one header row" in `docs/site/templates.md`. A new layout gets none.
- [ ] **Step 2: Implement it, run the test (PASS), and commit** with the subject `Doctor: name a layout with the old phone menu`.

### Task B5: Examples, design-system pages and test pages

**Files:**
- Modify: `examples/blog` (new topbar layout, `/menu` page, route and test), `examples/notes`, `examples/tickets` and `examples/helloworld` (whichever use a shell layout), `internal/designsystem` (shell samples), `ui/testdata/pages/**` (anything with the old markup outside a named legacy fixture)
- Test: `TestNoExampleCarriesTheOldShellMarkup`, ported to Rastrillo as `examples_shape_test.go` at the repo root

- [ ] **Step 1: Write the ported test** over `examples/`, `internal/designsystem/` and `ui/testdata/pages/`, allowing only `ui/testdata/layouts/*-0.37.html`. Confirm it fails.
- [ ] **Step 2: Move each example over.** Each example app's own `make ci` passes (run per example, targeted).
- [ ] **Step 3: Run the ported test** (PASS), then **commit** with the subject `Examples move to the one-row phone shells`.

### Task B6: Docs, skill and skilleval

**Files:**
- Modify: `docs/site/templates.md` (the Shells table, the block list, "On a phone" rewritten, a new "Upgrading to one header row" section after "Upgrading to the phone index", and the old topbar upgrade note marked historical), `docs/site/reference/ui.md` (scheme-menu), `docs/site/cli.md` (topbar writes two pages), `skill/design.md` ("Shells and colour scheme" with the shell-choice rule from the spec, section 6), `skill/screens.md` (step 4), `SKILL.md` (the index row "choose a shell, …" and the Never list: "Never add a Menu button, hamburger or drawer"), `skill/partials.md` (scheme-menu keys)
- Create: `hack/skilleval/tasks/sections.md` and `hack/skilleval/tasks/sections.check.sh`
- Test: `skillmd_test.go` and `skill_partials_test.go` (existing), plus the new skilleval check

- [ ] **Step 1: Write the skilleval task.** The brief: "a small club app with Events, Members and Notices, used mostly on phones". The check uses `lib.sh`'s `present`/`refuse`. It refuses `rst-shell-menu`, `rst-shell-back` and any `hamburger`/`menu` icon in `templates/`. It requires `define "view"}}index` and `[rst-shell-(sidebar|console|topbar)]`. It refuses a hand-rolled scheme toggle, as `scheme.check.sh` does.
- [ ] **Step 2: Measure the old skill.** Run skilleval against the new task with the current skill, using the baseline at `~/.cache/skilleval/baseline2` (see the memory on measuring skill changes), and record the score.
- [ ] **Step 3: Edit the docs and skill.** Keep `SKILL.md` within its existing budget test. Run Luna on any user-facing docs prose (the docs site) and apply its edits. The skill files are for agents, so no copy review.
- [ ] **Step 4: Run skilleval again** and record before and after in the commit body. Expected: the new task passes more often than in Step 2, with no other task worse.
- [ ] **Step 5:** Run `go test . -run 'SkillMD|SkillPartials'` (PASS), then **commit** with the subject `Skill and docs: pick a shell by shape; one phone header row`.

### Task B7: Land

- [ ] **Step 1:** Merge `origin/main` in, push, and wait for green hub CI (`amadan ci status rastrillo/rastrillo -branch phone-shells`).
- [ ] **Step 2:** Send the branch for review by Astra. Use `codex exec -m gpt-6-astra` on the full diff against `origin/main`, with the spec and this plan, then fix what it confirms.
- [ ] **Step 3:** Show Paul a topbar scaffold and `examples/blog` on a phone over Tailscale, then wait for his go-ahead.
- [ ] **Step 4:** Land it: `amadan branch merge rastrillo/rastrillo phone-shells -squash -expect <sha> -message …`, then `make mirror`. The Rastrillo release and site sync are a separate step (the release recipe), not part of this plan.
