# Copy for review: the JSON API

Status: English approved by Paul on 2026-10-22. Translations are drafted after approval, in the base catalogs' register.

Every user-facing string this branch adds, with where it lives. Later tasks append here.

## Reference docs (Task 1)

Prose added or changed in the reference and guide pages. The code blocks beside it change too (the `Ctx.Authorize` field, `Access`, `Operation`, the new `Actor` fields and methods) but are signatures, not copy.

### docs/site/reference/rastrillo.md:139-143 (Ctx and RenderFunc)

Changed. The second sentence is new. The old text said per-request state does not live on `Ctx`, which stopped being true when `Ctx.Actor` began to carry the subject and credential.

> Passed to every generated action: your own wiring, built once by your
> ctx factory. `Authorize` is the app's one policy for who may do what;
> nil means the default policy. `Actor` is the one per-request value: the
> router copies it from the request context (`WithActor`). The signed-in
> user is `sessions.Current(r)` and locale is `LocaleFrom(r)`.

### docs/site/reference/rastrillo.md:239-241 (Agents)

Moved above the `Access` block; the `Write` sentence is reworded from the `ToolWrite` one.

> An action opts in with `var Tool = rastrillo.Tool{...}`. A `Write` tool
> changes state, so it requires a confirm sentence and an explicitly
> confirmed call.

### docs/site/reference/rastrillo.md:256-259 (Agents)

New. Replaces "`ToolRead` observes and never changes state. ... `Access.String` is `"read"` or `"write"`."

> `Read` observes and never changes state. `Write` changes state. The zero
> `Access` is invalid: set one explicitly. `Access.String` is `"read"`,
> `"write"` or `"invalid"`, and `Access.Valid` is true for `Read` and
> `Write`. An `Operation` is what `Ctx.Authorize` is asked about.

### docs/site/reference/rastrillo.md:274-285 (Agents)

Changed. The first sentence is unchanged. The rest replaces the old `"human"` / `"agent:<name>"` description.

> Every action's caller is attributed, never anonymous, so an audit trail
> can say who did what. `Subject` is the person, `Agent` is the agent
> acting for them (empty when they act directly), and `Credential` is the
> token's public ID (empty for a browser session). The zero `Actor` is the
> app itself. `Actor.String` is the audit form: `app`, `user:<subject>`,
> `user:<subject> token:<id>` or `agent:<name> for user:<subject>
> token:<id>`. It is what `eventlog` stores on every appended event, so a
> stream says who did what without importing this package.
> `Actor.LogAttrs` returns the same as `slog` attributes, plus
> `agent_session` and `agent_harness` when the client sent them.
> `Actor.IsToken` is true when `Credential` is set.

### docs/site/agents.md:24, 26-27 (Read and write are treated differently)

Rename only: `rastrillo.ToolRead` to `rastrillo.Read`, `rastrillo.ToolWrite` to `rastrillo.Write` (also in the code sample at line 15). Prose otherwise unchanged. This page is the Tool registry guide that Task 2 removes.

> `rastrillo.Read` observes and never changes state.
>
> `rastrillo.Write` changes state, so it requires a `Confirm` sentence
> and an explicitly confirmed call (rest unchanged).

### docs/site/reference/tools.md:46 (errors table)

Rename only.

> | `ErrConfirmRequired` | a `Write` call arrived without confirmation |

## Task 2

The tool registry is removed. These Task 1 entries above no longer apply, because the text they describe is gone: the `docs/site/reference/rastrillo.md:239-241` Tool sentence, the `docs/site/agents.md:24, 26-27` rename, and the `docs/site/reference/tools.md:46` rename.

### docs/site/agents.md:1-4 (intro)

Replaces the whole page above `## The sidecar`, and the title "Agents and tools". Task 23 rewrites this page. The sidecar section is unchanged.

> # 🤖 Agents
>
> Operations replace the tool registry. `rastrillo.Tool`, `gen.Tools()` and
> the `tools` package are gone: remove any `var Tool` from your actions.

### docs/site/nav.json:108-109 (Agents entry)

Label "Agents & tools" becomes "Agents". Blurb changed.

> Operations in place of the tool registry, and the sidecar loop.

Links in `docs/site/reference/rastrillo.md:80` and `:277` that read "Agents and tools" now read "Agents".

### README.md:372-374 (Agents bullet)

Shortened to the sidecar half.

> - **Agents** — `Options.Sidecar` + the `sidecar run` argv speak the
>   platform's sidecar contract, and `Options.NextDue` answers the
>   activator's `GET /api/next-due` scheduled-wake poll.

### CHANGELOG.md:13-17 (Unreleased)

New entry.

> ### Removed: the agent tool registry; delete any `var Tool` from your actions and regenerate
>
> `rastrillo.Tool`, `rastrillo.ToolDef`, `gen.Tools()` and the `tools` package are gone. No app used them, and a call through `tools.Dispatch` was refused by `csrf.Protect`, because it sent a POST with no `Origin`. Operations replace the registry.
>
> To upgrade, delete any `var Tool = rastrillo.Tool{...}` from your actions and run `rastrillo generate`, which removes the old `gen/tools.go`. `rastrillo generate --check` no longer counts tools.

### cmd/rastrillo/generate.go:187 (CLI output)

`rastrillo generate --check` success line loses its tool count.

> rastrillo generate --check: N route(s), actions tagged, locale catalogs complete, icon slugs resolve

## Task 3

The `op` package is new, so its reference page is new. Every sentence of prose on it is listed here; the code blocks are signatures, not copy.

### docs/site/nav.json (Reference entry for op)

New. Label "op". Blurb:

> The refusals an operation returns, inputs that remember what was sent, and the descriptor for any transport.

### docs/site/reference/op.md (intro)

> What operations and both of their doors share: the refusals an operation
> returns, the input types that remember whether a field was sent, and the
> descriptor that names an operation for any transport.

### docs/site/reference/op.md (Refusals)

> An operation returns one of these to refuse a call. All four are values,
> so match them with `errors.As`:
>
> `Invalid.Fields` is keyed by manifest field name, such as `Title`. The
> page door puts each message beside its field, and the API door turns the
> key into its wire name. `NotFound` is the answer both for a row that does
> not exist and for one that is not the caller's. `Forbidden.Reason` is for
> your logs and never reaches the response. `Conflict.Message` is safe to
> show.
>
> `Invalid.Error`, `NotFound.Error`, `Forbidden.Error` and `Conflict.Error`
> are for log lines: `invalid: Amount, Title` (the field names, sorted,
> never the messages), `not found`, `forbidden: <reason>` and
> `conflict: <message>`.

The code sample's comment reads "inv.Fields names what was wrong".

### docs/site/reference/op.md (Opt and Some)

> `Opt` is a field a request may or may not carry. After decoding, `Set` is
> true when the member was sent, whatever its value, so an explicit `0` is
> present and a missing member is not. Use it for every field of a create
> or update input: an update then changes only the fields the client sent,
> and `{}` changes nothing.
>
> `Opt.UnmarshalJSONFrom` refuses `null`. No manifest field is nullable, so
> a `null` is a client bug and is reported on that field.
> `Opt.MarshalJSONTo` writes the value, or `null` when unset. `Some` makes a
> present `Opt` in Go code.

### docs/site/reference/op.md (Money and Nullable)

> `Money` is an amount on the wire: `amount` in the currency's minor unit
> and `currency` as an ISO 4217 code. Leave `currency` out of a request to
> use the manifest's default on a create, or to keep the stored currency on
> an update. The operation checks the code; this package does not.
>
> A required money field is `Opt[Money]`. An optional one is
> `Nullable[Money]`, which is `Opt` plus a `Null` flag. A JSON `null` sets
> both `Set` and `Null`: the member was sent, and it asks to clear the
> field. `Nullable.UnmarshalJSONFrom` is the only decoder here that accepts
> `null`. `Nullable.MarshalJSONTo` writes `null` when unset or `Null`, and
> the value otherwise.

### docs/site/reference/op.md (Descriptor)

> `Descriptor` names one operation for any transport. The embedded
> `rastrillo.Operation` gives its ID, resource, scope and access. `Exposed`
> says whether the resource is on the API. `Confirm` is the sentence an
> agent shows before a write. `Input` and `Result` are JSON Schema, and
> both are required. `Invoke` decodes `input` with the API door's rules and
> calls the operation.

### op/opt_json.go:13 (decode error text)

The message a client sees when it sends `null` for a field that cannot take it. The API door (Task 7) wraps it in the 422 response's `fields` map.

> null is not allowed

## Task 4

Reference prose for `op.Check`, `op.DefaultPolicy` and `sessions.ActorFrom`, and one changed paragraph about where `Ctx.Actor` comes from. The code blocks beside them are signatures, not copy. The `Forbidden.Reason` strings in `op/check.go` go to logs only and are not listed.

### docs/site/reference/op.md (Check and DefaultPolicy)

New section.

> `Check` is the one place both doors ask who may do what. Call it first in
> any operation you write by hand. It asks, in order:
>
> 1. A read-only token on a `Write` operation is refused.
> 2. A scoped operation with no `Subject` is refused.
> 3. If `rc.Authorize` is set, its error is the answer. Return `nil` to
>    allow, or `Forbidden` or `NotFound` to refuse. Any other error is a 500.
> 4. Otherwise `DefaultPolicy` decides.
>
> The first two run before your policy, so no policy can let a read-only
> token write or let a caller with no subject reach a scoped row.
>
> `DefaultPolicy` allows a scoped operation, because the owner filter keeps
> the caller to their own rows. It allows an unscoped operation for a
> signed-in person, an anonymous visitor and the app. It refuses an
> unscoped operation for a token, because your pages may guard those tables
> with middleware the API cannot see. To let tokens in, say so in your
> `Authorize`, and call `DefaultPolicy` from it for everything else.

### docs/site/reference/sessions.md (Reading the viewer)

New paragraph.

> `ActorFrom` is the signed-in person as a `rastrillo.Actor`, with `Write`
> access. The generated router uses it when your ctx factory leaves `Actor`
> empty. `ok` is false with no session, or with a session that has no
> `Subject`.

### docs/site/reference/rastrillo.md (Ctx)

Changed. Was: "`Actor` is the one per-request value: the router copies it from the request context (`WithActor`)."

> `Actor` is the one per-request value. The router copies it from the
> request context (`WithActor`). If there is none and your factory left
> `Actor` empty, the router uses the signed-in person
> (`sessions.ActorFrom`).

## Task 5: API error messages

The `api` package's error envelope. Each message is the `message` member of a refusal, keyed by its `code`. The spec fixes them (section 6); this is where they are reviewed. Log lines (`api: internal error`, `api: forbidden`) are for operators and are not listed.

### api/errors.go (codes table)

