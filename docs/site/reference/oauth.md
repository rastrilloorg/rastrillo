# 🤖 oauth

`amadan.net/rastrillo/rastrillo/oauth`

Lets a person connect an agent to the app with only its MCP address.
The client finds the app's authorization server from a 401, opens the
person's browser at the app's approval page, and keeps and renews its
own token. Nobody copies a secret. The app is its own authorization
server, as the MCP authorization specification (2026-07-28) describes.
Claude Code, Codex, claude.ai and ChatGPT connect this way.

The app needs sign-in first: a signed-in person approves each
connection, so `Session` and `SignIn` are required. An app without
sign-in adds it, with [the default sign-in](/docs/signin) or
[passwords](/docs/passwords), before it mounts `oauth`. Don't put a
secret in the MCP URL instead, even for a client that can't send a
header. A secret URL can appear in logs, browser history and `Referer`
headers. Anyone with the URL can use it. If several people share it,
you must change it for everyone to revoke access.

## Wiring it into an app

The server mints its access tokens with the app's `apitoken` store, and
the store asks the server what each of those tokens may do. Each needs
the other, so the store's `Grant` calls the server through a variable
set later:

```go
var server *oauth.Server
tokens, err := apitoken.New(apitoken.Config{
	DB:     d.Writer(),
	Prefix: "notes",
	Grant: func(ctx context.Context, connection string, access rastrillo.Access) (*rastrillo.Grant, error) {
		return server.Grant(ctx, connection, access)
	},
})
if err != nil {
	return err
}
server, err = oauth.New(oauth.Config{
	Store:    oauth.NewSQLStore(d.Writer()),
	Tokens:   tokens,
	Origin:   "https://notes.example.com",
	SiteName: "Notes",
	Session:  currentSession,
	SignIn:   signInURL,
	Allowed:  mayConnect,
	Render:   renderApproval,
})
if err != nil {
	return err
}

apiConfig := api.Config{
	Verify:    tokens,
	Ctx:       ctxFactory,
	Authorize: op.DefaultPolicy,
	Redact:    tokens.Redact,
	Challenge: server.Challenge("/api/v1"),
}
tree, err := api.New(apiConfig, gen.APIRoutes()...)
if err != nil {
	return err
}
gate := apiConfig.Gate()
gate.Challenge = server.Challenge("/api/v1/mcp")
tools, err := mcp.New(mcp.Config{
	Gate:      gate,
	Ctx:       apiConfig.Ctx,
	Authorize: apiConfig.Authorize,
	Name:      "notes",
	Version:   rastrillo.BuildVersion,
}, gen.ExposedOperations()...)
if err != nil {
	return err
}
mux.Handle("/api/v1/", tree)
mux.Handle("/api/v1/mcp", tools)
server.Mount(mux)
```

Add `oauth.Schema` to `BootSchema` in `migrations.go`, beside
`apitoken.Schema`. Both doors use the same verifier: `apitoken` checks
personal tokens and OAuth access tokens alike.

Each door gets its own challenge. The MCP door's 401 then says:

```text
WWW-Authenticate: Bearer resource_metadata="https://notes.example.com/.well-known/oauth-protected-resource/api/v1/mcp", scope="read write"
```

## New and Config

```go
func New(c Config) (*Server, error)

type Config struct {
	Store    Store
	Tokens   *apitoken.Store
	Origin   string // "https://notes.example.com"
	Base     string // "" or "/tenant"
	SiteName string
	Session  func(*http.Request) (sessions.Session, bool)
	SignIn   func(returnTo string, reauth bool) string
	Fresh    time.Duration
	Allowed  func(ctx context.Context, subject string) (ok bool, refusal string, err error)
	Render   func(w http.ResponseWriter, r *http.Request, page Page) error
	Fetcher  *Fetcher
	Hops     int
	Logger   *slog.Logger
	Now      func() time.Time
}

func (s *Server) Issuer() string
```

`Store` keeps clients, approvals, connections, codes and refresh tokens:
use `NewSQLStore` on a database migrated with `Schema`. `Tokens` is the
app's `apitoken` store. Access tokens are its tokens, under the app's
prefix, so redaction and logs treat them like any other.

`Origin` is the scheme and host the app is reached at, and `Base` the
path it is mounted under, if any. `Issuer` combines `Origin` and
`Base`. Every address the server publishes starts with it.

`Session` returns the signed-in person, from the browser session only,
never from a bearer token: the approval page is for a person.
`SignIn(returnTo, reauth)` is where to send someone who must sign in,
or sign in again when `reauth` is set, to come back to `returnTo`, a
local path with its query. `Fresh` is how recent a sign-in must be to
approve; zero means ten minutes.

`Allowed` decides who may connect apps. It runs on the approval page,
at every check of a connection's token and at every refresh, so a
person it refuses loses access within one request. A refusal shows
`refusal` on the page, or the package's own words when it is empty. An
error is a 503 on the doors.

