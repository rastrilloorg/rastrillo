# 🤖 roster

Use `roster` to keep an instance's member list in step with the root app. The root sends a signed snapshot of the whole roster, and the instance applies it to its own tables. A snapshot says who belongs and whether the instance is active. It carries no credentials, so a member still signs in through [`handoff`](/docs/reference/handoff).

`amadan.net/rastrillo/rastrillo/roster`

## Mount it

```go
// The platform host, even when the instance serves on a custom domain.
platformHost := "https://" + carlos.Host()
verifier, err := roster.NewVerifier(setup.Root, platformHost, setup.Keys)
// ...
receiver, err := roster.New(roster.Config{
	Verifier: verifier,
	Store:    store,
	DB:       db,
	Roles:    []string{"owner", "member"},
	// stop cancels the context whose Done() is rastrillo.Options.Stop.
	OnOrigin: func(previous, next string) { stop() },
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
func (r *Receiver) Origin(ctx context.Context) (string, error)
func StoredOrigin(ctx context.Context, db *sql.DB) (string, error)
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

## Keep emails out of the receiver's row

The receiver keeps a copy of the last applied snapshot in its own row, `roster_state`, so it can repair the roster after a lost lease (see [Store](#store)). By default the copy includes each member's email and name.

To keep emails and names out of the receiver's row, set `Config.RecordRolesOnly`:

```go
receiver, err := roster.New(roster.Config{
	// ...
	RecordRolesOnly: true,
})
```

The row then holds only each member's subject and role. Your `Store` still gets every field. To avoid storing the email, drop it in your `Store` too. Set the option in every process that shares the database: a receiver without it writes emails into the row again.

A repair from such a row restores who belongs and each member's role. It keeps the email and name your `Store` already has. A member added back by the repair has no email or name until the next snapshot. Any email or name changed by the stale apply also remains until that snapshot. The repaired row contains no emails, even if this receiver does not set `RecordRolesOnly`.

If the row already holds emails, it is rewritten without them the next time a snapshot is applied. To rewrite the row immediately, have the root send the current snapshot again. This removes emails from the row. The old bytes can remain in the database file, its write-ahead log and any existing backups.

## Custom origin

A snapshot can carry the account's custom origin, such as `https://blog.example.org`. The receiver stores it with the sequence. `Origin` returns the stored origin, or `""` when the instance serves on its own host. `StoredOrigin` reads the same value from the database before any receiver exists, which is what boot needs: see [`instance.ServingOrigin`](/docs/reference/instance).

The app fixes its origin at boot, so a change needs a restart. Set `Config.OnOrigin`, which receives the previous and next origin. The receiver calls it after it has applied a snapshot that changes the origin and sent the 204. A snapshot that repeats the origin does not call it, and neither does the root's retry. `OnOrigin` runs inside the roster request. From it, cancel the context whose `Done()` you passed as `rastrillo.Options.Stop`, and `Serve` shuts down gracefully. Do not wait there for the server to stop: the shutdown waits for this request. [`instance`](/docs/reference/instance) shows the wiring.

Make the verifier's audience the instance's platform host, `https://` and `carlos.Host()`, not the custom origin. The root always delivers to the platform host, so a custom domain that stops working cannot cut the root off.

When several processes share the database, only the one that applied the snapshot calls `OnOrigin`.

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
	Origin                                 string
}

func Sign(key *crypto.Keypair, snapshot Snapshot) (string, error)
func NewVerifier(issuer, audience string, keys map[string][]byte) (*Verifier, error)
func (v *Verifier) Verify(token string, now time.Time) (Snapshot, error)
```

`Sign` is for the root, and for tests. The snapshot is signed under the context `rastrillo/roster/v1`, so an assertion signed by the same key cannot be used as a roster, nor the reverse. `Version` is `1`. A snapshot lives at most 300 seconds, and a token is at most 1 MiB. `Sequence` starts at 1 and rises with every snapshot for one instance. An empty `Members` list is valid.

`Origin` is empty, or `https://` and a lower-case host with no port, path, query or user info. It cannot be the issuer's origin. `Sign` and `Verify` refuse anything else with `ErrClaims`. An empty `Origin` is left out of the token, so a snapshot without one has the same bytes as before. `Verify` refuses fields it does not know, so a receiver older than this field refuses a snapshot that sets it. Upgrade every instance before the root sends an origin.

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
