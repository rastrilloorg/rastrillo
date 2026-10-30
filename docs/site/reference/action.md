# 🤖 action

`amadan.net/rastrillo/rastrillo/action`

Write an action as one function of one value. A `Visit` holds the
response, the request and the app's context, and has a method for each
common step: read a form, render a page, flash a notice, redirect, answer
a refusal. The route table mounts your actions on a chi router and lists
what it mounted.

`action` is built on [view](/docs/reference/view),
[flash](/docs/reference/flash), [form](/docs/reference/form) and
[op](/docs/reference/op), and changes none of them. Your actions are
ordinary `net/http` handlers underneath, so they can share a router with
generated actions.

## An action

```go
type tagForm struct{ parsed *form.Parsed }

func (again tagForm) ShowAgain(
	visit *action.Visit, status int, errors form.Errors,
) {
	visit.Render("tag_edit", status, map[string]any{
		"Echo": again.parsed.Echo(), "Errors": errors,
	})
}

func (app *app) tagRename(visit *action.Visit) {
	parsed, err := visit.ReadForm(form.Field{Name: "Name", Required: true})
	if visit.RefusedShowing(err, tagForm{parsed}) {
		return
	}
	id, err := visit.PathID("id")
	if visit.Refused(err) {
		return
	}
	err = app.tags.Rename(visit.Context(), id, parsed.String("Name"))
	if visit.RefusedShowing(err, tagForm{parsed}) {
		return
	}
	visit.Flash("Tag renamed.")
	visit.Redirect("/tags")
}
```

An action does four things in order. It reads the input. It runs one
operation. It answers a refusal and returns. It says what success looks
like. Mount it with `routes.Post("/tags/{id}", app.tagRename)`.

## Routes

```go
type Handler func(visit *Visit)
type CtxFor func(request *http.Request) (*rastrillo.Ctx, error)

func NewRoutes(router chi.Router, ctxFor CtxFor) *Routes
```

`Handler` is an action. `NewRoutes` mounts on `router`. `ctxFor`
builds the app's `*rastrillo.Ctx` once per request, never when you
register a route, so you can register every route before the app's
stores exist. If `ctxFor` returns an error, the request answers a logged
500 and your action does not run.

The request's actor goes on a copy of what `ctxFor` returns. It is the
actor on the request's context if there is one, and otherwise the
signed-in session's, when the `Ctx` carries none. Your `ctxFor` can hand
every request the same `Ctx`: one person's actor never reaches the next
request.

```go
func (routes *Routes) Get(path string, handler Handler)
func (routes *Routes) Post(path string, handler Handler)
func (routes *Routes) Handle(method, path string, handler http.Handler, name string)
func (routes *Routes) Mount(prefix string, handler http.Handler, name string)
```

`Get` and `Post` mount an action. `Handle` mounts an existing
`net/http` handler for any method. `Mount` hands a whole subtree, such as
your API, to its own handler. `Handle` and `Mount` take a `name`, because
a handler of that kind has no function name worth showing in the list.

```go
func (routes *Routes) Guard(name string, middleware func(http.Handler) http.Handler)
func (routes *Routes) Group(mount func(routes *Routes))
func (routes *Routes) Route(prefix string, mount func(routes *Routes))
```

`Guard` runs middleware before every route mounted after it in the same
group. Call it before the group's first route: it panics if a route came
first, because chi would leave that route unguarded without a word.
`Group` shares guards without sharing a path prefix. `Route` mounts
routes under a prefix. A guard added inside a `Group` or a `Route` does
not reach the routes outside it.

```go
type Route struct {
	Method  string
	Path    string
	Handler string
	Guards  []string
}

func (routes *Routes) List() []Route
```

`List` returns every route mounted so far, in the order it was mounted.
Each `Path` is the full path with its prefixes, and `Guards` names the
guards that apply. A `Get` or `Post` is listed by its function's name, so
a method value such as `app.tagRename` appears as `tagRename`. A `Mount`
is one line, `*` with the path `<prefix>/*`. Read `List` in a test to
check that an admin route has its guard, without serving a request.

