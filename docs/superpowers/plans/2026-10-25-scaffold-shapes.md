# Scaffold Shapes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `rastrillo new --scaffold bare|single|accounts`, with `single` the default, `home` and `hub` reserved, tests beside the code, the migration check as a test, and the perf lane working on a stdlib mux.

**Architecture:** `runNew` resolves every flag into one `newOpts`, looks the shape up in a small registry (`shapes.go`), and calls that shape's writer. `single` is today's scaffold moved onto an `http.ServeMux` with a route table; `accounts` is `single` with `skill/add-signin.md` applied, on chi; `bare` is new, written from embedded `.tmpl` files under `cmd/rastrillo/scaffold/bare/`. `perftest.ScreenConfig` gains a `Patterns` field so a stdlib app has an inventory without chi.

**Tech Stack:** Go 1.26 toolchain (scaffolded `go 1.24`), `net/http` ServeMux patterns, chi v5.3.2 (accounts only), GORM v1.31.2, `migrate/modeldiff`.

**Spec:** `docs/superpowers/specs/2026-10-25-scaffold-shapes-design.md`

## Global Constraints

- One flag, `--scaffold`; values `bare`, `single` (default), `accounts`; `home` and `hub` refused as reserved.
- `--design-system` boolean, default true, accepted as false with `bare` only (spec §4.1, amended).
- A flag that would have no effect is an error, raised before anything is written: the target directory must not exist after a refusal.
- `bare`'s `go.mod` requires Rastrillo only: no chi, no GORM. `single`'s has no direct chi require. `accounts` requires chi.
- Every shape × design-system setting scaffolds, passes `go mod tidy`, `go test ./...` and `go vet -tags browser,perf ./...` against the checkout (`scaffoldWithReplace`). Icon/theme/shell combinations stay tested on `single` only.
- Tests live in `internal/<pkg>/` as `package <pkg>_test`. `internal/<pkg>test/` is no longer written. Existing apps that have one keep working with every CLI command.
- The platform contract is unchanged: `rastrillo.Run`/`Resolve`/`Serve`, the flags, `serve`, `sidecar run`.
- The framework gate is `make ci`; locally it needs `GOFLAGS=-mod=mod` (the Makefile sets it). A new suite needs a `SUITE` role and a bumped `.amadan/ci.d/50-suite@N`.
- Every user-facing string (CLI messages, scaffolded README text, SKILL.md, docs/site) goes through the copy-review skill before it is written into a file. Drafts below are drafts.
- SKILL.md stays under `skillCoreBudget` (8,000 bytes; 7,153 now); topics under 9,000.
- Comments say why, naming the failure prevented. Commits: imperative subject, body says why.
- Do not touch `examples/` (manifest retirement is a separate project) or the GORM advice (branch `gorm-measured-cost` owns it).
- `cmd/rastrillo` has no source-size headroom: `.rastrillo/budgets.txt` holds it at 5,667, its exact count, and the counter includes Go template constants (`internal/budgetsize/size.go:27`). New scaffold templates go in `cmd/rastrillo/scaffold/` as files, never as Go constants. Any task whose Go code still raises the count moves existing constants out to files in the same commit, or raises the ceiling there with the reason appended to that line, as every previous raise did.
- Every task's commit leaves `go test ./cmd/rastrillo/ ./perf/...` green: an existing assertion a task invalidates is rewritten in that task, not later.

## Review Focus

1. **A stdlib catch-all.** `GET /` on a ServeMux matches every path, so an unknown URL would render the index. Expect 404. Pinned in Task 4 (`single`) and Task 5 (`bare`).
2. **App names that make awkward packages.** A hyphenated name (`my-app`) or a keyword name (`func`) must give a working `package <pkg>_test` beside the code. Pinned in Task 3.
3. **`rastrillo vectors init` in either layout.** An app scaffolded before this change has `internal/<pkg>test/`; a new one does not. Both must get working parity tests. Pinned in Task 3.
4. **Existing chi apps' perf tests.** `ScreenConfig{Routes: r}` must compile and behave unchanged; setting both `Routes` and `Patterns`, or neither, must fail with a message. Pinned in Task 1.
5. **Flags that do nothing.** `--theme` with `--design-system=false`, `--ux` with `bare`, `--design-system=false` with `single`: each refused, and no directory left behind. Pinned in Tasks 2, 5 and 6.

