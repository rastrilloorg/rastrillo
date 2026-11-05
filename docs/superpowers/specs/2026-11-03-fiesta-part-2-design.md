# Rastrillo takes its look from Fiesta (Fiesta part 2)

Status: design, 2026-11-03. Branch `fiesta-part-2` (rastrillo) and a
Fiesta 1.2.0 release branch (fiesta). Builds on
`docs/superpowers/specs/2026-10-27-fiesta-design.md` (the Fiesta design,
on branch `fiesta-design`), whose "Decided" section still binds, and on
the 1.1 design's "Hooks left for part 2"
(`docs/superpowers/specs/2026-10-29-fiesta-1-1-design.md`, same branch).

## Why

Fiesta (fiesta.rastrillo.org, v1.1.0) is Rastrillo's design system as a
project of its own, with its own site and design-system gallery, live.
Rastrillo still owns a second copy of the same CSS and JS in `ui/`, and
the two have drifted. The original design decided that Rastrillo stops
owning the CSS and client JS: it takes a fixed Fiesta version and keeps
only what makes the patterns work on a server. This is that switch.

Paul (2026-11-03): part 2 is "mostly the Go stuff". Fiesta's gallery is
done and live.

## Goals

- **One source.** Rastrillo's stylesheets, themes, client scripts and
  icons come from a pinned Fiesta release. Nobody edits a copy in
  Rastrillo; a change to the look is a Fiesta release and a pin bump.
- **No visible change.** A Rastrillo app looks and behaves the same
  after the switch, apart from the column shell's menus moving into its
  footer row (below), which is the one intended change, and the fixes
  Fiesta made to its select and date scripts after the seed (a disabled
  select or fieldset locks the box, a form reset repaints it, a
  read-only date refuses edits, a reset no longer races a blur commit).
  Those are fixes, taken on purpose and listed in the changelog.
- **Everything around the files keeps working:** `rastrillo new`,
  `rastrillo doctor`, the vendored pin tests, `rastrillo markup`, the
  per-page slicing (#158: `tokens.css` sliced per page, `theme.css`
  whole), `pageScripts`, and the first-load budget.
- **The Row Rule reaches Rastrillo.** No dropdown on a line of its own:
  the column shell's language and scheme menus move to a footer row, as
  Fiesta 1.1 does, and SKILL.md says so.

## Non-goals

- **Shrinking rastrillo.org's gallery** to a Go-call reference linking
  to Fiesta. It comes next, on its own branch, after this lands: the
  current gallery's browser drives are part of how this branch proves no
  visible change.
- Any redesign. Fiesta 1.2.0's one visible change is a fix Rastrillo
  already ships: the console rail's current item draws its ring whole.
- Porting Fiesta's tests to chromedp v0.21. Fiesta does not import
  Rastrillo (its `no-rastrillo` gate forbids it) and pins its own
  chromedp, so Rastrillo's upgrade never reaches it.
- Moving server behaviour into Fiesta: live pages (`live.js`), polled
  fragments, sign-in, the `/_theme` and `/_locale` routes and the
  catalogs stay in Rastrillo.

## Anti-goals

- A Rastrillo app that looks or behaves differently, beyond the column
  menus.
- Two copies again: any CSS or browser JS left in Rastrillo's `ui/`
  that Fiesta also ships.
- Existing apps forced to edit anything on upgrade. Their vendored files
  are theirs; they keep working as they are.

## Where things stand (measured 2026-11-03)

Rastrillo `ui/` at main ab909999 against Fiesta main (1.1.0):

