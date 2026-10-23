# JSON API, tokens and agents

Every manifest resource can be JSON at `/api/v1/<name>`, called with a
personal token. Each action is one operation in `gen/ops/<name>/`
(`List`, `Get`, `Create`, `Update`, `Delete`); the page action in
`gen/actions/` and the API handler in `gen/api/<name>/doors.go` both
call it, and it begins with `op.Check`. Rules live in the operation
and the policy, never in a door. docs/site/api.md

**Wiring** (a new app has it in `internal/<app>/api.go`):
`tokens, err := apitoken.New(apitoken.Config{DB: d.Writer(), Prefix: tokenPrefix})`;
`api.New(api.Config{Verify: tokens, Ctx: ctxFactory, Authorize: authorize, Logger: logger, Redact: tokens.Redact}, gen.APIRoutes()...)`;
`mux.Handle("/api/v1", t)` and `mux.Handle("/api/v1/", t)` on the
outer `*http.ServeMux`, beside the chi router, never inside it:
`csrf.Protect` refuses a write with no browser headers, and
`csrf.Exempt` matches exact paths only and still runs sessions. Add
`apitoken.Schema` to `BootSchema`. `api.New` errors at boot without
`Verify`, `Ctx` or `Authorize`.

**One policy for both doors.** Pass the same `authorize` as
`api.Config.Authorize` and as `Ctx.Authorize` in the page ctx factory;
generated page actions call it too. `op.Check` order: a read-only token
on a `Write` op is `op.Forbidden`; a scoped op with empty `Subject` is
`op.Forbidden`; then `Ctx.Authorize` (nil means `op.DefaultPolicy`).
`nil` allows; refuse with `op.Forbidden{Reason}` (logged, never sent)
or `op.NotFound` (403 or 404 on both doors). The error comes back
unchanged, so the API maps any refusal it knows (`op.Conflict` is 409)
and answers 500 only for an error it does not recognise. `op.DefaultPolicy`
allows scoped ops (the owner filter limits rows), allows unscoped ops
for sessions, anonymous callers and the app, and **refuses every token
on an unscoped op, even with `expose = true`**. The API cannot see
page middleware: put role, suspension and admin rules in `authorize`,
matching `o.ID` (`"ticket_types.create"`), `o.Resource`, `o.Access`,
`a.IsToken()`, `a.Agent`, and end with `return op.DefaultPolicy(ctx, a, o)`.

**Actor.** `rastrillo.Actor{Subject, Agent, Credential, Access,
Session, Harness}`; `Human` and `Name` are gone. Zero value = the app.
Pages: `sessions.ActorFrom(r)` (Write access), filled by the generated
router when the factory leaves `Actor` zero. API: `Subject`, `Agent`,
`Credential`, `Access` come from the verified token, never the
request. `String()` is what `eventlog` stores: `app`, `user:<s>`,
`user:<s> token:<id>`, `agent:<a> for user:<s> token:<id>`; old rows
say `human` or `agent:<name>`, so never compare with `"human"`.
`LogAttrs()` for `slog`. On a page, `{{actor $L .Actor}}` or
`rastrillo.ActorPhrase` writes "Claude for Paul"; set `ui.ActorName`
to map a subject to a name.

**Tokens** (`apitoken`): `<prefix>_<12-char public id>_<43-char
secret>`, base62; only the secret's SHA-256 is stored; the public ID
is safe to log. Never change the prefix: every token stops working.
`Store.Mint(ctx, subject, MintInput{Agent, ForSelf, Access, TTL})`: an
agent name is 1 to 40 letters, digits, spaces, hyphens, dots
(`ValidAgentName`); `ForSelf` makes a token with no agent. The token
page is `apitoken.NewHandlers`; `Render` calls `ui`'s `api-tokens`
partial with `PageData` as `Page` and the path as `Path` (a new app
ships `tokens.go` and `templates/tokens.html`, mounted once it has
sign-in); `PageData` carries each row's `Revoked`/`Expired` and the
live `Agents`. Mount in the
signed-in group behind `csrf.Protect`; `Create` also behind
`sess.RequireFresh(10*time.Minute)`; revokes not. The subject comes
from the session, never the form. Show the secret only in the create
response, never in a URL, redirect, flash or session.

