# Page weight, iteration 2: a page's stylesheet holds only what it can show — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each page links a slice of the app's own `tokens.css` holding only the rules for the `rst-*` hooks it can show, and loads `busy.js`, `live.js`, `light.js` and `anchor.js` only where used, so a page's framework CSS and JS stays under 15 kB gzipped with no visible change.

**Architecture:** `ui.Pages` already walks each page's templates at boot (`tmpl.Inspect`) to pick its component scripts. The same walk collects the page's hooks, adds the hooks its scripts create, and binds two per-page template functions: `pageStylesheet` (the slice's URL, from `rastrillo.Assets`) and `pageScripts` (the scripts it needs). `Assets` slices the minified stylesheet with a CSS reader in `internal/vendored`, caches each distinct slice by its bytes, and serves it immutable under the same fingerprinted-name scheme. `perftest.FirstLoad` drops to 15 kB for sliced pages and fails a page whose HTML shows a hook its slice has no rules for.

**Tech Stack:** Go 1.26, `html/template` via `tmpl`, `github.com/tdewolff/parse/v2/css` (already a direct dependency) for reading CSS, chromedp v0.21 through `harness` for the browser drive.

**Spec:** `docs/superpowers/specs/2026-11-02-page-weight-2-design.md`. Read it first; it holds the measurements and Paul's answers. Iteration 1's spec and plan (`docs/superpowers/specs/2026-10-31-page-weight-design.md`, `docs/superpowers/plans/2026-10-31-page-weight.md`) show the shapes this extends.

## Global Constraints

- Keep the look: every browser test passes unchanged against the served files; a sliced page's computed styles equal the whole file's.
- Fail whole, never empty: a stylesheet the slicer cannot read is served whole on every page.
- Budget: 15,000 bytes of framework CSS and JS, gzipped at the default level, for a sliced page, plus each component script it loads; a page still linking the whole `tokens.css` keeps 40,000.
- Source files unchanged: `tokens.css` stays one commented file in the repo and in each app's `static/`. No new file in an app, no build step.
- Existing apps keep working with no edit; two one-line edits (the `PagesConfig.Assets` field in `render.go`, the stylesheet and script lines in the app-owned layout) opt them into slices.
- `theme.css` is not sliced.
- Code reads like a sentence: spelled-out names (except `err`), 80 columns, comments say why. `make code` and `make budget` must pass; raise a budget in `.rastrillo/budgets.txt` only with its reason, as the file's other records do.
- Local runs are targeted (`go test ./pkg -run TestX`, `GOFLAGS=-mod=mod`); the full gate, browser shards included, runs on the fleet (`amadan ci status rastrillo/rastrillo -branch page-weight-2`).
- Each task's new tests are seen red before their change and green after; record the evidence in one discussion on the branch, as iteration 1 did (discussion 192).

## Review Focus

1. **An app's edited `tokens.css` that the slicer cannot read** (an unbalanced brace, an at-rule it does not know): every page gets the whole file and a working page, never an empty or truncated stylesheet. Test in Task 4.
2. **An app that deleted `light.js` or `anchor.js`**, as the docs allow: no page links the missing file (a 404 that FirstLoad would also fail on). Test in Task 5.
3. **An app serving a live directory (`os.DirFS`) that edits `tokens.css` while running**: the next render links a new slice URL; no year-cached URL ever serves the old rules. Test in Task 4.
4. **Older layouts**: a pre-iteration-1 layout (fixed tags, no `neededScripts`) and an iteration-1 layout (fixed tags for busy, live, light and anchor plus `neededScripts`) still load every script exactly once and the whole stylesheet. Test in Task 6.
5. **An app under a base path** (`/acme`): the slice URL goes through `path $` like every other asset and resolves. Test in Task 6.

---

## File Structure

| file | change | responsibility |
|---|---|---|
| `internal/vendored/slice.go` | create | `Slice` and `StyledHooks`: read a stylesheet's rules and keep those a hook set can show |
| `internal/vendored/hooks.go` | create | `CreatedHooks` per script, `LitHooks` |
| `internal/vendored/slice_test.go`, `hooks_test.go` | create | |
| `tmpl/tmpl.go`, `tmpl/fingerprint.go` | modify | `InspectNamed`: Inspect that also says which template a node is in |
| `ui/hooks.go` | create | a page's hooks from its templates; `needs` for hooks |
| `ui/scripts.go` | modify | `pageScripts` for every optional script, by the spec's table |
| `ui/pages.go` | modify | `PagesConfig.Assets`; bind `pageStylesheet` and `pageScripts`; whole-app hooks for live pages |
| `assets_slice.go` | create | `Assets.Stylesheet`: slice, cache and serve |
| `assets.go` | modify | `Handler` serves a slice by its hash; `Assets.Has` |
| `ui/layouts/*.html` | modify | link `pageStylesheet`, range `pageScripts` |
| `cmd/rastrillo/scaffold/render.go.tmpl` | modify | `Assets: assets` |
| `perf/perftest/firstload*.go` | modify | 15 kB for sliced pages; unsliced report; hook coverage |
| `internal/designsystem/sliced_browser_test.go` | create | computed styles equal on sliced and whole stylesheets |
| `SKILL.md`, `skill/screens.md`, `CHANGELOG.md`, `docs/site/templates.md` | modify | the `needs` line, upgrade steps |

---

### Task 1: The stylesheet slicer

**Files:**
- Create: `internal/vendored/slice.go`, `internal/vendored/slice_test.go`

**Interfaces:**
- Produces: `func Slice(stylesheet []byte, hooks map[string]bool) ([]byte, error)` and `func StyledHooks(stylesheet []byte) (map[string]bool, error)`; `var ErrUnreadable error`, which both wrap when they cannot read the stylesheet.

- [ ] **Step 1: Write the failing tests** in `slice_test.go`, as one table `TestSliceKeepsTheRulesAPageCanShow` (input CSS, hooks, want CSS, all minified) with these rows:
  - `[rst-btn]{a:b}` with `{rst-btn}` kept; with `{}` dropped.
  - `[rst-a],[rst-b]{a:b}` with `{rst-b}` kept whole (selectors are not rewritten).
  - `[rst-a] [rst-b]{a:b}` with `{rst-a}` dropped; with `{rst-a,rst-b}` kept.
  - `[rst-btn=primary]{a:b}` and `.rst-btn:hover{a:b}` with `{rst-btn}` kept (values do not count; classes do).
  - `:is([rst-a],[rst-b]) p{a:b}` and `:where([rst-a],[rst-b]) p{a:b}` with `{rst-a}` kept: inside `:is()`, `:where()` and `:has()`, one branch's hooks are enough.
  - `p:not([rst-a]){a:b}` with `{}` kept: a hook inside `:not()` is never needed, since the rule applies exactly where the hook is absent.
  - `[rst-a]:not([rst-b]){a:b}` with `{rst-a}` kept.
  - `:root{--x:1}`, `html{a:b}`, `@property --rst-press{syntax:"*"}`, `@keyframes rst-spin{to{a:b}}`, `@font-face{a:b}` with `{}` all kept.
  - `@media (min-width:40rem){[rst-a]{a:b}[rst-b]{c:d}}` with `{rst-a}` keeps the block with only the `rst-a` rule; with `{}` the block is gone.
  - `@supports (anchor-name:--a){...}` and `@layer x{...}` behave as `@media`.
  - `[rst-a]{content:"}"}` and `[rst-a]{content:"\\7d"}` with `{rst-a}` kept byte for byte (a brace in a string is text).
- [ ] Add `TestSliceRefusesWhatItCannotRead`: `[rst-a]{a:b` and `@unknown-thing x{[rst-a]{a:b}}` return an error wrapping `ErrUnreadable`; an at-rule block it does not list is an error, not a guess.
- [ ] Add `TestStyledHooksNamesEveryHookInASelector`: `[rst-a] .rst-b,[rst-c=x]{}` gives `{rst-a,rst-b,rst-c}`.
- [ ] Add `TestSliceOfTheShippedTokensCSS`: slice `Minify("tokens.css", ui.TokensCSS())` with no hooks; it succeeds, is smaller than 2,000 bytes gzipped, and `StyledHooks` of the whole file is non-empty (guards against a reader that silently reads nothing). Read the file through the ui embed the way `internal/designsystem` does; if importing `ui` from `vendored` would cycle, put this one test in `ui`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./internal/vendored -run 'Slice|StyledHooks'` — FAIL (undefined).
- [ ] **Step 3: Implement** `Slice` and `StyledHooks` with `github.com/tdewolff/parse/v2/css`'s parser (grammar-level: qualified rules, at-rules with blocks, declarations), writing kept rules back from the parser's tokens. A selector's needed hooks are the `rst-[a-z0-9-]+` attribute and class names outside `:not()`, with each `:is()`, `:where()` or `:has()` argument list needing any one of its branches; a rule is kept when any top-level comma-separated selector's needs are met by `hooks`; hookless rules and `@property`, `@keyframes`, `@font-face`, `@counter-style`, `@page` are kept; `@media`, `@supports`, `@layer`, `@container`, `@scope` keep their kept rules and vanish when empty; any other at-rule with a block is `ErrUnreadable`.
- [ ] **Step 4: Run** the same command — PASS.
- [ ] **Step 5: Commit** `vendored: slice a stylesheet to the rules a hook set can show`.

### Task 2: The hooks scripts create, and the hooks light.js lights

**Files:**
- Create: `internal/vendored/hooks.go`; `ui/script_hooks_test.go`

**Interfaces:**
- Produces: `func CreatedHooks(script string) []string` (nil for a script that creates none, or an unknown name) and `func LitHooks() []string` (`rst-btn`, `rst-box`, `rst-card`, `rst-stats`, `rst-list`).

- [ ] **Step 1: Write the failing test** `TestEachScriptCreatesExactlyItsListedHooks` in `ui` (ui holds the scripts): for each `.js` in `vendored.Names()`, read the source and collect the hooks it creates — `rst-*` names that are the first argument of `setAttribute`, `toggleAttribute`, `classList.add`/`toggle`, the second argument of a two-string element-building call (`el("div", "rst-combo")`), or an attribute in a markup string — and assert the set equals `vendored.CreatedHooks(name)`. The spec's lists are the starting values; where the read set differs, read the script and fix the list or the reader, then say which in the commit.
- [ ] Add `TestLightLightsExactlyLitHooks`: the hooks in `light.js`'s `BTN` and `CARD` selector constants equal `vendored.LitHooks()`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./ui -run 'CreatesExactly|LightLights'` — FAIL.
- [ ] **Step 3: Implement** `hooks.go` with the lists as package variables and copying accessors, like `names.go`.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `vendored: the hooks each script creates, held to the scripts`.

### Task 3: Inspect that names each node's template

**Files:**
- Modify: `tmpl/fingerprint.go` (`reachable`), `tmpl/tmpl.go` (beside `Inspect`)
- Test: `tmpl/inspect_test.go`

**Interfaces:**
- Produces: `func (template *Template) InspectNamed(visit func(name string, node parse.Node))`, with `Inspect`'s guarantees (reachable from template, before execution); `Inspect` becomes a wrapper over it.

- [ ] **Step 1: Write the failing test** `TestInspectNamedSaysWhichTemplateANodeIsIn`: a set where `page` calls `{{template "menu"}}` and `{{template "form"}}`, each holding a distinct text node; collect `(name, text)` pairs and assert each text arrives with its own template's name and an unreachable template's text never arrives.
- [ ] **Step 2: Run** `go test ./tmpl -run InspectNamed` — FAIL.
- [ ] **Step 3: Implement** by passing the tree's name to `visit` from `reachable`'s loop.
- [ ] **Step 4: Run** `go test ./tmpl` — PASS (the existing Inspect tests too).
- [ ] **Step 5: Commit** `tmpl: InspectNamed, so a walk knows which template it is in`.

### Task 4: Assets serves a page's slice

**Files:**
- Create: `assets_slice.go`, `assets_slice_test.go`
- Modify: `assets.go` (`Assets` gains a slice cache; `Handler` consults it; `Has`)

**Interfaces:**
- Consumes: `vendored.Slice`, `vendored.ErrUnreadable` (Task 1).
- Produces:
  - `func (assets *Assets) Stylesheet(name string, hooks []string) string` — the URL path of `name` (a vendored stylesheet, `"static/tokens.css"`) sliced to `hooks`, shaped like `Path`'s (`/static/tokens.<hash>.css`, the hash of the slice's bytes). It returns `Path(name)` (the whole file) when `name` is not a vendored stylesheet, is missing, or cannot be sliced, and logs the last once per file version through `slog.Default()`.
  - `func (assets *Assets) Has(name string) bool` — the file exists in the tree.

