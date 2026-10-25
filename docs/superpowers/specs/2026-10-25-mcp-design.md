# MCP for every app: the operations an app already has, as tools an agent can call

Status: draft for Paul's review. Written from the conversation of
2026-10-23 to 2026-10-25 and from the Oficina integration agent's
requirements in amadan discussion #116. Calls I made where nobody decided
are marked **(my call)**. Nothing is implemented.

Citations: `T/` is Tito Go (`amadan.net/tito/titogo`, `amadan/main`
`5190203b0`). `O/` is Oficina's agent-access module
(`amadan.net/oficina/agent-access`, branch `feat/agent-access` at
`4b94ff9`). Everything
else is this repo at `97eee1fa`.

## Why

This is spec 2 of four. Spec 1, the JSON API, landed as `2e833dd`. Specs 3
(agent grants) and 4 (a generated skill) follow.

The bar Paul set is Tito Go. People already use its MCP endpoint every day,
for example to find attendees and to change an event's settings. Tito Go
built that layer by hand (`T/internal/mcpapi`, `T/internal/instance/api_mcp.go`).
An app built on Rastrillo should get the same thing without writing it.

Spec 1 left exactly the pieces this needs:

- `op.Descriptor`: a stable ID, read or write access, a description, a
  confirm sentence, JSON Schemas for input and result, and a
  transport-independent `Invoke`.
- `op.Check` and the app's `Authorize`: one policy for every door.
- `api.Verifier`: the token check, behind an interface.
- `rastrillo.Actor`: the person and the agent acting for them.

So MCP is a second transport over the operations an app already has. It
adds no new authorization and no new way of running an operation.

## Who uses it

- **Rastrillo apps generally**, with generated operations
  (`gen.Operations()`) and personal tokens (`apitoken`).
- **Oficina's apps** (Docs, Sheets, Memoria, Calendar, Calls, Slides).
  They are mostly hand-written, so they register descriptors explicitly.
  They verify tokens from a separate connection service, not from
  personal tokens. Oficina is building its own layer now (`O/`) and will
  move onto this one later.

