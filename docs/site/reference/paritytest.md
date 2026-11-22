# 🤖 paritytest

`amadan.net/rastrillo/rastrillo/paritytest`

In your app's tests, use `Routes` to check that every page names its
operation, and that the API and MCP serve the same operations the app
declares and no others. Use `Run` to check that a person and their agent
get the same answers.

## Surface and Routes

```go
type Surface struct {
	Routes     []route.Route   // Table.List()
	Operations []op.Descriptor // every operation the app declares
	Generated  []op.Descriptor // gen.Operations()
	API        http.Handler    // exactly what api.New returned
	MCP        http.Handler    // exactly what mcp.New returned
	Handler    http.Handler    // the whole app, both mounted
	Token      string
	Router     chi.Routes
}

func Routes(t testing.TB, surface Surface)
```

`Routes` compares the app's page table and both doors with the
operations it declares. It reports every problem with `t.Errorf` and
never stops early.

```go
func TestParity(t *testing.T) {
	app := notes.New()
	router := chi.NewRouter()
	table := route.New(route.Chi(router))
	notes.Routes(table, app)
	router.Mount("/api/v1/mcp", app.MCP)
	router.Mount("/api/v1/", app.API)
	paritytest.Routes(t, paritytest.Surface{
		Routes:     table.List(),
		Operations: app.Operations(),
		API:        app.API,
		MCP:        app.MCP,
		Handler:    router,
		Token:      adminAgentToken,
		Router:     router,
	})
}
```

It fails when:

- a route has no mark, or names an operation `Operations` doesn't
  list;
- `Operations` lists an ID twice, or an operation has no page;
- on chi, the router serves a method and path the table didn't record;
- the API doesn't serve a declared operation at the method and path
  `api.Operations` gives it, serves another operation there, or serves
  it with a `Descriptor` other than the declared one;
- MCP doesn't serve a declared operation, or serves it differently;
- either door serves an operation that is neither in `Operations` nor
  in `Generated`;
- a request with no credential to a declared operation's API path,
  through `Handler`, doesn't answer 401 with `Rastrillo-Operation` set
  to its ID. A catch-all or a middleware in front of the API answers
  without that header;
- MCP's tool list for `Token` leaves out an operation an agent may
  call, or shows a human-only one;
- the API or MCP answers `Token` with anything but `human_only` for a
  human-only operation.

`Token` must be an agent's write token for a person whom `Authorize`
allows every operation, so that only a fault in the app hides a tool.

`Generated` holds the CRUD operations `gen.Operations()` returns.
`gen/paritytest` checks those, so here they only count as declared.
Import this package beside it under another name, such as
`routeparity`.

`Router` is the app's chi router. Leave it nil for a `ServeMux`, which
can't be walked. The walk skips only what chi's `Mount` registers for
the API tree at `/api/v1/` and for MCP at `/api/v1/mcp`. A route
registered directly at one of those paths, such as a GET at
`/api/v1/mcp`, is still found. Register every other route under
`/api/v1/` through the table.

## Calling the doors

```go
type Outcome struct {
	Status int
	Kind   string         // "ok" for 2xx, else the error's code
	Fields []string       // an invalid answer's field names, sorted
	// FieldMessages is an invalid answer's message for each field.
	FieldMessages map[string]string
	Body          map[string]any // numbers as json.Number
	// Message is a refusal's message.
	Message string
}

func CallAPI(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome
func CallTool(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome
func ToolNames(t testing.TB, surface Surface) []string
```

Each sends one request through `Handler` on a test server, with
`Authorization: Bearer <Token>` and `Agent-Session: paritytest`.

`CallAPI` calls the operation's API route: a JSON POST, or a GET with
`input` as the query string for a read declared `InQuery`. `CallTool`
posts one stateless `tools/call` to `/api/v1/mcp`. `ToolNames` returns
the tool names MCP lists for `Token`, sorted.

An `Outcome`'s `Kind` is `"ok"` for a success, and the error's code for
a refusal, so the two doors' answers compare directly. `Status` is the
HTTP status. MCP answers a refused tool call with 200, so compare
`Kind`, not `Status`. `Body` contains a success's JSON object and is nil
otherwise. A success with a missing body, invalid JSON, or a non-object
body is reported, as is an MCP tool error with no content. `Message` is
a refusal's message, which the API and MCP both write with
`api.Refusal`. `FieldMessages` contains an `invalid` answer's message
for each field. `ToolNames` reports an unreadable tool list instead of
returning an empty list.

## Scenarios and Run

```go
type Fixture interface {
	Surface() Surface
	Token(t testing.TB, person string) string
	Page(t testing.TB, person string, step Step) Outcome
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

`Run` checks that a person on the app's pages and their agent on the
API or MCP get the same result for the same steps. It builds three
fresh fixtures with `Fresh`, one for each door, and does every step on
each:

- the page fixture through `Page`, as the step's person;
- the API fixture with `CallAPI`, and the MCP fixture with `CallTool`,
  each with `Token(person)`.

After each step, `Run` compares:

- `Kind` and `Fields` across all three doors;
- `Body`, `Message` and `FieldMessages` between the API and MCP;
- `State` across all three doors. A difference in state is reported
  once, at the step where it starts.

`Run` doesn't compare the page's rendered body or its wording. For a
read, it checks only that the page's `Kind` matches, and that the API
and MCP return the same body.

A step that calls a human-only operation fails, and runs on no door,
because the doors answer it differently by design. `Routes` checks
human-only operations.

`Run` reports every problem with `t.Errorf` and never stops early.

```go
func TestNotesScenario(t *testing.T) {
	paritytest.Run(t, paritytest.Scenario{
		Fresh: func(t testing.TB) paritytest.Fixture {
			return newNotesFixture(t)
		},
		Steps: []paritytest.Step{
			{Name: "a creates Hello", Person: "a",
				Operation: notes.Create.Descriptor,
				Input:     map[string]any{"title": "Hello"}},
			{Name: "a creates nothing", Person: "a",
				Operation: notes.Create.Descriptor,
				Input:     map[string]any{}},
			{Name: "a lists", Person: "a",
				Operation: notes.List.Descriptor},
		},
	})
}
```

`Page` drives the app's own page for the step, such as a form post,
and reads its answer as an `Outcome`: the `Kind`, and for `invalid`
the fields under their wire names (`api.FieldName`). `Body` may be nil,
because a page answers with HTML. `State` returns what the app has
stored, without ids and times, which differ between fresh fixtures.
