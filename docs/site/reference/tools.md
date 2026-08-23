# 🤖 tools

`github.com/carlosframework/rastrillo/tools`

Renders agent tool schemas and dispatches model-proposed calls back
through the app's own mux. A tool call and an HTTP POST reach the
identical `Handle` function.

[Agents and tools](/docs/agents) is the guide, and
[`rastrillo.Tool`](/docs/reference/rastrillo) is how an action opts in.

## Schemas

```go
func Schemas(defs []rastrillo.ToolDef) []Schema
func SchemasJSON(defs []rastrillo.ToolDef) ([]byte, error)
```

Turn the generated registry — `gen.Tools()` — into the schema list an
LLM provider expects. `Schema` is one tool's description and argument
map.

There is no LLM client here. Choosing a provider is the app's business;
this package supplies the registry, the schemas and the dispatch.

## Dispatch

```go
func Dispatch(handler http.Handler, defs []rastrillo.ToolDef, call Call) (Result, error)
```

Validates a `Call` against the registry, applies the consent gate,
attributes the actor, and routes it through your ordinary
`http.Handler`.

Because it goes through the same handler, a tool call cannot reach a
route that is not a registered tool and cannot skip the middleware your
routes sit behind — including CSRF and the session guards. `Result`
carries what the handler answered.

## The three refusals

| Error | Cause |
|---|---|
| `ErrUnknownTool` | the call names a tool the registry does not have |
| `ErrUnknownArg` | the call passes an argument the tool does not declare |
| `ErrConfirmRequired` | a `ToolWrite` call arrived without confirmation |

`ErrUnknownArg` matters more than it looks. Silently dropping an
unrecognised argument would let a model believe it had constrained an
operation that in fact ran unconstrained.

## ConfirmSentence

```go
func ConfirmSentence(def rastrillo.ToolDef, args map[string]string) string
```

Renders a write tool's confirmation sentence with the call's actual
arguments in it, so what a human approves is what will happen — not a
generic description of what the tool can do.
