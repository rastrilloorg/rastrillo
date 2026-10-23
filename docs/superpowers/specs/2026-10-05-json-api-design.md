# A JSON API for every app: one operation, two doors, and agents that say who they act for

Status: design approved in conversation 2026-10-05, section by section,
with the operator's decisions marked **(Paul)**. Revised after two rounds
of adversarial review by Astra (see "Review log"). Nothing is
implemented.

Citations: `T/` is Tito Go (`amadan.net/tito/titogo`, `amadan/main`
`15f856aa3`). `A/` is amadan (`amadan.net/amadan/amadan` at `c7a4007fe`).
`P/` is the CARLOS platform (`amadan.net/carlos/platform` at
`b5b0a53a0`). `K/` is keymail (`amadan.net/keymail/keymail` at
`0339fc378`). `O/slides` and `O/meet` are
`amadan.net/oficina/slides` `origin/main` `ed5cc515` and
`amadan.net/oficina/meet` `origin/main` `e99c8da3`. Everything else is
this repo at `cec9b54f`.

## Why

This is the first of four specs. The goal behind all four **(Paul)**:
any Rastrillo app can describe itself to an agent in a generated skill,
let a person connect an agent to it, and let that agent act through it.
The four, in order:

1. **A JSON API** (this spec).
2. **An MCP endpoint** that exposes the API's operations as tools.
3. **Agent grants**: an OAuth 2.1 server in the app (discovery, client
   registration, PKCE, browser consent, rotating refresh tokens), so
   claude.ai and ChatGPT connectors work as well as Claude Code and
   Codex.
4. **A generated skill** describing the app and how to connect.

The JSON API comes first because the other three stand on it. Today
Rastrillo answers HTML only:

- `view.Render` renders a template, and every generated write answers
  with a 303 (`examples/tickets/gen/actions/admin/announcements/index_post/index.POST.go`).
- Only the error helpers speak JSON, and only when `Accept` asks
  (`view/view.go:82-104`).
- `docs/site/compare.md:43-46` sends anyone building a JSON API to chi
  instead.
- The manifest spec deferred "JSON API rendering of generated actions"
  and named it the prerequisite for native clients
  (`docs/superpowers/specs/2026-08-03-manifest-system-design.md:217-224`).

Every app that needed JSON wrote it by hand. A survey of nine
Rastrillo-based apps (dineraya, oficina home, docs, sheets-core,
memoria, meet, slides, seapointish, correomona) found:

- six bearer-token parsers that differ on case and whitespace;
- four token tables of the same design (prefix, SHA-256, shown once,
  revocable): dineraya, slides, meet, correomona;
- five error shapes, and several apps answering plain-text errors beside
  JSON successes;
- JSON body decoders written per app with different strictness: meet's
  refuses unknown fields (`O/meet` `internal/calls/api.go:436-449`),
  slides' accepts a missing content type and unknown fields
  (`O/slides` `internal/slides/api.go:179-211`);
- the same paragraph in four apps explaining why bearer routes have to
  be mounted outside the router: `csrf.Protect` refuses a write with no
  browser headers, answering a plain-text 403 (`csrf/csrf.go:59-60`).

The agent tool registry (`tool.go`, `tools/tools.go`) is not a
foundation either. No app on any branch uses it, and it cannot do
writes: `tools.Dispatch` synthesises a form POST with no `Origin`
(`tools/tools.go:147-173`), so behind `csrf.Protect` every write tool
gets a 403. It also hands a model HTML pages and 303s.

## Decisions

1. **The API is a separate, bearer-only tree at `/api/v1`** **(Paul,
   with Fable and Astra concurring)**. It is not the page routes
   negotiated by `Accept`.
2. **Every operation is one Go function with two thin doors**, a page
   door and an API door. Authorization lives in the operation, so both
   doors enforce the same rules. The generator emits both doors.
3. **Personal API tokens ship in this spec** **(Paul)**. They are minted
   for a named agent by default.
4. **Every call says who it is for** **(Paul)**: "Claude for Paul".
   The framework supplies those words. It does not supply a badge or an
   emoji **(Paul)**.
5. **Tokens have two halves** **(Paul)**: a public ID that can be shown
   and logged, and a secret stored as a SHA-256 hash. Not a slow
   password hash.
6. **Tokens last 90 days by default**, with a choice of 30, 90 or 365
   days, or never **(Paul)**.
7. **New tokens can read and write by default**; "read only" is a choice
   on the token page **(Paul)**.
8. **The agent headers are generic**: `Agent-Session` and
   `Agent-Harness`, so one agent config works across apps **(Paul)**.

### Why a separate tree

Every codebase surveyed that serves bearer callers chose one, and none
negotiates on `Accept` for them:

| Codebase | Bearer JSON lives at | Notes |
|---|---|---|
| Tito Go | `/api/v1` on its own inner mux (`T/internal/instance/shop.go:1555`, `T/internal/instance/api.go:253-288`) | One route table drives the mux, the capability gate and the OpenAPI parity tests (`T/internal/instance/api.go:3-4`) |
| CARLOS console | `/api/cli/...` (`P/internal/console/authcli.go:456-471`) | No CSRF on it; requiring `Content-Type: application/json` is the cross-site defence (`P/internal/console/authcli.go:919-925`) |
| amadan | `/api/v1` (`A/internal/hub/server.go:767-875`) | Accepting the session cookie on `/api/v1` as well as bearer left it exposed to cross-site writes from same-site subdomains, fixed with an origin rule (`A/internal/hub/tokens.go:27-61`) |
| keymail | `/api/*` | Bearer only |
| meet, slides, dineraya, correomona | outside the CSRF router | Each says why in a comment |

The reasons, in order of weight:

- **Credential and middleware policy differ by caller, not by format.**
  Browsers get sessions, CSRF and redirects to a sign-in page. Bearer
  callers get none of those, and will get their own rate limits.
  `Accept` selects a representation; it should not select a security
  boundary.
- **Rastrillo's CSRF forces it.** `csrf.Protect` refuses a write with no
  `Sec-Fetch-Site`, `Origin` or `Referer` (`csrf/csrf.go:59-60`). A
  bearer client inside that router would have to forge browser headers.
  Main's later `csrf.Exempt` (`8fbde25b`) does not change this: it
  matches exact paths, not the `/api/v1/` subtree, and an exempt path
  still runs the router's session middleware, which the API must stay
  outside.
- **The edge cache is safe by default.** The CARLOS edge never stores a
  response to a request that carried `Authorization` unless the
  response says `public` (`P/internal/edge/edge.go:1606`). A negotiated
  route would need `Vary: Accept` on every cacheable page instead.

The cost of a separate tree is drift between the two doors. Tito Go
records it: 879 of 959 admin commits changed no API file until a CI
gate made them (`T/.github/workflows/api.yml:5-6`), and pre-checks
redone in each door let the API refuse refunds the admin allowed, twice
(`T/internal/instance/api_write.go:1106-1122`). Decision 2 is the
answer: the rules live once, in the operation, and the doors only
translate.

JSON for the app's own scripts and native shells, which ride the
session cookie, is a separate view feature. It is out of scope here.

## Design

### 1. The shape

