# Named operations and the parity check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an app declare any action once, in Go, as a named operation
that its page, `/api/v1` and MCP all call. Give it a test helper that
fails when:
- a route has no agent twin, or an agent-callable operation has no page;
- a door serves an operation other than the one declared;
- a human-only operation answers an agent;
- the three doors end the same scenario differently.

**Architecture:**
- `op.Define` takes an operation's Go input and result types and its
  function. It derives the JSON Schemas and builds the `op.Descriptor`.
  `Call` runs `op.Check` and then the function. This is the one code path
  every door shares.
- `api.Operations` turns descriptors into `/api/v1/<area>/<verb>` routes.
  The door runs `op.Check` itself before `Invoke`. `mcp.New` takes the
  same descriptors.
- Both doors report what they serve, through `api.Served` and
  `mcp.Served`. The API tree stamps each matched answer with the
  operation's ID.
- A new `route` package wraps chi or `ServeMux` registration and records
  each route with its operation or a page-only reason.
- A new `paritytest` package does two jobs:
  - `Routes` compares that record and both doors' inventories with the
    declared operations;
  - `Run` drives an app-supplied scenario through the page, the API and
    MCP, each on a fresh fixture, and compares the outcomes and the
    stored state.
- A human-only operation carries its reason on `rastrillo.Operation`.
  `api.Gate.Admit` and `op.Check` both refuse every token for it first,
  with a new `op.HumanOnly` error and the `human_only` code.

**Tech Stack:** Go 1.26, `github.com/go-json-experiment/json` (v2, the
wire codec), `github.com/go-chi/chi/v5` and `net/http`'s `ServeMux`, and
the MCP Go SDK through the existing `mcp` package.

**Spec:** `docs/superpowers/specs/2026-11-22-agent-parity-design.md`
§5.1, §5.2, §5.6 ("tools by access") and §5.8. Read with
`docs/superpowers/specs/2026-10-05-json-api-design.md` §6 (wire and
errors) and `docs/superpowers/specs/2026-10-25-mcp-design.md` §4–§5
(flags, fence, tool list).

**Not in this plan:** grants (§5.3, plan 1), OAuth (§5.5), plaza and
Patrix (Patrix content is plan 0b, written after this releases), manifest
or generator changes.

## Rulings

These are Paul's rulings on 2026-11-22, on Astra's review of the first
draft (with fixes, ten findings).

- **Refusal order** at the API gate, at MCP admission and in `op.Check`:
  1. human-only: an agent gets `human_only`, never `forbidden`,
     `invalid` or a field error;
  2. access, a read-only token's write: `forbidden`;
  3. the agent session: `invalid`;
  4. the app's policy;
  5. input validation.

  Steps 2 and 3 swap relative to v0.42.0's `Admit`. A read-only agent's
  write sent without `Agent-Session` is now `forbidden`, not `invalid`.
  The CHANGELOG says so.
- **The API door runs `op.Check` before `Invoke`,** whatever `Invoke`
  does, so a hand-written descriptor that skips the check still can't
  run for a refused caller.
- **`paritytest.Routes` compares complete inventories** (the API's, MCP's
  and the page table's) with the declared operations, and flags
  unexpected entries as well as missing ones. `paritytest.Run` is the
  scenario runner.
- **`rastrillo routes --check` learns the marking rule**, as the design
  says. It enforces the rule only for an app that declares at least one
  named operation (a call to `op.Define` in the module), so apps that
  haven't adopted operations stay green. There is no `--parity` flag.
- **Every fail-first run is a behavioural failure** against compilable
  stubs (`CONTRIBUTING.md` §3). Each task's first step lists its stubs.
- **Decisions confirmed by the review:**
  - every token is refused on a human-only operation, the person's own
    token included;
  - human-only operations stay registered on both doors, hidden from the
    tool list and refused;
  - an operation with no page fails the check;
  - a read served by POST is allowed only on routes `api.Operations`
    builds.

## Global Constraints

- **No new module dependencies.** `go.mod`'s require list stays as it
  is. Schemas are derived by Rastrillo's own code.
- **Hand-written, no codegen.** Add nothing to `internal/generate`, and
  no manifest steps.
- **Backwards compatible**, except for the precedence ruling and the new
  `human_only` answer. Every existing caller of `api.New`, `api.Route`,
  `mcp.New`, `op.Check` and `action.Routes` compiles unchanged. Apps that
  don't call `op.Define` see no new `routes --check` failure.
- **Names:**
  - An operation ID is `<area>.<verb>` and matches
    `^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`.
  - Its tool name is `<area>_<verb>` and is at most 64 characters.
  - Its API route is `POST /api/v1/<area>/<verb>`, or
    `GET /api/v1/<area>/<verb>` for a read with `Spec.InQuery`.
- **Wire names:**
  - Every exported field of an input or result type has a json tag that
    names it.
  - That name equals `internal/wirename.Name(<Go field name>)`.
  - So `op.Invalid.Fields`, keyed by the Go field name as `skill/code.md`
    writes it, reaches the API under the same snake_case name
    (`api.FieldName`).
- **Errors** follow the JSON API spec §6. There is one new code,
  `human_only` (403), and the order in Rulings.
- **Identity header.** The API tree sets `Rastrillo-Operation: <id>` on
  every answer to a request that matches one of its routes, 401
  included. IDs are already public (`api/v1.json` and the tool names).
- **Code** follows `skill/code.md`:
  - names spelled out except `err`, `ctx`, `t`;
  - 80 columns;
  - non-test files of 400 lines or fewer, and non-test functions of 40
    lines or fewer;
  - comments that say why;
  - tests named as sentences.
- `go run ./cmd/rastrillo budget size` and
  `go run ./cmd/rastrillo budget code` pass at the root with no new
  record.
- **Running things:**
  - Run git and go with the Bash sandbox disabled and `GOFLAGS=-mod=mod`.
  - Run targeted tests locally. The fleet runs `make ci` on push.
  - Put scratch in `$TMPDIR`.
- **Fail-first:**
  - Each task applies its stubs first: the new names with bodies that
    return zero values or do nothing, so the old code plus the stubs
    compiles.
  - Run the new tests against that and keep the `--- FAIL: TestX` output.
  - Then implement and keep the passing output, for the branch
    discussion.
  - A test of behaviour that already works says so and need not fail.
- **Copy:**
  - Every user-facing string is the approved copy, verbatim, held in a
    named constant. Tests compare against the constants.
  - Docs prose goes through Luna in Task 10.
- **Commits:** subjects of 79 characters or fewer, imperative, a body
  that says why, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- **A required input member that is left out** (`{}` to `notes.create`)
  reaches the operation as its zero value, because `op.DecodeInput`
  doesn't enforce `required`. People expect the operation's own
  validation to refuse it, with the same field on every door. Task 8's
  example scenario has this step.
- **A person's own token with no agent name** (`apitoken`'s `ForSelf`)
  calling a human-only operation. People expect a refusal exactly as for
  an agent. Task 1: `TestAdmitRefusesEveryTokenOnAHumanOnlyOperationFirst`.
- **A GET read sent a repeated, ill-typed or unknown query parameter.**
  People expect a 422 naming that parameter, with the body rules'
  message. Task 4: `TestQueryArgumentsAreRefusedByTheBodyRules`.
- **A POST with no body** to an operation whose inputs are all optional.
  People expect it to run as `{}`, as MCP treats absent arguments. A
  body with no JSON content type is still 415. Task 4:
  `TestAnEmptyPostIsAnEmptyObject` and
  `TestABodyWithoutJSONContentTypeIs415`.
