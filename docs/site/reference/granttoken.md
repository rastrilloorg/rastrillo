# 🤖 granttoken

`amadan.net/rastrillo/rastrillo/granttoken`

Use `granttoken` to let an agent act for one person on one hosted site. The root app signs a bearer token that names the person, the agent and the site. The token stops working when it expires or when the site's latest roster no longer lists the grant. A verified token says only what the root granted when it signed it. The site still checks the grant against its latest roster on every request.

## Mount it on a hosted site

```go
// The platform host, even when the instance serves on a custom domain.
platformHost := "https://" + carlos.Host()
verifier, err := granttoken.NewVerifier(setup.Root, platformHost, setup.Keys)
// ...
door, err := granttoken.NewDoor(granttoken.DoorConfig{
	Verifier: verifier,
	Grants:   receiver, // the *roster.Receiver
})
// ...
tokens := door.Or(personalTokens) // personalTokens is the app's *apitoken.Store

apiConfig := api.Config{
	Verify: tokens,
	Redact: tokens.Redact,
	// ...
}
gate := apiConfig.Gate() // carries Verify and Redact into mcp.Config.Gate
```

Set `api.Config.Verify` to `door.Or(...)` and `api.Config.Redact` to its `Redact` method. If you build the MCP gate separately from `Config.Gate`, set them there too. The verifier that `door.Or(...)` returns accepts grant tokens and the app's own tokens at one address. A bearer token that starts with `rastrillo_grant_` goes to the door. Any other token goes to the one you passed to `Or`, which is usually the `*apitoken.Store`.

## Wire form and claims

```go
const (
	Prefix = "rastrillo_grant_"
	Read   = "read"
	Write  = "write"
)

type Claims struct {
	Version   int64  `json:"v"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	KeyID     string `json:"kid"`
	Subject   string `json:"sub"`
	Grant     string `json:"gid"`
	Agent     string `json:"agent"`
	Access    string `json:"access"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func Sign(key *crypto.Keypair, claims Claims) (string, error)
func Redact(text string) string
```

A token is `rastrillo_grant_`, then the signed payload and its signature, at most 4096 bytes after the prefix. The payload is signed under the context `rastrillo/grant/v1`. A payload signed for a roster or an assertion fails as `ErrSignature`.

`Issuer` is the root's origin. `Audience` is one origin: the site's platform host. `Subject` is the person's root subject, `Grant` is the root's ID for the grant and `Agent` is the agent's name. `Access` is `read` or `write`. All ten members are required, and a token with a member it does not know, a repeated member, or a `null` is refused. `Version` is `1`.

A token lives at most 366 days, and its `ExpiresAt` is its grant's expiry. `Sign` is for the root, and for tests. It refuses claims that would not verify, but it uses no clock and does not compare the issuer or audience with anyone's: those are the verifier's to judge.

`Redact` hides every grant token in a string while keeping its prefix. `Either` also provides `Redact` as a method. `Door` does not.

## The checks, in order

```go
func NewVerifier(issuer, audience string, keys map[string][]byte) (*Verifier, error)
func (verifier *Verifier) Verify(token string, now time.Time) (Claims, error)

type Grants interface {
	GrantActive(ctx context.Context, subject, grant string) (bool, error)
}

type DoorConfig struct {
	Verifier *Verifier
	Grants   Grants
	Now      func() time.Time // nil means time.Now
}

func NewDoor(config DoorConfig) (*Door, error)
func (door *Door) Verify(ctx context.Context, request *http.Request) (rastrillo.Actor, error)
func (door *Door) Recheck(ctx context.Context, request *http.Request, was rastrillo.Actor) (rastrillo.Actor, error)
func (door *Door) Or(others Fallback) *Either
```

`Door.Verify` checks these in order and stops at the first that fails:

1. The request has one `Authorization: Bearer` header.
2. The signature matches a trusted key.
3. The token parses, and its claims are well formed with the lifetime in range.
4. `iss` is the verifier's issuer.
5. `aud` is the verifier's audience.
6. The token has not expired. A token issued more than 30 seconds in the future is refused too.
7. `Grants.GrantActive(sub, gid)` is true.

On a hosted site, set `Grants` to the `*roster.Receiver`. See [roster](/docs/reference/roster#grants). `GrantActive` is false before the site has applied any snapshot, when the latest snapshot suspends the instance, and when it lists no such grant under that person.

`Recheck` makes the same checks again. `Door` is an `api.Rechecker`, so a grant that ends while an answer is being sent cuts the answer off.

`Either` is the verifier that `Or` returns; pass it to `api.Config`.

```go
func (either *Either) Verify(ctx context.Context, request *http.Request) (rastrillo.Actor, error)
func (either *Either) Recheck(ctx context.Context, request *http.Request, was rastrillo.Actor) (rastrillo.Actor, error)
func (either *Either) Redact(text string) string
```

`Verify` and `Recheck` send requests with grant tokens to the door and all other requests to the fallback. If the door refuses a token, the app's own verifier does not retry the request. `Redact` hides both kinds of token.

```go
type Fallback interface {
	api.Verifier
	api.Rechecker
	Redact(text string) string
}
```

`*apitoken.Store` already satisfies `Fallback`.

## The actor

A good token gives the request this actor:

- `Subject` is `sub`, `Agent` is `agent`, and `Credential` is `gid`.
- `Access` is `rastrillo.Read` or `rastrillo.Write`.
- `Grant` is `&rastrillo.Grant{ID: gid, Issuer: iss, IdentityIssuer: iss, Audience: aud}`.
- `Session` is empty. A write still needs an `Agent-Session` header when `RequireAgentSession` is set.

`Actor.String` shows it as `agent:<agent> for user:<sub> grant:<gid>`.

## What a grant refuses

A read grant refuses every write, as any read-only actor does. A human-only operation refuses every token, so a grant token cannot call one, whatever its access. See [op](/docs/reference/op).

## Errors

Errors carry no submitted values, so they can be logged as they are.

- `ErrMalformed`: the token is not shaped like a grant token, is too large, or repeats a member.
- `ErrVersion`: the version is not `1`.
- `ErrUnknownKey`: the key id is not one the verifier trusts.
- `ErrSignature`: the signature does not match.
- `ErrIssuer`, `ErrAudience`: the token is from or for someone else.
- `ErrLifetime`, `ErrExpired`: the lifetime is out of range, or the token is from the future or has expired.
- `ErrClaims`: a member is missing, `null`, unknown or out of range.
- `ErrRevoked`: the grant is not active: revoked, ended, or not yet in the roster the site holds.
- `ErrConfig`: `NewVerifier` or `NewDoor` was given a configuration it cannot use.
- `ErrSigning`: `Sign` was given a key it cannot sign with.

`Door` answers a refused token, a refused claim or an inactive grant with a wrapped `api.ErrUnauthorized`, which is a 401. When `GrantActive` fails, `Door` returns a wrapped `api.ErrUnavailable` (503), so the client can retry.

## The root's own door

On the root, `iss` equals `aud`. `NewVerifier` allows that, and the root passes its own grant records as `Grants`. The dashboard uses this door to accept the same tokens it hands out.

For testing, see [`handofftest`](/docs/reference/handofftest).
