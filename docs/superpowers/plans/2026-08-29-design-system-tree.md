# The Design-System Tree Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `internal/designsystem` renders the whole UI vocabulary — every partial, every class idiom, the tokens, the three shells — into a committed static tree at `docs/design-system/`, per theme × locale, gated fresh, ready for the website to vendor (PR 5).

**Architecture:** A pure-Go renderer (no server) walks theme × locale, binds `ui.Funcs` to the framework catalogs for that locale, renders one index page plus three full-page shell demos per combination, and writes shared assets once. `go generate ./...` regenerates; `TestDesignSystemIsCurrent` renders to memory and diffs the committed tree. The class-idiom samples move from `ui_test.go` into the `ui` package proper (`ui.Styleguide()`) so the page and the gates share one source.

**Tech Stack:** Go stdlib + html/template; the shipped `ui` package; `rastrillo.BaseCatalogs`/`BaseLocales`/`Dir`.

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md` §5 (§5.1 content, §5.2 build). PR 4 of §6. Where §5 conflicts with what PRs 1–3 actually shipped, the shipped code wins and Task 4 records the divergence.

## Global Constraints

- The tree is STATIC and self-contained: no fetches beyond its own files; the only JS is the shipped `rastrillo.js`/`select.js`/`datetime.js`; theme and language switching are plain links between pages.
- Locale codes: `rastrillo.BaseLocales()` (twelve). Themes: `ui.ThemeNames()` (three). Shells: `ui.LayoutNames()` (three). NEVER hardcode these lists — a fourth theme must appear in the tree without touching designsystem.
- Page copy (headings, explanatory sentences) is English (spec §5.1); component strings render in the page's locale via the framework catalogs.
- Every `{{define}}` in `ui.Templates()` and every `ui.Styleguide()` sample must appear on the index page — a gate enforces both.
- Tree size gate: total ≤ 20MB (spec estimated ~15MB).
- Directory layout, spec §5.2 verbatim: `docs/design-system/index.html` (= ink/en), `<theme>/<locale>/index.html`, `<theme>/<locale>/shells/<shell>.html`, shared `tokens.css`, `theme-<name>.css`, `rastrillo.js`, `select.js`, `datetime.js` at the root. Pages reference assets relatively (`../../tokens.css` etc.) so the tree works from any mount path.
- RTL locales (`rastrillo.Dir`) get `dir="rtl"` on `<html>`; `lang` set everywhere.
- The rules from PR #98 (which card is padded; screens stack vertically) appear beside the components they govern (spec §5.1).
- Gates: `GOFLAGS=-mod=mod go test . ./ui/ ./internal/... ./cmd/... -count=1` + blog/tickets/notes; `SKILL.md` ≤ 18,000 (16,826 now); docsite symbol gate for new exports. Never bare `git stash`. GOCACHE=$TMPDIR/gocache if needed.

---

## File map

| File | Responsibility |
|---|---|
| `ui/styleguide.go` (new) + `ui/ui_test.go` | `ui.Styleguide() map[string]string` — the class-idiom samples, moved out of the test file; tests re-point |
| `internal/designsystem/designsystem.go` | `Render() (map[string][]byte, error)` — the whole tree in memory, deterministic |
| `internal/designsystem/samples.go` | per-partial sample data (every state: tones, required, error, help, Plain + enhanced) |
| `internal/designsystem/page.go` | index-page assembly: switchers, tokens section, partials, idioms, rules, shell links |
| `internal/designsystem/designsystem_test.go` | freshness gate, coverage gates (defines + styleguide), size gate, determinism gate |
| `gen.go` (repo root, new) | `//go:generate go run ./internal/designsystem/cmd/dsgen` |
| `internal/designsystem/cmd/dsgen/main.go` | writes Render() to docs/design-system/, deleting stale files |
| `docs/design-system/**` | the committed tree |
| docs: `templates.md`, `SKILL.md`, spec §5 as-built | Task 4 |

---

### Task 1: `ui.Styleguide()`

**Files:** Create `ui/styleguide.go`; modify `ui/ui_test.go`.