**Calling.** `Authorization: Bearer <token>` only (no cookie, no query
string). An agent's token may send `Agent-Session` and
`Agent-Harness`: 200 bytes or fewer, no control characters, else 422;
ignored on a person's own token. `RequireAgentSession` refuses an agent
write without `Agent-Session`. Each request that reaches a handler logs
`api: request` with method, path, actor, `agent_session`,
`agent_harness`.

**Wire.** No envelope. Names are snake_case (`api.FieldName`); IDs
are numbers; times RFC 3339 UTC strings. Money is
`{"amount": 1490, "currency": "USD"}` in minor units, `null` when an
optional one is unset or to clear it; `currency` may be omitted on
input; an update with another currency is 409. A list is
`{"<name>": [...], "next_after": "<cursor>" | null}`, newest first;
`?limit=` 1 to 200 (default 50), `?after=<next_after>`, `?q=` with
search, one parameter per filter by wire name. POST is 201 with
`Location`; PATCH changes only the members sent (`{}` writes nothing);
DELETE is 204 and takes no body. A body needs
`Content-Type: application/json` (415) and fits 1 MiB (413); an
unknown or repeated member, or `null` elsewhere, is 422. Errors are
`{"error": {"code", "message", "fields", "ref"}}`; branch on `code`:
unauthorized 401, forbidden 403, not_found 404 (another person's row
too), method_not_allowed 405, conflict 409, too_large 413,
unsupported_media_type 415, invalid 422, rate_limited 429, internal
500, unavailable 503. A handler you write uses `api.Decode`,
`api.Write`, `api.Refuse`.

**Exposing.** `scope = "user"` resources are on the API; unscoped are
not. Override in the manifest with `[api]` then `expose = true` or
`false`. A filter whose wire name is `q`, `after` or `limit` is refused.

**The contract.** `rastrillo generate` writes `api/v1.json` for every
app with `manifest/` (empty when nothing is exposed): commit it with
`gen/`. An addition updates it. A break (a route or field removed, an
argument newly required, a kind changed, read now needing write) fails
`generate` and leaves the file; `rastrillo generate -accept-breaking`
rewrites it on purpose. `generate --check` fails when it is missing or
stale.

**A hand-written operation.** Copy `gen/ops/<name>/<op>.go` to
`ops/<name>/<op>.go` and put `//go:build rastrillo_actions` and a blank
line above the package clause. Only `list`, `get`, `create`, `update`,
`delete` can be replaced; `types.go` stays generated. Every exported
function must begin with exactly this, or `generate --check` fails:

```go
if err := op.Check(ctx, rc, CreateOp); err != nil {
	return Bookmark{}, err
}
```

No package-level `var` whose value calls a function. Delete the file
to get the generated one back.

**A hand-written action** over a door of an exposed resource must call
its operation at the top level of `Handle` (or of a same-file function
`Handle` calls there) and use the error: `index.GET` `List`;
`[id]/index.GET`, `[id]/edit.GET`, `[id]/delete.GET` `Get`;
`index.POST` `Create`; `[id]/edit-basics.POST`,
`[id]/edit-advanced.POST` `Update`; `[id]/delete.POST` `Delete`. No API
twin: `//rastrillo:page-only <reason>` above the package clause. The
lint misses writes after the operation and handlers outside
`actions/`, which is why rules go in `authorize`.

**Parity test.** Per exposed resource, `gen/paritytest` has
`Run<Plural>(t, f)`, which drives one scenario through both doors and
compares stored rows and refusal kinds; the required-field names of an
empty create are checked in the API's 422 only (a page shows no names);
the update step is skipped when no form field is non-money.
Implement `paritytest.Fixture` (`New`, `SignIn`, `Token`, `Prepare`,
`ValidCreate`) in the app's test package and call it.
`gen.Operations()` lists every operation's `op.Descriptor`.
