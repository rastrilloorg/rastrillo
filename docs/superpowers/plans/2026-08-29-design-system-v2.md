# Design System v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Paul's v2 requirements for rastrillo.org/design-system and the framework beneath it: a new theme roster (day/plain/signal, all light+dark via `light-dark()`), semantic HTML for the interactive idioms (`<nav>`, `<dialog>`, `<search>`), default dropdown exclusivity, four visual bugs fixed, and a gallery rebuild — fully localised prose, top-right switchers (language + colour scheme), a searchable collapsible sidebar nav, per-example Desktop/Mobile/Code preview widgets via scaled srcdoc iframes, dead sample links neutralised, demo links in new tabs.

**Architecture:** Themes grow from colour+type to colour+type+shape (radius and shadow tokens move into theme files) and collapse to one `:root` block using `light-dark()` + `color-scheme` (tito CSS.md's principle), with the explicit toggle reduced to two one-line `color-scheme` overrides; the contrast gate learns to parse `light-dark(a, b)` and checks every pair in both schemes. The gallery keeps zero-required-JS (collapsible nav is `<details>`, iframes render without script) with two page-only enhancements (nav filter, scheme toggle) in a small gallery script.

**Tech Stack:** Go stdlib + html/template; `ui` package; chromedp (existing browser rig) for visual verification.

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md` §6-v2 (added in this plan's commit — the requirements list dated 2026-08-29). This is one PR.

## Global Constraints

- Zero-JS doctrine for the FRAMEWORK: every ui partial/idiom works with scripts off. Gallery-only scripts are allowed (the tree already ships JS) but must degrade: nav usable unfiltered, scheme follows OS, iframes render.
- All twelve locales everywhere; the gates from PRs 1–4 stay green or are consciously rewritten (never silently weakened). Tree ≤ 20MB unless measured over — then raise honestly with arithmetic in the test comment (house precedent).
- `GOFLAGS=-mod=mod`; `GOCACHE=$TMPDIR/gocache` if needed; never bare `git stash`; SKILL.md ≤ 18,000; docsite symbol gate per task that exports; examples' pins re-copied whenever tokens.css or a theme changes; regenerate the tree in any task that changes rendering (freshness gate).
- CSS.md adoption boundary (ruled): `light-dark()`, descriptive two-layer variables inside themes, semantic-first markup, alphabetised rules — YES. Custom elements replacing the `rst-*` class vocabulary — DEFERRED, recorded in the spec, not smuggled in.
- Browser drives: `go test -tags browser ./ui/` — run with the sandbox disabled where needed (established); keep assertions robust (CI flake history).

---

### Task 1: Themes v2 — day, plain, signal; light-dark(); shape tokens

**Files:** Rewrite `ui/themes/` (delete ink/teal/warm.css; create day.css, plain.css, signal.css); modify `ui/tokens.css` (radius+shadow token declarations move out; structure stays), `ui/ui.go` (`themeNames = {"day","plain","signal"}`, doc), `ui/contrast_test.go` (light-dark parser; every pair × both schemes), `ui/ui_test.go` (theme-block gates learn the single-block format), `cmd/rastrillo/new.go` (+test: default `--theme=day`), examples (re-vendor theme.css as day + tokens.css), `docs/site/templates.md`/`cli.md`/`reference/ui.md` theme prose, spec §1 as-built.

**Theme file format (binding):** one `:root` block: `color-scheme: light dark;` then every token as `--rst-x: light-dark(<light>, <dark>);` (single-valued tokens like fonts/radii plain); then exactly two toggle rules: `:root[data-theme="light"] { color-scheme: light; }` and `:root[data-theme="dark"] { color-scheme: dark; }`. Header comment carries the WCAG table for BOTH schemes. Shape tokens (`--rst-radius`, `--rst-radius-sm`, `--rst-radius-pill`, `--rst-shadow-pop`, `--rst-shadow-knob`, `--rst-shadow-lift`, `--rst-overlay`) now live in each theme; parity gate covers them.

**Palettes (starting points — tune until the rewritten gate passes both schemes; keep each theme's character):**
- **day** — personable-neutral default. Light: `bg #ffffff`, `surface #ffffff`/`surface-2 #f6f7f8`, grey text scale (`text #1a1d21`, `muted #4b5563`, `faint #6b7280`), lines `#e5e7eb`/`#9aa3af`, accent a friendly everyday blue `#2f6fed` (strong `#1d5bd6`, soft `#eef3fe`), tones: conventional green/amber/red on tinted bgs. Dark: true greys (`bg #111418`, `surface #1a1f26`), lighter blue accent. Radii 8/6/999, soft shadows. System font stack.
- **plain** — the skeleton. Greyscale only (accent = near-black `#111` light / near-white dark; accent-soft = light grey), system font, radii 4/3/999, shadows none-ish (`0 0 0 transparent` is invalid for the gate's hex parser — use plain rgba with 0 alpha? No: keep tiny real shadows or a flat 1px-style shadow; decide and document), tones = greyscale-tinted but still AA (positive/warning/negative must remain distinguishable — plain may use muted conventional hues at low chroma; note the choice in the header: a skeleton still has to signal errors).
- **signal** — modern, slick, assertive (the impeccable-style worked example; the implementer LOADS the `impeccable:impeccable` skill for the direction pass and records the direction in the file header). Starting intent: graphite neutrals (light: `bg #fafafa`, text near-black `#0c0e12`; dark: near-black `#0b0d10`, text `#e8eaee`), one electric cobalt accent `#1a56ff` (dark scheme: `#4d7dff`), tight grotesk-first font stack, sharp radii (4/2/999), decisive shadows (short, dark, low-blur), strong line-strong. Assertive ≠ unusable: every pair passes AA.

**Gate rewrite:** `contrast_test.go` parses `light-dark(A, B)` into per-scheme tables (plus plain-hex single-value fallback), runs the 26-pair table against light and dark per theme; the three-block `themeTokens` machinery and `blockBody` shrink accordingly. `TestThemesDeclareIdenticalTokenSets` unchanged in spirit (property parity incl. new shape tokens). tokens.css keeps a `var()`-only rule for everything structural; the no-colour-literal gate stays.

- [ ] TDD: rewrite gates first against the new format (RED on old files), write day.css, migrate machinery, then plain + signal; scaffold default flips to day (`TestNewThemeFlag` updates); examples re-vendored; `go generate ./...` NOT yet (Task 6 regenerates once — but the freshness gate would fail at this commit; ACTUALLY regenerate here too, tree must be green at every task commit). All suites + examples green. Commit `themes v2: day, plain and signal — one light-dark block, shape joins the theme axis`.

---

### Task 2: Semantic elements and dropdown exclusivity

**Files:** `ui/styleguide.go`, `ui/partials/*.html` (locale-menu, list-bar-search, pagination, dropdown-consumers), `ui/tokens.css` selectors where elements change, `ui/ui_test.go`, `internal/generate` goldens + examples if generated markup changes, `internal/designsystem` samples if markup changes, docs (`templates.md` idiom sections), spec as-built. Regenerate tree.

**Binding decisions:**
- **Modal → `<dialog open>`**: the idiom's panel becomes `<dialog class="rst-modal-panel" open>` inside the existing overlay div; rendered-open route + inert backdrop stay (zero-JS preserved; non-modal dialog is exactly the rendered-open pattern). CSS: dialog resets (`dialog { border: 0; padding: 0; ... }` scoped to `.rst-modal-panel`); `::backdrop` unused (our overlay div is the scrim). Update styleguide sample, modal demo template, tokens.css, docs.
- **`<nav>`**: pagination partial (check — may already be nav), locale-menu wraps in `<nav aria-label>` or sits within one (decide against double-landmarking if callers place it in a nav; simplest: locale-menu's `<details>` gains no nav, but the SHELLS' locale/account cluster is already inside the header — verify shells use `<nav>` for link clusters and add where missing), seg-tabs → `<nav>`, back-nav → `<nav>`.
- **`<search>`**: list-bar-search's form becomes `<search><form ...></form></search>` or `role="search"` on the form (the `<search>` element is baseline 2023 — use it; fallback is harmless).
- **Dropdown exclusivity by default**: every `rst-dropdown` and `rst-row-menu` `<details>` emits `name="rst-menus"` unless the caller sets its own (partial key `MenuGroup`/existing API check; for the class idioms it's documentation + styleguide samples updated). Nested `rst-menu-group` keeps its own distinct name (MUST NOT share, or opening a submenu closes its parent — test this in the browser drive). Shell chrome `<details>` and tblock stay outside the group. Browser drive: open account dropdown, open a row menu → account closes; open nested group → parent stays open.
- Audit pass: grep partials/idioms for divs playing button/nav/list roles; fix the clear cases, list the deliberate non-changes (rst-lrow grid stays divs — it's a layout grid with a documented reason) in the report + docs.

- [ ] TDD (markup assertions + browser drive for exclusivity) → implement → goldens/examples/tree regenerate → all suites → Commit `ui: semantic elements for the interactive idioms; dropdowns close each other by default`.

---

### Task 3: The four visual bugs (browser-verified)

**Files:** `ui/tokens.css` (+pins), possibly `ui/partials/field*.html`, `ui/browser_test.go` or a new screenshot-driven check, `internal/designsystem/samples.go` if sample data contributed. Regenerate tree.

From Paul's screenshots (reproduce each in the browser rig first — screenshot, fix, re-screenshot):
1. **rst-field-row misalignment when one field carries an error/help** (img: "Dates only, with an error on the end" — the To field rides high and its error text overlaps the From field). Root cause: `align-items: end` + content-below-input. Fix: field-row aligns by the CONTROL row — e.g. `align-items: start` with labels normalised, or grid rows (label row / control row / message row) via subgrid where supported with a flex fallback; simplest robust: each `.rst-field` in a row reserves message space (`.rst-field__error/help` doesn't shift siblings — message flows under its own column, row uses `align-items: start`). No overlap at any width; verify.
2. **Grow/short proportions** (img: City enormous, ZIP tiny far right, labels detached): revisit `.rst-field-row` + `.rst-grow` + `--short` interplay; a short field should hold a sane min width (`min-width: 8rem`?) and the row should wrap gracefully; labels stay glued to their controls.
3. **Date-field icon-button placement** (imgs: calendar button inconsistent — inside flush right vs mid-field): the enhancement's picker button position must be stable — absolutely positioned inside the field wrapper at inline-end with input padding-inline-end reserving space, identical in empty and filled states, RTL-correct (logical properties).
4. **Enhanced-sample dead space** (img: large empty area under the enhanced date field inside its card): the gallery's enhanced samples reserve popover space or the card has stray padding — find and fix (likely samples.go structure or `.rst-dtp` static layout leaking into flow when closed).

- [ ] Reproduce → fix → re-verify with screenshots attached to the report (paths in $TMPDIR listed) → pins + tree → Commit `ui: field-row alignment, short-field proportions, date-button placement`.

---

### Task 4: Gallery localisation and the top-right switchers

**Files:** `internal/designsystem/page.go`, new `internal/designsystem/prose.go` (the page's own strings ×12), `internal/designsystem/gallery.js` (new, embedded+emitted asset: scheme toggle + nav filter — written this task, nav filter wired in Task 5), `designsystem_test.go`. Regenerate tree.

- **All page text localises.** Every English string the renderer emits (section headings, explanations, notes, switcher labels, preview-tab labels) moves to a `map[string]map[string]string` (key → locale → text) in `prose.go`, en as source, eleven machine-drafted translations (house register; the catalogs' conventions). A gate: every prose key has all twelve locales, non-empty (mirror the catalog key-set gate). The `rastrillo.ui.*` component strings already localise. NO leaked English on a non-en page except deliberate fixture data (sample names etc.) — extend the leak gate: a curated list of en-only prose sentinels (e.g. "Skip to content" is a catalog string — fine; pick 3 distinctive prose strings and assert absent on ja page).
- **Language switcher top-right**, with the scheme toggle, in a compact `<header>` bar (page chrome, not the rst shells): language as the existing dropdown (autonyms), scheme toggle as a three-state control (System/Light/Dark — sets `data-theme` on `<html>`, persists localStorage, no-JS default = System via `color-scheme`). Theme switcher (day/plain/signal) stays with it. All three grouped top-right; `aria-current`/`aria-pressed` correct.
- gallery.js: tiny, first-party, inert-safe (page works scriptless; toggle hidden via `noscript`-friendly pattern or shown-but-System-only). Absolute-link + self-contained gates cover the new asset (add to the asset list + tree shape count).

- [ ] Gates first (prose parity, leak sentinels, tree shape) → implement → regenerate → Commit `design-system: the page speaks all twelve languages; switchers move top-right`.

---

### Task 5: Sidebar navigation with search

**Files:** `internal/designsystem/page.go` (+prose keys), `gallery.js` (filter), `designsystem_test.go`. Regenerate tree.

- Layout becomes a two-column shell (reuse `rst-shell-sidebar` classes — the gallery dogfoods the sidebar shell): left rail = the gallery nav; main = current content. Mobile: the existing `<details>` chrome collapse.
- Nav content: collapsible `<details open>` sections (Tokens / Partials by family / Class idioms / Shells / Demos), each item linking `#anchor` — every section and every example gets a stable, gated anchor id (extend the marker mechanism: `<section id="partial-badge">` etc.; gate: every nav href resolves to an id on the page — extend the absolute-link gate to fragment hrefs).
- Search: an `<input type="search">` at the rail top (inside `<search>`), gallery.js filters nav items live (substring, case/accent-folded — reuse datetime.js's fold? No: three lines inline), hiding empty sections; scriptless = plain nav. Placeholder + "no matches" via prose keys.

- [ ] Gates (anchor resolution, nav completeness: every rendered marker has a nav entry — derive nav from the same data, so gate is structural) → implement → regenerate → Commit `design-system: a searchable sidebar of everything on the page`.

---

### Task 6: Preview widgets, dead links, new tabs

**Files:** `internal/designsystem/page.go`, `samples.go`, `designsystem_test.go`, `ui/styleguide.go` untouched (source stays canonical). Regenerate tree.

- **Preview widget** per example (partial states, idioms, modal, shells): tabs `[Desktop] [Mobile] [Code]` (prose keys). Implementation: one `<iframe srcdoc="...">` per example — srcdoc document = minimal page (doctype, `<html lang dir>`, absolute stylesheet links tokens+theme, the example markup, script tags only for enhanced examples needing them) — plus the escaped-source `<pre>`. Desktop: iframe rendered at a virtual 1200px width, scaled to its container via `transform: scale(calc(...))` wrapper (fixed aspect box; measure a sane default height per family, overflow scrolls inside). Mobile: same srcdoc at 390px virtual width, centred. Tabs are CSS-only (radio inputs + labels, `:checked` panels) so no JS required; gallery.js may sync heights later — not required.
- Modal + shells: their inline preview becomes the same widget (srcdoc shows the real thing scaled — replacing/augmenting the escaped-source-only treatment; the nested-`<main>` concern vanishes inside an iframe document); keep the **full-page links**, now `target="_blank" rel="noopener"` (all renderer-owned "open the demo/shell" links).
- **Dead links**: samples.go href values become `"#"` (they're gallery data); Styleguide()-sourced LIVE renders get non-mount, non-fragment `href="/..."` rewritten to `#` at render (escaped source keeps the canonical hrefs); the "links go nowhere" callout softens to one line about `#`. Gate: no live `href="/` outside `/design-system/` on any page (tightens the absolute-link gate's content exemption — content links are now `#` or mount-rooted).
- srcdoc escaping: build the srcdoc document as a string, let html/template attribute-escape it; gate: each srcdoc round-trips (unescape → parseable, stylesheet links absolute, no `rastrillo.ui.` leaks) — extend the whole-document gate to srcdoc payloads.
- Size: measure; expect growth (each example ~2×). If >20MB, raise the gate honestly (arithmetic in the comment). Determinism/coverage/freshness all stay.

- [ ] Gates → implement → regenerate → Commit `design-system: desktop/mobile/code previews; sample links go nowhere on purpose`.

---

### Task 6b: The accessibility gate

**Files:** `ui/testdata/axe/axe.min.js` (vendored, version pinned in a README beside it), `ui/browser_test.go` or a new `ui/a11y_test.go` (build tag `browser`), `internal/designsystem` only if findings force markup changes. Regenerate tree if markup changes.

- Vendor axe-core (pin the version; record sha256). In the browser rig, load representative COMMITTED gallery pages (root index, plain/en, signal/en, day/ar, one modal demo, one shell demo) in both schemes (set data-theme), inject axe, run with the `wcag2a, wcag2aa, wcag21aa, wcag22aa` tags, and FAIL on any violation, printing rule id + selector per finding. Plus: a 320px-viewport reflow check (no horizontal scrollbar on the index) and a keyboard walk (Tab through the first N interactive elements of the index; assert focus visible — computed outline/box-shadow changes — and no keyboard trap).
- Fix what the scan finds in the source that owns it (renderer, partials, tokens.css — pins!); anything ruled-not-fixed gets a documented, named exemption list in the test with the reason (mirror colorMixSkip's convention).
- Docs: one honest sentence in templates.md's design-system section: the gallery is scanned to WCAG 2.2 AA by axe-core in CI; automated scanning covers roughly half the criteria, the rest is reviewed by hand.

- [ ] Vendor → scan RED (expect findings) → fix/rule → GREEN → Commit `design-system: the gallery is scanned to WCAG 2.2 AA`.

---

### Task 7: Docs, SKILL.md, spec as-built

**Files:** `docs/site/templates.md` (themes section rewrite: day/plain/signal, light-dark format, shape-in-theme; semantic elements; exclusivity default), `cli.md`, `reference/ui.md`, `forms.md` if field-row guidance changes, `SKILL.md` (theme names + one sentence on exclusivity default; budget), spec §6-v2 as-built sentences (incl. the CSS.md adoption boundary and the custom-elements deferral), `AGENTS.md`?? no. Gates: docsite; all suites; examples.
- [ ] Commit `Docs: design system v2 — the new themes, semantic idioms, and the gallery`.

---

### Task 8: Final review, PR, deploy

- [ ] Final whole-branch review (most capable model; render + READ pages incl. an RTL one and a srcdoc payload; probe the scheme toggle scriptlessly) → one wave max → push, PR (title "Design system v2: day/plain/signal, semantic idioms, and a gallery worth browsing"), CI, merge on approval — then the website sync + ship/promote chain with the smoke check (slash-less page, stylesheet 200s, cache-busted).

---

## Self-review

Every user requirement maps: AA verification → T6b; switcher-changes-everything + top-right → T4; sidebar+search → T5; ink→day → T1; plain → T1; impeccable theme (light+dark, switcher in preview) → T1 (+T4 scheme toggle); semantic elements → T2; visual hiccups → T3; preview desktop/mobile/code + iframe scaling + modal preview + keep links out → T6; CSS.md → T1 (light-dark, layering) + T2 (semantic) + spec boundary (T7); dead links → T6; new-tab → T6; dropdown exclusivity → T2. Order: T1 before T4/T6 (themes feed srcdoc + scheme toggle); T2 before T6 (markup feeds previews); T3 independent after T1 (pins). One PR, one deploy.
