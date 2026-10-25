# MCP for every app: the operations an app already has, as tools an agent can call

Status: draft for Paul's review. Written from the conversation of
2026-10-23 to 2026-10-25 and from the Oficina integration agent's
requirements in amadan discussion #116. Calls I made where nobody decided
are marked **(my call)**. Nothing is implemented.

Citations: `T/` is Tito Go (`amadan.net/tito/titogo`, `amadan/main`
`5190203b0`). `O/` is Oficina's agent-access module
(`amadan.net/oficina/agent-access`, branch `feat/agent-access`). Everything
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
   the verifier.
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
- **Idempotency keys.** Still deferred from spec 1. The descriptor gains a
  flag for them now (section 3) so spec 3 or a later spec can honour it.
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
   and item permissions, and runs its policy on every call. A grant can
   only narrow what the person could already do.
5. **Two deployments of the same interfaces:**
   - **Single app.** The app is its own connection service: personal
     tokens today (`apitoken`), and an app-hosted consent page in spec 3.
   - **Several apps.** A separate connection service that several apps
     trust, like Oficina's. Each app verifies its tokens online through a
     `Verifier`.

   MCP dispatch is identical in both. It sees only the `Actor` a verifier
   returns.

This spec builds only what MCP needs: the `Grant` facts on `Actor` and
the handler that honours them. Spec 3 designs the services that issue
them.

## Design

### 1. The package

**(my call)** A new package `amadan.net/rastrillo/rastrillo/mcp`. It
imports the official Go SDK as `sdk` internally, so an app never needs
to.

```go
package mcp

type Config struct {
    api.Gate                     // the token check and request pipeline, shared with api.New (section 2)
    Ctx          func(*http.Request) *rastrillo.Ctx
    Authorize    func(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error // required
    Name         string          // the server's name in the MCP handshake, e.g. "tickets"
    Version      string          // e.g. rastrillo.BuildVersion
    Instructions string          // the app's own part; the framework's part is prepended (section 6)
    MaxResult    int             // bytes of one tool result; zero means 2 MiB
}

func New(cfg Config, ops ...op.Descriptor) (http.Handler, error)
```

`New` refuses, at boot:
- a nil `Verify`, `Ctx` or `Authorize`;
- a descriptor with an empty ID, an invalid `Access`, an empty or
  non-object `Input` or `Result`, or a nil `Invoke`;
- two descriptors whose tool names (section 4) are equal.

It takes only exposed descriptors that are passed to it. For a generated
app that is the exposed half of `gen.Operations()`:

```go
mcpHandler, err := mcp.New(mcp.Config{Gate: gate, Ctx: ctxFactory, Authorize: op.DefaultPolicy, Name: "notes"}, gen.ExposedOperations()...)
mux.Handle("/api/v1/mcp", mcpHandler)
```

`gen.ExposedOperations()` is a new generated function. It returns the
descriptors whose `Exposed` is true, so an app doesn't filter by hand.

**(my call)** The endpoint is mounted at `/api/v1/mcp`, inside the API
tree's path space and outside CSRF and sessions, like the tree. The
app mounts it, and the path is the app's choice. The scaffold uses this
one.

### 2. Sharing the token check with the API tree

The API tree's request pipeline lives inside `api.New` today
(`api/tree.go:205-443`: `ServeHTTP`, `gate`, `handle`, `recovered`): `no-store`, `Before`, the credential check, the
agent headers, the read-only check, `After`, panic recovery and redacted
logging. MCP needs all of it except the per-route read-only check. For MCP
that check happens per tool, in `op.Check`, which already refuses a write
operation to a read-only actor (spec 1, §4).

So the pipeline moves into one exported type both use:

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

