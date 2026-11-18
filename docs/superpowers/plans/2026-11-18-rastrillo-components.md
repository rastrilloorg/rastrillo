# Rastrillo components Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** rastrillo.org's gallery becomes "Rastrillo components" at `/components/`: the Go calls with their live results, linking to Fiesta for tokens, primitives and the stylesheet and script details, with every old `/design-system/` address answering a one-hop 301.

**Architecture:** One table, `pageKinds()` in `internal/designsystem/page.go`, makes every page, rail entry, Overview route and prev/next step; pages leave by leaving it, and Fiesta links join the rail and Overview as entries that point off-site. `designsystem.Redirects(from string) []string` produces `_redirects` lines in CARLOS's edge format; `cmd/dsgen -redirect-from /design-system` writes them to `<out>/_redirects` for the website to place at its root.

**Tech Stack:** Go 1.26, html/template via `tmpl`, chromedp browser tests (`-tags browser`), the gallery's prose tables (`prose.go`, `sampleprose.go`) for twelve locales.

**Spec:** `docs/superpowers/specs/2026-11-18-rastrillo-components-design.md` (approved 2026-11-18).

## Global Constraints

- Name: "Rastrillo components", everywhere the gallery names itself. Address: `designsystem.DefaultMount = "/components"`.
- No visible change to what any component renders: previews, their markup and their scripts are untouched (except the Formats examples that claim a limitation that no longer exists, Task 4).
- Fiesta links: `https://fiesta.rastrillo.org/design-system/<theme>/<locale>/<page>.html`; Fiesta's locales equal `rastrillo.BaseLocales()` (`en ar bn es ga hi ja pt-PT ru vi yue zh-Hans`) and its themes equal `ui.ThemeNames()`; a test holds both equal so a link never names a missing locale or theme.
- `_redirects` format and limits are CARLOS's edge (`internal/edge/redirects.go`): `source destination [status]`, `:name` captures one segment, a final `*` captures the rest as `:splat`, first match wins, a file at the path wins over a rule; at most 2,000 rules, 256 KiB, 2,048 bytes a line. 301 throughout.
- Page-weight budgets are not raised; size and line records may move with a reason.
- Copy goes through Luna (controller); translations follow the approved English in each locale's existing register. No em dashes as separators. Code reads like a sentence: spelled-out names (`err`, `ctx` fine), 80 columns, why-only comments.
- The local, git-ignored render directory stays `.design-system/` (my call: a developer artefact; renaming it would make `rastrillo markup`'s skip list miss existing local copies).
- Locally, targeted `-run` tests only (internal/designsystem is long); full suites on the fleet: `amadan ci status rastrillo/rastrillo -branch components`.

## Review Focus

1. An old bookmark, `/design-system/plain/ja/tokens.html`, reaches Fiesta's Japanese plain tokens page in one hop (Task 1 test).
2. An old asset URL, hashed or plain (`/design-system/tokens.css`, `/design-system/assets/...`), reaches the same file under `/components/` in one hop, so a hotlinked stylesheet keeps working (Task 1 test).
3. No page, rail, prev/next step or Overview route points at a page that no longer exists (Task 2 test, over every theme and locale).
4. No redirect rule shadows a file of the new tree or leads to another rule's source (no second hop, no loop) (Task 1 test).
5. The theme and locale switchers on every page still land on a page that exists (Task 2 test).

---

### Task 1: The mount moves; old addresses redirect

**Files:**
- Modify: `internal/designsystem/designsystem.go` (`DefaultMount`, its comment), `cmd/dsgen/main.go` (flag `-redirect-from`, usage text, header comment)
- Create: `internal/designsystem/redirects.go`, `internal/designsystem/redirects_test.go`, `internal/designsystem/fiesta_links.go` (shared with Task 2)

**Interfaces:**
- Produces: `const DefaultMount = "/components"`; the leaving-page → Fiesta-page mapping in `fiesta_links.go`; `func FiestaPage(theme, locale, page string) string` (absolute Fiesta URL; `page` is a Fiesta file name such as `tokens.html`); `func Redirects(from string) ([]string, error)` returning rules from mount `from` to `DefaultMount` (pages that left go to Fiesta; everything else keeps its path under the new mount); dsgen `-redirect-from MOUNT` writes `Redirects(MOUNT)` joined by `\n` to `<out>/_redirects`.

- [ ] **Step 0: Read Fiesta's pinned gallery** (module `amadan.net/rastrillo/fiesta` at the version in `go.mod`, `internal/gallery`) and record in `fiesta_links.go` which Fiesta page replaces each leaving page: tokens → `tokens.html`; primitives → the page Fiesta renders for its primitives family (name it); stylesheet and script details → the Fiesta page listing its files (name it). Task 2 consumes this mapping.
- [ ] **Step 1: Failing tests** in `redirects_test.go`:
  - `TestTheOldTokensPageGoesToFiesta`: applying the rules (a small matcher in the test implementing the edge subset above: first match, `:name`, trailing `*`) to `/design-system/plain/ja/tokens.html` gives `https://fiesta.rastrillo.org/design-system/plain/ja/tokens.html` with 301; likewise every theme × locale for `tokens.html` and `primitives.html` (primitives' Fiesta page per Step 0's mapping).
  - `TestEveryOldAddressLandsOnANewFile`: for every key of `Render(DefaultMount)` (pages and assets, hashed and plain), the old address `"/design-system/"+key` resolves in one hop to `"/components/"+key`, and that destination is a key of the render or matches no rule (no second hop).
  - `TestTheRulesStayWithinTheEdgeLimits`: ≤ 2,000 rules, ≤ 256 KiB, every line ≤ 2,048 bytes, every line three fields ending `301`.
  - `TestFiestaSharesTheThemesAndLocales`: `ui.ThemeNames()` and `rastrillo.BaseLocales()` equal the lists `FiestaPage` accepts; `FiestaPage` with an unknown theme or locale panics or errors (choose one, document it).
  - dsgen: `TestDsgenWritesTheRedirects` (existing dsgen test style): `-redirect-from /design-system` writes `_redirects` equal to `Redirects("/design-system")`; without the flag no `_redirects` is written.
- [ ] **Step 2: Run** `go test ./internal/designsystem -run 'Redirect|OldAddress|EdgeLimits|FiestaShares|OldTokens' ./cmd/dsgen -run Redirect` — FAIL.
- [ ] **Step 3: Implement.** Rules are few and general: the Fiesta-bound pages first (`/design-system/:theme/:locale/tokens.html  https://fiesta.rastrillo.org/design-system/:theme/:locale/tokens.html  301`, same for primitives' mapping), then `/design-system/*  /components/:splat  301`. Update every test and doc that hard-codes `/design-system` as the default mount (grep `DefaultMount` and `"/design-system"`), but not the website-facing aliases' names.
- [ ] **Step 4: Run** the Step 2 command plus `go test ./internal/designsystem -run 'Mount|Render|Alias'` and `go vet ./internal/designsystem ./cmd/dsgen` — PASS.
- [ ] **Step 5: Commit** `designsystem: mount at /components; redirect the old addresses`.

### Task 2: Tokens, UI primitives and the file details leave for Fiesta

**Files:**
- Modify: `internal/designsystem/page.go` (`pageKinds`, `tokenNav`, `primitiveNav`, `assetNav`, the `getting-started` and `tokens` and `primitives` body templates, `galleryNav`, `pageRoutes`, `pageSteps`), `internal/designsystem/samples.go` (`tokensPageData` and the primitives samples, if only those pages use them), `internal/designsystem/prose.go` and `sampleprose.go` (drop keys only those pages used), `internal/designsystem/fiesta_links.go`
- Delete or narrow: tests that drive only the leaving pages (`designsystem_test.go`, `browser_test.go`, `a11y_test.go`, `header_rule_test.go`, `chrometarget_browser_test.go`, `code_browser_test.go`, `galleryrig/rig.go`), keeping every assertion that holds a page that stays.

**Interfaces:**
- Consumes: `FiestaPage(theme, locale, page string) string` (Task 1).
- Produces: `type offsite struct{ Title, Blurb, FiestaPage string }` (or equivalent) listed in the rail and the Overview's routes, rendered as links with `rel="external"`; `pageKinds()` without `tokens` and `primitives`; Getting started without the per-file rows.

- [ ] **Step 1: Check each primitive** (Task 1 recorded the Fiesta pages in `fiesta_links.go`). For each primitive, check whether a Rastrillo partial or template func draws it; if one does, that sample moves to the family its call belongs to instead of leaving (list the moves in the report).
- [ ] **Step 2: Failing tests:**
  - `TestTheGalleryHasNoTokensOrPrimitivesPage`: no key of `Render(DefaultMount)` ends in `/tokens.html` or `/primitives.html`.
  - `TestEveryLinkInTheTreeResolves` (extend the existing link test if there is one): over every page in every theme and locale, every `href` under the mount names a rendered file (rail, prev/next, Overview routes, theme and locale switchers), and every off-site link is a `FiestaPage` URL for that page's own theme and locale (Review Focus 3, 5).
  - `TestTheOverviewAndRailLinkToFiesta`: each page's rail and the Overview carry the Fiesta links for tokens, primitives and the stylesheet and script details, for their theme and locale.
  - `TestGettingStartedKeepsTheRastrilloHalf`: it still covers `rastrillo new`, `doctor` and upgrading; it has no per-file download or weight rows.
- [ ] **Step 3: Run** `go test ./internal/designsystem -run 'NoTokens|LinkInTheTree|LinkToFiesta|GettingStarted'` — FAIL.
- [ ] **Step 4: Implement**; remove code, prose keys and tests only the leaving pages used; keep the plain asset aliases.
- [ ] **Step 5: Run** the Step 3 command, `go test ./internal/designsystem -run 'Prose|Translation|Weight|Budget|Page|Nav|Route'`, the browser drives that touch navigation narrowly (`go test -tags browser ./internal/designsystem -run 'Index|Rail|Prefs' -count=1`, sandbox off), `make code`, `make budget` — PASS.
- [ ] **Step 6: Commit** `designsystem: tokens, primitives and file details link to Fiesta`.

### Task 3: The gallery is called Rastrillo components

**Files:**
- Modify: `internal/designsystem/page.go` (every `"Rastrillo Design System"` and self-referring "design system"), `internal/designsystem/prose.go` (the keys and their eleven translations), `internal/designsystem/designsystem.go` and `cmd/dsgen/main.go` comments, tests asserting the old title (`designsystem_test.go`, `index_browser_test.go`).

**Interfaces:**
- Produces: the prose key `Rastrillo components` and the Overview's new lead sentence (English drafted here, approved by the controller's copy review before translation).

- [ ] **Step 1: Failing test** `TestTheGalleryCallsItselfRastrilloComponents`: every page's `<title>` ends with the locale's translation of "Rastrillo components" (via `docTitle`), the top bar's brand and the Overview's `h1` say it, and no rendered page contains "Design System" or the locale's translation of "Rastrillo design system" as the gallery's own name (statements about Fiesta's design system are allowed: assert on the brand, title and `h1`, plus a search for the old key's translations).
- [ ] **Step 2: Run** `go test ./internal/designsystem -run 'RastrilloComponents|DocTitle'` — FAIL.
- [ ] **Step 3: Implement.** Draft the English (title, Overview lead, Getting started blurb, the Overview's sentence routing to Fiesta, any other changed gallery string) and write every new or changed English string, before → after, to the report for copy review. After the controller returns the approved English, translate it for every locale in `prose.go`.
- [ ] **Step 4: Run** the Step 2 command, `go test ./internal/designsystem -run 'Prose|Translation|Title'`, `go test -tags browser ./internal/designsystem -run Index -count=1` — PASS.
- [ ] **Step 5: Commit** `designsystem: the gallery is Rastrillo components`.

### Task 4: Dates, numbers and names show their Go calls

**Files:**
- Modify: `internal/designsystem/formats.go`, its prose in `prose.go`/`sampleprose.go`, `internal/designsystem/formats_test.go`

**Interfaces:**
- Consumes: the `ui` template funcs for dates, numbers and money (read `ui/date.go`, `ui/number.go`, `ui/localefuncs.go` for their names and arguments).

- [ ] **Step 1: Failing tests:** `TestEachFormatShowsItsHelper`: each Formats section whose value a `ui` helper produces shows that helper's call (name it per section in the test) beside its example, and the example's rendered value equals the helper's output for that locale; `TestNoFormatClaimsRastrilloCannot`: no Formats prose says Rastrillo cannot format a value a helper now formats (assert the old sentence keys are gone).
- [ ] **Step 2: Run** `go test ./internal/designsystem -run 'Format'` — FAIL.
- [ ] **Step 3: Implement**; sections with no helper (names, addresses) stay as guidance; previews keep their markup except where an example claimed the limitation. New or changed English to the report for copy review, then translations.
- [ ] **Step 4: Run** the Step 2 command and `go test -tags browser ./internal/designsystem -run Format -count=1` — PASS.
- [ ] **Step 5: Commit** `designsystem: dates, numbers and money show their Go calls`.

### Task 5: References, CHANGELOG, land

- [ ] **Step 1:** Every reference to the gallery follows the name and address: `docs/site/templates.md` ("The design system" section and the dsgen command lines), `docs/site/nav.json` ("Your app's design system", if it means the gallery; it may mean the app's own design, in which case only links change), `docs/site/cli.md`, `docs/site/reference/ui.md`, `skill/design.md`, `skill/screens.md`, SKILL.md, README, PRODUCT.md, DESIGN.md, `gen.go`, `.gitignore`'s comment. Statements about Fiesta's design system stay. Regenerate generated files.
- [ ] **Step 2:** CHANGELOG Unreleased: the gallery is Rastrillo components at `/components/`; tokens, primitives and file details are on Fiesta; old addresses 301; `dsgen -redirect-from`; what the website sync must do (mount, `_redirects` at its root, homepage stylesheet links, its top-bar label and docs link).
- [ ] **Step 3 (controller):** Luna on all new English; push; fleet CI green (merge main first if it moved); Astra whole-branch review; one fix wave; land with `-squash -expect -message`; `make mirror`; handoff.
- [ ] **Step 4 (controller, at the next Rastrillo release):** the rastrillo-website `Sync:` PR switches `-mount /components -redirect-from /design-system`, places the `_redirects` at the site root, updates the homepage's links and labels, and is checked live from the flagship (old addresses 301 once, new ones 200).
