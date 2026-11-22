# Named operations and the parity check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an app declare any action once, in Go, as a named operation
that its page, `/api/v1` and MCP all call, and give it a test helper that
fails when a route has no agent twin, when an operation is missing from a
door, or when a human-only operation answers an agent.

**Architecture:**
- `op.Define` takes an operation's Go input and result types and its
  function. It derives the JSON Schemas and builds the `op.Descriptor`.
  `Call` runs `op.Check` and then the function. This is the one code path
  every door shares.
- `api.Operations` turns descriptors into `/api/v1/<area>/<verb>` routes.
  `mcp.New` takes the same descriptors unchanged.
- A new `route` package wraps chi or `ServeMux` registration and records
  each route with its operation or a page-only reason. The new
  `paritytest` package checks that record against the live API and MCP.
- A human-only operation carries its reason on `rastrillo.Operation`.
  `op.Check` refuses every token with a new `op.HumanOnly` error. That
  error hides the tool from MCP's list, and both doors answer it with a
  new `human_only` code.

**Tech Stack:** Go 1.26, `github.com/go-json-experiment/json` (v2, already
the wire codec), `github.com/go-chi/chi/v5` and `net/http`'s `ServeMux`,
and the MCP Go SDK through the existing `mcp` package.

**Spec:** `docs/superpowers/specs/2026-11-22-agent-parity-design.md`
§5.1, §5.2, §5.6 ("tools by access") and §5.8. Read with
`docs/superpowers/specs/2026-10-05-json-api-design.md` §6 (wire and
errors) and `docs/superpowers/specs/2026-10-25-mcp-design.md` §4–§5
(flags, fence, tool list).

**Not in this plan:** grants (§5.3, plan 1), OAuth (§5.5), plaza and
Patrix (Patrix content is plan 0b, written after this releases), manifest
or generator changes.

## Global Constraints

- **No new module dependencies.** `go.mod`'s require list stays as it is.
  Schemas are derived by Rastrillo's own code, not `jsonschema-go`. That
  library reads v1 tags, while the wire is json v2, and it can't see
  through `op.Opt[T]`.
- **Hand-written, no codegen.** Add nothing to `internal/generate`, and no
  manifest steps: manifests are being deprecated.
- **Backwards compatible.** Every existing caller of `api.New`,
  `api.Route`, `mcp.New`, `op.Check` and `action.Routes` compiles and
  behaves as before. The two exceptions are the new `human_only` answer
  and the optional variadic marks.
- **Names:**
  - An operation ID is `<area>.<verb>` and matches
    `^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`.
  - Its tool name is `<area>_<verb>` and is at most 64 characters.
  - Its API route is `POST /api/v1/<area>/<verb>`, or
    `GET /api/v1/<area>/<verb>` for a read with `Spec.InQuery`.
- **Wire names:**
  - Every exported field of an input or result type has a json tag
    naming it.
  - That name equals `internal/wirename.Name(<Go field name>)`.
  - So `op.Invalid.Fields` keyed by the Go field name, as `skill/code.md`
    writes them, comes out under the right snake_case name at the API
    (`api.FieldName`), and a form error and a 422 name the same field.
- **Errors** follow the JSON API spec §6. There is one new code:
  - `human_only` answers 403 for `op.HumanOnly`.
  - Every other refusal keeps its code.
- **Human-only means no token.** `op.Check` refuses an operation whose
  `HumanOnly` reason is set when the actor has a credential or an agent
  name. A person's browser session and the app's zero actor pass.
- **Code** follows `skill/code.md`:
  - names spelled out except `err`, `ctx`, `t`;
  - 80 columns;
  - non-test files of 400 lines or fewer, and non-test functions of 40
    lines or fewer;
  - comments that say why;
  - tests named as sentences.
- `go run ./cmd/rastrillo budget size` and
  `go run ./cmd/rastrillo budget code` pass at the root with no new record
  in `.rastrillo/code.txt` or `.rastrillo/budgets.txt`.
- **Running things:**
  - Run git and go with the Bash sandbox disabled and `GOFLAGS=-mod=mod`.
  - Run targeted tests locally (`go test ./op -run TestX`). The fleet runs
    the full gate (`make ci`) on push. Don't run whole suites locally.
- **TDD, fail-first:**
  - Every test is run against the code before its change, and seen to
    fail, as `CONTRIBUTING.md` requires.
  - Keep both runs' output for the branch description.
- **Copy:**
  - Every user-facing string is the Task 0 approved copy, verbatim. That
    covers error messages, panics, test-failure messages and the CLI
    flag.
  - Strings live in named constants, and tests compare against the
    constants, not literals.
  - Docs prose goes through Luna in Task 8.
- **Commits:** subjects of 79 characters or fewer, imperative, a body that
  says why, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- **A required input member that is left out** (for example `{}` to
  `notes.create`) reaches the operation as its zero value, because
  `op.DecodeInput` doesn't enforce `required`. People expect the
  operation's own validation to refuse it, with the same field on the
  page, the API and MCP. Task 6:
  `TestAnEmptyCreateIsRefusedAlikeOnEveryDoor`.
- **A person's own token with no agent name** (`apitoken`'s `ForSelf`)
  calling a human-only operation. An agent can hold such a token, so
  people expect a refusal exactly as for an agent. Task 1:
  `TestCheckRefusesEveryTokenOnAHumanOnlyOperation`.
- **A GET read sent a repeated, ill-typed or unknown query parameter.**
  People expect a 422 naming that parameter, with the message the body
  rules give, not a 500 and not a silent zero. Task 4:
  `TestQueryArgumentsAreRefusedByTheBodyRules`.
- **A POST with no body** to an operation whose inputs are all optional.
  People expect it to run as `{}`, the way MCP treats absent arguments.
  A POST with a body but no JSON content type is still 415. Task 4:
  `TestAnEmptyPostIsAnEmptyObject` and
  `TestABodyWithoutJSONContentTypeIs415`.
- **A route registered inside a chi sub-router** must be recorded under
  its full path. The same method and path registered twice must stop the
  app's start: chi would silently keep the later one, and the parity
  check would pass a route that never runs. Task 5:
  `TestATableOnASubRouterRecordsTheFullPath` and
  `TestRegisteringAMethodAndPathTwicePanics`.

## Files

