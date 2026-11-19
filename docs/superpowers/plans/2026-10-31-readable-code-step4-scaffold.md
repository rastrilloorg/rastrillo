# Step 4, the scaffold: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `rastrillo new` writes an app in the new names and shape, with
short why-only comments, the readable-code rules in its `AGENTS.md`,
and `budget code` and `routes --check` in its `make ci`, so the first
code an agent copies is right and it reads far less to learn the app.

**Architecture:** The app gets an exported `App` and a `New`
constructor. One `Routes(routes *action.Routes, app *App)` registers
everything the app serves: pages in a group guarded by the CSRF check
and the page middleware, and the static files and the JSON API mounted
beside that group, unguarded, as they are served today. Each request's
`Ctx` renders through that request, so `visit.Render` has the
request's language, scheme, base path and session. Guidance that sits
in long comments today moves once into the app's `AGENTS.md` or a
linked topic. Tests hold new apps (every shell, plain and
`--auth=magic`) to the comment limits and both new gates.

**Tech Stack:** Go 1.26, the scaffold's emitters and their tests, package
`action`, `budget code`, `rastrillo routes`.

**Spec:** `docs/superpowers/specs/2026-10-29-readable-code-design.md`
("Where the rules live", "Order of work" step 4). Steps 1 to 3 are on
main (841e34e6, 7cb8e3ad, 0fef47c6). Evidence: discussion 159 (the
agent read the whole 35 kB scaffold to learn its shape; 32% of a new
app's Go lines are comments; `App`'s doc comment is 20 lines). Reviewed
by GPT-6 Astra (discussion on the branch).

## Global Constraints

### The app's shape

- `type App struct` (exported, as the `Routes` contract needs; fields
  unexported) holds what actions need: the database, the logger, the
  origin, the JSON API handler, and for `--auth=magic` its sessions,
  auth, background group and mailer handles.
- `func New(...)` replaces today's `App(...)` and takes exactly the
  parameters today's `App` takes for that variant, in the same order,
  renamed (`database`, not `d`). The `--auth=magic` variant keeps its
  background group and injected mailer, and every caller keeps today's
  ownership and cleanup order (stop the background group before the
  database closes) in `main.go`, the HTTP tests, the browser test and
  the perf test. `New` returns `*http.ServeMux` (as
  `rastrillo.Options.Mux` requires), holding one chi router at `/`.
- `routes.go`: named paths (`const homePage action.Path = "/"`) and
  `func Routes(routes *action.Routes, app *App)`. It registers
  everything the app serves:
  - `routes.Mount("/static", assets.Handler(), "static")`;
  - `routes.Mount("/api/v1", app.api, "api")`, so a bare `/api/v1` and
    everything under it reach the API's own JSON 404 and handlers, with
    no CSRF check and no page middleware (a bearer token is its only
    credential, as today);
  - `routes.Group(...)` with `Guard("csrf", csrf.Protect(app.origin))`
    and `Guard("pages", pages.Middleware)`, then the pages.
  `rastrillo routes` on a new app therefore lists the static and API
  mounts unguarded and every page with both guards.
- `Router` (or whatever the perf lane walks) builds that one chi router
  with `Routes(action.NewRoutes(router, app.contextFor), app)`. Walking
  it now also shows `/static/*` and `/api/v1/*`, which the perf
  inventory refuses without a record: the emitted budget file
  (`new_budget.go`) gains a skip record for each, with its reason (the
  static files and the JSON API are not pages), and the perf test still
  asserts every page is measured.
- `app.contextFor(request)`, an `action.CtxFor` (it returns
  `(*rastrillo.Ctx, error)`), builds a `*rastrillo.Ctx` with the logger,
  `ErrorPage`, and a `Render` closure that captures the request, so
  `visit.Render(page, status, data)` renders with the request's
  localizer, scheme, base path and session, at the status given.
  Pages are actions (`func (app *App) home(visit *action.Visit)`) that
  call `visit.Render`; there is no separate app render helper.
  `ErrorPage` keeps the `rastrillo.ErrorPageFunc` signature with
  spelled-out names.
- A feature package grown later registers its routes with its own
  `func Mount(routes *action.Routes, deps *Deps)` called from the app's
  `Routes` (only the app's top function is named `Routes`, which is
  what `rastrillo routes` finds).

### Names and comments

- Names spelled out everywhere in the scaffold's Go (`err`, `ctx`, and
  `t`/`b`/`f` with their testing types are the only short names).
