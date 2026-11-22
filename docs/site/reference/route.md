# 🤖 route

`amadan.net/rastrillo/rastrillo/route`

Register an app's page routes on chi or a `ServeMux`, and record each
one with the operation it serves or why an agent has no use for it.
`paritytest` reads that record to check that every page has an agent
twin on the API and MCP.

## New and Register

```go
type Register func(method, path string, handler http.Handler)

func Chi(router chi.Router) Register
func Mux(mux *http.ServeMux) Register
func New(register Register) *Table
```

`Register` gives one route to the router underneath. `Chi` calls
`router.Method(method, path, handler)`. `Mux` calls
`mux.Handle(method+" "+path, handler)`, so the mux answers 405 to
another method on the same path, as chi does. `New` starts a `Table` at
the root of that router.

## Table

```go
func (table *Table) Handle(pattern string, handler http.Handler, mark Mark)
func (table *Table) HandleFunc(pattern string,
	handler func(http.ResponseWriter, *http.Request), mark Mark)
func (table *Table) On(register Register, prefix string) *Table
func (table *Table) List() []Route
```

`Table.Handle` registers `handler` at `pattern` and records it with
`mark`. A pattern is `"<METHOD> <path>"`, such as
`"POST /notes/{id}/publish"`, and the method is GET, POST, PUT, PATCH
or DELETE. `Table.HandleFunc` does the same for a handler function.

```go
router := chi.NewRouter()
table := route.New(route.Chi(router))
table.Handle("POST /notes", createNote,
	route.Operation(notes.Create.Descriptor))
table.Handle("GET /style.css", stylesheet, route.PageOnly("the stylesheet"))
```

`Table.On` is a table for a sub-router mounted at `prefix`. It shares
the record and the twice-check with the table it came from, so one
`List` holds the whole app, and each route is recorded under its full
path. The prefix is relative to the table `On` is called on, as chi's
`Route` is:

```go
router.Route("/admin", func(sub chi.Router) {
	admin := table.On(route.Chi(sub), "/admin")
	admin.Handle("GET /people", people, route.PageOnly("the people page"))
})
```

That serves and records `GET /admin/people`.

`Table.List` returns every route recorded so far, in the order
registered. Each call returns its own copy.

`Handle` panics, so the app stops before it serves, when:

- the pattern names no method;
- the method isn't GET, POST, PUT, PATCH or DELETE;
- the same method and full path are already registered on the table or
  any table `On` made from it. Parameter names don't count:
  `/notes/{id}` and `/notes/{key}` are the same path. A chi regexp,
  ServeMux's `{$}` and a `{name...}` wildcard do count, because they
  change what the path matches.

## Register page routes through a table

Register every page route through a `Table` and mark it with its
`route.Operation` or `route.PageOnly`.

Use `On` instead of chi's `Mount` for sub-routers; routes mounted with
`Mount` are not recorded by the table. Create the sub-router with chi's
`Route`, pass it to `On`, then register its routes on the table `On`
returns.

To serve a whole handler under a prefix, such as a file server,
register it with `Handle` and a wildcard pattern: `"GET /static/*"` on
chi or `"GET /static/"` on a `ServeMux`. Mark it `PageOnly`.

Set `Surface.Router` to the app's chi router so `paritytest.Routes` can
find routes registered outside the table. A route registered directly
on a `ServeMux` isn't seen.

Mount the API tree and MCP endpoint directly on the router at
`/api/v1/` and `/api/v1/mcp`. The walk skips only the routes those
mounts produce: `/api/v1/*`, `/api/v1/mcp`, `/api/v1/mcp/` and
`/api/v1/mcp/*`. Other checks cover them. Register every other route
under `/api/v1/` through the table.

## Mark

```go
func Operation(descriptor op.Descriptor, more ...op.Descriptor) Mark
func PageOnly(reason string) Mark
func (mark Mark) Operations() []string
func (mark Mark) Reason() string
```

A `Mark` says what a route is for. `Operation` marks a route as the
page door of one or more declared operations; it panics on a
descriptor with an empty ID. `PageOnly` marks a route an agent has no
use for, such as a stylesheet or a CSRF-only redirect; it panics on a
blank reason. `Mark.Operations` returns a copy of the IDs, and
`Mark.Reason` the page-only reason.

The zero `Mark` says neither. A table records it as it is, and
`paritytest` refuses it.

## Route

```go
type Route struct {
	Method, Path string
	Operations   []string
	PageOnly     string
}
```

One recorded route: its method, its full path, and its mark's
operations or page-only reason.