- [ ] **Step 1: Write the failing tests** in `assets_slice_test.go`, on an `fstest.MapFS` with a small `static/tokens.css` (`[rst-a]{color:red}[rst-b]{color:blue}`):
  - `TestStylesheetLinksASliceAndServesIt`: `Stylesheet("static/tokens.css", []string{"rst-a"})` differs from `Path("static/tokens.css")`; GETting it through `Handler()` answers 200, `Cache-Control: public, max-age=31536000, immutable`, a body with `rst-a` and without `rst-b`, gzip when accepted (with `Vary: Accept-Encoding`), and an ETag of its hash.
  - `TestStylesheetSharesASliceBetweenEqualHookSets`: the hook lists `{rst-a, rst-b}` and `{rst-b, rst-a}` give the same URL.
  - `TestAnUnreadableStylesheetIsLinkedWhole` (Review Focus 1): `static/tokens.css` = `[rst-a]{color:red` gives `Path(...)`, and that URL serves the whole file.
  - `TestAnEditedStylesheetGetsANewSliceURL` (Review Focus 3): on a `MapFS` whose file is replaced with a new `ModTime`, the next `Stylesheet` call returns a different URL; GETting the old URL answers `no-cache` with the current whole file, never the old slice as immutable.
  - `TestHasNamesOnlyFilesInTheTree`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test . -run 'Stylesheet|Has'` — FAIL.
- [ ] **Step 3: Implement.** Slice the minified bytes `lookup` already keeps; cache slices in a map keyed by (source hash, sorted hook list) and a second index by slice hash for `Handler`; serve through the same code as `serveVendored` (gzip at `BestCompression`, ETags per encoding). In `Handler`, a hashed name whose hash is a current slice of that file is served before falling back to `serveFile`. Drop a file's slices when its source hash changes. The cache must not grow without bound per request: hook sets come from pages, so it is bounded by the app's pages; note that in the comment.
- [ ] **Step 4: Run** — PASS; then `go test . -run 'Asset|Vendored'` for iteration 1's tests.
- [ ] **Step 5: Commit** `assets: serve a stylesheet sliced to a page's hooks`.

