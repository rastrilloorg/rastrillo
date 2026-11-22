# 🤖 rootcall

Use `rootcall` to call an instance from its root app. For example, push the account's plan or read its daily usage. The root signs each request. The instance runs the handler only for a request the root signed for this instance, with this method, path, query and body. The call must not have expired, and it can run the handler only once.

`amadan.net/rastrillo/rastrillo/rootcall`

A call does not identify a person. In the handler, check that the root may ask for this.

## On the instance

```go
setup, err := instance.FromEnv(os.Getenv)
// ...
guard, err := rootcall.New(rootcall.Config{
	Root: setup.Root,
	Keys: setup.Keys,
	// The platform host, even when the instance serves on a custom domain.
	Audience: "https://" + carlos.Host(),
	DB:       db,
})
// ...
mux.Handle("POST /_carlos/acme/plan", guard.Wrap(planHandler))
mux.Handle("GET /_carlos/acme/usage", guard.Wrap(usageHandler))
```

Merge `rootcall.Schema` into `BootSchema`. Exempt the paths from CSRF with `csrf.Exempt`. The root's request is cross-site, and its signature protects it.

Serve calls under `/_carlos/`, in a segment named for your app, such as `/_carlos/acme/plan`. `instance.RedirectToOrigin` redirects paths outside `/_carlos/` on the platform host to the custom domain. A redirected call is refused. Rastrillo uses the other paths under `/_carlos/`.

```go
const Scheme = "Rootcall"

type Config struct {
	Root     string
	Keys     map[string][]byte
	Audience string
	DB       *sql.DB
	MaxBody  int64
	Now      func() time.Time
	Logger   *slog.Logger
}

var Schema *migrate.Set

func New(config Config) (*Guard, error)
func (g *Guard) Wrap(next http.Handler) http.Handler
func FromContext(ctx context.Context) (Claims, bool)
```

`Wrap` reads the whole body before the handler runs, up to `Config.MaxBody` (1 MiB if zero). The handler reads the same bytes from `request.Body`. Use `FromContext` inside the handler to read the call's claims.

| Status | Meaning |
| --- | --- |
| Your handler's | The call was accepted. |
| 400 | The body could not be read. |
| 403 | No `Authorization: Rootcall` header, a token that does not verify, a method, target or body that is not the one signed, or a call already used. |
| 404 | The request came in on a host other than the audience's. Nothing is read. |
| 413 | The body is over `Config.MaxBody`. |
| 500 | The database failed. The handler did not run. |

Each call can be used once. The guard records the call's ID in the instance's database before the handler runs, so a call is used up even if the handler fails. Every other check runs first, so a forged or altered request does not use up the real one. Old IDs are deleted as new calls arrive. You do not need to schedule a cleanup.

The guard compares the request target as it arrived (`Request.RequestURI`), not `URL.Path`. A guard behind `http.StripPrefix` or `basepath` still sees the path the root signed. Configure any proxy between the root and the instance to forward the path and query unchanged. A call with an altered path or query is refused.

A call whose body arrives after the call expires is refused, even if the guard began reading it in time.

## On the root

```go
caller := rootcall.Caller{Origin: "https://example.com", Key: key, KeyID: "k1"}
request, err := caller.NewRequest(ctx, http.MethodPost,
	"https://acme.example.net/_carlos/acme/plan", body)
// ...
response, err := client.Do(request)
```

```go
type Caller struct {
	Origin string
	Key    *crypto.Keypair
	KeyID  string
	Now    func() time.Time
}

func (c Caller) NewRequest(ctx context.Context, method, target string, body []byte) (*http.Request, error)
```

`NewRequest` signs a call to `target`, an `https://` URL on the instance's platform host. The call is valid for that host and has a 60-second lifetime. Send each request once. To retry, build a new request: the instance refuses a call it has already seen. Make the handler idempotent, for example with a sequence number, because a retry can arrive after the first call was applied.

Use `handofftest.Root.Call` to sign requests as the fake root in tests. See [`handofftest`](/docs/reference/handofftest).

## Tokens

```go
type Claims struct {
	Version, IssuedAt, ExpiresAt int64
	Issuer, Audience, KeyID, ID  string
	Method, Target, BodyHash     string
}

func Sign(key *crypto.Keypair, claims Claims) (string, error)
func BodyHash(body []byte) string
func NewVerifier(issuer, audience string, keys map[string][]byte) (*Verifier, error)
func (v *Verifier) Verify(token string, now time.Time) (Claims, error)
```

Use `Sign` only if you build requests yourself. A token is the JSON payload and its P-256 signature, each in unpadded base64url, joined by a dot. The token is signed under the context `rastrillo/rootcall/v1`. A handoff assertion or roster snapshot signed by the same key is not accepted as a call. A call is not accepted as either.

The payload's fields:

| Field | Value |
| --- | --- |
| `v` | `1` |
| `iss` | The root's origin. |
| `aud` | The instance's platform origin. It cannot be the root's. |
| `kid` | The signing key's id. |
| `jti` | 16 random bytes in base64url, from `assertion.RandomID`. |
| `iat`, `exp` | Unix seconds. A call has a lifetime of at most 120 seconds. |
| `method` | Upper case, such as `POST`. |
| `target` | The path and query as sent, such as `/_carlos/acme/usage?since=2026-11-01`. |
| `body` | `BodyHash` of the body: SHA-256 in unpadded base64url. A call with no body hashes zero bytes. |

`Verify` checks the signature, issuer, audience, version and lifetime. A call issued up to 30 seconds in the future is accepted, to allow for clock skew. `Verify` does not compare the request or record the ID, so a call checked only with `Verify` can be replayed. Use `Guard`.

## Errors

You can log errors as they are. They contain no submitted values.

- `ErrMissing`: there is no `Authorization: Rootcall` header, or there are two.
- `ErrMalformed`: the token is not shaped like a call, or is over 8 KiB.
- `ErrVersion`: the version is not `1`.
- `ErrUnknownKey`: the key id is not one the instance trusts.
- `ErrSignature`: the signature does not match.
- `ErrIssuer`, `ErrAudience`: the call's issuer or audience is not the one this instance expects.
- `ErrLifetime`, `ErrExpired`: the lifetime is over 120 seconds, the call was issued more than 30 seconds in the future, or it has expired.
- `ErrClaims`: a field is missing, unknown or out of range.
- `ErrBinding`: the method, target or body is not the one signed.
- `ErrReplay`: the call was already used.
- `ErrConfig`: `New` or `NewVerifier` was given a configuration it cannot use.
- `ErrSigning`: `Sign` was given a key it cannot sign with.