## Visit

```go
type Visit struct{ /* unexported */ }

func NewVisit(response http.ResponseWriter, request *http.Request, ctx *rastrillo.Ctx) *Visit
```

`Visit` is one request as an action sees it. The route table makes it for
you. Call `NewVisit` only to test an action without a router.

```go
func (visit *Visit) Response() http.ResponseWriter
func (visit *Visit) Request() *http.Request
func (visit *Visit) Ctx() *rastrillo.Ctx
func (visit *Visit) Context() context.Context
```

`Response` and `Request` are for what `Visit` has no word for, such as a
header or a query string. `Ctx` is the app's per-request context. Pass
`Context` to a store call, so the call stops when the client goes away.

```go
func (visit *Visit) PathValue(name string) string
func (visit *Visit) PathID(name string) (int64, error)
```

`PathValue` reads a URL parameter, from chi first and then from the
request's own path values. `PathID` reads a row id. An id that is not a
whole number above zero returns `op.NotFound`, so the person sees the 404
page, not a 400.

```go
func (visit *Visit) Render(page string, status int, data any)
func (visit *Visit) Flash(message string)
func (visit *Visit) Redirect(path string)
```

`Render` shows a page through the app's renderer at the status you give,
as `view.Render` does. `Flash` queues a notice of kind `"notice"` for the
next page. Call it before `Redirect`, which sends the cookie with the
redirect. `Redirect` answers 303, so the browser follows a POST with a GET
and a reload does not submit the form twice.

## Reading a form

```go
func (visit *Visit) ReadBody() error
func (visit *Visit) ReadForm(fields ...form.Field) (*form.Parsed, error)
```

`ReadBody` limits the body to 1 MiB and parses it. An error is a
`BadRequest`. Calling it again returns the same answer. `ReadForm` reads
the body and then your fields. Call it before any path or database work,
so a broken body is a 400 whatever else is wrong with the request.

When a field does not parse, `ReadForm` returns the `*form.Parsed` and an
`op.Invalid` that names each field. You get the `Parsed` even then,
because showing the form again needs what the person typed (`Echo`). When
the body is broken, it returns `nil` and a `BadRequest`.

```go
type BadRequest struct {
	Status  int
	Message string
}
```

`BadRequest` is the refusal for a body that could not be read. It answers
`Message` at `Status` and shows no form. `ReadBody` returns it with 400
and `Bad request.`. `BadRequest.Error` returns the message.

## Answering a refusal

```go
func (visit *Visit) Refused(err error) bool
func (visit *Visit) RefusedShowing(err error, again Form) bool
```

Both answer the error and report whether they did. A `nil` error writes
nothing and returns `false`. Write `if visit.Refused(err) { return }`.

| Error | Answer |
| --- | --- |
| `BadRequest` | its `Message` at its `Status` |
| `op.NotFound` | the 404 page |
| `op.Forbidden` | the 403 page |
| `op.Invalid` | the form again at 422 (`RefusedShowing` only) |
| `op.Conflict` | the form again at 409 (`RefusedShowing` only) |
| anything else | a logged 500 |

`Refused` answers the last row for an `op.Invalid` or `op.Conflict` too,
because it has no form to show. Use `RefusedShowing` on any page that has
one.

```go
type Form interface {
	ShowAgain(visit *Visit, status int, errors form.Errors)
}

const WholeForm = ""
```

`RefusedShowing` calls `ShowAgain` with the status and the errors to
render. A conflict arrives as one message under the key `WholeForm`, which
is about the whole form and not one field. Pass `WholeForm` to your
form-error partial to show it above the fields.

`RefusedShowing` panics if `again` is `nil`. A page with no form would
answer a typo with a 500, and the panic makes that mistake fail in the
first test run. Use `Refused` for a page with no form.
