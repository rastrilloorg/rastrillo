# OAuth for agents: Rastrillo spec 3

Status: draft for Paul, 2026-11-22.

Context:
- the agent-parity design, `docs/superpowers/specs/2026-11-22-agent-parity-design.md`, §5.5 (access C);
- the MCP design, `docs/superpowers/specs/2026-10-25-mcp-design.md`, which defers OAuth to spec 3;
- Oficina's requirements, `~/.cache/rastrillo-agent-access-notes.md`;
- the MCP authorization specification, version 2026-07-28.

## 1. Why

Today an agent reaches a Rastrillo app with a token that a person makes on a token page, copies, and pastes into the agent's settings. Paul wants to add a site such as patrix.cc to Claude Code, Codex, claude.ai or ChatGPT by its MCP address alone. The client should open his normal browser, he signs in and approves, and the client keeps and renews its own credential. Nobody copies a secret.

Paul brought this forward ahead of grant tokens (2026-11-22). It was step 4 of the agent-parity order and is now the next step. Patrix's standalone sites adopt it first; the dashboard's grants adopt it when they exist.

## 2. Goals, non-goals, anti-goals

Goals:
1. **Any Rastrillo app is its own authorization server** for its API and MCP doors, following the MCP authorization specification (2026-07-28).
2. **All four clients connect:**
   - Claude Code and Codex, which receive the approval on a loopback address on the person's machine;
   - claude.ai and ChatGPT, which receive it at their own web address.
3. **All three ways a client identifies itself are accepted:**
   - a Client ID Metadata Document, which the spec prefers;
   - Dynamic Client Registration, which the spec deprecates but still allows;
   - loopback redirects, in either of those.
4. **One approval page,** in the app's own look, behind the app's own sign-in, that names the client and where the approval goes.
5. **Short-lived access tokens with rotating refresh tokens.** The person sees each connected client, and can end it, on the same page as their tokens.
6. **Nothing downstream changes.** An OAuth token produces the same `rastrillo.Actor` that a personal token does, plus `Grant` facts. Every operation, policy, parity check and human-only refusal works unchanged.
7. **Patrix adopts it on standalone sites,** with a runbook line for each client.

