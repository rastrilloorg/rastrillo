# Fiesta part 2: Rastrillo takes its look from Fiesta — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fiesta 1.2.0 takes Rastrillo's fixes since the seed; Rastrillo then requires it and serves Fiesta's stylesheets, themes, scripts and Lucide icons instead of its own copies, with the column shell's menus in a footer row.

**Architecture:** Two repositories in order. Fiesta (rastrillo/fiesta, checkout `/home/paulca/amadan.net/rastrillo/fiesta-fiesta-1`, branch from `origin/main`) cuts 1.2.0 with its release recipe. Rastrillo (branch `fiesta-part-2`, worktree `/home/paulca/amadan.net/rastrillo/rastrillo-fiesta-part-2`) requires `amadan.net/rastrillo/fiesta v1.2.0` and reads every vendored byte through Fiesta's Go API.

**Tech Stack:** Go 1.26, embed, html/template via `tmpl`, chromedp through each repo's own harness.

**Spec:** `docs/superpowers/specs/2026-11-03-fiesta-part-2-design.md` (binding). Background: the Fiesta design and the 1.1 design on Rastrillo's `fiesta-design` branch.

## Global Constraints

- No visible change in a Rastrillo app except the column shell's menus moving to `<footer rst-page-foot>`; Fiesta 1.2.0's one visible change is the console rail's ring fix.
- No CSS or browser JS left in Rastrillo `ui/` that Fiesta also ships. Rastrillo keeps only `live.js` and a fragment-shim `rastrillo.js` (plus `transition.js` until Fiesta has it, then from Fiesta).
- Existing apps need no edit to keep working; `fiesta.js` loads through `pageScripts`, which skips a file the app lacks.
- Per-page slicing (#158) unchanged: `tokens.css` sliced per page, `theme.css` whole, sliced pages ≤ 16,000 bytes (live pages their measured limit); a page over its limit is reported, not raised.
- Fiesta: release with `go run ./cmd/fiesta release -version 1.2.0`, land with `amadan branch merge … -squash -expect <sha> -message …`, annotated tag `v1.2.0`, ship with `make site-publish SITE_REF=v1.2.0` then `carlos ship`/`promote` to `canary/rehearsal` (memory: fiesta-repo-and-deploy). Its stripped build stays under 25 KiB.
- House rules: code reads like a sentence, 80 columns, why-only comments; `make code` and `make budget` pass, raises carry reasons; targeted local tests, full suites and browser shards on the fleet.
- Copy (CHANGELOG, SKILL.md, docs) goes through Luna; prose has no em dashes as separators.
- Diminishing returns (Paul): fix what changes real behaviour; record the rest.

## Review Focus

1. **An existing app that runs `doctor --fix` with a layout that predates `pageScripts`**: menus still close on Escape once it adds the one line `doctor` names; `doctor` names it. Test in Task 4.
2. **The scheme menu with a server cookie**: choosing Dark in a Rastrillo app still persists across a reload (the `/_theme` POST and cookie), with `fiesta.js` in place of the old `rastrillo.js` apply. Test in Task 3.
3. **A page that uses the date field**: the calendar opens, now through `window.fiestaCalendar`. Test in Task 3.
4. **Pin drift**: a file Rastrillo vendors that the pinned Fiesta release lacks fails a test naming it. Test in Task 2.
5. **Slicing on Fiesta's bytes**: the created-hook and lit-hook lists still match the scripts (now Fiesta's). Test in Task 2.

---

### Task 1: Fiesta 1.2.0 catches up with Rastrillo

**Repo:** fiesta. Branch `rastrillo-catch-up` from `origin/main`, in its own worktree.

**Files:** `css/tokens.css`, `js/shell.js`, `js/busy.js`, new `js/transition.js` with an example and a gallery mention, `fiesta.go` (scripts list; the mapping API), `check/baseline_test.go` (named exceptions), `releases_test.go` (named exception), `CHANGELOG.md`, `releases/1.2.0/`.

**Interfaces:**
- Produces: Fiesta v1.2.0 tagged and live; `fiesta.ScriptNames()` includes `transition.js`; `fiesta.Sources() map[string]Source` (or an equally small API) exposing `hack/seed/sources.json`'s state-to-partial mapping, where `type Source struct{ Kind, Source, Locale string; State int }`.