| Path | Responsibility |
|---|---|
| `ctx.go` (modify) | `Operation.HumanOnly` |
| `op/op.go`, `op/check.go` (modify) | `HumanOnly` refusal; checked first in `Check`; `Descriptor.InQuery` |
| `op/schema.go` (create) | `schemaFor`: JSON Schema from a Go type, by json v2 tags |
| `op/define.go` (create) | `Spec`, `None`, `Named`, `Define`, `Call` |
| `op/messages.go` (create) | `Define`'s and `schemaFor`'s panic and error copy |
| `api/errors.go` (modify) | `human_only` code and its `Refusal` case |
| `api/decode.go` (modify) | split out the shared body read |
| `api/tree.go` (modify) | POST may carry `Read` on a route `Operations` built |
| `api/operations.go` (create) | `Operations`, the named door, query arguments |
| `mcp/list.go` (modify) | `refused` counts `op.HumanOnly` |
| `route/route.go` (create) | `Register`, `Chi`, `Mux`, `Mark`, `Operation`, `PageOnly`, `Route` |
| `route/table.go` (create) | `Table`, `New`, `On`, `Handle`, `HandleFunc`, `List` |
| `paritytest/paritytest.go` (create) | `Surface`, `Routes` |
| `paritytest/doors.go` (create) | `Outcome`, `CallAPI`, `CallTool`, `ToolNames` |
| `paritytest/messages.go` (create) | the check's failure copy |
| `paritytest/*_test.go` (create) | the example app over chi and `ServeMux`, and its tests |
| `action/routes.go` (modify) | optional marks on `Get`, `Post`, `Handle`; recorded on `Route` |
| `internal/routelist/unmarked.go`, `messages.go`, `format.go` | `Unmarked`, `FormatUnmarked` |
| `cmd/rastrillo/routes.go` (modify) | `--parity` |
| docs, skill, CHANGELOG | Task 8, plus each task's reference-page lines |

---

### Task 0: Approved copy

**Files:**
- Modify: this plan's `## Approved copy` section below

- [ ] **Step 1: Send the drafts to Luna.** Write a prompt that holds:
  - the drafts below;
  - `AGENTS.md`'s "User-facing copy" section;
  - the reader of each string: an app developer for panics, check
    failures and the CLI, or an agent and its person for `human_only`.

  Then run, unsandboxed:
  `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o "$TMPDIR/luna-0a.md" - < "$TMPDIR/luna-0a-prompt.md"`
  Expected: `$TMPDIR/luna-0a.md` exists and holds a verdict for each
  string. A zero exit with no file means the model slug failed: read the
  log.
- [ ] **Step 2: Apply Luna's corrections.** Skip only a correction that is
  factually wrong, and note why. Keep every `%s`, `%q` and `%d` in the
  same order. Replace the drafts below with the approved strings, and
  record the date and anything skipped.
- [ ] **Step 3: Commit** with the subject
  `Plan 0a: approved copy for named operations and parity`.

## Approved copy

Drafts, to be replaced in Task 0. The name in brackets is the constant
each string lives in.

- **`api` code `human_only`, 403** [`codes["human_only"]`]:
  `Only a person can do this, in the app itself. Ask them to do it there.`
- **`op.Define` panics.** Each is prefixed with `op: <id>: ` by `Define`,
  except the first, which names the bad ID itself:
  - [`msgBadID`]: `%q is not an operation name. Write it as <area>.<verb> in lowercase letters, digits and underscores, such as posts.publish`
  - [`msgLongToolName`]: `its tool name %s is %d characters. Keep <area>_<verb> to 64 or fewer`
  - [`msgNoDescription`]: `Description is empty. Say in one sentence what the operation does, for the agent reading the tool list`
  - [`msgBadAccess`]: `set Spec.Access to rastrillo.Read or rastrillo.Write`
  - [`msgNilRun`]: `the function is nil. Pass the function that does the work`
  - [`msgInputNotStruct`]: `the input type %s must be a struct, because an agent sends a JSON object`
  - [`msgResultNotStruct`]: `the result type %s must be a struct, or op.None when there is nothing to return`
  - [`msgQueryWrite`]: `InQuery is only for reads. Remove it, or set Access to rastrillo.Read`
  - [`msgQueryNotFlat`]: `InQuery needs every input field to be a string, number or bool, but %s is not. Remove InQuery to take a JSON body`
- **`schemaFor` errors**, wrapped by the panic above:
  - [`msgUntagged`]: `field %s.%s has no JSON name. Tag it json:"%s"`
  - [`msgWireName`]: `field %s.%s is %q on the wire. Name it %q, its name in snake_case, so a form error and an API error name the same field`
  - [`msgUnsupported`]: `field %s.%s is a %s, which JSON can't carry. Use a string, number, bool, struct, slice, or map with string keys`
  - [`msgCycle`]: `type %s contains itself, which a JSON Schema here can't describe. Break the cycle with an ID`
- **`route` panics:**
  - [`msgNoMethod`]: `route: %q names no method. Write it as "POST /posts/{id}/publish"`
  - [`msgUnknownMethod`]: `route: %q: the method must be GET, POST, PUT, PATCH or DELETE`
  - [`msgTwice`]: `route: %s %s is registered twice. Register each method and path once, because chi serves only the later one`
  - [`msgNoOperation`]: `route: Operation needs a declared operation. Pass its Descriptor, such as posts.Publish.Descriptor`
  - [`msgNoReason`]: `route: PageOnly needs a reason. Say why an agent has no use for this route, such as "the stylesheet"`
- **`paritytest` failures:**
  - [`msgUnmarked`]: `%s %s names no operation and no page-only reason. Name its operation with route.Operation, or say why an agent has no use for it with route.PageOnly`
  - [`msgUndeclared`]: `%s %s names %s, which Surface.Operations doesn't list. Add it there, to api.Operations and to mcp.New`
  - [`msgListedTwice`]: `Surface.Operations lists %s twice`
  - [`msgNoPage`]: `%s has no page. Give it a route, so a person can do what an agent can`
  - [`msgNotOnAPI`]: `%s isn't on the API: %s %s answered %d without a token, not 401. Pass it to api.Operations`
  - [`msgNotOnMCP`]: `%s isn't in the MCP tool list. Pass its Descriptor to mcp.New, and check that Authorize allows the test's token`
  - [`msgHumanListed`]: `%s is human-only, but the MCP tool list shows it. Pass the same Descriptor to mcp.New as to Surface.Operations`
  - [`msgHumanCallable`]: `%s is human-only, but %s answered an agent with %q, not human_only`
- **`rastrillo routes --parity`:**
  - flag usage: `with --check, also fail a route that names no operation and no page-only reason`
  - [`unmarkedRoutes`]: `These routes name no operation and no page-only reason:\n\n%s\nName each one's operation with route.Operation, or say why an agent has no use for it with route.PageOnly.`

---

### Task 1: Human-only operations refuse every token

**Files:**
- Modify: `ctx.go:42-47` (`Operation`), `op/op.go` (refusals),
  `op/check.go:22-35`, `api/errors.go` (`codes`, `Refusal`),
  `mcp/list.go:101-105` (`refused`)
- Modify docs: `docs/site/reference/op.md`, `docs/site/reference/rastrillo.md`
  (Operation), `docs/site/reference/api.md` (codes), `skill/api.md`
  (the code list: add `human_only 403`)
- Test: `op/check_test.go`, `api/errors_test.go`, `mcp/list_test.go`,
  `mcp/call_test.go`

