# Rastrillo components Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** rastrillo.org's gallery becomes "Rastrillo components" at `/components/`: the Go calls with their live results, linking to Fiesta for tokens, primitives and the stylesheet and script details, with every old `/design-system/` page address answering a one-hop 301.

**Architecture:** `pageKinds()` in `internal/designsystem/page.go` makes every page, rail entry, Overview route and prev/next step, but primitives' previews and search synonyms are built separately (`page.go` ~719 and ~789, `samples.go` ~149), so leaving takes all three. Fiesta links join the rail and Overview as off-site entries. Redirects come last, from the finished tree: `designsystem.Redirects(from, to string) ([]string, error)` makes `_redirects` lines in CARLOS's edge format, and `cmd/dsgen -redirect-from /design-system` writes them to `<out>/_redirects` for the website to publish at its root.

**Tech Stack:** Go 1.26, html/template via `tmpl`, chromedp browser tests (`-tags browser`), the gallery's prose tables (`prose.go`, `sampleprose.go`) for twelve locales.

**Spec:** `docs/superpowers/specs/2026-11-18-rastrillo-components-design.md` (approved 2026-11-18). Plan reviewed by Astra (22 findings, folded in here).

## Global Constraints

- Name: "Rastrillo components", everywhere the gallery names itself, including back-links ("Back to the design system" in shell, modal and demo pages). Address: `designsystem.DefaultMount = "/components"`.
- No visible change to what any component renders. Exception: Formats samples whose values a `ui` helper now produces show the helper's output for the page's locale (Task 3).
- Fiesta links: `https://fiesta.rastrillo.org/design-system/<theme>/<locale>/<page>`; destinations are checked against a pinned manifest of Fiesta's published pages for the Fiesta version in go.mod (Task 1), and live at the website sync (Task 5).
- `_redirects` format and limits are CARLOS's edge (`internal/edge/redirects.go` in the CARLOS repo; copy at `/home/paulca/.cache/carlos-console-release/worktree/internal/edge/redirects.go`): `source destination [status]`, `:name` one segment, final `*` as `:splat`, first match wins, a file at the path wins, characters it rejects listed there; ≤ 2,000 rules, ≤ 256 KiB, ≤ 2,048 bytes a line; the edge itself sends an existing `.../index.html` to its directory, so an Overview rule must target the directory. 301 throughout.
- Page-weight budgets are not raised; size and line records may move with a reason.
- Copy: each task drafts its English and translations; the controller runs Luna inside that task's review, and the fix round applies the approved English and retranslates. A task is not complete until its copy is approved. No em dashes as separators. Code reads like a sentence: spelled-out names (`err`, `ctx` fine), 80 columns, why-only comments.
- `tokensPageData` in `samples.go` is the API-token sample's data, not the Tokens page: it stays.
- The local, git-ignored render directory stays `.design-system/` (my call: a developer artefact; renaming it would make `rastrillo markup`'s skip list miss existing local copies).
- The redirect test's edge matcher implements the documented subset; the real parser gate is `carlos ship` (PackDir refuses a line it cannot use) at the website sync, where a refused ship is a failed sync, not a workaround.
- Hashed asset names from earlier deploys are not preserved (they never were across syncs; only old pages linked them, and those redirect). The plain aliases (`tokens.css`, `theme-<name>.css`, `shell.css`, and the rest `Render` writes unhashed) redirect to the new mount.
- Tests run as separate `go test` commands per package (a package after `-run` is a test-binary argument). Locally, targeted `-run` tests only; full suites on the fleet: `amadan ci status rastrillo/rastrillo -branch components`.

## Review Focus

1. An old bookmark, `/design-system/plain/ja/tokens.html`, reaches Fiesta's Japanese plain tokens page in one hop (Task 4).
2. An old Overview, `/design-system/`, `/design-system/day/en/` or `/design-system/day/en/index.html`, reaches `/components/day/en/` (a directory URL) in one hop (Task 4).
3. An old Portuguese address under the `/pt/` compatibility stubs dsgen writes reaches its final `pt-PT` destination in one hop (Task 4).
4. No page, rail, prev/next step, Overview route, search result or theme/locale switcher points at a page that no longer exists (Task 1).
5. No redirect rule's source matches a file of the new tree, and no destination is another rule's source (Task 4).

---

### Task 1: Tokens, UI primitives and the file details leave for Fiesta

**Files:**
- Create: `internal/designsystem/fiesta_links.go`, `internal/designsystem/fiesta_links_test.go`, `internal/designsystem/testdata/fiesta-pages.txt` (the manifest)
- Modify: `internal/designsystem/page.go` (`pageKinds`, `tokenNav`, `primitiveNav`, `assetNav`, the getting-started, tokens and primitives body templates, idiom construction ~719 and preview emission ~789, `galleryNav`, `pageRoutes`, `pageSteps`), `internal/designsystem/samples.go` (search synonyms ~149 for idiom anchors that leave), `prose.go`/`sampleprose.go` (keys only the leaving content used), `designsystem_test.go` (outbound-link allowlist ~562 gains exact Fiesta destinations beside Lucide)
- Retarget, not delete: `chrometarget_browser_test.go` (shared rail/tab/Copy target sizes onto a page that stays), `code_browser_test.go` (Form's clipboard), `header_rule_test.go` (headers across the tree), `galleryrig/rig.go`, `designsystem_test.go` ~4107 and ~4478, `index_browser_test.go` ~346 (search anchors). Delete only assertions about the leaving pages themselves; name each in the report.

**Interfaces:**
- Produces: `func FiestaPage(theme, locale, page string) string` in `fiesta_links.go` (panics on a theme, locale or page outside the manifest: every caller passes values from Rastrillo's own tables); the constants naming the Fiesta page that replaces tokens, primitives and the file details; `type offsite struct{ Title, Blurb, Page string }` rendered with `rel="external"` in the rail and the Overview's routes; `pageKinds()` without `tokens` and `primitives`; Getting started without the per-file rows.

- [ ] **Step 1: Pin Fiesta's pages.** From the Fiesta module version in go.mod (`go list -m -f '{{.Dir}}' amadan.net/rastrillo/fiesta`), render or read its gallery's page set (its `internal/gallery/pages.go` table and family names from `fiesta.Sources()`), and write `testdata/fiesta-pages.txt`: one `<theme>/<locale>/<page>` a line for the pages Rastrillo links. Record in `fiesta_links.go` which Fiesta page replaces tokens (`tokens.html`), primitives (name it) and the file details (name it). For each primitive, check whether a Rastrillo partial or template func draws it; if one does, that sample moves to its call's family instead of leaving (list the moves).
- [ ] **Step 2: Failing tests:**
  - `TestFiestaLinksNameAPublishedPage`: every `FiestaPage` call the render makes names a line of the manifest; the manifest's themes and locales equal `ui.ThemeNames()` and `rastrillo.BaseLocales()`.
  - `TestTheGalleryHasNoTokensOrPrimitivesPage`: no render key ends in `/tokens.html` or `/primitives.html`, and no preview frame belongs to a primitive that left (the one-frame-per-preview gate ~682 still passes).
  - `TestEveryLinkInTheTreeResolves` (extend the existing link test): over every page in every theme and locale, every `href` under the mount names a rendered file (rail, prev/next, Overview routes, search results, theme and locale switchers); off-site links are Lucide's (Icons) or a `FiestaPage` URL for the page's own theme and locale.
  - `TestTheOverviewAndRailLinkToFiesta`; `TestGettingStartedKeepsTheRastrilloHalf` (covers `rastrillo new`, `doctor`, upgrading; no per-file download or weight rows); search finds no leaving anchor on desktop or phone (`index_browser_test.go`).
- [ ] **Step 3: Run** `go test ./internal/designsystem -run 'FiestaLinks|NoTokens|LinkInTheTree|LinkToFiesta|GettingStarted|Search|Frame'` — FAIL.
- [ ] **Step 4: Implement**; draft English and translations for the off-site entries and changed Getting started prose; list every new or changed English string (before → after) in the report for copy review.
- [ ] **Step 5: Run** the Step 3 command; `go test ./internal/designsystem -run 'Prose|Translation|Weight|Budget|Page|Nav|Route|Header|Outbound'`; `go test -tags browser ./internal/designsystem -run 'Index|Search|Prefs|<the test functions in chrometarget_browser_test.go and code_browser_test.go, by name>' -count=1` (sandbox off); `make code`; `make budget` — PASS.
- [ ] **Step 6: Commit** `designsystem: tokens, primitives and file details link to Fiesta`.

### Task 2: The gallery is called Rastrillo components

**Files:**
- Modify: `internal/designsystem/page.go` (every "Rastrillo Design System" and self-referring "design system", including "Back to the design system" ~3145), `prose.go` (those keys and translations, ~865 among them), `designsystem.go` and `cmd/dsgen/main.go` comments, tests asserting the old name (`designsystem_test.go` ~4179–4193, `index_browser_test.go` ~75, ~255).

- [ ] **Step 1: Failing test** `TestTheGalleryCallsItselfRastrilloComponents`, per document kind: gallery pages' `docTitle` is `<page> · <Rastrillo components in the locale> · <theme>` (the theme suffix stays); demo and modal pages' titles and back-links use the new name; standalone preview documents still carry neither brand nor chrome; the top bar's brand and the Overview's `h1` say it; no prose key that names the gallery as a design system is still rendered, in any locale.
- [ ] **Step 2: Run** `go test ./internal/designsystem -run 'RastrilloComponents|DocTitle'` — FAIL.
- [ ] **Step 3: Implement**; draft English (name, Overview lead, back-links, any other changed string) and translations; list English before → after in the report.
- [ ] **Step 4: Run** Step 2's command; `go test ./internal/designsystem -run 'Prose|Translation|Title'`; `go test -tags browser ./internal/designsystem -run Index -count=1` — PASS.
- [ ] **Step 5: Commit** `designsystem: the gallery is Rastrillo components`.

### Task 3: Dates, numbers and money show their Go calls

**Files:**
- Modify: `internal/designsystem/formats.go`, its prose, `formats_test.go`, `format_browser_test.go` (its comparison ~61 reads `formatMarkup` instead of `d.Markup`, so locale-dependent samples are compared as rendered).

**Interfaces:**
- Consumes: the `ui` funcs for dates, numbers and money (read `ui/date.go`, `ui/number.go`, `ui/localefuncs.go` for names and arguments).

- [ ] **Step 1: Failing tests:** `TestEachFormatShowsItsHelper`: each Formats section whose value a `ui` helper produces shows the call beside the example (name the helper per section in the test), and the sample's value is the helper's output for the page's locale; `TestNoFormatClaimsRastrilloCannot`: the sentences saying Rastrillo cannot format a value a helper now formats are gone (assert their keys are absent); the browser comparison uses `formatMarkup`.
- [ ] **Step 2: Run** `go test ./internal/designsystem -run 'Format'` — FAIL.
- [ ] **Step 3: Implement**; sections with no helper (names, addresses) stay as guidance; samples keep their structure, with helper-driven values. English before → after in the report.
- [ ] **Step 4: Run** Step 2's command and `go test -tags browser ./internal/designsystem -run 'Format|TestTheHTMLTabRendersExactlyLikeThePreview' -count=1` — PASS.
- [ ] **Step 5: Commit** `designsystem: dates, numbers and money show their Go calls`.

### Task 4: The mount moves; old addresses redirect

**Files:**
- Modify: `internal/designsystem/designsystem.go` (`DefaultMount`, `CleanMount` rejects characters the edge's rule parser rejects), `cmd/dsgen/main.go` (`-redirect-from`, usage, header comment), every literal of the old mount meant to follow the default (search the substring `design-system` repository-wide, quoted or not, e.g. `browser_test.go` ~735; keep fixtures that deliberately use a custom mount, e.g. `flipevidence_test.go` ~57, and say which)
- Create: `internal/designsystem/redirects.go`, `redirects_test.go`, `testdata/published-design-system.txt` (the inventory of what rastrillo.org publishes today: every public path `dsgen -mount /design-system` writes at tag v0.35.0, the version the site is synced to, run from a detached worktree of that tag; it includes every HTML page, preview frame, the `/pt/` compatibility stubs and the unhashed asset aliases, and excludes dsgen's bookkeeping file `.dsgen` and hashed asset names; generated once and committed with the command in a header comment)

**Interfaces:**
- Consumes: `FiestaPage` and the replacement-page constants (Task 1).
- Produces: `const DefaultMount = "/components"`; `func Redirects(from, to string) ([]string, error)` (errors when `from` and `to` overlap or contain a character the edge rejects); dsgen `-redirect-from MOUNT` writes `Redirects(MOUNT, mount)` to `<out>/_redirects`, one rule a line.

- [ ] **Step 1: Failing tests** in `redirects_test.go`, using a test matcher that implements the edge subset in the Global Constraints (first match, `:name`, final `*`, a file at the path wins, `.../index.html` → directory):
  - `TestEveryPublishedAddressLandsInOneHop`: for every path in the inventory (old tree, `/pt/` stubs included), the old URL resolves in one hop to a file of `Render(DefaultMount)` (or its directory, for an index), or to a manifest Fiesta page for a page that left (a preview frame of a primitive that left goes to Fiesta's primitives page); the destination matches no rule (Review Focus 1–3, 5).
  - `TestNoRuleShadowsTheNewTree`: no source matches a path of the new tree.
  - `TestRedirectsRefuseOverlappingOrBadMounts`: `Redirects("/components", "/components")`, `("/a", "/a/b")`, and a mount with `?` or `#` error.
  - `TestTheRulesStayWithinTheEdgeLimits`: ≤ 2,000 rules, ≤ 256 KiB, every line ≤ 2,048 bytes and three fields ending `301`.
  - `TestDsgenWritesTheRedirects` (in `cmd/dsgen`): with `-redirect-from /design-system` the file equals `Redirects("/design-system", mount)` for the `-mount` given; without it, no `_redirects`.
- [ ] **Step 2: Run** `go test ./internal/designsystem -run 'OneHop|Shadows|BadMounts|EdgeLimits'` and `go test ./cmd/dsgen -run Redirect` — FAIL.
- [ ] **Step 3: Implement.** Rule order: `/pt/` stubs to their final `pt-PT` destinations (Overview directories included); then Overview directories and `index.html` (root and per theme/locale) to the new directory URLs; the leaving pages to Fiesta; then the catch-all `<from>/*  <to>/:splat  301`.
- [ ] **Step 4: Run** Step 2's commands, `go test ./internal/designsystem -run 'Mount|Render|Alias'`, `go test ./cmd/dsgen`, `go vet ./internal/designsystem ./cmd/dsgen` — PASS.
- [ ] **Step 5: Commit** `designsystem: mount at /components; redirect the old addresses`.

### Task 5: References, CHANGELOG, land, then the website sync

- [ ] **Step 1:** Every reference to the gallery follows the name and address: `docs/site/templates.md` ("The design system" section, dsgen command lines), `docs/site/nav.json` (if "Your app's design system" means the gallery; if it means an app's own design, only its links change), `docs/site/cli.md`, `docs/site/reference/ui.md`, `skill/design.md`, `skill/screens.md`, SKILL.md, README, PRODUCT.md, DESIGN.md, `gen.go`, `.gitignore`'s comment. Statements about Fiesta's design system stay. Regenerate generated files.
- [ ] **Step 2:** CHANGELOG Unreleased: the gallery is Rastrillo components at `/components/`; tokens, primitives and file details are on Fiesta; old addresses 301; `dsgen -redirect-from`; what the website sync does (below).
- [ ] **Step 3 (controller):** Luna on all new English; push; fleet CI green (merge main first if it moved); Astra whole-branch review; one fix wave; land with `-squash -expect -message`; `make mirror`; handoff.
- [ ] **Step 4 (controller, the next release's website `Sync:` PR, one change):** `hack/gen-design-system.mjs` writes `src/components` with `-mount /components -redirect-from /design-system`; `eleventy.config.js` passes through and cleans `components` instead of `design-system` (~54, ~66) and publishes the generated `_redirects` at the site root; `hack/check-docs.mjs` (~54, ~129) checks the new mount and page set; the homepage's links and labels (`src/index.njk` ~168, ~466–503 stylesheet links, ~578, ~797; `src/_includes/docs.njk` ~37) use `/components/` and the new name; the old published tree is not shipped; `npm run check` passes; after promote, from the flagship: each Review Focus 1–3 URL answers one 301 to a 200, and every Fiesta destination answers 200.
