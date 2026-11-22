# 🤖 Named operations and parity

An operation is one thing an app can do, such as publishing a post.
Write it once with `op.Define`, and the page, the [JSON API](/docs/api)
and [MCP](/docs/mcp) all call that function. Use
[`paritytest.Routes`](/docs/reference/paritytest) to check that every
page names its operation and that the API and MCP serve it. Use
`paritytest.Run` to check that a person on the page and their agent get
the same answers.

You need this guide when an app has no manifest, or has actions the
generator doesn't write. Apps whose resources are all generated already
get their operations and their parity test from `rastrillo generate`.

## Declaring an operation

```go
type PublishInput struct {
	ID int64 `json:"id"`
}

type PublishResult struct {
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
}

var Publish = op.Define("posts.publish", op.Spec{
	Description: "Publish a draft post. Readers see it at once.",
	Confirm:     "Publish post {id}?",
	Access:      rastrillo.Write,
	Scoped:      true,
}, publish)

func publish(
	ctx context.Context, rc *rastrillo.Ctx, in PublishInput,
) (PublishResult, error) {
	// Check has already run. Put the rule here, once.
}
```

`op.Define` takes an ID, a `Spec` and the function that does the work.
It returns a `*op.Named[In, Out]`, whose `Descriptor` is what the API
and MCP serve and whose `Call` is what a page uses. The ID is
`<area>.<verb>` in lowercase letters, digits and underscores. The API
route is `POST /api/v1/posts/publish`, and the MCP tool is
`posts_publish`.

`Define` panics at start-up if the ID is invalid, `Description` is
empty, or a field can't be described. The message says what to change.

### Wire names

Every exported field of the input and the result needs a `json` tag,
and the tag is the snake_case form of the Go name: `PublishedAt` is
`published_at`. `Define` panics on any other tag. The rule exists so
that one name works everywhere. `op.Invalid.Fields` is keyed by the Go
name, `PublishedAt`, as the page needs it, and the API and MCP report
the same field as `published_at`.

### Input and output types

- The input is a struct, and the result is a struct or `op.None`.
  Structs must be JSON objects on the wire, so `time.Time` is refused. A
  field can't hold `op.None`.
- A required input field is a plain value. A field a caller may leave
  out is `op.Opt[T]`, and one that may be `null` is `op.Nullable[T]`.
  A pointer in an input is refused.
- A required member that is left out reaches your function as its zero
  value, because decoding doesn't enforce `required`. Refuse it in the
  function with `op.Invalid`, and every door gives the same answer.
- A result may use pointers for values that can be `null`, but not
  `op.Opt` or `op.Nullable`.
- Strings, numbers, bools, structs, slices and maps with string keys
  are allowed. A type that contains itself is refused.
- A field of type `any`, raw JSON, or a type with its own JSON methods
  may hold any JSON value except `null`. Use `op.Nullable` to allow
  `null`.

`Input` and `Result` on the descriptor are JSON Schema that `Define`
derives from these types, so they can't drift from the code.

### Scoped, and tokens

`Scoped: true` says the rows belong to one person, and `op.Check`
refuses a caller with no `Subject`. Your function must filter by that
subject.

If `Scoped` is false, `op.DefaultPolicy` refuses every token, because
your page middleware may guard the operation in a way the API can't see.
To let tokens call it, say so in your `Authorize`, and call
`DefaultPolicy` for everything else:

```go
if o.ID == "posts.publish" && a.IsToken() {
	return nil
}
return op.DefaultPolicy(ctx, a, o)
```

### Reads, writes and human-only

