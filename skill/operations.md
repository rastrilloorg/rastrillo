# Named operations and parity

**Declare.** One operation is one function every door calls. For an app
with no manifest, or an action `generate` doesn't write:

```go
var Publish = op.Define("posts.publish", op.Spec{
	Description: "Publish a draft post.", // first sentence is MCP's title
	Confirm:     "Publish post {id}?",
	Access:      rastrillo.Write,
	Scoped:      true,
}, publish) // func(ctx, *rastrillo.Ctx, In) (Out, error)
```

ID is `<area>.<verb>`, lowercase, digits, `_`. The API route is
`POST /api/v1/<area>/<verb>`, the MCP tool `<area>_<verb>` (64 bytes at
most). `Define` panics at start-up on anything a door couldn't serve,
and the message says what to change. `Spec`: `Description` (required),
`Confirm`, `Access` (`rastrillo.Read` or `Write`, required),
`Destructive`, `Idempotent`, `Scoped`, `InQuery`, `HumanOnly` (a reason).
A write whose verb undoes or sends (`cancel`, `send`, `delete`) must be
`Destructive`.

**Types.** `In` is a struct; `Out` is a struct or `op.None` (204, no
body). Each must be a JSON object on the wire (not `time.Time`), and no
field may hold `op.None`. Every exported field has a `json` tag equal to the snake_case of
the Go name (`PublishedAt` is `published_at`) or `Define` panics.
`op.Invalid.Fields` is keyed by the Go name; the API and MCP report it
as the wire name. A field that may be left out is `op.Opt[T]`, one that
may be `null` is `op.Nullable[T]`; a pointer in an input is refused, and
`Opt` and `Nullable` are refused in a result. A required member that is
absent reaches `run` as its zero value, so refuse it with `op.Invalid`.
`Input` and `Result` schemas are derived from the types.

**Scoped.** `Scoped: true` means rows belong to a person: `op.Check`
refuses a caller with no `Subject`, and `run` filters by it. An
unscoped operation is refused for every token by `op.DefaultPolicy`; let
tokens in from your `Authorize` (`a.IsToken()`, `o.ID`) and end with
`op.DefaultPolicy`.

**The page calls `Call`.** `Publish.Call(ctx, rc, In{...})` runs
`op.Check`, then `run`. Read the form into `In`, then call `Call`. On
`op.Invalid`, show each message beside its field, by Go name.
Otherwise, redirect or render. Put every rule in the operation.

**Serve it.** `api.New(config, api.Operations(descriptors...)...)` and
`mcp.New(mcpConfig, descriptors...)`, the same `Descriptor`s. Both doors
run `op.Check` before they read any input.

**POST and GET.** A POST body needs `Content-Type: application/json`
(415); no body at all runs as `{}`. `InQuery: true` on a read, with every
input field a string, number or bool, serves it as
`GET /api/v1/<area>/<verb>?field=value`; a repeated, unknown or ill-typed
parameter is 422 naming it. A POST with a query string, or a GET with a
body, is 422. A read-only token may use a POST read from
`api.Operations`.

**Refusal order,** on both doors and in `op.Check`: human-only
(`human_only`, 403, every token, a person's own too), then a read-only
token's write (`forbidden`), then no `Agent-Session` under
`RequireAgentSession` (`invalid`), then `Authorize`, then input. MCP
hides a human-only tool from the list and refuses a call to it.

**The route table.** Register page routes through it:

```go
table := route.New(route.Chi(router)) // or route.Mux(mux)
table.HandleFunc("POST /posts/{id}/publish", app.publish,
	route.Operation(posts.Publish.Descriptor))
table.HandleFunc("GET /style.css", app.css,
	route.PageOnly("the stylesheet"))
admin := table.On(route.Chi(sub), "/admin") // a sub-router
```

`On`, never `Mount`, for a sub-router. The same method and path twice
panics. On `action.Routes`, pass the mark as the last argument of `Get`,
`Post` or `Handle`.

**`paritytest.Routes`.** `paritytest.Routes(t, paritytest.Surface{Routes:
table.List(), Operations: all, Generated: gen.Operations(), API: api,
MCP: mcp, Handler: whole app, Token: agentWriteToken, Router: chiRouter})`.
It compares the page table, the API and MCP inventories with `Operations`
and reports every difference: an unmarked route, an operation with no
page, one missing from or extra on a door, a wrong descriptor, no 401
with `Rastrillo-Operation` (a catch-all in front of the API), a
human-only tool listed or callable. `Token` is an agent's write token for
someone `Authorize` allows everything. Import it beside `gen/paritytest`
under another name (`routeparity`).

**`paritytest.Run`.** `Run(t, Scenario{Fresh: newFixture, Steps: ...})`
does each `Step{Name, Person, Operation, Input}` on three fresh fixtures,
through the page, the API and MCP, and compares kind, fields, body,
message, field messages and stored state. If a result holds a generated
ID or time, give the fixture `Normalize(step, body)` to strip it from
the API and MCP bodies first. Write a `Fixture`: `Surface`, `Token(person)`,
`Page(person, step)` (drive the page, answer an `Outcome`; for `invalid`
use wire names, `api.FieldName`) and `State` (what is stored, without ids
and times). Include a refused step, such as `{}` to a create. A step on a
human-only operation fails; `Routes` covers those.

**Not caught.** A route registered outside the table (seen only on chi
with `Surface.Router`); a write the page makes beyond the operation, so
put every write inside it; a rule only the page enforces, unless a step
exercises it.

**`rastrillo routes --check`** fails an unmarked `action.Routes` route
once the module calls `op.Define` anywhere. A `Mount` needs none.
docs/site/operations.md