Non-goals:
- **Grant tokens (access B) and the dashboard issuing access to hosted sites.** That is the next spec. This one leaves a seam for it (§5.10).
- **A shared connection service that several apps trust** (Oficina's deployment shape). The MCP design keeps that door open through `api.Verifier`; this spec builds the single-app shape.
- **Being an OpenID Provider.** There are no ID tokens and no userinfo endpoint. Clients need an access token for this app, not an identity.
- **Machine-to-machine clients:** the client-credentials grant and confidential clients with secrets.
- **Idempotency receipts and re-verifying the credential inside a write transaction** (Oficina). Local tokens are revoked in the same database the write uses. Remote grants need these, so they belong to the grants spec.
- **Step-up of an existing connection from read to write** (`insufficient_scope`). A read connection lists only read tools (`mcp.Served`), so a model never calls a write tool. To widen access, the person approves a new connection.

Anti-goals, which would be failures even if the flow works:
- **An agent approving its own access.** Approval is a person, signed in, pressing a button in a browser. Paul's rule makes creating or widening tokens human-only, and this keeps it so.
- **Approval inside an embedded web view.** Oficina requires passkeys to work, so approval happens in the system browser. Rastrillo serves an ordinary page and never offers an embedded flow.
- **An open redirect:** sending an approval, or an error, anywhere other than an address the client registered.
- **Remembered consent that silently reissues access.** Every authorization request shows the page.
- **Accepting a token issued for another resource,** or passing a client's token on to anything else.

## 3. Constraints

- **Human-only rule** (Paul, agent-parity §5.8). The approval page is a page, never an operation, and it accepts only a browser session (§5.9).
- **The person must have signed in recently** to approve, judged by `sessions.Session.AuthTime`, as Patrix's token page already requires (ten minutes there). The app sets the window.
- **The app decides who may connect,** the way `apitoken.Config.Allowed` already does. A hosted Patrix site's team members are refused until grants exist (Patrix plan 0b ruling). Readers are allowed.
- **HTTPS everywhere** except loopback redirect addresses. The spec requires this.
- **Apps served under a path** on a hub's domain (#186; constraints in Rastrillo discussion 328) can't own the domain's root `/.well-known/`. No app runs under a path yet, so this spec serves the path-local addresses (§5.2) but doesn't prove them with a real client (my call). The first app mounted under a path does that, and asks the platform for root forwarding if a client needs it.
- **CARLOS edge.** Every OAuth answer carries `Cache-Control: no-store`. No edge cache across boxes (discussion 328).
- **Copy** goes through Luna: the approval page, refusals, the connected-clients list and the runbook.

## 4. Approaches

1. **Each app is its own authorization server, in a new Rastrillo `oauth` package (chosen by Paul).** It needs nothing outside the app, matches the MCP design's single-app shape, and the dashboard can run the same package for grants later.
2. **An outside identity provider** (Auth0, WorkOS and similar). Rejected: it doesn't know the app's people or roles, its support for Client ID Metadata Documents is uneven, and it adds a vendor.
3. **One shared connection service for many apps,** like Oficina's. Deferred: it's right once several apps share accounts, and approach 1 doesn't block it.

## 5. The design

### 5.1 Package and wiring

A new package, `amadan.net/rastrillo/rastrillo/oauth`. An app builds one `Server` from:
- its database, migrated with the package's schema;
- its origin and base path;
- the resource paths it protects: `/api/v1/mcp`, and `/api/v1`, its JSON tree;
- the token store's prefix, so OAuth tokens look like the app's other tokens;
- its sign-in hooks: the current session, the sign-in address with a return path, and the freshness window;
- `Allowed(ctx, subject)`;
- a renderer for the approval page, so the page wears the app's look;
- a logger and redactor, and a clock;
- an HTTP client for fetching Client ID Metadata Documents, with safe defaults (§5.4).

The server mounts these routes, each `no-store`:

| Route | What it is |
|---|---|
| `GET /.well-known/oauth-protected-resource/api/v1/mcp`, `GET /.well-known/oauth-protected-resource/api/v1` and `GET /.well-known/oauth-protected-resource` | protected-resource metadata (RFC 9728): one per door, and the bare form describing the MCP door (my call) |
| `GET /.well-known/oauth-authorization-server` | authorization-server metadata (RFC 8414) |
| `GET /oauth/authorize`, `POST /oauth/authorize` | the approval page and its answer |
| `POST /oauth/token` | code exchange and refresh |
| `POST /oauth/register` | Dynamic Client Registration (RFC 7591) |
| `POST /oauth/revoke` | token revocation (RFC 7009) |

It also provides a `Verifier` that the app passes to `api.Config.Verify`. It accepts both the app's personal tokens (`apitoken`) and OAuth access tokens, so an app wires one verifier and both doors take both kinds.

### 5.2 Discovery

- **The 401 challenge.** `api.Refuse`'s 401 gains the resource-metadata address and scopes, for example `WWW-Authenticate: Bearer resource_metadata="<origin><base>/.well-known/oauth-protected-resource/api/v1/mcp", scope="read write"`. Each door points at its own document: MCP at the `/api/v1/mcp` one, the JSON tree at the `/api/v1` one. This takes a new field on `api.Config` and `mcp.Config` (or `Gate`). A door without OAuth keeps today's bare `Bearer`.
- **Protected-resource metadata,** one document per door:
  - `resource` is that door's canonical address, `<origin><base>/api/v1/mcp` or `<origin><base>/api/v1`, matching the address the client called (RFC 9728);
  - `authorization_servers` is `[<issuer>]`;
  - `scopes_supported` is `["read","write"]`;
  - `bearer_methods_supported` is `["header"]`.
- **Authorization-server metadata.** The issuer is `<origin><base>`.
  - `authorization_endpoint`, `token_endpoint`, `registration_endpoint`, `revocation_endpoint`;
  - `response_types_supported: ["code"]`;
  - `grant_types_supported: ["authorization_code","refresh_token"]`;
  - `code_challenge_methods_supported: ["S256"]`;
  - `token_endpoint_auth_methods_supported: ["none"]`;
  - `scopes_supported`;
  - `client_id_metadata_document_supported: true`;
  - `authorization_response_iss_parameter_supported: true`.
- **Apps under a path.** The challenge's `resource_metadata` may point anywhere, so it points under the app's own path. For the issuer, clients try `/.well-known/oauth-authorization-server<base>` at the domain root first, then the OpenID form at the root, then `<base>/.well-known/openid-configuration`. The app serves the same metadata at that last address, its own. Clients that stop after the first two can't find it, so the builder must confirm with a real client that an app under a path works. If one client fails, the platform's hub routing is asked to forward the root forms (carloku is the flagship, so we request the feature rather than work around it).

### 5.3 Scopes and access

- **Two scopes.** `read` maps to `rastrillo.Read`, and `write` to `rastrillo.Write`. `write` includes `read`.
- **The person can lower, never raise.** The approval page offers "Read only" or "Read and write", preselected to what the client asked for. A request with no scope gets read.
- **What an OAuth token carries.** The resulting `Actor` has:
  - `Subject`: the person;
  - `Agent`: the client's display name, cut down to `apitoken.ValidAgentName` rules;
  - `Credential`: the access token's public ID;
  - `Access`;
  - `Grant`: `ID` is the connection's ID, `Issuer` and `IdentityIssuer` are the issuer (one app vouches for both today; Oficina asked to keep the two fields distinct), `Audience` is the resource, and `Scopes`.

### 5.4 Clients

- **Client ID Metadata Documents.** A `client_id` that is an `https://` URL with a path is fetched, and must parse to a document with `client_id` (equal to the URL, byte for byte), `client_name` and `redirect_uris`.
  - Fetches are guarded:
    - HTTPS only, to public addresses (loopback, private, link-local and metadata ranges are refused after DNS resolution);
    - no redirects followed;
    - a 5-second timeout;
    - a 64 KiB cap.
  - Documents are cached by their HTTP cache headers, at most 24 hours. A failure is never cached as a success.
- **Dynamic Client Registration.** Registration is open (anyone may register) and rate-limited per address. It accepts public clients only (`token_endpoint_auth_method: none`), `client_name`, `redirect_uris`, `grant_types` and `response_types`, and refuses anything else it can't honour. A registration answers a random `client_id`. Unused registrations are deleted after 30 days.
- **Redirect addresses** must be `https://` or loopback `http://` (`127.0.0.1`, `[::1]` or `localhost`). Other schemes are refused.
  - Matching is exact, except that a loopback address matches on any port with the same path (RFC 8252 §7.3; OAuth 2.1). Codex and Claude Code listen on a port chosen at run time.
- **What the page says about a client:**
  - a document client's name, with its domain;
  - a registered client's name, marked as supplied by the client itself;
  - a loopback redirect, as "an app on this computer".

### 5.5 The approval page

`GET /oauth/authorize` validates the request before showing anything:
- `response_type=code`;
- a known client;
- a `redirect_uri` that matches the client;
- `code_challenge` with method `S256`;
- `resource` equal to one of the app's resources;
- `state`, passed back as-is.

A request that fails before the redirect address is trusted gets an error page, never a redirect. Failures after that redirect with an OAuth `error`, plus `iss`.

The person must be signed in. If not, they go to the app's sign-in with the request as the return path. If their sign-in is older than the app's window, they sign in again. Then `Allowed` decides. A refused person sees why on the page itself, in the app's words; nothing goes back to the client.

The page shows:
- the site's name;
- the client, as in §5.4;
- the host the approval returns to, which the spec requires;
- the access choice;
- that the connection lasts until they end it, or 30 days after its last use (§5.6);
- Approve and Deny.

The POST is CSRF-protected and bound to the GET it answers. Deny redirects with `access_denied`. Approve creates the connection and redirects with a code and `iss`.

### 5.6 Codes and tokens

- **Codes:**
  - single use, valid for 60 seconds, stored as hashes;
  - bound to the client, the exact redirect address, the PKCE challenge, the resource, the scope and the connection.
  - A second use of a code revokes the connection it made (OAuth 2.1).
- **Token endpoint:**
  - `authorization_code` checks the code, the PKCE verifier, `client_id`, `redirect_uri` and `resource`.
  - `refresh_token` rotates the refresh token on every use. If an already-used refresh token comes back, the whole connection is revoked.
  - Answers are `no-store`, with standard OAuth error codes.
- **Access tokens:**
  - one hour;
  - in the app's token format, under its prefix, so redaction and logs treat them like any other token;
  - stored as hashes.
- **Refresh tokens** expire 30 days after their last use. A connection unused for 30 days ends by itself; one in use renews indefinitely until the person ends it.
- **A connection** is one approval: person, client, client name as shown, redirect host, access, created, last used, ended.
  - Ending one ends its refresh and access tokens at once.
  - `Allowed` is checked at every verify and every refresh, so a suspended person loses access within one request.

### 5.7 Verifying on the doors

- **One Verifier for both kinds.** The server's `Verifier` reads the bearer token:
  - a personal token goes to `apitoken`, unchanged;
  - an OAuth access token is checked by hash, with expiry, its connection still live, and `Allowed`.
- **Audience.** The token's audience must be one of this app's two resources. A token issued for either door is valid on both, because the person approved access to the app, not to a protocol (my call). Tokens from any other issuer are refused.
- **The agent session.** Agent-parity §5.3 has every agent write carry `Agent-Session`, which names the session in the logs, and Patrix turns that rule on. claude.ai and ChatGPT can't add headers, and Claude Code under OAuth doesn't. So for an OAuth actor with no header, the gate takes the session from the verifier: `oauth:<connection ID>`. A sent header still wins. Today the gate clears what a verifier sets in `Session`; it changes to keep a verifier's session only for an actor with `Grant` set (my call). A personal token's agent still has to send the header.
- **Recheck.** It implements `api.Rechecker`, so a connection ended mid-answer stops the answer within one chunk, as personal tokens do today.

### 5.8 Revocation and the person's view

- **Clients end connections themselves** at `POST /oauth/revoke`, with either token.
- **People see and end connections in the app.** The package exposes `Connections(subject)` and `End(subject, id)`, and the app lists them next to its tokens: for Patrix, "Connected apps" on `/account/tokens` and `/admin/tokens`.
- **Ending is not human-only.** An agent may end a connection through the app's operations, because ending only removes access (agent-parity §5.8).

### 5.9 Patrix adoption

- **Mount the server on standalone sites only** (my call). Hosted sites mount it with grants, when their team can be served too; until then they keep today's doors.
- **The approval page** uses the same chrome as `/account/tokens`, the public sign-in look, because readers can't reach `/admin`. The freshness window is the token page's ten minutes.
- **`Allowed`** applies the token page's policy: staff and readers.
- **Wire the verifier** into `doors.New` and add the challenge fields.
- **"Connected apps"** goes on `/account/tokens`, with end buttons.
- **Runbook in `docs/deploy.md`:** `claude mcp add --transport http patrix https://patrix.cc/api/v1/mcp`; Codex's equivalent; claude.ai's and ChatGPT's custom-connector steps.
- **Parity ledger.** Every OAuth route is `PageOnly`, with a reason: the approval routes' reason says they are human-only (`route` has no human-only page mark, and this spec doesn't add one; my call). What actually keeps agents out is that the approval page accepts only a browser session, never a bearer token. A test proves it.