- **A route registered inside a chi sub-router** must be recorded under
  its full path. The same method and path registered twice must stop the
  app's start. Task 6: `TestATableOnASubRouterRecordsTheFullPath` and
  `TestRegisteringAMethodAndPathTwicePanics`.

## Files

| Path | Responsibility |
|---|---|
| `ctx.go` (modify) | `Operation.HumanOnly` |
| `op/op.go`, `op/check.go` (modify) | the `HumanOnly` refusal, checked first; `None`; `Descriptor.InQuery` |
| `op/schema.go` (create) | `schemaFor`: JSON Schema from a Go type, per direction |
| `op/define.go` (create) | `Spec`, `Named`, `Define`, `Call` |
| `op/messages.go` (create) | `Define`'s and `schemaFor`'s copy |
| `api/gate.go` (modify) | `Admit`'s new order |
| `api/errors.go` (modify) | the `human_only` code |
| `api/decode.go` (modify) | split out the shared body read |
| `api/tree.go` (modify) | the identity header; POST+Read for named routes; `Served` |
| `api/operations.go` (create) | `Operations`, the named door, query arguments |
| `mcp/mcp.go`, `mcp/list.go` (modify) | `Served`; `refused` counts `op.HumanOnly` |
| `route/route.go`, `route/table.go` (create) | the recorder |
| `paritytest/paritytest.go`, `inventory.go` (create) | `Surface`, `Routes` |
| `paritytest/doors.go` (create) | `Outcome`, `CallAPI`, `CallTool`, `ToolNames` |
| `paritytest/scenario.go` (create) | `Fixture`, `Step`, `Scenario`, `Run` |
| `paritytest/messages.go` (create) | the check's failure copy |
| `paritytest/*_test.go` (create) | the example app over chi and `ServeMux`, its broken variants, and the tests |
| `action/routes.go` (modify) | optional marks on `Get`, `Post`, `Handle` |
| `internal/routelist/unmarked.go`, `declares.go` (create) | `Unmarked`, `DeclaresOperations` |
| `cmd/rastrillo/routes.go` (modify) | `--check` applies the rule |
| docs, skill, CHANGELOG | Task 10, plus each task's reference-page lines |

---

### Task 0: Approved copy

**Files:**
- Modify: this plan's `## Approved copy` section

- [ ] **Step 1: Send the drafts to Luna.** Write a prompt in `$TMPDIR`
  that holds:
  - the drafts below;
  - `AGENTS.md`'s "User-facing copy" section;
  - each string's reader: an app developer for panics, check failures
    and the CLI, or an agent and its person for `human_only`.

  Then run, unsandboxed:
  `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o "$TMPDIR/luna-0a.md" - < "$TMPDIR/luna-0a-prompt.md"`
  Expected: the output file holds a verdict per string. A zero exit with
  no file means the model slug failed: read the log.
- [ ] **Step 2: Apply the corrections.** Skip only a correction that is
  factually wrong, and note why. Keep every verb (`%s`, `%q`, `%d`) in
  the same order. Replace the drafts below, and record the date and
  anything skipped.
- [ ] **Step 3: Commit** with the subject
  `Plan 0a: approved copy for named operations and parity`.

## Approved copy

Approved by Luna (gpt-6-luna) on 2026-11-22. The name in brackets is each
string's constant. Skipped, with reasons:

- `msgHumanListed`: Luna's "Remove it from the tool list" names a fix the
  developer cannot make (`mcp.New` builds the list), so the draft stands.
- `msgHumanCallable`: Luna's "Ask a person to use %s" drops the failure
  (the door answered something other than `human_only`) and misdirects the
  developer, so the draft stands.
- `msgScenarioHumanOnly`: Luna's "Test this scenario with paritytest.Routes"
  is wrong, because `Routes` tests the operation, not the scenario. Applied
  with "it".

- **`api` code `human_only`, 403** [`codes["human_only"]`]:
  `A person must do this in the app. Ask them to do it there.`
