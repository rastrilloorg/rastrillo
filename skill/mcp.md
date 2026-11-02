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
