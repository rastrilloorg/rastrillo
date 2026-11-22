# Agent parity: every Patrix and dashboard action through an API and MCP

**Date:** 2026-11-22
**Status:** approved by Paul 2026-11-22
**Builds on:**
- the JSON API spec: `docs/superpowers/specs/2026-10-05-json-api-design.md`
- the MCP spec, which describes spec 3: `docs/superpowers/specs/2026-10-25-mcp-design.md`
- the multi-instance design, which covers the root, handoff and roster: `docs/superpowers/specs/2026-11-19-multi-instance-apps-design.md`
- Patrix hosted: `patrix/patrix` `docs/superpowers/specs/2026-11-21-patrix-hosted-design.md`

## 1. Why

Paul (2026-11-22): "The Patrix dashboard and any Patrix instance should
be fully driveable by an agent." The trigger was patrix.cc's marketing
pages. The copy was approved, but no agent could publish it: Patrix has
no content API, and an agent can't sign in to `/admin`. Today an agent
can do only what a person does by hand in a browser.

**Success:**
- Anything a person can do in the dashboard (plaza at
  `dashboard.patrix.cc`) or in a Patrix site's admin, an agent can do
  through a JSON API (`/api/v1`) and an MCP server, with the same
  permissions.
- A check fails CI when someone adds a UI action without its agent
  twin. Parity is enforced, not promised. The few human-only actions
  (§5.8) are named in the same check and refuse agents.

## 2. Goals, non-goals, anti-goals

**Goals**