Move `styleguideSamples` (ui_test.go ~line 1376) into the package as `func Styleguide() map[string]string` returning a copy. The samples call `iconSVG(...)` (a test helper) — replace those call sites with the real inline SVG the helper produces, or better: move the samples' icon usage to `{{icon "..."}}`-free literal SVG exactly as the test renders them today (check what iconSVG returns — it's the vendored Lucide markup; embed it literally). Tests re-point: `TestStyleguideSamplesRender`, `TestIdiomClassesAreStyled` and friends iterate `Styleguide()`; behaviour and coverage unchanged. Add `Styleguide` to `docs/site/reference/ui.md` (symbol gate). Doc comment: the canonical markup for the class idioms; the design-system page renders it; tests keep it honest against tokens.css.

- [ ] TDD: re-point the tests first (RED on undefined), move, GREEN, docsite gate, commit `ui: Styleguide() — the class-idiom samples become package API`.

---

### Task 2: The renderer

**Files:** Create `internal/designsystem/{designsystem.go,samples.go,page.go,designsystem_test.go}`.

**Interfaces (produces):**
```go
func Render() (map[string][]byte, error) // path (relative to docs/design-system) → content
```

Determinism: two calls yield identical maps (gate it — map iteration must be sorted before assembly anywhere order reaches output).

**Per locale, T binds to the framework catalogs:** `loc := rastrillo.BaseCatalogs()[code]`, lookup falls back to en, else the key; wrap as `ui.Funcs(ui.WithT(func(key string, args ...any) string {...interpolate {n}/{example} args…}))` — reuse the interpolation shape `locale.go`'s Tf uses (reimplement the tiny arg-map inline; no root-package export needed unless one exists — check first).

