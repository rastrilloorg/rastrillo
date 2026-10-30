# Step 1, the action shape proved: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `page` package (one value for a request, refusal
calls, a form that can show itself again, and a recording route
table), then prove the shape on a generated money form and on Patrix's
tag form before anything enforces it.

**Architecture:** `page` sits above `view`, `op`, `form`, `flash` and
`sessions`, which it calls and none of which imports it. A `Visit`
wraps the response, the request and a per-request copy of
`*rastrillo.Ctx`. `Routes` wraps a `chi.Router`, adapts `func(*Visit)`
handlers, takes ordinary handlers and mounts too, and records every
registration so `rastrillo routes` (step 3) can print it. The proofs
rewrite one generated action and one Patrix handler in the new shape
and check they behave the same.

**Tech Stack:** Go 1.26, chi v5 (already a dependency), the existing
`view`, `op`, `form`, `flash`, `sessions` packages.

**Spec:** `docs/superpowers/specs/2026-10-29-readable-code-design.md`
(sections "The action shape in Rastrillo", "The route table's shape",
"Order of work" step 1). This plan covers step 1 only; steps 2 to 10
get their own plans. Reviewed by GPT-6 Astra (discussion 155).

## Global Constraints

- Every new file meets the spec's limits (the gates arrive in step 2):
  lines ≤ 80 columns with a tab counted as 4; files ≤ 400 lines; a
  handler ≤ 15 lines; any other function ≤ 40 lines. In the proofs a
  breach is a finding about the shape: stop and report it, do not
  record it and carry on.
- No single-letter names except `err`, `t`, `b`, `f` and type
  parameters. Receivers are `visit`, `routes`, `app`, never `v`, `r`,
  `a`. Parameters are `response`, `request`, never `w`, `r`.
- Comments say why, in one or two full sentences; never what.
- No behaviour change to any existing package or route. `page` is
  additive; nothing existing calls it yet.
- Statuses: a field refusal (`op.Invalid`) re-shows at 422; a conflict
  (`op.Conflict`) re-shows at 409; a broken body answers 400 with
  "Bad request."; `NotFound`, `Forbidden` and anything else go through
  `view.Refuse` (404, 403, logged 500). A context the route table
  cannot build answers 500 with "Something went wrong.", as
  `gen.Router` does.
- No new user-facing copy. Both strings above already exist.
- Test evidence follows CONTRIBUTING §3. Record the task's BASE commit
  (`git rev-parse HEAD` before the task starts). The red run happens
  in a separate detached worktree at BASE
  (`git worktree add --detach ~/.cache/action-proof/before-taskN BASE`),
  with the task's new test files copied in and compiling stubs (zero
  values, no behaviour) for what they call. Save its `--- FAIL` output,
  headed with the BASE sha, to `~/.cache/action-proof/evidence/`, plus
  the stubs. A build error is not a red run. Remove that worktree
  afterwards (`git worktree remove`). The green run is in this
  worktree, after the implementation. Task 5 posts every pair.
- Run git unsandboxed in this repo; `make ci` sets `GOFLAGS=-mod=mod`,
  and a hand `go test` at the root needs it too.
- Commit with `amadan commit` per the amadan skill, ending the body
  with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- A `CtxFor` that returns the same `*rastrillo.Ctx` for every request:
  one request's actor must never reach the next (Task 3).
- A guard registered after a route in the same group: registration
  fails at once with a message naming the guard and the route (Task 3).
- `PathID` given `0`, `-1`, `abc` or a number past int64: a not-found
  refusal, never a panic or a 500 (Task 1).
- A body over 1 MiB, or one that cannot be parsed, sent to a route
  whose ID is bad or whose record is missing: 400, as today, because
  the body is checked first, and the operation is never called
  (Tasks 2 and 4).
- A price that does not parse beside a name that does: nothing is
  written, not even the name (Task 4).

---

## File Structure