- [ ] **Step 1:** Diff Rastrillo main's `ui/tokens.css`, `ui/shell.js`, `ui/busy.js`, `ui/transition.js` against Fiesta's; write `hack/catch-up-1.2.md` listing each hunk as Fiesta-newer (keep) or Rastrillo-newer (take), with the Rastrillo commit.
- [ ] **Step 2:** Run Fiesta's gates (`make ci` targets for check/ and releases) after taking the Rastrillo-newer hunks; expect `TestOneOhScriptsAreUnchanged`, `TestEveryOneOhDeclarationStillApplies`, `TestEveryNewRuleRequiresNewMarkup` and the console pixel baseline to fail, naming exactly these changes.
- [ ] **Step 3:** Add one named exception per change in each gate (style of `widenedSelectors` / `newOnOldMarkup`), each with its reason and Rastrillo commit; move the console pixel baseline with a recorded reason. Add `transition.js` (example, gallery line, scripts list, string-free). Expose the mapping through the Go API with a test that it equals `hack/seed/sources.json`.
- [ ] **Step 4:** Gates pass; `CHANGELOG.md` 1.2.0 entry announces the ring fix, the shell and busy fixes, `transition.js` and the mapping API (copy through Luna).
- [ ] **Step 5:** `go run ./cmd/fiesta release -version 1.2.0`, commit `releases/1.2.0/`, push, hub CI green, land, tag `v1.2.0` (annotated), ship and promote, check `/1/fiesta.css` reports 1.2.0 from the flagship over SSM.

### Task 2: Rastrillo reads its vendored bytes from Fiesta

**Repo:** rastrillo, branch `fiesta-part-2`.

**Files:** `go.mod`/`go.sum` (require `amadan.net/rastrillo/fiesta v1.2.0`), `ui/ui.go` (embeds), `ui/vendored.go`, `internal/vendored/names.go`, `internal/vendored/hooks.go`, delete `ui/tokens.css`, `ui/shell.css`, `ui/themes/`, and the Fiesta-shipped scripts from `ui/`; tests: `ui/vendored_fiesta_test.go`, the existing script-hook tests.

**Interfaces:**
- Consumes: `fiesta.TokensCSS() []byte`, `fiesta.ShellCSS() []byte`, `fiesta.ThemeCSS(name string) ([]byte, bool)`, `fiesta.Script(name string) ([]byte, bool)`, `fiesta.ScriptNames() []string`, `fiesta.Version`.
- Produces: `ui.TokensCSS()`, `ui.VendoredAssets(theme)`, `vendored.Names()` unchanged in signature, now returning Fiesta's bytes and names plus Rastrillo's own `rastrillo.js` and `live.js`; `fiesta.js` and `copy.js` in `vendored.Names()`; `copy.js` a component script loaded where a page shows `rst-snippet`.

- [ ] **Step 1: Write the failing tests** in `ui/vendored_fiesta_test.go`:
  - `TestEveryVendoredFileComesFromThePinnedFiesta`: for each name in `vendored.Names()` except `rastrillo.js` and `live.js`, `ui.VendoredAssets("day")[name]` equals Fiesta's bytes for it (Review Focus 4: a name Fiesta lacks fails naming it).
  - `TestNoFiestaFileHasACopyInUI`: none of Fiesta's CSS or script names exists as a file under `ui/`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./ui -run 'Fiesta'` — FAIL.
- [ ] **Note (YAGNI):** no Rastrillo partial emits `rst-snippet` yet, so `copy.js`'s two words (`copy-done`, `copy-failed`) come from its own table and English fallback; the partial and its catalog keys wait for a partial that needs them. Say so in the CHANGELOG.
- [ ] **Step 3: Implement**: require v1.2.0, route the embeds through Fiesta's API, delete the copies, add `fiesta.js` and `copy.js` to the vendored list; update `vendored.CreatedHooks` and the read-only lists so `TestEachScriptCreatesExactlyItsListedHooks` and `TestEveryHookAScriptNamesIsCreatedOrOnlyRead` pass against Fiesta's scripts (Review Focus 5); `copy.js` joins `ComponentScripts` with its hook `rst-snippet`.
- [ ] **Step 4: Run** `go test ./ui ./internal/vendored . ./cmd/rastrillo -run 'Vendored|Fiesta|Hook|Scripts|Doctor|Markup'`, `make code`, `make budget`, `make generate-check` — PASS.
- [ ] **Step 5: Commit** `ui: vendor Fiesta 1.2.0's files instead of copies`.

### Task 3: rastrillo.js keeps only the shim; fiesta.js does the rest

**Files:** `ui/rastrillo.js`, `ui/scripts.go` (`optionalScripts` gains `fiesta.js`, first), `ui/layouts/*.html` (no change if `pageScripts` already ranges), any `rastrilloCalendar` reference, tests in `ui/`.

**Interfaces:**
- Consumes: Task 2's vendored `fiesta.js`; `planPage`'s `has`.
- Produces: `pageScripts` lists `static/fiesta.js` first on every page where the app has it.

