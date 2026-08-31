# 🤖 view

`amadan.net/rastrillo/rastrillo/view`

The plain HTTP-response helpers a generated action needs against a
`*rastrillo.Ctx`. Hand-written handlers are welcome to use them and
equally welcome not to.

## Render

```go
func Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any)
```

Renders a named page at an explicit status. The status is a parameter
because the commonest render in a form app is a 422 re-render, not a
200.

A nil renderer on the `Ctx` logs and answers 500 instead of panicking
mid-response.

## Fail

```go
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, what string, err error)
```

A safe 500. The real error goes to the logger with `what` as context,
and the response body says nothing about it.

That split is the point. An error string is written for an operator
reading logs, and it routinely names a table, a path or a query — none
of which belongs in a response to someone who may have caused the error
deliberately.

What the caller *does* get is a reference: `Fail` mints one
([`rastrillo.NewRef`](/docs/reference/rastrillo)) and puts it in both
places — the log line, under `ref`, and the page. Six characters is
what a person will actually quote down a phone line.

The response itself is your own error page when
[`Ctx.ErrorPage`](/docs/reference/rastrillo) is wired, JSON when the
client sent `Accept: application/json`, and plain text otherwise. `r`
may be nil for a caller with no request in scope; that is the plain-text
case too.

## NotFound and Forbidden

```go
func NotFound(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
func Forbidden(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
```

The same three answers at 404 and 403, and neither logs or carries a
reference: there is nothing to look up afterwards.

`Forbidden` says nothing about what exists. "You can't see this" and
"there is nothing here" are deliberately the same amount of information
beyond the status itself — see [Scoping](/docs/scoping).

## ParseID

```go
func ParseID(r *http.Request) (int64, bool)
```

Reads the `{id}` path value as an `int64`.

A malformed id answers `false`, and your handler should turn that into a
404, not a 400. "That is not a valid id" and "that id is not yours"
should be indistinguishable from outside — see
[Scoping](/docs/scoping).