```
page/
  doc.go          package doc: what page is for, why it sits above view
  visit.go        Visit, NewVisit, accessors, PathID, Render, Flash,
                  Redirect
  readform.go     ReadBody, ReadForm, BadRequest
  refused.go      Refused, RefusedShowing, Form, WholeForm
  routes.go       Handler, CtxFor, Route, Routes and its methods
  visit_test.go
  refused_test.go
  routes_test.go
examples/tickets/internal/proof/
  ticket_types.go       the edit-basics route in the new shape
  ticket_types_form.go  its Form: reading, re-showing, seeding money
examples/tickets/internal/ticketstest/
  proof_test.go         the proof against the generated route
CHANGELOG.md            "New package page" under Unreleased
~/.cache/action-proof/  scratch: evidence, the measuring tool, the
                        Patrix copy; deleted at the end of Task 5
```

---

### Task 1: `Visit` and what an action reads from it

**Files:**
- Create: `page/doc.go`, `page/visit.go`
- Test: `page/visit_test.go`

**Interfaces:**
- Consumes: `rastrillo.Ctx`, `view.Render`, `flash.Set(w, r, kind,
  message)`, `chi.URLParam`.
- Produces:
  - `type Visit struct` (unexported fields `response`, `request`,
    `ctx`)
  - `func NewVisit(response http.ResponseWriter, request *http.Request,
    ctx *rastrillo.Ctx) *Visit`
  - `func (visit *Visit) Response() http.ResponseWriter`
  - `func (visit *Visit) Request() *http.Request`
  - `func (visit *Visit) Ctx() *rastrillo.Ctx`
  - `func (visit *Visit) Context() context.Context` (the request's)
  - `func (visit *Visit) PathValue(name string) string`
  - `func (visit *Visit) PathID(name string) (int64, error)`
  - `func (visit *Visit) Render(page string, status int, data any)`
  - `func (visit *Visit) Flash(message string)` (kind `"notice"`)
  - `func (visit *Visit) Redirect(path string)` (303 See Other)

- [ ] **Step 1: Write the failing tests**

```go
func TestPathIDRefusesWhatIsNotAnID(t *testing.T) {
	for _, raw := range []string{"0", "-1", "abc", "99999999999999999999"} {
		visit := visitAt(t, "/things/"+raw) // chi route /things/{id}
		_, err := visit.PathID("id")
		if !errors.As(err, new(op.NotFound)) {
			t.Errorf("PathID(%q) = %v, want op.NotFound", raw, err)
		}
	}
	// visitAt(t, "/things/42").PathID("id") == 42, nil
}

func TestFlashAndRedirectSendTheNoticeAndASeeOther(t *testing.T) {
	// Flash("Saved.") then Redirect("/things"): status 303,
	// Location "/things", and a Set-Cookie that flash.Take reads back
	// as kind "notice", message "Saved."
}

func TestRenderGoesThroughTheAppsRenderSeam(t *testing.T) {
	// ctx.Render records (page, status, data); Render("x", 422, 7)
	// reaches it with exactly those values.
}
```

`visitAt` is a helper in `visit_test.go` that runs a chi router with
`/things/{id}` and captures the `*Visit` built inside it.

- [ ] **Step 2: Add stubs, run, save the red run**

Stubs return zero values. Run:
`GOFLAGS=-mod=mod go test ./page/ -count=1 2>&1 | tee ~/.cache/action-proof/evidence/task1-red.txt`
Expected: `--- FAIL` for all three tests, with their own messages.

- [ ] **Step 3: Implement `page/doc.go` and `page/visit.go`**

`PathValue` reads `chi.URLParam`, falling back to
`request.PathValue` so a `ServeMux` pattern works too. `PathID`
answers `op.NotFound{}` for anything `strconv.ParseInt` rejects or
that is below 1, matching `view.ParseID`. `Render` calls
`view.Render(visit.ctx, visit.response, page, status, data)`.

- [ ] **Step 4: Run them and see them pass**

Run: `GOFLAGS=-mod=mod go test ./page/ -count=1 | tee ~/.cache/action-proof/evidence/task1-green.txt`
Expected: PASS.

- [ ] **Step 5: Commit**

`amadan commit -m "Add page.Visit: one value for a request" ...`
with `page/doc.go page/visit.go page/visit_test.go`.

---

### Task 2: Reading a form and answering refusals

**Files:**
- Create: `page/readform.go`, `page/refused.go`
- Test: `page/refused_test.go`

**Interfaces:**
- Consumes: Task 1's `Visit`; `view.Refuse(ctx, w, r, what, err)`;
  `form.Parse`, `form.Errors`; `op.Invalid`, `op.Conflict`.
- Produces:
  - `type BadRequest struct { Status int; Message string }` with
    `Error() string`
  - `func (visit *Visit) ReadBody() error`: caps the body at 1 MiB and
    parses it; a body it cannot read or parse is `BadRequest{Status:
    400, Message: "Bad request."}`. Calling it twice is safe. An
    action calls it first, before any path or database work, so a
    broken body is a 400 whatever else is wrong.
  - `func (visit *Visit) ReadForm(fields ...form.Field) (*form.Parsed,
    error)`: calls `ReadBody`, then `form.Parse`. Field errors come
    back as `op.Invalid{Fields: parsed.Errors()}` beside the parsed
    form, which a re-show needs.
  - `type Form interface { ShowAgain(visit *Visit, status int, errors
    form.Errors) }`
  - `const WholeForm = ""` (the `form.Errors` key for a message about
    the whole form, which a template passes to the `form-error`
    partial)
  - `func (visit *Visit) Refused(err error) bool`
  - `func (visit *Visit) RefusedShowing(err error, again Form) bool`

- [ ] **Step 1: Write the failing tests**

```go
func TestRefusedAnswersEachKindAndReportsIt(t *testing.T) {
	cases := []struct {
		err    error
		status int
		answer bool
	}{
		{nil, 0, false},
		{page.BadRequest{Status: 400, Message: "Bad request."}, 400, true},
		{op.NotFound{}, 404, true},
		{op.Forbidden{Reason: "x"}, 403, true},
		{errors.New("disk"), 500, true},
	}
	// Refused(err) returns answer; the recorder's status is status
	// (0 means nothing was written).
}

func TestRefusedShowingShowsTheFormAgain(t *testing.T) {
	// op.Invalid{Fields: {"Name": "m"}} -> again.ShowAgain called once
	// with 422 and {"Name": "m"}; returns true.
	// op.Conflict{Message: "moved"} -> ShowAgain with 409 and
	// {page.WholeForm: "moved"}; returns true.
	// A BadRequest -> 400, ShowAgain not called.
	// op.NotFound{} -> 404 through view.Refuse; ShowAgain not called.
	// nil -> false; nothing written; ShowAgain not called.
}

func TestRefusedShowingWithNoFormSaysUseRefused(t *testing.T) {
	// RefusedShowing(op.Invalid{...}, nil) panics; the recovered
	// value's text contains "use Refused".
}

func TestABrokenBodyIsABadRequest(t *testing.T) {
	// 1 MiB + 1 byte: ReadBody and ReadForm both return
	// BadRequest{400, "Bad request."}.
}

func TestAFieldThatDoesNotParseIsARefusal(t *testing.T) {
	// A well-formed body missing a required field: ReadForm returns
	// a non-nil *form.Parsed and op.Invalid naming that field.
	// A complete body: a parsed form and a nil error.
}
```

`Refused` logs through `view.Refuse` with `what` set to the request's
method and chi route pattern (`"POST /things/{id}"`), so a log line
names the route without each action spelling it out.

- [ ] **Step 2: Add stubs, run, save the red run**

Run: `GOFLAGS=-mod=mod go test ./page/ -run 'Refused|Body|Parse' -count=1 2>&1 | tee ~/.cache/action-proof/evidence/task2-red.txt`
Expected: `--- FAIL` for each new test, with its own message.

- [ ] **Step 3: Implement `page/readform.go` and `page/refused.go`**

`BadRequest` is answered with `http.Error(response, Message, Status)`.
`RefusedShowing` handles `op.Invalid` and `op.Conflict` itself and
hands everything else to `Refused`.

- [ ] **Step 4: Run them and see them pass**

Run: `GOFLAGS=-mod=mod go test ./page/ -count=1 | tee ~/.cache/action-proof/evidence/task2-green.txt`
Expected: PASS.

- [ ] **Step 5: Commit**

`amadan commit -m "page: read a form and answer an operation's refusal" ...`

---

### Task 3: A route table that records what it mounts

**Files:**
- Create: `page/routes.go`
- Test: `page/routes_test.go`
- Modify: `CHANGELOG.md` (Unreleased: "New package `page`", naming its
  exported types and functions; nothing existing changes)

**Interfaces:**
- Consumes: Task 1's `NewVisit`; `rastrillo.ActorFromContext`,
  `sessions.ActorFrom`; `chi.Router`.
- Produces:
  - `type Handler func(visit *Visit)`
  - `type CtxFor func(request *http.Request) (*rastrillo.Ctx, error)`;
    an error answers a logged 500, "Something went wrong."
  - `type Route struct { Method, Path, Handler string; Guards
    []string }`
  - `func NewRoutes(router chi.Router, ctxFor CtxFor) *Routes`
  - `func (routes *Routes) Get(path string, handler Handler)`
  - `func (routes *Routes) Post(path string, handler Handler)`
  - `func (routes *Routes) Handle(method, path string, handler
    http.Handler, name string)`: an existing `net/http` handler,
    recorded under `name`
  - `func (routes *Routes) Mount(prefix string, handler http.Handler,
    name string)`: a whole subtree such as the API, recorded as one
    route, method `"*"`, path `prefix + "/*"`
  - `func (routes *Routes) Guard(name string, middleware
    func(http.Handler) http.Handler)`
  - `func (routes *Routes) Group(mount func(routes *Routes))`
  - `func (routes *Routes) Route(prefix string, mount func(routes
    *Routes))`: chi's `Route`; recorded paths carry the prefix
  - `func (routes *Routes) List() []Route`

- [ ] **Step 1: Write the failing tests**

```go
func TestRoutesRecordEveryRouteWithItsGuards(t *testing.T) {
	// Over chi.NewRouter(): Get("/a", handlerA); Route("/admin",
	// func(admin) { admin.Guard("admin", mw); admin.Post("/b",
	// handlerB); admin.Handle("GET", "/c", plain, "plain") });
	// Mount("/api", api, "api"). List() ==
	//   {GET /a handlerA []}
	//   {POST /admin/b handlerB [admin]}
	//   {GET /admin/c plain [admin]}
	//   {* /api/* api []}
	// A method value records "tagUpdate", not "(*app).tagUpdate-fm".
}

func TestRegisteringNeedsNoDependencies(t *testing.T) {
	// A mount function given a nil-field app struct and a CtxFor that
	// would fail registers everything above without calling either:
	// List() is complete and CtxFor was called zero times.
}

func TestOneRequestsActorNeverReachesTheNext(t *testing.T) {
	// CtxFor returns the SAME *rastrillo.Ctx every call. Request 1
	// carries rastrillo.WithActor(actor A), request 2 none. The
	// handler sees A, then the zero Actor; the shared Ctx's Actor is
	// still zero afterwards.
}

func TestAContextThatCannotBeBuiltIsAFiveHundred(t *testing.T) {
	// CtxFor returns an error: 500, body "Something went wrong.",
	// handler never runs.
}

func TestAGuardAfterARouteFailsAtOnce(t *testing.T) {
	// Post("/b", h) then Guard("admin", mw) in the same group panics;
	// the message names "admin" and "/b" and says to put the guard
	// first.
}

func TestGuardsRunBeforeTheHandler(t *testing.T) {
	// A guard that answers 403 and does not call next: the handler
	// never runs.
}
```

Step 1 proves the mount API meets the spec's registration contract:
registering does no work and every kind of route can be recorded. An
app's single "register everything" function arrives with the scaffold
(step 4) and with Patrix's move (step 9), which test it whole.

- [ ] **Step 2: Add stubs, run, save the red run**

Run: `GOFLAGS=-mod=mod go test ./page/ -run 'Routes|Registering|Actor|Context|Guard' -count=1 2>&1 | tee ~/.cache/action-proof/evidence/task3-red.txt`
Expected: `--- FAIL` for each new test.

- [ ] **Step 3: Implement `page/routes.go`**

Each request copies `*ctxFor(request)` and sets its `Actor` exactly as
`gen.Router` does today (`internal/generate/generate.go`, the `handle`
closure): the request context's actor first, else
`sessions.ActorFrom` when the factory left it zero. The generated
router's boot check is the `CtxFor`'s job: whoever builds the context
fills a nil database with the booted one and refuses a different one,
which Task 4 proves. `Group` and `Route` carry the parent's prefix and
guard names into the child's records. The handler name comes from
`runtime.FuncForPC` with the package path and `-fm` suffix trimmed.
Registration does nothing but record and hand the route to chi.

- [ ] **Step 4: Run them and see them pass**

Run: `GOFLAGS=-mod=mod go test ./page/ -count=1 && go vet ./page/`
Expected: PASS, no vet output. Save the test output as
`task3-green.txt`.

- [ ] **Step 5: Commit**

`amadan commit -m "page: a route table that records what it mounts" ...`
with `page/routes.go page/routes_test.go CHANGELOG.md`.

---

### Task 4: Proof on a generated money form

Rewrite `POST /admin/ticket_types/{id}/edit-basics` (generated today
in `examples/tickets/gen/actions/admin/ticket_types/id/edit_basics_post/edit-basics.POST.go`)
in the new shape, and show it behaves the same except for the spec's
422.

**Files:**
- Create: `examples/tickets/internal/proof/ticket_types.go`,
  `examples/tickets/internal/proof/ticket_types_form.go`
- Test: `examples/tickets/internal/ticketstest/proof_test.go`
- Scratch: `~/.cache/action-proof/measure/main.go`

**Interfaces:**
- Consumes: Tasks 1 to 3; `ops_ticket_types.Get`, `Update`,
  `ValidateUpdate`, `UpdateInput`; `form.SeedMoney`,
  `(*form.Parsed).MoneyInput`, `ErrorArgs`, `Echo`, `Minor`.
- Produces:
  - `type TicketTypes struct { Update func(ctx context.Context, rc
    *rastrillo.Ctx, id int64, in ops_ticket_types.UpdateInput)
    (ops_ticket_types.TicketType, error) }`: its dependencies; a test
    swaps `Update` to count calls and to change the stored currency
  - `func (ticketTypes *TicketTypes) Mount(routes *page.Routes)`
  - `func (ticketTypes *TicketTypes) updateBasics(visit *page.Visit)`,
    at most 15 lines
  - an unexported form type implementing `page.Form`, which carries
    the parsed form and the stored record, and seeds money exactly as
    the generated code's `reshow` does

**The order is fixed by today's behaviour.** `ReadBody` first, so a
broken body is a 400 even on a bad ID or a missing record. Then the
ID, then the stored record, because the money field's currency and
stored amount come from it. Then `ReadForm` with those fields, whose
refusal merges `ValidateUpdate(in)` with the parse errors so a person
sees every problem at once, as the generated code does. Then `Update`.
A helper may group the first three steps if that is what keeps the
handler within 15 lines and reading in order.

**The currency conflict stays in the form layer.** On `op.Conflict`
the action starts again from the original submission: it reloads the
record, rebuilds the fields with the record's new currency, and parses
the submission again, exactly as `handle(..., try+1)` re-enters today.
If the reload makes the submitted currency invalid, the parse refuses
and `Update` is not called again. A second conflict shows the form
with `"rastrillo.ui.money_currency_changed"` on `Price`.

- [ ] **Step 1: Write the failing proof test**

`newProofApp(t)` builds the tickets app as `newAppWithAt` does, with
two differences:
- the outer mux registers `POST /admin/ticket_types/{id}/edit-basics`
  for the proof's chi router, and `/` for `gen.Router`'s mux, **both
  wrapped in the same `app.Middleware`**, so the proof gets the
  localizer the form needs to render;
- the proof's `CtxFor` returns the same `&rastrillo.Ctx{...}` the
  generated factory does, filling a nil `DB` with the booted database
  and refusing a different one, as `gen.Router` does.

Each parity case runs the same requests on a fresh `newApp` and a
fresh `newProofApp` with the same seeded row, and compares the
Location header, the stored row afterwards (name, price minor units,
currency), the status (except that the proof's field refusals are 422
where the generated route's are 400), and in a re-shown form the field
values, the messages with their interpolated arguments, and the hidden
money inputs (`Price.locale`, `Price.currency`, the seed).

```go
func TestTheProofAnswersLikeTheGeneratedEditForm(t *testing.T) {
	cases := []struct {
		name string
		path string     // "/admin/ticket_types/1/edit-basics" by default
		form url.Values // nil when body is set
		body string     // a raw body instead of form
		then url.Values // non-nil: submit the re-shown form again
	}{
		{name: "unchanged save"},
		{name: "changed save"},
		{name: "blank required price"},
		{name: "an amount that does not parse"},
		{name: "an amount in another currency (an error with args)"},
		{name: "a parse error and a validation error together"},
		{name: "another field's error keeps the verified seed"},
		{name: "a rejected locale reseeds from the record"},
		{name: "an oversized body"},
		{name: "an oversized body to a missing record"},
		{name: "an oversized body to an id that is not one"},
		{name: "no such ticket type"},
		{name: "an id that is not one"},
		{name: "a re-shown form submitted again"},
	}
}

func TestABadPriceBesideAGoodNameWritesNothing(t *testing.T) {
	// Update swapped for a counter: an unparseable price with a new
	// name -> 422, Update called zero times, the stored name unchanged.
}

func TestAMovedCurrencyIsReparsedFromTheSubmission(t *testing.T) {
	// Update swapped: on its first call it changes the row's currency
	// to EUR in the database, then returns op.Conflict{}. The proof
	// reloads, rebuilds the fields in EUR and parses the submission
	// again, which still says USD: 422, Update called once, nothing
	// stored, the re-shown form's hidden Price.currency is "EUR" and
	// its body contains catalogEN("rastrillo.ui.money_currency_changed").
}

func TestASecondConflictSaysTheCurrencyChanged(t *testing.T) {
	// Update swapped to return op.Conflict{} without changing the row,
	// twice: 422 with the currency-changed message on Price, Update
	// called twice, nothing stored. Once then success: 303.
}

func TestTheProofKeepsTheBootCheck(t *testing.T) {
	// A factory returning a nil DB: the booted database is used and
	// the save works. A factory returning another *sql.DB: 500,
	// "Something went wrong.", nothing stored.
}
```

"An amount in another currency" posts `moneyPost("12.50", "en",
"EUR")` to the USD record; both sides must show the same interpolated
values from `ErrorArgs` (the currencies named in the message), not
just the same message key.

The cases' form values come from the existing money tests
(`moneyPost`, `TestEditRejectedLocaleReseedsFromTheRecord`,
`TestBlankRequiredPriceKeepsTheAmountAndItsCurrency`), so both routers
are judged on scenarios the generated route is already known to pass.

- [ ] **Step 2: Add a stub `proof` package, run, save the red run**

A stub `updateBasics` that answers 200 with nothing. Run (from
`examples/tickets`):
`go test ./internal/ticketstest/ -run 'Proof|Price|Currency|Conflict' -count=1 2>&1 | tee ~/.cache/action-proof/evidence/task4-red.txt`
Expected: `--- FAIL` for every test in `proof_test.go`, by name.

- [ ] **Step 3: Implement `internal/proof/ticket_types.go` and
  `ticket_types_form.go`**

`proof`'s package doc says it is the target step 5's generator will
emit, and that step 5 deletes it.

- [ ] **Step 4: Run it, then the whole example**

Run: `go test ./internal/ticketstest/ -count=1 && go test ./... -count=1`
Expected: PASS for both. Save the first as `task4-green.txt`.

- [ ] **Step 5: Measure both sides against every limit**

Write `~/.cache/action-proof/measure/main.go`, a `go/ast` program that,
for the Go files it is given, prints each line over 80 columns (tab =
4), each file over 400 lines, each function over 40 lines, each
function whose only parameter is `*page.Visit` over 15 lines, and every
single-letter name it declares (variables, parameters, receivers,
named results) other than `err`, `t`, `b`, `f` and type parameters.
Build it with `-buildvcs=false`. Run it over
`examples/tickets/internal/proof/` and over the generated action's
file, and count, for each side, the handler's lines and the total
lines of everything it needs (helpers, the form type, view data).

Expected: nothing printed for the proof. If anything is, the shape has
not fitted: stop, and report the breach with the code to Paul before
Task 5.

- [ ] **Step 6: Commit, with the measurements in the message**

`amadan commit -m "Prove the action shape on the generated money edit form" ...`
The `-notes` give both sides' handler and total line counts.

---

### Task 5: Proof on Patrix's tag form, and the record

Patrix pins a Rastrillo without `page`, so this proof runs in a
scratch copy, is not committed to Patrix, and is reported on the
branch.

**Files (scratch, under `~/.cache/action-proof/patrix/`):**
- A `git archive` of Patrix `origin/main`, with `go.mod` replacing
  `amadan.net/rastrillo/rastrillo` by this worktree.
- Modify: every `flash.Set(w, kind, message)` call becomes
  `flash.Set(w, r, kind, message)`, the upgrade this worktree's
  CHANGELOG already asks for. Nothing else.
- Modify: `internal/patrix/access.go` (the tag routes mounted through
  `page.Routes` inside `mountAccess`; `tagSave` replaced by `tagCreate`
  and `tagUpdate`).
- Create: `internal/patrix/access/tags.go` (`CreateTag`, `UpdateTag`,
  returning `op.Invalid` and `op.NotFound`, as in the design's spike).

**Interfaces:**
- Consumes: Tasks 1 to 3.
- Produces: nothing other tasks use; a discussion reply.

- [ ] **Step 1: Make the copy build, then record the baseline**

Apply only the `flash.Set` change, save its diff as
`~/.cache/action-proof/evidence/patrix-compat.diff`, then run:
`go test -buildvcs=false ./internal/patrixtest/ -run 'Access|Protected|TagsTake' -count=1 -v 2>&1 | tee ~/.cache/action-proof/evidence/patrix-baseline.txt`
Expected: PASS. A failure here is about the upgrade, not the shape:
report it rather than changing the test.

- [ ] **Step 2: Rewrite the tag routes in the new shape**

Patrix renders through `app.render`, not `ctx.Render`, so the tag
form's `ShowAgain` calls `app.render(visit.Response(),
visit.Request(), status, "admin_access_tag", ...)`. The `CtxFor`
passed to `NewRoutes` sets `Logger` and `ErrorPage` to Patrix's own,
so a refusal looks like Patrix's other error pages. A field refusal
keeps Patrix's 422.

The tag form reads its body through `ReadForm`, which today's
`tagSave` does not: it calls `form.Parse` and ignores a body it cannot
parse. So a malformed body (`name=VIP&ignored=%zz`) or one over 1 MiB
changes from saving to a 400. That is the shape's rule for every page
form, and it is accepted as a behaviour change (recorded in the
ledger and the report). Show it: send both requests to the baseline
copy (Step 1) and to the rewrite, and save the four responses as
`patrix-transport.txt`.

The `/admin` prefix and the admin guard are
installed above `mountAccess` (`app.go`, `admin.go`), so the routes
this proof records lack them. Making Patrix's route record complete is
step 9's work; say so in the report.

- [ ] **Step 3: Run the same tests and see them pass**

Run: the command from Step 1, saving `patrix-after.txt`.
Expected: PASS, the same tests as the baseline.

- [ ] **Step 4: Measure, with Task 4's tool, over `access.go` and
  `access/tags.go`, and over the original `tagSave`**

Expected: nothing printed for the new code. A breach stops the proof,
as in Task 4.

- [ ] **Step 5: Run the whole gate once**

Run (repo root): `make ci`
Expected: PASS. A failure outside `page` and the proof is not caused
by them: report it rather than working around it.

- [ ] **Step 6: Report on the branch, then clean up**

`amadan discuss reply rastrillo/rastrillo 155 -body -` with: each new
handler as written; both sides' handler and total line counts; every
red and green run and the stubs, per CONTRIBUTING §3; the Patrix
compatibility diff, baseline and after runs; and anything that did
not fit. Then delete `~/.cache/action-proof/`.