### Task 5: A page's hooks and scripts

**Files:**
- Create: `ui/hooks.go`, `ui/hooks_test.go`
- Modify: `ui/scripts.go` (`needs`, `neededScript`, the per-page script set), `ui/scripts_test.go`

**Interfaces:**
- Consumes: `tmpl.InspectNamed` (Task 3), `vendored.CreatedHooks`, `vendored.LitHooks` (Task 2).
- Produces (package-internal, used by Task 6):
  - `func pageHooks(name string, page *tmpl.Template, entry string) (hooks map[string]bool, err error)` — every `rst-*` name in the page's reachable text nodes and string arguments, plus each `{{needs "rst-…"}}`.
  - `type pagePlan struct { hooks []string; scripts []string; live bool }` and `func planPage(name string, page *tmpl.Template, entry string, has func(string) bool) (pagePlan, error)` — `scripts` are static paths in `vendored.Names()` order among `busy.js`, `live.js`, `light.js`, `anchor.js` and the component scripts; `hooks` is `pageHooks` plus `CreatedHooks` of `rastrillo.js` and of every script in `scripts`; `has` reports whether the app still has a file (Review Focus 2).
- `needs` accepts a hook (`"rst-…"`) as well as a component script or one of the four scripts; anything else keeps iteration 1's error, its message listing both kinds.