- **What is measured:** every `.go` file a new app has, outside `gen/`,
  that is not marked `//rastrillo:data`. A **comment line** is a line
  whose text, trimmed, starts with `//` or is inside a `/* */` comment;
  directives (`//go:`, `//rastrillo:`, `//lint:`) are not comment
  lines. A **comment block** is a run of consecutive comment lines.
  Limits: comment lines at most 15% of the measured lines; no block
  longer than 3 lines, except a licence or attribution block (which
  must say so in its first line). Measured for a plain and an
  `--auth=magic` app, every shell.
- Short local warnings stay where a mistake is made (for example one
  or two lines at `BootSchema` saying a subsystem's schema goes here,
  not in `Schema`); the recipe moves to `AGENTS.md`.
- `internal/<app>/icons/icons.go` (from `internal/iconsets`) is a table
  of SVG strings: mark it `//rastrillo:data <reason>` and keep its
  licence attribution; shorten its other guidance.

### The app's AGENTS.md and reading budget

- `AGENTS.md` gains a short "Code" section (the limits one line each,
  rules 1 to 7 a sentence each, matching `SKILL.md`, and a pointer to
  `skill/code.md` in the module, using the module path as the existing
  pointers do) and the growth guidance moved out of the code, said
  once. It may grow by at most 1,000 bytes over today's (6,536 plain,
  7,360 `--auth=magic`). Recipes longer than a paragraph go to a
  `skill/` topic it links instead.
- The total bytes an agent reads to learn the shape (every file `rastrillo
  new` writes outside `gen/` and `static/`) must fall by at least 25%
  for both variants. A test asserts both limits and that the
  essential instructions survive (the `BootSchema`/`Schema` warning,
  scoping every owned query through `owned`, the migration commands,
  the two gates).

### The app's make ci

- New Makefile lanes `code` (`$(RASTRILLO) budget code`) and `routes`
  (`$(RASTRILLO) routes --check`), in the `budget` lane's style, in
  `ci`; and the matching executable steps in the scaffold's
  `.amadan/ci.d/` map (`cmd/rastrillo/new.go`, the step map). A test
  asserts the Makefile's `ci` checks and the runner steps name the same
  lanes. A new app passes both with no `.rastrillo/code.txt`.

### Manifests added later

- `cmd/rastrillo/scaffold/manifest-README.md`'s recipe is rewritten for
  the new shape: `gen.Boot` runs in `New`, the generated router is
  stored on `App`, and `Routes` mounts it (`routes.Mount(...)`, nil on
  the recorder, which accepts it). An integration test scaffolds an app,
  adds a manifest resource, runs `generate`, applies the recipe
  exactly as written, and checks a generated page serves, `rastrillo
  routes` lists the mount, and an exposed API operation answers. The
  recipe wires the app's one policy into generated page contexts
  (`Authorize: authorize`) as well as the API's, and the test has a
  policy deny one operation and checks it is refused through both the
  page and the API (without the page wiring, `op.Check` falls back to a
  policy that allows it).

### Existing apps

- Adoption is by hand (this amends the spec's "a command of its own",
  ruled: `budget code --init` already writes the starting records, and
  the first adopters are new apps and Patrix in step 9): run `rastrillo
  budget code --init`, add the `code` lane; add the `routes` lane only
  after writing `Routes` (without it, `routes --check` fails
  discovery). Adopt going forward, not a rewrite. The CHANGELOG entry
  says so; record the amendment in the design doc.

### Docs in this step

- `SKILL.md`: its "App shape" and "Owner scoping" examples, its
  statement that a scaffolded `make ci` does not run the code gate, and
  any other shape, gate, render or feature-mount instruction. `skill/*.md`:
  every app-handler example, `skill/code.md`'s note that the scaffold
  still writes short names and that apps do not run the gate,
  `skill/screens.md`'s `Mount(r chi.Router, deps)` guidance. Tests
  assert the obsolete phrases are gone. `docs/site/app-shape.md` is
  drafted for copy review. Other `docs/site` pages' handler examples
  are step 7's. `SKILL.md` ≤ 9,100 bytes, topics ≤ 9,000.

### For every task

- Budgets: `cmd/rastrillo` source is at 6,164 and must not grow; its
  tests are at 8,000 of 8,000, so new scaffold tests go in a new
  package (`internal/scaffoldcheck`, driving the built `rastrillo`) or
  the test column rises by exactly the lines added, with a reason.
- New code meets the limits (`make code`); test evidence per
  CONTRIBUTING §3 under `~/.cache/code-scaffold/evidence/`; git
  unsandboxed; `amadan commit` with the Co-Authored-By line.
- User-facing text (CHANGELOG, `docs/site/app-shape.md`, the scaffold's
  README) is copy-reviewed with Paul in Task 6.