---

### Task 0: Copy review of the CLI strings

**Files:** none changed; produces the approved strings Tasks 2, 5, 6 and 7 use verbatim.

- [ ] **Step 1: Run the copy-review skill on these drafts**

```
flag help --scaffold: what to scaffold: bare (a page, no database), single (the default: one app and its SQLite data) or accounts (people sign up and sign in)
flag help --design-system: write the design system: templates, styles, scripts and icons (bare only can turn it off)
unknown shape: unknown --scaffold %q: choose bare, single or accounts
reserved shape: --scaffold %s is not built yet: it is %s. Use bare, single or accounts for now
  %s for home: "Plan 6 of the user-models roadmap"; for hub: "Plan 7 of the user-models roadmap"
design system off with single/accounts: --design-system=false works with --scaffold bare only: single and accounts build their pages from the design system
design flag with it off: --%s does nothing with --design-system=false: leave it out
--ux with bare: --ux does nothing with --scaffold bare, which writes no AGENTS.md: leave it out
bare's printed summary, first line: rastrillo new: scaffolded %s/ (bare: a page and a route)
bare README: see Task 5 Step 3
```

- [ ] **Step 2: Record the approved strings in the branch discussion** (`amadan discuss new rastrillo/rastrillo -branch scaffold-shapes -title "Approved CLI copy" -body -`), so later tasks copy them from one place.

### Task 1: `perftest` inventory without chi

**Files:**
- Modify: `perf/perftest/screens.go:24-45` (ScreenConfig), `:177-189` (the walk)
- Test: `perf/perftest/screens_test.go`
- Modify: `docs/site/reference/perftest.md`, `CHANGELOG.md`

**Interfaces:**
- Produces: `ScreenConfig.Patterns []string`: the app's GET routes as ServeMux path patterns without the method (`"/{$}"`, `"/notes/{id}"`). Exactly one of `Routes` and `Patterns` is set. A trailing `{$}` is dropped from both the screen name and the requested path, so `"/{$}"` is screen `GET /`, requested at `/`, the same name a chi app's root gets.

- [ ] **Step 1: Write the failing tests** in `screens_test.go`, using the file's existing fake-handler helpers:

```go
func TestScreensMeasuresStdlibPatterns(t *testing.T)      // Patterns {"/{$}", "/about"}: both measured, keys "GET /" and "GET /about"
func TestScreensRefusesBothRoutesAndPatterns(t *testing.T) // problem text contains "set Routes or Patterns, not both"
func TestScreensRefusesNeitherRoutesNorPatterns(t *testing.T) // problem text contains "set Routes (a chi router) or Patterns (a stdlib mux's GET routes)"
func TestScreensPatternParamNeedsPaths(t *testing.T)      // "/notes/{id}" with no Paths entry: the existing "has a parameter and no Paths entry" problem
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./perf/perftest/ -run 'TestScreens(MeasuresStdlib|Refuses|PatternParam)' -count=1`
Expected: FAIL (unknown field `Patterns`).

- [ ] **Step 3: Implement.** Add the field with a doc comment saying why it exists (a ServeMux cannot list its routes, so the app keeps one table and hands it here). Build `routes` from `Patterns` when set, else from `chi.Walk` as now; normalize `{$}` in one place, before screen names are made. The two refusals join `problems` like the existing ones. Keep `Routes chi.Routes` as is: existing apps change nothing.

- [ ] **Step 4: Run the package**

Run: `go test ./perf/perftest/ -count=1`
Expected: PASS, including every existing test.

- [ ] **Step 5: Docs and changelog.** `reference/perftest.md` gains the `Patterns` field and the `{$}` rule; CHANGELOG gets one line under the unreleased heading. Commit: `perftest: measure a stdlib mux's routes through Patterns`.

### Task 2: The `--scaffold` flag, the shape registry, and `single` as the default

**Files:**
- Create: `cmd/rastrillo/shapes.go`, `cmd/rastrillo/shapes_test.go`
- Modify: `cmd/rastrillo/new.go:67-362` (runNew)