**Interfaces:**
- Produces:
  - `rastrillo.Operation.HumanOnly string`: the reason. It is non-empty
    when only a person may call the operation.
  - `op.HumanOnly struct{ ID string }`, whose `Error()` is
    `"human only: " + ID`.
  - `op.Check` returns `op.HumanOnly{ID: o.ID}` before any other check
    when `o.HumanOnly != ""` and `a.IsToken() || a.Agent != ""`.
  - The API code `human_only` (403), and `api.Refusal` maps
    `op.HumanOnly` to it.
  - MCP's tool list hides a tool whose check answers `op.HumanOnly`.

- [ ] **Step 1: Write the failing tests**

```go
// op/check_test.go
var tokensCreate = rastrillo.Operation{ID: "tokens.create",
	Resource: "tokens", Access: rastrillo.Write,
	HumanOnly: "an agent must never raise its own access"}

func TestCheckRefusesEveryTokenOnAHumanOnlyOperation(t *testing.T) {
	allowAll := func(context.Context, rastrillo.Actor,
		rastrillo.Operation) error { return nil }
	for _, actor := range []rastrillo.Actor{
		{Subject: "a", Agent: "Claude", Credential: "t1",
			Access: rastrillo.Write},
		{Subject: "a", Credential: "t2", Access: rastrillo.Write},
		{Subject: "a", Agent: "Claude", Access: rastrillo.Write},
		{Subject: "a", Agent: "Claude", Credential: "t3",
			Access: rastrillo.Read},
	} {
		err := op.Check(context.Background(),
			&rastrillo.Ctx{Actor: actor, Authorize: allowAll}, tokensCreate)
		var refused op.HumanOnly
		if !errors.As(err, &refused) || refused.ID != "tokens.create" {
			t.Errorf("%v: Check = %v, want op.HumanOnly", actor, err)
		}
	}
}

func TestCheckLetsAPersonAndTheAppCallAHumanOnlyOperation(t *testing.T) {
	for _, actor := range []rastrillo.Actor{
		{Subject: "a", Access: rastrillo.Write}, {},
	} {
		err := op.Check(context.Background(),
			&rastrillo.Ctx{Actor: actor}, tokensCreate)
		if err != nil {
			t.Errorf("%v: Check = %v, want nil", actor, err)
		}
	}
}
```

```go
// api/errors_test.go
func TestHumanOnlyIsA403WithItsOwnCode(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/x/y", nil)
	status, refused := Refusal(request, op.HumanOnly{ID: "tokens.create"})
	if status != http.StatusForbidden || refused.Code != "human_only" ||
		refused.Message != codes["human_only"].message {
		t.Errorf("Refusal = %d %+v", status, refused)
	}
}
```

```go
// mcp/list_test.go: bookmarks() with one more descriptor whose
// Operation.HumanOnly is set; writerToken as in TestListPerCaller.
func TestListHidesAHumanOnlyOperation(t *testing.T) {
	// toolNames(t, url, writerToken) does not contain "tokens_create"
	// and still contains every bookmarks tool.
}

// mcp/call_test.go
func TestCallingAHumanOnlyToolIsAHumanOnlyToolError(t *testing.T) {
	// callTool(..., "tokens_create", `{}`) as writerToken:
	// refusalOf(t, result).Code == "human_only", and the descriptor's
	// Invoke was never called.
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./op ./api ./mcp -run 'HumanOnly'`
Expected: FAIL. The build fails on `HumanOnly` (the unknown field and the
undefined type).

- [ ] **Step 3: Implement**
  - Add `HumanOnly string` to `rastrillo.Operation`, with a why-comment:
    the reason is kept for the docs and the parity check.
  - Add the `op.HumanOnly` refusal value type beside `Forbidden`.
  - Check it first in `op.Check`. It is a property of the operation and
    the credential, like the read-only check, so a lax policy can't
    override it. And it must win over the read-only check, so a
    read-only agent learns that no token will do.
  - Add `"human_only": {http.StatusForbidden, <approved copy>}` to
    `codes`, a `case errors.As(err, &humanOnly)` in `Refusal`, and the
    count in `Error`'s comment ("one of the thirteen").
  - Add `op.HumanOnly` to `refused` in `mcp/list.go`.

- [ ] **Step 4: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./op ./api ./mcp && GOFLAGS=-mod=mod go test ./internal/docsite -run TestExportedSymbolsAreDocumented`
Expected: PASS.

- [ ] **Step 5: Commit** with the subject
  `op: refuse every token on a human-only operation`.

---

### Task 2: JSON Schema from Go types

**Files:**
- Create: `op/schema.go`, `op/messages.go`
- Modify: `op/op.go` (`None`), `docs/site/reference/op.md`
- Test: `op/schema_test.go` (package `op`, because `schemaFor` is
  unexported)

**Interfaces:**
- Produces: `func schemaFor(goType reflect.Type) (json.RawMessage, error)`.
  Its rules, all from json v2's own reading of the tags:
  - A struct is `{"type":"object","properties":{...},"required":[...],"additionalProperties":false}`.
    `required` lists the fields in field order.
  - A field is required unless it is an `op.Opt[T]` or `op.Nullable[T]`,
    or is tagged `omitzero` or `omitempty`.
  - `json:",inline"` and an untagged embedded struct merge their
    properties in. `json:"-"` and unexported fields are skipped.
  - Every other exported field must have a json name equal to
    `wirename.Name(field.Name)`. Otherwise the error is `msgUntagged`
    or `msgWireName`.
  - `string` is `string`. `bool` is `boolean`. Every int and uint kind
    is `integer`. `float32` and `float64` are `number`.
  - `time.Time` is `{"type":"string","format":"date-time"}`.
  - `op.Money` is
    `{"type":"object","properties":{"amount":{"type":"integer"},"currency":{"type":"string"}},"required":["amount"],"additionalProperties":false}`.
  - `op.Opt[T]` is T's schema. `op.Nullable[T]` and `*T` are T's schema
    with `"null"` added to `type` (`["object","null"]`).
  - A slice or array is `{"type":"array","items":...}`, never with
    null: json v2 writes a nil slice as `[]`.
  - `map[string]T` is `{"type":"object","additionalProperties":<T>}`.
  - `any`, `json.RawMessage` and `jsontext.Value` are `{}`.
  - `op.None` is exactly `{"type":"null"}`.
  - These fail with `msgUnsupported`: chan, func, complex, unsafe
    pointer, a map whose key is not a string, and an interface other
    than `any`.
  - A type that contains itself fails with `msgCycle`.
  - `type None struct{}` is declared here, in `op/op.go`, beside the
    refusals, and documented in `docs/site/reference/op.md`.
- Consumes: `internal/wirename.Name`.

- [ ] **Step 1: Write the failing tests.** `sameJSON(t, got, want)`
  unmarshals both into `any` and compares with `reflect.DeepEqual`.

```go
func TestSchemaOfAnInputNamesEachMemberByItsTag(t *testing.T) {
	type input struct {
		Title  string          `json:"title"`
		Notes  Opt[string]     `json:"notes"`
		Price  Nullable[Money] `json:"price"`
		Count  int             `json:"count,omitzero"`
		Tags   []string        `json:"tags"`
		When   time.Time       `json:"when"`
		Parent *int64          `json:"parent"`
	}
	got, err := schemaFor(reflect.TypeFor[input]())
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, got, `{"type":"object","properties":{`+
		`"title":{"type":"string"},"notes":{"type":"string"},`+
		`"price":{"type":["object","null"],"properties":{`+
		`"amount":{"type":"integer"},"currency":{"type":"string"}},`+
		`"required":["amount"],"additionalProperties":false},`+
		`"count":{"type":"integer"},`+
		`"tags":{"type":"array","items":{"type":"string"}},`+
		`"when":{"type":"string","format":"date-time"},`+
		`"parent":{"type":["integer","null"]}},`+
		`"required":["title","tags","when","parent"],`+
		`"additionalProperties":false}`)
}

