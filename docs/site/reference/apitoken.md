# 🤖 apitoken

`amadan.net/rastrillo/rastrillo/apitoken`

Personal API tokens. A person makes a token for an agent, such as
Claude, or for their own script. The agent sends it on every API call,
and the call acts as that person. [JSON API](/docs/api#tokens) shows
the store and the token page wired into an app.

## The token

```
tickets_k3Jx9QpL2mWa_<43 characters>
```

A token has three parts: the app's prefix, a public ID and a secret.
The public ID is safe to log and to show. The secret is shown once,
when the token is made. The database stores only its SHA-256.

## New, Config and Schema

```go
func New(cfg Config) (*Store, error)
type Config struct {
	DB      *sql.DB
	Prefix  string // ^[a-z][a-z0-9]{0,15}$
	Allowed func(ctx context.Context, subject string) (bool, error)
	Now     func() time.Time
}
func DerivePrefix(appName string) string
```

`DB` and `Prefix` are required. `DerivePrefix` makes a prefix from your
app name. Do not change the prefix later: every token already made
stops working.

Set `Allowed` to refuse tokens whose owner can no longer use the app,
such as a suspended account. With no `Allowed`, every owner is allowed.

Merge `Schema` into your boot set. It adds one table, `api_tokens`.

## Making and managing tokens

```go
func (s *Store) Mint(ctx context.Context, subject string, in MintInput) (secret string, t Token, err error)
type MintInput struct {
	Agent   string        // "Claude"
	ForSelf bool          // a token for the person, not an agent
	Access  rastrillo.Access
	TTL     time.Duration // 0 means never
}
func ValidAgentName(name string) (string, bool)

func (s *Store) List(ctx context.Context, subject string) ([]Token, error)
func (s *Store) Revoke(ctx context.Context, subject, id string) error
func (s *Store) RevokeAgent(ctx context.Context, subject, agent string) (int64, error)
func (s *Store) RevokeAll(ctx context.Context, subject string) (int64, error)
type Token struct {
	ID, Subject, Agent             string
	Access                         rastrillo.Access
	CreatedAt                      time.Time
	LastUsed, ExpiresAt, RevokedAt *time.Time
}
func (t Token) Actor() rastrillo.Actor
```

Show the secret `Mint` returns once, in the response to the form that
made it. Never put it in a URL, a redirect, a flash message or the
session.

`Token.Actor` is who the token acts as: the person, the agent if there
is one, the token as the credential, and its access. It is what
`Verify` answers for a request on the token.

Name the agent in `Agent`. An agent name is 1 to 40 letters, digits,
spaces, hyphens and dots. `ValidAgentName` checks one. For a token that
is not for an agent, set `ForSelf` and leave `Agent` empty.

Take `subject` from `sessions.Current`, never from the form. Every
method only reads or changes that person's tokens. `Revoke` returns
`sql.ErrNoRows` for a token that is not theirs: answer 404.

`List` returns revoked and expired tokens too, newest first.

## The token page

```go
func NewHandlers(cfg HandlersConfig) (*Handlers, error)
type HandlersConfig struct {
	Store  *Store
	Path   string // the page's path; revokes redirect there
	Render func(w http.ResponseWriter, r *http.Request, d PageData)
	Name   func(ctx context.Context, subject string) string
}
func (h *Handlers) Page(w http.ResponseWriter, r *http.Request)        // GET
func (h *Handlers) Create(w http.ResponseWriter, r *http.Request)      // POST
func (h *Handlers) Revoke(w http.ResponseWriter, r *http.Request)      // POST, field "id"
func (h *Handlers) RevokeAgent(w http.ResponseWriter, r *http.Request) // POST, field "agent"
func (h *Handlers) RevokeAll(w http.ResponseWriter, r *http.Request)   // POST

type PageData struct {
	Tokens  []Row
	Agents  []string          // each agent with a live token, once
	Created *Created          // set only by a successful Create
	Errors  map[string]string // "agent"
	Form    map[string]string // agent, for_self, access, expiry
	CSRFOK  bool
}
type Row struct {
	Token
	Display          string // "Claude for Paul"
	Revoked, Expired bool   // against the store's clock
}
type Created struct {
	Secret string
	Token  Row
}
```

The handlers take the signed-in person from `sessions.Current` and
answer 403 without one. They set `Cache-Control: no-store` on every
response. Mount them under your session router, behind CSRF, and mount
`Create` behind `sessions.RequireFresh(10 * time.Minute)` as well.

`Render` draws the page. Call `ui`'s `api-tokens` partial with
`PageData` as `Page` and the page's path as `Path`, under your own
`page-header`; see [Templates](/docs/templates#the-partials).
`Row.Display` is the `actor` phrase for the token, in the reader's
language. Set `Name` to turn a subject into a person's name; without
it, the page shows the subject. `Row.Revoked` and `Row.Expired` say a
token's state against the store's clock, and `Agents` lists each agent
with a live token once, for one "revoke all for this agent" button per
agent.

`Create` reads the form fields `agent`, `for_self`, `access` (`read` or
`write`) and `expiry` (`30`, `90`, `365` or `never`). It answers 200
with `Created` set, in that response only. An empty agent name without
`for_self` answers 422 with `Errors["agent"]` and the form echoed. A
value the form never offers answers 400.

The revokes redirect to `Path` with 303. `Revoke` answers 404 for a
token that is not the caller's.

## Verify and Redact

```go
func (s *Store) Verify(ctx context.Context, r *http.Request) (rastrillo.Actor, error)
func (s *Store) Redact(text string) string
```

Pass the `Store` as `api.Config.Verify`, and `Store.Redact` as
`api.Config.Redact`.

`Verify` reads only `Authorization: Bearer <token>`. A missing, wrong,
revoked or expired token gets the same 401. So does an owner `Allowed`
refuses. A database error, or an error from `Allowed`, is a 503.
It records when each token was last used, at most once a minute.

`Redact` replaces each token's secret in a log line with `[redacted]`.
The prefix and public ID stay, so the log still shows which token it
was.
