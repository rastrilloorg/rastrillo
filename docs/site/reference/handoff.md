# 🤖 handoff

Use `handoff` to sign members in to an instance by way of the root app. The instance sends the browser to the root, the root says who it is, and the instance starts a session.

`amadan.net/rastrillo/rastrillo/handoff`

## Mount it

```go
setup, err := instance.FromEnv(os.Getenv)
// ...
entry, err := handoff.New(handoff.Config{
	Root:      setup.Root,
	Keys:      setup.Keys,
	Origin:    "https://acme.example.com",
	Sessions:  sessions,
	DB:        db,
	Suspended: receiver.Suspended,
})
// ...
mux.HandleFunc("GET "+handoff.EnterPath, entry.Enter)
mux.HandleFunc("POST "+handoff.EnterPath, entry.Consume)
```

Do these four things in your app:

- Exempt both cross-site paths from CSRF: `csrf.Exempt(handoff.EnterPath, roster.Path)`. The root's POST back is cross-site by design. A signature protects it, not the same-origin check.
- Merge `handoff.Schema` and `roster.Schema` into `BootSchema`, so the tables exist.
- Wrap the signed-in routes with `entry.Renew`.
- Call `entry.Sweep(time.Now())` from the daily tick.

## Config

```go
type Config struct {
	Root          string
	Keys          map[string][]byte
	Origin        string
	Sessions      *sessions.Sessions
	DB            *sql.DB
	MaxAge        time.Duration
	Suspended     func(context.Context) (bool, error)
	Now           func() time.Time
	Logger        *slog.Logger
	RenderRefusal func(http.ResponseWriter, *http.Request, RefusalPage)
}

func New(config Config) (*Handoff, error)
```

`Root` and `Keys` come from the `instance.Setup`. `Keys` is the root's public keys, read from `RASTRILLO_ROOT_KEYS`: one or two comma-separated `<kid>.<base64url>` values with distinct kids. `Origin` is this instance's own origin. `New` returns `ErrConfig` if `Sessions` or `DB` is missing, if `Root` or `Origin` is malformed, if `Root` equals `Origin`, if `Keys` is empty or has more than 8 keys, or if `Sessions` is in signed mode. Path tenants use the hub's session instead of handoff.

`MaxAge` is how long a handoff session lasts before it goes back through the root. It defaults to one hour. `Suspended` is optional: pass `receiver.Suspended` from [`roster`](/docs/reference/roster), and a suspended account is refused at sign-in. `Now` and `Logger` default to `time.Now` and `slog.Default()`.

## Enter and Consume

```go
const EnterPath = "/_carlos/enter"
const Method = "handoff"

func (h *Handoff) Enter(w http.ResponseWriter, r *http.Request)
func (h *Handoff) Consume(w http.ResponseWriter, r *http.Request)
```

`Enter` answers GET and HEAD. It sets a short-lived nonce cookie for this browser and sends it to the root with a 303. `?next=` says where to land afterwards, and only a local path is kept. A `next` that would make the nonce cookie longer than 2048 bytes becomes `/`, because browsers drop a cookie that is too large.

`Consume` answers POST. It checks the root's signed assertion, that the nonce is this browser's, that the account is not suspended, and that the assertion has not been used before. Then it signs the person in with `Method` `"handoff"` and sends them to `next`. A check that fails never spends the assertion.

Both answer only on the instance's own origin. A path-routed request (`basepath.PathRouted`) gets a 404. On any other host, `Enter` sends the browser to the same URL on `Origin` with a 303 and sets no cookie, and `Consume` answers 404 and changes nothing. Hosts are compared as `basepath.SameHost` compares them.

## Renew

```go
func (h *Handoff) Renew(next http.Handler) http.Handler
```

Wrap the signed-in routes with `Renew`, inside `sessions.Middleware`. When a handoff session exceeds `MaxAge`, `Renew` sends the member back through the root. A member the root has removed or suspended loses access within that window. For a GET or HEAD request, `Renew` redirects the member to `EnterPath`. After signing in again, the member returns to the same page. Any other method gets a 401 with no body, because a redirect would drop what was submitted.

`Renew` lets `EnterPath` through itself, so a stale person can reach the route that renews them. `Renew` leaves requests with other session types and path-routed requests unchanged.

## Sweep

```go
func (h *Handoff) Sweep(now time.Time) error
```

Each assertion can be used once, and the instance remembers the ones it has used. `Sweep` deletes the ones that have expired. Call it from the daily tick. A missed run only leaves the table larger.

## RenderRefusal

```go
type RefusalPage struct {
	Title    string
	Message  string
	LinkText string
	LinkURL  string
}
```

A refused handoff shows a page: either "We couldn't verify that link", with a link back to the root, or, for a suspended account, "This account is suspended", with no link. Set `Config.RenderRefusal` to draw it in your app's layout. Your function must answer with `http.StatusForbidden`; `Cache-Control: no-store` is already set. `LinkText` and `LinkURL` are empty when there is nowhere useful to send the person. Left nil, the page is plain.

A fault of the instance's own, such as a database error, is a 500 and not a refusal page.

## Errors

Errors contain no submitted values, so you can log them as they are. For testing, see [`handofftest`](/docs/reference/handofftest).