func TestSchemaMergesAnInlineMemberAndAnEmbeddedStruct(t *testing.T)
// type base struct{ ID int64 `json:"id"` }
// type args struct { base; Extra struct{ Note string `json:"note"` } `json:",inline"` }
// → properties id and note, required ["id","note"].

func TestSchemaOfNoneIsNull(t *testing.T)
// schemaFor(reflect.TypeFor[None]()) is exactly {"type":"null"}.

func TestSchemaRefusesAFieldWithNoJSONName(t *testing.T)
// struct{ Title string } → error == fmt.Sprintf(msgUntagged, <type>, "Title", "title").

func TestSchemaRefusesAWireNameThatIsNotItsSnakeCase(t *testing.T)
// struct{ IPAddress string `json:"ipaddress"` } → error ==
// fmt.Sprintf(msgWireName, <type>, "IPAddress", "ipaddress", "ip_address").

func TestSchemaRefusesWhatJSONCannotCarry(t *testing.T)
// One case each: chan int, func(), complex128, map[int]string,
// an interface with a method, and a struct that contains itself through
// a slice. Each returns an error, and none panics.

func TestEveryRequiredMemberIsOneTheEncoderWrites(t *testing.T)
// For the result type struct{ Items []string `json:"items"`; Next string
// `json:"next,omitzero"` }: json.Marshal of the zero value is
// {"items":[]}, and the schema's required list is exactly ["items"].
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./op -run 'TestSchema|TestEveryRequired'`
Expected: FAIL with `undefined: schemaFor`.

- [ ] **Step 3: Implement `schemaFor` in `op/schema.go`.**
  - Build the schema as ordered Go values, then marshal them with
    `json.Deterministic(true)`.
  - Recognize `Opt`, `Nullable`, `Money`, `None` and `time.Time` by
    `reflect.Type` identity, or for the generic two, by a small
    unexported marker method that each implements: `optional()`, and
    `IsNullable` for `Nullable`. Don't match on type names.
  - Keep the error constants in `op/messages.go`.

- [ ] **Step 4: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./op ./internal/docsite`
Expected: PASS.

- [ ] **Step 5: Commit** with the subject
  `op: derive JSON Schema from an operation's Go types`.

---

### Task 3: `op.Define`, one function for every door

**Files:**
- Create: `op/define.go`
- Modify: `op/op.go` (`Descriptor.InQuery`), `op/messages.go`
- Modify docs: `docs/site/reference/op.md` (a "Define" section: `Spec`,
  `None`, `Named`, `Call`, `InQuery`)
- Test: `op/define_test.go`

**Interfaces:**
- Consumes: `schemaFor` and `None` (Task 2), `HumanOnly` (Task 1),
  `CheckFlags`, `DecodeInput`, `Check`.
- Produces:

```go
type Spec struct {
	Description string // one sentence first: MCP's title is it
	Confirm     string
	Access      rastrillo.Access
	Destructive bool
	Idempotent  bool
	Scoped      bool   // rows belong to one person: Check needs a subject
	InQuery     bool   // a read the API serves at GET, input in the query
	HumanOnly   string // why only a person may call it; "" for agents too
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

  - `Descriptor.InQuery bool` is new. It is set only through `Spec`.
  - The descriptor `Define` builds has:
    - `Operation{ID: id, Resource: <area>, Scoped, Access, HumanOnly}`;
    - `Exposed: true`;
    - the `Spec`'s text and flags;
    - `Input: schemaFor(In)` and `Result: schemaFor(Out)`;
    - an `Invoke` that runs `DecodeInput` into a fresh `In`, then
      `Call`. It returns `nil` when `Out` is `None`, and the `Out`
      value otherwise.
  - `Call` runs `op.Check` with `named.Descriptor.Operation`, then
    `run`. It never calls `run` after a refusal.
  - `Define` panics with `"op: " + id + ": " + <approved copy>` when:
    - the ID fails the pattern (`msgBadID`, unprefixed);
    - the tool name is over 64 characters;
    - `Description` is empty;
    - `Access` is invalid;
    - `run` is nil;
    - `In` is not a struct, or `Out` is not a struct or `None`;
    - `schemaFor` fails;
    - `CheckFlags` fails;
    - `InQuery` is set on a write;
    - `InQuery` is set and an input property is anything but a string,
      integer, number or boolean (an `Opt` of one is fine).

    It panics, like `regexp.MustCompile`, because an operation is
    declared once at start-up, and a bad one must stop the app before it
    serves.

- [ ] **Step 1: Write the failing tests.** The fixture is
  `createInput{Title string `json:"title"`}` and
  `note{ID int64 `json:"id"`; Title string `json:"title"`}`, with
  `create := op.Define("notes.create", op.Spec{Description:
  "Create a note.", Access: rastrillo.Write}, run)`, where `run` counts
  its calls.

```go
func TestDefineBuildsTheDescriptor(t *testing.T)
// create.Descriptor: ID "notes.create", Resource "notes", Access Write,
// Exposed true, Description "Create a note.", Input has property
// "title", Result has properties "id" and "title", Invoke non-nil,
// HumanOnly "".

func TestCallChecksBeforeItRuns(t *testing.T)
// rc.Authorize returns op.Forbidden{Reason: "test"}:
// create.Call → errors.As Forbidden, and run was called 0 times.

func TestInvokeDecodesWithTheAPIRules(t *testing.T)
// Invoke(`{"title":"a","colour":1}`) → op.Invalid with AsSent and a
// "colour" field, run called 0 times; Invoke(`{"title":"a"}`) → a note
// whose Title is "a".

func TestAnOperationWithNoResultInvokesToNil(t *testing.T)
// op.Define[deleteInput, op.None]("notes.delete", Spec{..., Destructive:
// true, Idempotent: true}, ...): Result is {"type":"null"}, and Invoke
// returns (nil, nil).

func TestSpecHumanOnlyReachesTheOperation(t *testing.T)
// Spec{HumanOnly: "reason"} → Descriptor.Operation.HumanOnly == "reason".