- [ ] **Step 1: Write the failing tests** in `hooks_test.go`, building pages with `renderShellPages`' setup (`ui/layouts_scripts_test.go`) or a smaller `NewPages` on a `MapFS`:
  - `TestPageHooksIncludeBranchesNotTaken`: `{{if .X}}<p rst-a>{{else}}<p class="rst-b">{{end}}` gives both.
  - `TestPageHooksFollowPartials`: a page calling `{{template "empty" .}}` has `rst-empty`.
  - `TestPageHooksIncludeNeeds`: `{{needs "rst-card"}}` adds `rst-card`; `{{needs "rst"}}` and `{{needs "card"}}` fail with the listing message.
  - Scripts, one test per row of the spec's table: `TestBusyLoadsOnlyWithTheAppsOwnForm` (the layout's language and scheme menus alone: no `busy.js`; a page `<form>`: `busy.js`; `{{needs "busy.js"}}`: `busy.js`), `TestLiveLoadsOnLivePages` (a region; `data-poll`), `TestLightLoadsWithTheHooksItLights` (each of `LitHooks()` loads it; a page with none does not), `TestAnchorLoadsWithAMenu`.
  - `TestADeletedScriptIsNeverLinked`: with `has` false for `light.js` and `anchor.js`, a page with buttons and menus lists neither.
  - `TestAPagesHooksIncludeWhatItsScriptsCreate`: a select page's hooks include `rst-combo`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./ui -run 'PageHooks|Busy|Live|Light|Anchor|Deleted|ScriptsCreate'` — FAIL.