The first four clients are the Oficina CLI, Paseo, ChatGPT and Claude,
relayed from Paul by the Oficina agent (#116).

## Goals

1. **One handler an app mounts.** `mcp.New` serves MCP over the operations
   it is given, and the app mounts it like the API tree.
2. **The same rules as the API.** The same token check, the same policy,
   and the same `op.Check` on every call, with the actor coming only from
   the verifier. Authority is checked again while the answer goes out.
3. **Generated and hand-written operations alike.** It takes an explicit
   `[]op.Descriptor`, so an app needs no generated CRUD.
4. **Tools that tell an agent what calling them does.** Read-only,
   destructive, safe to repeat.
5. **What Tito Go learned, built in from the start.** Argument errors the
   model can read and fix. A response size cap. A private tool list.
   Server instructions. Exact numbers.
6. **Ready for spec 3.** A separate connection service plugs in through
   `api.Verifier`, with grant facts the app's policy can read. Nothing in
   MCP dispatch knows how a person signed in.

## Non-goals

- **The grant service itself**, meaning OAuth, browser consent, client
  registration and refresh tokens. That is spec 3.
- **Resources, prompts and completions.** Tito Go uses none (`T/` §7 of
  the survey). This spec adds tools only.
- **File uploads, binary downloads, large exports and long-running jobs.**
  Tito Go deliberately leaves binary transfer out: "never put a database
  in model context" (`T/internal/mcpapi/mcpapi.go:92`). Same here.
- **Framework idempotency keys.** Still deferred from spec 1. An
  operation can still take its own retry key as an input (section 4).
- **A stdio transport.** Remote MCP over HTTP only. Oficina's CLI provides
  a local stdio adapter over its own API (`O/README.md`).

## Anti-goals

These would make it a failure even if it worked:

- **A second authorization path.** If an MCP call can do anything the same
  actor couldn't do through the API, the design has failed.
- **Agents told a retry is safe when it isn't.** Tito Go once offered
  `idempotency_key` on every POST, including ones that ignored it. Its test
  now reads "an agent told a retry is safe must be told the truth"
  (`T/internal/mcpapi/mcpapi_test.go:341-342`).
- **Sign-in baked into dispatch.** The Oficina agent's words: "do not bake
  per-app browser login into MCP operation dispatch."
- **One person's tools served to another.** The tool list is per caller,
  and nothing about it is cached or shared.

## Trust boundaries

This section is the decision Paul is asked to approve.

```
person ──signs in──> identity provider   (who you are)
   │
   └──consents──> connection service     (what this agent may do, for which app, team and scopes)
                       │ issues
                       ▼
                  app-audience token ──> app: api.Verifier ──> Actor{Subject, Agent, Credential, Grant}
                                            │
                                            └─> resolves Subject to an existing local account,
                                                then op.Check and Authorize, per call
```

1. **The identity provider and the connection service are separate
   parties.** The identity provider says who a person is. The connection
   service records what a person let an agent do. Identity assertions never
   carry agent grants. Oficina's identity provider is `id.oficina.works`.
   Its connection service is a separate deployment. Neither is required.
2. **Tokens reach an app limited to that app.** A token names its audience
   (one app), and may name teams and scopes. An app refuses a token whose
   audience isn't its own.
3. **The issuer of a grant and the issuer of the identity are separate
   namespaces.** A grant token carries both. An app checks the exact
   identity issuer it is configured for before it maps an opaque subject to
   a local account. Oficina binds its grant store to its identity issuer so
   a config change can't relabel old grants (#116).
4. **The app always has the last word.** After verifying, it resolves the
   subject to an existing local account, applies current local admission
   and item permissions, and runs its policy on every call. It checks
   again while the answer goes out (section 3). A grant can only narrow
   what the person could already do.
5. **Two deployments of the same interfaces:**
   - **Single app.** The app is its own connection service: personal
     tokens today (`apitoken`), and an app-hosted consent page in spec 3.
   - **Several apps.** A separate connection service that several apps
     trust, like Oficina's. Each app verifies its tokens online through a
     `Verifier`.

   MCP dispatch is identical in both. It sees only the `Actor` a verifier
   returns.

This spec builds only what MCP needs: the `Grant` facts on `Actor`, the
recheck and guard hooks, and the handler that honours them. Spec 3 designs the services that issue
them.

## Design

### 1. The package

**(my call)** A new package `amadan.net/rastrillo/rastrillo/mcp`. It
imports the official Go SDK internally, so an app never needs to.

```go
package mcp

type Config struct {
    Gate         api.Gate         // from apiConfig.Gate(): the API tree's token check and pipeline (section 2)
    Ctx          func(*http.Request) *rastrillo.Ctx
    Authorize    func(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error // required, as for api.New
    Guard        op.ResponseGuard // optional: the app's check of what an answer discloses (section 3)
    Name         string           // the server's name in the MCP handshake, e.g. "tickets"
    Version      string           // e.g. rastrillo.BuildVersion
    Instructions string           // the app's own part; the framework's part comes first (section 7)
    MaxResult    int              // bytes of one tool result; zero means 2 MiB
}

func New(cfg Config, ops ...op.Descriptor) (http.Handler, error)
```

`New` refuses, at boot:
- a nil `Gate.Verify`, `Ctx` or `Authorize`;
- a descriptor with an empty ID, an invalid `Access`, an `Input` that
  isn't an object schema, a `Result` that is neither an object schema nor
  exactly `{"type":"null"}`, or a nil `Invoke`;
- a read descriptor marked `Destructive`, or a write that fails the fence
  in section 4;
- two descriptors whose tool names (section 5) are equal.

It serves exactly the descriptors it is given. For a generated app that
is the exposed half of `gen.Operations()`:

```go
mcpHandler, err := mcp.New(mcp.Config{
    Gate: apiCfg.Gate(), Ctx: apiCfg.Ctx, Authorize: apiCfg.Authorize, Name: "notes",
}, gen.ExposedOperations()...)
mux.Handle("/api/v1/mcp", mcpHandler)
```

`gen.ExposedOperations()` is a new generated function. It returns the
descriptors whose `Exposed` is true, so an app doesn't filter by hand.

**(my call)** The endpoint is mounted at `/api/v1/mcp`, inside the API
tree's path space and outside CSRF and sessions, like the tree. The app
mounts it, and the path is the app's choice. The scaffold uses this one.

### 2. Sharing the token check with the API tree

The API tree's request pipeline lives inside `api.New` today
(`api/tree.go:205-443`: `ServeHTTP`, `gate`, `handle`, `recovered`):
`no-store`, `Before`, the credential check, the agent headers, two checks
that depend on the route's operation, `After`, panic recovery and
redacted logging.

The two per-operation checks are `RequireAgentSession`, which refuses an
agent's write with no `Agent-Session`, and then the read-only check
(`api/tree.go:318-329`). The tree knows the operation from the route. One
MCP endpoint serves every operation, so it only knows the operation once
it has parsed a tool call.

So the pipeline moves into one exported type both use, split at that
point:

```go
package api

// Gate is the request pipeline every agent-facing handler shares.
type Gate struct {
    Verify              Verifier
    Logger              *slog.Logger
    MaxBody             int64
    Before, After       []func(http.Handler) http.Handler
    RequireAgentSession bool
    Redact              func(string) string
}

// Gate returns the pipeline half of a tree's Config.
func (c Config) Gate() Gate

// Wrap runs h behind the gate: Before, Verify, agent headers, After,
// panic recovery, redacted logging and guarded emission (section 3), with
// the Actor on the context. A malformed agent header is refused here, for
// every request.
func (g Gate) Wrap(h http.Handler) http.Handler

// Admit is the gate's per-operation half, in the tree's order:
// RequireAgentSession for a write, then the read-only check. The tree
// calls it per route, before the handler; MCP calls it per tool call,
// before Invoke.
func (g Gate) Admit(a rastrillo.Actor, o rastrillo.Operation) error
```

- **`api.Config` keeps its fields.** Embedding `Gate` would break every
  existing `api.Config{Verify: ...}` literal, because Go doesn't accept
  promoted fields as keys, the scaffold's included
  (`cmd/rastrillo/scaffold/api.go.tmpl:33`). `Config.Gate()` copies them
  out instead.
- **Over MCP, an `Admit` refusal is a tool error with the API's code:**
  `invalid` with the API's agent-session message for a missing session,
  and `forbidden` for a read-only token. A read-only agent's write with no
  session gets `invalid`, as at the API door. `initialize`, `tools/list`
  and read tools never need a session, just as API reads don't.
- **Panics are caught inside the SDK's dispatch too.** The SDK runs each
  request's handler on a goroutine of its own, with no recovery
  (go-sdk v1.8.0 `internal/jsonrpc2/conn.go:684`), so the gate's recovery
  can't reach it. MCP recovers inside each tool handler and the
  `tools/list` middleware. That covers `Admit`, `op.Check`, `Authorize`,
  `Invoke` and encoding the result. A panic answers an `internal` tool
  error with a ref, logged redacted, and the process stays up.
- **A test pins the same answers from both handlers** for an unknown
  token, a malformed agent header, a write with no session, a read-only
  agent's write with no session, and a panic.

### 3. Authority checked again while the answer goes out

A token can be revoked, or its grant narrowed, between the start of a
request and the end of its answer. Oficina's agent-access checks again as
the answer leaves (`O` `4b94ff9`, `http_guarded.go`), and asks that
Rastrillo converge on the same hooks (#116). The gate does this for both
doors.

**A pure recheck on the verifier.** One optional interface:

```go
package api

// Rechecker is a Verifier that can check a credential again without side
// effects: no quota charged, no last-used time written. Verify runs once
// per request; Recheck runs as the answer is emitted.
type Rechecker interface {
    Recheck(ctx context.Context, r *http.Request, was rastrillo.Actor) (rastrillo.Actor, error)
}
```

`apitoken` implements it as a read-only lookup. Its `Verify` writes
`last_used` (`apitoken/store.go:244`), and `Recheck` must not.

**The app's response guard.** One optional hook:

```go
package op

// ResponseGuard checks, without side effects, that what an answer
// discloses is still the actor's to see: for example that each note in a
// search result is still in a team they belong to. It must never run the
// operation again or change state. d is nil for the tool list.
type ResponseGuard func(ctx context.Context, a rastrillo.Actor, d *Descriptor, input json.RawMessage, result any) error
```

**How an answer goes out:**

1. The handler's answer is buffered, up to `MaxResult` for MCP, and up to
   the same cap, 2 MiB by default, for the tree. An answer over the cap
   is refused as too large, as section 6 describes.
2. Before the headers are sent, and again before each 64 KiB of body:
   - `Recheck` runs, with a 5-second deadline.
   - The fresh actor's `Subject`, `Agent`, `Credential` and `Grant` (ID,
     issuers and audience) must equal the original's.
   - `Admit` and `op.Check` pass again, with the fresh actor, for the
     operation that ran.
   - For the tool list, they pass again for every tool that was listed.
     The disclosed set is frozen when the list is built. Recomputing the
     list instead would quietly drop a tool that became unavailable and
     pass.
   - `Guard` runs last, when there is one. The guard's docs say to put a
     slow check against another service before the app's own revision
     and link checks, so the last thing checked is the closest to the
     bytes going out.
3. Each chunk gets a write deadline. A transport that can't set one gets
   `unavailable` before any data is sent.
4. **If a check fails before the headers:** the answer becomes the
   refusal. That is 401 `unauthorized` when the identity changed or the
   token is gone, 403 `forbidden` when access narrowed, in the API's error
   envelope.
5. **If a check fails after the headers:** the connection is aborted
   (`http.ErrAbortHandler`). The answer carries a `Content-Length`, so a
   client never takes a cut-off answer for a whole one.
6. **Without a `Rechecker`, nothing is rechecked,** but the answer is
   still buffered and capped.
7. **The headers are set at commit:** `Cache-Control: no-store` and the
   `Content-Length`. The SDK sets `Cache-Control: no-cache, no-transform`
   itself (go-sdk v1.8.0 `mcp/streamable.go:1683`), and the gate's value
   replaces it.

A write has already happened when its answer is refused. The
instructions tell agents to read back rather than retry (section 7).

### 4. What a descriptor says about a call

`op.Descriptor` gains two fields, with MCP's meanings
(go-sdk v1.8.0 `mcp/protocol.go:1958-1970`):

```go
Destructive bool // may change or remove what's already there; false means it only adds
Idempotent  bool // calling it twice with the same input leaves the same state as calling it once
```

- **Read-only** is not a new field. It is `Access == rastrillo.Read`.
- **What the generator sets:**
  - `Create` adds, so it is neither destructive nor idempotent.
  - `Update` overwrites fields, so it is destructive and idempotent.
  - `Delete` is destructive and idempotent.
  - `List` and `Get` are read-only.
- **A replaced operation body gets the safe flags.** An app may replace a
  generated create, update or delete with its own file while `types.go`,
  which holds the descriptor, stays generated
  (`internal/generate/opsgen/opsgen.go:229`). The generator can't know
  what the replacement does, for example whether it sends email. So a
  replaced write is marked destructive and not idempotent. An app that
  knows better passes its own descriptor to `mcp.New` instead of the
  generated one.
- **Can't be undone is a different question, and `Confirm` answers it.**
  An operation with a `Confirm` sentence is one to ask the person about
  first (section 5). `Destructive` alone doesn't mean that: renaming a
  bookmark is destructive in MCP's sense.
- **A hand-written descriptor says what's true of it.**
- **The framework never adds an idempotency argument.** An operation that
  honours a retry key has the key in its own `Input` and sets
  `Idempotent`. Memoria's `notes_create` does this with `client_id`
  (`memoria` `origin/feat/agent-access` `70aa5b8`,
  `internal/memoria/agent_access.go:122-124`). Generated creates have no
  key, so they are not idempotent, and spec 1's deferral of idempotency
  keys stands.
- **A fence, the way Tito Go does it**
  (`T/internal/mcpapi/mcpapi_test.go:280-297`). A write descriptor whose
  ID's last part starts with one of Tito Go's undoing verbs must have
  `Destructive` set. Those verbs are `cancel`, `refund`, `revoke`,
  `reject`, `remove`, `withdraw`, `discard`, `disconnect`, `rotate`,
  `regenerate`, `replace`, `delete`, `send` and `resend`, plus `purge`.
  The generator's tests run it over `gen.Operations()`, and `mcp.New`
  runs it at boot on whatever it is given.

### 5. The tool list

- **One tool per descriptor.** The name is the descriptor ID with `.`
  replaced by `_` (`bookmarks.create` becomes `bookmarks_create`). That
  fits MCP's name rule and stays readable. `New` refuses a name that
  doesn't match `^[a-z][a-z0-9_]{0,63}$`.
- **The tool:**
  - Its title is the description's first sentence, and its description is
    the descriptor's `Description`.
  - When there is a `Confirm` sentence, the description gains a line:
    "Before calling, confirm with the person: <Confirm>", with the
    sentence's placeholders left as written.
  - Input and output schemas are `Input` and `Result` (section 6 for a
    null result).
  - Annotations: `readOnlyHint` from `Access`, plus `destructiveHint` and
    `idempotentHint` from section 4.
  - `openWorldHint` is left at its default, true. Tito Go's reason: many
    writes reach real people, and saying which don't would be a claim per
    operation (`T/internal/mcpapi/mcpapi.go:191-206`).
- **The list is per caller.** A descriptor is listed only if `Admit` and
  `op.Check` both pass for this caller, so a read-only token sees no write
  tools. Tito Go narrows the same way (`T/internal/instance/api_mcp.go:67-81`).
- **Every tool is registered; only the list is narrowed.**
  - Tito Go registers only the allowed tools, so a call to a hidden one
    gets the SDK's "unknown tool" protocol error.
  - Here the per-request server registers every descriptor. A receiving
    middleware on `tools/list` (`AddReceivingMiddleware`, go-sdk v1.8.0
    `mcp/server.go:1860`) drops the ones this caller can't use.
  - A call to a hidden tool then reaches `Admit` and `op.Check`. It gets
    the same `forbidden` the API door gives, which the agent can explain
    to the person.
  - The list is never treated as permission. Tool names and schemas
    aren't secret: they're the app's published API (`api/v1.json`).
- **The list is private.** The SDK's cacheable hint is set to `private`
  (`T/internal/mcpapi/mcpapi.go:278`), and the HTTP answer is `no-store`
  (section 3).
- **The list doesn't change mid-connection.** The server's capabilities
  are set explicitly, with `tools.listChanged` false. Otherwise the SDK
  advertises list changes as soon as a tool is registered
  (go-sdk v1.8.0 `mcp/server.go:673-678`), and section 10 refuses the
  stream that would carry them.

### 6. A call

1. The SDK parses the JSON-RPC request. The gate has already verified the
   token and put the `Actor` on the context.
2. The handler builds `rc` from `Config.Ctx`, a copy per request, as the
   tree does since the whole-branch review of spec 1. It sets `rc.Actor`
   and `rc.Authorize`.
3. **The arguments' bytes must be valid UTF-8 with no lone surrogate
   escapes.** The check runs on the raw bytes, before any decoder can
   repair them (#116), and a failure is an `invalid` tool error.
4. `Admit` runs.
5. **The raw arguments go to `Invoke` unchanged.** There is no separate
   check against the input schema. **(my call)** Two reasons:
   - The API door has none either. It hands the body to `Invoke`, which
     decodes with `op.DecodeInput` and refuses what doesn't fit. Doing the
     same keeps both doors giving the same answer by construction.
     A schema check would refuse `limit: 0` and `limit: 201`, which the API
     door accepts (the operation defaults the first and clamps the
     second, `examples/notes/gen/ops/bookmarks/list.go:22`).
   - A schema check through jsonschema-go isn't exact. It compares numbers
     as float64 (jsonschema-go v0.4.3 `jsonschema/validate.go:157`), so
     `9007199254740993` would pass a `maximum` of `9007199254740992`. With
     `UseNumber` it is worse: it reads a `json.Number` as a string, and
     every integer fails "type: integer", the bug Tito Go fixed in
     `T` 37471b2a9.

   A hand-written descriptor validates inside its `Invoke`, as it must for
   the API door already. Memoria's registry does this with exact numbers
   (#116).
6. `Invoke` calls the operation, whose first statement is `op.Check`.
7. **A refusal is a tool error, not a protocol error**, so the model can
   read it and fix the call (`T/internal/mcpapi/mcpapi.go:285-296`). The
   text is the same code and message the API's error envelope uses:
   `invalid` with its fields, `forbidden`, `not_found`, `conflict`, or
   `internal` with a `ref`. An `internal` error is logged, redacted, with
   the ref. An unknown tool stays a protocol error, as the SDK answers it.
8. **The result:**
   - `structuredContent` is the operation's result.
   - The text block is the same value as JSON.
   - An operation whose `Result` is `{"type":"null"}`, such as every
     generated delete, has no `outputSchema` and no `structuredContent`.
     Its text block is `null`. The API door already answers these with no
     body, so spec 1's contract and its parity test don't change.
   - Results are objects or null, nothing else. SDK v1.8.0 accepts any
     output schema (`mcp/server.go:299`), but protocol revision 2025-06-18
     requires an object, and clients built to it may reject anything else.
   - Numbers survive intact: the result is encoded with json v2 and never
     goes through float64.
9. **The size cap.** A result over `MaxResult` (default 2 MiB) is replaced
   with a tool error: "Ask for less: pass a smaller limit or a narrower
   query." Tito Go returns the same (`T/internal/mcpapi/mcpapi.go:285-296`).
10. The answer goes out through section 3.

Each call logs one line with the actor's `LogAttrs` and the tool name, the
same as an API request.

### 7. Instructions

The server sends instructions on connect: the framework's part, then the
app's `Config.Instructions`. **(my call)** The framework's part is drafted
from Tito Go's (`T/internal/mcpapi/mcpapi.go:208-217`) and covers only
what is true of every Rastrillo app:

> Every tool here is one operation of this app, run as you, for the
> person who connected you.
>
> - Money is an object: an integer amount in the currency's minor unit,
>   and an ISO 4217 code. Never send a float.
> - Times are RFC 3339 in UTC.
> - Take ids from list and get results. Don't guess them.
> - Read before you write: get a thing, then change only the fields you
>   mean to.
> - Lists are paged: pass next_after back as after until it is null.
> - When a tool's description asks you to confirm with the person, it
>   can't be undone. Ask them first, using the sentence it gives.
> - Don't retry a write blindly. If a call timed out or was cut short,
>   read the thing back before trying again.

This is new user-facing copy. It goes through Paul's copy review before it
ships.

### 8. Grant facts on the Actor

Spec 1's `Actor` holds read or write access only, which can't carry
Oficina's grants (#116, question 3). So `Actor` gains one optional field:

```go
type Grant struct {
    ID             string   // the grant this token belongs to; revoking it ends every token under it
    Issuer         string   // who issued the grant: the connection service
    IdentityIssuer string   // who says who Subject is: the identity provider
    Audience       string   // the app this token is for
    Teams          []string // empty means the grant names no team
    Scopes         []string // empty means the grant names no scope
}

// in Actor:
Grant *Grant // nil for a browser session and a personal token
```

- **A verifier sets it, and nothing else does.** `apitoken` leaves it nil.
  A connection-service verifier, such as Oficina's `RemoteVerifier`
  (`O/remote.go`), fills it from introspection.
- **The app's policy reads it.** `Authorize` checks the scope for each
  operation, and the app's `Guard` checks a team named in the input. The
  framework doesn't interpret `Teams` or `Scopes`, because their meaning
  is the app's: Oficina's `notes:write` means nothing to Rastrillo.
- **`op.Check` keeps its order.** The read-only check runs first, then
  scoped needs a subject, then `Authorize`. A nil `Grant` changes nothing.
- **The audit line grows.** `Actor.String()` gains `grant:<id>` when there
  is one, and `LogAttrs` adds the issuers and audience.
- **The verifier owns three checks, and the docs say so:**
  - The audience is this app.
  - The identity issuer is the exact one the app is configured for.
  - The subject resolves to an existing local account.

  Rastrillo ships no grant verifier in this spec. Spec 3 ships one, with
  its `Recheck`.

### 9. Clients

| Client | How it connects in spec 2 |
|---|---|
| Claude Code | `claude mcp add --transport http <app> https://<host>/api/v1/mcp --header "Authorization: Bearer <token>"`, with a personal token |
| Codex | the same URL and bearer header in its MCP config |
| Oficina CLI, Paseo | through a connection service's tokens, verified by the app's `Verifier` |
| ChatGPT, claude.ai | need OAuth discovery and consent: spec 3 |

A request with no token is answered 401 with `WWW-Authenticate: Bearer`.
Spec 3 adds `resource_metadata` to that header, plus the protected-resource
metadata document. This spec reserves the path
`/.well-known/oauth-protected-resource/api/v1/mcp` and mounts nothing there.

### 10. Transport

- **The official Go SDK**, `github.com/modelcontextprotocol/go-sdk`, at
  v1.8.0, the version Tito Go runs. The SDK handles protocol version
  negotiation, and the tests pin the versions it answers.
- **Streamable HTTP, stateless, with plain JSON responses.** Each request
  builds its own server, so no state outlives the request
  (`T/internal/instance/api_mcp.go:66-82`).
- **No streams at all.** A CARLOS instance hibernates, and an open stream
  would hold it awake. `JSONResponse` isn't enough on its own: the SDK
  switches to an event stream for `subscriptions/listen` whatever the
  setting (go-sdk v1.8.0 `mcp/streamable.go:1678-1683`).
  - The handler refuses a GET with 405.
  - It refuses a POST whose JSON-RPC message, or any message in a batch,
    is `subscriptions/listen`, before the SDK sees it. It answers that
    with a JSON-RPC "method not found" error.
  - The body has already been capped, so reading the method is cheap.
- **Request body cap:** `Gate.MaxBody`, default 1 MiB, the API's.
- **Dependencies:** the SDK, which brings `google/jsonschema-go` with it.
  MCP itself doesn't validate with it (section 6). Both must pass
  govulncheck and the size and dependency gates, and `chromedp-graph`
  shows they stay out of the browser test graph.

### 11. The generator and the scaffold

- **The generator** sets section 4's flags on every generated descriptor,
  including the safe flags for a replaced body. It also emits
  `gen.ExposedOperations()`.
- **`api/v1.json`** gains the two flags per operation.
  - A flag changing from true to false is not a breaking change.
  - `Destructive` changing from false to true is noted, not refused.
- **The scaffold** mounts `/api/v1/mcp` beside the API tree, with
  `apiCfg.Gate()` and the same policy. Its `README` gives the Claude Code
  line.

### 12. Testing

- **Through a real MCP client.** Tests drive the handler with the SDK's
  own client over `httptest`: initialize, list tools, call one.
- **For a scoped and an unscoped generated resource:**
  - list as a person's token, a read-only token and a token without
    policy;
  - call create, get, update and delete;
  - check that a refusal is a tool error with the API's code;
  - check that the oversized-result path returns its tool error.
- **The same outcome on both doors.** The generated parity test (spec 1,
  §8) gains an MCP leg: the same scenario through the API door and the MCP
  door gives the same rows and refusal kinds. It includes `limit: 0` and
  `limit: 201`.
- **Exact numbers:** a 2^53 + 1 integer in and out. **Bad bytes:** invalid
  UTF-8 and a lone surrogate are refused.
- **The gate:**
  - An unknown token, a malformed agent header, a write with no session,
    a read-only agent's write with no session, and a panic each answer the
    way the API tree does.
  - A panic in `Authorize`, in `Invoke` and while encoding a result each
    answer an `internal` tool error, and the test process survives.
  - Every real HTTP answer carries `Cache-Control: no-store`, the error
    answers included.
  - A GET and a `subscriptions/listen` are refused. `initialize`
    advertises no list changes.
- **Rechecks:**
  - A token revoked before the headers go out turns the answer into 401.
  - A token revoked after the first 64 KiB aborts the connection.
  - A grant narrowed between building the list and sending it refuses the
    list.
  - The guard runs once per check, and `Invoke` runs once per call.
  - Rechecks leave `apitoken`'s `last_used` alone.
- **Flags:** a replaced generated update is listed destructive and not
  idempotent, and the fence refuses an unmarked `notes.purge`.
- **Grant facts:** a fake verifier sets `Grant`, and an `Authorize` that
  checks a scope hides the tool from the list and refuses the call.
- **The Oficina interop fixture.**
  - Memoria's `notes_create` and `notes_search`, copied with their names,
    descriptions, scopes, flags and schemas from `memoria`
    `origin/feat/agent-access` `70aa5b8`
    (`internal/memoria/agent_access.go:118-124`), as hand-written
    descriptors in a test. Their `Invoke` is a fake store.
  - They run through `mcp.New` with a fake connection-service verifier and
    rechecker, and a guard that checks the input's team.
  - This proves Oficina can move its Memoria integration onto `mcp.New`
    without changing its operations (#116).
- **Fail-first** as `CONTRIBUTING.md` requires.

### 13. Docs

- `docs/site/mcp.md`, new: mounting, connecting from Claude Code and
  Codex, writing descriptors by hand, the flags, the instructions, the
  recheck and guard contracts, and the trust boundaries in plain words.
- `docs/site/api.md`: a section pointing at MCP, plus `Gate`,
  `Rechecker` and guarded emission.
- `skill/api.md`: the MCP lines.
- The reference pages for `mcp`, the changes in `api` and `op`, and the
  CHANGELOG.

All new user-facing copy goes through Paul's copy review.

## Left to the builder

- How `Gate` is factored out of the tree, as long as sections 2 and 3's
  behaviour holds and the tree's tests pass unchanged.
- How the buffering writer and the chunk loop are built, within
  section 3.
- The exact wording of each tool error, using the API's existing messages
  wherever they fit.
- How the SDK's per-request server is wired, within section 10.

## Open for Paul

1. **The trust boundaries.** Approve the two-deployment shape: an app as
   its own connection service, or a separate service several apps trust.
   Spec 3 builds both on it.
2. **Checking again on the API door too.** Section 3 buffers and rechecks
   the API tree's answers as well as MCP's, so both doors keep the same
   rules. That caps a tree answer at 2 MiB. Generated lists stay well
   under that, at 200 rows a page.
3. **The package name `mcp`.** It shares a name with the SDK's own
   package, which an app would only notice if it imports both.
4. **The endpoint path `/api/v1/mcp`.** Tito Go uses `/<account>/api/mcp`.
