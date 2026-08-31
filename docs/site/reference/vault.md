# 🤖 vault

`amadan.net/rastrillo/rastrillo/vault`

The client half of the Pegamento vault: one person's named sealed
blobs and per-method wrapped seed on a home service your operator may
not run. The server lives at `amadan.net/carlos/pegamento`; this
package speaks its v1 wire, seals and opens with
[keyring](/docs/reference/keyring) keys so plaintext never crosses the
package boundary, and refuses undeclared blob names before any request
leaves the process.

Two rulings bind everything here, inherited from the doctrine.
Strictly additive: an app that configures no home constructs no
`Client` and makes no request, ever. Closed namespace: every blob name
the app may touch is declared at construction.

## New and Config

```go
type Config struct {
	Home   string
	Ring   keyring.Ring
	Blobs  []string
	Token  string
	Seed   []byte
	Client *http.Client
}

func New(cfg Config) (*Client, error)
```

A `Client` is one person's bound home session, not a service handle —
construct it when you hold a link token and an unwrapped seed, never
at boot. `New` validates everything and makes no request: `Home` needs
a scheme, `Blobs` is the closed namespace with every name matching
`[a-z0-9-]{1,64}`, `Seed` is the person's 32 bytes. `Client` defaults
to a 10-second timeout.

## Blobs

```go
const Create = "0"

func (c *Client) Get(ctx context.Context, name string) (plaintext []byte, version string, err error)
func (c *Client) Put(ctx context.Context, name string, plaintext []byte, version string) (newVersion string, err error)
func (c *Client) PutPadded(ctx context.Context, name string, plaintext []byte, target int, version string) (newVersion string, err error)
func (c *Client) Blobs(ctx context.Context) ([]BlobInfo, error)

type BlobInfo struct {
	Name    string
	Version string
}
```

Plaintext in, plaintext out: `Put` seals under
`Ring.BlobKey(seed, name)` and `Get` opens, so a caller cannot
accidentally store plaintext. Versions are opaque strings the client
compares only for equality; `Create` is the create-only sentinel.
`PutPadded` pads to a fixed target first, for a blob whose length
leaks (a server list); the target is a floor, not a cap. `Blobs`
lists names and versions, never content.

```go
type ErrStale struct{ Current string }

var ErrNotFound = errors.New("rastrillo/vault: blob not found")
var ErrUndeclared = errors.New("rastrillo/vault: undeclared blob name")
```

`Put` refuses a stale version with `ErrStale` carrying the current one
— re-read, merge, retry. `Get` answers `ErrNotFound` for a blob never
written (first run, not an outage). An undeclared name answers
`ErrUndeclared` locally, before any dial. `ErrStale.Error` prints the
current version so an unhandled one still reads well in a log.

## Methods

```go
type Method struct {
	ID        string
	Kind      string
	CreatedAt string
}

func (c *Client) Methods(ctx context.Context) ([]Method, error)
func (c *Client) Wrapped(ctx context.Context, methodID string) ([]byte, error)
func (c *Client) Enrol(ctx context.Context, methodID string, wrapped []byte, ceremonyProof string) error
func (c *Client) RemoveMethod(ctx context.Context, methodID, ceremonyProof string) error
```

A method is one way into the vault — a passkey, an identity anchor, a
synced item — and the unit the wrapped seed is keyed by. `Wrapped`
fetches the seed wrapped for one method (unwrap with
`keyring.Ring.UnwrapSeed`); `Enrol` stores a new wrap and
`RemoveMethod` removes one, both demanding a fresh ceremony proof
because they change who can open everything. The home's last-method
guard refuses to strand the vault; it surfaces here as an ordinary
error.

## The handoff

```go
type Handoff struct {
	Sessions   *sessions.Sessions
	Home       string
	Origin     string
	SigninPath string
}

func (h Handoff) Enrol(w http.ResponseWriter, r *http.Request)
func (h Handoff) Restore(w http.ResponseWriter, r *http.Request)
```

The instance's two POST handlers. Mount `Enrol` behind
`sessions.Require` and `Restore` on the signed-out router, both under
the app-wide [csrf](/docs/reference/csrf) middleware. `Enrol` mints a
fresh `Method: "vault"` session row via `sessions.Mint` — never the
browser's own cookie — and answers the home URL whose fragment carries
it. `Restore` adopts the token the browser unwrapped via
`sessions.Adopt` and redirects to its same-site `return_to`; a dead
token lands on `SigninPath` (default `/signin`) with no cookie, so a
failed restore is a sign-in prompt, never an error page. The instance
and the home never speak: everything secret rides URL fragments
through the person's browser.

```go
type EnrolAnswer struct {
	EnrolURL string
	Nonce    string
	Entry    EnrolEntry
}

type EnrolEntry struct {
	URL   string
	Token string
}
```

`EnrolAnswer` is `Enrol`'s JSON response; `EnrolEntry` is what the
home stores in the person's servers blob — this instance's origin and
the token that re-admits them here.

```go
type RestoreRequest struct {
	V      int
	Ret    string
	Nonce  string
	EphPub []byte
}

func NewRestoreRequest(ret string) (RestoreRequest, *crypto.Keypair, error)
func OpenRestoreReturn(kp *crypto.Keypair, nonce string, sealed []byte) (token string, err error)
```

The restore round trip's pure half, for CLI-shaped consumers and
tests. `NewRestoreRequest` mints the ephemeral keypair and one-time
nonce; the home seals its answer to `EphPub` under
`rastrillo/vault/restore/v1`; `OpenRestoreReturn` opens it and
verifies the nonce, failing closed on a stale, replayed, or tampered
return.

## JS

```go
func JS() []byte
```

The browser twin (`js/vault.mjs`): `restoreRequest` and
`openRestoreReturn` over WebCrypto, plus fragment encode/decode.
Serve it beside `crypto.JS()` — the sibling import is the deployment
contract, [keyring](/docs/reference/keyring)'s pattern exactly, and
the package's tests prove a Go-sealed return opens in JS.