func TestDefineRefusesWhatWouldFailAtADoor(t *testing.T)
// Table of name → func(); each must panic with a message containing
// the ID: "Notes.create", "notes", "notes.create.now",
// a 65-character tool name, empty Description, zero Access, nil run,
// In = string, Out = int, a Destructive read, "notes.delete" without
// Destructive (CheckFlags), InQuery on a write, InQuery with a []string
// input field.
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./op -run 'TestDefine|TestCall|TestInvoke|TestAnOperation|TestSpec'`
Expected: FAIL with `undefined: op.Define`.

- [ ] **Step 3: Implement `Define`, `Call`, `Spec`, `None` and `Named`** in
  `op/define.go`, and add `InQuery` to `Descriptor`. Check that a
  function stays at 40 lines or fewer, by splitting the panics into a
  `validateSpec(id string, spec Spec, input reflect.Type)` helper.

- [ ] **Step 4: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./op && GOFLAGS=-mod=mod go test ./internal/docsite -run TestExportedSymbolsAreDocumented`
Expected: PASS.

- [ ] **Step 5: Commit** with the subject
  `op: Define declares a named operation every door calls`.

---

### Task 4: `api.Operations`, the API door for named operations

**Files:**
- Create: `api/operations.go`
- Modify:
  - `api/decode.go`: extract `readJSONBody`, which `Decode` then calls.
  - `api/tree.go`: `Route` gains an unexported `readByPost bool`, and
    `add` accepts `POST` with `Read` only when it is set.
- Modify docs: `docs/site/reference/api.md` (`Operations`)
- Test: `api/operations_test.go` (package `api`, so it can use
  `serve`, `config`, `verifier` and `assertCode` from `tree_test.go`)

**Interfaces:**
- Consumes: `op.Define`, `op.Descriptor.InQuery` (Task 3), and
  `human_only` (Task 1).
- Produces:
  - `func Operations(descriptors ...op.Descriptor) []Route`. For a
    descriptor with `ID "<area>.<verb>"`, the route is
    `{Method: POST, Pattern: "/api/v1/<area>/<verb>", Op: descriptor,
    Handle: <named door>, readByPost: Access == Read}`. With `InQuery`,
    the method is `GET`.
  - `func readJSONBody(response http.ResponseWriter, request *http.Request) ([]byte, bool)`
    holds `Decode`'s 415 and 413 rules, unchanged.
  - The named door:
    - **POST:**
      - `ContentLength == 0` runs as `{}`.
      - Otherwise the door calls `readJSONBody`, then refuses a body
        that is not one JSON object with `msgNotObject` and no fields.
        It checks this with `json.Unmarshal` into a `jsontext.Value`
        and `Kind() == '{'`, as `DecodeInput` does first.
      - Then it calls `Invoke`.
    - **GET:** the door calls `Invoke(queryArguments(request.URL.Query(), descriptor.Input))`.
    - **Answers:**
      - an error goes to `Refuse`;
      - a nil value is `204 No Content`;
      - anything else is `Write(response, 200, value)`.
  - `func queryArguments(query url.Values, inputSchema json.RawMessage) []byte`
    builds one JSON object, with keys sorted and every value of a key
    in the order sent:
    - a value whose property type is integer or number goes in raw if
      it is a valid JSON number literal;
    - a boolean's value goes in raw if it is `true` or `false`;
    - everything else, including unknown keys, goes in as a JSON
      string.

    `DecodeInput` then refuses a repeated key (duplicate member), a
    wrong type and an unknown key, with its own messages. So the query
    and the body can't drift, and this function has no copy of its own.

- [ ] **Step 1: Write the failing tests.** The fixtures are `op.Define`
  operations:
  - `notes.create` (write), returning `{"id":1,"title":<title>}`;
  - `notes.count` (read, POST), returning `{"count":2}`;
  - `notes.list` (read, `InQuery`), whose input is
    `{Search op.Opt[string] "search"; Limit op.Opt[int] "limit"; Pinned op.Opt[bool] "pinned"}`
    and which echoes its input;
  - `notes.delete` (write, `None`);
  - `tokens.create` (write, `HumanOnly`).

  `mustNew(t, config(), Operations(...)...)`. The `serve` helper's
  tokens are as in `tree_test.go`.

```go
func TestOperationsRoutesEachOperationByItsName(t *testing.T)
// Operations(create.Descriptor, list.Descriptor): [POST /api/v1/notes/create,
// GET /api/v1/notes/list], each Op.ID as declared.

func TestAPostedOperationAnswersItsResult(t *testing.T)
// POST /api/v1/notes/create {"title":"Hello"} with JSON content type
// → 200, body {"id":1,"title":"Hello"}.

func TestAReadServedByPostTakesAReadOnlyToken(t *testing.T)
// POST /api/v1/notes/count with the read-only token → 200.
// And the existing TestNewRefuses "POST with Read" case still passes.

func TestAnOperationWithNoResultAnswers204(t *testing.T)
// POST /api/v1/notes/delete {"id":1} → 204, empty body.

func TestAnEmptyPostIsAnEmptyObject(t *testing.T)
// POST /api/v1/notes/create, no body, no Content-Type → the run sees a
// zero createInput (and here refuses Title: 422 with field "title").

func TestABodyWithoutJSONContentTypeIs415(t *testing.T)
// POST with body {"title":"x"} and Content-Type text/plain →
// assertCode 415 "unsupported_media_type".

func TestABodyThatIsNotAnObjectIsInvalidWithNoFields(t *testing.T)
// body [] → 422 "invalid", message msgNotObject, Fields nil.

func TestQueryArgumentsAreRefusedByTheBodyRules(t *testing.T)
// GET /api/v1/notes/list?search=hi&limit=5&pinned=true → 200, echoed
// {"search":"hi","limit":5,"pinned":true}.
// Each of these is 422 "invalid" whose Fields has exactly that key:
// ?limit=five → "limit"; ?limit=1&limit=2 → "limit";
// ?pinned=yes → "pinned"; ?colour=red → "colour".

func TestAHumanOnlyOperationRefusesAnAgentWith403(t *testing.T)
// POST /api/v1/tokens/create {} with the writer token and an
// Agent-Session → 403 "human_only", and the run was never called.
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./api -run 'TestOperations|TestAPosted|TestARead|TestAnOperation|TestAnEmpty|TestABody|TestQuery|TestAHumanOnly'`
Expected: FAIL with `undefined: Operations`.

- [ ] **Step 3: Implement** `Operations`, the named door and
  `queryArguments` in `api/operations.go`, and `readJSONBody` in
  `decode.go`. Then relax `add`'s method check for `readByPost` only, with
  a why-comment: the access comes from the declaration, which `Admit`
  and `op.Check` both enforce, and hand-built routes keep the stricter
  rule.

