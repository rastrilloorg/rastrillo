# 🤖 action

`amadan.net/rastrillo/rastrillo/action`

Write an action as one function of one value. A `Visit` holds the
response, the request and the app's context, and has a method for each
common step: read a form, render a page, flash a notice, redirect, answer
a refusal. The route table mounts your actions on a chi router and lists
what it mounted.

You don't need to rewrite existing handlers: write new routes this way,
and move old ones when you change them.

`action` is built on [view](/docs/reference/view),
[flash](/docs/reference/flash), [form](/docs/reference/form) and
[op](/docs/reference/op), and changes none of them. Your actions are
ordinary `net/http` handlers underneath, so they can share a router with
generated actions.

## An action

```go
const tagPage action.Path = "/tags/{id}"

type tagForm struct{ parsed *form.Parsed }

func (again tagForm) ShowAgain(
	visit *action.Visit, status int, errors form.Errors,
) {
	visit.Render("tag_edit", status, map[string]any{
		"Echo": again.parsed.Echo(), "Errors": errors,
	})
}

func (app *App) tagRename(visit *action.Visit) {
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
	visit.Redirect(tagPage.For(id))
}
```

An action does four things in order. It reads the input. It runs one
operation. It answers a refusal and returns. It says what success looks
like. Mount it, and the page it redirects to, on the same path:

    routes.Get(tagPage, app.tagShow)
    routes.Post(tagPage, app.tagRename)

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
func (routes *Routes) Get(path Path, handler Handler)
func (routes *Routes) Post(path Path, handler Handler)
func (routes *Routes) Handle(method string, path Path, handler http.Handler, name string)
func (routes *Routes) Mount(prefix Path, handler http.Handler, name string)
```

`Get` and `Post` mount an action. `Handle` mounts an existing
`net/http` handler for any method. `Mount` hands a whole subtree, such as
your API, to its own handler. `Handle` and `Mount` take a `name`, because
a handler of that kind has no function name worth showing in the list.
Every path is the full path from the root. A quoted string such as
`"/tags/{id}"` works where a `Path` is asked for.

```go
func (routes *Routes) Guard(name string, middleware func(http.Handler) http.Handler)
func (routes *Routes) Group(mount func(routes *Routes))
func (routes *Routes) Route(prefix Path, mount func(routes *Routes))
```

`Guard` runs middleware before every route mounted after it in the same
group. Call it before the group's first route: it panics if a route came
first, because chi would leave that route unguarded without a word.
`Group` shares guards without sharing a path prefix. `Route` mounts
routes under a prefix. A guard added inside a `Group` or a `Route` does
not reach the routes outside it.

Inside a `Route`, still write each path in full, starting with the
prefix:

```go
routes.Route("/admin", func(admin *action.Routes) {
	admin.Guard("admin", app.requireAdmin)
	admin.Get("/admin", app.adminHome)
	admin.Get("/admin/tags", app.adminTags)
})
```

A path that does not start with the prefix panics when you register it,
and the message gives the full path to write.

Register each method and path once. A second `Get`, `Post` or `Handle`
for the same method and path panics, even from another group or `Route`,
because chi would serve only the second one. Parameter names do not make
two paths different: `/tags/{id}` and `/tags/{name}` are the same path.
A pattern does: `/tags/{id}` and `/tags/{id:[0-9]+}` are two.

```go
type Route struct {
	Method  string
	Path    string
	Handler string
	Action  string
	Guards  []string
}

func (routes *Routes) List() []Route
```

`List` returns every route mounted so far, in the order it was mounted.
Each `Path` is the path as you wrote it, and `Guards` names the guards
that apply. A `Get` or `Post` is listed by its function's name, so a
method value such as `app.tagRename` appears as `tagRename`. Its `Action`
is the full name, such as `example.com/app/internal/app.(*App).tagRename`.
Compare `Action`, not `Handler`, to tell two methods called `Show` apart.
`Action` is empty for `Handle` and `Mount`. A `Mount` is one line, `*`
with the path `<prefix>/*`. Read `List` in a test to check that an admin
route has its guard, without serving a request.

## Naming a path

```go
type Path string

func (path Path) For(values ...any) string
```

Name a route's path once, and use the name both to register the route
and to fill the URL you redirect to:

```go
const tagPage action.Path = "/tags/{id}"

routes.Get(tagPage, app.tagShow)
visit.Redirect(tagPage.For(id))
```

`For` fills the path's parameters with your values, in order. Each value
is escaped as one part of the path, so `"a/b"` stays one value, and
`PathValue` gives it back as you passed it. A path with no parameters
comes back as it is.

`For` panics when the number of values does not match the parameters,
when the path ends in a `*`, and when a value is empty, `.` or `..`,
which would send the redirect somewhere else. It also panics on a value
that chi would read back as a different one:

- A value that contains the character right after its parameter, such as
  `"a-b"` for `{owner}` in `"/{owner}-{name}"`, or `"a.b"` in
  `"/x-{id}.json"`. Pass a value without that character, or put a `/`
  after the parameter.
- A value that the parameter's pattern does not match, such as `42` for
  `{name:[a-z]+}`. Pass a value the pattern matches.
- For a parameter with a pattern, a value that escaping would change,
  such as `"x y"`. Use only letters, digits, `-` and `_` there, or drop
  the pattern.

Each is a mistake in the code, so the first test that runs it fails.

## Registering every route

Register every route in one function, in a package under `internal/`:

```go
func Routes(routes *action.Routes, app *App)
```

`App` can be any exported type in that package, or a pointer to one.
Your app calls `Routes` with its real dependencies when it serves.
[`rastrillo routes`](/docs/cli#rastrillo-routes) calls it with a
recorder and an empty `App` to print the route table, so `Routes` must
only register: no queries, no network calls, nothing built. Code that
runs when the package loads must not need the app's dependencies either.

```go
func NewRecorder() *Routes
```

`NewRecorder` returns a route table that records what you register and
serves nothing. It never builds a guard or calls a handler, so `Routes`
can run on it with an empty `App`. `List` reports what a real table
would. It refuses what chi would refuse, such as the same prefix mounted
twice, so an app that lists cleanly also starts.

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
request's own path values. A value that `For` filled comes back as it
was given, even one with `/` or `%` in it. `PathID` reads a row id. An id that is not a
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