`Render` draws the [approval page](#the-approval-page) with the app's
look. `Fetcher` reads client metadata documents; nil uses
`NewFetcher`. `Hops` is how many proxies sit in front of the app, for
the registration rate limit's [clientip](/docs/reference/clientip).
`Logger` defaults to `slog.Default()` and `Now` to `time.Now`.

`New` returns an error when `Store`, `Tokens`, `Session`, `SignIn`,
`Allowed` or `Render` is nil; when `Origin` is not an HTTPS origin or
an HTTP origin on a loopback address; or when `Base` does not start
with a slash, ends with one, or has a segment that is empty, `.`, `..`,
or contains anything but letters, digits and `-._~`.

## Routes and Mount

```go
func (s *Server) Routes() []Route
func (s *Server) Mount(mux *http.ServeMux)

type Route struct {
	Method, Pattern string       // Pattern includes Base
	Handler         http.Handler // no-store already applied
	Reason          string       // a PageOnly reason for an app's route ledger
	Browser         bool         // needs the app's session and CSRF middleware
}
```

| Route | What it is |
|---|---|
| `GET /.well-known/oauth-protected-resource/api/v1/mcp` | the MCP door's resource metadata (RFC 9728) |
| `GET /.well-known/oauth-protected-resource/api/v1` | the JSON door's |
| `GET /.well-known/oauth-protected-resource` | the MCP door's resource metadata, for clients that ask without a path |
| `GET /.well-known/oauth-authorization-server` | the server metadata (RFC 8414) |
| `GET /.well-known/openid-configuration` | the same, where some clients look |
| `GET /oauth/authorize` | the approval page |
| `POST /oauth/authorize` | the person's answer |
| `POST /oauth/token` | a code or refresh token for new tokens |
| `POST /oauth/register` | client registration (RFC 7591) |
| `POST /oauth/revoke` | a client ends its connection (RFC 7009) |

Every answer has `Cache-Control: no-store`. The approval page also
refuses to be framed by another site.

`Routes` lists them for an app with a [route table](/docs/reference/route).
Register each at its `Pattern`, marked `route.PageOnly(r.Reason)`. Put
the two `Browser` routes inside your session and CSRF group, and the
rest outside it: clients call them from other origins with no cookie.

`Mount` registers every route on a `ServeMux`, for an app without a
router. It adds no session or CSRF middleware, so `Session` must read
the session itself. The `POST /oauth/authorize` route checks that the
request came from the app's own origin in either setup.

An app under a path serves its metadata under that path. Some clients
look only at the domain root, so check that each client you rely on
connects.

## Challenge and Grant

```go
func (s *Server) Challenge(door string) api.Challenge
func (s *Server) Grant(ctx context.Context, connection string, access rastrillo.Access) (*rastrillo.Grant, error)
```

`Challenge` is what a door's 401 tells a client: the address of the
door's resource metadata, and the scopes `read write`. Pass
`Challenge("/api/v1")` as `api.Config.Challenge`, and
`Challenge("/api/v1/mcp")` as the MCP gate's.

Use `Grant` for `apitoken.Config.Grant`. It returns an error wrapping
`api.ErrUnauthorized` for an ended connection, a resource this app
does not serve, or a person whom `Allowed` refuses. A store that fails
is an error wrapping `api.ErrUnavailable`. The grant's `ID` is the
connection's, `Issuer` and `IdentityIssuer` are the server's issuer,
and `Audience` is the resource the person approved. `Scopes` follows
the token's own `access`: `read`, or `read` and `write`. A refresh can
narrow a token below its connection's access, and `Grant` reflects
that.

The `Agent` of an OAuth token's actor is the client's name when
`apitoken.ValidAgentName` accepts it; otherwise, it is `"Agent"`. Its
`Session` is `oauth:<connection ID>`. When a client omits
`Agent-Session`, the gate uses that session, so `RequireAgentSession`
passes. If the client sends the header, its value takes precedence. A
token issued for either door works on both: the person approved access
to the app, not to one protocol.

## The approval page

```go
type Page struct {
	Kind         string // "approve" or "error"
	SiteName     string
	ClientName   string
	ClientDomain string
	SelfNamed    bool
	Loopback     bool
	ReturnHost   string
	Requested    rastrillo.Access
	Pending      string
	Error        string
	ReturnURL    string
}
```

`GET /oauth/authorize` checks the request before it shows anything. A
person who isn't signed in goes to `SignIn`, and one whose sign-in is
older than `Fresh` signs in again. Then `Allowed` decides. Every
problem shows the error page; nothing redirects to the client until
the person answers.

For `"approve"`, show:

- `SiteName`;
- the client: `ClientName` with `ClientDomain`, the host of its metadata
  document, or, when `SelfNamed` is set, the name marked as one the
  client gave itself;
- `ReturnHost`, where the approval returns, or "an app on this
  computer" when `Loopback` is set;
- a choice of read only, or read and write, starting at `Requested`;
- that the connection lasts until the person ends it, or 30 days after
  it was last used;
- Approve and Deny.

The form posts to `<Base>/oauth/authorize` with `pending` (the page's
`Pending`), `decision` (`approve` or `deny`) and `access` (`read` or
`write`). The person can lower the access, never raise it. Approve
redirects to the client with a code. Deny shows the `"error"` page with
a link back to the client carrying `access_denied`. Anyone can
register a redirect, so the person goes there only by choosing to.

For `"error"`, show `Error`. When `ReturnURL` is set, link to it as
"Return to `ReturnHost`". It is the client's address, so escape it as
any other value: `html/template` does, unless you mark it
`template.URL`, which you must not.

`Render` may write the body without a status: the package sends the
right one.

## Clients

A client is one of two kinds:

- **A metadata document.** Its `client_id` is an `https://` address
  with a path, which serves a JSON document whose `client_id` is that
  address, with `client_name` and `redirect_uris`. The fetch goes to
  public addresses only, checked after DNS resolution, over HTTPS, with
  no redirects, a 5-second timeout and a 64 KiB cap. A document is
  cached for its `Cache-Control: max-age`, at most 24 hours. A failed
  fetch is never cached. Only public clients can connect, so a document
  is refused if it has a `client_secret` or a
  `token_endpoint_auth_method` other than `none`.
- **Registered.** Anyone may register at `POST /oauth/register`, ten
  times a minute per address, or per /64 for IPv6. It takes public
  clients only, with no secret. The response lists both grant types
  regardless of what was requested, and refuses requests naming any
  other grant type. A registration unused for 30 days is deleted.

A redirect address must use `https://`, or `http://` with host
`127.0.0.1`, `[::1]` or `localhost`, and must not include a fragment,
whitespace, quotes or angle brackets. It must match one the client
listed exactly, except for a loopback address's port: Claude Code and
Codex listen on a port they pick when they start.

```go
type Client struct {
	ID, Name     string
	Redirects    []string
	Document     bool
	RegisteredAt time.Time
	UsedAt       time.Time
}

func NewFetcher() *Fetcher
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Client, error)
```

## Codes and tokens

- A code works once, within 60 seconds. The exchange sends
  `client_id`, `code_verifier` and `resource`. `redirect_uri` may be
  left out, as OAuth 2.1 allows, but must match if sent.
- An exchange that fails a check, such as a wrong `code_verifier`,
  changes nothing, and the code still works. A second exchange with the
  same code that passes every check ends the connection. A used code is kept only
  until its 60 seconds are up, so a later replay is refused without
  ending anything.
- An access token lasts one hour.
- A refresh token lasts 30 days from its last use, and is replaced on
  every use. Using a replaced refresh token ends its connection, even
  after the token's 30-day lifetime, for as long as the connection
  lives. A connection unused for 30 days ends by itself; one
  in use renews until the person ends it.
- `POST /oauth/revoke` with either token and the client's `client_id`
  ends the connection. Another client's `client_id` is refused. A token
  the server doesn't know is a 200 all the same.

## Connections and End

```go
func (s *Server) Connections(ctx context.Context, subject string) ([]Connection, error)
func (s *Server) End(ctx context.Context, subject, id string) error

type Connection struct {
	ID, Subject, ClientID, ClientName, ReturnHost string
	Resource          string
	Access            rastrillo.Access
	Created, LastUsed time.Time
	Ended             *time.Time
}
```

A connection is one approval. `Connections` lists a person's live ones,
newest first, for the app to show beside its tokens. `End` ends one and
its tokens at once. It returns `ErrNotFound` for a connection that
isn't the person's: answer 404. Ending a connection only removes
access, so it need not be human-only.

## Store and Schema

```go
type Store interface { /* see the source */ }
func NewSQLStore(db *sql.DB) Store
var Schema *migrate.Set
var ErrNotFound = errors.New("oauth: not found")

type Pending struct { /* an approval waiting for the person */ }
type Code struct { /* an authorization code, held by its hash */ }
type Refresh struct { /* a refresh token, held by its hash */ }
```

`Schema` holds the package's tables, and depends on no other package's.
Codes and refresh tokens are stored only as SHA-256 hashes. A future
shared-grant design can add a second `Store` implementation. The store
holds `Pending`, `Code` and `Refresh` values. `ErrNotFound` covers
values that are absent, expired, ended or do not belong to the caller.

## Connecting a client

Give the client the MCP address. It opens the browser at the approval
page when it first connects.

- **Claude Code:**
  `claude mcp add --transport http notes https://notes.example.com/api/v1/mcp`,
  then `/mcp` in a session to sign in.
- **Codex:**
  `codex mcp add notes --url https://notes.example.com/api/v1/mcp`,
  then `codex mcp login notes`.
- **claude.ai and ChatGPT:** add a custom connector with the address
  `https://notes.example.com/api/v1/mcp`.

The person sees the connection in the app, and can end it there.