**Interfaces:**
- Produces:
  - `type newOpts struct { Name, Pkg, Locale, IconSet, IconDelivery, UX, Theme, Shell string; DesignSystem bool; Set map[string]bool }`: `Set` records which flags the user passed (`fset.Visit`), for the "does nothing" refusals.
  - `type shape struct { Name, Summary, Reserved string; Write func(newOpts) error }`
  - `var shapes []shape` in menu order: bare, single, accounts, home, hub.
  - `func lookupShape(name string) (shape, error)`: the unknown and reserved errors, Task 0's copy.
  - `func writeSingle(o newOpts) error`: today's body of runNew from the `dirs` slice to the printed summary, unchanged in output.
  - `bare` and `accounts` entries exist with `Write` returning `errors.New("not built yet")` until Tasks 5 and 7.

- [ ] **Step 1: Write the failing tests** in `shapes_test.go`:

```go
func TestNewRefusesReservedShapes(t *testing.T)  // home and hub: error equals Task 0's reserved text; dir "x" does not exist after
func TestNewRefusesUnknownShape(t *testing.T)    // --scaffold=tiny: Task 0's unknown text; no dir
func TestDefaultShapeIsSingle(t *testing.T)      // runNew(["a"]) and runNew(["--scaffold","single","b"]) write the same relative file set with equal contents, the app name substituted
func TestUsageListsTheScaffoldFlag(t *testing.T) // runNew([]) error contains "--scaffold=bare|single|accounts"
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/rastrillo/ -run 'TestNewRefuses(Reserved|Unknown)Shape|TestDefaultShapeIsSingle|TestUsageListsTheScaffoldFlag' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement.** runNew parses flags, fills `newOpts`, resolves the shape, runs every check that can fail (icons, theme, shell, locale, the shape's own refusals) before creating the directory, then calls `Write`. Move the body into `writeSingle` without changing what it writes. This task adds Go lines to a package at its size ceiling: in this commit, move `readmeTemplate` and `agentsMDTemplate` out of `new.go` into `scaffold/README.md.tmpl` and `scaffold/AGENTS.md.tmpl` (read with `scaffoldFile`), and check `rastrillo budget size cmd/rastrillo` is at or under 5,667.

- [ ] **Step 4: Run the whole command package**

Run: `go test ./cmd/rastrillo/ -count=1`
Expected: PASS. Every existing scaffold test passes unchanged, which is the proof `writeSingle` moved without changing.

- [ ] **Step 5: Commit** `new: a --scaffold flag, with today's scaffold as single`.

### Task 3: Tests beside the code

