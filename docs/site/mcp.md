# 🤖 MCP

An app's operations can also be MCP tools at `/api/v1/mcp`. Claude
Code, Codex and other MCP clients connect with the same token they use
for the [JSON API](/docs/api). They see one tool per operation and can
call those tools. Each call goes through the API's token check, policy
and operation, so an agent can do nothing over MCP that its token
couldn't do over the API.

## Mounting

A new app from `rastrillo new` already serves MCP. `newDoors`, in
`internal/<app>/api.go`, builds the API and MCP from one `api.Config`,
and `Routes` mounts both. To add MCP to an app that already serves the
API:

```go
mcpHandler, err := mcp.New(mcp.Config{
	Gate:      apiConfig.Gate(),
	Ctx:       apiConfig.Ctx,
	Authorize: apiConfig.Authorize,
	Name:      "notes",
	Version:   rastrillo.BuildVersion,
}, gen.ExposedOperations()...)
if err != nil {
	return nil, err
}
mux.Handle("/api/v1/mcp", mcpHandler)
```

Build it from the `api.Config` you pass to `api.New`. `Gate()` copies
out the token check, the logger, the body cap, `Before`, `After`,
`RequireAgentSession` and the redactor, so the API and MCP endpoints
admit the same callers. `gen.ExposedOperations()` is every operation
whose resource is on the API.

On a route table, mount it beside the API with
`routes.Mount("/api/v1/mcp", app.mcp, "mcp")`, outside your page group.
Like the API, it takes a bearer token and nothing else, so CSRF and
sessions have no part in it. Add a line to `.rastrillo/budgets.txt` so
the perf walk skips it:

```text
perf  GET /api/v1/mcp/*  skip  the MCP endpoint
```