```
ServeMux (the scaffold's Mux)
├── GET /static/...      assets
├── /api/v1, /api/v1/... api.Tree: bearer only, JSON only, no CSRF, no sessions
│     └── API doors ──────────┐
└── /                    chi router: csrf.Protect, sessions, pages
      └── page doors ─────────┤
                              ▼
                     operations (gen/ops/<resource>)
                     op.Check → validate → store
```

The scaffold already mounts the chi router (with `csrf.Protect`) inside
a plain `ServeMux` beside `/static/` (`cmd/rastrillo/new.go:486-497`).
The API tree mounts on that same `ServeMux`, so it sits outside CSRF and
sessions with no workaround. `/api/version` and `/api/next-due` are
answered by the framework before the app's mux (`serve.go:533`) and do
not collide with `/api/v1`.

### 2. Operations

#### The `op` package

A new package holds what operations and both doors share:

```go
package op

// Refusals. Page doors and API doors each map these one way.
type Invalid struct{ Fields map[string]string } // manifest field name -> message
type NotFound struct{}
type Forbidden struct{ Reason string }          // Reason is for logs, never the response
type Conflict struct{ Message string }

// Opt is a field a request may or may not carry.
type Opt[T any] struct {
    Value T
    Set   bool
}

// Descriptor names one operation, independent of transport.
type Descriptor struct {
    rastrillo.Operation         // ID, Resource, Scoped, Access
    Exposed     bool            // the resource is on the API (section 2, "Which resources are exposed")
    Description string
    Confirm     string          // consent sentence, for MCP later
    Input       json.RawMessage // JSON Schema of the arguments; required
    Result      json.RawMessage // JSON Schema of the result; required
    Invoke      func(ctx context.Context, rc *rastrillo.Ctx, args json.RawMessage) (any, error)
}

// Check authorizes the actor on rc for o. Every operation calls it
// first. See section 4.
func Check(ctx context.Context, rc *rastrillo.Ctx, o rastrillo.Operation) error

// DefaultPolicy is the policy section 4 describes, for an app to name
// deliberately as its Authorize.
func DefaultPolicy(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error
```

`Access` and `Operation` live in the root package, not in `op`:

```go
package rastrillo

type Access int
const (
    Read  Access = 1
    Write Access = 2
)

type Operation struct {
    ID       string // stable: "announcements.create"
    Resource string // "announcements"
    Scoped   bool   // the resource is scope = "user"
    Access   Access // Read or Write
}
```

`Actor` needs `Access` and `Ctx.Authorize` needs `Operation`, and `op`
imports the root package for `Ctx`, so defining them in `op` would be
an import cycle.

- `Access` has no valid zero value. The old `rastrillo.Access` made zero
  mean read (`tool.go:16-25`), so a route that forgot to say became
  readable by a read-only token.
- `Descriptor` is what the MCP spec will list as tools and what the
  skill spec will describe. `Invoke` is the transport-independent call:
  it decodes the arguments with the same rules as the API door
  (section 6) and calls the operation. This spec generates it and tests
  it, so spec 2 has nothing to retrofit.
- **Every operation has a complete argument shape**, the same one
  `Invoke` decodes and `Input` describes:

  | Operation | Arguments |
  |---|---|
  | `List` | `{"q", "after", "limit", <one per filter>}`, all optional |
  | `Get` | `{"id"}` |
  | `Create` | the `CreateInput` fields |
  | `Update` | `{"id"}` plus the `UpdateInput` fields |
  | `Delete` | `{"id"}` |

  The generator emits `ListArgs`, `GetArgs`, `UpdateArgs` and
  `DeleteArgs` types for these. `api.New` refuses a route whose
  descriptor has an empty `Input` or `Result`, so a hand-written
  operation must describe itself too.

#### What the generator emits

For each manifest resource, a package `gen/ops/<resource>`, one file
per operation plus one for types:

```
gen/ops/announcements/
  types.go    Announcement, ListInput, CreateInput, UpdateInput, Page, descriptors
  list.go     func List(ctx context.Context, rc *rastrillo.Ctx, in ListInput) (Page, error)
  get.go      func Get(ctx context.Context, rc *rastrillo.Ctx, id int64) (Announcement, error)
  create.go   func Create(ctx context.Context, rc *rastrillo.Ctx, in CreateInput) (Announcement, error)
  update.go   func Update(ctx context.Context, rc *rastrillo.Ctx, id int64, in UpdateInput) (Announcement, error)
  delete.go   func Delete(ctx context.Context, rc *rastrillo.Ctx, id int64) error
```

Operations take `context.Context` first, so request cancellation reaches
the store as it does today (`internal/generate/actions.go:971` passes
`r.Context()`), and later MCP cancellation will too.

**Types.**

- `Announcement` is a **public result type**: `ID`, the manifest's
  fields, and `CreatedAt`/`UpdatedAt` where the store has them. It is
  never a template view model. Tito Go's hand-built response maps
  silently dropped fields for four days
  (`T/internal/instance/api_detail_shape_test.go:3-14`).
- `CreateInput` and `UpdateInput` hold every form field, basics and
  advanced, as `op.Opt[T]`. Presence is explicit in both, so a required
  money field can tell "not sent" from "sent as zero", which `form.Parse`
  distinguishes. Money fields are `op.Opt[op.Money]` when required and
  `op.Nullable[op.Money]` when optional, where
  `op.Money{Amount int64; Currency string}` and `op.Nullable[T]` is
  `Opt[T]` plus a `Null` flag that a JSON `null` sets. `Opt` itself
  still refuses `null`.
- `ListInput` carries `Search` (when `list.search` is on), one field per
  manifest filter, `Limit`, and **either** `After` (an opaque cursor,
  for the API) **or** `PageNumber` (for pages). `Page` carries `Items`,
  `NextAfter`, and `Total` (filled only in page-number mode, because
  only the page's numbered pagination needs `Count`,
  `internal/generate/actions.go:631-721`).

**Validation and normalisation live in the operation.** Trimming text,
required checks, and length and kind checks move out of the page
action into `Create` and `Update`, so both doors accept and refuse
exactly the same values. What stays in each door is representation:
the page door turns `"14.90"` into 1490 cents (`form.ParseCents`), the
API door takes the integer.