## Review Focus

- `rastrillo routes` on a new app lists the static and API mounts with
  no guards and every page with `csrf` and `pages` (Task 1).
- A bearer-token POST to `/api/v1/...` with no Origin header still
  works, and a bare `/api/v1` answers the API's JSON 404 (Task 1).
- `visit.Render` in a new app renders in the request's language under
  a base path, and a refusal renders the app's error page (Task 1).
- The sidebar and console shells' extra route appears in `rastrillo
  routes` and passes `--check` (Task 2).
- An existing app that adds the `code` lane but not `Routes` is told
  plainly not to add the `routes` lane yet (Task 5's CHANGELOG).

---

## File Structure

```
cmd/rastrillo/new.go         app, routes, handlers->actions, main, migrations,
                             models, harness/test templates, makefile, step map
cmd/rastrillo/new_auth.go    the --auth=magic overlay
cmd/rastrillo/new_budget.go  the perf test template
cmd/rastrillo/scaffold/      api, tokens, auth, signin templates, manifest-README.md
internal/iconsets/iconsets.go  the icons file it emits
internal/scaffoldcheck/      new: tests that drive the built rastrillo on new apps
SKILL.md, skill/*.md, docs/site/app-shape.md (draft), CHANGELOG.md (draft)
docs/superpowers/specs/2026-10-29-readable-code-design.md (the amendment)
```

---

### Task 1: The app's shape

**Files:** `new.go` (app, routes, handlers, render, main, migrations,
models), `new_auth.go`, `new_budget.go`, `scaffold/*.tmpl`,
`internal/iconsets/iconsets.go`, and the existing tests that assert
their text.

- [ ] **Step 1: Failing tests** in `internal/scaffoldcheck`: for a plain
  and an `--auth=magic` app (default shell): the comment limits as
  defined; `codelimits.Measure` finds nothing; `rastrillo routes` lists
  the static and API mounts unguarded and every page guarded by `csrf`
  and `pages`; a bearer-token POST to the API without an Origin header
  works and a bare `/api/v1` is the API's JSON 404; a page rendered
  through `visit.Render` under a base path is in the requested
  language; a refusal renders the error page.
- [ ] **Step 2: Red** at BASE (`task1-red.txt`).
- [ ] **Step 3: Rewrite the emitters.** Keep every behaviour; list each
  piece of guidance moved out of a comment, for Task 3.
- [ ] **Step 4: Green**: the new tests; `go test ./cmd/rastrillo/
  -count=1` (scaffold tests included); `make code`; `make budget`.
- [ ] **Step 5: Commit** "rastrillo new: the app in the new names and shape".

### Task 2: Every shell, and a real browser

- [ ] **Failing tests**: for every shell (`rastrillo new --shell=...`),
  plain and `--auth=magic`: `budget code` and `routes --check` pass,
  and the two-page shells' extra route is listed. One real browser run
  (the scaffold's browser drive) for a plain and an `--auth=magic` app.
- [ ] Red, implement whatever the shells need, green, commit
  "rastrillo new: every shell in the new shape".

### Task 3: AGENTS.md, README, make ci and the runner steps

- [ ] **Failing tests**: the `code` and `routes` lanes in `ci` and in the
  `.amadan/ci.d/` steps, the same set in both; `AGENTS.md`'s Code
  section; the byte limits and the essential instructions; the whole
  gate offline (`TestScaffoldWholeGatePassesOffline` and the stage
  shell's) with both lanes.
- [ ] Red, implement (the README is a draft for Task 6), green, commit
  "rastrillo new: the rules in AGENTS.md, and both gates in make ci".

### Task 4: Manifests added later

- [ ] **Failing integration test** as the constraints describe; red;
  rewrite `manifest-README.md`'s recipe; green; commit "rastrillo new:
  a manifest recipe for the new shape, run end to end".

### Task 5: SKILL.md, skill/, the design amendment, CHANGELOG, measurement

- [ ] Update `SKILL.md` and `skill/*.md` as the constraints list, with
  tests that the obsolete phrases are gone; record the adoption
  amendment in the design doc; draft the CHANGELOG entry and
  `docs/site/app-shape.md` for Task 6; measure before (0fef47c6) and
  after and write the numbers into the branch workspace. Commit.

### Task 6: Copy review, then the gate

- [ ] The controller runs the copy review with Paul, applies it
  verbatim, posts the measurement on discussion 159, lands on a green
  gate, and sends the MCP session (`rastrillo-7b`) the new `New`,
  `Routes` and API mount.
