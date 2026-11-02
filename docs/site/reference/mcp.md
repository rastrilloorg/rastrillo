# 🤖 mcp

`amadan.net/rastrillo/rastrillo/mcp`

An app's operations as MCP tools, behind the JSON API's gate.
[MCP](/docs/mcp) is the guide: mounting, connecting a client, and
writing descriptors by hand.

## New and Config

```go
func New(config Config, descriptors ...op.Descriptor) (http.Handler, error)

type Config struct {
	Gate          api.Gate
	Ctx           func(*http.Request) *rastrillo.Ctx
	Authorize     func(ctx context.Context, actor rastrillo.Actor, operation rastrillo.Operation) error
	Guard         op.ResponseGuard
	Guarded       bool
	Name, Version string
	Instructions  string
	MaxResult     int
}
```

`New` builds the MCP endpoint, with one tool per descriptor. Mount it
at `/api/v1/mcp`.

`Gate` is the API tree's, from `api.Config.Gate()`. `Ctx` builds the
app's `Ctx` for a request, and MCP sets `Actor` and `Authorize` on a
copy of it. `Authorize` is required: pass `op.DefaultPolicy` to keep the
default. `Guard` checks what an answer discloses as it goes out, and
`Guarded` requires one. `Name` and `Version` are the server's in the MCP
handshake. `Instructions` follows the framework's own instructions to
the agent. `MaxResult` caps one tool result in bytes; zero or less means
2 MiB.

`New` returns an error when:

- `Ctx` or `Authorize` is nil, or `Gate.Validate` fails;
- `Guard` is set and `Gate.Verify` isn't an `api.Rechecker`;
- `Guarded` is set and there is no `Guard`, or no `Rechecker`;
- a descriptor has an empty `ID`, an invalid `Access`, an `Input` that
  isn't an object schema, a `Result` that is neither an object schema
  nor exactly `{"type":"null"}`, or a nil `Invoke`;
- a descriptor fails `op.CheckFlags`;
- a tool name doesn't match `^[a-z][a-z0-9_]{0,63}$`, or two descriptors
  have the same tool name;
- the SDK refuses a tool.

The handler runs every request behind `Gate.Wrap`, so a missing or bad
token gets the API's 401 or 503, and every answer has
`Cache-Control: no-store`. Then:

- It answers only POST. Any other method, GET included, is 405 with
  `Allow: POST`. A batch is 400, and `subscriptions/listen` is 404,
  each with a JSON-RPC error.
- Send `Accept: application/json, text/event-stream` and
  `Content-Type: application/json` with each POST. Without `Accept`,
  the SDK returns a plain-text 400. Without `Content-Type`, it returns a
  plain-text 415. These are not JSON-RPC errors.
- A message whose `id` is a number is refused unless the number is a
  plain integer, with no fraction or exponent, within
  ±9007199254740991. The SDK would round any other number, and the
  answer would carry a different id. The refusal is 400 with JSON-RPC
  error -32600 and a null id, and nothing runs. A string `id` is never
  refused.
- Each request gets its own stateless server, which answers plain JSON
  and never advertises list changes.
- `tools/list` shows only the tools `Gate.Admit` and `op.Check` allow
  the caller, without the agent-session check, which a list never needs.
  Only `op.Forbidden` or `op.NotFound` hides a tool. Any other error
  fails the list as a JSON-RPC internal error, with a ref in its data.
- A tool call runs `Gate.Admit` and `op.Check` before `Invoke`, so a
  hand-written `Invoke` that skips `op.Check` is still checked. A
  refusal is a tool error holding the API's error envelope, from
  `api.Refusal`. A panic in a call is an `internal` tool error with a
  ref. So is a result that isn't an object, including `null`, when the
  tool's `Result` is an object schema.
- With a `Rechecker`, the gate checks the answer again as it goes out.
  For a call, it reruns `Admit` and `op.Check` for the operation that
  ran on every answer. It runs `Guard` only when the call returned a
  successful result. For the list, it reruns them for every tool that
  was listed, then runs `Guard` with a nil descriptor and the listed
  descriptors as the result.

`New` raises `Gate.MaxResponse`, when it is lower, to three times
`MaxResult` plus 64 KiB. A result goes out twice, as
`structuredContent` and as the text block, and escaping can make the
text longer.

## ToolName

```go
func ToolName(id string) string
```

The tool an operation is served as: `bookmarks.create` is
`bookmarks_create`.