### 5.10 The seam for grants

When the dashboard issues grants, it runs the same package with a different connection store. A connection becomes a grant record, and the site verifies the grant's tokens through the roster channel. Section 5.6's connection type is that seam: the package depends on an interface for connections and tokens, not on its own tables directly, so the grants spec can supply a second implementation.

### 5.11 Testing

The whole flow runs in Go tests against a real HTTP server:
- discovery from a bare 401;
- a document client;
- a registered client;
- a loopback redirect on a random port;
- approve, then the MCP tool list, then a write.

Refusals each get a test:
- a wrong PKCE verifier;
- a replayed code, which ends its connection;
- a mismatched redirect;
- a foreign resource;
- a reused refresh token, which ends its connection;
- a stale sign-in;
- a refused person;
- an ended connection mid-answer.

The document fetch is tested against unsafe addresses with a test hook for its dialer.

Real clients last: Claude Code and Codex against dev.patrix.cc, then claude.ai and ChatGPT against patrix.cc, with Paul approving. Each one approves, lists tools and makes a write.

### 5.12 Order

1. Rastrillo `oauth` package, the `api` challenge field, docs, skill. Release.
2. Patrix adoption. Deploy.
3. Paul connects each client.

## 6. Left to the builder

- Table layout and whether OAuth access tokens share `api_tokens` or have their own table, within §5.6 and §5.10.
- How the approval page's request is bound across sign-in, such as a signed continuation like `auth/continuation.go`.
- Rate-limit numbers for registration and the token endpoint.
- How the document fetcher is configured in tests.
- Copy wording, through Luna.
