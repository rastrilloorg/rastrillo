# 🤖 JSON API

Every manifest resource can also be a JSON API at `/api/v1`. A person
makes a token for an agent, such as Claude, and the agent calls the API
with it. Every call acts for that person and says so: "Claude for Paul".

The pages and the API call the same operation for each action, so they
accept the same values and follow the same rules.

## What you get

A resource with `scope = "user"` is on the API by default. For a
resource named `bookmarks`:

| Method | Path | Does | Answers |
|---|---|---|---|
| GET | `/api/v1/bookmarks` | list | 200 |
| POST | `/api/v1/bookmarks` | create | 201, with `Location` |
| GET | `/api/v1/bookmarks/{id}` | get one | 200 |
| PATCH | `/api/v1/bookmarks/{id}` | change some fields | 200 |
| DELETE | `/api/v1/bookmarks/{id}` | delete | 204 |

`rastrillo generate` writes the operations to `gen/ops/<name>/`, the API
handlers to `gen/api/<name>/doors.go`, and the list of routes to
`gen.APIRoutes()`. The page actions in `gen/actions/` call the same
operations.

## Mounting the API

A new app from `rastrillo new` already has this in
`internal/<app>/api.go`, and its `Mux` mounts it. To add it to an
existing app:

```go
tokens, err := apitoken.New(apitoken.Config{DB: d.Writer(), Prefix: "notes"})
if err != nil {
	return nil, err
}
apiTree, err := api.New(api.Config{
	Verify:    tokens,
	Ctx:       ctxFactory,
	Authorize: authorize,
	Logger:    logger,
	Redact:    tokens.Redact,
}, gen.APIRoutes()...)
if err != nil {
	return nil, err
}
mux := http.NewServeMux()
mux.Handle("/", router)
mux.Handle("/api/v1", apiTree)
mux.Handle("/api/v1/", apiTree)
```

1. Add `apitoken.Schema` to `BootSchema` in `migrations.go`.
2. Write the token prefix out as text. `apitoken.DerivePrefix` makes one
   from your app's name.
3. Mount the tree on the outer `http.ServeMux`, beside your chi router.
   Mount it at both `/api/v1` and `/api/v1/`.
4. Pass the same `authorize` function to your page ctx factory as
   `Ctx.Authorize`, so the pages and the API share one policy.

Do not mount the API inside the chi router. `csrf.Protect` refuses a
write that has no browser headers, and an API client has none. Do not
use `csrf.Exempt` for it either. `Exempt` matches exact paths, not
everything under `/api/v1/`, and the router's session middleware would
still run. The API takes a token and nothing else.

`api.New` fails at boot if `Verify`, `Ctx` or `Authorize` is missing.
The [api reference](/docs/reference/api) has every option.

## Who may do what

Every operation starts with `op.Check`. It asks, in order:

1. Is this a read-only token on a write? Refused.
2. Is this a resource with `scope = "user"` and no signed-in person?
   Refused.
3. Your `Authorize` function decides. With none set, `op.DefaultPolicy`
   decides.

`op.DefaultPolicy` lets a token do what its owner can do on a
`scope = "user"` resource: read and change their own rows. On a
resource with no owner, it refuses every token, even when the manifest
sets `expose = true`. People using the pages are not affected.

The API cannot see your page middleware. If your pages check a role, a
suspended account or a recent sign-in, write that rule into
`Authorize`. Return `op.Forbidden` to refuse, and end with
`op.DefaultPolicy`:

```go
func authorize(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error {
	if o.Resource == "ticket_types" && o.Access == rastrillo.Write && !isAdmin(ctx, a.Subject) {
		return op.Forbidden{Reason: "not an admin"}
	}
	if o.Resource == "ticket_types" && o.Access == rastrillo.Read && a.IsToken() {
		return nil // tokens may read ticket types
	}
	return op.DefaultPolicy(ctx, a, o)
}
```

Return `op.Forbidden` or `op.NotFound` to refuse. The API answers 403
or 404, and a page shows its 403 or 404 page. `Forbidden.Reason` goes
to your log, never to the caller.