- **`api` 422 `invalid` for a GET query that does not parse**
  [`msgBadQuery`], added in Task 4's fix round by Luna on 2026-11-22
  (the draft "The query string could not be read. Send each argument as
  name=value, percent-encoded." could be read as encoding the `=` too):
  `Send each argument as name=value. Percent-encode the name and value.`
- **`op.Define` panics.** Each is prefixed `op: <id>: ` except
  `msgBadID`:
  - [`msgBadID`]: `%q is not an operation name. Write it as <area>.<verb> in lowercase letters, digits and underscores, such as posts.publish`
  - [`msgLongToolName`]: `its tool name %s is %d characters. Keep <area>_<verb> to 64 or fewer`
  - [`msgNoDescription`]: `Description is empty. Write one sentence that says what the operation does`
  - [`msgBadAccess`]: `set Spec.Access to rastrillo.Read or rastrillo.Write`
  - [`msgNilRun`]: `the function is nil. Pass the function that does the work`
  - [`msgInputNotStruct`]: `the input type %s must be a struct. Use a struct for the input`
  - [`msgResultNotStruct`]: `the result type %s must be a struct or op.None. Use op.None when there is nothing to return`
  - [`msgQueryWrite`]: `InQuery is only for reads. Remove it, or set Access to rastrillo.Read`
  - [`msgQueryNotFlat`]: `InQuery needs every input field to be a string, number or bool, but %s is not. Remove InQuery to take a JSON body`
- **`schemaFor` errors**, wrapped by the panic above:
  - [`msgUntagged`]: `field %s.%s has no JSON name. Tag it json:"%s"`
  - [`msgWireName`]: `field %s.%s is %q on the wire. Use %q as its JSON name so errors use the same field name`
  - [`msgUnsupported`]: `field %s.%s is a %s, which JSON can't carry. Use a string, number, bool, struct, slice, or map with string keys`
  - [`msgTagOption`]: `field %s.%s has unsupported json option %q. Use only omitzero, omitempty or inline`
  - [`msgPointerInput`]: `field %s.%s is a pointer in an input. Use op.Opt for a field that may be left out, or op.Nullable for one that may be null`
  - [`msgOptResult`]: `field %s.%s is an op.Opt or op.Nullable in a result. Use a pointer for a value that may be null`
  - [`msgCycle`]: `type %s contains itself, which a JSON Schema here can't describe. Break the cycle with an ID`
- **`route` panics:**
  - [`msgNoMethod`]: `route: %q names no method. Write it as "POST /posts/{id}/publish"`
  - [`msgUnknownMethod`]: `route: %q: the method must be GET, POST, PUT, PATCH or DELETE`
  - [`msgTwice`]: `route: %s %s is registered twice. Register it once`
  - [`msgNoOperation`]: `route: Operation needs a declared operation. Pass its Descriptor, such as posts.Publish.Descriptor`
  - [`msgNoReason`]: `route: PageOnly needs a reason. Say why an agent has no use for this route, such as "the stylesheet"`
- **`paritytest.Routes` failures:**
  - [`msgUnmarked`]: `%s %s names no operation and no page-only reason. Name its operation with route.Operation, or say why an agent has no use for it with route.PageOnly`
  - [`msgUndeclared`]: `%s %s names %s, which Surface.Operations doesn't list. Add it to Surface.Operations, api.Operations and mcp.New`
  - [`msgListedTwice`]: `Surface.Operations lists %s twice. Remove the duplicate`
  - [`msgNoPage`]: `%s has no page. Add a page route for it`
  - [`msgMissing`]: `%s isn't served by %s. Pass its Descriptor to %s`
  - [`msgUnexpected`]: `%s serves %s, which Surface.Operations doesn't list. Add it to Surface.Operations and give it a page, or stop serving it`
  - [`msgDiffers`]: `%s serves %s as %+v, but it is declared as %+v. Pass the declared Descriptor`
  - [`msgWrongOperation`]: `the API serves %s at %s %s, where %s belongs. Move it to its declared route`
  - [`msgNotMounted`]: `%s %s answered %d with Rastrillo-Operation %q, not 401 with %q. Mount the API tree at /api/v1/, ahead of any catch-all`
  - [`msgNotListed`]: `%s isn't in the MCP tool list for Surface.Token. Check that Authorize allows that token every operation`
  - [`msgHumanListed`]: `%s is human-only, but the MCP tool list shows it`
  - [`msgHumanCallable`]: `%s is human-only, but %s answered an agent with %q, not human_only`
  - [`msgUnrecorded`], added in Task 7 by Luna on 2026-11-22 for the
    chi walk (the draft "%s %s is registered on the router directly, so
    the route table has no mark for it. Register it through the table
    with Handle, or with On for a sub-router" was cut to the action;
    the closing period is dropped, as for its siblings):
    `Register %s %s through the table with Handle, or use On to register a sub-router's routes`
- **`paritytest.Run` failures:**
  - [`msgDoorsDiffer`]: `step %q: %s returned %s %v, but %s returned %s %v`
  - [`msgBodiesDiffer`]: `step %q: API returned body %v; MCP returned body %v`
  - [`msgStateDiffers`]: `after step %q: %s left state %v; %s left state %v`
  - [`msgScenarioHumanOnly`]: `step %q calls human-only %s. Test it with paritytest.Routes`
- **`rastrillo routes --check`** [`unmarkedRoutes`]:
  `This app declares operations. Each route must name one or explain why it has none. These routes don't:\n\n%s\nUse route.Operation to name each route's operation. Use route.PageOnly for routes agents should not use.`

---

### Task 1: Human-only comes first, at every door

**Files:**
- Modify:
  - `ctx.go:42-47` (`Operation`) and `op/op.go` (the refusal);
  - `op/check.go:22-35`;
  - `api/gate.go:118-133` (`Admit`) and `api/errors.go` (`codes`,
    `Refusal`, `Error`'s comment "thirteen");
  - `mcp/list.go:101-105` (`refused`).
- Modify docs:
  - `docs/site/reference/op.md` and `rastrillo.md`;
  - `docs/site/reference/api.md` (the code and the order);
  - `skill/api.md` (the code list, and the order in "One policy for
    both doors").
- Test: `op/check_test.go`, `api/gate_test.go` (rewrite
  `TestAdmitChecksTheSessionBeforeReadOnly` and line 73's case),
  `api/errors_test.go`, `mcp/list_test.go`, `mcp/call_test.go`

**Interfaces:**
- Produces:
  - `rastrillo.Operation.HumanOnly string`: the reason. It is non-empty
    when only a person may call the operation.
  - `op.HumanOnly struct{ ID string }`, whose `Error()` is
    `"human only: " + ID`.
  - `Gate.Admit` runs three checks in order:
    1. human-only: `operation.HumanOnly != ""` and
       `actor.IsToken() || actor.Agent != ""` → `op.HumanOnly`;
    2. access: a write with `actor.Access != Write` → `op.Forbidden`;
    3. the session → `errAgentSession`.
  - `op.Check` runs human-only first, then the read-only check, the
    no-subject check and the policy.
  - The code `human_only` (403), which `api.Refusal` maps from
    `op.HumanOnly`.
  - MCP's list hides a tool whose check answers `op.HumanOnly`.

- [ ] **Step 1: Stubs.** Add the `HumanOnly` field, and the
  `op.HumanOnly` type with its `Error`. Change no behaviour.
- [ ] **Step 2: Write the tests**

```go
// op/check_test.go; tokensCreate is a Write operation with HumanOnly set.
func TestCheckRefusesEveryTokenOnAHumanOnlyOperation(t *testing.T)
// For an agent token, a person's own token (Credential, no Agent), an
// agent with no credential, and a read-only agent token, all with an
// allow-all Authorize: errors.As(err, &op.HumanOnly{}) with ID
// "tokens.create".
func TestCheckLetsAPersonAndTheAppCallAHumanOnlyOperation(t *testing.T)
// {Subject "a", Access Write} and the zero actor → nil.

// api/gate_test.go
func TestAdmitRefusesEveryTokenOnAHumanOnlyOperationFirst(t *testing.T)
// strict := Gate{RequireAgentSession: true}. For agentRead with no
// session, agentWrite with no session, and personWrite (a person's own
// token): Refusal(request, strict.Admit(actor, humanOnlyWrite)) is
// 403 "human_only".
func TestAdmitChecksReadOnlyBeforeTheSession(t *testing.T)
// The old table with the first row now 403 codes["forbidden"].message.
// The other rows are unchanged.
func TestAHumanOnlyRouteRefusesBeforeTheBodyIsRead(t *testing.T)
// mustNew with a hand-built POST route for humanOnlyWrite whose Handle
// calls api.Decode and counts calls. serve(..., agentWrite's token, no
// session, body "not json") → 403 "human_only", and Handle ran 0 times.

// api/errors_test.go
func TestHumanOnlyIsA403WithItsOwnCode(t *testing.T)
// Refusal(request, op.HumanOnly{ID: "tokens.create"}) → 403,
// Code "human_only", Message codes["human_only"].message.

// mcp/list_test.go and mcp/call_test.go: bookmarks() plus a human-only
// tokens.create whose Invoke counts calls.
func TestListHidesAHumanOnlyOperation(t *testing.T)
// toolNames(t, url, writerToken) lacks "tokens_create" and keeps every
// bookmarks tool.
func TestCallingAHumanOnlyToolAnswersHumanOnlyWhateverElseIsWrong(t *testing.T)
// As the read-only token, as a writer with no session, and with the
// arguments {"x": null}: refusalOf(...).Code == "human_only" each time,
// and Invoke ran 0 times.
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./op ./api ./mcp -run 'HumanOnly|ReadOnlyBeforeTheSession'`
Expected: `--- FAIL` for each new test, for example "Check = <nil>, want
op.HumanOnly" and "answered 422 ... want 403".

- [ ] **Step 4: Implement** the order in `Admit` and `Check`, with a
  why-comment for each: human-only is a property of the operation and
  the credential, and must win, so an agent learns that no token will
  do. Add the code and the `Refusal` case, and add `op.HumanOnly` to
  `refused`.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./op ./api ./mcp && GOFLAGS=-mod=mod go test ./internal/docsite -run TestExportedSymbolsAreDocumented`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `Refuse every token on a human-only operation, before anything else`.

---

### Task 2: JSON Schema from Go types, as the codec reads them

**Files:**
- Create: `op/schema.go`, `op/messages.go`
- Modify: `op/op.go` (`None`), `docs/site/reference/op.md`
- Test: `op/schema_test.go` (package `op`)

**Interfaces:**
- Produces:
  - `type None struct{}` in `op/op.go`: the result of an operation that
    returns nothing.
  - `type direction int` with `input` and `output`.
  - `func schemaFor(goType reflect.Type, way direction) (json.RawMessage, error)`.
- **Both directions:**
  - **Structs:**
    - A struct is an object with `properties`, a `required` list in
      field order, and `additionalProperties: false`.
    - `json:",inline"` and an untagged embedded struct merge their
      properties in. `json:"-"` and unexported fields are skipped.
    - Any other json option, such as `string`, `format:`, `case:` or
      `unknown`, fails with `msgTagOption`.
    - Each exported field needs a json name equal to
      `wirename.Name(field.Name)` (`msgUntagged`, `msgWireName`).
  - **Scalars:** `string` is a string. `bool` is a boolean. Every int and
    uint kind is an integer. Floats are numbers.
  - **Named types:**
    - `time.Time` is a string with `format: date-time`.
    - `op.Money` is an object with `amount` (integer, required) and
      `currency` (string).
    - `None` is exactly `{"type":"null"}`.
  - **Bytes and containers:**
    - `[]byte` is a string with `contentEncoding: base64`, which is what
      json v2 writes.
    - Any other slice or array is an array. A nil slice encodes as `[]`.
    - `map[string]T` is an object with `additionalProperties` set to T's
      schema.
  - **Anything:** `any`, `json.RawMessage` and `jsontext.Value` are `{}`.
  - **Refused:** chan, func, complex, unsafe pointer, a map whose key is
    not a string, and any interface but `any` (`msgUnsupported`). A type
    that contains itself fails with `msgCycle`.
- **Input only:**
  - `op.Opt[T]` is T's schema and is not required.
  - `op.Nullable[T]` is T's schema with `"null"` added, and is not
    required.
  - A field tagged `omitzero` or `omitempty` is not required.
  - Any other field is required.
  - A pointer fails with `msgPointerInput`, because `DecodeInput`
    refuses null for it.
- **Output only:**
  - `*T` is T's schema with `"null"` added, and is required.
  - `Opt` and `Nullable` fail with `msgOptResult`, because an unset one
    encodes as null.
  - `omitzero` and `omitempty` fields are not required. Every other
    field is required.

- [ ] **Step 1: Stubs.** Add `None` and a `schemaFor` that returns
  `json.RawMessage("{}"), nil`.
- [ ] **Step 2: Write the tests.** `sameJSON` compares two documents
  semantically. `conforms(t, schema, document)` is a test helper of
  about 40 lines. It checks a document against the subset of schema used
  here: type or types, properties, required, `additionalProperties:
  false`, items and `contentEncoding`.

```go
func TestAnInputSchemaNamesEachMemberByItsTag(t *testing.T)
// The input type has fields string, Opt[string], Nullable[Money],
// int `omitzero`, []string, time.Time and []byte → the exact document:
// required ["title","tags","when","blob"]; price ["object","null"];
// blob {"type":"string","contentEncoding":"base64"}.

func TestAnOutputSchemaAllowsNullOnlyForPointers(t *testing.T)
// An output with *int64, []string and string `omitzero` → parent is
// ["integer","null"] and required; next is not required.

func TestSchemaMergesAnInlineMemberAndAnEmbeddedStruct(t *testing.T)
func TestSchemaOfNoneIsNull(t *testing.T)

func TestSchemaRefusesWhatTheWireWouldContradict(t *testing.T)
// One case per error, each compared with fmt.Sprintf of its constant:
// an untagged field; "ipaddress" for IPAddress; `json:"n,string"`; a
// *int in an input; Opt[string] in an output; chan int; func();
// complex128; map[int]string; an interface with a method; a type that
// contains itself.

func TestOutputSchemasDescribeWhatTheEncoderWrites(t *testing.T)
// For the output type above, json.Marshal the zero value and a filled
// value (pointer set, slice non-empty, next set): both conform.

func TestInputSchemasDescribeWhatDecodeInputTakes(t *testing.T)
// For the input type above: a document holding exactly the required
// members, values of the schema's types, conforms and DecodeInput
// accepts it. {"title":null} fails conforms and DecodeInput refuses it.
// {"price":null} passes both. {"blob":"aGk="} decodes to []byte("hi").
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./op -run 'Schema'`
Expected: `--- FAIL` for each. The stub's `{}` matches no expected
document, and it returns no errors.

- [ ] **Step 4: Implement `schemaFor`.**
  - Build the schema as ordered Go values, then marshal them with
    `json.Deterministic(true)`.
  - Recognize `Opt` and `Nullable` by unexported marker methods
    (`optional()`, and the existing `IsNullable`), never by type name.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./op ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `op: derive JSON Schema from Go types as the codec reads them`.

---

### Task 3: `op.Define`, one function for every door

**Files:**
- Create: `op/define.go`
- Modify: `op/op.go` (`Descriptor.InQuery`), `op/messages.go`,
  `docs/site/reference/op.md` (a "Define" section)
- Test: `op/define_test.go`

**Interfaces:**
- Consumes: `schemaFor` and `None` (Task 2), `HumanOnly` (Task 1),
  `CheckFlags`, `DecodeInput`, `Check`.
- Produces:

```go
type Spec struct {
	Description string // first sentence is MCP's title
	Confirm     string
	Access      rastrillo.Access
	Destructive bool
	Idempotent  bool
	Scoped      bool   // rows belong to one person: Check needs a subject
	InQuery     bool   // a read the API serves at GET, input in the query
	HumanOnly   string // why only a person may call it
}

type Named[In, Out any] struct {
	Descriptor Descriptor
	// unexported: run
}

func Define[In, Out any](
	id string, spec Spec,
	run func(context.Context, *rastrillo.Ctx, In) (Out, error),
) *Named[In, Out]

func (named *Named[In, Out]) Call(
	ctx context.Context, rc *rastrillo.Ctx, input In,
) (Out, error)
```

  - `Descriptor.InQuery bool` is new.
  - The descriptor `Define` builds has:
    - `Operation{ID, Resource: <area>, Scoped, Access, HumanOnly}`;
    - `Exposed: true`;
    - the spec's text and flags;
    - `Input: schemaFor(In, input)` and `Result: schemaFor(Out, output)`;
    - an `Invoke` that runs `DecodeInput` into a fresh `In`, then
      `Call`. It returns `nil` for `None`.
  - `Call` runs `op.Check`, then `run`.
  - `Define` panics with `"op: " + id + ": " + <copy>` on:
    - a bad ID (`msgBadID`, unprefixed);
    - a tool name over 64 characters;
    - an empty `Description`;
    - an invalid `Access`;
    - a nil `run`;
    - an `In` that is not a struct, or an `Out` that is not a struct or
      `None`;
    - a `schemaFor` error or a `CheckFlags` error;
    - `InQuery` on a write;
    - `InQuery` with an input property other than a string, integer,
      number or boolean.

    It panics, like `regexp.MustCompile`, because a bad declaration must
    stop the app before it serves.

- [ ] **Step 1: Stubs.**
  - `Spec` and `Named` as above.
  - A `Define` that returns `&Named{}` with a zero `Descriptor`.
  - A `Call` that returns `run`'s answer without checking.
  - `Descriptor.InQuery`.
- [ ] **Step 2: Write the tests.** The fixture is
  `create := op.Define("notes.create", op.Spec{Description: "Create a note.",
  Access: rastrillo.Write}, run)`, where `run` counts its calls.

```go
func TestDefineBuildsTheDescriptor(t *testing.T)
// ID "notes.create", Resource "notes", Access Write, Exposed true,
// Description, Input with property "title", Result with "id" and
// "title", Invoke non-nil.
func TestCallChecksBeforeItRuns(t *testing.T)
// Authorize refuses with op.Forbidden → that error, and run ran 0 times.
func TestInvokeDecodesWithTheAPIRules(t *testing.T)
// `{"title":"a","colour":1}` → op.Invalid with AsSent and "colour",
// run ran 0 times; `{"title":"a"}` → a note titled "a".
func TestAnOperationWithNoResultInvokesToNil(t *testing.T)
func TestSpecHumanOnlyReachesTheOperation(t *testing.T)
func TestDefineRefusesWhatWouldFailAtADoor(t *testing.T)
// Each case panics with a message containing the ID: "Notes.create",
// "notes", "notes.create.now", a 65-character tool name, an empty
// Description, a zero Access, a nil run, In = string, Out = int, a
// Destructive read, "notes.delete" without Destructive, InQuery on a
// write, InQuery with a []string input field.
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./op -run 'Define|Call|Invoke|NoResult|SpecHumanOnly'`
Expected: `--- FAIL` for each, for example "ID = "", want notes.create",
"run ran 1 times, want 0" and "Define did not panic".

- [ ] **Step 4: Implement.** Keep functions to 40 lines or fewer by
  moving the panics into
  `validateSpec(id string, spec Spec, input reflect.Type)`.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./op ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `op: Define declares a named operation every door calls`.

---

### Task 4: `api.Operations`, the API door for named operations

**Files:**
- Create: `api/operations.go`
- Modify:
  - `api/decode.go`: extract `readJSONBody`, which `Decode` then calls;
  - `api/tree.go`: an unexported `Route.readByPost`, and `add` accepts
    POST with Read only when it is set.
- Modify docs: `docs/site/reference/api.md` (`Operations`)
- Test: `api/operations_test.go` (package `api`)

**Interfaces:**
- Consumes: `op.Define` and `Descriptor.InQuery` (Task 3), and the gate
  order (Task 1).
- Produces:
  - `func Operations(descriptors ...op.Descriptor) []Route`. For ID
    `<area>.<verb>`, the route is POST, or GET with `InQuery`, at
    `/api/v1/<area>/<verb>`, with `readByPost` set for a POST read.
  - `func readJSONBody(response http.ResponseWriter, request *http.Request) ([]byte, bool)`
    holds `Decode`'s 415 and 413 rules, unchanged.
  - The named door, after the gate has run `Admit`:
    1. `op.Check(ctx, rc, descriptor.Operation)` comes first, so a
       hand-written `Invoke` that skips it still can't run for a refused
       caller (ruling).
    2. **POST:** `ContentLength == 0` is `{}`. Otherwise the door calls
       `readJSONBody`. The bytes go to `Invoke` unparsed, so
       `DecodeInput` alone classifies them, as it does for MCP.
       **GET:** `Invoke(queryArguments(request.URL.Query(), descriptor.Input))`.
    3. An error goes to `Refuse`, nil is `204`, and anything else is
       `Write(response, 200, value)`.
  - `func queryArguments(query url.Values, inputSchema json.RawMessage) []byte`
    builds one JSON object, with keys sorted and each key's values in
    the order sent:
    - a value whose property is an integer or number goes in raw when it
      is a valid JSON number literal;
    - a boolean's value goes in raw when it is `true` or `false`;
    - everything else, unknown keys included, is quoted.

    `DecodeInput` then refuses repeats, wrong types and unknown keys
    with its own messages.

- [ ] **Step 1: Stubs.**
  - An `Operations` that returns nil.
  - `readByPost`, unused.
  - A `queryArguments` that returns `{}`.
- [ ] **Step 2: Write the tests.** The fixtures are `op.Define`
  operations:
  - `notes.create` (write);
  - `notes.count` (read, POST);
  - `notes.list` (read, `InQuery`), whose input is
    `{search Opt[string]; limit Opt[int]; pinned Opt[bool]}` and which
    echoes its input;
  - `notes.delete` (write, `None`).

  Plus one hand-written descriptor, `notes.purge`: write, `Destructive`,
  `HumanOnly`, with an `Invoke` that counts calls and never calls
  `op.Check`. The tests use `mustNew(t, config(), Operations(...)...)`
  and `serve`.

```go
func TestOperationsRoutesEachOperationByItsName(t *testing.T)
// [POST /api/v1/notes/create, GET /api/v1/notes/list], with Op.IDs.
func TestAPostedOperationAnswersItsResult(t *testing.T)
// 200 {"id":1,"title":"Hello"}.
func TestAReadServedByPostTakesAReadOnlyToken(t *testing.T)
// The existing TestNewRefuses "POST with Read" still passes.
func TestAnOperationWithNoResultAnswers204(t *testing.T)
func TestAnEmptyPostIsAnEmptyObject(t *testing.T)
// No body, no Content-Type → run sees a zero input (422 on title).
func TestABodyWithoutJSONContentTypeIs415(t *testing.T)
func TestMalformedBodiesAreClassifiedByDecodeInput(t *testing.T)
// For `[]`, `{"title":"a","title":"b"}`, `{"title":null}` and
// `{"colour":1}`: the answer's code, message and fields equal those of
// api.Refusal(request, create.Descriptor.Invoke(ctx, rc, body)'s error).
// That is what MCP answers; Task 7's example compares the two doors
// end to end.
func TestQueryArgumentsAreRefusedByTheBodyRules(t *testing.T)
// ?search=hi&limit=5&pinned=true → 200, echoed with typed values.
// ?limit=five, ?limit=1&limit=2, ?pinned=yes and ?colour=red are each
// 422 with Fields holding exactly that key.
func TestTheDoorChecksBeforeAnUncheckedInvoke(t *testing.T)
// notes.purge as a session-less agent write token with valid {} →
// 403 "human_only" from the gate. As the person's own token, the same.
// Then with a gate Admit can't see (the Operation given to the route
// with HumanOnly cleared and Authorize refusing notes.purge): 403
// "forbidden". notes.purge's Invoke ran 0 times across all three.
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./api -run 'Operations|APosted|ARead|NoResult|EmptyPost|ABody|Malformed|Query|UncheckedInvoke'`
Expected: `--- FAIL` for each, for example "Operations = [], want 2
routes" and "answered 404".

- [ ] **Step 4: Implement**, with a why-comment on `readByPost`: the
  access comes from the declaration, which `Admit` and `op.Check` both
  enforce, and hand-built routes keep the stricter rule.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./api ./internal/docsite`
Expected: PASS, with every existing tree test still passing.

- [ ] **Step 6: Commit** with the subject
  `api: serve named operations at /api/v1/<area>/<verb>`.

---

### Task 5: Each door says what it serves

**Files:**
- Modify:
  - `api/tree.go`: the identity header, and `Served`;
  - `mcp/mcp.go`: `New` returns an unexported `endpoint` that embeds
    the wrapped handler and keeps the descriptors, plus `Served`.
- Modify docs: `docs/site/reference/api.md`, `docs/site/reference/mcp.md`
- Test: `api/served_test.go`, `mcp/served_test.go`

**Interfaces:**
- Produces:
  - `func Served(handler http.Handler) ([]Route, error)` in `api`. It
    returns the routes `api.New` registered, in order, with `Handle`
    nil. It errors when `handler` is not a value `api.New` returned.
  - `func Served(handler http.Handler) ([]op.Descriptor, error)` in
    `mcp`, the same for `mcp.New`.
  - The tree sets `Rastrillo-Operation: <Op.ID>` before `gate.serve`,
    when `matches(request)` finds a route. So a 401, a 405 for another
    method and every refusal carry it, and an outer catch-all can't fake
    a match.

- [ ] **Step 1: Stubs.** Both `Served` functions return `nil, nil`.
- [ ] **Step 2: Write the tests**

```go
// api
func TestServedListsWhatNewRegistered(t *testing.T)
// Served(mustNew(t, config(), getThing(ok), createThing(ok))) → two
// routes with their Method, Pattern and Op.ID. Served(http.NotFoundHandler())
// is an error.
func TestAMatchedRequestNamesItsOperationEvenWhenRefused(t *testing.T)
// No token → 401 with Rastrillo-Operation "things.get"; an unknown path
// → 404 with no header.

// mcp
func TestServedListsEveryDescriptorNewTook(t *testing.T)
// Served of mcp.New(testConfig(), bookmarks()...) → the five IDs in
// order. Served(http.NotFoundHandler()) is an error.
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./api ./mcp -run 'Served|NamesItsOperation'`
Expected: `--- FAIL`: "Served = [], want 2 routes" and "header "", want
things.get".

- [ ] **Step 4: Implement.** Check that every existing `mcp` test still
  passes with the new return type.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./api ./mcp ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `api, mcp: report what each door serves, and name the matched operation`.

---

### Task 6: The route recorder over chi and `ServeMux`

**Files:**
- Create: `route/route.go`, `route/table.go`, `route/messages.go`
- Create docs: `docs/site/reference/route.md`, with
  `"route": "reference/route"` in `referencePages` and a nav entry after
  `reference/mcp`
- Test: `route/table_test.go`

**Interfaces:**
- Consumes: `op.Descriptor`.
- Produces:

```go
type Register func(method, path string, handler http.Handler)
func Chi(router chi.Router) Register   // router.Method(method, path, h)
func Mux(mux *http.ServeMux) Register  // mux.Handle(method+" "+path, h)

type Mark struct{ /* operations []string; pageOnly string */ }
func Operation(descriptor op.Descriptor, more ...op.Descriptor) Mark
func PageOnly(reason string) Mark
func (mark Mark) Operations() []string
func (mark Mark) Reason() string

type Route struct {
	Method, Path string
	Operations   []string
	PageOnly     string
}

type Table struct{ /* register, prefix, shared list and twice-check */ }
func New(register Register) *Table
func (table *Table) On(register Register, prefix string) *Table
func (table *Table) Handle(pattern string, handler http.Handler, mark Mark)
func (table *Table) HandleFunc(pattern string,
	handler func(http.ResponseWriter, *http.Request), mark Mark)
func (table *Table) List() []Route
```

  - A pattern is `"<METHOD> <path>"`. The method must be GET, POST, PUT,
    PATCH or DELETE (`msgNoMethod`, `msgUnknownMethod`).
  - `Handle` registers `path` through `register` and records
    `prefix + path`.
  - `On` shares the list and the twice-check, which keys on the method
    and the full path with each `{name}` as `{}` (`msgTwice`).
  - `Operation` panics on an empty ID, and `PageOnly` on a blank reason.
  - A zero `Mark` is recorded as it is, for `paritytest` to refuse.
  - `List` returns a deep copy.

- [ ] **Step 1: Stubs.** Every type and function above, with
  `Handle` doing nothing, `List` returning nil, and the mark
  constructors returning zero marks.
- [ ] **Step 2: Write the tests**

```go
func TestATableOverChiServesAndRecords(t *testing.T)
// POST /notes reaches h; List() == [{POST /notes [notes.create] ""}].
func TestATableOverAServeMuxServesAndRecords(t *testing.T)
// The same, plus GET /style.css with PageOnly("the stylesheet").
func TestATableOnASubRouterRecordsTheFullPath(t *testing.T)
// Inside router.Route("/admin", ...): table.On(route.Chi(sub), "/admin")
// .HandleFunc("GET /people", ...) → served at /admin/people, recorded
// as "/admin/people".
func TestRegisteringAMethodAndPathTwicePanics(t *testing.T)
// "POST /notes/{id}" then "POST /notes/{key}" → the msgTwice panic.
func TestAPatternMustNameAKnownMethod(t *testing.T)
func TestAMarkMustSaySomething(t *testing.T)
func TestAZeroMarkIsRecordedForTheParityCheck(t *testing.T)
func TestListReturnsACopy(t *testing.T)
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./route`
Expected: `--- FAIL` for each, for example "List() = [], want 1 route"
and "did not panic".

- [ ] **Step 4: Implement** the package and its reference page.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./route ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `route: record each route with its operation, over chi or ServeMux`.

---

### Task 7: `paritytest.Routes`, complete inventories, and the example app

**Files:**
- Create:
  - `paritytest/paritytest.go`: `Surface`, `Routes` and the route
    checks;
  - `paritytest/inventory.go`: the door checks;
  - `paritytest/doors.go`;
  - `paritytest/messages.go`.
- Create tests:
  - `paritytest/example_app_test.go`: the app and its broken variants;
  - `paritytest/example_test.go`;
  - `paritytest/routes_test.go`.
- Create docs: `docs/site/reference/paritytest.md`, with
  `referencePages` and nav entries after `reference/route`

**Interfaces:**
- Consumes: `route` (Task 6), `api.Operations` (Task 4), `api.Served`,
  `mcp.Served` and the identity header (Task 5), and `op.Define`
  (Task 3).
- Produces:

```go
type Surface struct {
	Routes     []route.Route   // Table.List()
	Operations []op.Descriptor // every operation the app declares
	Generated  []op.Descriptor // gen.Operations(): gen/paritytest's job,
	                           // exempt from the page check
	API        http.Handler    // exactly what api.New returned
	MCP        http.Handler    // exactly what mcp.New returned
	Handler    http.Handler    // the whole app, both mounted
	Token      string          // an agent's write token for a person
	                           // whom Authorize allows everything
}
func Routes(t testing.TB, surface Surface)

type Outcome struct {
	Status int
	Kind   string         // "ok" for 2xx, else the error's code
	Fields []string       // an invalid answer's field names, sorted
	Body   map[string]any // numbers as json.Number
}
func CallAPI(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome
func CallTool(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome
func ToolNames(t testing.TB, surface Surface) []string
```

  - `Routes` reports every problem with `t.Errorf`, using the approved
    copy, and never stops early. Its checks:
    1. **Page table:** each route is marked (`msgUnmarked`) and names
       only declared operations (`msgUndeclared`).
    2. **Declarations:** `Operations` lists no ID twice
       (`msgListedTwice`), and each declared operation has a page
       (`msgNoPage`).
    3. **API inventory** (`api.Served(API)`):
       - each declared operation is served at its expected method and
         pattern (`msgMissing`);
       - no other operation sits at that pattern (`msgWrongOperation`);
       - each is served with an `Operation` equal to the declared one
         (`msgDiffers`);
       - every served route's operation is in `Operations` or
         `Generated` (`msgUnexpected`).
    4. **MCP inventory** (`mcp.Served(MCP)`): the same checks for
       missing, differing and unexpected descriptors.
    5. **Mounted:** an unauthenticated request to each declared
       operation's path through `Handler` answers 401 with
       `Rastrillo-Operation` equal to its ID (`msgNotMounted`). A
       catch-all or outer middleware sets no such header.
    6. **Listed:** `ToolNames` with `Token` contains each operation that
       is not human-only (`msgNotListed`), and no human-only one
       (`msgHumanListed`).
    7. **Human-only:** `CallAPI` and `CallTool` with `Token` both answer
       `human_only` (`msgHumanCallable`).
  - `CallAPI` sends `Authorization: Bearer <Token>` and `Agent-Session:
    paritytest`, as a JSON POST, or a GET with a query for `InQuery`.
    `CallTool` posts a stateless `tools/call` to `/api/v1/mcp`. All
    three go through `httptest.NewServer`.

- [ ] **Step 1: Stubs.** Every name above, with `Routes` reporting
  nothing and the call functions returning a zero `Outcome`.
- [ ] **Step 2: Write the example app** (`example_app_test.go`).

  `buildExample(t, router, options) (paritytest.Surface, *exampleApp)`
  takes `router` `"chi"` or `"ServeMux"`.

  **Operations,** made with `op.Define` in the constructor, over notes
  kept in memory behind a mutex:
  - `notes.create`: write. A blank title is
    `Invalid{"Title": "Enter a title."}`.
  - `notes.list`: read, `InQuery`.
  - `notes.delete`: write, `Destructive`, `Idempotent`, `None`. A note
    that isn't there is `NotFound`.
  - `tokens.create`: write, `HumanOnly`.

  **Policy:** `Authorize` refuses every write by person `b`, a viewer,
  with `op.Forbidden`.

  **Pages,** through one `route.Table`:
  - `GET /notes` names `notes.list`;
  - `POST /notes` names `notes.create`;
  - `POST /notes/{id}/delete` names `notes.delete`;
  - `POST /settings/tokens` names `tokens.create`;
  - `GET /style.css` is `PageOnly("the stylesheet")`.

  Each page reads the form and calls `Call`. It answers 303, 422 with one
  Go field name per line, 403 or 404. A page's actor is the session
  person named by the test's `X-Example-Person` header.

  **Doors:**
  - `api.New(config, api.Operations(app.operations()...)...)` and
    `mcp.New(..., app.operations()...)`, mounted at `/api/v1/mcp` and
    `/api/v1/`.
  - A map verifier holds tokens `writer-a`, `writer-b`, `reader-a`
    (agent tokens) and `person-a` (no agent name).
  - `RequireAgentSession` is set.

  **Broken variants,** from `exampleOptions`, one field per variant:
  - `apiLeavesOut`, `mcpLeavesOut` and `pageLeavesOut` each name an
    operation ID that door doesn't get.
  - `apiExtra` and `mcpExtra` add the undeclared write `notes.export`
    to that door.
  - `wrongOperation` puts a hand-built `api.Route` for
    `notes.create`'s descriptor at `POST /api/v1/notes/delete`, and
    leaves `notes.delete` out.
  - `catchAll` mounts the API at `/api/v2/` and answers 401 to
    everything under `/api/v1/` except MCP.
  - `humanOnlyCallable` gives both doors a separate unsafe descriptor
    for `tokens.create`: the declared one's fields with `HumanOnly` ""
    and an `Invoke` that mints directly, never through `Call`.
  - `extraMark` registers `GET /extra` with that mark.
  - `pageSkipsValidation` makes the `POST /notes` page store the title
    directly, without `Call`. Task 8 uses it.
- [ ] **Step 3: Write the tests**

```go
// example_test.go; each runs for "chi" and "ServeMux".
func TestTheExampleAppPassesTheParityCheck(t *testing.T)
func TestAReadTokenListsOnlyReadTools(t *testing.T)
// ToolNames as reader-a == ["notes_list"]; as writer-a ==
// ["notes_create","notes_delete","notes_list"]. (Spec §5.6.)
func TestAHumanOnlyOperationRefusesEveryTokenWhateverElseIsWrong(t *testing.T)
// tokens.create through CallAPI and CallTool: as writer-a; as
// person-a; as reader-a; as writer-a with no Agent-Session (a raw
// request); and with input {"agent": 5}. Each is Kind "human_only",
// and the app minted nothing. The page as session person a → 303, and
// one token minted.
func TestMalformedInputIsAnsweredAlikeByTheAPIAndMCP(t *testing.T)
// For {"title":null}, {"colour":1}, {"title":5}, and through raw
// requests `{"title":"a","title":"b"}`: CallAPI and CallTool on
// notes.create give the same Kind and Fields.

// routes_test.go; spy embeds testing.TB and collects Errorf.
// Each asserts the exact formatted messages.
func TestParityFailsARouteWithNoMark(t *testing.T)               // extraMark: Mark{}
func TestParityFailsARouteNamingAnUnlistedOperation(t *testing.T)
func TestParityFailsAnOperationWithNoPage(t *testing.T)           // pageLeavesOut
// The LeavesOut variants leave out notes.delete.
func TestParityFailsAnOperationMissingFromTheAPI(t *testing.T)    // msgMissing, msgNotMounted (404)
func TestParityFailsAnOperationMissingFromMCP(t *testing.T)       // msgMissing, msgNotListed
func TestParityFailsAnAgentOnlyOperationOnEitherDoor(t *testing.T) // apiExtra, mcpExtra → msgUnexpected
func TestParityFailsTheWrongOperationAtAPath(t *testing.T)
// msgWrongOperation, and msgNotMounted (header "notes.create")
func TestParityFailsAnAPIBehindACatchAll(t *testing.T)
// msgNotMounted for each of the four operations, and msgHumanCallable
// for the API, which answered "unauthorized"
func TestParityFailsAHumanOnlyOperationAnAgentCanCall(t *testing.T)
// First, without Routes: CallAPI on the unsafe variant mints one token,
// so the fixture really executes. Then Routes reports msgDiffers for
// the API and MCP, msgHumanListed, and msgHumanCallable for "the API"
// and for "MCP".
func TestParityReportsEveryProblemInOneRun(t *testing.T)
```

- [ ] **Step 4: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./paritytest`
Expected: `--- FAIL` for each. The stub reports nothing, and a zero
`Outcome` has Kind "".

- [ ] **Step 5: Implement** the package and its reference page.
- [ ] **Step 6: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./paritytest ./internal/docsite`
Expected: PASS.

- [ ] **Step 7: Commit** with the subject
  `paritytest: check routes and door inventories, with an example app`.

---

### Task 8: `paritytest.Run`, one scenario through three doors

**Files:**
- Create: `paritytest/scenario.go`
- Modify: `paritytest/messages.go`, `docs/site/reference/paritytest.md`
- Test: `paritytest/scenario_test.go`

**Interfaces:**
- Consumes: `CallAPI`, `CallTool` and the example app (Task 7).
- Produces:

```go
type Fixture interface {
	Surface() Surface
	Token(t testing.TB, person string) string
	// Page does step as person through the app's own pages and reports
	// it as an Outcome (Kind and Fields; Body may be nil).
	Page(t testing.TB, person string, step Step) Outcome
	// State is what the app has stored, without ids and times, which
	// differ between fresh fixtures by design.
	State(t testing.TB) any
}
type Step struct {
	Name      string
	Person    string
	Operation op.Descriptor
	Input     map[string]any
}
type Scenario struct {
	Fresh func(t testing.TB) Fixture
	Steps []Step
}
func Run(t testing.TB, scenario Scenario)
```

  - `Run` builds three fresh fixtures, one per door, and runs every step
    on each. The API and MCP use `Token(person)`.
  - After each step it compares:
    - Kind and Fields across all three doors (`msgDoorsDiffer`);
    - Body between the API and MCP (`msgBodiesDiffer`);
    - `State` across all three (`msgStateDiffers`).
  - A step naming a human-only operation fails with
    `msgScenarioHumanOnly`, because the doors differ for it by design.
  - It reports everything with `Errorf` and never stops early.

- [ ] **Step 1: Stubs.** The types, and a `Run` that does nothing.
- [ ] **Step 2: Write the tests.** `exampleFixture` implements `Fixture`
  over `buildExample`. Its `Page` posts the step's input as a form, with
  `X-Example-Person`, and maps 303 to ok, 422 to invalid (field names
  through `api.FieldName`), 403 to forbidden and 404 to not_found. Its
  `State` is the sorted list of note titles.

```go
// The example scenario: a creates "Hello"; a creates "" (invalid,
// title); b creates "Hi" (forbidden); a lists; a deletes note 1; a
// deletes note 1 again (not_found).
func TestTheExampleScenarioEndsAlikeOnEveryDoor(t *testing.T) // chi and ServeMux
func TestRunCatchesAPageThatSkipsTheOperation(t *testing.T)
// pageSkipsValidation: the spy has msgDoorsDiffer for the step
// "a creates nothing" (page ok, API invalid) and msgStateDiffers
// after it.
func TestRunRefusesAHumanOnlyStep(t *testing.T)
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./paritytest -run 'Scenario|RunCatches|RunRefuses'`
Expected: `--- FAIL` for the two failure tests, because the stub reports
nothing. The passing-scenario test passes against the stub. Note that
in the discussion, and show it failing against `pageSkipsValidation`
instead.

- [ ] **Step 4: Implement** `Run`, and add it to the reference page.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./paritytest ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `paritytest: Run drives one scenario through page, API and MCP`.

---

### Task 9: `action.Routes` marks, and `routes --check` learns the rule

**Files:**
- Modify: `action/routes.go`. `Get`, `Post` and `Handle` take
  `marks ...route.Mark`, and `Route` gains `Operations []string` and
  `PageOnly string`.
- Create: `internal/routelist/unmarked.go` and `declares.go`. Modify
  `messages.go` and `format.go`.
- Modify: `cmd/rastrillo/routes.go` (`listRoutes`)
- Create fixture: `internal/routelist/testdata/declares/`, a module
  like `testdata/app`. One package calls `op.Define`. Its `Routes` marks
  one route with `route.Operation`, one with `route.PageOnly`, and
  leaves one unmarked.
- Modify docs: `docs/site/cli.md` (`--check` and the rule),
  `docs/site/reference/action.md`
- Test: `action/routes_marks_test.go`,
  `internal/routelist/unmarked_test.go`, `cmd/rastrillo/routes_test.go`

**Interfaces:**
- Consumes: `route.Mark`, with `Operations()` and `Reason()`.
- Produces:
  - `Get(path Path, handler Handler, marks ...route.Mark)`, the same
    for `Post`, and `Handle(method string, path Path, handler
    http.Handler, name string, marks ...route.Mark)`. More than one
    mark panics.
  - `func Unmarked(routes []action.Route) []action.Route` returns the
    routes with neither mark. It skips `Mount` lines (`Method == "*"`).
  - `func DeclaresOperations(ctx context.Context, target Target) (bool, error)`
    is true when a package in `go list ./...` calls `Define` through an
    import of `amadan.net/rastrillo/rastrillo/op`. It uses the same
    parsed packages `Unmounted` reads.
  - `func FormatUnmarked(routes []action.Route) string`.
  - `routes --check`: after the unmounted check, when
    `DeclaresOperations` is true and `Unmarked` is non-empty, exit 1
    with `FormatUnmarked` (ruling). There is no new flag.

- [ ] **Step 1: Stubs.**
  - The variadic parameters, unused.
  - The new `Route` fields.
  - An `Unmarked` that returns nil, and a `DeclaresOperations` that
    returns false.
  - `FormatUnmarked`.
- [ ] **Step 2: Write the tests**

```go
func TestAMarkIsRecordedWithTheRoute(t *testing.T)
// recorder.Post("/notes", handlerA, route.Operation(create)) and
// recorder.Get("/style.css", handlerB, route.PageOnly("the stylesheet"))
// → recorded Operations ["notes.create"] and PageOnly.
func TestARouteWithNoMarkRegistersAsBefore(t *testing.T)
func TestTwoMarksOnOneRoutePanics(t *testing.T)
func TestUnmarkedListsRoutesWithNeitherMark(t *testing.T)
// [{GET /a [x]}, {POST /b}, {* /api/*}, {GET /c PageOnly r}] → [{POST /b}].
func TestDeclaresOperationsFindsACallToDefine(t *testing.T)
// testdata/declares → true; testdata/app → false.

// cmd/rastrillo/routes_test.go, with the built binary.
func TestRoutesCheckAppliesTheRuleOnlyWhereOperationsAreDeclared(t *testing.T)
// routes --check testdata/declares → status 1, stderr holds
// FormatUnmarked of the one unmarked route; routes --check
// testdata/app, whose routes carry no marks → status 0.
```

- [ ] **Step 3: Run them against the stubs**

Run: `GOFLAGS=-mod=mod go test ./action ./internal/routelist -run 'Mark|Unmarked|Declares' && GOFLAGS=-mod=mod go test ./cmd/rastrillo -run TestRoutesCheckApplies`
Expected: `--- FAIL` for each new test except
`TestARouteWithNoMarkRegistersAsBefore`, which already works and says
so.

- [ ] **Step 4: Implement**, with the CLI docs.
- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./action ./internal/routelist ./internal/docsite && GOFLAGS=-mod=mod go test ./cmd/rastrillo -run 'TestRoutes'`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `routes --check: every route names an operation once an app declares one`.

---

### Task 10: Guide, skill, CHANGELOG, and the gates

**Files:**
- Create: `docs/site/operations.md`, the "Named operations and parity"
  guide, with a nav entry after `mcp`.
- Create: `skill/operations.md`, 9,000 bytes or fewer.
- Modify:
  - `SKILL.md`: the API row names `skill/operations.md`, reworded so the
    file stays at 9,700 bytes or fewer. It is at 9,694 now.
  - `docs/site/api.md`: `human_only` in the error table, the new order,
    the identity header, and a pointer to the guide.
  - `docs/site/mcp.md`: human-only tools are hidden and refused, and a
    read token sees only reads.
  - `CHANGELOG.md`

**The guide covers, in order:**
- **Declaring an operation:**
  - `op.Define` and `Spec`;
  - wire names follow the Go names, and `Invalid.Fields` is keyed by
    the Go name;
  - input and output schema rules;
  - `Scoped`, and why an unscoped operation needs `Authorize` for
    tokens.
- **The page calls `Call`**, in four beats.
- **Serving it:** `api.Operations` and `mcp.New` with the same
  descriptors.
- **POST and GET:** `InQuery`, 204 for `op.None`, and an empty POST is
  `{}`.
- **The refusal order:** human-only first, and why every token is
  refused.
- **The route table:** `route.New`, `On` and marks.
- **`paritytest.Routes`:** what each failure means, the token it needs,
  `Generated`, and importing it beside `gen/paritytest` with an alias.
- **`paritytest.Run`:** writing a fixture and a scenario.
- **What it doesn't catch:** routes registered outside the table, and
  writes an operation doesn't make.
- **`routes --check`** for `action.Routes` apps, once they declare an
  operation.

**The CHANGELOG** gets `## Unreleased`:
- a lead line: apps that don't call `op.Define` change nothing, except
  the precedence below;
- `### Changed`: a read-only agent's write with no `Agent-Session` is
  now `forbidden`, not `invalid`; and API answers to a matched route
  carry `Rastrillo-Operation`;
- `### Added`: named operations, `api.Operations`, `api.Served` and
  `mcp.Served`, `route`, `paritytest` (`Routes` and `Run`), human-only
  operations and `human_only`, and the `routes --check` rule.

- [ ] **Step 1: Write the guide, the skill topic, the SKILL.md row, the
  docs edits and the CHANGELOG.**
- [ ] **Step 2: Have Luna review the prose.** Send every new or changed
  paragraph through Task 0's command with
  `-o "$TMPDIR/luna-0a-docs.md"`. Apply the corrections unless one is
  factually wrong.
- [ ] **Step 3: Run the docs and skill tests**

Run: `GOFLAGS=-mod=mod go test ./internal/docsite && GOFLAGS=-mod=mod go test . -run 'TestSkill'`
Expected: PASS, with `SKILL.md` at 9,700 bytes or fewer.

- [ ] **Step 4: Run the budget gates**

Run: `GOFLAGS=-mod=mod go run ./cmd/rastrillo budget size && GOFLAGS=-mod=mod go run ./cmd/rastrillo budget code && make gofmt`
Expected: each exits 0, and `.rastrillo/` is unchanged.

- [ ] **Step 5: Commit** with the subject
  `docs: named operations, the route table and the parity check`.
- [ ] **Step 6: Push and let the fleet run the gate.**
  - Merge `origin/main` first if it moved.
  - Push `agent-parity` and wait for `make ci` on the fleet.
  - Then follow `CONTRIBUTING.md`: a cross-provider review (Astra), and
    a branch discussion with the fail-first runs and their stubs, the
    Luna replies and what was applied.

  Expected: the fleet's gate is green.
