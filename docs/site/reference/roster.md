# 🤖 roster

Use `roster` to keep an instance's member list in step with the root app. The root sends a signed snapshot of the whole roster, and the instance applies it to its own tables. A snapshot says who belongs and whether the instance is active. It carries no credentials, so a member still signs in through [`handoff`](/docs/reference/handoff).

`amadan.net/rastrillo/rastrillo/roster`

## Mount it

```go
verifier, err := roster.NewVerifier(setup.Root, origin, setup.Keys)
// ...
receiver, err := roster.New(roster.Config{
	Verifier: verifier,
	Store:    store,
	DB:       db,
	Roles:    []string{"owner", "member"},
})
// ...
mux.Handle(roster.Path, receiver)
```

Exempt the path from CSRF with `csrf.Exempt(handoff.EnterPath, roster.Path)`. The root's POST is cross-site, and its signature protects it. Merge `roster.Schema` and `handoff.Schema` into `BootSchema`.

## Receiver

```go
const Path = "/_carlos/roster"

func New(config Config) (*Receiver, error)
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request)
func (r *Receiver) Suspended(ctx context.Context) (bool, error)
```

The receiver takes POST only, with the token as the body. It answers 204 when the snapshot is applied. It answers only on the verifier's audience origin, which is the instance's own host even for a hub tenant. On any other host it answers 404 without reading the body.

| Status | Meaning |
| --- | --- |
| 204 | Applied. |
| 403 | The token did not verify. |
| 404 | The request came in on a host other than the audience origin's. |
| 409 | An older snapshot than one already applied. |
| 413 | The token is over 1 MiB. |
| 422 | A member has a role not in `Config.Roles`. |
| 503 | Another apply is running, or kept taking the lease from this one. `Retry-After` says when to try again. |
| 500 | The store or the database failed. |

Each snapshot contains the whole roster, so the next snapshot recovers any one the instance missed. The receiver uses `Put` to add or change members. It uses `Remove` for members missing from the snapshot. It records the snapshot's sequence last, so a failed apply is applied again on the root's retry.

A time-limited database lock allows only one apply at a time, even when several processes share the database. A busy lease answers 503 with `Retry-After`. `Config.Roles` lists the app's roles, highest first.

`Suspended` reports whether the last applied snapshot suspended the instance. Before any snapshot it is false. Pass it as `handoff.Config.Suspended`.

## Store

```go
type Store interface {
	Members(ctx context.Context) ([]Member, error)
	Put(ctx context.Context, member Member) error
	Remove(ctx context.Context, subject string) error
}
```

The app's `Store` keeps the members in its own tables. Every method should honour `ctx`. The receiver gives an apply 25 seconds through `ctx`. A method that ignores `ctx` can keep writing after the lease expires.

So writes from a stale apply can land on top of a newer roster. The receiver repairs them before it answers. After its Store writes, it checks that it still holds the lease. If another process took the lease meanwhile, the receiver takes a new lease and applies the newest recorded roster again, diffing it against `Members`. Then it answers 409 for its own, older snapshot. If the lease stays busy, or is lost three times in a row, it answers 503, and the root's retry finishes the repair.

When a `Store` method fails, the receiver answers 500 and logs only which method failed: `store put failed`, `store remove failed` or `store members failed`. It leaves out the error's text, which could hold a member's email or name.

## Snapshots

```go
type State string

const (
	Active    State = "active"
	Suspended State = "suspended"
)

type Member struct {
	Subject, Email, Name, Role string
}

type Snapshot struct {
	Version, Sequence, IssuedAt, ExpiresAt int64
	Issuer, Audience, KeyID                string
	State                                  State
	Members                                []Member
}

func Sign(key *crypto.Keypair, snapshot Snapshot) (string, error)
func NewVerifier(issuer, audience string, keys map[string][]byte) (*Verifier, error)
func (v *Verifier) Verify(token string, now time.Time) (Snapshot, error)
```

`Sign` is for the root, and for tests. The snapshot is signed under the context `rastrillo/roster/v1`, so an assertion signed by the same key cannot be used as a roster, nor the reverse. `Version` is `1`. A snapshot lives at most 300 seconds, and a token is at most 1 MiB. `Sequence` starts at 1 and rises with every snapshot for one instance. An empty `Members` list is valid.

`Verify` checks the signature, issuer, audience and lifetime. It does not compare sequences; the receiver does.

## Errors

Errors carry no submitted values, so they can be logged as they are.

- `ErrMalformed`: the token is not shaped like a snapshot, or is too large.
- `ErrVersion`: the version is not `1`.
- `ErrUnknownKey`: the key id is not one the verifier trusts.
- `ErrSignature`: the signature does not match.
- `ErrIssuer`, `ErrAudience`: the snapshot is from or for someone else.
- `ErrLifetime`, `ErrExpired`: the lifetime is over 300 seconds, or the snapshot is from the future or has expired.
- `ErrClaims`: a field is missing, duplicated or out of range.
- `ErrConfig`: `NewVerifier` or `New` was given a configuration it cannot use.
- `ErrSigning`: `Sign` was given a key it cannot sign with.
- `ErrUnknownRole`: a member has a role the app does not define. The receiver answers 422.

For testing, see [`handofftest`](/docs/reference/handofftest).