- [ ] **Step 1: Write the failing tests:**
  - `TestRastrilloJSHoldsOnlyTheShim`: `ui/rastrillo.js` defines no menu-dismiss, switch-drag, touch-press or scheme-apply listener (assert absent identifiers taken from the old file's function names) and keeps `poll`.
  - `TestEveryPageLoadsFiestaJS`: each shell's plain page's `pageScripts` starts with `static/fiesta.js`; with `has` false for it, it is absent.
  - Browser (fleet): `TestTheSchemeChoicePersistsWithFiestaJS` (Review Focus 2): choose Dark through the scheme menu on a scaffold-shaped page, reload, still dark; `TestTheCalendarOpensThroughFiestaCalendar` (Review Focus 3).
- [ ] **Step 2: Run** the non-browser tests — FAIL.
- [ ] **Step 3: Implement**: cut `rastrillo.js` to the shim; any server-side need the old apply served (writing the scheme cookie) listens for Fiesta's `rst-scheme` event inside the shim; rename `rastrilloCalendar` uses.
- [ ] **Step 4: Run** the tests; run the browser ones narrowly (`go test -tags browser ./ui -run '…' -count=1`, sandbox off) — PASS; the existing menu, switch and shell drives run on the fleet.
- [ ] **Step 5: Commit** `ui: rastrillo.js keeps the fragment shim; fiesta.js does the rest`.

### Task 4: The column footer row, doctor's line, the Row Rule

**Files:** `ui/layouts/column.html`, the scaffold's column layout source (via `ui.Layout`), test pages that copy the column layout, `cmd/rastrillo/doctor.go` + `doctor_test.go`, `SKILL.md` (+ `skillmd_test.go` budget if needed), affected ui browser tests.

**Interfaces:**
- Produces: column layout with `{{block "locale"}}` and `{{block "scheme"}}` inside `<footer rst-page-foot>` after `{{template "content" .}}`; `doctor` reports `layout loads no fiesta.js: add {{range pageScripts}}… (CHANGELOG)` when `static/rastrillo.js` is the slim shim and the app's layout neither ranges `pageScripts` nor links `fiesta.js`.

- [ ] **Step 1: Write the failing tests:**
  - `TestTheColumnMenusSitInTheFooterRow`: the column layout's rendered page has the locale and scheme menus inside `footer[rst-page-foot]`, after the content, and none before the page's `h1`.
  - `TestDoctorNamesTheFiestaJSLine` (Review Focus 1): an app with the slim `rastrillo.js` and a pre-`pageScripts` layout gets the message; an app with the line, or the old full `rastrillo.js`, gets none.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement**; update browser drives that located the column menus at the top (fleet runs them); SKILL.md gains one Row Rule line.
- [ ] **Step 4: Run** `go test ./ui ./cmd/rastrillo . -run 'Column|Doctor|Skill|Layout'`, `make code`, `make budget` — PASS.
- [ ] **Step 5: Commit** `ui: the column shell's menus sit in a footer row`.

### Task 5: Partials checked against Fiesta's examples; Lucide icons from Fiesta

**Files:** a new `ui/fiesta_golden_test.go`; `icons.go` (Lucide source) and its test; nothing in `internal/iconsets` beyond reading Lucide from Fiesta.

**Interfaces:**
- Consumes: Task 1's mapping API; `fiesta.FS()` for `examples/` and `icons/`.

- [ ] **Step 1: Write the failing tests:**
  - `TestEveryPartialMatchesItsFiestaExample`: for each mapping entry of kind `partial`, render the partial with the state's data (as Fiesta's seed did) and compare with the example as the 1.1 design's table says (`Element` for four states, the toolbar's `Contents` for the menu idiom, the standalone scheme state with actions removed); a difference names the partial, the state and the first differing line.
  - `TestLucideIconsComeFromFiesta`: each icon Rastrillo inlines equals Fiesta's `icons/<slug>.svg`.
- [ ] **Step 2: Run** — FAIL (or pass where already equal; a passing comparison is fine, the test is the guard).
- [ ] **Step 3: Implement**: read Lucide from Fiesta; fix any partial whose markup drifted from its example, or record a named exception with its reason where Rastrillo's markup is deliberately different.
- [ ] **Step 4: Run** `go test ./ui ./internal/iconsets -run 'Fiesta|Icon'` — PASS.
- [ ] **Step 5: Commit** `ui: hold the partials to Fiesta's examples; icons from Fiesta`.

### Task 6: Docs, changelog, land

- [ ] **Step 1:** CHANGELOG Unreleased `### Changed: the look comes from Fiesta 1.2.0`: what changed; the optional column-menu move (the exact lines); `fiesta.js` and the one layout line for pre-`pageScripts` layouts; `fiestaCalendar`; `doctor --fix` to take the new files. Docs point to fiesta.rastrillo.org for the look. Luna reviews; post as a branch discussion.
- [ ] **Step 2:** Push; fleet CI green (merge main first if it moved). Whole-branch review by Astra; one fix wave; stop by the diminishing-returns rule.
- [ ] **Step 3:** Land with `-squash -expect -message`, `make mirror`, branch handoff.
