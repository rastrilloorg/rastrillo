# Design System v2.1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the four bugs Paul found on the live v2 page, split the 371 KB gallery into per-section pages, and add the three pages it was missing — Overview (with an "everything" demo), Icons, and Getting started.

**Architecture:** `internal/designsystem` already renders a deterministic tree from `ui`'s own data. This slice changes what it renders into: one index page per section instead of one page carrying everything, plus three new page kinds. The CSS bugs are fixed in `ui/tokens.css` and the sidebar layout in `ui/layouts/`, so apps get them too — the gallery is the witness, not the owner.

**Tech Stack:** Go stdlib + html/template; the shipped `ui` package; `rastrillo.BaseCatalogs`/`BaseLocales`/`Dir`.

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md` §6-v2.1.

## Global Constraints

- **Zero JavaScript required.** Every bug fix and every new page works with JS off. The sidebar dropup is `<details>` positioned `bottom: 100%`, not script.
- **`ui/rastrillo.js` is at 16,378 of its 16,384-byte cap — six bytes.** No task here may add behaviour to it. If a task believes it needs JS, it must stop and say so rather than raise the cap or split the file on its own initiative.
- Never hardcode the theme, locale, shell or icon lists: `ui.ThemeNames()`, `rastrillo.BaseLocales()`, `ui.LayoutNames()`, `rastrillo.IconSlugs()`. A fourth of anything must appear without touching the renderer.
- **Every user-facing string goes through `prose.go` in all twelve locales.** New English gets machine-drafted translations in the same register; `TestEveryProseKeyIsTranslated` and `TestNoEnglishProseReachesATranslatedPage` both gate it. Identifiers (`rst-box`, `select.js`, `tokens.css`) are never translated; `{placeholders}` survive verbatim.
- Every link stays inside `/design-system` and resolves; the absolute-link gate holds (the CARLOS edge serves slash-less directory URLs as 200 with no redirect).
- WCAG 2.2 AA: the axe scan covers the new pages too. Zero violations, zero rules exempted.
- Gates: `GOFLAGS=-mod=mod go test . ./ui/ ./view/ ./form/ ./internal/... ./cmd/...` + `-tags browser -p 1 ./harness/ ./ui/ ./internal/designsystem/` + blog/tickets/notes. `SKILL.md` ≤ 18,000 (17,602 now). Tree ≤ 20 MiB. Regenerate with `go generate ./...`.
- **Environment:** the sandbox denies writes to the default GOCACHE — run go commands with `dangerouslyDisableSandbox: true`. Failures in `cmd/rastrillo`/`internal/generate`/`internal/manifest` are that artefact, not a defect. Never bare `git stash`.

---

## File map

| File | Responsibility |
|---|---|
| `ui/tokens.css` | list-card clipping fix; menu `hr`; page column 52rem → 64rem; dropup positioning |
| `ui/layouts/sidebar.html` | profile to the rail's foot, language as a dropup above it |
| `ui/partials/*.html` | nav disclosure glyphs → `{{icon "chevron-down"}}` |
| `internal/designsystem/designsystem.go` | render one page per section instead of one index |
| `internal/designsystem/page.go` | per-page assembly; nav across pages; Overview, Icons, Getting started |
| `internal/designsystem/samples.go` | the "everything" demo screen |
| `internal/designsystem/prose.go` | new strings × 12 locales |
| `internal/designsystem/designsystem_test.go` | gates follow the split |
| `docs/site/templates.md`, `SKILL.md`, spec §6-v2.1 as-built | Task 8 |

---

### Task 1: The four bugs

**Files:** `ui/tokens.css`, `ui/layouts/sidebar.html`, the nav partial(s), `ui/ui_test.go`.

1. **List-card clipping.** Remove `overflow: hidden` from `.rst-list` (tokens.css:255-260) and round the first/last row corners instead so the card still clips its rows visually. A menu opened in a `rst-bulkbar` or `rst-lbar` inside a list card must escape the card.
2. **Menu `hr`.** Widen `.rst-row-menu__panel hr` (`:651`) to every menu surface — `.rst-dropdown__menu`, `.rst-locale`, `.rst-row-menu__panel`.
3. **Nav glyphs → Lucide.** Replace the ▸/▾ text glyphs with `{{icon "chevron-down"}}`, rotated by CSS on `[open]`. The icon is decorative beside a text label: `aria-hidden`.
4. **Sidebar rail.** Profile block to the foot of the rail (`margin-block-start: auto`), language switcher directly above it as a **dropup** — the same `<details>` menu with `inset-block-end: 100%` instead of `top: 100%`. Must work with JS off and collapse correctly below 800px, where the rail becomes a disclosure.

- [ ] TDD: a failing assertion per bug first (the clipping one is a browser drive — measure the menu's rect against the card's), then fix, then GREEN. Mutation-verify each. Commit `ui: four fixes from the live page — clipping, the menu rule, chevrons, the rail's foot`.

---

### Task 2: The column

**Files:** `ui/tokens.css`, whatever gates measure the preview scale.

`--rst-page`'s cap 52rem → 64rem. The desktop preview holds 1200px in a scaled frame; the scale factor and the measured numbers in comments and docs move with this. Re-measure rather than adjust by arithmetic.

- [ ] Change, re-measure every number that quotes the old one, GREEN, commit `ui: the page column is 64rem`.

---

### Task 3: The split

**Files:** `internal/designsystem/{designsystem,page,designsystem_test}.go`.

One page per section, each at its own URL under `<theme>/<locale>/`: `index.html` (Overview), `tokens.html`, `components.html`, `primitives.html`, `shells.html`, `icons.html`, `getting-started.html`. The sidebar nav is the same on every page, with the current page's section marked `aria-current` and its own items expanded.

Renames are reader-facing only: **Partials → Components**, **Class idioms → UI primitives**. `ui.Templates()` still returns partials and `templates.md` still says partials; the Components page says so once, in a sentence.

The marker-comment coverage gates (`<!-- partial: NAME -->`, `<!-- idiom: NAME -->`) now have to find their markers across pages rather than on one — the gate must assert the union, and must fail if a partial goes missing rather than silently landing on no page.

- [ ] TDD: gates first (union coverage, per-page nav, no orphan pages), then split, GREEN, commit `designsystem: one page per section`.

---

### Task 4: Overview and the everything demo

**Files:** `internal/designsystem/{page,samples,prose}.go`.

Overview opens with Paul's paragraph (spec §6-v2.1, verbatim — it is his copy, do not rewrite it), then the demo iframe, then a short route into each section.

The **everything demo** is one self-contained page: a dashboard, a list and a detail view as sections you click between, using the framework's own URL-per-view idiom with zero JavaScript. It is framed at the top of Overview and openable full-page in a new tab.

- [ ] Build the demo as its own page first and look at it, then frame it. Commit `designsystem: Overview, and one page that shows what an app looks like`.

---

### Task 5: Icons

**Files:** `internal/designsystem/page.go`, `prose.go`.

Every slug `rastrillo.IconSlugs()` returns, rendered at the size the components use it, with its name, its Lucide provenance, and the `{{icon "slug"}}` call to copy. Say what to do when an app needs an icon the framework does not ship: `ui.WithIcons` takes the app's own set — and note the trap already documented in `funcs.go:117`, that an app which scaffolds icons must pass `WithIcons` or silently revert to the built-in set. Link lucide.dev; mention Font Awesome as an alternative source, not a dependency.

- [ ] Derive from `IconSlugs()`, never a literal list — gate that a twelfth icon appears without touching the page. Commit `designsystem: the Icons page`.

---

### Task 6: Getting started

**Files:** `internal/designsystem/page.go`, `prose.go`.

How the CSS and JS are structured, what each file does, and **what each weighs — measured from the embedded bytes at render time, never typed into prose**, so the numbers cannot go stale. An independent download for someone not using rastrillo. A plain statement that new rastrillo apps get this by default, and how the theme is pinned.

- [ ] Gate the weights against the real `len()` of the embedded assets. Commit `designsystem: Getting started, with weights that cannot go stale`.

---

### Task 7: Translations and the a11y sweep

**Files:** `internal/designsystem/prose.go`, `a11y_test.go`.

Every new English string translated into the eleven other locales. The axe scan's page sample widens to cover the new page kinds (Overview, Icons, Getting started, the demo) in both schemes.

- [ ] Parity + leak gates GREEN across twelve locales; axe zero violations, zero exemptions. Commit `designsystem: the new pages speak twelve languages, and axe reads them all`.

---

### Task 8: Docs, SKILL.md, spec as-built

**Files:** `docs/site/templates.md`, `SKILL.md`, spec §6-v2.1.

The four fixes as rules where they belong; the new page map; the renames explained once. `SKILL.md` ≤ 18,000.

- [ ] All gates, `wc -c SKILL.md`, commit `Docs: v2.1 — the fixes, the pages, the names`.

---

### Task 9: Copy review + final review

- [ ] Every new user-facing string through the copy-review gate BEFORE the final review (Paul reviews; his text is applied verbatim; an edit costs eleven redrafted translations because the English is the key). Then the final whole-branch review on the most capable model available, then PR.

---

## Self-review

**Coverage:** §6-v2.1 bugs → T1; column → T2; split + renames → T3; Overview + demo → T4; Icons → T5; Getting started → T6; localisation and a11y → T7; docs → T8; copy → T9. Not in scope: the palette generator (§6-v2.2) and the markup migration (§6-v3).
**Order:** T1 and T2 are independent of T3–T6 and can land first; T7 depends on T4–T6 having produced their strings; T8 and T9 last.
**Risk:** T3 is the one that can go wrong quietly — a partial that lands on no page still satisfies a per-page gate. The union assertion is the guard, and it must be mutation-verified by deleting a section from the render and watching the gate fail.