| Code | Message |
|---|---|
| `unauthorized` | Send a valid API token in the Authorization header. |
| `forbidden` | This token can't do that. |
| `not_found` | Nothing here. |
| `method_not_allowed` | This address doesn't accept that method. |
| `conflict` | (the operation's own `Conflict.Message`) |
| `too_large` | The request body is too large. |
| `unsupported_media_type` | Send the body as JSON, with Content-Type: application/json. |
| `invalid` | Check the fields and try again. |
| `rate_limited` | (none here; a `Config.Before` limiter writes its own) |
| `internal` | Something went wrong. Quote the reference if you report it. |
| `unavailable` | Try again in a moment. |

### docs/site/nav.json (reference/api blurb)

> The JSON door: one error shape with a machine code, and how a handler answers.

### docs/site/reference/api.md (intro)

> The JSON door. This page covers the error shape and the functions a
> hand-written handler uses to answer the way generated ones do.

### docs/site/reference/api.md (Errors)

> Every refusal is `{"error": {...}}` with this body. Branch on `Code`,
> not on `Message`: each code maps to one status, and the prose may change.

Table "When" column, one per code:

> the token is missing or does not work; the token may not do this; no such row, or not the caller's; no such path; a known path, wrong method; the write lost a race; the body is over the cap; a write body without `Content-Type: application/json`; a field was refused; `fields` says which; a `Config.Before` limiter said no; something else went wrong; `ref` is in the log; the token store could not be asked

> `Fields` is keyed by wire name, such as `title`. `Ref` is set only on a
> 500. Quote it when you report the failure: the log line carries the same
> one.

### docs/site/reference/api.md (Write, WriteError and Refuse)

> `Write` answers with `v` as JSON, with `Content-Type: application/json`
> and `Cache-Control: no-store`. An empty list encodes as `[]`, never
> `null`. `WriteError` puts `e` under `"error"` and writes it. Use it for a
> code `Refuse` cannot derive from an error, such as `rate_limited` from a
> rate limiter.
>
> `Refuse` answers an error. An `op` refusal gets its code and status:
> `op.Invalid` is 422 with its fields re-keyed by `FieldName`,
> `op.NotFound` 404, `op.Forbidden` 403, `op.Conflict` 409 with
> `Conflict.Message` as the message. `ErrUnauthorized` is 401 with
> `WWW-Authenticate: Bearer`, and `ErrUnavailable` is 503. Any other error
> is a 500: the error text goes to the log under a fresh ref, and the
> response carries only the ref. `op.Forbidden.Reason` is logged and never
> sent.
>
> A `Verifier` wraps one of these when it cannot name an actor. Keep them
> distinct: a client retries a 503 and gives up on a 401.

### docs/site/reference/api.md (FieldName)

> The wire name of a manifest field: `Title` is `title`, `MaxPerOrder` is
> `max_per_order`, `IPAddress` is `ip_address`. Use it when you build an
> `op.Invalid` by hand and want to know what the client will see.

### docs/site/reference/api.md (WithLogger and WithRedact)

> `Refuse` reads its logger and redactor from the request. The tree sets
> both from `Config` on every request; set them yourself only in a test
> that reads the log line. Without them, `Refuse` logs to `slog.Default()`
> and redacts nothing.

## Task 6: API error messages (request bodies)

The 422 messages `api.Decode` and `api.NoBody` send. A refusal tied to one member puts its message in `fields` under the member's name as sent, with the table's "Check the fields and try again." as the top-level `message`. A refusal of the body as a whole has no `fields`, so its top-level `message` says what to send. The 413 and 415 answers use the Task 5 table.

### api/decode.go (top-level messages, no fields)

| When | Message |
|---|---|
| A DELETE (or any no-input route) sent a body | Send this request with no body. |
| Bad JSON syntax, an empty body, a value that is not an object, or anything after the object | Send one JSON object as the body. |

### api/decode.go (`fields` messages)

| When | Message |
|---|---|
| A member the input does not have, including a wrong-case name | Unknown field. Check the spelling and case. |
| `null` for anything but optional money | Send a value, or leave this field out. |
| The same member twice | Send this field once. |
| A problem inside an object or array member, such as `{"price":{"amount":"1"}}` (keyed by the outer member) | Check the values inside this field. |
| Wrong type, string wanted | Send a string. |
| Wrong type, or a fraction or out-of-range number, where an integer is wanted | Send a whole number. |
| Wrong type, number wanted | Send a number. |
| Wrong type, boolean wanted | Send true or false. |
| Wrong type, object wanted | Send an object. |
| Wrong type, array wanted | Send an array. |
| A value the field's type refused for its own reasons, such as a time that does not parse | Check this value. |

### docs/site/reference/api.md (Errors table)

Changed. The `invalid` row's "When" was "a field was refused; `fields` says which".

> a field was refused, and `fields` says which; or the body is not one JSON object

### docs/site/reference/api.md (Decode, NoBody and WithMaxBody)

New section.

> `Decode` reads the request body into `dst`, which must be a non-nil
> pointer. If it returns false, it has already answered: return at once
> and do not read `dst`. It checks, in this order:
>
> 1. `Content-Type` must be `application/json`, or the answer is 415. A
>    `charset` parameter is fine.
> 2. The body must fit the cap, or the answer is 413. The whole body is
>    read first, so a body that is too large is 413 even if it is also
>    broken.
> 3. The body must be one JSON object, or the answer is 422 with no
>    `fields`.
> 4. Each member must be one `dst` has, in the same case, sent once, of
>    the right type, and not `null`. If not, the answer is 422, and
>    `fields` names the member.
>
> `null` is allowed only for an `op.Nullable` field, such as optional
> money, where it clears the value. It is refused everywhere else,
> including inside a `Nullable` value.
>
> Call `NoBody` first in a handler that takes no input, such as a DELETE.
> Any body at all is 422, whatever its `Content-Type`.
>
> The cap is 1 MiB. The tree sets it from `Config.MaxBody` with
> `WithMaxBody`. Call `WithMaxBody` yourself only outside the tree or in a
> test. An `n` of 0 or less keeps 1 MiB.

### docs/site/reference/op.md (Money and Nullable)

New paragraph after the `Nullable` one.

> `Nullable.IsNullable` is always true. It is the marker `api.Decode` reads
> to accept `null` for a field. `Opt` does not have it.

## Task 7: the tree

The 422 messages the tree sends for the agent headers. They are not body members, so the answer has no `fields` and the top-level `message` names the header. Every other answer the tree gives (404, 405, 401, 403, 413, 500) uses the Task 5 table. Log lines (`api: panic`) are for operators and are not listed.

### api/tree.go (top-level messages, no fields)

| When | Message |
|---|---|
| An agent token sent `Agent-Session` over 200 bytes or with a control character | Send Agent-Session as 200 bytes or fewer, with no control characters. |
| The same for `Agent-Harness` | Send Agent-Harness as 200 bytes or fewer, with no control characters. |
| `RequireAgentSession` is on and an agent token sent a write with no `Agent-Session` | Send an Agent-Session header with every write. |

### docs/site/reference/api.md (intro)

Changed. Was "The JSON door. This page covers the error shape and the functions a hand-written handler uses to answer the way generated ones do."

> The JSON door. This page covers the handler tree, the error shape, and
> the functions a hand-written handler uses to answer the way generated
> ones do.

### docs/site/reference/api.md (New)

New section.

> `New` builds the `/api/v1` handler. Mount it at both `/api/v1` and
> `/api/v1/`.
>
> `Verify`, `Ctx` and `Authorize` are required. To keep the default
> policy, pass `op.DefaultPolicy`. `New` returns an error if one is
> missing, or if a route:
>
> - has no `Op.ID`, `Op.Input`, `Op.Result` or `Handle`;
> - has an `Op.Access` that does not match its method: GET and HEAD must
>   be `Read`, and POST, PUT, PATCH and DELETE must be `Write`;
> - has the same method and pattern as another route.
>
> Every answer is JSON with `Cache-Control: no-store`. A request goes
> through these steps in order, and the first one that refuses answers:
>
> 1. Routing: 404 for an unknown path, and 405 with `Allow` for a known
>    path with the wrong method. A GET route also answers HEAD.
> 2. `Config.Before`, such as a rate limiter.
> 3. `Verify`: 401 or 503.
> 4. The agent headers: 422.
> 5. A read-only token on a `Write` route: 403.
> 6. `Config.After`, then the route's `Handle`.
>
> `Handle` gets the `Ctx` that `Config.Ctx` builds, with `Actor` set to
> the verified caller and `Authorize` set to `Config.Authorize`. `Pattern`
> is the full path, such as `/api/v1/bookmarks/{id}`, and `r.PathValue`
> works in `Handle`.
>
> A `Verifier` names the caller. When it cannot, wrap `ErrUnauthorized` or
> `ErrUnavailable` in the error it returns. The actor it returns must have
> a `Credential` and an `Access`, or the answer is 500.
>
> An agent may send `Agent-Session` and `Agent-Harness`. They are read only
> for an agent's token, and copied to `Actor.Session` and `Actor.Harness`.
> Each must be 200 bytes or fewer, with no control characters, or the
> answer is 422. Set `RequireAgentSession` to refuse a write from an agent
> token that sends no `Agent-Session`.
>
> If a handler panics before writing, the answer is 500 with a `ref`. If
> it panics after, the connection is closed. Either way the panic is
> logged to `Config.Logger` under the ref, after `Config.Redact`.

## Task 11: Manifest validation messages

### manifest.go (msgFilterParam, a `Resource.Validate` error)

New. Shown when `rastrillo generate` loads a manifest whose filter (in `filter` or `filters`) is on a field whose wire name is `q`, `after` or `limit`. `internal/manifest.Load` prefixes it with the file, as it does every `Validate` error. The two values are the field as declared and its wire name.

> filter: "Limit" would be the query parameter "limit", which API lists use for search and paging; rename the field

The `[api]` table's other errors (an unknown key, a non-boolean `expose`) come from the TOML decoder's existing messages and are not new copy.

### docs/site/reference/rastrillo.md (Manifest types)

New, after the `HasMoney` paragraph. The code block is signatures, not copy.

> `Resource.API` is the `[api]` table. `Exposed` reports whether the
> resource is on the JSON API. A `scope = "user"` resource is on it and an
> unscoped one is not, unless `API.Expose` says otherwise. Leave `Expose`
> nil to keep the default for the scope.
>
> `Resource.Validate` also refuses a filter on a field whose wire name is
> `q`, `after` or `limit`. API lists already use those query parameters
> for search and paging.

## Task 10: Actor phrase ("Claude for Paul")

### locales/en.toml (rastrillo.ui.agent_for)

The words shown wherever an agent acted for a person. `{agent}` is the token's agent name, `{person}` the person's name (or their subject when the app has no name for them). Plain text, no badge or emoji.

> {agent} for {person}

Rendered: "Claude for Paul".

### docs/site/reference/ui.md (actor)

New section.

> Writes who did something, in the reader's language: nothing for the
> app, `Paul` for a person, and `Claude for Paul` for an agent acting for
> them. It is plain text, with no badge or icon. Style it yourself.
>
> Set `ActorName` once at startup to turn a subject into a person's name.
> Without it, the page shows the subject. A template has no request, so
> `ActorName` is called with `context.Background()`. If your lookup needs
> the request's context, build the words in Go with
> `rastrillo.ActorPhrase` instead.

The Funcs paragraph now says "twenty-two" and lists `actor` with the locale functions; the localizer table gains a row, "see [actor](#actor)". `docs/site/templates.md` (Template functions) and `docs/site/migrate-templates.md` (What it changes, item 1) add `actor` to their lists of locale functions.

### docs/site/reference/rastrillo.md (Agents, after Actor)

New paragraph.

> `ActorPhrase` is the words a page shows for an actor: nothing for the
> app, the person's name, or `Claude for Paul` for an agent. `name` turns
> a subject into a name; if it is nil or finds no name, the page shows the
> subject. `t` is the page's translator, such as `Locales.Tf` for the
> reader's language. In a template, use `ui`'s `actor` function instead.

## Task 10: translated after copy review

Each of these carried the English `{agent} for {person}` as a placeholder for `rastrillo.ui.agent_for` until the English was approved. Drafted on 2026-10-22, unreviewed, in the base catalogs' register, with `{agent}` and `{person}` kept verbatim; `TestTheAgentAndTokenStringsAreInEveryCatalog` holds each to carrying both once.

- locales/ga.toml
- locales/zh-Hans.toml
- locales/es.toml
- locales/hi.toml
- locales/pt-PT.toml
- locales/bn.toml
- locales/ru.toml
- locales/ja.toml
- locales/yue.toml
- locales/vi.toml
- locales/ar.toml

## Task 12: Comments in generated files

Doc comments `internal/generate/mergeablestore.go` writes into every mergeable store, `gen/store/<name>/store.go`. `<Singular>` and `<Plural>` are the resource's names (`Announcement`, `Announcements`). Where a scoped (`scope = "user"`) or money store reads differently, both forms are given. The goldens under `internal/generate/testdata/mergeable/` show each in place. Comments not listed are unchanged.

### Package doc (changed)

Replaces the "Two recorded caveats, both deliberate" list.

> Every write takes its actor from the caller, rastrillo.Actor's
> String(), so each event says who wrote it. One recorded caveat,
> deliberate: record ids are writer-local. Create takes the next id and
> appends the record's first event in one transaction
> (eventlog.AppendNew), so two creates on one database never share an
> id; keeping ids apart across writers belongs to the platform
> transport design, and this allocator is the first thing it replaces.

### appActor (changed; was `actor`)

> appActor is what the per-group updates stamp; every other write takes
> its actor from the caller.

### errGone (new)

> errGone stops an append to a record that is missing or deleted. It is
> decided inside the append's transaction, on the history the event
> would land on, so a delete that commits first is seen.

Scoped: "a record that is missing, deleted or someone else's".

### payload (changed, last sentence)

> payload is the JSON shape of every created/updated event: pointer
> fields, omitted when nil, so a created event carries every field and
> an updated event only the fields it changes; an absent field leaves
> the derived value untouched.

Drafted from the plan's text with its em dash replaced by a semicolon.

### encode (new, money stores)

> encode is p as stored JSON with amounts merged in: a map, because an
> explicit null (a cleared field) is what a struct with omitempty cannot
> write.

### fold (new, money stores)

> fold replays history and then the event about to be appended through
> the money rules. An event the fold refuses (an amount without its
> currency, a changed currency) would otherwise be written for good,
> and every later read of the stream, and every boot, would fail on it.
> Callers run it inside AppendChecked, on the history as it is under the
> write lock: a history read earlier let a racing writer clear the field
> and refill it in another currency in between.

### appendLive (new)

> appendLive appends one event to record id's stream and reports 1, or
> 0 when the record is missing or deleted. Whether it is live is decided
> inside AppendChecked, on the history the event lands on, so a delete
> that commits first is seen and nothing is written after it.

Scoped: "0 when the record is missing, deleted or not owner's". Money: "Whether it is live, and whether the money rules accept the event, are decided inside AppendChecked, on the history the event lands on, so a delete or a currency change that commits first is seen and nothing is written after it." A scoped store with money combines the two.

### List\<Plural\>After (new)

> List\<Plural\>After is the page after the row (AfterCreatedAt, AfterID)
> in List's order; an empty AfterCreatedAt starts at the top. The cursor
> carries that row's own values, so it still works after the row is
> deleted.

### Create\<Singular\> (new)

> Create\<Singular\> starts a record. eventlog.AppendNew takes its id and
> appends its first event in one transaction, so two creates at once
> make two records.

Scoped ends "make two records, each with its own owner." A money store also has, inside the method:

> A new record has no history, so its first event is folded alone,
> before it is written.

### Update\<Singular\>Params (new)

> Update\<Singular\>Params is a partial update: a nil field is left as it
> is, so the event carries only the fields that were sent.

On each money field `<F>`:

> \<F\>Set says \<F\> is part of the update: with both pointers
> nil it clears the amount, which an absent field cannot say.

### Update\<Singular\> (new)

> Update\<Singular\> appends one "updated" event and reports 1, or 0 when
> the record is missing or deleted.

Scoped: "missing, deleted or someone else's". A money store adds: "An amount in a currency other than the one its field holds is refused with replay.ErrCurrencyChange."

### Delete\<Singular\> (new doc)

> Delete\<Singular\> appends a tombstone and reports 1, or 0 when the
> record is missing or already deleted; derive skips dead ids.

Scoped: "missing, already deleted or someone else's".

### docs/site/reference/eventlog.md (Open, Append, Events)

`AppendNew` joins the signature block. The `AppendChecked` paragraph's last sentence changes from "Generated stores with money append this way." to:

> Generated stores update and delete records this way.

New paragraph after it:

> Use `AppendNew` to start a record. It takes the next id under
> `streamPrefix` (the largest number any stream there ends in, plus one),
> appends the first event to `streamPrefix` plus that id, and returns the
> id. Two calls at once on one database get different ids. A stream under
> the prefix that does not end in a number is an error. Generated stores
> create records this way.

## Task 8: apitoken

The `apitoken` reference page and its nav entry. The code blocks on the page are signatures, not copy. The package's Go errors (`rastrillo/apitoken: Mint needs an agent name ...`) are for the developer wiring it, like log lines, and are not listed; the token page (a later task) writes its own form messages.

### docs/site/nav.json (reference/apitoken blurb)

> Personal API tokens for agents: make, verify, list and revoke them.

### docs/site/reference/apitoken.md (New page)

> Personal API tokens. A person makes a token for an agent, such as
> Claude, or for their own script. The agent sends it on every API call,
> and the call acts as that person.

#### The token

> A token has three parts: the app's prefix, a public ID and a secret.
> The public ID is safe to log and to show. The secret is shown once,
> when the token is made. The database stores only its SHA-256.

#### New, Config and Schema

> `DB` and `Prefix` are required. `DerivePrefix` makes a prefix from your
> app name. Do not change the prefix later: every token already made
> stops working.
>
> Set `Allowed` to refuse tokens whose owner can no longer use the app,
> such as a suspended account. With no `Allowed`, every owner is allowed.
>
> Merge `Schema` into your boot set. It adds one table, `api_tokens`.

#### Making and managing tokens

> Show the secret `Mint` returns once, in the response to the form that
> made it. Never put it in a URL, a redirect, a flash message or the
> session.
>
> Name the agent in `Agent`. An agent name is 1 to 40 letters, digits,
> spaces, hyphens and dots. `ValidAgentName` checks one. For a token that
> is not for an agent, set `ForSelf` and leave `Agent` empty.
>
> Take `subject` from `sessions.Current`, never from the form. Every
> method only reads or changes that person's tokens. `Revoke` returns
> `sql.ErrNoRows` for a token that is not theirs: answer 404.
>
> `List` returns revoked and expired tokens too, newest first.

#### Verify and Redact

> Pass the `Store` as `api.Config.Verify`, and `Store.Redact` as
> `api.Config.Redact`.
>
> `Verify` reads only `Authorization: Bearer <token>`. A missing, wrong,
> revoked or expired token gets the same 401. So does an owner `Allowed`
> refuses. A database error, or an error from `Allowed`, is a 503.
> It records when each token was last used, at most once a minute.
>
> `Redact` replaces each token's secret in a log line with `[redacted]`.
> The prefix and public ID stay, so the log still shows which token it
> was.

## Task 9: Token page

The page's strings, as catalog keys in `locales/en.toml`. The page itself ships later (Tasks 21 and 22 write the template for `examples/notes` and the scaffold; the `ui` partial is deferred until after this review): the `apitoken` handlers hand the app the data, and the app's template reads these keys. Each line is one key; the words after the key are what the reader sees.

### locales/en.toml (page)

> rastrillo.ui.api_tokens_title: API tokens

> rastrillo.ui.api_tokens_intro: A token lets an agent or a script use this app as you. Anyone with the token can do what it allows.

> rastrillo.ui.api_tokens_empty: No tokens yet.

### locales/en.toml (the create form)

> rastrillo.ui.api_tokens_agent: Agent name

> rastrillo.ui.api_tokens_for_self: This token is for me, not an agent

> rastrillo.ui.api_tokens_access: Access

> rastrillo.ui.api_tokens_access_write: Read and write

> rastrillo.ui.api_tokens_access_read: Read only

> rastrillo.ui.api_tokens_expires: Expires

> rastrillo.ui.api_tokens_expiry_30: 30 days

> rastrillo.ui.api_tokens_expiry_90: 90 days

> rastrillo.ui.api_tokens_expiry_365: 1 year

> rastrillo.ui.api_tokens_expiry_never: Never

> rastrillo.ui.api_tokens_create: Create token

"Read and write" and "90 days" are preselected. `api_tokens_access` and `api_tokens_expires` are also the list's column headings.

### locales/en.toml (the one-time reveal)

Shown once, in the response to the create form, above the whole token.

> rastrillo.ui.api_tokens_reveal: Copy this token now. It won't be shown again.

### locales/en.toml (the list)

Column headings. The first column shows who the token is for, as the `actor` phrase ("Claude for Paul"), then the public ID.

> rastrillo.ui.api_tokens_col_token: Token

> rastrillo.ui.api_tokens_col_created: Created

> rastrillo.ui.api_tokens_col_last_used: Last used

Buttons: one per row, one per agent name, one for the page.

> rastrillo.ui.api_tokens_revoke: Revoke

> rastrillo.ui.api_tokens_revoke_agent: Revoke all for this agent

> rastrillo.ui.api_tokens_revoke_all: Revoke all tokens

### locales/en.toml (form error)

Under the agent name field, when it is empty and the box is not ticked. The handlers translate it and pass it to the template as `Errors.agent`.

> rastrillo.ui.api_tokens_error_agent: Give the agent a name, or tick the box if the token is for you.

### docs/site/reference/apitoken.md (The token page)

New section, between "Making and managing tokens" and "Verify and Redact". The code block is signatures, not copy.

> The handlers take the signed-in person from `sessions.Current` and
> answer 403 without one. They set `Cache-Control: no-store` on every
> response. Mount them under your session router, behind CSRF, and mount
> `Create` behind `sessions.RequireFresh(10 * time.Minute)` as well.
>
> `Render` draws the page: there is no partial yet. `Row.Display` is the
> `actor` phrase for the token, in the reader's language. Set `Name` to
> turn a subject into a person's name; without it, the page shows the
> subject.
>
> `Create` reads the form fields `agent`, `for_self`, `access` (`read` or
> `write`) and `expiry` (`30`, `90`, `365` or `never`). It answers 200
> with `Created` set, in that response only. An empty agent name without
> `for_self` answers 422 with `Errors["agent"]` and the form echoed. A
> value the form never offers answers 400.
>
> The revokes redirect to `Path` with 303. `Revoke` answers 404 for a
> token that is not the caller's.

### Not listed

The handlers' own `http.Error` texts ("signed out", "bad form", "method not allowed", "access must be read or write", "expiry must be 30, 90, 365 or never", "agent must be a name", "could not create the token", "could not revoke the token", "could not revoke the tokens", "could not list the tokens") answer a tampered form or a store failure in plain text, like `jobs` and `password` do, and never reach a person using the page as built.

## Task 9: translated after copy review

Each of these carried the English of all twenty-two `rastrillo.ui.api_tokens_*` keys above as a placeholder until the English was approved. Drafted on 2026-10-22, unreviewed, in the base catalogs' register. "Token" stays "Token" in Spanish and Portuguese, which `TestTheAgentAndTokenStringsAreInEveryCatalog` lists by name.

- locales/ga.toml
- locales/zh-Hans.toml
- locales/es.toml
- locales/hi.toml
- locales/pt-PT.toml
- locales/bn.toml
- locales/ru.toml
- locales/ja.toml
- locales/yue.toml
- locales/vi.toml
- locales/ar.toml

## Task 13: Comments in generated files

Task 13 adds three queries to every exclusive store's `gen/store/<name>/queries.sql` and changes one. None of them carries prose: the only new lines a reader sees are sqlc's name directives, which are code, not copy. They are listed so the reviewer can see the names an app's developer will meet in `queries.sql.go`. `<Singular>` and `<Plural>` are the resource's names (`Bookmark`, `Bookmarks`; `TicketType`, `TicketTypes`; `Post`, `Posts`).

### queries.sql (new directives)

> -- name: List<Plural>After :many

> -- name: Update<Singular> :execrows

### queries.sql (changed directive)

> -- name: Delete<Singular> :execrows

Was `:exec`. The generated `Delete<Singular>` now returns the rows it removed.

### delete.POST.go (unchanged comment)

The generated delete action's one comment, `// Handle is POST <route>/{id}/delete.`, is unchanged; only the call below it takes the row count.

### Not listed

The comments on `afterClause`, `fullUpdateStatement` and `queriesSQL` in `internal/generate/store.go`, and the one in `actionDeletePOST` in `internal/generate/actions.go`, are the generator's own and never reach an app. sqlc's `queries.sql.go` carries no doc comments of its own.

## Task 14: Operation messages

The messages the generated operations and `op.DecodeInput` refuse with. The operations' are English, not catalog keys: the API answers them as they are, and the page never shows the money ones because its own `form.Parse` checks run first. `<Field>` is the field's humanized name (`Title`, `Max per order`), `<CODE>` a currency code.

### gen/ops/<resource>/types.go (ValidateCreate, ValidateUpdate, checkMoney, settle)

| When | Message |
|---|---|
| A required text field is empty, or a required money field was not sent | `<Field> is required` (the text `form.Parse` gives a text field today; unchanged) |
| A money field's currency is not one the storage set has | Use a currency code this app supports, like <CODE>. |
| A money field's amount is below zero | Enter an amount of 0 or more. (the English of `rastrillo.ui.money_negative`; generated money fields never allow a negative) |
| An update sends a currency different from the one the field already holds | <Field> is in <CODE>. Send the amount in <CODE>, or leave out currency. |

### gen/ops/<resource>/update.go (mergeable stores with money)

| When | Message |
|---|---|
| The record's currency changed between the operation's read and the store's write | A currency changed while this update was saved. Get the record again, then send the amounts in its currencies. |

### gen/ops/<resource>/list.go

| When | Message |
|---|---|
| `after` is not a cursor a previous page answered | Use the next_after value from the previous page. |

### op/decode.go

`op.DecodeInput` now holds the member rules `api.Decode` applied, with Task 6's messages unchanged. One is new:

| When | Message |
|---|---|
| A money object with no `amount`, such as `{"currency":"USD"}` | Send an amount. |

A value that is not one JSON object is refused with no field and no message of its own; `api.Decode` answers it with Task 6's "Send one JSON object as the body."

## Task 14: Operation descriptions and confirm sentences

What `Descriptors()` carries for the API tree now and MCP later. `<one>` is the resource's singular in lower case (`bookmark`, `ticket type`), `<many>` its plural, `a` becomes `an` before a vowel.

| Operation | Description | Confirm |
|---|---|---|
| List, scoped resource | List your <many>, newest first. | |
| List, unscoped resource | List <many>, newest first. | |
| Get | Get one <one> by id. | |
| Create | Create a <one>. | |
| Update | Change a <one>. Send only the fields to change. | Change <one> {id}? |
| Delete | Delete a <one>. | Delete <one> {id}? |

## Task 14: Comments in generated files

Every doc comment `gen/ops/<resource>/` carries, written into apps. `<Singular>` and `<Plural>` are the Go names, `<one>` and `<many>` the words, `<name>` the manifest name, `<CODE>` the manifest currency.

### types.go

> Package ops_<name> holds the <name> resource's operations: one function per action, called by the page door and the API door alike, so the rules are written once. A hand-written list.go, get.go, create.go, update.go or delete.go under ops/<name>/ replaces the generated file of that name; this file is the contract and stays.

> <Singular> is one <one> as both doors answer it. It is the API's result type and never a template's view model, so a field the manifest declares cannot go missing from one door only.

> ListInput selects one page. A PageNumber from 1 is the page door's numbered pagination, and fills Page.Total; with PageNumber 0 the page starts after the cursor After, which is how the API pages.

> Page is one page of <many>. NextAfter is empty on the last page.

> ListResult is a page as the API answers it.

> Result is p as the API answers it: next_after is null on the last page.

> CreateInput and UpdateInput carry every form field with whether it was sent, so an update changes only what was sent and a required zero is not mistaken for a missing value.

> ListArgs, GetArgs, UpdateArgs and DeleteArgs are what Descriptor.Invoke decodes and the descriptors' Input schemas describe.

> ValidateCreate is Create's checks with no write. A page door whose form did not parse calls it instead of Create and shows both sets of problems at once. The map is never nil, and empty when in passes.

> ValidateUpdate is Update's checks with no write, for the same reason. A field that was not sent is not checked.

> checkMoney is what an amount must be to be stored: in a currency the storage set has, and not below zero, as the page's money field requires. It answers "" for an amount that passes.

> settle is the amount and currency to store: the field's own currency when it has one, else the one sent, else <CODE>, the resource's default. An update never changes a currency that is set (that is the change-currency operation's job), so a different one is op.Conflict.

> load reads one of the caller's <many> without a check of its own: Get, Create and Update have made theirs, and a policy that allows a write but not a read must not turn a stored write into an error. (unscoped: "load reads one <one> without a check of its own: ...")

> moneyOf is a stored amount, or nil for an empty field. Boot refuses a row that has one of the pair without the other.

> items is never nil, so an empty page answers [] and not null.

> The descriptors' JSON Schemas: the arguments as Invoke decodes them and the results as the API door answers them.

> Descriptors names every <name> operation, for the API tree now and MCP later. Each Invoke decodes its arguments with the API door's rules and calls the operation.

### list.go

> List answers one page of the caller's <many>, newest first. By page number it also counts them, for the page door's numbered links. After a cursor it reads one row more than it returns, so it knows whether there is a next page without counting. (unscoped: "List answers one page of <many>, newest first. ...")

### get.go

> Get answers one of the caller's <many>. Someone else's is op.NotFound, the same answer as a missing one, so nobody can probe for rows they may not see. (scoped)

> Get answers one <one>, or op.NotFound. (unscoped)

### create.go

> Create checks and stores a <one>. Both doors call it, so a value one door accepts the other accepts too.

With "owned by the caller" after <one> on a scoped resource, and ", recording who asked" on a mergeable store.

### update.go

> Update changes only the fields that were sent and answers the <one> as it is afterwards. With nothing sent it writes nothing, so updated_at stays as it was. (exclusive)

> update reads the row, merges what was sent and writes the whole row back, in one transaction. That is atomic because rc.DB is the writer pool, which has one connection: no other write can run between the read and the write. Nothing here may use rc.DB while tx is open, or it waits for the connection tx holds. (exclusive)

> Update changes only the fields that were sent, in one event that names who asked, and answers the <one> as it is afterwards. With nothing sent it appends nothing. (mergeable)

> The record's currencies, for an amount sent without one. The store checks again under its write lock, on the history the event lands on, so a currency changed since this read is still refused. (mergeable with money)

> heldOf is the currency a stored amount is in, or nil for an empty field. (mergeable with money)

### delete.go

> Delete removes one <one>. A missing one is op.NotFound.

With "one of the caller's <many>" and "A missing one, or someone else's," on a scoped resource, and ", recording who asked" on a mergeable store.

## Task 14: docs/site/reference/op.md

New, after the `Nullable.IsNullable` paragraph (whose "api.Decode" is now "DecodeInput"):

> `Money.UnmarshalJSONFrom` reads `{"amount": 1490, "currency": "USD"}`.
> `amount` is required and `currency` may be left out; any other member is
> refused.

### DecodeInput (new section)

> `DecodeInput` decodes one JSON object into `dst`, a pointer to a struct,
> with the rules both doors share. `api.Decode` calls it on the body it
> read, and every generated `Descriptor.Invoke` calls it on its
> arguments, so the two cannot drift. It refuses an unknown member, a
> member sent twice, a wrong type, and `null` anywhere but a `Nullable`
> field, each as an `Invalid` keyed by the member's name as sent. A value
> that is not one JSON object is an `Invalid` with no fields; the door
> says what to send instead.

### EncodeCursor and DecodeCursor (new section)

> A list's `next_after` is the last row's `created_at` and `id`, encoded as
> one opaque string. `DecodeCursor` reverses it: `""` is the first page,
> and anything `EncodeCursor` could not have made answers `ok == false`,
> so a hand-edited cursor is refused rather than read as some other place
> in the list.

### Not listed

The comments in `internal/generate/opsgen`, `internal/generate/shape` and `internal/generate/scratch` are the generator's own and never reach an app.

## Task 14: Manifest validation messages

### manifest.go (msgOpsNameTaken, a `Resource.Validate` error)

A resource named `pages`, `updates` or `list_inputs` would be the Go type `Page`, `Update` or `ListInput`, which every generated operations package declares for itself, so the package would not compile. `Validate` refuses the manifest instead. `%q` is the resource name, then the Go name it would take.

> name: %q would be the Go name %q, which every generated operations package already declares; rename the resource

## Task 15: Comments in generated files

Task 15 makes every generated page action call its operation (`gen/ops/<resource>`) instead of the store. The actions' comments are unchanged but one: the doc comment on `handle`, which every edit group with a money field now carries (before, only a mergeable store's did), and which named the store as the one refusing the save.

### gen/actions/<route>/[id]/edit-basics.POST.go and edit-advanced.POST.go (changed comment, `handleDoc` in internal/generate/actions.go)

Was: "handle is Handle's body. When the store refuses the save because the record's currency changed, it runs again: once more to save, and then only to show the form with that message."

> handle is Handle's body. When the operation refuses the save because
> the record's currency changed, it runs again: once more to save, and
> then only to show the form with that message.

### Unchanged comments

`// Handle is GET <route>.`, `// Handle is POST <route>.`, `// Handle is GET <route>/{id}/delete: the confirm page. A GET never mutates; the delete itself is the sibling POST.` and the rest of the `Handle is ...` lines are as they were. The `href`, `normalize<Field>`, `filter<Field>LabelKey` and `filter<Field>Href` comments in index.GET are as they were.

### Not listed

The new helpers an action emits (`present`, `amount`, `clearable`, `minorOf`, and the changed `money` and `currencyOf`) carry no comments, as `amount` and `seed` did not: a comment in a file written into an app is copy, and these have none approved. The comments on `refuseStmts`, `getStmts`, `inputLines`, `inputHelpers`, `mergeParseErrors` and the rewritten package doc in `internal/generate/actions.go`, and on `Refuse` in `view/refuse.go`, are the framework's own and never reach an app. The catalog key the last run shows, `rastrillo.ui.money_currency_changed`, is an existing string.

## Task 15: docs/site/reference/view.md

### Refuse (new section)

> Answers an operation's error the way a page must. An `op.NotFound` is
> the 404 page and an `op.Forbidden` the 403 page, so a page says what the
> API door says for the same id and the same caller. Anything else is
> `Fail`'s 500, logged with `what`. A `Forbidden`'s `Reason` is logged at
> Info and never sent.

> Generated actions call it after every operation call that has no form
> to show a refusal on. A hand-written handler that calls an operation
> can do the same.

## Task 16: API error messages

Task 16 emits the API doors (`gen/api/<resource>/doors.go`) and `gen/api.go`. One message is new: the door's own 422 for a `limit` query parameter that is not an integer. Every other refusal a door answers is an existing string: `api.Decode`'s (Task 6), `api.Refuse`'s codes table (Task 5), or the operation's own field messages (Task 14), re-keyed to their wire names.

### gen/api/<resource>/doors.go (`list`, the `limit` field message; `msgLimit` in internal/generate/apigen)

Answered as `fields.limit` under the `invalid` code when `?limit=` is not a whole number. The range is named so a client that sent 0 or 500 learns what the operation clamps to without a second request.

> Use a whole number from 1 to 200.

## Task 16: Comments in generated files

### gen/api/<resource>/doors.go (package doc; `<pkg>` and `<name>` are the resource's)

> Package `<pkg>` is the `<name>` resource's API doors. Each one
> decodes the request, calls the operation the page door also calls,
> and writes the answer; who may do what is op.Check's to decide, not
> this file's.

### gen/api/<resource>/doors.go (`Routes`)

> Routes is the `<name>` API, for api.New.

### gen/api/<resource>/doors.go (`pathID`)

> pathID is the {id} path value. One that is not a positive integer
> names no row, so it is the same 404 a missing row gets, as
> view.ParseID answers it on the page.

### gen/api.go (`APIRoutes`, with at least one exposed resource)

> APIRoutes is every exposed resource's API doors, for api.New.

### gen/api.go (`APIRoutes`, with none exposed, or no resources at all)

> APIRoutes is every exposed resource's API doors, for api.New. This app
> exposes none.

### gen/api.go (`Operations`, with at least one resource)

> Operations is every resource's operations, exposed or not. Each
> descriptor's Exposed says which, so an MCP server can offer only
> those without guessing.

### gen/api.go (`Operations`, with no resources)

> Operations is every resource's operations. This app has none.

### Not listed

The handlers (`list`, `create`, `get`, `update`, `remove`) carry no comments, as the page actions' helpers do not. The comments in `internal/generate/apigen` are the generator's own and never reach an app.

## Task 17: Generator messages

Task 17 lets a hand-written `ops/<resource>/<operation>.go` replace one generated operation file. Five messages are new, all printed by `rastrillo generate` (the first four) or `rastrillo generate --check` (the last) when the generator refuses what it finds under `ops/`. Each names the file and says what to do. The constants are in `internal/generate/opsgen/handops.go`.

### `msgTypes` (a hand `ops/<resource>/types.go`)

> ops/bookmarks/types.go: only the five operation files can be replaced; types are the API contract. Remove it, or put what it declares in one of list.go, get.go, create.go, update.go or delete.go

### `msgNotAnOp` (any other filename under `ops/<resource>/`)

> ops/bookmarks/helpers.go: only the five operation files can be replaced: list.go, get.go, create.go, update.go and delete.go. Remove it, or put what it declares in one of them

### `msgNoResource` (an `ops/<name>/` with no manifest resource `<name>`)

> ops/nope: no manifest resource is named "nope". Remove the directory, or declare the resource in manifest/nope.toml

### `msgNotInDir` (a file directly under `ops/`)

> ops/stray.go: a hand-written operation lives in ops/<resource>/<operation>.go. Move it there

### `msgUntagged` (`--check` only; one line per file, after the first)

The same wording `--check` already uses for an untagged action file, with "operation files" in place of "action files".

> operation files missing the build constraint:
>   ops/bookmarks/create.go
> each needs `//go:build rastrillo_actions` (then a blank line) above its package clause,
> so `go build ./...` and friends skip generator input instead of failing on it
> (a file that already carries a //go:build line needs it amended, never a second line)

### `cmd/rastrillo/generate.go` (clearing `gen/ops` before a run fails)

Wraps the operating system's error, as the line above it does for `gen/actions`.

> clear stale generated operations: %w

### Not listed

The comments in `internal/generate/handfile` and `internal/generate/opsgen/handops.go` are the generator's own and never reach an app. The hand file's copy under `gen/ops/` is the author's text with only its package clause changed.

## Task 18: Lint messages

Task 18 adds the parity lint to `rastrillo generate --check` (spec section 7). Every finding is printed as `<file>:<line>: <message>` under one header, then the command fails with a count. The constants are in `internal/generate/lint/lint.go`; the header and the count are in `cmd/rastrillo/generate.go`. `<Op>` is the operation the door calls (`List`, `Get`, `Create`, `Update` or `Delete`), `<Func>` the exported function in the hand operation file, `<name>` the package-level variable.

### The header and the count (`cmd/rastrillo/generate.go`)

> rastrillo generate: hand-written code is off its operation:

> 3 parity finding(s); the page and the API must do the same thing (JSON API spec §7)

### `msgGuard` (an exported function in `ops/<resource>/<op>.go` that does not begin with the guard)

> ops/bookmarks/create.go:12: Create: begin with `if err := op.Check(...); err != nil { return ..., err }`, so this operation enforces the same rules as the generated one

### `msgVar` (a package-level `var` whose value calls a function, in a hand operation file)

> ops/bookmarks/delete.go:11: var started: no work may run before the guard. Give it its value inside the operation, after op.Check

### `msgCall` (an override of an exposed door that never calls the door's operation at the top level of `Handle`)

Also the message when the override calls a different operation, or only `ValidateCreate` or `ValidateUpdate`. The line is `Handle`'s.

> actions/bookmarks/index.POST.go:15: call ops_bookmarks.Create from Handle, at the top level, and use its error, so this page and the API do the same thing; or say why it has no API twin with `//rastrillo:page-only <reason>` above the package clause

### `msgBranch` (the operation is called, but only inside an `if`, `for`, `switch`, `select` or function literal)

The line is the call's.

> actions/bookmarks/index.POST.go:24: ops_bookmarks.Create runs only in a branch. Call it at the top level of Handle, so every request goes through the operation

### `msgErrUnused` (the operation is called at the top level, but its error is dropped: a bare call, or `_` in the error's position)

The line is the call's.

> actions/bookmarks/index.POST.go:23: use the error ops_bookmarks.Create returns. A refusal must reach the page

### `msgReason` (`//rastrillo:page-only` with nothing after it)

The line is the directive's.

> actions/bookmarks/index.POST.go:3: say why this action has no API twin: `//rastrillo:page-only <reason>`

### `cmd/rastrillo/generate.go` (the `--check` success line and the flag's help)

The success line gains a last clause; the flag's help gains the same words.

> rastrillo generate --check: N route(s), actions tagged, locale catalogs complete, icon slugs resolve, hand-written code on its operation

> verify without writing (route collisions, action build tags, i18n catalog completeness, hand-written code on its operation)

### Not listed

The package doc of `internal/generate/lint`, which says plainly what the lint does not catch (a write made after the operation returns, and a handler registered outside `actions/`), and the comments in `internal/generate/lint/lint.go` and on `Door` and `Doors` in `internal/generate/actions.go`, are the framework's own and never reach an app. The reference and guide pages say the same in Task 23.

## Task 19: Contract messages

Task 19 adds `api/v1.json`, the committed record of what `/api/v1` serves (spec section 6). `rastrillo generate` compares the file it would write with the committed one before it writes anything; `rastrillo generate --check` reads and never writes. Every string below is printed by the command, and is in `internal/generate/contract/contract.go` unless said otherwise. `<id>` is the operation, as `bookmarks.create`; `<name>` is a field's wire name.

### The breaking changes (one line each, under the headers below)

> `<id>`: route removed (DELETE /api/v1/bookmarks/{id})

> `<id>`: route changed from GET /api/v1/links/{id} to GET /api/v1/bookmarks/{id}

> `<id>`: now needs write access, so a read-only token loses it

> `<id>`: argument `<name>` was removed

> `<id>`: argument `<name>` changed kind from textarea to text

> `<id>`: argument `<name>` is now required

> `<id>`: new argument `<name>` is required, so a client that does not send it is refused

> `<id>`: result field `<name>` was removed

> `<id>`: result field `<name>` changed kind from textarea to text

### `generate` refuses a breaking change (`BreakingError`)

The breaks are listed between the first line and the last.

> 1 breaking change(s) to /api/v1:
>   bookmarks.create: argument link is now required
> api/v1.json was left as it was. Undo the manifest edit, or run rastrillo generate -accept-breaking to rewrite it (JSON API spec section 6)

### `generate` on a file that does not parse

> api/v1.json does not parse: `<error>`. Fix it, or run rastrillo generate -accept-breaking to rewrite it

### `generate` wrote the file (one line on standard output)

> api/v1.json: written

> api/v1.json: updated

With `-accept-breaking` over a break, the breaks follow, indented:

> api/v1.json: rewritten over 1 breaking change(s):
>   bookmarks.create: argument link is now required

### `--check`

> api/v1.json is missing. Run rastrillo generate to write it

> api/v1.json is out of date. Run rastrillo generate

> api/v1.json is out of date, and the change breaks /api/v1 clients:
>   bookmarks.create: argument link is now required
> Undo the manifest edit, or run rastrillo generate -accept-breaking to rewrite it

> api/v1.json does not parse: `<error>`. Delete it and run rastrillo generate

### The flag's help (`cmd/rastrillo/generate.go`)

> rewrite api/v1.json over a change that breaks /api/v1 clients; without it, generate refuses and leaves the file as it was

### `docs/site/cli.md` (rastrillo generate; the synopsis gains `[--accept-breaking]`)

> `generate` also writes `api/v1.json` at the app root: every route `/api/v1` serves, with its arguments and result fields. Commit it. Before writing, `generate` compares the file it would write with the committed one. A change that only adds (a new route, a new optional argument, a new result field) is written. A change that breaks a client (a route or field removed, an argument newly required, a kind changed) fails, names each break, and leaves the file as it was. To rewrite it over a break on purpose, run `rastrillo generate --accept-breaking`; the diff shows the break in review. `--check` fails if the file is missing or stale, and never writes it.

### Not listed

The kinds in the file (`text`, `textarea`, `money`, `integer`, `time`, `cursor`, `array`) are the file's vocabulary, not sentences. The package doc and the comments in `internal/generate/contract` and on `Door` and `Doors` in `internal/generate/apigen` are the framework's own. The reference page for the contract is Task 23's.

## Task 20: Comments in generated files

New in `gen/paritytest/`, which the generator writes for an app with at least one exposed resource (`internal/generate/paritygen`). An app's tests call it; people using the app never see it.

### gen/paritytest/fixture.go (package doc)

> Package paritytest runs one scenario through a resource's page door
> and through its API door, each against a fresh app of its own, and
> fails when the two end differently. The app's tests supply a Fixture
> and call one Run function per exposed resource.

### gen/paritytest/fixture.go (`Fixture`)

> Fixture is the app's half of the parity test. A subject is the test's
> name for a person, "a" or "b"; the fixture maps it to someone it signs
> in, the same person across SignIn, Token and Prepare.

### gen/paritytest/fixture.go (`Fixture.New`)

> New is a fresh app on its own database, mounted as production
> mounts it: the pages and the API tree on one handler.

### gen/paritytest/fixture.go (`Fixture.SignIn`)

> SignIn returns a client carrying subject's browser session. Every
> request it sends must reach h, whatever the host; the test sends
> them to http://app.test, without a locale prefix.

### gen/paritytest/fixture.go (`Fixture.Token`)

> Token mints a read and write API token for subject.

### gen/paritytest/fixture.go (`Fixture.Prepare`)

> Prepare seeds what a hand-written operation needs. It may do
> nothing.

### gen/paritytest/fixture.go (`Fixture.ValidCreate`)

> ValidCreate is a valid create's form values, keyed by manifest
> field name, or nil for the generated ones. A money field's value
> is the amount as typed in English, such as "12.50", in the
> resource's currency.

### gen/paritytest/fixture.go (`step`)

> step is how one step of the scenario ended: its kind ("ok",
> "invalid" or "not_found") and, for a read, the row as the API answers
> it without id and timestamps, which differ between the two apps.

### gen/paritytest/fixture.go (`compare`)

> compare fails on each step the two doors ended differently.

### gen/paritytest/fixture.go (`page`)

> page sends one request through the signed-in client without following
> redirects, so a 303 is seen as the page door's success. Sec-Fetch-Site
> is what a browser sends with its own form, which csrf.Protect
> accepts.

### gen/paritytest/fixture.go (`kind`)

> kind is a page outcome without the redirect's target.

### gen/paritytest/fixture.go (`lastID`)

> lastID is the id at the end of a create's redirect, /<route>/<id>.

### gen/paritytest/fixture.go (`call`)

> call sends one API request with token and answers its kind and its
> decoded body.

### gen/paritytest/fixture.go (`call`, inside, above the decoding)

> Numbers stay as written: as float64, two amounts past 2^53
> would compare equal, and an edit would post the rounded one.

### gen/paritytest/fixture.go (`record`)

> record is one row as the API answers it.

### gen/paritytest/fixture.go (`row`)

> row is record without what differs between two apps by design.

### gen/paritytest/fixture.go (`refused`)

> refused fails unless the API's 422 named exactly want.

### gen/paritytest/fixture.go (`hidden`, emitted when a resource is scoped)

> hidden fails unless another person's row was not found through
> door. compare alone would pass two doors that both showed it.

### gen/paritytest/fixture.go (`money`)

> money is an amount typed in English as the API takes it.

### gen/paritytest/fixture.go (`moneyForm`)

> moneyForm sets name's four form values as a money input sends them:
> the text, the locale it is written in, the currency and, on an edit,
> the seed, so an unchanged amount saves as it is.

### gen/paritytest/<name>_parity.go (Run\<Plural\>; `<Plural>` and `<name>` are the resource's)

> Run\<Plural\> runs the parity scenario for \<name\>.

### Not listed

`money`, `moneyForm`, `refused` and `hidden` are emitted only when a resource needs them; their comments are listed above all the same. The per-resource `<name>Pages` and `<name>API` functions carry no comments. The comments in `internal/generate/paritygen` and in `examples/notes/internal/notestest/fixture_test.go` are code comments, not generated copy.

The failure messages in `fixture.go` (`compare`, `refused`, `hidden` and the helpers' `t.Fatalf` lines) are read by a developer whose parity test failed. They name the step, the door and what each door answered, and are not listed one by one.

## Task 21: Token page (examples/notes)

The notes app draws the token page with its own template, `examples/notes/internal/notes/templates/tokens.html`, from the Task 9 keys above. These are the strings it adds.

### locales/en.toml (the list, a token that cannot be revoked)

Shown in a row's last column in place of the Revoke button.

> rastrillo.ui.api_tokens_revoked: Revoked

> rastrillo.ui.api_tokens_expired: Expired

### examples/notes/internal/notes/templates/layout.html (navigation)

The signed-in navigation gains a link to the page. Its text is `rastrillo.ui.api_tokens_title`:

> API tokens

### examples/notes/internal/notes/templates/tokens.html (one button per agent)

The page shows one "Revoke all for this agent" button for each agent name that still has a live token. The template puts the agent's name after the key's text, so the button reads:

> Revoke all for this agent: Claude

This joins a translated sentence and a name with a colon in the template. If that reads wrong, the fix is a key with an `{agent}` placeholder, such as "Revoke all for {agent}".

### Not listed

The request log line `api: request` (`api/tree.go`), with `method`, `path`, `actor` and, for agent tokens, `agent_session` and `agent_harness`, is for operators reading logs. People using the app never see it.

## Task 21: translated after copy review

Each of these carried the English of `rastrillo.ui.api_tokens_revoked` and `rastrillo.ui.api_tokens_expired` as a placeholder until the English was approved. Drafted on 2026-10-22, unreviewed, in the base catalogs' register.

- locales/ga.toml
- locales/zh-Hans.toml
- locales/es.toml
- locales/hi.toml
- locales/pt-PT.toml
- locales/bn.toml
- locales/ru.toml
- locales/ja.toml
- locales/yue.toml
- locales/vi.toml
- locales/ar.toml

## Task 22: The scaffold's API, token page and gate

`rastrillo new` now writes the JSON API into every app. Everything below is read by app authors: comments in files the scaffold writes once and the app owns, the scaffold's docs, its Makefile, and the messages its own tests fail with. The token page reuses the Task 9 and Task 21 keys and adds none. Comments the scaffold already carried and this task only reworded around (the `App` doc's list, "schema, router, JSON API, static files") are not repeated.

### internal/\<app\>/app.go (`Mux`)

> Mux mounts the static files and the JSON API (api.go) beside the router.

> Outside the router on purpose: no CSRF check and no page middleware.
> A bearer token is the API's only credential, so a page cannot make
> it act with the browser's cookies. Both patterns, so a bare /api/v1
> gets the API's JSON 404 rather than the mux's own redirect.

### internal/\<app\>/migrations.go (`BootSchema`)

Replaces "A fresh scaffold uses no subsystem yet, so today this is just Schema. Adding one (say, sessions) means editing this line, not Schema:".

> A fresh scaffold uses one subsystem, apitoken, whose table the JSON
> API reads on every request (api.go). Adding another (say, sessions)
> means editing this line, not Schema:

### internal/\<app\>/api.go (new; template `cmd/rastrillo/scaffold/api.go.tmpl`)

`tokenPrefix`:

> tokenPrefix starts every API token this app makes, so a secret
> scanner can tell a leaked one is this app's (.gitleaks.toml has the
> rule). rastrillo new made it from the app's name. It is written out
> rather than worked out at boot because changing it makes every token
> already given out stop working, and renaming the module should not.

`API`:

> API is the JSON API Mux mounts at /api/v1: every operation a manifest
> resource exposes (gen.APIRoutes), called with a bearer token instead
> of a session. A new app exposes none, so it answers 404 until a
> manifest declares a resource (manifest/README.md).

Inside `API`, above `Ctx` and above `Redact`:

> Actor stays zero: the tree fills it from the token.

> Redact keeps a token quoted in an error message out of the log.

`authorize`:

> authorize is this app's policy: op.Check asks it before every
> operation, on the API and on generated pages alike. It is
> op.DefaultPolicy on purpose. A token may do what its owner can do
> with their own rows, and nothing on a resource with no owner.
>
> The API cannot see page middleware. If your pages add a rule, such
> as only admins may change prices or a suspended account may not
> write, add it here before the return, refusing with op.Forbidden.

`newTokens`:

> newTokens is the token store, for the API and the token page alike:
> both must use one prefix, or the page makes tokens the API refuses.

### internal/\<app\>/tokens.go (new; template `cmd/rastrillo/scaffold/tokens.go.tmpl`)

`Tokens`:

> Tokens returns the API token page's routes, for Router to mount with
> r.Group: a person makes a token for an agent or a script at /tokens,
> sees it once, and revokes it there.
>
> Every door takes the token's owner from the signed-in session, so the
> page needs sign-in, which a new app does not have yet. Once it does
> (examples/notes in the rastrillo repository shows how), add this to
> Router after csrf.Protect:

Above the create route:

> Making a token needs a sign-in within ten minutes: a token
> outlives the session that made it, so this is the moment to
> be sure who is at the keyboard. Revoking does not, so that
> stopping a token stays easy.

`tokenRow` and `renderTokens`:

> tokenRow is one token with its state worked out here, because a
> template has no clock: a live token gets a revoke button, a revoked
> or expired one gets the word instead.

> renderTokens is apitoken.HandlersConfig.Render. Agents is each agent
> name that still has a live token, so the page offers one "revoke all
> for this agent" button per agent rather than one per row.

### internal/\<app\>/templates/tokens.html (new; header comment)

The page itself is examples/notes' tokens.html with the same keys; the "Revoke all for this agent: Claude" join from Task 21 applies here too.

> tokens.html: the API token page, drawn by tokens.go's renderTokens.
> The new token's secret is in .Page.Created only in the response
> that made it: never put it in a link, a redirect or a flash.

### internal/\<app\>test/api_test.go (new; template `cmd/rastrillo/scaffold/api_test.go.tmpl`)

Comments:

> The API answers at /api/v1 itself, not only below it, and in JSON: a
> bare /api/v1 that the outer mux answered with its own redirect would
> send an agent's request somewhere the tree never sees. A new app
> exposes nothing, so the answer is the tree's 404.

> tokenRE is a whole token with this app's prefix.

> The token page, mounted the way tokens.go says to once the app has
> sign-in: the person makes a token and sees it once, and the page
> lists it afterwards by its public ID alone. A new app has no sign-in,
> so this test brings the sessions table and a session of its own.

Failure messages (`%s` and `%d` are the path, status or header):

> GET %s: status %d, want 404

> GET %s: not the API's JSON 404 (%v):

> GET %s: Cache-Control = %q, want no-store

> GET /tokens: status %d, want 200 and the page title:

> POST /tokens: status %d, want 200 and the new token:

> POST /tokens: Cache-Control = %q, want no-store: the reveal must not be cached

> GET /tokens after making one: want the public ID %s listed and the secret gone:

### .gitleaks.toml (new; template `cmd/rastrillo/scaffold/gitleaks.toml`)

> gitleaks' default rules, plus one for this app's API tokens. make
> gitleaks reads this file because it sits at the repository root.

> An API token reads \<prefix\>\_\<public id\>\_\<secret\>. The default rules do
> not know the shape, so without this one a token pasted into a test or
> a config file would be committed unnoticed. If one is found, revoke it
> on the app's token page: it stays in every clone of the history.

The rule's description, which gitleaks prints beside a finding:

> Rastrillo API token

### Makefile

New target, above `generate-check:`:

> generate-check fails when gen/ or api/v1.json is not what rastrillo
> generate would write from manifest/, or when a manifest change breaks
> a client of /api/v1. Run rastrillo generate to bring them up to date.

Added to the `gitleaks` comment:

> .gitleaks.toml adds a rule for this app's API tokens to the defaults.

Removed from the `ci` comment: "If the app declares manifest resources, also add: rastrillo generate --check".

### go.mod (the tool directive's comment)

Replaces "make migration-check, and rastrillo generate once the app declares manifest resources."

> The CLI this app's own gate runs: make migration-check and make
> generate-check.

### README.md and AGENTS.md (the gate's list)

README.md:

> `make ci` is the gate: vet, gofmt, staticcheck, govulncheck, gitleaks, tests, the migration check and the generate check, one definition for CI and for you.

AGENTS.md:

> The gate is `make ci` (vet, gofmt, staticcheck, govulncheck, gitleaks, budgets, tests, the perf lane, the migration check and the generate check), the same definition CI runs.

### AGENTS.md (new rule, under "Rules the family holds hard")

> The API cannot see page middleware. When a page gains a rule beyond
> ownership (admins only, no writes while suspended), add it to
> `authorize` in `api.go` too, or a token gets around it.

### manifest/README.md (steps 6 and 7)

Step 6's code gains `Authorize: authorize,` and this sentence after its paragraph:

> Authorize is api.go's authorize, so the pages and the API follow the
> same rules.

Step 7 replaces "Add `rastrillo generate --check` to the Makefile's ci target.":

> 7. Commit api/v1.json with gen/. A resource with scope = "user" is on
>    the JSON API at /api/v1/\<name\> after its first generate. A resource
>    with no scope stays off it until its manifest adds:
>
>        [api]
>        expose = true
>
>    Tokens are still refused there until you allow them in api.go's
>    authorize. `make ci` runs `rastrillo generate --check`, which fails
>    when gen/ or api/v1.json is out of date.

### manifest/README.md (step 5, fix round 1)

Step 5's code now ends `var BootSchema = migrate.Merge(apitoken.Schema, genSchema, Schema)`, and this paragraph follows it:

> Keep every schema already in BootSchema, apitoken.Schema and any
> other subsystem's. A schema left out is a table boot never creates:
> without api_tokens, every API request fails.

### rastrillo new (what it prints)

> Makefile             (make ci = vet + fmt + staticcheck + govulncheck + gitleaks + budget + test + perf + migration check + generate check, the one gate definition;

### Not listed

The repository's own `.gitleaks.toml` comments, the scaffold-smoke messages (`hack/scaffold-smoke.sh`) and the messages of the tests in `cmd/rastrillo` and `examples/tickets` are for people working on rastrillo, not on an app.

## Task 23: Docs, the skill topic and the CHANGELOG

Whole pages and sections, listed by file and heading. Review them in the files. Short strings a reader sees out of context (nav labels and blurbs, headings, quoted UI words) are pasted here verbatim.

This supersedes three Task 2 entries above: the `docs/site/agents.md` intro (the page is rewritten), the `docs/site/nav.json` Agents blurb, and the CHANGELOG "Removed: the agent tool registry" entry, which is folded into the new JSON API entry as its first bullet.

### docs/site/nav.json (new entry, Building an app, before Agents)

> label: JSON API
>
> blurb: Serve your manifest resources at /api/v1 to tokens, with one set of rules for the pages and the API.

### docs/site/nav.json (Agents blurb, changed)

> Agents act for a person: who the token names, what the audit trail records, and the sidecar loop.

### docs/site/api.md (new page, "JSON API")

Every section is new copy:

- intro (above the first heading)
- What you get (with the routes table: "list", "create", "get one", "change some fields", "delete")
- Mounting the API (the four numbered steps, and the two paragraphs on why not inside the router and not `csrf.Exempt`)
- Who may do what (the `op.Check` order, `op.DefaultPolicy`, and the example `authorize` with its comment "tokens may read ticket types")
- Tokens
- The token page (quotes the token page's own strings: "This token is for me, not an agent", "Read and write", "Read only", "30 days", "90 days", "1 year", "never")
- Calling the API (the agent headers and the per-request log line)
- Requests and answers (answers, money, writes, PATCH, lists, the errors table's "When" column)
- The contract file
- Putting a resource on the API
- Writing an operation by hand
- Changing a page action by hand (the example directive below is copy too)
- The parity test

> //rastrillo:page-only the importer takes a file upload, and the API has no file transfer yet

Headings, verbatim:

> What you get / Mounting the API / Who may do what / Tokens / The token page / Calling the API / Requests and answers / The contract file / Putting a resource on the API / Writing an operation by hand / Changing a page action by hand / The parity test

### docs/site/agents.md (rewritten above "The sidecar")

- intro
- Tokens are made for a named agent
- Actor says who is calling (with its table's "Holds" column)
- What the audit trail records (with its table's "Who" column)
- Showing it on a page
- Coming later

> Three later pieces build on the API: an MCP endpoint, sign-in for agents with OAuth, and a generated skill that describes the app to an agent.

The sidecar section is unchanged except one em dash, now a comma: "the loop sleeps for a minute, the family's polling floor."

### docs/site/compare.md

New section "A JSON API", above "Where the others win":

> Every Rastrillo app can serve its manifest resources as JSON at `/api/v1`, beside its pages. A person makes a token for their agent, and the app records each call as the agent acting for that person. The pages and the API call the same operations, so they follow the same rules. See [JSON API](/docs/api).
>
> There are no OpenAPI files, generated clients, OAuth sign-in, CORS or file uploads yet, and only one API version is served at a time.

"Where the others win", first paragraph (no longer says "when you are building a JSON API"):

> Reach for a router (chi, echo, gin, fiber) when you want to pick your own database, auth and deployment. A router plus four libraries you chose is a legitimate architecture, and the most common one in Go.

"Where Rastrillo is the wrong answer", second and third paragraphs (was "And for a pure JSON API, sessions, CSRF, flash and owner-scoped 404s are most of what you would be paying for."):

> Velocity has more JS tooling. Rastrillo assumes SQLite, so Postgres and MySQL have to be own-rolled. [Data](/docs/data) has the detail.
>
> If your app is only an API, with no pages and no people signing in, sessions, CSRF, flash and the page screens are most of what you would be paying for. Goa or a router fits better.

### docs/site/manifests.md (new section "The JSON API", after Ownership)

> When your app mounts the API, a `scope = "user"` resource is also served as JSON at `/api/v1/<name>`, to a person's token. A resource with no owner is not, unless you add: [the `[api]` block]
>
> Commit `api/v1.json` with `gen/`. See [JSON API](/docs/api).

### docs/site/reference/api.md

Intro, new last sentence:

> [JSON API](/docs/api) is the guide: how to mount the tree and write your policy.

New, under New, after the `Handle` paragraph:

> Before `Handle` runs, the tree logs one Info line to `Config.Logger`, `api: request`, with `method`, `path` and the actor's `Actor.LogAttrs`: `actor`, and `agent_session` and `agent_harness` when they were sent. Every value but the method goes through `Config.Redact`.

### docs/site/reference/apitoken.md (intro, new last sentence)

> [JSON API](/docs/api#tokens) shows the store and the token page wired into an app.

### docs/site/reference/op.md (intro, new last sentence)

> `rastrillo generate` writes one operation per action to `gen/ops/<name>/`; [JSON API](/docs/api#writing-an-operation-by-hand) says how to replace one.

### skill/api.md (new topic, "JSON API, tokens and agents")

Read by agents, not people, but listed because it names the same facts the pages do. Every paragraph is new: the intro, Wiring, One policy for both doors, Actor, Tokens, Calling, Wire, Exposing, The contract, A hand-written operation, A hand-written action, Parity test.

### SKILL.md

New index row:

> | serve JSON at `/api/v1`, mint tokens, let an agent act for a person, write `Authorize`, hand-write an operation | `skill/api.md` |

"Reach for a manifest" paragraph: "commit `gen/`" becomes "commit `gen/` and `api/v1.json`".

### skill/tooling.md

> `rastrillo generate` writes `gen/` and `api/v1.json` from `manifest/`: commit both, never hand-edit; `make ci` runs `generate --check` (an app made before it: add a `generate-check` target to `ci`).

New end of the Manifests paragraph:

> ...takes that one screen over; on a resource the API exposes it must call the screen's operation (`skill/api.md`).

### CHANGELOG.md (Unreleased, new first entry)

The bold headline, the intro paragraph, the seven upgrade steps and the "What changed" bullets. Headline, verbatim:

> **A JSON API for every app, and agents that act for a person (breaking upgrade for generated apps and for code that reads `Actor`).**

### Task 23 fix round 1: docs/site/api.md (Who may do what, after the example)

Replaces "`Forbidden.Reason` goes to your log, never to the caller. Any error other than `op.Forbidden` or `op.NotFound` answers 500.":

> Return `op.Forbidden` or `op.NotFound` to refuse. The API answers 403 or 404, and a page shows its 403 or 404 page. `Forbidden.Reason` goes to your log, never to the caller.
>
> `op.Check` returns your error unchanged. The API answers any error from the [errors table](#requests-and-answers) with its own code, so an `op.Conflict` is 409. An error it does not recognise is 500.

### Task 23 fix round 1: docs/site/api.md (The parity test, the scenario)

Replaces the scenario list:

> - a valid create, and the row it stored;
> - a create with no values, when the resource has a required field;
> - a change to one text field, and the row after it, when the resource has a field that is not money;
> - for a resource with an owner, another person's row, which must be 404 through both;
> - a delete;
> - a row that does not exist.
>
> Each step compares how the two doors ended: the stored row, or the kind of refusal. A page's answer shows only the kind, not which fields it refused. So the API's answer to the create with no values is also checked on its own: its 422 must name exactly the required fields.

### Task 23 fix round 1: skill/api.md

The policy paragraph's refusal sentence and the Parity test paragraph are reworded to the same facts as above.

## Final review fixes

The cross-provider review of the whole branch (final-review.md) found five things; these are the words that changed with them.

### Finding 1: gen/router.go (the generated comment on Router)

The first two sentences change. The old text said the factory constructs a fresh Ctx per request; the router now copies whatever the factory returns, so a factory may hand every request the same one.

> Router builds the app's mux. ctxFactory constructs the *rastrillo.Ctx
> a request runs with; see cmd/<app>/main.go. Each request gets a copy
> of what the factory returns, so a factory may hand every request the
> same Ctx. An actor stamped on the request context (rastrillo.WithActor,
> the API tree's doing) lands on Ctx.Actor after the factory runs.
> Failing that, a factory that left Actor zero gets the signed-in person
> (sessions.ActorFrom), so op.Check sees the subject the scoped store
> filters by and the caller is attributed even when the factory never
> thinks about actors.

### Finding 2: rastrillo generate --check (new refusal, one line per stale hand action)

`generate.StaleRewriteMsg`. Printed when the copy of a hand action under `gen/actions` is not what generate would copy from today's source. The shape follows `op.generate.stale`.

> gen/actions/index_get/index.GET.go is an older copy of actions/index.GET.go; run rastrillo generate

### Finding 3: manifest loading (new refusal)

`internal/manifest.sameGoName`. Printed by generate, --check and anything else that loads the manifests, when two resource names become one title-cased Go identifier. The parity package declares that identifier once per resource in one package, so the app would not build. The files are the manifest paths, as the duplicate-name and duplicate-route refusals print them.

> resources "report2s" (manifest/report2s.toml) and "report_2s" (manifest/report_2s.toml) both become Report2s in generated code; rename one

### Finding 4: docs/site/reference/op.md (Check, step 3)

Replaces "Any other error is a 500.", which was wrong: `Check` returns the policy's error as it is, and the API already answers `Invalid` and `Conflict` by their codes.

> If `rc.Authorize` is set, its error is the answer. Return `nil` to allow, or `Forbidden` or `NotFound` to refuse. `Check` returns your error unchanged, and the door that called it answers it the way it answers the same error from an operation: the API by its errors table, a page with its 403 or 404 page. An error neither recognises is a 500.

### Finding 4: the design spec, section 4 step 3

The same correction, in the spec's words:

> Its error is the answer: `nil` allows, an `op.Forbidden` or `op.NotFound` refuses. `op.Check` returns the error unchanged, and the door that called it answers it as it answers the same error from the operation: the API by its errors table (section 6, so an `op.Conflict` is 409), a page with its 403 or 404 page. An error neither recognises is a 500.

### Finding 5: gen/store/<mergeable>/store.go (the generated comment on Create)

One word in the first line: the create goes through `eventlog.AppendUnder`, which main's merge kept; `AppendNew` was the branch's name for it and no longer exists.

> CreateAnnouncement starts a record. eventlog.AppendUnder picks its id and appends its first event in one transaction, so two creates at once make two records.

## Partials (approved by Paul on 2026-10-23)

The two deferred `ui` partials, `actor` and `api-tokens`, landed on 2026-10-22 after the English above was approved. Their samples reuse the approved keys for everything the reader sees inside a component; the strings below are the new English around them, approved without edits on 2026-10-23. The gallery's sentences carry unreviewed drafts in all eleven languages (`internal/designsystem/prose.go`), written the way Task 9's were, and `proseDrafts` is empty again.

### internal/designsystem/samples.go (the actor sample: blurb, two state names, one note)

> Who did something, as words: a person's name, or the agent and the person it acted for. No badge and no icon.

> An agent acting for a person

> A person

> Set ui.ActorName once at boot to turn a subject into a name. The app itself, the zero Actor, renders nothing.

### internal/designsystem/samples.go (the api-tokens sample: blurb, three state names, two notes)

> The token page from apitoken's PageData: the one-time reveal, the create form, the list and the revoke buttons.

> Four tokens, two of them live

> Pass what Handlers hand Render as Page, and the page's own path as Path. The revoke forms post to Path/revoke, Path/revoke-agent and Path/revoke-all; set RevokePath, RevokeAgentPath or RevokeAllPath to change one.

> Just created: the secret, shown once

> Render the create form's response with Created set, and do not redirect it: the secret is in that response and nowhere else.

> No tokens yet

### docs/site/reference/ui.md (actor, after the ActorName paragraph)

> The `actor` partial writes the same words in a span, for a list cell
> or a detail row:
>
> (code: `{{template "actor" dict "L" $L "Actor" .Actor}}`)
>
> The span is `<span rst-actor>`. The app itself, the zero `Actor`,
> renders nothing. Pass `Display` with words you built in Go when your
> lookup needed the request.

### docs/site/templates.md (The partials, after the media-grid paragraph)

> Who did something is `actor`: a person's name, or `Claude for Paul`
> for an agent acting for them, as plain words. Pass the
> `rastrillo.Actor`. Set `ui.ActorName` once at boot to turn a subject
> into a name.
>
> The API token page is `api-tokens`: the one-time reveal, the create
> form, the list and the revoke buttons, from `apitoken`'s `PageData`.
> Pass it as `Page` and the page's path as `Path`, under your own
> `page-header`. See [apitoken](/docs/reference/apitoken).

The partial list above it gains `actor` and `api-tokens` (and `stat`, which it already carried).

### docs/site/reference/apitoken.md (The token page; replaces "`Render` draws the page: there is no partial yet. ...")

> `Render` draws the page. Call `ui`'s `api-tokens` partial with
> `PageData` as `Page` and the page's path as `Path`, under your own
> `page-header`; see [Templates](/docs/templates#the-partials).
> `Row.Display` is the `actor` phrase for the token, in the reader's
> language. Set `Name` to turn a subject into a person's name; without
> it, the page shows the subject. `Row.Revoked` and `Row.Expired` say a
> token's state against the store's clock, and `Agents` lists each agent
> with a live token once, for one "revoke all for this agent" button per
> agent.

### docs/site/api.md (The token page; the second sentence replaces "There is no `ui` partial for the page yet, so your app renders it from `apitoken.PageData`.")

> Your `Render` draws the page with `ui`'s `api-tokens` partial, from `apitoken.PageData`.

### skill/api.md (Tokens; replaces "there is no `ui` partial, so the app renders `PageData`")

> `Render` calls `ui`'s `api-tokens` partial with `PageData` as `Page` and the path as `Path` (a new app ships `tokens.go` and `templates/tokens.html`, mounted once it has sign-in); `PageData` carries each row's `Revoked`/`Expired` and the live `Agents`.

### SKILL.md (Look before you build)

> the API token page (`api-tokens`),

### CHANGELOG.md (What changed; replaces the `actor` partial bullet)

> New partials `actor`, which writes the same words in a span for a list cell or a detail row, and `api-tokens`, the token page's body from `apitoken.PageData`; see [Templates](/docs/templates#the-partials). `apitoken.Row` gains `Revoked` and `Expired`, `PageData` gains `Agents`, and `Token` gains `Actor()`.

### cmd/rastrillo/scaffold/tokens.html (the comment at the top of a file the app owns)

> tokens.html: the API token page, drawn by tokens.go's renderTokens through ui's api-tokens partial. The new token's secret is in .Page.Created only in the response that made it: never put it in a link, a redirect or a flash.

### cmd/rastrillo/scaffold/tokens.go.tmpl (renderTokens)

> renderTokens is apitoken.HandlersConfig.Render: tokens.html hands the page to ui's api-tokens partial, which draws the reveal, the form, the list and the revoke buttons.

### Not listed

The partials' own doc comments (`ui/partials/actor.html`, `ui/partials/api-tokens.html`) and the summaries `skill/partials.md` generates from them are read by developers and agents, like every other partial's. Comments in `apitoken` and `internal/designsystem` are the framework's own.
