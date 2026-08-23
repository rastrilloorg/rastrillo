# 🤖 view

`github.com/carlosframework/rastrillo/view`

The plain HTTP-response helpers a generated action needs against a
`*rastrillo.Ctx`. Three functions. Hand-written handlers are welcome to
use them and equally welcome not to.

## Render

```go
func Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any)
```

Renders a named page at an explicit status. The status is a parameter
rather than an assumption, because the commonest render in a form app is
a 422 re-render rather than a 200.

A nil renderer on the `Ctx` logs and answers 500 rather than panicking
mid-response.

## Fail

```go
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error)
```

A safe 500. The real error goes to the logger with `what` as context;
the response body says nothing about it.

That split is the whole point. An error string is written for an
operator reading logs, and it routinely names a table, a path or a
query — none of which belongs in a response to someone who may have
caused the error deliberately.

## ParseID

```go
func ParseID(r *http.Request) (int64, bool)
```

Reads the `{id}` path value as an `int64`.

A malformed id answers `false`, and your handler should turn that into a
**404, not a 400**. "That is not a valid id" and "that id is not yours"
should be indistinguishable from outside — see
[Scoping](/docs/scoping).