- [ ] **Step 3: Implement.** Forms count when their `<form` text is in a template other than `locale-menu` and `scheme-menu` (`InspectNamed`). Live keeps iteration 1's rule (`hasLiveRegion` or `data-poll`); a live page's component scripts stay "all of them". Keep `pageScripts`' old behaviour available for `neededScripts` (component scripts only) so iteration-1 layouts do not load a script twice.
- [ ] **Step 4: Run** the same, then `go test ./ui -run 'Scripts|Needs|Funcs'` for iteration 1's tests — PASS.
- [ ] **Step 5: Commit** `ui: a page's hooks, and the scripts it needs by what it shows`.

### Task 6: Pages bind the stylesheet and scripts; the layouts use them

**Files:**
- Modify: `ui/pages.go` (`PagesConfig.Assets *rastrillo.Assets`; `addPage`; whole-app hooks after every page is added), `ui/scripts.go` (`scriptFuncs` defaults), `ui/funcs_test.go` (`documentedFuncs`), `ui/layouts/{column,topbar,sidebar,console,stage}.html`, `cmd/rastrillo/scaffold/render.go.tmpl`, `ui/layouts_scripts_test.go`
- Test: `ui/stylesheet_test.go`

**Interfaces:**
- Consumes: `planPage` (Task 5), `Assets.Stylesheet`, `Assets.Has` (Task 4).
- Produces: template functions `pageStylesheet() string` and `pageScripts() []string`, reserved like `neededScripts`. Bound per page by `ui.Pages`; the defaults in `ui.Funcs` (a render outside `ui.Pages`) return `"static/tokens.css"`'s whole path through the app's `asset` func when it has one, else `"/static/tokens.css"`, and every optional script. A page whose plan is live links the stylesheet sliced to the union of every page's hooks. With `PagesConfig.Assets` nil, `pageStylesheet` is the whole file and `has` is always true.
- The layouts: `<link rel="stylesheet" href="{{path $ pageStylesheet}}">` in place of the `tokens.css` line; the fixed `busy.js`, `live.js`, `light.js` and `anchor.js` tags become `{{range pageScripts}}<script defer src="{{path $ (asset .)}}"></script>{{end}}` (which also replaces the `neededScripts` range); `rastrillo.js`, `shell.js` and their comments stay. Keep `live.js` after `busy.js` (the order `pageScripts` returns).