| file | differing lines | why |
|---|---|---|
| shell.css, themes, light.js, anchor.js | 0 | |
| tokens.css | ~220 | Fiesta 1.1's additions (footer row, snippet, side nav, button seg-tabs, stats pairs, inline status) and Rastrillo's fixes since the seed (the console rail's ring) |
| shell.js | ~40 | Rastrillo's skipped-view-transition handling |
| busy.js | ~20 | Rastrillo's `data-live-form` handling for live pages |
| select.js, datetime.js, calendar.js | ~90 / ~240 / ~10 | Fiesta reads its words from the attribute, then its own string table (`i18n/<lang>.js`), then English; the calendar is `window.fiestaCalendar` (was `rastrilloCalendar`) |
| transition.js | Rastrillo only | new on Rastrillo main (skipped transitions) |
| rastrillo.js | Rastrillo only | the fragment shim (`data-poll`) plus menu dismiss, switch drag, touch press and the scheme menu's in-place apply, which Fiesta's `fiesta.js` already holds |
| live.js | Rastrillo only | live pages: stays |

Rastrillo's partials emit the select and date fields' words as
attributes from its own catalogs. Fiesta's scripts read the attribute
first, so that keeps working with no string table loaded.

## The work

### 1. Fiesta 1.2.0: catch up with Rastrillo

Before Rastrillo can switch, Fiesta takes every change Rastrillo made to
these files after the seed (f892f8be), so the switch-over changes no
bytes a Rastrillo app sees except the column menus:

- tokens.css: the console rail's ring and any other post-seed fix;
- shell.js: skipped view transitions;
- busy.js: `data-live-form` (an attribute Fiesta documents as "set by a
  server that swaps forms"; harmless without one);
- transition.js: joins Fiesta as an optional script, with its example
  and gallery mention;
- anything else the builder finds in the diff that is Rastrillo-side
  newer.

For each differing hunk the builder decides which side is newer
(Fiesta 1.1's changes stay; Rastrillo's later changes move to Fiesta)
and records it. Fiesta's own gates (examples, pixel and contrast
checks, the 25 KiB stripped build) hold the release.

**The 1.x gates (my call).** Fiesta's gates are stricter than its `/1/`
promise: `TestOneOhScriptsAreUnchanged` rejects any change to a 1.0
script (shell.js, busy.js), and `TestEveryOneOhDeclarationStillApplies`
and `TestEveryNewRuleRequiresNewMarkup` reject the ring fix's changed
declarations on 1.0 markup. The promise itself allows refinements
announced in the changelog, and these are fixes. So each gets a named,
scoped exception in its gate, in the style of the existing
`widenedSelectors` and `newOnOldMarkup` lists, each with its reason, and
the changelog announces them. Pixel baselines for the console shell move
with the ring fix, recorded the same way.

**The partial-to-example mapping (my call).** Fiesta 1.2.0 exposes the
state-to-partial mapping that `hack/seed/sources.json` holds today
through its Go API, so Rastrillo's golden tests (section 4) read it from
the pinned release instead of keeping a copy. Released with
Fiesta's recipe (`fiesta release -version 1.2.0`, land, tag, ship); `/1/`
moves to 1.2.0.

### 2. Rastrillo requires Fiesta 1.2.0

- `go.mod` requires `amadan.net/rastrillo/fiesta v1.2.0`. Fiesta's Go
  API (`fiesta.TokensCSS`, `ShellCSS`, `ThemeCSS`, `Script`,
  `ScriptNames`, `Strings`, `IconSlugs`, `FS`) is the source of every
  byte Rastrillo vendored before.
- The copies in `ui/` (tokens.css, shell.css, themes/, and the scripts
  Fiesta ships) are deleted. `ui`'s embeds and `ui.VendoredAssets`,
  `internal/vendored`'s list, the slicer's created-hook lists (#158),
  `rastrillo new`, `doctor` and the pin tests read Fiesta's bytes.
- Rastrillo keeps, as its own: `live.js`, and a smaller `rastrillo.js`
  holding only the fragment shim (polling and what serves it). Menu
  dismiss, switch drag, touch press and the scheme menu's apply come
  from Fiesta's `fiesta.js`, which joins the vendored set. **It loads
  through `pageScripts`, on every page that has the file (my call)**:
  `pageScripts` already skips a script the app does not have, so an app
  with the old full `rastrillo.js` and no `fiesta.js` keeps working, and
  an app that refreshes its files with `doctor --fix` and has the
  `pageScripts` line gets `fiesta.js`. An app whose layout predates
  `pageScripts` and refreshes `rastrillo.js` loses menu dismiss, switch
  drag and the in-place scheme switch until it adds the layout line; the
  changelog says so in the upgrade steps, as it already asks for
  `transition.js`, and `doctor` names the line when it finds a slim
  `rastrillo.js` with a layout that loads no `fiesta.js`. The builder checks nothing a Rastrillo
  page relied on in the old `rastrillo.js` is lost (cookie-backed scheme
  switching in particular: `fiesta.js`'s `rst-scheme` event exists for
  a server to write its cookie).
- Icons (my call): the Lucide icons Rastrillo inlines today
  (`icons.go`) come from Fiesta's `icons/` through its Go API, held equal
  by a test; the other icon sets in `internal/iconsets` (Font Awesome,
  the CDN and JS delivery modes) stay Rastrillo's.
- `rastrilloCalendar` becomes `fiestaCalendar` wherever Rastrillo named
  it.
- `copy.js` (Fiesta 1.1's copy button) joins the vendored scripts and
  `doctor`, loaded by `pageScripts` only where a page uses
  `rst-snippet`. The two string keys it needs (`copy-done`,
  `copy-failed`) join what partials can read, from
  `fiesta.Strings(lang)` or Rastrillo's catalog, whichever the builder
  shows keeps one source.
- Bumping the pin is one line in `go.mod` plus whatever the pin tests
  say changed; a test fails if a Fiesta file Rastrillo vendors is not in
  the release it pins.

### 3. The column menus move to a footer row

- `ui/layouts/column.html`: `{{block "locale"}}` and `{{block "scheme"}}`
  move from the top of `<main rst-page>` into
  `<footer rst-page-foot>`, after `{{template "content"}}`, as Fiesta's
  `examples/layout-column` does in 1.1. The same in the scaffold's
  column layout and every test page that copies it.
- The other shells keep their menus where they are (bars and rails are
  rows already).
- SKILL.md gains the Row Rule in one line, in Rastrillo's words: a
  dropdown sits in a row beside what it acts on (page header, section
  head, bar, toolbar, footer row), never on its own line or above a
  page title. Within SKILL.md's byte budget, or with a reasoned raise.
- Existing apps keep their app-owned column layout; the changelog shows
  the move as an optional edit.

### 4. Golden tests against Fiesta's examples

This branch compares partials only (my call); the layout examples'
known difference (Rastrillo's topbar, sidebar and console render a
scheme block that Fiesta's layout examples lack) is left for a later
pin bump, when Fiesta's layout examples catch up.

Rastrillo's partials are checked against the pinned Fiesta's examples,
as the Fiesta design decided ("the reference markup is Fiesta's"). The
1.1 design's table says how the five component states that now carry a
row are read (`Element` for four, the toolbar's `Contents` for the menu
idiom, the standalone scheme state against the partial with its actions
removed). A partial whose markup differs from its Fiesta example fails,
naming both.

### 5. Proof of no visible change

- Rastrillo's existing browser drives (gallery, shells, components) pass
  unchanged on the vendored-from-Fiesta files, except where the column
  menus moved, which are updated to expect the footer row.
- #158's sliced-vs-whole drive passes.
- A test holds Rastrillo's served bytes for each vendored file equal to
  the pinned Fiesta release's (after minification), so a stray local
  copy cannot creep back.
- The first-load budget passes; `fiesta.js` replaces most of
  `rastrillo.js`, so a page's weight should move by little. If a page
  goes over, the builder reports the number rather than raising it.

### 6. Upgrade path and docs

- Existing apps: no edit needed. Their vendored `static/` files are
  theirs; `doctor` reports them as the old version, as it does today
  after any framework upgrade, with the usual way to refresh.
- New apps: Fiesta's files, the footer-row column layout, `fiesta.js`.
- CHANGELOG (Unreleased): what changed, that the look comes from Fiesta
  1.2.0, the optional column-menu move, `fiestaCalendar`, and how to
  take the new files (`doctor`).
- Rastrillo's docs point to fiesta.rastrillo.org for the look; the
  gallery change itself is the next branch.
- Copy goes through Luna, as for #158.

## Order

1. Fiesta 1.2.0 (its own branch, CI, release, ship).
2. Rastrillo `fiesta-part-2`: require 1.2.0, delete the copies, split
   `rastrillo.js`, vendor `fiesta.js` and `copy.js`, the footer row,
   golden tests, docs.
3. rastrillo-website: sync `docs/site` and SKILL.md to the landed sha
   (its usual `Sync:` PR), so the corrected references are published.
4. Next branch: rastrillo.org's gallery shrinks to the Go-call
   reference.

## Decided after Astra's first review (2026-11-04)

Astra's round 1 (33 findings, 4 blockers) is in the branch discussion.
What it changed:

- **Fiesta's pixel gates keep their frozen 1.0 tree.** The parity and
  1.0 browser suites photograph frozen 1.0 CSS against current CSS, and
  `TestTheBaselineIsOneOhOh` forbids touching the frozen tree. The ring
  fix gets a named, scoped rendering exception in both suites (the
  console rail's current item), not a moved baseline.
- **The script exceptions carry their own regression tests.** Both
  sides of Fiesta's pixel comparisons load current scripts, which is
  why the 1.0 scripts are frozen. Each excepted script change
  (shell.js's skipped transitions, busy.js's `data-live-form`) comes
  with a Node or browser test of its own in Fiesta.
- **Fiesta's `Version` moves to 1.2.0 first**, with a `-candidate`
  build, rebuilt after later asset changes; the release command refuses
  otherwise.
- **The mapping API keeps `View` and the example path.**
  `fiesta.Sources()` returns `map[string]Source` keyed by the example
  path, with `Source` as `{Kind, Source, Locale, View string; State
  int}`, equal to `sources.json` key for key and field for field.
- **`transition.js` keeps its place:** synchronous, before the
  stylesheets, as today; only its bytes come from Fiesta. `pageScripts`
  is deferred and would miss a rejection before `pagereveal`.
- **`fiesta.js` is chosen for every page explicitly**, ahead of
  `plannedScripts`' file-presence filter. Without `PagesConfig.Assets`
  (and outside `ui.Pages`) `has` cannot see the app's files, so such an
  app's pages ask for `fiesta.js` whether or not it has it: a 404 for
  an app that never took it, which breaks nothing. The changelog says
  so.
- **`doctor` refreshes in pairs, and never makes a duplicate.**
  `rastrillo.js` and `fiesta.js` move together: if the app owns an
  edited `rastrillo.js`, `doctor --fix` leaves both and says why
  (Fiesta's own header calls the full shim plus `fiesta.js`
  unsupported). `calendar.js` and `datetime.js` move together for the
  same reason (`rastrilloCalendar` became `fiestaCalendar`). `doctor`
  decides its layout warning on the asset set the fix will leave, not
  the one it found, so the first `doctor --fix` on an old app warns.
- **`copy.js` loads on `rst-snippet`** through both of `scripts.go`'s
  detectors (the hook table and the attribute pattern).
- **Upstream script sizes are Fiesta's.** Rastrillo's raw-size gates on
  `select.js` and `datetime.js` (56,320 and 77,824 bytes) go; Fiesta's
  are 59,673 and 84,708. The first-load budget still weighs what a page
  sends, with references for `fiesta.js`, `copy.js` and the slim shim.
- **Node harnesses for Fiesta's scripts are Fiesta's.** Rastrillo's
  `select_node.mjs`, `datetime_node.mjs`, `shell_node.mjs` and
  `shell_transition_node.mjs` go (Fiesta has `check/node/`); what they
  proved about Rastrillo's integration stays as Go or browser tests.
- **The partial check sits beside the gallery's sample builders**
  (`internal/designsystem`), where the seed rendered states, keeping
  the seed's wrappers and picture rewriting; it enumerates its
  categories explicitly (partials, the menu idiom, the sign-in screen)
  and asserts it covered every partial with a Fiesta example.
- **The gallery serves what pages load.** `galleryAssets()` and its
  hashing gain `fiesta.js` and `copy.js`; `previewScripts` and the
  gallery page load `fiesta.js` for menus and switches; the gallery's
  OS-scheme isolation test targets Fiesta's listener.

## References this branch corrects

"Update all the references" (Paul, 2026-11-04). Historical CHANGELOG
entries stay as written; statements that vendored files become the
app's stay true.

- Ownership of the look: `DESIGN.md` (its title section, the
  source-of-truth line, and the shim's duties), `PRODUCT.md`,
  `skill/design.md`, `skill/screens.md`, `skill/scripts.md`, and
  SKILL.md's pointer to them. Fiesta (fiesta.rastrillo.org) owns the
  look; Rastrillo owns the partials, server routes and its gallery of
  Go calls.
- What `rastrillo.js` does: `docs/site/templates.md`,
  `docs/site/localization.md`, `docs/site/cli.md`,
  `docs/site/reference/ui.md`, `docs/site/jobs.md`, `docs/site/assets.md`,
  `ui/styleguide.go`, and the gallery's asset rows and sample labels
  (`internal/designsystem/page.go`, `samples.go`) with their
  translations in `prose.go`.
- The date script: how it finds its words (attribute, then Fiesta's
  table, then English) in `docs/site/templates.md` and `ui/ui.go`; the
  calendar seam `fiestaCalendar` in `docs/site/reference/ui.md` and
  the field partials' comments.
- Inventories of vendored files: `docs/site/assets.md`,
  `docs/site/cli.md`, `docs/site/templates.md`, `cmd/rastrillo/new.go`,
  the gallery's downloads and its weight test lists.
- Deleted paths in comments and tests: the `field-date`,
  `field-datetime`, `field-time`, `field-daterange` partials,
  `cssfloor_test.go`, `markupspelling_test.go`, generated
  `skill/partials.md`.
- The examples' committed `static/` files (blog, tickets), refreshed.

## Left to the builder

- How `ui` embeds or re-exports Fiesta's bytes (direct calls into the
  fiesta package, or an `fs.FS` view), and how `internal/vendored`'s
  list is derived from Fiesta's `ScriptNames` plus Rastrillo's own two
  scripts.
- The per-hunk record of which side was newer in step 1.
- The exact form of the 1.x gate exceptions and the mapping's Go API.
- Where the copy button's strings come from, within "one source".
