# 🤖 Agents and tools

An action can opt in as an agent-callable tool. The generator collects
the registry, and the `tools` package renders schemas and dispatches
model-proposed calls back through the same mux — a tool call and an
HTTP POST reach the identical `Handle` function.

## Marking an action

Declared in the action file, next to `Handle`:

```go
var Tool = rastrillo.Tool{
	Description: "Cancel one order and release its tickets.",
	Access:      rastrillo.ToolWrite,
	Args:        map[string]string{"id": "the order id"},
}
```

`rastrillo generate` emits the registry as `gen.Tools()`.

## Read and write are treated differently

`rastrillo.ToolRead` observes and never changes state.

`rastrillo.ToolWrite` changes state, and therefore requires a `Confirm`
sentence and an explicitly confirmed call — the same consent a confirm
page asks a human for. `tools.ConfirmSentence(def, args)` renders that
sentence with the call's actual arguments in it, so what is approved is
what will happen.

## Dispatching

```go
schemas := tools.Schemas(gen.Tools())
payload, err := tools.SchemasJSON(gen.Tools())

result, err := tools.Dispatch(handler, gen.Tools(), call)
```

`Dispatch` validates the call against the registry, applies the consent
gate, attributes the actor, and routes it through your ordinary
`http.Handler`. A tool cannot reach a route that is not a registered
tool, and it cannot skip the middleware your routes sit behind —
including [CSRF](/docs/sessions) and the session guards.

There is no LLM client. Choosing a provider is the app's business;
Rastrillo supplies the registry, the schemas and the dispatch.

## The sidecar

An app can run scheduled or delivery-driven work outside a request:

```go
opts.Sidecar = func(ctx context.Context) (time.Time, error) {
	// one pass; return when the next pass is due
}
```

`Options.Sidecar` plus the `sidecar run` argv speak the platform's
sidecar contract, and `Options.NextDue` answers the activator's
`GET /api/next-due` scheduled-wake poll.

A pass returning a zero next-due time means "no scheduled work", and the
loop sleeps for a minute — the family's polling floor.

A sidecar **never dies on a pass error**. The platform would only
respawn it into the same failure, so it backs off and retries, capped at
five minutes so a dead dependency cannot turn the loop into a busy-wait.