- [ ] **Step 1: Write the failing tests** in `stylesheet_test.go`:
  - `TestEachShellLinksItsPagesSlice`: for every shell, the plain page's stylesheet URL differs from the whole file's and from the select page's; the live page links the union slice, which is the same URL on every live page.
  - `TestARenderOutsidePagesLinksTheWholeFile`.
  - `TestPagesWithoutAssetsLinkTheWholeFile`.
  - `TestTheSliceURLHonoursTheBasePath` (Review Focus 5): rendered under `/acme`, the link starts `/acme/static/tokens.`.
  - `TestOlderLayoutsLoadEachScriptOnce` (Review Focus 4): a pre-iteration-1 layout and an iteration-1 layout (copy each from git: `git show 732cd281:ui/layouts/column.html` and `9a72d55d:ui/layouts/column.html` into `testdata/`) each load every script exactly once and link the whole stylesheet.
  - Update `shellWants` in `layouts_scripts_test.go` for the new script sets.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./ui -run 'Slice|Outside|WithoutAssets|BasePath|OlderLayouts|Shell'` — FAIL.
- [ ] **Step 3: Implement**, and add `Assets: assets` to the scaffold's `ui.PagesConfig` in `render.go.tmpl`.
- [ ] **Step 4: Run** the same, then `go test ./ui ./cmd/rastrillo -run 'Layout|Scripts|Funcs|NewScaffolds|Template'` — PASS. A scaffolded app compiles its templates (`rastrillo generate`) with the new functions: `go test ./internal/scaffoldcheck -run TestNewAppsMeetTheCodeLimits` and one scaffold test that runs `generate --check`.
- [ ] **Step 5: Commit** `ui: each page links its slice and loads the scripts it needs`.

### Task 7: The budget and the hook check

**Files:**
- Modify: `perf/perftest/firstload.go`, `perf/perftest/firstload_report.go`
- Test: `perf/perftest/firstload_test.go`, `perf/perftest/firstload_framework_test.go`

**Interfaces:**
- Consumes: `vendored.StyledHooks` (Task 1); the served pages of Task 6.
- Produces: `frameworkBudget.shells` for sliced pages (column and topbar 15,000; sidebar, console and stage measured plus 10%, rounded up to the next 100, at most 15,000) and an `unsliced` limit of 40,000.

- [ ] **Step 1: Write the failing tests:**
  - `TestASlicedPageIsHeldToFifteenKilobytes`: a page linking a slice over 15,000 fails, naming the page.
  - `TestAnUnslicedPageKeepsTheOldLimitAndSaysHowToSlice`: a page linking the whole `tokens.css` (its bytes equal the bare `static/tokens.css` answer) is held to 40,000 and its report names `PagesConfig.Assets` and `pageStylesheet`.
  - `TestAHookWithoutRulesInItsSliceFails` (spec 3a): a page whose HTML carries `rst-card` while its slice has no `rst-card` rule (and the whole file has one) fails with the page, the hook and `{{needs "rst-card"}}`.
  - `TestAHookTheWholeFileDoesNotStyleIsFine`: `rst-made-up` in the HTML, absent from both, passes.
  - In `firstload_framework_test.go`, every shell's plain, select, date and live page as served through `Assets` passes the new limits.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./perf/perftest -run 'Fifteen|Unsliced|Hook|Framework'` — FAIL.