- [ ] **Step 4: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./api && GOFLAGS=-mod=mod go test ./internal/docsite -run TestExportedSymbolsAreDocumented`
Expected: PASS, including every existing tree test.

- [ ] **Step 5: Commit** with the subject
  `api: serve named operations at /api/v1/<area>/<verb>`.

---

### Task 5: The route recorder over chi and `ServeMux`

**Files:**
- Create: `route/route.go`, `route/table.go`, `route/messages.go`
- Create docs: `docs/site/reference/route.md`. Add `"route":
  "reference/route"` to `referencePages` in
  `internal/docsite/symbols_test.go`, and a `reference/route` entry to
  `docs/site/nav.json` after `reference/mcp`.
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
	Operations   []string // IDs; empty for a page-only route
	PageOnly     string
}

type Table struct{ /* register, prefix, shared *[]Route, seen map */ }
func New(register Register) *Table
func (table *Table) On(register Register, prefix string) *Table
func (table *Table) Handle(pattern string, handler http.Handler, mark Mark)
func (table *Table) HandleFunc(pattern string,
	handler func(http.ResponseWriter, *http.Request), mark Mark)
func (table *Table) List() []Route
```

  - A pattern is `"<METHOD> <path>"`, in `ServeMux`'s form.
  - The method must be GET, POST, PUT, PATCH or DELETE. Otherwise
    `Handle` panics with `msgNoMethod` or `msgUnknownMethod`.
  - `Handle` registers `path` (as given) through `register`, and records
    `prefix + path`.
  - `On` returns a table over another `Register` (a chi sub-router or
    group) whose recorded paths start with `prefix`. It shares the list
    and the twice-check with its parent.
  - The twice-check keys on method plus full path, with each `{name}`
    replaced by `{}`. A repeat panics with `msgTwice`.
  - `Operation` panics with `msgNoOperation` on a descriptor with an
    empty ID. `PageOnly` panics with `msgNoReason` on a blank reason.
  - A zero `Mark` is recorded as it is, with no panic. It is the parity
    check's job to refuse it, so the app's whole table is reported in
    one run.
  - `List` returns a deep copy.

- [ ] **Step 1: Write the failing tests**

```go
func TestATableOverChiServesAndRecords(t *testing.T)
// router := chi.NewRouter(); table := route.New(route.Chi(router));
// table.HandleFunc("POST /notes", h, route.Operation(create)), where
// create is an op.Descriptor{Operation: rastrillo.Operation{ID:
// "notes.create"}}: a POST /notes reaches h, and List() is
// [{POST /notes [notes.create] ""}].

func TestATableOverAServeMuxServesAndRecords(t *testing.T)
// The same through route.Mux(http.NewServeMux()), plus
// GET /style.css with route.PageOnly("the stylesheet") recorded with
// that reason.

func TestATableOnASubRouterRecordsTheFullPath(t *testing.T)
// router.Route("/admin", func(sub chi.Router) {
//     table.On(route.Chi(sub), "/admin").HandleFunc("GET /people", h, mark) })
// → GET /admin/people reaches h; List()[0].Path == "/admin/people".

func TestRegisteringAMethodAndPathTwicePanics(t *testing.T)
// "POST /notes/{id}" then "POST /notes/{key}" on one chi table: the
// second panics with fmt.Sprintf(msgTwice, "POST", "/notes/{key}").

func TestAPatternMustNameAKnownMethod(t *testing.T)
// "/notes" panics with msgNoMethod; "OPTIONS /notes" with
// msgUnknownMethod.

func TestAMarkMustSaySomething(t *testing.T)
// route.PageOnly("  ") and route.Operation(op.Descriptor{}) panic.

func TestAZeroMarkIsRecordedForTheParityCheck(t *testing.T)
// HandleFunc("GET /x", h, route.Mark{}) → no panic; recorded with no
// operations and no reason.

func TestListReturnsACopy(t *testing.T)
// Changing List()[0].Operations[0] does not change the next List().
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./route`
Expected: FAIL. The package does not exist yet.

- [ ] **Step 3: Implement** `route/route.go` and `route/table.go`, and
  write `docs/site/reference/route.md` with every exported symbol.

- [ ] **Step 4: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./route && GOFLAGS=-mod=mod go test ./internal/docsite`
Expected: PASS.

- [ ] **Step 5: Commit** with the subject
  `route: record each route with its operation, over chi or ServeMux`.

---

### Task 6: `paritytest`, and the example app through every door

**Files:**
- Create: `paritytest/paritytest.go`, `paritytest/doors.go`,
  `paritytest/messages.go`
- Create tests:
  - `paritytest/example_app_test.go`: the app, built over chi or
    `ServeMux`;
  - `paritytest/example_test.go`: the doors agree;
  - `paritytest/routes_test.go`: the check fails what it should.
- Create docs: `docs/site/reference/paritytest.md`. Add `"paritytest":
  "reference/paritytest"` to `referencePages`, and a nav entry after
  `reference/route`.

**Interfaces:**
- Consumes: `route.Route` and `route.Table` (Task 5), `api.Operations`
  (Task 4), `op.Define` (Task 3), and `mcp.New`.
- Produces:

```go
type Surface struct {
	Routes     []route.Route   // the page table, from Table.List
	Operations []op.Descriptor // every operation the app declares
	Handler    http.Handler    // the whole app: pages, /api/v1, /api/v1/mcp
	Token      string          // an agent's write token for a person
	                           // whom Authorize allows every operation
}

func Routes(t testing.TB, surface Surface)

type Outcome struct {
	Status int
	Kind   string            // "ok" for 2xx, else the error's code
	Fields []string          // an invalid answer's field names, sorted
	Body   map[string]any    // numbers kept as json.Number
}

