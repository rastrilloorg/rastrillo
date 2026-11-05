# Fiesta part 2: Rastrillo takes its look from Fiesta — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fiesta 1.2.0 takes Rastrillo's fixes since the seed; Rastrillo then requires it and serves Fiesta's stylesheets, themes, scripts and Lucide icons instead of its own copies, with the column shell's menus in a footer row, and every reference to Rastrillo owning the look corrected.

**Architecture:** Two repositories in order. Fiesta (rastrillo/fiesta, worktree `/home/paulca/amadan.net/rastrillo/fiesta-rastrillo-catch-up`, branch `rastrillo-catch-up` from `origin/main` bb23996) cuts 1.2.0 with its release recipe. Rastrillo (branch `fiesta-part-2`, worktree `/home/paulca/amadan.net/rastrillo/rastrillo-fiesta-part-2`) requires `amadan.net/rastrillo/fiesta v1.2.0` and reads every vendored byte through Fiesta's Go API. Then rastrillo-website syncs.

**Tech Stack:** Go 1.26, embed, html/template via `tmpl`, chromedp through each repo's own harness, Node for Fiesta's `check/node` harnesses.

**Spec:** `docs/superpowers/specs/2026-11-03-fiesta-part-2-design.md` (binding), including "Decided after Astra's first review" and "References this branch corrects". Background: the Fiesta design and the 1.1 design on Rastrillo's `fiesta-design` branch.

## Global Constraints