1. **1:1 parity.**
   - **Scope:** every route a signed-in person can use. That covers the Patrix team admin (all 15 areas), the reader's own account actions (subscribe, comment, profile, membership), and the dashboard, including `/fleet` for superadmins.
   - **Contract:** each route is one named operation, served as a page action, as JSON and as an MCP tool.
   - **Reads:** public content gets read operations (list and get posts, pages and collections), so an agent can read what it writes. Any token holder may call them. There are no anonymous, credential-less API calls (my call: Rastrillo's gate then needs no anonymous actor).
2. **Access B now** (Paul, 2026-11-22). Each grant:
   - is created at the dashboard: an agent name, read or write access, all of the person's sites or chosen ones, and an expiry;
   - is accepted by every site it names, which still applies its own roster;
   - is revoked at the root and takes effect everywhere.
3. **Access C by the end of this job** (Paul, 2026-11-22). An MCP client (Claude, Codex) connects with OAuth browser consent, and that issues the same grants. This is Rastrillo's "spec 3".
4. **Standalone Patrix stays drivable.**
   - With no root, an owner mints per-site tokens in the site's own settings. This is option A, which Rastrillo's `apitoken` already does.
   - C covers standalone sites too.
5. **Contribute back.** Every general piece goes into Rastrillo or plaza with its own release: named operations, the parity check, grant tokens and the OAuth service.

**Non-goals**

- A public, anonymous write API.
- Webhooks or event streams.
- Bulk or batch endpoints beyond what the UI does.
- A GraphQL-style query language.
- Agent-only features with no UI twin. Parity runs both ways: anything an agent can do, a person can see and do.

**Anti-goals: failure even if it technically works**

- **UI and API drift apart:** two code paths per action, each with its own validation or permission checks.
- **An agent gets more than its person:** a grant must never exceed what the person's own roster role allows on that site.
- **HTML scraping posing as an API:** answers must be typed JSON with schemas, so MCP tools carry real descriptions and input shapes.
- **Grants that can't be revoked quickly.**

## 3. Constraints

- **Rastrillo already has the shapes.**
  - `op.Descriptor` (`op/op.go:88`) names any operation, with JSON Schema input and result, `Invoke`, a `Confirm` sentence and MCP hints.
  - `api.New(Config, ...Route)` and `mcp.New(Config, ...op.Descriptor)` serve descriptors.
  - `api.Verifier` (`api/tree.go:24`) is the token seam, and `apitoken` gives per-person tokens with an agent name.
  - What's missing is support for hand-written operations beyond the five CRUD verbs, which today come only from generated manifest resources.
- **Parity is enforced only for generated CRUD today.** `rastrillo routes --check` only finds actions no route mounts. No check covers a hand-written route with no operation.
- **Patrix has no manifest.**
  - It has about 305 routes on plain chi, about 170 of which change something, with each admin area mounted by `Mount(r chi.Router)`.
  - Converting it to manifests is out of scope: manifests are being deprecated (Paul, 2026-11-03).
- **plaza** has about 30 actions on the stdlib `ServeMux` and no API.
- **Hosted sites have no local sign-in for the team.** Members arrive only by the root's handoff (`internal/patrix/rooted/mount.go:82-101`). A per-site token minted after a handoff would work, but it isn't B.
- **The root is stateless.** Its records live in objrec, so grants are objects in its store.
- **The roster channel already exists.** The root delivers a signed whole-roster snapshot to each site (`roster`, spec §6.3), and the site applies it to its own roster.
- **The platform rule:** a gap the platform should fill gets requested from carlos/platform, never worked around.

## 4. Approaches considered

**1. Every action becomes an operation (chosen, Paul 2026-11-22).**
- Each UI handler becomes: read the form, call the operation, draw the page. The same operation is served at `/api/v1` and as an MCP tool.
- A new Rastrillo check fails CI when a route that changes something has no operation and no `page-only` reason.
- Cost: the largest refactor, about 170 Patrix actions and about 30 in plaza.

**2. A separate hand-written API calling shared functions.** Rejected. It needs two handlers per action, which drift, and repeats the validation and permission checks.

**3. An automatic form adapter that scrapes HTML into JSON.** Rejected. It has no contract and brittle answers, which works against "fully drivable".

**Access:**
- **B now:** dashboard grants that every site verifies.
- **C last:** OAuth in front of the same grants (Paul).
- **A:** per-site tokens, kept for standalone sites only.

## 5. The design

### 5.1 Named operations (Rastrillo)

- **Any named operation.** An app declares an operation for any action, not only the five CRUD verbs. Its name is `<area>.<verb>`, for example `posts.publish`, `people.invite` or `accounts.suspend`.
- **Declared once:**
  - the Go input and result types;
  - a function `func(ctx, *rastrillo.Ctx, Input) (Result, error)`;
  - a description and an optional `Confirm` sentence;
  - `Destructive` and `Idempotent`.
- **Rastrillo derives the rest:** the JSON Schema for `Input` and `Result` comes from the Go types, plus `Invoke` and the `op.Descriptor`.
- **The prologue and permission check.** `op.Check` runs first, exactly as in generated operations. The app supplies the permission check per operation, through the existing `Authorize` hook, keyed by name.
- **Pages, API and MCP:**
  - The page handler calls the same function.
  - `api.New` serves each operation at `POST /api/v1/<area>/<verb>`, or as GET for reads that declare it.
  - `mcp.New` lists it as tool `<area>_<verb>`.
- **Errors.** Error shapes and validation answers follow the JSON API spec, so a form error and a 422 come from one source.

### 5.2 The parity check (Rastrillo)

- **Route registration.** Routes are registered through a small Rastrillo recorder that works over both chi and `ServeMux`. Each route names its operation, or carries `PageOnly("<reason>")` for routes that truly have no agent meaning (a CSRF-only redirect, an asset).
- **What the test asserts.** A test helper (`paritytest.Routes`) fails when a route has neither, and when a declared operation isn't exposed on both the API and MCP.
- **Reads.** A GET page that shows data names its read operation, and the check covers those too.
- **Run by** the app's own tests, so Patrix's and plaza's CI enforce it. Rastrillo's `routes --check` learns the same rule.
- **End to end.** For hand-written operations, `paritytest` drives one scenario through the page, the API and MCP, as it does for generated CRUD today.

### 5.3 Grants: access B (Rastrillo core and plaza)

- **The grant record** at the root (plaza, objrec):
  - the person's root subject, the grant ID and an agent name;
  - access: `read` or `write`;
  - sites: `all`, or a list of account slugs;
  - created, expires (default 90 days, at most 1 year) and revoked.
- **One token per site, never one for many** (my call, from the builder check).
  - **Why:** a token sent to many sites could be captured by any one of them, for example by an account owner who points their custom domain elsewhere, and then replayed on the person's other sites.
  - **So:** a grant yields one token for each site it names, bound to that site's audience, its platform host. The dashboard shows them at creation. The holder can get a site's token again later through the dashboard operation `grants.site_token`, called with their dashboard token.
  - **Fits MCP:** each MCP server is configured with its own token anyway.
- **The tokens.** Each is shown once.
  - **Format:** a Rastrillo grant token, signed by the root's key (signing context `rastrillo/grant/v1`, the same discipline as `assertion` and `roster`). It carries the subject, grant ID, agent name, access, expiry and **one audience**.
  - **Storage:** only the grant ID is stored. Agents put each token in that server's MCP config as a long-lived bearer token.
- **What a site checks.** A site accepts a token only when **all** of these hold:
  1. the signature verifies with the root's keys;
  2. its audience is this site;
  3. it hasn't expired;
  4. **the grant ID appears in this site's latest roster snapshot for that person.**
- **Revocation in the roster.** Roster snapshots gain each member's active grant IDs. Revoking a grant, removing someone from a site, or suspending a person or account sends a new snapshot. Revocation therefore reaches each site as fast as roster delivery does, and no site needs to call the root.
- **Suspending a person cuts their grants** (my call). Today plaza's `SuspendPerson` tells no site. It will send every site the person belongs to a snapshot without their grant IDs. Resuming restores the grants that are neither revoked nor expired.
- **Rollout** (my call). `roster` refuses unknown fields, and plaza never retries a refused snapshot. So the order is: a Rastrillo release, then `patrix-sites` on it, then the dashboard starts sending grant IDs. All hosted sites run one `patrix-sites` release, so that middle step is a single deploy.
- **What the agent may do.**
  - The request runs as the person (`Actor`: "agent:<name> for user:<subject> grant:<id>"), with the person's current roster role on that site.
  - A `read` grant can call only operations that don't change state.
  - Writes need `Agent-Session` as today (`RequireAgentSession`).
- **The dashboard itself** accepts the same grants for its own operations, such as creating a site, inviting someone or `/fleet`. Superadmin operations still need the person to be a superadmin.
- **Rastrillo's share:** grant token sign and verify, an `api.Verifier` for grant tokens, the roster field for active grants, and the actor wiring.
- **plaza's share:** grant records, the grants page and its operations (create, list, revoke), and putting grants into roster snapshots.

### 5.4 Per-site tokens: access A (standalone sites and readers)

Readers of a hosted site sign in on the site itself and have no dashboard identity, so their agents use per-site tokens too (my call). Any signed-in person mints their own, for their own access, on the site.

A standalone Patrix keeps per-site tokens for everyone. An owner mints one in the site's own settings with `apitoken` (`RequireFresh` re-authentication, agent name, read or write). The same operations, API and MCP apply. C later replaces minting with consent.

### 5.5 OAuth: access C (Rastrillo, last)

- **What C is:** Rastrillo's spec 3, the OAuth 2.1 authorization server an MCP client discovers through `/.well-known/oauth-protected-resource`.
- **Where it runs:** at the root, which consents in a browser and **issues the same grant records**, adding refresh tokens. A standalone site runs it for itself.
- **Oficina's requirements apply** (`~/.cache/rastrillo-agent-access-notes.md`):
  - passkey consent in the system browser;
  - grant re-verification inside write transactions;
  - idempotency receipts committed with the write;
  - separate identity and grant issuers.
- **Its own spec.** C gets a spec of its own when B lands, written against what B actually shipped.

### 5.6 MCP servers

- **One per origin:**
  - the dashboard at `https://dashboard.patrix.cc/api/v1/mcp`;
  - each site at `https://<site>/api/v1/mcp`.
- **Finding sites.** The dashboard's MCP includes `sites_list`, which returns each site the grant reaches, with its MCP URL. With `grants.site_token`, an agent holding a dashboard token can then find and drive every site.
- **Tools by access** (my call). Each server lists only the tools the caller's grant or token can use, so a read grant sees read tools only. About 170 Patrix tools is a lot for a client's context.

### 5.8 Human-only actions (Paul, 2026-11-22)

These refuse any agent, with or without a write grant, at `op.Check`. The parity check accepts them through a `HumanOnly("<reason>")` marking, alongside `PageOnly`:
- creating, widening or extending a grant or a per-site token, so an agent can never raise its own access;
- changing a person's sign-in details or passkeys.

Everything else can be called by an agent with a write grant. That includes `/fleet` when the person is a superadmin, suspending and resuming sites and people, and listing and revoking grants, since revoking only removes access.

### 5.7 Copy

Every new user-facing string goes through Luna (Paul's rule for agent-run work): the grants page, token display, refusals and tool descriptions.

## 6. Order of work, each its own plan and release

Content and per-site tokens come first (my call, from the builder check), so patrix.cc's marketing pages can be published early.

0. **Rastrillo, then Patrix content:**
   - named operations (§5.1);
   - the route recorder and parity check (§5.2);
   - human-only marking (§5.8).
   - Release. Then Patrix content operations (posts, pages, collections, media) and per-site tokens (A), which publish the marketing pages.
1. **Rastrillo:**
   - grant tokens, the verifier and the roster field (§5.3).
   - Release.
2. **plaza:**
   - its ~30 actions as operations, with API, MCP and parity;
   - grants: records, page, operations and roster delivery;
   - `sites_list`.
   - Release.
3. **Patrix, area by area**, each area landing with its parity test green:
   1. people and access;
   2. members, comments, email, feeds, templating, branding, redirects, terms, trash, analytics and imports;
   3. grants (B) on hosted sites.
4. **Rastrillo:** OAuth, access C (§5.5), adopted by plaza and Patrix.

The first visible result (step 0) is that an agent publishes patrix.cc's marketing pages through Patrix's content operations, using a per-site token. patrix.cc is standalone, so that's option A.

## 7. Left to the builder

- The exact Go API of the operation declaration and the route recorder. Keep them as small as `action.Routes` and `op.Descriptor` allow.
- JSON Schema derivation details, using the struct tags the JSON API spec already uses.
- Which GET pages count as "showing data" in the parity check. Err towards including them.
- The order of Patrix areas after content.