// Wrap runs h behind the gate: no-store, Before, Verify, agent headers,
// After, panic recovery and redacted logging, with the Actor on the context.
func (g Gate) Wrap(h http.Handler) http.Handler
```

`api.Config` embeds `Gate`, which changes no field names, so existing
callers compile unchanged. The tree's per-route read-only check stays in
the tree. A test pins that both handlers answer an unknown token, a
malformed agent header and a panic the same way.

### 3. What a descriptor says about a call

`op.Descriptor` gains three fields:

```go
Destructive    bool // can't be undone through the API: a delete, a cancel, a refund, a send, a whole-list replace
Idempotent     bool // calling it twice with the same input leaves the same state as calling it once
IdempotencyKey bool // the operation honours a client-supplied key (none do yet; false everywhere in this spec)
```

- **Read-only** is not a new field. It is `Access == rastrillo.Read`.
- **The generator sets them:** `Delete` is destructive and idempotent;
  `Update` is idempotent; `Create` is neither; `List` and `Get` are
  read-only.
- **A hand-written descriptor says what's true of it.** `New` refuses a
  read operation marked destructive.
- **A fence test, the way Tito Go does it** (`T/internal/mcpapi/mcpapi_test.go:280-297`):
  any write descriptor whose ID's last part starts with one of Tito Go's
  undoing verbs (`cancel`, `refund`, `revoke`, `reject`, `remove`,
  `withdraw`, `discard`, `disconnect`, `rotate`, `regenerate`, `replace`,
  `delete`, `send`, `resend`) or `purge` must have `Destructive` set. It runs over `gen.Operations()` in the generator's tests, and
  `mcp.New` applies the same check at boot to hand-written ones.

### 4. The tool list

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
  - Input and output schemas are `Input` and `Result`.
  - Annotations: `readOnlyHint` from `Access`, plus `destructiveHint` and
    `idempotentHint` from section 3.
  - `openWorldHint` is left at its default, true. Tito Go's reason: many
    writes reach real people, and saying which don't would be a claim per
    operation (`T/internal/mcpapi/mcpapi.go:191-206`).
- **The list is per caller.** On each request, a descriptor is listed only
  if `op.Check` passes for this caller, so a read-only token sees no write
  tools. Tito Go narrows the same way (`T/internal/instance/api_mcp.go:67-81`).
  `op.Check` runs again on every call, so the list is never treated as
  permission.
- **The list is private.** Every response sends `Cache-Control: no-store`,
  and the SDK's cacheable hint is set to `private`
  (`T/internal/mcpapi/mcpapi.go:278`).

### 5. A call

1. The SDK parses the JSON-RPC request. The gate has already verified
   the token and put the `Actor` on the context.
2. The handler builds `rc` from `Config.Ctx`, a copy per request, as the
   tree does since the whole-branch review of spec 1. It sets `rc.Actor`
   and `rc.Authorize`.
3. **The arguments are checked before anything else:**
   - The raw bytes must be valid UTF-8 with no lone surrogate escapes. The
     check runs before any decoder can repair them (#116).
   - The input is validated against `Input`, and a failure is a tool
     error. The validator must see a number as a number: jsonschema-go
     reads a `json.Number` as a string, so decoding with `UseNumber` for
     the check made every integer argument fail "type: integer". Tito Go
     shipped that bug and fixed it in `T` 37471b2a9.
   - Nothing the validator decoded is passed on. The raw bytes go to
     `Invoke` unchanged, so no value is rounded on its way to the
     operation.
4. `Invoke` decodes with the API door's rules and calls the operation. The
   operation's first statement is `op.Check`.
5. **A refusal is a tool error, not a protocol error**, so the model can
   read it and fix the call (`T/internal/mcpapi/mcpapi.go:285-296`). The
   text is the same code and message the API's error envelope uses:
   `invalid` with its fields, `forbidden`, `not_found`, `conflict`, or
   `internal` with a `ref`. An `internal` error is logged, redacted, with
   the ref. An unknown tool stays a protocol error, as the SDK answers it.
6. **The result:**
   - `structuredContent` is the operation's result.
   - The text block is the same value as JSON.
   - Numbers survive intact: the result is encoded with json v2 and never
     goes through float64.
7. **The size cap.** A result over `MaxResult` (default 2 MiB) is replaced
   with a tool error: "Ask for less: pass a smaller limit or a narrower
   query." Tito Go returns the same (`T/internal/mcpapi/mcpapi.go:285-296`).
   A write may already have happened when this fires, so the instructions
   tell agents not to retry it blindly (section 6).

Each call logs one line with the actor's `LogAttrs` and the tool name,
the same as an API request.

### 6. Instructions

The server sends instructions on connect. They are the framework's part,
then the app's `Config.Instructions`. **(my call)** The framework's part,
drafted from Tito Go's (`T/internal/mcpapi/mcpapi.go:208-217`) and
covering only what is true of every Rastrillo app:

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
> - A tool marked destructive can't be undone. Confirm with the person
>   first, using the sentence its description gives.
> - Don't retry a write blindly. If a call timed out or was cut short,
>   read the thing back before trying again.

This is new user-facing copy. It goes through Paul's copy review before
it ships.

### 7. Grant facts on the Actor

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
- **The app's policy reads it.** `Authorize` checks the team and scope
  for each operation. The framework doesn't interpret `Teams` or
  `Scopes`, because their meaning is the app's: Oficina's `notes:write`
  means nothing to Rastrillo.
- **`op.Check` keeps its order.** The read-only check runs first, then
  scoped needs a subject, then `Authorize`. A nil `Grant` changes nothing.
- **The audit line grows.** `Actor.String()` gains `grant:<id>` when there
  is one, and `LogAttrs` adds the issuers and audience.
- **The verifier owns three checks, and the docs say so:**
  - The audience is this app.
  - The identity issuer is the exact one the app is configured for.
  - The subject resolves to an existing local account.

  Rastrillo ships no grant verifier in this spec. Spec 3 ships one.

### 8. Clients

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

### 9. Transport

- **The official Go SDK**, `github.com/modelcontextprotocol/go-sdk`, at
  v1.8.0, the version Tito Go runs. The SDK handles protocol version
  negotiation; the tests pin the versions it answers.
- **Streamable HTTP, stateless, with plain JSON responses.** There are no
  sessions and no SSE streams. A CARLOS instance hibernates, and an open
  stream would hold it awake. Each request builds its own server, so no
  state outlives the request (`T/internal/instance/api_mcp.go:82`).
- **Request body cap:** `Gate.MaxBody`, default 1 MiB, the API's.
- **Dependencies:** this adds the SDK and `google/jsonschema-go`, which the
  SDK uses to validate. They must pass govulncheck and the size and
  dependency gates. `chromedp-graph` shows they stay out of the browser
  test graph.

### 10. The generator and the scaffold

- **The generator** sets section 3's flags on every generated descriptor
  and emits `gen.ExposedOperations()`. `api/v1.json` gains the three flags
  per operation. A flag changing from true to false is not a breaking
  change; `Destructive` changing from false to true is noted, not refused.
- **The scaffold** mounts `/api/v1/mcp` beside the API tree, with the same
  gate and policy, and its `README` gives the Claude Code line.

### 11. Testing

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
  door gives the same rows and refusal kinds.
- **Exact numbers:** a 2^53 + 1 integer in and out. **Bad bytes:** invalid
  UTF-8 and a lone surrogate are refused.
- **The gate:** an unknown token, a revoked token between list and call,
  a malformed agent header, and a panic, each answering the way the API
  tree does.
- **Grant facts:** a fake verifier sets `Grant`, and an `Authorize` that
  checks a scope hides the tool from the list and refuses the call.
- **The Oficina interop fixture.** Memoria's `notes_create` and
  `notes_search`, from Oficina's contract, rebuilt as hand-written
  descriptors in a test. They run through `mcp.New` with a fake
  connection-service verifier. This proves Oficina can move its Memoria
  integration onto `mcp.New` without changing its operations (#116).
- **Fail-first** as `CONTRIBUTING.md` requires.

### 12. Docs

- `docs/site/mcp.md`, new: mounting, connecting from Claude Code and
  Codex, writing descriptors by hand, the flags, the instructions, and
  the trust boundaries in plain words.
- `docs/site/api.md`: a section pointing at MCP, and `Gate`.
- `skill/api.md`: the MCP lines.
- The reference pages for `mcp`, the changes in `api` and `op`, and the
  CHANGELOG.

All new user-facing copy goes through Paul's copy review.

## Left to the builder

- How `Gate` is factored out of the tree, as long as section 2's behaviour
  holds and the tree's tests pass unchanged.
- The exact wording of each tool error, using the API's existing messages
  wherever they fit.
- How the SDK's per-request server is wired, within section 9.

## Open for Paul

1. **The trust boundaries.** Approve the two-deployment shape: an app as
   its own connection service, or a separate service several apps trust.
   Spec 3 builds both on it.
2. **The package name `mcp`.** It collides in name with the SDK's own
   package, which an app would only notice if it imports both.
3. **The endpoint path `/api/v1/mcp`.** Tito Go uses `/<account>/api/mcp`.
