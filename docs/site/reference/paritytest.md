# 🤖 paritytest

`amadan.net/rastrillo/rastrillo/paritytest`

Check, in an app's own tests, that its pages, its API and its MCP
endpoint offer the same named operations. An agent can then do what a
person can, and no door serves an operation the app never declared.

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
can't be walked. The walk skips only the routes produced by the API
tree and MCP mounts: `/api/v1/*`, `/api/v1/mcp`, `/api/v1/mcp/` and
`/api/v1/mcp/*`. Register every other route under `/api/v1/` through
the table.

## Calling the doors

```go
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

Each sends one request through `Handler` on a test server, with
`Authorization: Bearer <Token>` and `Agent-Session: paritytest`.

`CallAPI` calls the operation's API route: a JSON POST, or a GET with
`input` as the query string for a read declared `InQuery`. `CallTool`
posts one stateless `tools/call` to `/api/v1/mcp`. `ToolNames` returns
the tool names MCP lists for `Token`, sorted.

An `Outcome`'s `Kind` is `"ok"` for a success, and the error's code for
a refusal, so the two doors' answers compare directly. `Status` is the
HTTP status. MCP answers a refused tool call with 200, so compare
`Kind`, not `Status`. `Body` is a success's JSON object, and nil for
anything else.