- [ ] **Step 3: Implement.** Tell sliced from whole by comparing the linked stylesheet's bytes with the bare `static/tokens.css` answer. The hook check reads the page's `rst-*` attribute and class names from its HTML (an HTML tokenizer, as iteration 1's link reader does), and fails for each hook that `StyledHooks(whole)` has and `StyledHooks(slice)` lacks. A failure names the largest files rather than "grew" for a slice.
- [ ] **Step 4: Run** — PASS; measure the three other shells and set their limits.
- [ ] **Step 5: Commit** `perftest: hold sliced pages to 15 kB and their hooks to their slice`.

### Task 8: No visible change, in a browser

**Files:**
- Create: `internal/designsystem/sliced_browser_test.go`

**Interfaces:**
- Consumes: `vendored.Slice`, `vendored.CreatedHooks`; the gallery's preview documents and `treeHandler` (as `minified_browser_test.go` uses them).

- [ ] **Step 1: Write the test** `TestEveryPreviewLooksTheSameOnItsSlice` (`-tags browser`): for every preview document the gallery renders, in each theme and both schemes, load it once with the whole `tokens.css` and once with a slice made from the hooks in its HTML plus `CreatedHooks` of the scripts it loads; after scripts settle, read `getComputedStyle` for every element and its `::before` and `::after`, every property, and require the two to be equal, naming the document, the element path and the first differing property. Add the same comparison for each shell's plain, select, date and live pages from Task 6.
- [ ] **Step 2: Run** it once against a slicer that drops `@media` blocks (a temporary edit, reverted) to see it fail; then `./hack/gotest.sh browser internal.designsystem -run Sliced` — PASS. Locally only if the machine is free; otherwise let the fleet's browser shard run it.
- [ ] **Step 3: Commit** `designsystem: every preview looks the same on its slice`.

### Task 9: What agents and upgraders read

**Files:**
- Modify: `SKILL.md`, `skillmd_test.go` (only if the core budget moves, with its reason), `skill/screens.md`, `docs/site/templates.md`, `CHANGELOG.md`, `examples/*/…/render.go` and layouts where they use `ui.PagesConfig`, `.rastrillo/budgets.txt` for any directory that grew

- [ ] **Step 1:** SKILL.md's Never bullet on component scripts gains hooks, in the same bytes or near them: hand-written markup asks with `{{needs "select.js"}}` or `{{needs "rst-card"}}`. `skill/screens.md`'s `make perf` sentence says 15 kB and names the hook check. `docs/site/templates.md` documents `pageStylesheet`, `pageScripts` and `needs` for hooks.
- [ ] **Step 2:** CHANGELOG, under Unreleased, `### Changed: each page sends only the CSS it can show`: what changed; to upgrade, the `Assets: assets` line in `render.go` and the two layout lines (quoted exactly from Task 6); that apps making neither keep the whole file and the 40 kB limit; `needs` for hooks.
- [ ] **Step 3:** Update the in-repo examples' `PagesConfig` and layouts the same way; `make generate-check` passes.
- [ ] **Step 4:** Copy review for the CHANGELOG, SKILL and docs prose with Luna (`codex exec -m gpt-6-luna`, see memory), apply, and record it in a branch discussion.
- [ ] **Step 5:** Run `go test . -run Skill`, `make budget`, `make code` — PASS. Commit `Say what slicing changes: skill, docs, changelog, examples`.

### Task 10: Gate, review, hand to the benchmark

- [ ] Push; wait for `amadan ci status rastrillo/rastrillo -branch page-weight-2` to pass (if main moved, merge it first and re-run).
- [ ] Cross-provider review of the whole branch by Astra (`codex exec`, see memory), fixes red then green, recorded in a discussion; stop by the review-loop rule.
- [ ] Post on discussion 211 and message the benchmark session (`ListAgents`; the one running `bench/agent`) with the branch and commit, so it runs the spec arm on all three briefs before landing. Do not run the benchmark.
- [ ] Write the branch handoff.