- No visible change in a Rastrillo app except the column shell's menus moving to `<footer rst-page-foot>` and Fiesta's select/date fixes (spec, Goals); Fiesta 1.2.0's one visible change is the console rail's ring fix.
- No CSS or browser JS left in Rastrillo `ui/` that Fiesta also ships. Rastrillo keeps only `live.js` and a fragment-shim `rastrillo.js`.
- Existing apps need no edit to keep working. `doctor --fix` never leaves a full `rastrillo.js` beside `fiesta.js`, nor an old `datetime.js` beside a new `calendar.js`.
- Per-page slicing (#158) unchanged: `tokens.css` sliced per page, `theme.css` whole, sliced pages ≤ 16,000 bytes (live pages their measured limit); a page over its limit is reported, not raised.
- Fiesta: bump `Version` to 1.2.0 and build with `go run ./cmd/fiesta release -version 1.2.0 -candidate` early, rebuild after asset changes, final `release -version 1.2.0`; land with `amadan branch merge … -squash -expect <sha> -message …`, annotated tag `v1.2.0`, ship with `make site-publish SITE_REF=v1.2.0` then `carlos ship`/`promote` to `canary/rehearsal` (memory: fiesta-repo-and-deploy). Stripped CSS build stays under 25 KiB (Astra's estimate with the ring fix: ~23.8k).
- Fiesta's frozen 1.0 tree is never edited (`TestTheBaselineIsOneOhOh`).
- House rules: code reads like a sentence, 80 columns, why-only comments; `make code` and `make budget` pass, raises carry reasons; targeted local tests (`GOFLAGS=-mod=mod` where a scratch module needs it), full suites and browser shards on the fleet.
- Copy (CHANGELOG, SKILL.md, docs, gallery prose) goes through Luna; prose has no em dashes as separators.
- Diminishing returns (Paul): fix what changes real behaviour; record the rest.

## Review Focus

1. **Old app, `doctor --fix`**: the first run on an app with the full `rastrillo.js` and a pre-`pageScripts` layout warns about the layout line; after adding it, a menu closes on Escape and focus returns to its summary. Task 4.
2. **Scheme with a server cookie**: choosing Dark persists across a reload with `fiesta.js` in place of the old apply. Task 3.
3. **Date field**: the calendar opens through `window.fiestaCalendar`; an app with an edited `datetime.js` keeps both date files after `doctor --fix`. Tasks 3–4.
4. **Pin drift**: a file Rastrillo vendors that the pinned release lacks fails a test naming it; served bytes (after minification) equal Fiesta's for every asset and theme. Task 2.
5. **Slicing on Fiesta's bytes**: created-hook and lit-hook lists match Fiesta's scripts. Task 2.
6. **Gallery**: every page the gallery renders resolves every script it links (no `assetURL` panic) and menus/switches in previews still work. Tasks 2–3.

---

### Task 1: Fiesta 1.2.0 catches up with Rastrillo

**Repo:** fiesta, worktree `/home/paulca/amadan.net/rastrillo/fiesta-rastrillo-catch-up`, branch `rastrillo-catch-up`.

**Files:** `fiesta.go` (Version, scripts list, mapping API), `css/tokens.css`, `js/shell.js`, `js/busy.js`, new `js/transition.js` + example + gallery mention + `check/node` harness, `check/parity_browser_test.go` and `check/oneoh_browser_test.go` (scoped ring exception), `check/baseline_test.go` / `releases_test.go` (named script and declaration exceptions), new regression tests for the excepted script changes, `CHANGELOG.md`, `releases/1.2.0/`, `hack/catch-up-1.2.md`.

**Interfaces:**
- Produces: Fiesta v1.2.0 tagged and live; `fiesta.ScriptNames()` includes `transition.js`; `fiesta.Sources() map[string]Source`, keyed by the example path as `hack/seed/sources.json` is, with `type Source struct{ Kind, Source, Locale, View string; State int }`; a test holds keys and values equal to the JSON.

- [ ] **Step 1:** Diff Rastrillo main's `ui/tokens.css`, `shell.js`, `busy.js`, `transition.js`, `select.js`, `datetime.js`, `calendar.js` against Fiesta's; write `hack/catch-up-1.2.md` listing each hunk as Fiesta-newer (keep) or Rastrillo-newer (take), with the Rastrillo commit. Known: Rastrillo-newer are the console ring (cc7c9c69), skipped transitions (d9c9ed13, 023740c5, e5e35a3e, 6b1c5478), busy's `data-live-form` (8548e455); Fiesta-newer are 1.1's additions, the select/date strings and fixes, `fiestaCalendar`.
- [ ] **Step 2:** Bump `Version` to 1.2.0; `go run ./cmd/fiesta release -version 1.2.0 -candidate`.
- [ ] **Step 3:** Take the Rastrillo-newer hunks. Run the gates; expect `TestOneOhScriptsAreUnchanged`, `TestEveryOneOhDeclarationStillApplies`, `TestEveryNewRuleRequiresNewMarkup` and the console parity/1.0 browser comparisons to fail naming exactly these changes.
- [ ] **Step 4:** Add one named exception per change (style of `widenedSelectors` / `newOnOldMarkup`), each with its reason and Rastrillo commit. The pixel suites get a scoped rendering exception for the console rail's current item; the frozen 1.0 tree is untouched. Each excepted script change gets its own regression test (shell.js: a skipped transition's rejection is swallowed, other errors still throw; busy.js: a `data-live-form` form claimed by a live script is not re-armed, one left native is).
- [ ] **Step 5:** Add `transition.js` (example, gallery line, scripts list, string-free, `check/node` harness ported from Rastrillo's `shell_transition_node.mjs`). Expose `Sources()` with a test that it equals `sources.json`.
- [ ] **Step 6:** Rebuild the candidate; gates pass locally where cheap, fleet CI green; `CHANGELOG.md` 1.2.0 entry (Luna) announces the ring fix, the shell and busy fixes, `transition.js` and `Sources()`.
- [ ] **Step 7:** Final rebuild with `go run ./cmd/fiesta release -version 1.2.0 -candidate` (the command refuses a non-empty `releases/1.2.0/` without `-candidate`), commit `releases/1.2.0/`, push, CI green, land, tag `v1.2.0` (annotated), push tag, ship and promote; check `/1/` serves 1.2.0 from the flagship over SSM.

### Task 2: Rastrillo reads its vendored bytes from Fiesta

**Repo:** rastrillo, branch `fiesta-part-2`.

**Files:** `go.mod`/`go.sum`; `ui/ui.go` (embeds), `ui/vendored.go`, `ui/transition.go` (`TransitionJS` from Fiesta), `ui/ui_test.go` (the `themesFS` inventory test), `internal/vendored/names.go`, `internal/vendored/hooks.go`, `ui/scripts.go` (`copy.js` in both detectors), `internal/designsystem/hashed.go` (`galleryAssets` gains `fiesta.js`, `copy.js`), `perf/perftest/firstload*.go` + `.rastrillo/budgets.txt` (references for `fiesta.js`, `copy.js`; derived live ceiling), `ui/shim_test.go` and `ui/datetime_test.go` (drop upstream raw-size gates), examples blog/tickets `static/` (refresh); delete `ui/tokens.css`, `ui/shell.css`, `ui/themes/`, the Fiesta-shipped scripts and their Node harnesses (`select_node.mjs`, `datetime_node.mjs`, `shell_node.mjs`, `shell_transition_node.mjs`); new tests `ui/vendored_fiesta_test.go`, `assets_vendored_test.go` additions.

**Interfaces:**
- Consumes: `fiesta.TokensCSS()`, `ShellCSS()`, `ThemeCSS(name)`, `Script(name)`, `ScriptNames()`, `Version`.
- Produces: `ui.TokensCSS()`, `ui.VendoredAssets(theme)`, `ui.TransitionJS`, `vendored.Names()` unchanged in signature, returning Fiesta's bytes and names plus Rastrillo's `rastrillo.js` and `live.js`; `copy.js` a component script loaded on `rst-snippet`.

- [ ] **Step 1: Failing tests:** `TestEveryVendoredFileComesFromThePinnedFiesta` (each name except `rastrillo.js`, `live.js` equals Fiesta's bytes; a missing one fails naming it); `TestNoFiestaFileHasACopyInUI`; `TestServedVendoredBytesEqualFiestas` (HTTP, every asset and every theme as `theme.css`, after Rastrillo's minification, against Fiesta's minified the same way); `TestASnippetLoadsCopyJS` (present, absent, region-inserted, file-deleted); `TestTheGalleryResolvesEveryScriptItLinks`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./ui ./internal/designsystem -run 'Fiesta|Snippet|Resolves'` — FAIL.
- [ ] **Step 3: Implement**: require v1.2.0, route embeds through Fiesta's API, delete the copies and harnesses (and the Go tests that drive them: `ui/select_test.go`, `ui/datetime_test.go`, `ui/shell_test.go` lose their Node calls; integration checks they made stay as Go or browser tests), add `fiesta.js` and `copy.js` to the vendored list and `galleryAssets`; update `vendored.CreatedHooks` and read-only lists for Fiesta's scripts; budget references measured (report any page over its limit); refresh example `static/` files.
- [ ] **Step 4: Run** `go test ./ui ./internal/vendored ./internal/designsystem ./perf/... . ./cmd/rastrillo -run 'Vendored|Fiesta|Hook|Scripts|Doctor|Markup|Budget|Gallery|Theme|Transition'`, the examples' token tests, `make code`, `make budget`, `make generate-check` — PASS.
- [ ] **Step 5: Commit** `ui: vendor Fiesta 1.2.0's files instead of copies`.

**Note (YAGNI):** no Rastrillo partial emits `rst-snippet` yet, so `copy.js`'s words come from its own table and English fallback; the partial and catalog keys wait for a partial that needs them.

### Task 3: rastrillo.js keeps only the shim; fiesta.js does the rest

**Files:** `ui/rastrillo.js`, `ui/scripts.go` (`fiesta.js` chosen for every page ahead of the presence filter), `ui/shim_test.go` (`TestShimContract` moves its menu/scheme/touch assertions to `fiesta.js`), browser fixtures that load only `/rastrillo.js` (`ui/browser_test.go`, `ui/touch_browser_test.go`, others found), `internal/designsystem/page.go` (`previewScripts` splits polling from Fiesta's enhancements; the gallery page links `fiesta.js`), `internal/designsystem/gallery_os_scheme_browser_test.go` (targets Fiesta's listener), every `rastrilloCalendar` use; `transition.js` stays synchronous before the stylesheets in each layout.

**Interfaces:**
- Produces: `pageScripts` lists `static/fiesta.js` first on every page where `has` allows it.

- [ ] **Step 1: Failing tests:** `TestRastrilloJSHoldsOnlyTheShim` (no menu-dismiss, switch-drag, touch-press or scheme-apply listener; keeps `poll`); `TestEveryPageLoadsFiestaJS` (first in `pageScripts` for each shell's plain page; absent when `has` is false); `TestTransitionJSStaysBeforeTheStylesheets`. Browser (fleet): `TestTheSchemeChoicePersistsWithFiestaJS` (Review Focus 2), `TestTheCalendarOpensThroughFiestaCalendar` (Review Focus 3), gallery preview menu and switch still work (Review Focus 6).
- [ ] **Step 2: Run** the non-browser tests — FAIL.
- [ ] **Step 3: Implement**: cut `rastrillo.js` to the shim; the cookie write listens for Fiesta's `rst-scheme` event inside the shim if the `/_theme` POST needs it; rename `rastrilloCalendar`; update fixtures and gallery script selection.
- [ ] **Step 4: Run** the tests; browser ones narrowly (`go test -tags browser ./ui -run '…' -count=1`, sandbox off) — PASS; the rest on the fleet.
- [ ] **Step 5: Commit** `ui: rastrillo.js keeps the fragment shim; fiesta.js does the rest`.

### Task 4: Column footer row, doctor's pairs and line, the Row Rule

**Files:** `ui/layouts/column.html`, the scaffold's column layout, test pages copying it, `cmd/rastrillo/doctor.go` + `doctor_test.go`, `SKILL.md` (+ `skillmd_test.go` budget if needed), affected browser tests.

**Interfaces:**
- Produces: column layout with `{{block "locale"}}` and `{{block "scheme"}}` inside `<footer rst-page-foot>` after `{{template "content" .}}`. `doctor` plans its fix first and decides on the asset set the fix leaves: `rastrillo.js`+`fiesta.js` and `calendar.js`+`datetime.js` refresh as pairs or not at all (an app-owned member keeps the pair, with a message why); the layout warning (`layout loads no fiesta.js: add {{range pageScripts}}… (CHANGELOG)`) fires when the resulting set has the slim shim and the layout neither ranges `pageScripts` nor links `fiesta.js`.

- [ ] **Step 1: Failing tests:** `TestTheColumnMenusSitInTheFooterRow`; `TestDoctorFixWarnsOnTheRunThatSlimsTheShim` (old app, full shim, pre-`pageScripts` layout: first `--fix` warns); `TestDoctorKeepsAnOwnedShimAndFiestaJSApart`; `TestDoctorMovesTheDateFilesTogether`; browser (fleet) `TestAnUpgradedOldAppClosesMenusOnEscape` (doctor --fix, add the named line, open a menu, Escape, focus on the summary).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement**; update browser drives that located column menus at the top; SKILL.md gains one Row Rule line.
- [ ] **Step 4: Run** `go test ./ui ./cmd/rastrillo . -run 'Column|Doctor|Skill|Layout'`, `make code`, `make budget` — PASS.
- [ ] **Step 5: Commit** `ui: the column shell's menus sit in a footer row`.

### Task 5: Partials held to Fiesta's examples; Lucide icons from Fiesta

**Files:** new `internal/designsystem/fiesta_golden_test.go` (beside the sample builders, reusing the seed's wrappers and picture rewriting from Fiesta's `hack/seed/_rastrillo/designsystem_fiestaseed_test.go`); `icons.go` and its test.

**Interfaces:**
- Consumes: `fiesta.Sources()`; `fiesta.FS()` for `examples/` and `icons/`.

- [ ] **Step 1: Failing tests:** `TestEveryPartialMatchesItsFiestaExample`: enumerate categories explicitly (kind `partial`; the menu idiom's `Contents`; sign-in's `screen` states; the standalone scheme state with actions removed), render each from its gallery sample, compare; a difference names the partial, the state and the first differing line; assert every `ui/partials/*.html` with a Fiesta example was compared. `TestLucideIconsComeFromFiesta`.
- [ ] **Step 2: Run** — FAIL (or pass where equal; the test is the guard).
- [ ] **Step 3: Implement**: read Lucide from Fiesta; fix drifted partials or record a named exception with its reason.
- [ ] **Step 4: Run** `go test . ./ui ./internal/designsystem ./internal/iconsets -run 'Fiesta|Icon'` — PASS.
- [ ] **Step 5: Commit** `ui: hold the partials to Fiesta's examples; icons from Fiesta`.

### Task 6: References, changelog, land, publish

- [ ] **Step 1:** Correct every reference in the spec's "References this branch corrects", from the final vendored set: ownership (DESIGN.md, PRODUCT.md, skill/*.md, SKILL.md), what `rastrillo.js` does, the date script's words and `fiestaCalendar`, inventories (docs, `new.go`, gallery downloads and weight lists), deleted paths in comments and tests, regenerated `skill/partials.md`. Gallery prose changes carry their translations in `prose.go`.
- [ ] **Step 2:** CHANGELOG Unreleased `### Changed: the look comes from Fiesta 1.2.0`: what changed; Fiesta's select/date fixes; the optional column-menu move (exact lines); `fiesta.js` and the layout line for pre-`pageScripts` layouts; `doctor`'s pairs; the 404 for apps without `Assets`; `fiestaCalendar`; `doctor --fix`. Luna reviews all copy; post as a branch discussion.
- [ ] **Step 3:** Push; fleet CI green (merge main first if it moved). Whole-branch review by Astra; one fix wave; stop by the diminishing-returns rule.
- [ ] **Step 4:** Land with `-squash -expect -message`, `make mirror`, branch handoff.
- [ ] **Step 5:** rastrillo-website `Sync:` PR to the landed sha (`node hack/sync-docs.mjs`, refresh `src/SKILL.md`), CI, merge, ship and promote per its AGENTS.md.