The checks are also generated as pure functions,
`ValidateCreate(in CreateInput) map[string]string` and
`ValidateUpdate(in UpdateInput) map[string]string`, which `Create` and
`Update` call and which never write. When the page door has a parse
failure (money that isn't a number), it does **not** call the
operation: it merges its parse errors with `ValidateCreate`'s and
re-renders the form, so the person sees every problem at once and
nothing is written. Only a submission with no parse errors reaches
`Create`.

**Partial updates are one atomic read, merge and write.** The generated
stores update a whole field group at a time
(`internal/generate/store.go`, `Update<Singular>Basics` and
`...Advanced`). The operation reads the row, applies only the supplied
fields, and writes the whole row back with one generated full-row
`UPDATE`, all inside one transaction. That is atomic because the
writer pool has exactly one connection (`db/db.go:80`): no other write
can run between the read and the write.

Two alternatives were rejected. `COALESCE(sqlc.narg(x), x)` cannot
clear a field, and an optional money field's `<f>_minor` and
`<f>_currency` columns are nullable, where clearing is a legitimate
change. A presence flag inside `CASE` is typed by sqlc v1.31.1 as the
target column rather than a boolean
(`sqlc/internal/compiler/find_params.go:139`).

`recordKey` is the existing `id`, or `id AND owner` for a scoped
resource. A row that isn't there is `op.NotFound`. A mergeable store
appends one event carrying only the supplied fields, and its fold
applies them field by field. A mergeable update that races a currency
change is refused as `op.Conflict`, the rule the mergeable stores
already apply to page saves. The page door's `edit-basics` and
`edit-advanced` each set their own subset.

**An update with no fields set writes nothing.** After `op.Check`,
`Update` with an empty `UpdateInput` returns the current row through
`Get`: no `UPDATE`, no event, and `updated_at` unchanged.

**Mergeable creates become atomic first.** The mergeable store
allocates an id by reading every stream and taking `max(id)+1`, then
appends in a separate transaction
(`internal/generate/mergeablestore.go:486`, `eventlog/eventlog.go:105`).
Two concurrent creates can take the same id, and the fold then lets the
later `created` event replace the owner of the earlier
(`internal/generate/mergeablestore.go:237`). That is a pre-existing bug,
but an API makes concurrent creates normal, so this spec fixes it:
creates go through `eventlog.AppendUnder`, which reads the streams
under the prefix, names the new one and appends its first event under
one write lock (main's fix, adopted over the branch's own `AppendNew`
in discussion #74). A test runs
concurrent creates by two subjects and requires two distinct records,
each with its own owner.

**Mergeable events say who wrote them.** The mergeable store stamps
every event's actor as `"app"`
(`internal/generate/mergeablestore.go:144-146`). Its generated methods
gain an actor argument, and operations pass `rc.Actor.String()`.

**Scoped resources** filter by `rc.Actor.Subject` where the page actions
read `sessions.Current` today (`internal/generate/actions.go:444-470`).
A scoped operation with an empty subject is `op.Forbidden`, never a
query for owner `''`.

#### The doors

The page doors in `gen/actions/` keep their files and routes, and get
thinner: read the form, call the operation, then render or redirect.

The API doors are generated as `gen/api/<resource>.go`, named from the
manifest's `name`, not its page route:

| Method | Path | Operation | Success |
|---|---|---|---|
| GET | `/api/v1/announcements` | `List` | 200 |
| POST | `/api/v1/announcements` | `Create` | 201, `Location`, body |
| GET | `/api/v1/announcements/{id}` | `Get` | 200 |
| PATCH | `/api/v1/announcements/{id}` | `Update` | 200, body |
| DELETE | `/api/v1/announcements/{id}` | `Delete` | 204 |

`gen.APIRoutes() []api.Route` lists the exposed resources' doors.
`gen.Operations() []op.Descriptor` lists every resource's operations,
each with `Exposed` set from the manifest, so spec 2 can offer only the
exposed ones as tools without guessing.

#### Which resources are exposed

- **`scope = "user"` resources are exposed by default.**
- **Unscoped resources are not.** They are admin-style tables
  (`manifest.go:48-50`), guarded today by whatever middleware the app
  mounts their pages behind, which the API cannot see.
- A manifest overrides either default:

  ```toml
  [api]
  expose = true   # or false
  ```

  In Go, `Resource.API.Expose` is a `*bool`: nil means "use the
  default for this scope". `gen/manifest.json` carries the key only
  when it is set (`omitempty`), so existing artifacts stay
  byte-identical, matching how `Scope` was added (`manifest.go:79`).
  The TOML decoder rejects unknown keys
  (`internal/manifest/manifest.go:18`), so the `[api]` table is added
  to it explicitly.

Exposure decides only whether API doors are generated and what
`Descriptor.Exposed` says. What a caller may do is decided by section
4, for every operation.

### 3. The `api` package

```go
package api

type Config struct {
    Verify    Verifier                           // required; apitoken.Store implements it
    Ctx       func(*http.Request) *rastrillo.Ctx // required; the app's ctx factory
    Authorize func(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error // required
    Redact    func(string) string                // token-shaped text out of log lines
    Logger   *slog.Logger
    MaxBody  int64                              // default 1 MiB
    Before   []func(http.Handler) http.Handler  // runs before the token check: rate limits
    After    []func(http.Handler) http.Handler  // runs after it, with the Actor known
    RequireAgentSession bool                    // refuse agent writes with no Agent-Session
}

// Verifier turns a request's credential into an Actor. OAuth grants
// (spec 3) will be a second implementation.
type Verifier interface {
    Verify(ctx context.Context, r *http.Request) (rastrillo.Actor, error)
}

type Route struct {
    Method, Pattern string
    Op              op.Descriptor // required; Access and policy come from it
    Handle          func(ctx context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
}

func New(cfg Config, routes ...Route) (http.Handler, error)

func Decode(w http.ResponseWriter, r *http.Request, dst any) bool
func Write(w http.ResponseWriter, status int, v any)
func Refuse(w http.ResponseWriter, r *http.Request, err error)
```

**`New` validates every route at boot** and returns an error if:

- `Verify`, `Ctx` or `Authorize` is nil;
- a route has no `Op.ID`, `Op.Access` is zero, or `Op.Input` or
  `Op.Result` is empty;
- the method and access disagree: GET and HEAD must be `Read`; POST,
  PUT, PATCH and DELETE must be `Write`;
- two routes share a method and pattern.

**Every request through the tree, in this order.** Each step's refusal
wins over every later one; this is the error precedence.

1. Gets `Cache-Control: no-store`.
2. **Routing.** The tree registers each route on an inner `ServeMux`.
   It asks `mux.Handler(r)` only to classify: when no pattern matches,
   it answers JSON 404 `not_found`, or JSON 405 `method_not_allowed`
   with an `Allow` header when the path matches under another method.
   It never lets the inner mux write its own plain-text 404 or 405. A
   matched request is dispatched through `mux.ServeHTTP`, because only
   that fills `r.PathValue("id")`; `mux.Handler` does not. HEAD is
   answered for every GET route.
3. Runs `Config.Before`.
4. **Credential.** `Config.Verify` (section 5): 401 or 503.
5. **Agent headers** (section 4): 422.
6. **Read-only.** 403 `forbidden` when the actor is read-only and the
   route is `Write`.
7. Runs `Config.After`, then the route's handler, with `rc.Actor` set
   and `rc.Authorize` set to `Config.Authorize`. The handler's
   `api.Decode` then refuses, in order: a DELETE with any body (422);
   a missing or wrong content type (415); a body over the cap (413,
   decided by reading the whole bounded body before parsing, so size
   always wins over syntax); then malformed or refused JSON (422).
8. The operation calls `op.Check` (section 4), then validates.

`Config.Authorize` is required. An app turning on the API must name its
policy, even if the policy it names is the default: `op.DefaultPolicy`
is a valid, deliberate choice. The app passes the same function to its
page ctx factory, so both doors share one policy.

**Mounting.** The scaffold mounts the tree at both `/api/v1` and
`/api/v1/`, so the outer `ServeMux` never answers a bare `/api/v1` with
its own redirect. The outer mux still redirects paths it cleans
(`..`, `//`) with a 301 before the tree runs (`net/http` `ServeMux`);
those carry no data and no credential is checked, and the spec accepts
them rather than replacing the outer mux.

```go
apiTree, err := api.New(api.Config{Verify: tokens, Ctx: ctxFactory, Logger: logger}, gen.APIRoutes()...)
mux.Handle("/api/v1", apiTree)
mux.Handle("/api/v1/", apiTree)
```

**Panics.** The tree recovers a panic in a handler. If nothing has been
written, it answers JSON 500 `internal` with a `ref`. If the response
has started, it aborts the connection (`http.ErrAbortHandler`) rather
than append JSON to a half-written body.

**Logging.** `Refuse` and the panic handler log errors and panic values
with a `ref`, as `view.Fail` does. Before logging, both pass the text
through a redactor that replaces anything matching the token grammar
(section 5) with its public ID, so a token quoted inside an error
message never reaches a log line. The `Authorization` header is never
logged.

`Options.Router` returns `*http.ServeMux`, which forced amadan to an
outer-mux workaround (`A/internal/hub/server.go:878-909`). The scaffold
already wraps its own `ServeMux`, so this spec does not need to change
that signature.

### 4. Who is calling, and what they may do

#### `Actor`

`rastrillo.Actor` is `{Human bool; Name string}` today (`ctx.go:11-14`).
It changes to:

```go
type Actor struct {
    Subject    string    // the person: sessions.Session.Subject, or the token owner's
    Agent      string    // "" when the person acts directly; "Claude" when an agent acts for them
    Credential string    // the token's public ID; "" for a browser session
    Access     Access    // what the credential allows; Write for a session
    Session    string    // the agent's session id, when it sent one
    Harness    string    // e.g. "claude-code/2.1.288", when it sent one
}
```

- `Subject` is the same TEXT subject sessions and scoped stores already
  use (`sessions/sessions.go:37-42`, `manifest.go:52-61`), so keymail
  email subjects and numeric password subjects both fit.
- The zero `Actor` is the app itself: a sidecar, a job, a migration.
- **Page requests** get `Subject` from `sessions.Current`, an empty
  `Agent` and `Credential`, and `Access` `Write`.
- **API requests** get `Subject`, `Agent`, `Credential` and `Access`
  from the verified token. None of them ever comes from the request. An
  agent cannot claim to be someone else (amadan's rule,
  `A/internal/hub/docs/agents.md`, "Who was driving").
- **`Agent-Session` and `Agent-Harness`** are copied into `Session` and
  `Harness`, each capped at 200 bytes with control characters refused,
  and only for an agent token. They are the client's own claims and are
  recorded as such. A person's own token cannot use them to pose as an
  agent.
- With `Config.RequireAgentSession` on, a write from an agent token
  without `Agent-Session` is refused 422 `invalid`, as amadan requires
  its session block (`A/internal/hub/discuss_api.go:150-171`). Off by
  default.

**Audit.** `String()` is the compact form `eventlog` stores
(`eventlog/eventlog.go:63`):

| Actor | `String()` |
|---|---|
| the app | `app` |
| a person in a browser | `user:<subject>` |
| a person's own token | `user:<subject> token:<public id>` |
| an agent | `agent:<agent> for user:<subject> token:<public id>` |

The token's public ID is in it, so two tokens both named "Claude" stay
distinguishable. Session and harness are logged per request as
structured fields (`Actor.LogAttrs()` for `slog`) rather than stored in
every event. Nothing in this repo parses the string. Old rows keep
`human` and `agent:<name>`; a reader that compared against `"human"`
must change, and the CHANGELOG says so.

`rastrillo.WithActor` and `ActorFromContext` stay, so the generated
router keeps copying the actor onto `Ctx.Actor`
(`internal/generate/generate.go:307-318`).

#### Authorization: `op.Check`

Ownership is not authorization. A page can sit behind a role check, an
account-standing check or a fresh sign-in that the API never sees. So
every operation's first line is `op.Check`, and both doors pass
through it.

There are four kinds of caller, told apart by the actor's fields:

| Caller | `Subject` | `Credential` |
|---|---|---|
| a token (API) | set | set |
| a signed-in person (page) | set | empty |
| an anonymous page visitor | empty | empty |
| the app itself (job, sidecar) | empty | empty |

The last two look the same and are treated the same.

`op.Check` asks, in order:

1. **Can this credential write?** A `Write` operation with an actor
   whose `Access` is `Read` is refused `Forbidden`. This runs whatever
   the policy says, so a direct call or a `Descriptor.Invoke` with a
   read-only actor is refused as the HTTP tree refuses it.
2. **Is there a person?** A scoped operation with no `Subject` is
   refused `Forbidden`.
3. **The app's policy.** `Ctx.Authorize`, when set, is called with the
   actor and the operation:

   ```go
   Authorize func(ctx context.Context, a Actor, o Operation) error
   ```

   Its error is the answer: `nil` allows, an `op.Forbidden` or
   `op.NotFound` refuses. `op.Check` returns the error unchanged, and
   the door that called it answers it as it answers the same error from
   the operation: the API by its errors table (§6, so an `op.Conflict`
   is 409), a page with its 403 or 404 page. An error neither
   recognises is a 500. This is where an app
   says "only admins may create ticket types", "suspended accounts can't
   write", or "agents can't delete accounts", once, for both doors.
4. **With no policy set, `op.DefaultPolicy`:**
   - a **scoped** operation is allowed. Step 2 already required a
     person, and the owner filter limits them to their own rows;
   - an **unscoped** operation is allowed for every caller without a
     credential: signed-in, anonymous and the app. That is today's
     behaviour, where admin pages are guarded by middleware or, in
     `examples/tickets` and `examples/blog`, not at all;
   - an **unscoped** operation is **refused for a token**.

`op.DefaultPolicy` lets a token do whatever its owner can do on a
scoped resource. If an app's pages add rules beyond ownership (roles,
suspension, a fresh sign-in), the app must write those rules into its
`Authorize`, because the API cannot see page middleware. That is why
`api.Config.Authorize` is required: the choice is made in writing, at
the moment the API is turned on.

A hand-written operation calls `op.Check` itself; section 7's lint
checks it does.

#### "Claude for Paul"

A `ui` template function and partial, `actor`, renders:

- "Paul" for a person;
- "Claude for Paul" for an agent;
- nothing for the app itself.

It is plain text. The app styles it, and there is no badge or emoji
**(Paul)**. The person's name comes from a lookup the app sets once,
`ui.ActorName func(ctx context.Context, subject string) string`,
because the framework does not know how an app names its users. With no
lookup set it shows the subject.

The phrase "{agent} for {person}" is new user-facing copy and a
translation key in `internal/designsystem`. It goes through copy review,
then gets its eleven translations, before it ships.

### 5. Personal tokens: the `apitoken` package

#### The token

```
tickets_k3Jx9QpL2mWa_<43 characters>
└─ prefix └─ public ID └─ secret
```

- **Prefix:** `[a-z][a-z0-9]{0,15}`, ASCII only, with no underscores,
  so the three parts split unambiguously. The scaffold derives it from
  the app name: lowercase, keep only ASCII `a-z` and `0-9`, prepend
  `app` if the result is empty or starts with a digit, then cut to 16.
  `tickets` stays `tickets`; `123-app` becomes `app123app`. The app can
  set it. It lets secret scanners recognise a leaked token.
- **Public ID:** 12 base62 characters (about 71 bits), random. Stored
  as-is with a unique index. It is shown on the token page and may
  appear in logs and audit records, so "token `k3Jx9QpL2mWa` deleted 4
  orders" is traceable and a person can match it against their own
  agent config **(Paul)**.
- **Secret:** 43 base62 characters (about 256 bits), random. Shown once.
  Stored as SHA-256.

Base62 has no `_`, so the three parts split unambiguously.

A slow password hash such as bcrypt is deliberately not used **(Paul)**.
Slow hashes exist to make guessing a low-entropy password expensive. A
256-bit random secret cannot be guessed, so a stolen SHA-256 of it is
as useless as a stolen bcrypt. And bcrypt costs 50 to 100ms by design,
on every API call, against a 150ms first-byte budget (`perf`).

A gitleaks rule for the token shape ships in the repo's own config and
in the scaffold's, so an app's CI catches its tokens being committed.

#### The table

`apitoken.Schema` is a migration set the app merges, like
`sessions.Schema`:

```sql
CREATE TABLE api_tokens (
    id          TEXT PRIMARY KEY,          -- the public ID
    subject     TEXT NOT NULL,             -- the owner
    agent       TEXT NOT NULL DEFAULT '',  -- '' = the person's own token
    access      TEXT NOT NULL,             -- 'read' or 'write'
    secret_hash BLOB NOT NULL,             -- SHA-256 of the secret
    created_at  TEXT NOT NULL,
    last_used   TEXT,                      -- written at most once a minute
    expires_at  TEXT,                      -- NULL = never
    revoked_at  TEXT
);
CREATE INDEX api_tokens_subject ON api_tokens(subject);
```

**Every management method is bound to its owner.** `Mint`, `List`,
`Revoke`, `RevokeAgent` and `RevokeAll` all take the subject, and every
query that reads or changes a row includes `subject = ?` beside the id,
the way generated stores use `id AND owner`
(`internal/generate/store.go:401-413`). The token page's handlers take
the subject from `sessions.Current` only, never from the form. Revoking
another person's token ID is a 404.

#### Verifying a request

`apitoken.Store` implements `api.Verifier`:

1. Only `Authorization: Bearer <token>`, scheme case-insensitive. No
   cookies on the tree and no token in the query string, so tokens stay
   out of access logs and browser history.
2. The whole token must match the grammar, and its prefix must equal
   the configured one, before anything touches the database.
3. Look up the public ID, compare SHA-256 of the secret in constant
   time, then check revoked and expired.
4. Ask `Config.Allowed(ctx, subject)` whether the owner may still use
   the app. A nil `Allowed` means yes. An error is a 503.
5. Write `last_used`, at most once a minute per token, so a busy agent
   doesn't turn every read into a SQLite write.

An unknown, malformed, wrong-prefix, revoked or expired token, and an
owner `Allowed` refuses, all get the same JSON 401 `unauthorized` with
`WWW-Authenticate: Bearer`. A store error is a 503 `unavailable`, never
a 401, so an outage doesn't look like a revoked token to the agent
(`K/handlers.go:74-77`).

#### Agent names

The agent name is what the person types: "Claude", "Codex", "Nightly
import". It is 1 to 40 characters of letters, digits, spaces, hyphens
and dots, trimmed. It is shown as typed, so "Claude for Paul" reads
naturally. It is a label, not an identity: the token's public ID is
what identifies it.

A token for the person themselves, for a script with no AI in it, is
possible but is a deliberate choice: the person ticks "This token is
for me, not an agent". The default path names the agent **(Paul)**.

#### The token page

`apitoken` ships page doors and a `ui` partial. The app mounts them
under its session router, behind CSRF, like any other page. Every
response from them carries `Cache-Control: no-store`, as
credential-bearing pages already do (`password/handlers.go:192`).

- **List:** each token as its `actor` phrase ("Claude for Paul"), public
  ID, access, created, last used and expires, with a revoke button.
- **Create:** agent name, the "for me" choice, access ("Read and write"
  preselected, or "Read only") **(Paul)**, and expiry (30, 90 or 365
  days, or never; 90 preselected) **(Paul)**.
- **The secret is shown once, in the response to the create POST
  itself.** It is never put in a URL, a redirect, a flash message or the
  session. Reloading that page re-submits the form and mints a second
  token, which the browser warns about; the page tells the person to
  copy the token now.
- **Creating needs a recent sign-in**: the routes mount behind
  `sessions.RequireFresh(10 * time.Minute)` (`sessions/sessions.go:349`),
  which works whichever sign-in plugin the app uses. Revoking does not,
  because a kill switch must be easy.
- **Kill switches:** revoke one token, every token for one agent name,
  or all of them.

All copy on the page goes through copy review before it ships.

### 6. The wire

JSON is encoded and decoded with `github.com/go-json-experiment/json`,
already a dependency (`go.mod`).

#### Responses

- **No envelope.** A single resource is a bare object:

  ```json
  {"id": 7, "title": "Doors open at six", "body": "", "created_at": "2026-10-05T18:00:00Z", "updated_at": "2026-10-05T18:00:00Z"}
  ```

  A list names its items after the resource, and the array is `[]`,
  never `null`, when there are none:

  ```json
  {"announcements": [], "next_after": null}
  ```

- **Field names** are the manifest's field names in snake_case: `Title`
  becomes `title`, `IPAddress` becomes `ip_address`.
- **IDs** are JSON numbers. **Times** are RFC 3339 in UTC.
- **Money** is an object, matching how the stores keep it (`<f>_minor`
  and `<f>_currency`) and how templates write it
  (`{{money $L .Amount .Currency}}`):

  ```json
  {"price": {"amount": 1490, "currency": "USD"}}
  ```

  `amount` is an integer in the currency's minor unit; `currency` is
  the ISO 4217 code as `currency.Normalize` returns it. An optional
  money field with no value is `null`.

#### Errors

```json
{"error": {"code": "invalid", "message": "Check the fields and try again.", "fields": {"title": "Title is required"}}}
```

Field messages are the operation's, and the same text `form.Parse`
gives on the page, so both doors say the same thing.

The code is required, and each maps to exactly one status. amadan's
callers needed a machine code, not prose
(`A/internal/hub/branch_merge_api.go:119-123`).

| Code | Status | From |
|---|---|---|
| `unauthorized` | 401 | the credential check, including `Allowed` saying no |
| `forbidden` | 403 | `op.Forbidden`; a read-only token on a write route |
| `not_found` | 404 | `op.NotFound`; an unmatched path |
| `method_not_allowed` | 405 | a known path, wrong method |
| `conflict` | 409 | `op.Conflict` |
| `too_large` | 413 | the body cap |
| `unsupported_media_type` | 415 | a write body without `Content-Type: application/json` |
| `invalid` | 422 | `op.Invalid`; a malformed or refused JSON body |
| `rate_limited` | 429 | reserved for `Config.Before` middleware; carries `Retry-After` |
| `internal` | 500 | anything else, including an unexpected `Authorize` error; carries `ref` |
| `unavailable` | 503 | the token store or `Allowed` failing |

- `fields` keys are the snake_case wire names. `op.Invalid.Fields` is
  keyed by manifest field name, and the API door converts.
- Messages are English.
- **Precedence** is the processing order in section 3: routing (404,
  405), credential (401, 503), agent headers (422), read-only (403),
  then the body (DELETE-with-body 422, 415, 413, 422), then the
  operation (403, 404, 409, 422).

#### Requests

- **A write with a body must send `Content-Type: application/json`** or
  get 415. A browser form cannot send that, so it is also the
  cross-site defence (the CARLOS console's rule,
  `P/internal/console/authcli.go:919-925`). A DELETE needs no content
  type, and a DELETE with any body is refused 422 before the content
  type is looked at.
- **`api.Decode`** reads at most `Config.MaxBody` bytes (the tree puts
  the limit in the request context), answering 413 over it. It refuses
  with 422: invalid JSON, a top-level value that isn't an object,
  unknown members, duplicate member names, trailing data after the
  value, and `null` for any field except an optional money field.
  Member names are matched case-sensitively.
- **Money input** is `{"amount": <integer>, "currency": "<code>"}`.
  `currency` may be left out: a create then uses the manifest's
  default `currency`, and an update keeps the row's stored currency.
  A code `currency.Normalize` refuses is 422 on that field. `null`
  clears an optional money field and is 422 on a required one.
- **An update never changes a stored amount's currency.** Sending a
  `currency` different from the one the field already holds is 409
  `conflict`, on both store kinds. Changing currency is a separate,
  future operation. A field with no amount yet takes the sent currency,
  or the manifest default.
- On create, any code `currency.Normalize` accepts is allowed, including
  one no longer issued. Refusing withdrawn codes belongs with the
  change-currency operation.
- **`op.Opt[T]` implements the JSON decoder**: a member that is present
  sets `Set`, and an absent one leaves it unset. So `PATCH` changes only
  the fields sent, and `{}` writes nothing and answers the current row.
- **Lists** take `?after=<cursor>&limit=`, default 50, maximum 200,
  plus `?q=` where the manifest turns on search and one parameter per
  manifest filter. The order is the stores' existing one, `created_at
  DESC, id DESC`. The cursor is an opaque encoding of the last row's
  `(created_at, id)`, and the next page is the keyset condition on
  that pair, written out as `created_at < ? OR (created_at = ? AND id <
  ?)` because sqlc v1.31.1 types the row-value form's id as a string.
  It carries its own values, so it still works after that row is
  deleted.
- **A filter may not be named `q`, `after` or `limit`.** Those are the
  list's own parameters; `Resource.Validate` refuses a manifest whose
  filter field's wire name collides with one.

#### Versioning

`/api/v1` changes only by adding: new fields, new routes. Because the
API is generated from manifests, a manifest edit can break it silently:
a new required field, a renamed field, a changed kind, or a resource
taken off the API. So the app commits `api/v1.json`: every exposed
operation's method, path, access, argument fields (name, kind,
required) and result fields.

- **`rastrillo generate`** compares the contract it would write with
  the committed file **before** writing anything. An additive change
  (a new route, a new optional argument, a new result field) updates
  the file. A breaking change (a removed route or field, a newly
  required argument, a changed kind) **fails generate and leaves the
  file untouched**, naming what broke. Ordinary regeneration can
  therefore never erase the evidence.
- **`rastrillo generate --check`** writes nothing, like every other
  check. It fails if the file is missing, or differs from what
  generate would write.
- **`rastrillo generate -accept-breaking`** is the one way to rewrite
  the file over a breaking change. It is a deliberate act, and the diff
  to `api/v1.json` shows the break in review.

This spec deliberately offers no way to keep the old contract served
while the manifest moves on: an app that needs that keeps the old
resource, or writes the old routes by hand beside the new ones. Serving
a v2 beside v1 is a later spec.

### 7. Hand-written code and the parity lint

#### Replacing an operation

This uses the mechanism actions already use. Hand-written action files
are never compiled in place: each is parsed, its package clause
rewritten, and the result written under `gen/`, with the
`//go:build rastrillo_actions` constraint stripped
(`internal/generate/generate.go:5-25`). Operations work the same way,
file for file:

- A hand-written `ops/announcements/create.go`, with the build
  constraint, replaces `gen/ops/announcements/create.go`. Its package
  clause is rewritten to the generated package's name, so it shares
  `types.go` and can call the other operations in the package directly.
- Only the five operation files can be replaced. `types.go` stays
  generated: the types are the API's contract, and section 6's check
  compares them.
- When a hand-written file is removed, the next `generate` writes the
  generated one back; when a resource is removed, its `gen/ops`
  directory is removed with it, as `gen/actions` is today.

#### Operations with no manifest

A hand-written operation outside any resource lives in the app's own
package. It declares an `op.Descriptor` by hand, calls `op.Check`
first, and is mounted with an `api.Route` in the app's own list beside
`gen.APIRoutes()`. Its input and result schemas may be left empty in
this spec; spec 2 will require them for MCP.

#### The lint

`rastrillo generate --check` runs a static pass. It is a lint, not a
proof: it catches the common drift, and section 8's behavioural tests
catch the rest.

- Every exported function in a hand-written `ops/` file must begin with
  exactly this guard, as its first statement:

  ```go
  if err := op.Check(ctx, rc, <Op>); err != nil {
      return ..., err
  }
  ```

  A call whose error is ignored, or any statement before it, fails.
  Replacement files may not declare package-level variables with
  initialisers that call functions, so no work runs before the guard.
- A hand-written action in `actions/` that overrides a generated door of
  an exposed resource must call that door's operation:

  | Overridden action | Must call |
  |---|---|
  | `index.GET` | `List` |
  | `[id]/index.GET`, `[id]/edit.GET`, `[id]/delete.GET` | `Get` |
  | `index.POST` | `Create` |
  | `[id]/edit-basics.POST`, `[id]/edit-advanced.POST` | `Update` |
  | `[id]/delete.POST` | `Delete` |

  The call must be unconditional: a top-level statement of `Handle`, or
  of a same-file function `Handle` calls at its top level. A branch
  before it that returns on a parse failure is allowed, because a
  submission with parse errors must not reach the operation (section
  2). Its error result must be used.
- An action that genuinely has no API twin opts out with a reason:

  ```go
  //rastrillo:page-only the bulk importer takes a file upload; the API has no file transfer yet
  ```

  An opt-out with no reason fails the check.
- **What it does not catch**, said plainly in the docs: extra writes
  after the operation returns, and handlers registered outside
  `actions/`, such as the scaffold's own chi handlers
  (`cmd/rastrillo/new.go:474-482`). Those are why authorization lives in
  `op.Check` and not in the doors.

#### The gate

The framework's own `make ci` runs `generate --check` over the examples
(`Makefile:187`). Scaffolded apps do not yet: the scaffold's `ci`
target and its CI steps omit it (`cmd/rastrillo/new.go:1531`,
`cmd/rastrillo/new.go:226`). This spec adds it to both, and the
CHANGELOG tells existing apps to add the line.

### 8. Testing

- **`api`:** each decode refusal (content type, oversize, invalid JSON,
  non-object, unknown member, duplicate member, trailing data, `null`);
  JSON 404, 405 with `Allow`, and HEAD; `/api/v1` with and without the
  slash through the whole framework handler; `no-store` on every
  response; a panic before and after the response starts; `New`
  refusing a route with zero access, a method that disagrees with its
  access, or a duplicate; the error precedence order.
- **`op.Check`:** the default policy for scoped and unscoped operations,
  for a session and for a token; an empty subject refused; `Authorize`
  allowing, refusing and failing.
- **`apitoken`:** mint, verify, revoke one, revoke by agent, revoke all;
  unknown, malformed, wrong-prefix, revoked, expired and
  `Allowed`-refused all give the same 401; a store error gives 503;
  `last_used` throttled; agent headers ignored on a person's own token;
  every management method refusing another owner's token, and the page
  ignoring a forged subject in the form; the reveal page sending
  `no-store` and the secret appearing in no URL or redirect.
- **Redaction:** an error and a panic value containing a whole token
  produce a log line with only its public ID; no log line ever contains
  an `Authorization` value.
- **Mergeable stores:** concurrent creates by two subjects give two
  records with their own owners; a partial update appends one event;
  every event carries the actor string.
- **`Actor`:** `String()` for all four rows; the `actor` partial with
  and without a name lookup.
- **Generator:** golden files for `gen/ops`, `gen/api`, the slimmer page
  actions and `api/v1.json`; the lint failing each case in section 7 and
  passing a reasoned opt-out; the contract check failing each breaking
  change and passing additive ones; manifest round-trips for `expose`
  absent, true and false.
- **Behavioural parity.** Matching route counts prove nothing. The
  generator emits a test per exposed resource that runs one scenario
  through both doors, against the app's **real mounted handler**, and
  compares outcomes: the stored rows, and the refusal kind. The API
  door's 422 must also name exactly the resource's required create
  fields, which are what `ValidateCreate` refuses on both doors; a page
  door's refused field names are not visible from its status.
  It needs an app fixture, which the scaffold writes
  once and the app keeps:

  ```go
  // in the app's test package
  type Fixture interface {
      New(t *testing.T) (http.Handler, *sql.DB)                                     // a fresh app, as production mounts it
      SignIn(t *testing.T, h http.Handler, db *sql.DB, subject string) *http.Client // a browser session
      Token(t *testing.T, db *sql.DB, subject string) string                        // an API token
      Prepare(t *testing.T, db *sql.DB, subject string)                             // seed what a custom operation needs
      ValidCreate(resource string) map[string]string                                // nil: use the generated values
  }
  ```

  The generated valid input fills every required field with a value of
  its kind. An app whose hand-written `Create` needs related rows seeds
  them in `Prepare` and supplies the input in `ValidCreate`.

  Each door gets its own fresh database (`dbtest`), and the scenario
  compares normalised outcomes: the stored rows, and the refusal kind
  and field names. The scenario:
  - a valid create;
  - an invalid create, which must report the same fields, generated
    only when the resource has a required field;
  - a partial update;
  - a delete;
  - a row that isn't there;
  - for a scoped resource, another person's row, which must be 404
    through both.
- **Examples.** `examples/notes` (user-scoped `bookmarks`,
  `examples/notes/manifest/bookmarks.toml:11`, password sign-in) proves
  tokens, the token page, attribution, the fixture and the API end to
  end. `examples/tickets` proves an unscoped resource stays unexposed,
  and that exposing one without `Authorize` refuses tokens. Both are
  tested from their own module directories (AGENTS.md).

### 9. Docs

- **New `docs/site/api.md`:** mounting the tree, minting tokens, the
  wire conventions, exposing a resource, writing `Authorize`, writing a
  custom operation, the contract file.
- **`docs/site/agents.md` rewritten:** the Tool marker and dispatcher
  are gone; agents act for people, and how to say so.
- **`docs/site/compare.md`:** stop sending JSON API builders elsewhere.
- **SKILL.md:** a short section within its byte budget
  (`skillmd_test.go`). If it does not fit, raise the ceiling and say why
  in the test, rather than cut a fact (AGENTS.md).
- **Reference pages** for `api`, `apitoken` and `op`.

All new user-facing copy goes through copy review first: the token page,
"{agent} for {person}", and the API error messages.

### 10. What breaks

Each gets a CHANGELOG entry.

- `rastrillo.Tool`, `rastrillo.ToolDef`, `rastrillo.Access`,
  `rastrillo.ToolRead`/`ToolWrite`, the generated `gen.Tools()`
  (`internal/generate/tools.go`), and the `tools` package are removed.
  No app on any branch uses them, and their writes could not pass CSRF.
  Their metadata lives on `op.Descriptor`.
- `rastrillo.Actor` changes shape. `Human` and `Name` are gone.
- `Actor.String()` changes, and so does what new `eventlog` rows store.
- `rastrillo.Ctx` gains `Authorize`.
- Generated mergeable store methods take an actor, and mergeable creates
  go through `eventlog.AppendUnder`.
- Generated stores gain a partial-update query and a keyset list query.
- Generated page actions change shape (they call operations), so an app
  that copied a generated action into `actions/` to customise it should
  regenerate and re-apply its change. The lint will name the ones that
  skip their operation.
- Scaffolded apps' gates gain `generate --check`.

## Out of scope

- **JSON on page routes** for the app's own scripts and native shells.
  A view feature that can come later without touching this design.
- **CORS.** The app's own scripts use page routes.
- **A rate limiter.** `Config.Before` is where one goes, and
  `rate_limited` is reserved. A 256-bit token cannot be guessed, and a
  failed check is one indexed lookup, so none ships in this slice.
- **`Idempotency-Key`, ETags, OpenAPI emission, serving v2 beside v1.**
- **OAuth, MCP and the generated skill:** specs 2 to 4.
- **File uploads over the API.**

## Review log

### Round 1 (Astra, high effort): 26 findings

1. **Ownership is not authorization (blocker).** Accepted. Authorization
   moved into the operation (`op.Check`, `Ctx.Authorize`) so both doors
   share it; scoped operations refuse an empty subject; `Guard` and
   `NeedsGuard` are gone. Section 4.
2. **Mergeable creates can collide (blocker).** Accepted, though
   pre-existing: `eventlog.AppendUnder` allocates and appends in one
   transaction, with a concurrency test. Section 2.
3. **Route access could default to read.** Accepted. `op.Access` has no
   valid zero; `New` validates method against access. Sections 2-3.
4. **Token management not bound to its owner.** Accepted. Section 5.
5. **The AST check doesn't prove parity.** Accepted. Renamed a lint,
   the operation each action must call is tabled, unconditional and
   error-used calls are required, and the gaps are stated. Section 7.
6. **Behavioural tests had no harness.** Accepted. An app fixture
   through the real handler, fresh databases per door, normalised
   comparison, invalid-create only when a field is required. Section 8.
7. **Scaffolded apps don't run `generate --check`.** Accepted; added to
   both of the scaffold's gate definitions. Section 7.
8. **Operation replacement undefined.** Accepted. One file per
   operation, replaced file for file by the actions mechanism;
   `types.go` stays generated. Section 7.
9. **No `context.Context`.** Accepted; operations take it first.
10. **Partial updates could clobber concurrent writes.** Accepted. One
    `CASE WHEN` update query; one mergeable event. Section 2.
11. **Required money loses presence; trimming differs by door.**
    Accepted. Inputs are `op.Opt`; normalisation moves into the
    operation. Section 2.
12. **`Opt` had no JSON form.** Accepted. `Opt` decodes presence;
    `null`, duplicates and case variants refused. Section 6.
13. **ID-only cursor mismatched the ordering.** Accepted. The cursor
    carries `(created_at, id)`. Section 6.
14. **Page pagination lost.** Accepted. `ListInput` has both modes.
    Section 2.
15. **The middleware slot ran after authentication.** Accepted.
    `Before` and `After`; `rate_limited` reserved. Section 3.
16. **The audit string lost the token.** Accepted. `Credential` on
    `Actor` and in `String()`; session and harness go to structured
    logs. Section 4.
17. **No transport-independent descriptor or verifier seam for MCP and
    OAuth.** Accepted. `op.Descriptor` with schemas and `Invoke`;
    `api.Verifier`. Sections 2-3.
18. **Nested `ServeMux` writes plain-text 404/405.** Accepted. The tree
    resolves routes itself; mounted with and without the slash; the
    outer mux's clean-path 301 is accepted and stated. Section 3.
19. **Panic and logging guarantees untestable.** Accepted. Abort after
    the response starts; token redaction in errors and panic values.
    Section 3.
20. **The reveal page wasn't `no-store`.** Accepted, with a reveal flow
    that never puts the secret in a URL or flash. Section 5.
21. **Config incomplete; wrong freshness guard.** Accepted.
    `RequireAgentSession`, `MaxBody` via the request context, and
    `sessions.RequireFresh(10 * time.Minute)`, which works with password
    sign-in. Sections 3, 5.
22. **`expose` needs three states.** Accepted. `*bool`, `omitempty`,
    explicit TOML support. Section 2.
23. **App names don't fit the prefix grammar.** Accepted. Derivation
    rule, and the prefix is checked before lookup. Section 5.
24. **Wire contradictions.** Accepted. Examples, field-name conversion,
    the status of every hook failure, precedence, bodyless DELETE.
    Section 6.
25. **Manifest edits can break v1 silently.** Accepted. A committed
    contract file compared by `generate --check`. Section 6.
26. **Wrong citations.** Accepted and corrected: amadan's CSRF case was
    cookie auth on `/api/v1`, not `.json` twins; `csrf.Protect` answers
    plain text; slides' decoder is not strict; sibling paths are now
    full; `/api/next-due` cites `serve.go:533`. The claim about amadan's
    form and API gates drifting is dropped.

### Round 2 (Astra, high effort): verdicts and 11 new findings

Round-one verdicts: 15 resolved, 9 partly, 1 not resolved. What changed:

- **Policy (1, partly).** `api.Config.Authorize` is now required, so an
  app turning on the API names its policy in writing; `op.DefaultPolicy`
  is documented as "a token can do whatever its owner can on a scoped
  resource". Sections 3 and 4.
- **Fixture (6, partly).** `Prepare` and `ValidCreate` added. Section 8.
- **Descriptors (17, partly).** `Input` and `Result` are required for
  every route; complete argument shapes per operation. Capabilities
  beyond read and write are the OAuth spec's to add. Section 2.
- **Routing (18, partly).** Classify with `mux.Handler`, dispatch with
  `mux.ServeHTTP` so path values are filled. Section 3.
- **Prefix (23, partly).** Leading digits get `app` prepended; ASCII
  only. Section 5.
- **Precedence (24, partly).** One processing order, which is the
  precedence. Sections 3 and 6.
- **Versioning (25, not resolved).** Generate compares before writing
  and fails on a breaking change without touching the file; `--check`
  is read-only; the contract carries method and path. Keeping an old
  contract served is stated as out of scope. Section 6.
- **Citations (26, partly).** amadan, CARLOS and keymail pinned.

New findings:

1. **Import cycle (blocker).** Accepted: `Access` and `Operation` move to
   the root package; `Descriptor` embeds `Operation`. Section 2.
2. **`op.Check` ignored read-only.** Accepted: the credential's access
   is checked first, whatever the policy. Section 4.
3. **Callers not fully classified.** Accepted: four kinds tabled, with
   the default for each; tickets and blog keep working. Section 4.
4. **Calling the operation to collect errors could write.** Accepted:
   pure `ValidateCreate`/`ValidateUpdate`; a page submission with parse
   errors never reaches the operation. Section 2.
5. **sqlc types a `CASE` flag as the column.** Accepted:
   `COALESCE(sqlc.narg(x), x)` instead, with a compile test. Section 2.
   (Superseded by the money merge below.)
6. **`mux.Handler` loses `{id}`.** Accepted, as round-one 18 above.
7. **The contract check could erase its own evidence.** Accepted, as
   round-one 25 above.
8. **The operation lint allowed an ignored `op.Check`.** Accepted: one
   exact guard shape, first; no initialisers that call functions.
   Section 7.
9. **Exposure and argument shapes.** Accepted: `Descriptor.Exposed`;
   argument types for every operation. Section 2.
10. **Precedence contradicted the pipeline.** Accepted, as round-one 24.
11. **An empty PATCH still wrote.** Accepted: no write, no event.
    Section 2.

### Implementation planning

`api.Config.Redact` is the redaction seam, implemented by
`(*apitoken.Store).Redact`, so `api` never imports `apitoken`.
`api.ErrUnauthorized` and `api.ErrUnavailable` are the sentinels a
`Verifier` wraps. `sessions.ActorFrom(r)` builds a page request's actor
from `sessions.Current`, and the generated router uses it when the ctx
factory left `Actor` zero.

### Merging main's money in every currency (`d48dd074`)

`main` gave every money field a currency while this branch was paused:
stores keep `<f>_minor` and `<f>_currency` (both nullable), the
manifest has a default `currency`, and templates write
`{{money $L .Amount .Currency}}`. What changed here:

- **Money on the wire** is `{"amount", "currency"}`, and `null` for an
  optional field with no value. The old line saying the API adds no
  currency field is gone. Section 6.
- **Partial updates** are a read, merge and write in one transaction on
  the single-connection writer, not `COALESCE`, which cannot clear a
  nullable money field. Section 2.
- **`op.Money` and `op.Nullable`** carry money input and its presence.
  Section 2.

### Plan revision against the merged generator

Rulings recorded while revising the generator tasks: the keyset is
written out (sqlc types the row-value id as a string); filter names may
not collide with list parameters; an update never changes a stored
currency (409); any `currency.Normalize` code is accepted on create
for v1; field messages are `form.Parse`'s ("Title is required"); the
lint allows a returning parse-failure branch before the operation call;
parity compares refusal kinds plus the API's required-field names.
Comments the generator writes into apps are listed for copy review, as
the money work did.