`op.Check` returns your error unchanged. The API answers any error
from the [errors table](#requests-and-answers) with its own code, so an
`op.Conflict` is 409. An error it does not recognise is 500.

Generated page actions call the same operations, so your rules apply to
the pages too.

## Tokens

A token looks like `notes_k3Jx9QpL2mWa_<secret>`: your app's prefix, a
public ID, and a secret. The public ID is safe to show and to log. The
secret is shown once, when the token is made, and the database keeps
only its SHA-256.

Do not change the prefix once tokens exist. Every token made with the
old prefix stops working.

### The token page

`apitoken.NewHandlers` gives you the page's handlers: list, create and
three ways to revoke. Your `Render` draws the page with `ui`'s
`api-tokens` partial, from `apitoken.PageData`. `examples/notes` shows
the page mounted in a working app.

A new app has the page in `tokens.go` and `templates/tokens.html`, but
does not mount it, because the page needs a signed-in person. Once your
app has sign-in, add this to `Router` after `csrf.Protect`:

```go
tokens, err := Tokens(d, sess)
if err != nil {
	return nil, err
}
r.Group(tokens)
```

To mount the handlers yourself, put them in your signed-in group,
behind `csrf.Protect`, and put `Create` behind
`sess.RequireFresh(10 * time.Minute)` as well:

```go
r.Get("/tokens", th.Page)
r.With(sess.RequireFresh(10*time.Minute)).Post("/tokens", th.Create)
r.Post("/tokens/revoke", th.Revoke)
r.Post("/tokens/revoke-agent", th.RevokeAgent)
r.Post("/tokens/revoke-all", th.RevokeAll)
```

On the page, a person names the agent the token is for, or ticks "This
token is for me, not an agent". They choose "Read and write" or "Read
only", and when it expires: 30 days, 90 days, 1 year or never. Read and
write and 90 days are preselected.

They can revoke one token, every token for one agent, or all of them.
Revoking does not need a recent sign-in.

## Calling the API

Send the token in the `Authorization` header:

```sh
curl -H "Authorization: Bearer $TOKEN" \
     -H "Agent-Session: 7f3c2a" \
     -H "Agent-Harness: claude-code/2.1.288" \
     https://notes.example.com/api/v1/bookmarks
```

An agent may send two more headers:

- `Agent-Session`: the agent's own session ID.
- `Agent-Harness`: the program it runs in, with its version.

Each must be 200 bytes or fewer, with no control characters, or the
answer is 422. They are read only for a token made for an agent. Set
`api.Config.RequireAgentSession` to refuse a write from an agent that
does not send `Agent-Session`.

Every request that reaches a handler logs one line, `api: request`,
with the method, the path, the actor, and the two agent headers when
they were sent. The actor reads like
`agent:Claude for user:42 token:k3Jx9QpL2mWa`. See
[Agents](/docs/agents) for what each part means.

## Requests and answers

**Answers.** A single row is a bare JSON object. A list puts the rows
under the resource's name, with the cursor for the next page:

```json
{"bookmarks": [{"id": 7, "title": "Docs", "link": "", "notes": "", "created_at": "2026-10-05T18:00:00Z", "updated_at": "2026-10-05T18:00:00Z"}], "next_after": null}
```

Field names are the manifest's, in snake_case: `MaxPerOrder` is
`max_per_order`. IDs are numbers. Times are RFC 3339 strings in UTC.

**Money** is an object. `amount` is an integer in the currency's
smallest unit, and `currency` is the ISO 4217 code:

```json
{"price": {"amount": 1490, "currency": "USD"}}
```

An optional money field with no value is `null`. Send `null` to clear
one. When you send money, you may leave out `currency`: a create uses
the manifest's currency, and an update keeps the stored one. An update
that sends a different currency is refused with 409.

**Writes.** Send the body as JSON with
`Content-Type: application/json`, or the answer is 415. The body may
be up to 1 MiB, or the answer is 413. A member the resource does not
have, a member sent twice, or `null` where it is not allowed is 422. A
DELETE takes no body.

**PATCH** changes only the fields you send. `{}` changes nothing and
answers the row as it is.

**Lists** take these query parameters:

- `limit`: rows per page, 1 to 200. The default is 50.
- `after`: the `next_after` value from the previous page. `next_after`
  is `null` on the last page.
- `q`: a search, when the manifest turns search on.
- One parameter per manifest filter, named like the field: `?status=draft`.

Rows come newest first.

**Errors** all have one shape:

```json
{"error": {"code": "invalid", "message": "Check the fields and try again.", "fields": {"title": "Title is required"}}}
```

Branch on `code`, never on `message`. Each code has one status:

| Code | Status | When |
|---|---|---|
| `unauthorized` | 401 | no token, or one that does not work |
| `forbidden` | 403 | the token may not do this |
| `not_found` | 404 | no such row, or not the caller's; no such path |
| `method_not_allowed` | 405 | the path exists but not with this method |
| `conflict` | 409 | the change clashes with what is stored |
| `too_large` | 413 | the body is over the limit |
| `unsupported_media_type` | 415 | a body without `Content-Type: application/json` |
| `invalid` | 422 | `fields` names what to fix |
| `rate_limited` | 429 | a rate limiter in `Config.Before` said no |
| `internal` | 500 | something broke; quote `ref` when you report it |
| `unavailable` | 503 | the token store did not answer; try again |

Another person's row is 404, the same as a row that does not exist.

## The contract file

`rastrillo generate` writes `api/v1.json`: every route the API serves,
with its arguments and result fields. Commit it with `gen/`. Every app
with a `manifest/` directory has one, even when nothing is on the API.

`/api/v1` may only grow. When you change a manifest, `generate`
compares the new contract with the committed one:

- A new route, a new optional argument or a new result field is
  written to the file.
- A removed route or field, a newly required argument, a changed kind,
  or a read that now needs write access stops `generate`. It names each
  break and leaves the file as it was.

Undo the manifest change, or run `rastrillo generate --accept-breaking`
if you mean to break the API. The diff to `api/v1.json` then shows the
break in review. `rastrillo generate --check` fails when the file is
missing or out of date.

## Putting a resource on the API

A resource with `scope = "user"` is on the API. A resource with no
owner is not, because its pages may be guarded by middleware the API
cannot see. To change either default, add this to the manifest:

```toml
[api]
expose = true   # or false
```

Then run `rastrillo generate` and commit `gen/` and `api/v1.json`.

A resource with no owner stays closed to tokens after `expose = true`,
until your `Authorize` allows them. See [Who may do what](#who-may-do-what).

A filter may not be named so that its query parameter is `q`, `after`
or `limit`. Lists already use those. `generate` refuses the manifest:
rename the field.

## Writing an operation by hand

To change what an operation does, replace its file:

1. Copy `gen/ops/<name>/create.go` to `ops/<name>/create.go`.
2. Put `//go:build rastrillo_actions` and a blank line above its package
   clause.
3. Edit it and run `rastrillo generate`.

You can replace `list.go`, `get.go`, `create.go`, `update.go` and
`delete.go`. `types.go` stays generated, because it is the API's
contract. Your file shares the generated package, so it can use its
types and call the other operations. Delete your file to get the
generated one back.

Every exported function in your file must start with exactly this
check:

```go
if err := op.Check(ctx, rc, CreateOp); err != nil {
	return Bookmark{}, err
}
```

`rastrillo generate --check` fails if a function starts any other way,
if a package-level `var` calls a function to get its value, or if the
file is missing its `//go:build` line.

## Changing a page action by hand

A hand-written action that replaces a page of a resource on the API
must call that page's operation:

| Action | Must call |
|---|---|
| `index.GET` | `List` |
| `[id]/index.GET`, `[id]/edit.GET`, `[id]/delete.GET` | `Get` |
| `index.POST` | `Create` |
| `[id]/edit-basics.POST`, `[id]/edit-advanced.POST` | `Update` |
| `[id]/delete.POST` | `Delete` |

Call it at the top level of `Handle`, or of a function in the same file
that `Handle` calls, and use the error it returns. You may return early
before the call when the form does not parse.

If the action has no API twin, say why above its package clause:

```go
//rastrillo:page-only the importer takes a file upload, and the API has no file transfer yet
```

`rastrillo generate --check` checks this. It cannot see a second write
your action makes after the operation returns, or a handler you mount
yourself outside `actions/`. Put the rules in `Authorize`, where every
path that calls an operation meets them.

## The parity test

For each resource on the API, `rastrillo generate` writes a test to
`gen/paritytest`. It runs one scenario through the pages and through the
API, each on a fresh app, and fails if they end differently:

- a valid create, and the row it stored;
- a create with no values, when the resource has a required field;
- a change to one text field, and the row after it, when the resource
  has a field that is not money;
- for a resource with an owner, another person's row, which must be 404
  through both;
- a delete;
- a row that does not exist.

Each step compares how the two doors ended: the stored row, or the kind
of refusal. A page's answer shows only the kind, not which fields it
refused. So the API's answer to the create with no values is also
checked on its own: its 422 must name exactly the required fields.

To run it, implement `paritytest.Fixture` in your test package and call
the resource's `Run` function:

```go
func TestBookmarksParity(t *testing.T) { paritytest.RunBookmarks(t, fixture{}) }
```

The fixture has five methods:

- `New` builds a fresh app, mounted as production mounts it.
- `SignIn` returns a client signed in as a person.
- `Token` makes a read and write token for that person.
- `Prepare` adds rows a hand-written operation needs. It may do nothing.
- `ValidCreate` returns a valid create for a resource, or nil to use the
  generated values.

`examples/notes/internal/notestest/fixture_test.go` is a complete one.
