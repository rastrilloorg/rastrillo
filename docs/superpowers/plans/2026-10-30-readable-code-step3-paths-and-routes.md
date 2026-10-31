# Step 3, named paths and `rastrillo routes`: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an action redirect to a route by a name the compiler
checks, so renaming a path is one edit; print an app's whole route
table, with each route's handler and guards, without running the app;
and fail an action that no route mounts.

**Architecture:** Package `action` gains `type Path string`: a route's
path is written once, in full, and the same value registers the route
and fills the URL an action redirects to (`tagEdit.For(tag)`). Every
path given to the route table is the full path, so the path a person
reads in the code is the URL. A recording route table that touches no
router lets a tool register an app's routes without building any of
its middleware. `rastrillo routes` builds a small program with `go
build -overlay` (nothing is written into the app), runs it, and prints
the table; `--check` also fails an action no route mounts.

**Tech Stack:** Go 1.26, chi v5, `go/build` and `go/parser` to find the
registering function, `go build -overlay`.

**Spec:** `docs/superpowers/specs/2026-10-29-readable-code-design.md`
("The route table's shape": named paths, `rastrillo routes` and its
contract, a handler no route mounts; "Order of work" step 3). Step 1 is
841e34e6 (package `action`), step 2 is 7cb8e3ad (`budget code`).
Reviewed by GPT-6 Astra (discussion on the branch).

## Global Constraints

### Paths

- **Full paths.** Every path given to `Get`, `Post`, `Handle`, `Mount`
  and `Route` is the full path from the root. A prefix is normalised by
  dropping one trailing slash (`"/admin/"` and `"/admin"` are the same;
  `"/"` is the root, normalised to `""`). Inside `Route(prefix, ...)`,
  a path must equal the normalised prefix, or start with it followed by
  `/`; the rest is handed to chi (`"/"` when the path equals the
  prefix). Anything else panics at registration, naming the prefix and
  the path and giving the full path to write. Nested `Route`s check
  against the innermost prefix. `List` records each path as given;
  `Mount` records its prefix followed by `/*`, as now. This replaces
  step 1's relative paths inside `Route`; every existing serving and
  guard assertion is kept, rewritten to full paths, including the root
  and trailing-slash cases in `routes_nesting_test.go`.