**Files:**
- Modify: `cmd/rastrillo/new.go` (harness, vendored, index, browser templates; `dirs`; the files map; printed summary), `cmd/rastrillo/new_budget.go` (perf template), `cmd/rastrillo/scaffold/api_test.go.tmpl`, `cmd/rastrillo/vectorsinit.go:29-32`, `cmd/rastrillo/scaffold/AGENTS.md.tmpl` (its `go test -short ./internal/<app>/... ./internal/<app>test/` line, `new.go:1604` before Task 2 moved it, becomes `go test -short ./...`), `cmd/rastrillo/scaffold/README.md.tmpl` (the browser test's location, `new.go:1272` before the move)
- Modify: every `cmd/rastrillo/*_test.go` that reads `internal/<pkg>test/...`
- Modify: `docs/site/testing.md`, `skill/add-signin.md` (step 3's path), `skill/screens.md`

**Interfaces:**
- Produces: test files at `internal/<pkg>/{harness,vendored,index,browser,perf,api}_test.go`, `package <pkg>_test`, importing the app as `<pkg> "<module>/internal/<pkg>"`. Helper names (`newApp`, `get`, `post`, `schema`, `testOrigin`) are unchanged.
- Produces: `func testDir(appDir, pkg string) (dir, pkgClause string)` in `vectorsinit.go`: `internal/<pkg>test` and `<pkg>test` if that directory exists, else `internal/<pkg>` and `<pkg>_test`.

- [ ] **Step 1: Write the failing tests**

```go
func TestNewPutsTestsBesideTheCode(t *testing.T)            // no internal/<pkg>test; internal/<pkg>/harness_test.go starts "package <pkg>_test"
func TestHyphenAndKeywordAppsPassTheirOwnTests(t *testing.T) // "my-app" and "func": scaffoldWithReplace, go test ./... passes
func TestVectorsInitUsesTheAppsTestLayout(t *testing.T)      // new layout: parity_test.go in internal/<pkg>, package <pkg>_test; an app with internal/<pkg>test: written there, package <pkg>test; both pass go vet
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/rastrillo/ -run 'TestNewPutsTestsBesideTheCode|TestHyphenAndKeyword|TestVectorsInitUsesTheAppsTestLayout' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement.** Change the package clauses and paths. The vendored test reads `static/` relative to its own directory now, not `../<pkg>/static`; check every relative path in the moved templates, including the parity shim's working-directory fix (both layouts sit two levels below the module root, so it is unchanged, which the test confirms).

- [ ] **Step 4: Run the package and the scaffold build**

Run: `go test ./cmd/rastrillo/ -count=1`
Expected: PASS, including `TestScaffoldedAppTestsPass`.

- [ ] **Step 5: Docs, then commit** `new: write an app's tests beside its code`. The docs say `internal/<app>/*_test.go`; skill files stay within budget (`go test -run TestSkillStaysWithinBudget .`).

### Task 4: `single` on a stdlib mux, and the migration check as a test

**Files:**
- Modify: `cmd/rastrillo/new.go`: `goModTemplate` (chi require only for accounts: take the require block as an argument), `appTemplate`, `handlersTemplate`, `overviewHandler`, `makefileTemplate` (drop `migration-check` from the target list and `ci`), the `ciScripts` map (drop `40-migration-check`), `agentsMDTemplate` (the app.go line and the `Mount(r chi.Router, deps)` advice become `Mount(mux *http.ServeMux, deps)`), the printed summary's Makefile line
- Modify: `cmd/rastrillo/new_budget.go` (perf template uses `Patterns`)
- Create: `cmd/rastrillo/scaffold/migrations_test.go.tmpl`
- Modify: `cmd/rastrillo/scaffold/tokens.go.tmpl` and `api_test.go.tmpl`, which import chi today (`tokens.go.tmpl:28` returns `func(chi.Router)`; `api_test.go.tmpl:80` builds a chi router), so `go mod tidy` would put chi straight back: `single`'s copies use the stdlib mux (below); `accounts` keeps today's chi versions in Task 7
- Modify, so the package stays green in this commit: `new_test.go:185` (`TestNewPinsCLIsOwnVersions` expects a direct chi require), `new_test.go:282` (expects the `chi.Router` signature), `new_scaffold_test.go:63` (the CI step list), `new_scaffold_test.go:388` (`TestScaffoldMigratesAndPassesCheck` runs `make migration-check`; it runs `go test -run TestModelsMatchMigrations ./internal/<pkg>/` instead), `TestNewScaffoldsMakefileMigrationCheck` (becomes `TestNewScaffoldsMigrationCheckTest`), `TestScaffoldWholeGatePassesOffline` (loses the step)

**Interfaces:**
- Consumes: `perftest.ScreenConfig.Patterns` (Task 1); `newOpts`, `writeSingle` (Task 2).
- Produces, in the scaffolded `internal/<pkg>/app.go`:
  - `type route struct { Pattern string; Handler http.HandlerFunc }`: `Pattern` is a full ServeMux pattern, `"GET /{$}"`.
  - `func (a *app) routes() []route`: the one route table. Index is `"GET /{$}"`; the two-page shells add `"GET /overview"`.
  - `func Router(d *db.DB, origin string, logger *slog.Logger) (http.Handler, error)`: migrates as now, registers `routes()` on a new ServeMux, returns `csrf.Protect(origin)(mux)`.
  - `func GETPatterns() []string`: the GET entries of `(&app{}).routes()` with `"GET "` cut, for the perf test.
  - `App`, `Mux`, `Configure` keep their signatures.
- Produces, in the scaffolded `internal/<pkg>/tokens.go` (single): `func Tokens(d *db.DB, sess *sessions.Sessions) (func(mux *http.ServeMux), error)`, registering every route today's chi version does (`tokens.go.tmpl:39-47`) with the same guards: `GET /tokens`, `POST /tokens/revoke`, `POST /tokens/revoke-agent` and `POST /tokens/revoke-all` behind `sess.Require`, and `POST /tokens` behind `sess.RequireFresh(10*time.Minute)` (which includes Require, `sessions/sessions.go:417`). The `api-tokens` partial posts to all four (`ui/partials/api-tokens.html:39`), so a missing one is a 404 button; `api_test.go` asserts each answers something other than 404 when signed out. Its doc comment's wiring example becomes `tokens(mux)` before the CSRF wrap. `api_test.go`'s router becomes a ServeMux.
- Produces, `internal/<pkg>/migrations_test.go`: `TestModelsMatchMigrations` calls `modeldiff.Generate(context.Background(), <pkg>.Schema.All(), <pkg>.Models)` and fails on any change with exactly `models and migrations disagree:\n  <sql per change>\n\nrun: rastrillo migration generate`, the CLI's text (`cmd/rastrillo/migration.go`).

- [ ] **Step 1: Write the failing tests**

```go
func TestSingleUsesTheStdlibMux(t *testing.T)          // no scaffolded .go file imports go-chi; after go mod tidy, go.mod has no direct chi require; app.go contains `"GET /{$}"`
func TestSingleAnswers404ForUnknownPaths(t *testing.T) // scaffoldWithReplace; a generated test file asserting GET /nope is 404 passes (add it to index_test template: TestUnknownPathIs404)
func TestNewScaffoldsMigrationCheckTest(t *testing.T)   // Makefile has no migration-check; .amadan/ci.d has no 40-migration-check; migrations_test.go exists
func TestMigrationCheckTestCatchesDrift(t *testing.T)   // scaffold, add a field to Note in models.go, go test ./internal/<pkg>/ -run TestModelsMatchMigrations fails with "models and migrations disagree"; run rastrillo migration generate (the CLI's runMigration in-process), passes
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/rastrillo/ -run 'TestSingle|TestNewScaffoldsMigrationCheckTest|TestMigrationCheckTestCatchesDrift' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** the templates above. The perf template passes `Patterns: <pkg>.GETPatterns()` and `Handler: h`. Comments in `app.go` say why the table exists (a ServeMux cannot list its routes, so the perf lane would miss a route registered anywhere else) and why the index is `{$}` (a bare `/` catches every path).

- [ ] **Step 4: Run the package**

Run: `go test ./cmd/rastrillo/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit** `new: single serves from a stdlib mux and checks its migrations in go test`.

### Task 5: The `bare` shape

**Files:**
- Create: `cmd/rastrillo/scaffold/bare/{main.go,app.go,locale.go,render.go,app_test.go,README.md,gitignore}.tmpl`, `cmd/rastrillo/newbare.go`, `cmd/rastrillo/newbare_test.go`
- Modify: `cmd/rastrillo/shapes.go` (bare's `Write`), `cmd/rastrillo/dev.go:226` (rebuild calls `runGenerate` unconditionally, which would write `gen/` into a bare app: run it when the app has any generation input or output, that is `manifest/`, `actions/` or `gen/`, and skip it only when it has none; `generate_test.go:55-65` supports `actions/` without manifests, `generate.go:288-290` copies them)

**Interfaces:**
- Consumes: `newOpts`, the existing vendored-asset, layout, icon-set and errors-template code paths `writeSingle` uses (extract shared helpers rather than copy them).
- Produces: `func writeBare(o newOpts) error`. The files:
  - `go.mod`: `amadan.net/rastrillo/rastrillo` only, no `tool` line (bare has no gate that runs the CLI).
  - `cmd/<name>/main.go`: `rastrillo.Run(rastrillo.Options{Mux: <pkg>.Mux(), Logger: logger, ErrorPage: <pkg>.ErrorPage, DefaultLocale: <pkg>.Locale})`. No `gen/`, no `manifest/`.
  - `internal/<pkg>/locale.go`: `const Locale = "<code>"` and a blank import of each pack `locales.PacksFor([]string{code})` names (`locales/config.go:16`; `pt-PT` needs `pt`, `zh-Hans` needs `zh`). It lives in the app package, not `main`, because `newPages` runs `CheckConfig` when the package loads, so the app's own tests need the pack linked too.
  - `internal/<pkg>/app.go`: the `route` table as in Task 4 with `"GET /{$}"`; `func Mux() *http.ServeMux` registering `GET /static/` and, at `/`, the routes' own ServeMux wrapped in `pages.Middleware`, which puts the localizer `render` reads into the request (`ui/pages.go:245` refuses a nil one). When `pagesErr` is set, `/` answers `ErrorPage`'s plain 500, as `single`'s `Mux` does (`new.go:599-606`).
  - `internal/<pkg>/render.go`: today's `renderTemplate`, with `locales.CheckConfig(Locale, nil)` in place of `genlocales`.
  - templates (`layout.html`, `index.html`, `errors.html`), `static/`, `icons/`: as `single` writes them.
  - `internal/<pkg>/app_test.go`, `package <pkg>_test`: `TestIndexRenders` (200, links the fingerprinted stylesheet) and `TestUnknownPathIs404`.
  - `README.md`, `.gitignore`, and the icon set's attribution file when it has one.

- [ ] **Step 1: Write the failing tests** in `newbare_test.go`:

```go
func TestBareWritesOnlyItsFiles(t *testing.T)  // exact relative file set for the default flags (list it in the test); nothing under migrations/, manifest/, gen/, .amadan/, .rastrillo/; no Makefile, AGENTS.md, CLAUDE.md
func TestBareGoModRequiresOnlyRastrillo(t *testing.T) // after go mod tidy: no go-chi, no gorm.io in go.mod
func TestBarePassesItsOwnTests(t *testing.T)   // scaffoldWithReplace(t, "dash", "--scaffold", "bare"); go test ./... and go vet ./... pass
func TestBareWithLocale(t *testing.T)          // table: es, pt-PT, zh-Hans: locale.go imports the pack PacksFor names, Locale is the code, the app's tests pass
func TestBareRefusesUX(t *testing.T)           // --ux standard: Task 0's text; no dir
func TestDevLeavesABareAppBare(t *testing.T)   // one rebuild of dev's loop in a bare app writes no gen/; in a single app, and in an app with actions/ and no manifest/, it still runs generate
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/rastrillo/ -run TestBare -count=1`
Expected: FAIL ("not built yet").

- [ ] **Step 3: Implement**, then the README. Draft for the copy review (plain, short, says what to do):

```
# <name>

A small Rastrillo app: one page, no database, no sign-in.

    go run ./cmd/<name> -addr 127.0.0.1:8080
    go test ./...

Add a page: a route in internal/<pkg>/app.go, a template in
internal/<pkg>/templates/. When the app needs to store data, start
again with `rastrillo new --scaffold single` and move your pages across.
```

- [ ] **Step 4: Run the package**

Run: `go test ./cmd/rastrillo/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit** `new: a bare shape, a page and a route with no database`.

### Task 6: `--design-system=false` for `bare`

**Files:**
- Create: `cmd/rastrillo/scaffold/bare/app_plain.go.tmpl`, `app_plain_test.go.tmpl`
- Modify: `cmd/rastrillo/newbare.go`, `cmd/rastrillo/shapes.go` (the refusals), `cmd/rastrillo/newbare_test.go`, `cmd/rastrillo/doctor.go:380` (`findStaticDir` fails when nothing is vendored, `:488`, which aborts even `doctor --probe`)

**Interfaces:**
- Produces: with the design system off, bare writes no `render.go`, templates, `static/`, `icons/` or attribution. `app.go`'s index handler writes a short HTML page (`<!doctype html>`, `<html lang="<code>">`, a title, an `<h1>`) with `Content-Type: text/html; charset=utf-8`. `main.go` sets no `ErrorPage`.
- Produces: doctor treats an app with no vendored static files and no `templates/layout.html` under `internal/*/` as one without a design system: it skips the vendored comparison, says so in one line, and runs every other check. An app with templates but no static files is still the error it is today.

- [ ] **Step 1: Write the failing tests**

```go
func TestBarePlainWritesNoDesignSystem(t *testing.T)    // --design-system=false: no templates/, static/, icons/, render.go
func TestBarePlainPassesItsOwnTests(t *testing.T)       // go test ./... passes: index 200 with the h1, unknown path 404
func TestDesignFlagsRefusedWithoutDesignSystem(t *testing.T) // each of --icons, --icon-delivery, --theme, --shell with --design-system=false: Task 0's text naming the flag; no dir
func TestDesignSystemOffRefusedForSingleAndAccounts(t *testing.T) // Task 0's text; no dir
func TestDoctorChecksAPlainBareApp(t *testing.T)  // doctor and doctor --probe on a plain bare app succeed and say the design system is absent
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/rastrillo/ -run 'TestBarePlain|TestDesignFlagsRefused|TestDesignSystemOffRefused' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement.** The refusals use `newOpts.Set`, so a flag the user typed counts even when it equals its default.

- [ ] **Step 4: Run the package.** Run: `go test ./cmd/rastrillo/ -count=1`. Expected: PASS.

- [ ] **Step 5: Commit** `new: bare can leave the design system out`.

### Task 7: The `accounts` shape

**Files:**
- Create: `cmd/rastrillo/newaccounts.go`, `cmd/rastrillo/newaccounts_test.go`, `cmd/rastrillo/scaffold/accounts/{app.go,auth.go,models.go,signin.html,signup.html,signin_test.go,0001_init.sql,schema.sql}.tmpl`
- Modify: `cmd/rastrillo/shapes.go`, `cmd/rastrillo/new_scaffold_test.go` (`TestScaffoldMigratesAndPassesCheck` runs for single and accounts)

**Interfaces:**
- Consumes: `writeSingle` (Task 2) and Task 4's templates; `skill/add-signin.md` steps 1 to 8, which are the specification of this shape's code.
- Produces: `func writeAccounts(o newOpts) error`: runs `writeSingle`, then overwrites and adds files so the result is a fresh `single` with add-signin applied:
  - `go.mod` with the chi require.
  - `app.go`: `Router(d *db.DB, origin string, bg *background.Group, logger *slog.Logger) (chi.Router, error)` as add-signin step 4; `App` takes `bg` in the same place.
  - `models.go`: `User` beside `Note`, and `Note` gains `UserID int64 \`gorm:"index"\`` as in `examples/notes/internal/notes/models.go:23`; `0001_init.sql` and `schema.sql` are the complete baseline: produced by running `rastrillo migration generate` once on an accounts scaffold whose `migrations/` holds no `.sql` files yet, so the generator writes every table from nothing (run against a `single` scaffold it would write only the delta after `single`'s `0001`, `migration.go:239-248`), then pasted in as `initMigrationTemplate` was.
  - Owned routes in the signed-in group, every query through `scope.Owned(g, uid)` (`scope/scope.go:19`), the uid from the session: `GET /notes` lists the viewer's notes, `POST /notes` creates one owned by the viewer, `GET /notes/{id}` shows one or answers 404 when it is not the viewer's. These replace add-signin's placeholder `/account`, with a `templates/notes.html` page.
  - `tokens.go`: today's chi version, wired in `Router` inside the signed-in group (its doc comment's recipe, `tokens.go.tmpl:15-27`), since this shape has the session it needs.
  - `migrations.go`: `BootSchema = migrate.Merge(sessions.Schema, pow.Schema, apitoken.Schema, Schema)`.
  - `auth.go` (add-signin step 5), `templates/signin.html` and `signup.html` (step 6), the layout's sign-out form, `render.go`'s `SignedIn`.
  - `main.go`: `bg := &background.Group{}`, `opts.Background = bg`; `<APP>_INSTANCE_KEY` read as step 4 shows.
  - `.rastrillo/budgets.txt` with step 7's line. The perf test uses `Routes: r` (chi), step 8's `Signin`, and a `Paths` entry for `/notes/{id}` whose note it seeds.
  - `signin_test.go`: `TestSignupThenNotes` (step 8's flow; then GET /notes is 200), `TestNotesNeedSignin` (signed out, GET /notes is 303 with a `Location` starting `/signin`), and `TestNotesAreOwned` (two people sign up; A creates a note; B's `GET /notes` does not list it and B's `GET /notes/<A's id>` is 404).

- [ ] **Step 1: Write the failing tests** in `newaccounts_test.go`:

```go
func TestAccountsPassesItsOwnTests(t *testing.T) // scaffoldWithReplace(t, "club", "--scaffold", "accounts"); go test ./... and go vet -tags browser,perf ./... pass
func TestAccountsRequiresChi(t *testing.T)       // go.mod has the chi require at chiPinnedVersion
func TestAccountsMigratesAndPassesCheck(t *testing.T) // via the parametrized TestScaffoldMigratesAndPassesCheck
```

- [ ] **Step 2: Run them to see them fail.** Run: `go test ./cmd/rastrillo/ -run 'TestAccounts|TestScaffoldMigratesAndPassesCheck' -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement.** Where add-signin and this shape disagree, the shape's tests decide and Task 8 fixes the recipe.

- [ ] **Step 4: Run the package and the perf lane of a scaffold.** Run: `go test ./cmd/rastrillo/ -count=1`, then in a `scaffoldWithReplace` accounts app, `go test -tags perf ./...`. Expected: PASS, `/notes` and `/notes/{id}` measured.

- [ ] **Step 5: Commit** `new: an accounts shape, single with email and password sign-in`.

### Task 8: The skill and the docs

**Files:**
- Modify: `SKILL.md` (frontmatter description, "Start here" → "Pick a shape"), `skill/add-signin.md`, `skill/screens.md`, `skill/tooling.md` if it lists `new`'s flags
- Modify: `docs/site/getting-started.md`, `cli.md`, `app-shape.md`, `testing.md`, `compare.md`, `README.md` (the `rastrillo new` bullet), `CHANGELOG.md`

- [ ] **Step 1: Draft and copy-review** the SKILL.md section. Draft:

```
## Pick a shape

Choose by who the users are and where they live:

| Shape | For | Command |
|---|---|---|
| bare | a page or a dashboard: no data of its own, no sign-in | `rastrillo new --scaffold bare <name>` |
| single (default) | one instance and its data in SQLite; add sign-in later (skill/add-signin.md) | `rastrillo new <name>` |
| accounts | people sign up and sign in; each sees their own rows | `rastrillo new --scaffold accounts <name>` |

home and hub (a home that provisions an instance per team) are not built yet.
```

Frontmatter: `"Build a CARLOS app: pick a shape (bare, single, accounts), then GORM models or plain SQL, sessions, owner-scoped queries."`

- [ ] **Step 2: Rewrite add-signin's opening** to say new apps use `--scaffold accounts` and the steps are for an existing `single` app, and add the step that switches its stdlib mux to chi (`go get github.com/go-chi/chi/v5@v5.3.2`; step 4's router replaces the route table; `GETPatterns` and the perf test's `Patterns` give way to `Routes: r`). **Execute the recipe** on a fresh `single` scaffold until its own `make ci` passes, and fix the text wherever it was wrong.

- [ ] **Step 3: Docs pages.** Each says what the shapes are where it used to describe the one scaffold; `compare.md` says scaffolded `accounts` apps use chi for its route groups and the others use the stdlib mux.

- [ ] **Step 4: Budgets and the skill test.** Run: `go test -run 'TestSkill' . -count=1`. Expected: PASS within budgets.

- [ ] **Step 5: Measure.** `go run ./hack/skilleval -name shapes -out ~/.cache/skilleval/baseline2` (about $7), then compare `summary.md` against `split3`. A clear regression in tokens or task success goes to a discussion before landing; noise does not.

- [ ] **Step 6: Commit** `skill, docs: pick a scaffold shape first`.

### Task 9: The whole gate, review and landing

- [ ] **Step 1: Run the gate once.** Run: `make ci` in the worktree. Expected: PASS. If a new suite was added, the `@N` bump is in this branch.
- [ ] **Step 2: Before-and-after runs.** Per CONTRIBUTING, run each new test against `origin/main` (expect failure) and the branch (expect pass); post both runs in a branch discussion.
- [ ] **Step 3: Cross-provider review.** Astra (`codex exec -m gpt-6-astra -c model_reasoning_effort=high -s read-only`, sandbox off) on the branch diff against the spec and this plan; post the prompt, the whole reply and what was done about each finding.
- [ ] **Step 4: Describe the branch** with the model and reasoning setting, and update the handoff.
- [ ] **Step 5: Land** with `amadan branch merge rastrillo/rastrillo scaffold-shapes -squash -expect <full sha> -message "<subject>\n\n<why>"`, then `make mirror`.