`Access` is `rastrillo.Read` or `rastrillo.Write`. A write whose verb
undoes or sends something, such as `cancel` or `send`, must be
`Destructive`. See [the flags](/docs/mcp#the-flags).

`HumanOnly` is a sentence saying why only a person may call the
operation, such as creating an API token. Every token is refused on it,
including a person's own, because an agent may be holding that token.

## The page calls Call

```go
func (app *App) publishPost(
	response http.ResponseWriter, request *http.Request,
) {
	id, _ := strconv.ParseInt(request.PathValue("id"), 10, 64)
	_, err := posts.Publish.Call(request.Context(), app.ctxFor(request),
		posts.PublishInput{ID: id})
	var invalid op.Invalid
	if errors.As(err, &invalid) {
		app.showErrors(response, request, invalid.Fields)
		return
	}
	if err != nil {
		app.fail(response, request, err)
		return
	}
	http.Redirect(response, request, "/posts", http.StatusSeeOther)
}
```

A page does four things:

1. Read the form into the input type.
2. Call `Call` with the page's `Ctx`. It runs `op.Check`, then your
   function.
3. On `op.Invalid`, show each message beside its field. `Fields` is
   keyed by the Go name.
4. On any other error, answer as your pages already do. On success,
   redirect or render.

Put each rule in the operation. Do not check it again in the page. If
you find yourself checking a title's length there, move the check into
the operation.

## Serving it

Pass the same descriptors to both doors:

```go
operations := []op.Descriptor{posts.Publish.Descriptor}

apiHandler, err := api.New(config, api.Operations(operations...)...)
mcpHandler, err := mcp.New(mcpConfig, operations...)
```

`api.Operations` builds one route for each descriptor, and you can pass
it beside routes you wrote by hand. `mcp.New` takes the descriptors as
they are. Mount both as the [API guide](/docs/api#mounting-the-api) and
the [MCP guide](/docs/mcp#mounting) say.

Both doors run `op.Check` before they read any input, so a refused
caller learns nothing about your input rules.

## POST and GET

Every operation is a POST with a JSON body. A body needs
`Content-Type: application/json`, or the answer is 415. A POST with no
body at all runs as `{}`, so an operation whose inputs are all optional
works with a bare POST.

A read may be a GET. Set `InQuery: true` in its `Spec`, and every input
field must be a string, number or bool:

```go
var List = op.Define("posts.list", op.Spec{
	Description: "List posts.",
	Access:      rastrillo.Read,
	InQuery:     true,
}, list)
```

`GET /api/v1/posts/list?status=draft` reads the query as the same JSON
object. A repeated or unknown parameter, or a value of the wrong type,
is a 422 that names the parameter.

Each operation reads its arguments from one place. A POST with a query
string, or a GET with a body, is a 422, so no argument is ever dropped
silently.

An operation that returns `op.None` answers 204 with no body.

## The refusal order

Every door refuses in the same order, and the first refusal answers:

1. Human-only. Any token or agent gets `human_only`, never `forbidden`
   or `invalid`, and never a field error. Every token is refused, so
   that no agent can use a person's own token to get past it.
2. Access. A read-only token on a write gets `forbidden`.
3. The agent session. With `RequireAgentSession`, an agent's write with
   no `Agent-Session` gets `invalid`.
4. Your `Authorize`.
5. Input validation.

Human-only operations stay registered on both doors. MCP hides them
from the tool list and refuses a call to one, so the answer is the same
whether or not an agent guessed the name.

## The route table

`paritytest` needs to know which page serves which operation. Register
page routes through a `route.Table`, and give each a mark:

```go
router := chi.NewRouter()
table := route.New(route.Chi(router))
table.Handle("POST /posts/{id}/publish", app.publishPost,
	route.Operation(posts.Publish.Descriptor))
table.Handle("GET /style.css", app.stylesheet,
	route.PageOnly("the stylesheet"))
```

`route.Operation` names the operations the page is the door of.
`route.PageOnly` says why an agent has no use for the route. Use `On`
for a sub-router, so its routes are recorded under their full path. The
same method and path registered twice panics at start-up. See the
[route reference](/docs/reference/route).

An app on `action.Routes` passes the same marks to `Get`, `Post` and
`Handle`.

## paritytest.Routes

```go
func TestParity(t *testing.T) {
	app := newApp(t)
	routeparity.Routes(t, routeparity.Surface{
		Routes:     app.Table.List(),
		Operations: app.Operations(),
		Generated:  gen.Operations(),
		API:        app.API,
		MCP:        app.MCP,
		Handler:    app.Router,
		Token:      adminAgentToken,
		Router:     app.Router,
	})
}
```

It compares the whole inventory of the page table, the API and MCP with
the operations you declare, and reports every problem it finds. In
brief:

- A route with no mark, or a mark naming an operation you haven't
  declared, means the table and the declaration disagree.
- An operation with no page means a person can't do what an agent can.
- A declared operation missing from a door, or served there with a
  different descriptor, means the door was given the wrong list.
- An operation a door serves that you didn't declare is an
  unreviewed way in.
- A request with no credential that doesn't answer 401 with
  `Rastrillo-Operation` set means a catch-all or middleware sits in
  front of the API.
- A human-only operation that MCP lists, or that a door answers with
  anything but `human_only`, means the marking isn't working.

`Token` must be an agent's write token for a person your `Authorize`
allows every operation, so that only a fault in the app hides a tool.
`Generated` is `gen.Operations()`: those operations are checked by
`gen/paritytest`, so here they only count as declared. Import this
package beside `gen/paritytest` under an alias, such as `routeparity`.

## paritytest.Run

`Routes` checks what is served. `Run` checks what happens. It runs the
same steps on fresh copies of the app, as a person on the page and as
their agent on the API and on MCP, and compares the answers and the
stored state.

Write a fixture once. It builds a fresh app, makes a token for a
person, drives your page for a step, and reports what the app stored:

```go
type fixture struct{ app *App }

func (f fixture) Surface() routeparity.Surface {
	return f.app.surface()
}
func (f fixture) Token(t testing.TB, person string) string {
	return f.app.tokenFor(person)
}

func (f fixture) Page(
	t testing.TB, person string, step routeparity.Step,
) routeparity.Outcome {
	return f.app.postForm(t, person, step)
}

func (f fixture) State(t testing.TB) any { return f.app.titles() }
```

Then write the scenario. Include a refused step, because the doors must
refuse alike:

```go
func TestPostsScenario(t *testing.T) {
	routeparity.Run(t, routeparity.Scenario{
		Fresh: func(t testing.TB) routeparity.Fixture {
			return fixture{app: newApp(t)}
		},
		Steps: []routeparity.Step{
			{Name: "a publishes a draft", Person: "a",
				Operation: posts.Publish.Descriptor,
				Input:     map[string]any{"id": 1}},
			{Name: "a publishes nothing", Person: "a",
				Operation: posts.Publish.Descriptor,
				Input:     map[string]any{}},
		},
	})
}
```

A step that calls a human-only operation fails the scenario, because
the doors answer it differently on purpose. `Routes` checks those.

## What it doesn't catch

- A route registered outside the table is seen only on chi, and only
  when `Surface.Router` is set. A route registered straight on a
  `ServeMux` isn't seen.
- A write an operation doesn't make. If a page also sends an email
  after `Call` returns, nothing compares that with the API. Put the
  write inside the operation, where every door gets it.
- A rule the page enforces and the operation doesn't. `Run` finds it
  only if a step exercises it.

## rastrillo routes --check

Once an app declares an operation, a call to `op.Define` anywhere in
the module, [`rastrillo routes --check`](/docs/cli#rastrillo-routes)
fails each `action.Routes` route that has no mark. Give `Get`, `Post`
or `Handle` a `route.Operation` or a `route.PageOnly`, as above. A
`Mount` needs none. An app that has declared no operation is not held
to the rule.