`mcp.New` checks everything when the app starts. It fails if `Ctx`,
`Authorize` or `Gate.Verify` is missing, if a descriptor is malformed or
fails [the fence](#the-flags), or if two operations would get the same
tool name. The [mcp reference](/docs/reference/mcp) has the full list.

## Connecting

Make a token on the app's token page, then give the client the endpoint
and the token. In Claude Code:

```sh
claude mcp add --transport http notes https://notes.example.com/api/v1/mcp \
  --header "Authorization: Bearer $TOKEN"
```

In Codex, add the server to `~/.codex/config.toml` and keep the token
in an environment variable:

```toml
[mcp_servers.notes]
url = "https://notes.example.com/api/v1/mcp"
bearer_token_env_var = "NOTES_TOKEN"
```

If the app sets `RequireAgentSession` and the token was made for an
agent, the client must also send an `Agent-Session` header. Without
one, the agent still sees the write tools, but each write call is
refused as `invalid`, with a message telling it to send the header. In
Claude Code, add `--header "Agent-Session: <id>"`.

ChatGPT and claude.ai connect through OAuth, which Rastrillo doesn't
serve yet. A request with no token gets 401 with
`WWW-Authenticate: Bearer`.

## What an agent sees

When a client connects, the server sends it instructions: how to send
money and times, to take IDs from results rather than guess them, to
page with `next_after`, to ask the person before a call whose tool says
so, and to read a write back rather than retry it blindly. Your
`Config.Instructions` comes after them. Use it for what is true of your
app and not of every app.

Each operation is one tool. The tool's name is the operation's ID with
`.` replaced by `_`, so `bookmarks.create` is `bookmarks_create`. Its
title is the first sentence of the descriptor's `Description`, and its
description is the whole of it. When the descriptor has a `Confirm`
sentence, the description gains a line: "Before calling, ask the person
to confirm:" and the sentence, with placeholders such as `{id}` left for
the agent to fill in. The input and output schemas are the descriptor's
`Input` and `Result`. An operation whose `Result` is `{"type":"null"}`,
such as every generated delete, has no output schema.

The tool list is per caller. A tool is listed only when `op.Check` and
the gate allow this caller to use it, so a read-only token sees no write
tools. `api/v1.json` lists each operation's id, method, path, access,
destructive and idempotent flags, and input and result field kinds. It
does not list tool names or JSON schemas. A tool's name is its
operation id with `.` replaced by `_`. A call to a tool the list left
out is refused the way the API would refuse it, usually with
`forbidden`.

## The flags

Each tool tells the client what calling it does, with MCP's hints:

| Hint | Comes from |
|---|---|
| `readOnlyHint` | `Access` is `rastrillo.Read` |
| `destructiveHint` | `Descriptor.Destructive`: a call may change or remove what is already there |
| `idempotentHint` | `Descriptor.Idempotent`: two calls with the same input leave the state one call would |

`openWorldHint` is left unset, so clients read it as true. Many writes
reach real people, and saying which ones don't would be a claim to make
for every operation.

`rastrillo generate` sets the flags. A create is neither destructive nor
idempotent. An update is destructive, and not idempotent, because a
repeat moves `updated_at` again. A delete is both. A list and a get are
reads, and idempotent. When you replace a generated create, update or
delete with your own file, the generator can't tell what your code does,
such as whether it sends email, so it marks the operation destructive
and not idempotent. If you know better, pass `mcp.New` a descriptor of
your own instead of the generated one.

Whether to ask the person first is a separate question, and `Confirm`
answers it. Generated updates and deletes carry a sentence such as
"Delete a bookmark {id}?", and their tools tell the agent to ask with
it. `Destructive` alone doesn't.

A write whose ID, after its last `.`, starts with a verb that undoes or
sends something, such as `cancel`, `refund`, `delete`, `send` or
`purge`, must be `Destructive`. A read can't be. `mcp.New` refuses a
descriptor that fails this check, and `op.FenceVerbs` lists every verb.
Set the flag, or rename the operation if the verb is wrong.

## Writing descriptors by hand

`mcp.New` serves exactly the descriptors you give it, so an app without
manifests can write its own:

```go
search := op.Descriptor{
	Operation: rastrillo.Operation{
		ID: "notes.search", Resource: "notes", Access: rastrillo.Read,
	},
	Description: "Search your notes. Matches titles and bodies.",
	Idempotent:  true,
	Input: json.RawMessage(`{"type": "object",
		"properties": {"query": {"type": "string"}},
		"required": ["query"], "additionalProperties": false}`),
	Result: json.RawMessage(`{"type": "object",
		"properties": {"notes": {"type": "array"}}}`),
	Invoke: func(ctx context.Context, rc *rastrillo.Ctx,
		input json.RawMessage) (any, error) {
		return searchNotes(ctx, rc, input)
	},
}
```

MCP passes the arguments to `Invoke` as they were sent. It doesn't check
them against `Input`, because the API doesn't either: a generated
`Invoke` decodes with `op.DecodeInput`, under the API's rules, so both
endpoints accept and refuse the same things. A hand-written `Invoke`
validates its own input. MCP refuses arguments that aren't valid UTF-8,
or that hold an unpaired surrogate escape, before `Invoke` sees them.

Before `Invoke` runs, MCP runs the gate's `Admit` and `op.Check` for the
operation, so your policy decides every call, even one to a
hand-written `Invoke` that skips `op.Check`. Still begin the operation
with `op.Check`, as a generated one does. The API and your pages call it
without that step.

Put a check that depends on the input inside `Invoke`, before the
operation does anything. `Authorize` sees the operation but not its
input, so it can check a grant's scope but not which team the input
names. Check the team in `Invoke`, then again in your
[guard](#checking-again-as-the-answer-goes-out) as the answer goes out.
The guard is never the first check, because by the time it runs a write
has already happened.

A result must be a JSON object, unless `Result` is `{"type":"null"}`, in
which case it may be `null`. If a result isn't an object and `Result`
isn't exactly `{"type":"null"}`, MCP returns an `internal` tool error
with a ref. A result goes out as `structuredContent` and as JSON in a
text block. Numbers come out exactly as the operation returned them,
never through a float64, and an `encoding/json` `json.Number` is
written as the number it holds. A result over `Config.MaxResult`, 2 MiB
unless you set it, is replaced with a `response_too_large` tool error
that tells the agent to ask for less.

## Refusals

A refusal is a tool error, not a protocol error, so the model can read
it and fix the call. Its text is the API's error envelope, with the
API's code and message:

```json
{"error":{"code":"forbidden","message":"This token can't do that."}}
```

The codes are the API's: `invalid` with its `fields`, `forbidden`,
`not_found`, `conflict`, `response_too_large`, and `internal` with a
`ref` that the log line shares. A panic anywhere in a call answers an
`internal` tool error, and the app keeps running. A call to a tool that
doesn't exist is the SDK's protocol error.

Each call logs one line, `mcp: call`, with the tool name and the actor,
redacted like the API's `api: request` line.

## Checking again as the answer goes out

A token can be revoked, or its grant narrowed, while a call is running.
When `Gate.Verify` is an `api.Rechecker`, as `apitoken`'s store is, MCP
checks again before the answer's headers and before each 64 KiB of its
body, the same way the
[API does](/docs/api#checking-again-as-the-answer-goes-out).

For a tool call, each check is the token, then the gate and `op.Check`
for the operation that ran. The gate rechecks them on every answer.
Your `Config.Guard` runs last only when the call returns a successful
result. For the tool list, each check covers every tool that was
listed. If any of them is no longer the caller's to use, the whole list
is refused rather than quietly shortened.

`Guard` is an `op.ResponseGuard`. It gets the fresh actor, the
descriptor, the arguments as sent and the value `Invoke` returned, and
checks that the actor may still see the result: for example, that each
note in a search result is still in a team they belong to. For the tool
list, the descriptor and the arguments are nil, and the value is the
`[]op.Descriptor` that was listed. A guard must not run the
operation again or change anything.

Put a slow check against another service first in your guard, and your
own revision and link checks after it. The guard is the last check
before each chunk, and the last thing it checks should be the closest to
the bytes going out.

`mcp.New` refuses a `Guard` when the verifier isn't a `Rechecker`: a
guard that never runs would look like protection and give none. If your
answers need an item-level check, set `Guarded`, and `New` refuses to
start without both.

## Who decides what an agent may do

Only the token says who is calling. The app's verifier turns it into a
`rastrillo.Actor`, and MCP sees that actor and nothing else, whoever
issued the token.

An app can issue its own tokens, as `apitoken`'s personal tokens do. Or
several apps can trust one connection service. There, a person signs in
with an identity provider, which says who they are, and consents at the
connection service, which records what they let an agent do. The two
are separate parties. The service issues tokens limited to one app, and
each carries a `rastrillo.Grant`: the grant's ID, who issued it, who
vouches for the person, the app it is for, and any teams and scopes it
names.

A verifier for a connection service's tokens has three checks to make
before it returns an actor:

1. The token's audience is this app.
2. Its identity issuer is exactly the one the app is configured for.
3. Its subject is an existing local account.

Your app decides how to apply the grant. The framework records it but
never reads its `Teams` or `Scopes`, because what they mean is yours.
Your `Authorize` checks the scope for each operation, `Invoke` checks a
team the input names, and your guard checks it again as the answer goes
out. A grant can only narrow what the person could already do.
Rastrillo ships no verifier for grants yet.

## What it doesn't serve

This is tools only: no resources, prompts or completions. Every request
is one JSON-RPC message in an HTTP POST, answered with plain JSON. Each
request builds its own server, so nothing is kept between requests and
no open stream keeps an instance awake. A GET, a batch or a
`subscriptions/listen` is refused with "Send one JSON-RPC message per
HTTP POST. Do not send batches, use GET, or request subscriptions."

The SDK's DNS-rebinding check is off. Every request must carry a bearer
token before the SDK sees it, and a rebinding page has none. With the
check on, an app behind a proxy on the same machine, such as
`tailscale serve`, would refuse every request.