- **`Path`:** `type Path string`; untyped string constants convert to
  it. `func (path Path) For(values ...any) string` fills each chi
  parameter in order. A parameter is `{...}` with balanced braces (so
  `{id:[0-9]{2}}` and `{name:[a-z]*}` are one parameter each, and one
  can sit inside a segment, as in `/x-{id}.json`); a catch-all is a
  `*` outside any parameter at the end of the pattern (`/files/*` and
  chi's `/files/prefix*` alike), and `For` panics on a path that has
  one. Each value is `fmt.Sprint`ed and must not be empty, `.` or
  `..` (each would send a redirect off the route: `//admin` is
  scheme-relative, `..` is cleaned away); `For` panics on one. The
  value is then `url.PathEscape`d. A count that does not match panics,
  naming the path, the parameters and the values. `For()` on a path
  with no parameters returns the path.
- **Round trip.** chi matches against `URL.RawPath` when it is set
  (the path arrived escaped in a non-canonical way, or `basepath` set
  it) and against the already-decoded `URL.Path` otherwise. So
  `Visit.PathValue` unescapes (`url.PathUnescape`) only when the
  request's `RawPath` is set, and returns chi's value as it is
  otherwise; an unescape error returns the raw text and `PathID`
  treats it as not-found. `For(value)` → request → `PathValue` must
  give back `value` for `a/b`, `x y`, `100%`, and the literal texts
  `%2F`, `%31` and `%252F`, at the root and under a tenant base path.
- **Positional values** stay (ruled, a known limit): routes with
  several parameters are rare in Rastrillo apps and the pattern names
  each one, but two values of different types can still be swapped
  without the compiler noticing. A typed calling convention can come
  later if real apps need it.

### Registering and listing

- **The contract** (spec: "the app exposes one function that registers
  everything, given its dependencies"): an exported, package-level,
  non-generic function in the app's package:
  `func Routes(routes *action.Routes, app <T>)` where `<T>` is an
  exported type of that package or a pointer to one, and no results.
  The app calls it with its real dependencies when it serves;
  `rastrillo routes` calls it with a recording table and an empty
  dependency: the zero value of `<T>`, or for a pointer type a pointer
  to a zero value (`&App{}`), so `app.API` or a value-receiver method
  bound through `app` does not dereference nil. It only registers: no queries, no network, nothing built.
  Package-level initialisation in the app package must not need the
  app's dependencies either (it runs when the program imports it).
- **The recorder:** `func NewRecorder() *Routes` returns a route table
  that records every registration and touches no router: guards are
  named but never built, handlers and mounted handlers are never
  wrapped, so a guard or handler that reads a zero app cannot panic.
  `List` reports exactly what a real table would.
- **Finding the function:** `go list -json ./internal/...` run from
  `<dir>` with the same environment and flags the build will use
  (`GOFLAGS`, tags, `GOOS`), so discovery and compilation choose the
  same files; only each package's `GoFiles` are parsed (never test
  files, never another platform's). Exactly one match is
  used; none is an error that shows the signature to write; several is
  an error listing them, resolved with `--package <import path>`.
  A function named `Routes` with the wrong shape is an error that shows
  the shape it has and the one to write.
- **Running it, without touching the app:** the program's source is
  written to a temporary directory under `$TMPDIR`, and `go build
  -overlay <json> -mod=readonly -o <tmp binary> <dir>/.rastrillo-routes`
  makes it appear inside the module (so `internal/` imports are
  allowed) without writing there. The binary writes its JSON to a
  temporary file whose path it is given, never to stdout, so anything
  the app prints is shown, not parsed. Build and run use a context
  cancelled on SIGINT or SIGTERM, with the child in its own process
  group killed and waited for; the temporary directory is removed
  however the command ends. `go.mod` and `go.sum` are byte-identical
  afterwards. If the overlay cannot place a new package directory,
  stop and report: do not fall back to writing into the app.
- **`rastrillo routes [--package path] [--check] [dir]`** (default
  `.`): prints method, path, handler and guards, one route per line,
  in registration order, then one line saying guards are page
  middleware and an operation's `Authorize` can still refuse. Exit 1
  on an error (the message says what to do). `--check` also finds
  every function or method with the action signature (as
  `internal/codelimits` identifies one, through the import path) in
  every package of the app module that `go list ./...` builds, and
  fails, exit 1, naming each one no route mounts (spec: "A handler no
  route mounts fails the gate"). Wiring `--check` into an app's
  `make ci` is step 4's.
- **Handler identity.** `action.Route` gains `Action string`: for a
  `Get` or `Post`, the handler's full identity from
  `runtime.FuncForPC` (package path, receiver and name, without `-fm`,
  e.g. `example.com/app/internal/app.(*App).tagEdit`); empty for
  `Handle` and `Mount`, whose `Handler` is only a display name.
  `--check` compares these identities, never display names, so
  `(*Tags).Show` and `(*Tickets).Show` stay distinct and a `Handle`
  named like an action mounts nothing.

### For every task

- New code meets the limits with no new records (`make code`). `main`
  in `cmd/rastrillo` has a recorded 51-line body: move its dispatch
  `switch` into a `dispatch(command string, args []string) error` in
  `main.go` (the docsite scanner reads `main.go`'s switches), then run
  `budget code --lower` so `main`'s record comes down.
- `cmd/rastrillo` is at its size ceiling (6,078): logic lives in
  `internal/routelist`; `cmd/rastrillo` only parses flags and
  dispatches; its record rises by exactly the lines added, with a
  reason.
- User-facing text (panics in `action`, `routelist`'s messages, help,
  the `action` reference page's changes, `cli.md`, the CHANGELOG) is
  drafted in Tasks 1 to 4 and copy-reviewed with Paul in Task 5;
  `skill/code.md` is for agents and is not.
- Test evidence per CONTRIBUTING §3 (red in a detached worktree at the
  task's BASE with compiling stubs, under
  `~/.cache/code-routes/evidence/`).
- Git unsandboxed; `GOFLAGS=-mod=mod` for hand `go` commands at the
  root; `amadan commit`, body ending
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- A relative path inside `Route("/admin", ...)`: a panic saying to
  write `/admin/x` (Task 1).
- `For("")`, `For("..")`, and a value with `/`, `%` and a space: the
  first two panic; the last round-trips through a request (Task 1).
- A guard whose constructor reads a nil field of a zero app: the
  recorder never calls it (Task 2).
- `rastrillo routes` on an app whose `Routes` lives only in a
  `_test.go` file or behind another platform's build tag: "no Routes
  function", not a build failure (Task 3).
- `rastrillo routes` interrupted mid-build: no temporary files left,
  no child process left, `go.mod` unchanged (Task 3).

---

## File Structure

```
action/
  path.go           Path, For, parameter parsing
  routes.go         full paths, the prefix check, the recorder
  visit.go          PathValue unescapes
  example_test.go   one value registers a page and fills the redirect to it
  *_test.go
examples/tickets/internal/proof/   Routes(routes, ticketTypes) contract
internal/routelist/
  doc.go, find.go (go/build discovery and the contract check),
  program.go (the generated source), run.go (overlay build, run,
  cancellation, cleanup), handlers.go (--check's inventory),
  table.go, messages.go, *_test.go, testdata/
cmd/rastrillo/main.go (dispatch, help), routes.go
.rastrillo/budgets.txt, .rastrillo/code.txt (main lowered)
docs/site/reference/action.md, docs/site/cli.md, CHANGELOG.md
skill/code.md
```

---

### Task 1: `action.Path`, full paths and the round trip

**Files:** Create `action/path.go`, `action/path_test.go`,
`action/example_test.go`; modify `action/routes.go`, `action/visit.go`
and their tests; `examples/tickets/internal/proof/` (full paths; its
own route through a named `Path`).

**Interfaces:**
- Produces: `type Path string`; `func (path Path) For(values ...any)
  string`; `Get(path Path, handler Handler)`, `Post(path Path, handler
  Handler)`, `Handle(method string, path Path, handler http.Handler,
  name string)`, `Mount(prefix Path, handler http.Handler, name
  string)`, `Route(prefix Path, mount func(routes *Routes))`.

- [ ] **Step 1: Write the failing tests**

```go
func TestForFillsEachParameterInOrder(t *testing.T)  // "/a/{x}/b/{y:[0-9]+}".For("p q", 7) == "/a/p%20q/b/7"
func TestForReadsChiParameters(t *testing.T)         // {id:[0-9]{2}}, {name:[a-z]*}, /x-{id}.json each one parameter
func TestForRefusesWhatLeavesTheRoute(t *testing.T)  // "", ".", ".." each panic naming the path
func TestForPanicsOnTheWrongCount(t *testing.T)
func TestForRefusesACatchAll(t *testing.T)
func TestAValueRoundTrips(t *testing.T)              // "a/b", "x y", "100%", "%2F", "%31", "%252F": For -> request through chi -> PathValue -> equal; at the root and under basepath "/acme"
func TestARedirectStaysOnTheRoute(t *testing.T)      // Redirect(page.For(v)) Location headers, at the root and under "/acme"; "", "..": panic before any header
func TestForRefusesChisPrefixCatchAll(t *testing.T)   // "/files/prefix*"
func TestPathsInsideARouteAreFull(t *testing.T)      // served and recorded at the full path, guards run
func TestARelativePathInsideARoutePanics(t *testing.T)
func TestRootAndTrailingSlashPrefixes(t *testing.T)  // Route("/") and Route("/admin/") with full paths, served, recorded; a path equal to the prefix
func TestAGroupInsideAPrefixedRoute(t *testing.T)    // parked from step 1
```

`example_test.go`: `ExamplePath` registers `GET pagePath` and a
`POST formPath` whose action redirects to `pagePath.For(id)`, drives
the POST, follows the redirect, and prints the page handler's output:
one value, two uses.

- [ ] **Step 2: Stubs, red run** (`task1-red.txt`).
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Rewrite existing tests and the proof to full paths;
  `cd examples/tickets && GOFLAGS=-mod=mod go test ./... -count=1`.
- [ ] **Step 5: Green run**, `go vet ./action/`, `make code`.
- [ ] **Step 6: Commit** "action: name a route's path once, and fill it to redirect".

---

### Task 2: The recorder and the registering contract

**Files:** modify `action/routes.go` (`NewRecorder`); tests; the
proof's `Mount` becomes `func Routes(routes *action.Routes, ticketTypes
*TicketTypes)`, called by its harness with the real `TicketTypes`.

- [ ] **Step 1: Write the failing tests**

```go
func TestARecorderListsWhatATableWould(t *testing.T)   // same registrations on NewRoutes and NewRecorder: equal List()
func TestARecorderBuildsNoGuard(t *testing.T)          // a guard middleware func that panics when called: never called
func TestARecorderAcceptsANilMount(t *testing.T)
func TestRouteRecordsAnActionsFullIdentity(t *testing.T) // Get(path, app.tagEdit): Action is pkg.(*App).tagEdit; Handle: Action empty
```

- [ ] **Step 2: Stubs, red run** (`task2-red.txt`).
- [ ] **Step 3: Implement**, and move the proof to the contract.
- [ ] **Step 4: Green run**, the tickets example, `make code`.
- [ ] **Step 5: Commit** "action: a recorder, and Routes(routes, app) as the one way to register".

---

### Task 3: `internal/routelist`

**Files:** Create `internal/routelist/*` and `testdata/`: an `app`
module laid out as `rastrillo new` lays one out, with `cmd/app` and
`cmd/genvectors` (two commands), `internal/app` with
`Routes(routes *action.Routes, app *App)` registering a public route,
a `Route("/admin", ...)` with a guard that reads a field of `app`, a
`Handle` and a `Mount`, and one action method no route mounts; plus
two action types with a method of the same name (`(*Tags).Show`
mounted, `(*Tickets).Show` not), a mounted handler field read through
`app`, a value-receiver action, a `Handle` whose display name matches
an unmounted action, and its own extra local `replace` to a sibling
module; plus small modules for the error cases (no `Routes`; wrong
signature; only in a test file; behind a build tag the build does not
set; two candidates). Each `go.mod` has a `replace` to the repo root
and a `go.sum`.

**Interfaces:**
- Produces: `func Find(dir, chosen string) (Target, error)`;
  `func List(context.Context, Target) ([]action.Route, error)`;
  `func Unmounted(Target, []action.Route) ([]string, error)`;
  `func Format([]action.Route) string`; messages in `messages.go`.

- [ ] **Step 1: Write the failing tests**

```go
func TestListPrintsEveryRouteWithItsGuards(t *testing.T)
func TestFindIgnoresTestFilesAndOtherPlatforms(t *testing.T)
func TestFindSaysWhatToWriteWhenThereIsNone(t *testing.T)
func TestFindExplainsAWrongSignature(t *testing.T)
func TestFindAsksWhichWhenThereAreTwo(t *testing.T)
func TestListLeavesTheModuleUntouched(t *testing.T)   // go.mod, go.sum byte-identical, no new files, after success, a failing build and a failing run
func TestListShowsWhatTheAppPrints(t *testing.T)      // an init that prints: shown, not parsed
func TestCancellingListCleansUp(t *testing.T)         // cancel mid-build: no temp files, no child left
func TestUnmountedNamesTheActionNoRouteMounts(t *testing.T) // (*Tickets).Show named; (*Tags).Show not; the Handle look-alike does not count
func TestFormatEndsWithTheAuthorizeNote(t *testing.T)
```

- [ ] **Step 2: Stubs, red run** (`task3-red.txt`).
- [ ] **Step 3: Implement.** Verify first that `go build -overlay` can
  place `<dir>/.rastrillo-routes/main.go`; if it cannot, stop and
  report (BLOCKED), do not write into the app.
- [ ] **Step 4: Green run**, `go vet`, `make code`.
- [ ] **Step 5: Commit** "routelist: an app's route table, without running the app".

---

### Task 4: `rastrillo routes` and the docs drafts

**Files:** modify `cmd/rastrillo/main.go` (move the switch into
`dispatch`; the `routes` case; help), create `cmd/rastrillo/routes.go`
(flags, signal context, exit codes); `.rastrillo/budgets.txt`;
`.rastrillo/code.txt` (`--lower` after the `dispatch` move); draft
`docs/site/cli.md`'s `rastrillo routes` section, the `action` reference
page's changes (`Path`, `For`, full paths, `NewRecorder`, `Routes(routes,
app)`, `PathValue` unescaping) and a CHANGELOG entry; update
`skill/code.md` (a named `Path` registers and fills; paths are full;
the registering function) within its 9,000 bytes.

- [ ] **Step 1: Write the failing test**: the built command on the
  testdata app prints the table (exit 0), `--check` exits 1 naming the
  unmounted action, an app with no `Routes` exits 1 with the message;
  and SIGINT and SIGTERM sent to the built command during the build
  and during the program's run leave no temporary files, no child
  process, and `go.mod` unchanged.
- [ ] **Step 2: Stubs, red run** (`task4-red.txt`).
- [ ] **Step 3: Implement**; raise `cmd/rastrillo`'s size record by
  exactly the lines added, with a reason.
- [ ] **Step 4: Draft the docs** (drafts for Task 5).
- [ ] **Step 5: Green run**: the new test, `go test ./internal/docsite/`,
  `go test -run Skill .`, `make code`, `make budget`.
- [ ] **Step 6: Commit** "rastrillo routes: list every route, and check none is missing".

---

### Task 5: Copy review, then the gate

- [ ] **Step 1:** The controller runs a copy review with Paul over every
  user-facing string Tasks 1 to 4 drafted.
- [ ] **Step 2:** Apply the approved text verbatim; re-run the tests.
- [ ] **Step 3:** Green CI on the fleet; then land.
