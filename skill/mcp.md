# MCP

**MCP.** `/api/v1/mcp` serves each exposed operation as a tool named
for its ID (`bookmarks.create` is `bookmarks_create`), behind the API's
gate: the same token, policy and `op.Check`, and a refusal is a tool
error with the API's code. The tool list shows a caller only tools
allowed by the token and policy. `Descriptor.Destructive` and `Idempotent` become MCP's
hints; `generate` sets them, and a replaced write body gets
destructive and not idempotent. A write whose ID, after its last `.`,
starts with an `op.FenceVerbs` verb (`delete`, `cancel`, `send`...)
must be `Destructive`, or `mcp.New` fails at boot. MCP runs no schema
check: a hand-written `Invoke` validates its own input and checks rules
that depend on it before acting. Under `RequireAgentSession`, a client
with no `Agent-Session` sees write tools, but each write call is
`invalid`. Claude Code:
`claude mcp add --transport http <app> https://<host>/api/v1/mcp --header "Authorization: Bearer <token>"`.
docs/site/mcp.md

**Connectors need sign-in.** claude.ai, ChatGPT, Claude Code and Codex
connect by address through OAuth, and OAuth needs sign-in: a signed-in
person approves each connection. An app with no sign-in adds it first
(skill/add-signin.md; a new app, `rastrillo new --auth=signin`), then
mounts OAuth as below. The app's token page also requires sign-in.
Never put a secret in the MCP URL (a secret path or a `?token=`), even
when a connector cannot send a header. A URL lands in access and proxy
logs, browser history and the connector's settings, and leaks through
`Referer`. Anyone with the URL can use it. If several people share
it, you must change it for everyone to revoke access.

**OAuth.** To let a person connect Claude Code, Codex, claude.ai or
ChatGPT with only the address, mount `oauth`. Add `oauth.Schema` to
`BootSchema`. Declare `var server *oauth.Server` first, because the two
need each other: `apitoken.Config.Grant` calls `server.Grant`. Build it
with `oauth.New(oauth.Config{...})`, setting `Store`
(`oauth.NewSQLStore(db)`), `Tokens`, `Origin`, `SiteName`, `Session`,
`SignIn`, `Allowed` and `Render`. Set
`api.Config.Challenge` to `server.Challenge("/api/v1")`, and on a copy
of the tree's gate for MCP, `Challenge` to
`server.Challenge("/api/v1/mcp")`. Register `server.Routes()` on the
route table, each `route.PageOnly(r.Reason)`, with the `Browser` routes
inside the session and CSRF group and the rest outside it; an app
without a router calls `server.Mount(mux)`. `Session` reads only the
browser session, never a bearer token. `Render` draws `oauth.Page`;
leave `ReturnURL` a normal `html/template` value, never `template.URL`.
An OAuth actor's session is
`oauth:<connection ID>`, so it needs no `Agent-Session`. List
`server.Connections(ctx, subject)` beside the tokens, with an end button
that calls `server.End`. Clients: `claude mcp add --transport http
<app> https://<host>/api/v1/mcp`, then `/mcp`; `codex mcp add <app>
--url https://<host>/api/v1/mcp`, then `codex mcp login <app>`;
claude.ai and ChatGPT add a custom connector with the address.
docs/site/reference/oauth.md