**The index page** (`page.go`), one per theme × locale, column-shell chrome (`rst-page`), in order:
1. Header: title, one-sentence English intro, the **theme switcher** (`rst-seg-tabs`, three links, `aria-current` on the active theme → `../../<theme>/<locale>/index.html` paths) and the **language switcher** (a `rst-dropdown rst-locale`-styled list of twelve links labelled by autonym — `locale_name` from each catalog — current one `aria-current`).
2. **Tokens**: every `--rst-*` custom property in the active theme's LIGHT block as a swatch row (name, value, colour chip via inline `style="background: var(--rst-…)"`), then the type scale (fs tokens rendered at size), spacing and radius rows. Parse the theme CSS with the same regex family `contrast_test.go` uses (copy the tiny parser, do not import test code). State plainly (English) that dark values live in the same file and the gate holds every documented pair to AA floors.
3. **Partials**: for every `{{define}}` name in `ui.Templates()` (enumerate via template parsing — parse with Funcs and list defined templates whose names aren't the page's own), render each state from `samples.go`. Group by family (list-screen / display / form / route — hardcode the grouping map; a new partial lands in an "ungrouped" section rather than being lost, and the coverage gate still counts it). Forms render inside `rst-box` per the #98 rules. Include: every tone of badge/callout/status-pill/meter; field partials in bare/hint/error/required states plus one `Plain`; the date fields with their enhancement live; a ≥10-option field-select (enhanced) AND an optgroup'd hand-written select; locale-menu with sample items; error-page in all five statuses + generic.
4. **Class idioms**: every `ui.Styleguide()` entry, rendered raw (they're complete HTML), each preceded by its name and — for `box` and `list-grid` — the #98 rules ("rst-list/rst-card hold rows only…", "screens stack vertically…") as short English callouts (`callout` partial, tone info).
5. **Shells**: three links to `shells/<name>.html` full-page demos.

**Shell pages**: parse `ui.Layout(name)` with the same funcs + `asset` mapped to relative paths + `iconAssets` empty; define `content` as a small representative screen (page-header + a rst-box + a short list-grid), `lang`/`dir` blocks per locale, nav/brand blocks filled with sample links so the chrome shows.

**Assets:** root-level `tokens.css` (= `ui.TokensCSS()`), `theme-<name>.css` per theme (= `ui.ThemeCSS`), the three JS files. Index pages link `../../tokens.css` + `../../theme-<theme>.css`; shell pages `../../../…`. The root `index.html` is byte-identical to `ink/en/index.html` except its asset paths (root-relative `tokens.css` etc.) — generate it separately with a path prefix parameter, don't copy.

**Gates in `designsystem_test.go`:**
- `TestRenderIsDeterministic` — two Renders, deep-equal.
- `TestEveryPartialAppearsOnThePage` — every define name occurs (as a rendered comment marker `<!-- partial: NAME -->` the page emits per section — gate greps markers, not fragile HTML).
- `TestEveryStyleguideSampleAppears` — same, `<!-- idiom: NAME -->`.
- `TestTreeStaysUnderTheSizeGate` — sum ≤ 20MB.
- `TestDesignSystemIsCurrent` — Render() vs `os.ReadFile` over `docs/design-system/**`: every rendered path exists with identical bytes AND no committed file is unrendered. Skip with a loud message if `docs/design-system` doesn't exist yet (Task 3 commits it; flip to hard-fail there via a `treeCommitted` const — mirror the fixturesComplete pattern).
- Every page parses as HTML far enough to check: `lang=` present, `dir="rtl"` iff `rastrillo.Dir(locale)=="rtl"`, no `rastrillo.ui.` key leaks, no absolute `/` asset links.

- [ ] TDD: gates first (RED), build renderer, GREEN, commit `designsystem: the renderer — theme × locale × shell, gated deterministic`.

---

### Task 3: Generate and commit the tree

**Files:** Create `gen.go` + `internal/designsystem/cmd/dsgen/main.go`; run it; commit `docs/design-system/**`; flip `treeCommitted`.

`dsgen`: delete `docs/design-system` (so removals show), `Render()`, write with 0644/dirs 0755, print a one-line summary (files, bytes). `gen.go` at repo root: `//go:generate go run ./internal/designsystem/cmd/dsgen` with a comment naming the freshness gate. Run `go generate ./...`; verify `TestDesignSystemIsCurrent` now hard-fails on any hand-edit (mutation-check one file). Spot-open two pages (en/ink and ar/warm shell) and sanity-read the HTML (report what you looked at). Commit `The design-system tree: docs/design-system, generated and gated` — the tree rides in this commit.

- [ ] Steps as above; full gate suite + examples.

---

### Task 4: Docs, SKILL.md, spec as-built

**Files:** `docs/site/templates.md` (a short section: the design system exists, what it shows, where it lives — rastrillo.org/design-system once PR 5 ships; regenerating: `go generate ./...`; the freshness gate), `docs/site/reference/ui.md` if any new ui export, `SKILL.md` (one sentence in the UI bullet: the vocabulary is browsable at rastrillo.org/design-system; regenerate docs/design-system with go generate after ui changes), spec §5 as-built sentences (dated 2026-08-29): actual page structure vs §5.1's list (strikethrough-plus-amend per convention where shipped differs — e.g. if wrong-version-struck-through examples of #98 rules aren't rendered as such, say what is), the marker-comment gating mechanism, the root index generated not copied, the 20MB gate, anything else Task 2/3 diverged.
- [ ] docsite RED→GREEN if applicable; all gates; `wc -c SKILL.md`; commit `Docs: the design-system tree; spec §5 as-built`.

---

### Task 5: Final review + PR

- [ ] Final whole-branch review BEFORE the push (standing order), one fix wave max, then push `design-system-tree` and `gh pr create` — title "The design-system tree: docs/design-system, generated and gated"; body: §5 delivery, the Styleguide move, the gates, the tree stats; note PR 5 (website) makes it public. Watch CI.

---

## Self-review

**Coverage:** §5.1 tokens/partials/idioms/shells/rules/switchers → T2; §5.2 layout/build/gate → T2+T3; docs → T4. Not in scope: the website (PR 5); translating page prose (spec: English).
**Types:** `Render() (map[string][]byte, error)` consumed by dsgen + gates; `ui.Styleguide()` consumed by T2 and the existing ui tests; `treeCommitted` mirrors the PR-3 `fixturesComplete` pattern.
**Order:** T1 → T2 → T3 → T4 strictly.