func CallAPI(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome
func CallTool(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome
func ToolNames(t testing.TB, surface Surface) []string // sorted
```

  - `Routes` reports every problem with `t.Errorf`, using the approved
    copy, and never stops early. It calls `t.Fatalf` only when it can't
    start the test server.
  - It checks, in this order:
    1. **Each route** names at least one operation or has a page-only
       reason (`msgUnmarked`), and every operation it names is in
       `Operations` (`msgUndeclared`).
    2. **`Operations`** lists no ID twice (`msgListedTwice`), and a
       route names every operation (`msgNoPage`).
    3. **The API:** an unauthenticated POST to
       `/api/v1/<area>/<verb>` answers 401 (GET for `InQuery`).
       Anything else fails with `msgNotOnAPI`. The gate routes before
       it checks the credential, so this runs nothing.
    4. **MCP:** `ToolNames` with `Token` contains each operation that
       is not human-only (`msgNotOnMCP`), and no human-only one
       (`msgHumanListed`).
    5. **Human-only operations:** with `Token` and `Agent-Session:
       paritytest`, both `CallAPI` and `CallTool` answer `Kind ==
       "human_only"` (`msgHumanCallable`). `op.Check` refuses before
       the operation runs.
  - `CallAPI` sends `Authorization: Bearer <Token>` and `Agent-Session:
    paritytest`. It sends a JSON POST, or a GET with the input as the
    query, for `InQuery`.
  - `CallTool` posts a stateless `tools/call` to `/api/v1/mcp`, as the
    generated `toolPost` does. A tool error's kind is its envelope's
    code.
  - All three go through `httptest.NewServer`, because a `Rechecker`
    answers a `ResponseRecorder` 503.

- [ ] **Step 1: Write the example app**
  (`paritytest/example_app_test.go`, package `paritytest_test`).

  `buildExample(t *testing.T, router string, options exampleOptions)
  (paritytest.Surface, *exampleApp)` returns the app.
  - `router` is `"chi"` or `"ServeMux"`.
  - `exampleOptions{apiLeavesOut, mcpLeavesOut, pageLeavesOut string;
    humanOnlyCallable bool; extraMark *route.Mark}` creates the broken
    variants that `routes_test.go` needs. Each `LeavesOut` names an
    operation ID that door doesn't get. `extraMark` registers
    `GET /extra` with that mark.

  The app keeps its notes in memory, behind a mutex. Its operations are
  made with `op.Define` inside the constructor:
  - `notes.create`: write. The input is `{Title string "title"}` and
    the result is `note{ID int64 "id"; Title string "title"}`. An empty
    title is `op.Invalid{Fields: {"Title": "Enter a title."}}`.
  - `notes.list`: read, `InQuery`. The input is
    `{Search op.Opt[string] "search"}` and the result is
    `{Notes []note "notes"}`.
  - `notes.delete`: write, `Destructive`, `Idempotent`. The input is
    `{ID int64 "id"}` and the result is `op.None`.
  - `tokens.create`: write, `HumanOnly: "an agent must never raise its
    own access"`. The input is `{Agent string "agent"}` and the result
    is `{Token string "token"}`.

  Its page routes, through one `route.Table`:

  | Route | Mark | Answer |
  |---|---|---|
  | `GET /notes` | `notes.list` | the notes |
  | `POST /notes` | `notes.create` | 303 on success; 422 with one Go field name per line on `op.Invalid` |
  | `POST /notes/{id}/delete` | `notes.delete` | |
  | `POST /settings/tokens` | `tokens.create` | |
  | `GET /style.css` | `route.PageOnly("the stylesheet")` | |

  Each page handler reads the form and calls the operation's `Call`.

  The API and MCP are mounted directly on the router, at `/api/v1/mcp`
  and `/api/v1/`:
  - `api.New(config, api.Operations(app.operations()...)...)`;
  - `mcp.New(mcp.Config{Gate: config.Gate(), Ctx: config.Ctx, Authorize:
    config.Authorize, Name: "example", Version: "test"},
    app.operations()...)`.

  The verifier is a map verifier:
  - `writer` is `{Subject "a", Agent "Claude", Credential "writer",
    Access Write}`;
  - `reader` is the same with `Access Read`;
  - `person` is `{Subject "a", Credential "person", Access Write}`.

  `Authorize` allows everything. Page requests get the session actor
  `{Subject "a", Access Write}` from the ctx factory.
  `RequireAgentSession` is true.

- [ ] **Step 2: Write the failing tests**

```go
// paritytest/example_test.go; each runs for "chi" and "ServeMux".
func TestTheExampleAppPassesTheParityCheck(t *testing.T)
// paritytest.Routes(t, surface) with no failures.

func TestACreateEndsAlikeOnEveryDoor(t *testing.T)
// page POST /notes Title=Hello → 303; CallAPI(create, {"title":"Hello"})
// and CallTool(...) → Kind "ok", Body["title"] == "Hello";
// CallAPI(list, {"search":"Hello"}) → three notes.

func TestAnEmptyCreateIsRefusedAlikeOnEveryDoor(t *testing.T)
// page POST /notes with no fields → 422, the field names mapped through
// api.FieldName == ["title"]; CallAPI(create, {}) and
// CallTool(create, {}) → Kind "invalid", Fields ["title"].

func TestAReadTokenListsOnlyReadTools(t *testing.T)
// ToolNames with Token "reader" == ["notes_list"]; with "writer" ==
// ["notes_create", "notes_delete", "notes_list"]. (Spec §5.6.)

func TestAHumanOnlyOperationRefusesEveryToken(t *testing.T)
// CallAPI and CallTool on tokens.create with "writer", and with
// "person" → Kind "human_only", Status 403 on the API; and the app
// minted nothing. The page POST /settings/tokens agent=Claude as the
// session → 303, and the app minted one token.

func TestADeleteAnswersNoContent(t *testing.T)
// CallAPI(remove, {"id":1}) → Status 204, Kind "ok";
// CallTool(remove, {"id":2}) → Kind "ok".
```

```go
// paritytest/routes_test.go. spy embeds testing.TB, and its Errorf
// appends to failures; each test builds one broken variant and asserts
// spy.failures equals exactly the formatted approved message(s).
func TestParityFailsARouteWithNoMark(t *testing.T)            // msgUnmarked
func TestParityFailsARouteNamingAnUnlistedOperation(t *testing.T) // msgUndeclared
func TestParityFailsAnOperationWithNoPage(t *testing.T)        // msgNoPage
func TestParityFailsAnOperationMissingFromTheAPI(t *testing.T) // msgNotOnAPI, 404
func TestParityFailsAnOperationMissingFromMCP(t *testing.T)    // msgNotOnMCP
func TestParityFailsAHumanOnlyOperationAnAgentCanCall(t *testing.T)
// humanOnlyCallable: API and MCP get a copy of tokens.create whose
// Operation.HumanOnly is ""; Surface.Operations keeps the real one →
// msgHumanListed, and msgHumanCallable for "the API" and for "MCP".
func TestParityReportsEveryProblemInOneRun(t *testing.T)
// two breaks at once → two failures.
```

- [ ] **Step 3: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./paritytest`
Expected: FAIL with `undefined: paritytest.Routes`.

- [ ] **Step 4: Implement** `paritytest.go`, `doors.go` and `messages.go`,
  and write `docs/site/reference/paritytest.md`.

- [ ] **Step 5: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./paritytest ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit** with the subject
  `paritytest: check routes against the API and MCP, with an example app`.

---

### Task 7: `action.Routes` marks and `rastrillo routes --parity`

**Files:**
- Modify: `action/routes.go`. `Get`, `Post` and `Handle` take
  `marks ...route.Mark`, and `Route` gains `Operations []string` and
  `PageOnly string`.
- Create: `internal/routelist/unmarked.go`. Modify
  `internal/routelist/messages.go` and `format.go`.
- Modify: `cmd/rastrillo/routes.go` (the `--parity` flag; usage in
  `routelist.Usage`)
- Create fixture: `internal/routelist/testdata/marked/` (a module like
  `testdata/app`, with one route marked
  `route.Operation(<descriptor>)`, one `route.PageOnly("the
  stylesheet")` and one unmarked)
- Modify docs: `docs/site/cli.md` (`rastrillo routes`: `--parity`),
  `docs/site/reference/action.md` (the marks)
- Test: `action/routes_marks_test.go`, `internal/routelist/unmarked_test.go`,
  `cmd/rastrillo/routes_test.go`

**Interfaces:**
- Consumes: `route.Mark`, `Mark.Operations()` and `Mark.Reason()`
  (Task 5).
- Produces:
  - `func (routes *Routes) Get(path Path, handler Handler, marks ...route.Mark)`,
    and the same for `Post`.
  - `func (routes *Routes) Handle(method string, path Path, handler http.Handler, name string, marks ...route.Mark)`.
  - More than one mark panics: "pass one route.Mark".
  - `func Unmarked(routes []action.Route) []action.Route` returns the
    routes with no `Operations` and no `PageOnly`, skipping `Mount`
    lines (`Method == "*"`), whose subtree lists its own.
  - `func FormatUnmarked(routes []action.Route) string` writes one
    `\t<METHOD> <path>` line each into `unmarkedRoutes`.
  - `rastrillo routes --parity` implies `--check`. After the unmounted
    check, it exits 1 with `FormatUnmarked` when `Unmarked` is
    non-empty. Without `--parity`, `--check` is unchanged, so existing
    apps stay green.

- [ ] **Step 1: Write the failing tests**

```go
func TestAMarkIsRecordedWithTheRoute(t *testing.T)
// recorder := action.NewRecorder(); recorder.Post("/notes", handlerA,
// route.Operation(create)); recorder.Get("/style.css", handlerB,
// route.PageOnly("the stylesheet")) → List()[0].Operations ==
// ["notes.create"], List()[1].PageOnly == "the stylesheet".

func TestARouteWithNoMarkRegistersAsBefore(t *testing.T)
// recorder.Get("/who", handlerA) → Operations nil, PageOnly "".

func TestTwoMarksOnOneRoutePanics(t *testing.T)

func TestUnmarkedListsRoutesWithNeitherMark(t *testing.T)
// [{GET /a Operations [x]}, {POST /b}, {* /api/*}, {GET /c PageOnly r}]
// → [{POST /b}].

// cmd/rastrillo/routes_test.go, built binary as in
// TestRoutesPrintsTheTableAndChecksEveryActionIsMounted:
func TestRoutesParityFailsAnUnmarkedRoute(t *testing.T)
// routes --check <marked> → status 0; routes --parity <marked> → status
// 1, stderr contains FormatUnmarked of the one unmarked route.
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./action ./internal/routelist -run 'Mark|Unmarked' && GOFLAGS=-mod=mod go test ./cmd/rastrillo -run TestRoutesParity`
Expected: FAIL. Calls pass too many arguments to `Post`, and `Unmarked`
is undefined.

- [ ] **Step 3: Implement** the marks, `Unmarked`, `FormatUnmarked`, the
  flag and the docs.

- [ ] **Step 4: Run them to see them pass**

Run: `GOFLAGS=-mod=mod go test ./action ./internal/routelist ./internal/docsite && GOFLAGS=-mod=mod go test ./cmd/rastrillo -run 'TestRoutes'`
Expected: PASS.

- [ ] **Step 5: Commit** with the subject
  `routes: --parity fails a route with no operation or page-only reason`.

---

### Task 8: Guide, skill, CHANGELOG, and the gates

**Files:**
- Create: `docs/site/operations.md`, the "Named operations and parity"
  guide, with a `docs/site/nav.json` entry after `mcp`.
- Create: `skill/operations.md`, a topic of 9,000 bytes or fewer.
- Modify:
  - `SKILL.md`: the API row of the index names `skill/operations.md`.
    Reword that row so the file stays at 9,700 bytes or fewer. It is at
    9,694 now.
  - `docs/site/api.md`: add `human_only` to the error table, and a
    pointer to the guide under "Writing an operation by hand".
  - `docs/site/mcp.md`: human-only tools are hidden and refused; a read
    token sees only reads.
  - `CHANGELOG.md`
- Test: the existing `skillmd_test.go` and `internal/docsite` tests

**The guide covers, in this order:**
- **Declaring an operation:** `op.Define` with a `Spec`. Wire names
  follow the Go names. `Invalid.Fields` is keyed by the Go field name.
  `Scoped`, and why an unscoped operation needs `Authorize` for tokens.
- **The page calls `Call`:** an action in four beats, from
  `skill/code.md`.
- **Serving it:** `api.Operations` beside `gen.APIRoutes()`, and
  `mcp.New` with the same descriptors.
- **POST and GET:** POST by default; `InQuery` reads at GET; 204 for
  `op.None`; an empty POST is `{}`.
- **Human-only operations:** what refuses, and why every token does,
  not only agent-named ones.
- **The route table:** `route.New` over chi or `ServeMux`, `On` for
  sub-routers, and marks.
- **The parity check:** `paritytest.Routes` in the app's own test, what
  each failure means, the token it needs, and `CallAPI` and `CallTool`
  for the end-to-end scenario. Import it beside the generated
  `gen/paritytest` with an alias.
- **What it doesn't catch:** routes registered on the router outside
  the table, and writes the operation doesn't make.
- **`rastrillo routes --parity`** for `action.Routes` apps.

**The CHANGELOG** gets an `## Unreleased` section with `### Added`:
named operations, `api.Operations`, `route`, `paritytest`, human-only
operations and `human_only`, and `routes --parity`. Its lead line says
that existing apps change nothing, and that a hand-written route of
theirs is checked only when they opt in.

- [ ] **Step 1: Write the guide, the skill topic, the SKILL.md row, the
  docs edits and the CHANGELOG entry.**
- [ ] **Step 2: Have Luna review the prose.** Put every new or changed
  paragraph in the guide, the reference pages, the skill topic and the
  CHANGELOG into one prompt. Run Task 0's command with
  `-o "$TMPDIR/luna-0a-docs.md"`, and apply its corrections unless one
  is factually wrong.
- [ ] **Step 3: Run the docs and skill tests**

Run: `GOFLAGS=-mod=mod go test ./internal/docsite && GOFLAGS=-mod=mod go test . -run 'TestSkill'`
Expected: PASS. `TestSkillStaysWithinBudget` passes with `SKILL.md` at
9,700 bytes or fewer.

- [ ] **Step 4: Run the budget gates**

Run: `GOFLAGS=-mod=mod go run ./cmd/rastrillo budget size && GOFLAGS=-mod=mod go run ./cmd/rastrillo budget code && make gofmt`
Expected: each exits 0, and `git status` shows no change to
`.rastrillo/`.

- [ ] **Step 5: Commit** with the subject
  `docs: named operations, the route table and the parity check`.

- [ ] **Step 6: Push and let the fleet run the gate.** Merge `origin/main`
  first if it moved. Push `agent-parity`, and wait for `make ci` on the
  fleet. Then follow `CONTRIBUTING.md`:
  - get a cross-provider review (Astra);
  - post the branch description with the fail-first runs, the Luna
    replies and what was applied.

  Expected: the fleet's gate is green.
