# 🤖 keyring

`github.com/carlosframework/rastrillo/keyring`

The E2EE seed lifecycle over [crypto](/docs/reference/crypto)'s
primitives: one 32-byte seed per person, namespaced purpose derivation,
the seed wrapped under a passkey, content keys granted to members, and
the guard that keeps the last wrap unrevokable.

Two rulings bind everything here. **No new cryptography** — every
operation composes crypto's golden-vectored primitives (`Derive`,
`SealSym`/`OpenSym`, `Seal`/`Open`); the keyring adds names, formats
and ceremonies, not ciphers. **No storage** — pure functions plus wire
formats, in both languages; tables for wrapped seeds, grant rows and
member public keys belong to the app.

## The ring

```go
type Ring struct{ Namespace string }

func (r Ring) PRFSalt() string
func (r Ring) ContentKey(seed []byte) []byte
func (r Ring) WrapKey(prf []byte) []byte
```

A `Ring` carries the app's namespace and derives every context string
from it — `PRFSalt` is `ns/prf/v1`, `ContentKey` derives with
`ns/content/v1`, `WrapKey` with `ns/wrap/v1` — so two apps on one
keyring can never collide. Kass's existing strings fall out as the
`Ring{"kass"}` case, byte-identically: its `deriveBytes` is HKDF-SHA256
with a zero-length salt, `crypto.Derive` passes a nil salt, and RFC
5869 treats the two the same.

## Seeds

```go
func NewSeed() ([]byte, error)

func (r Ring) WrapSeed(prf, seed []byte) ([]byte, error)
func (r Ring) UnwrapSeed(prf, wrapped []byte) ([]byte, error)
```

`NewSeed` mints the one per-person root everything else derives from.
`WrapSeed` seals it under the key derived from a passkey's PRF output —
`iv(12) ‖ AES-256-GCM ciphertext` — and wrapping the same seed under a
different credential's PRF output is the whole of device add and RPID
move. `UnwrapSeed` reverses it; a wrong credential, wrong namespace and
tampered blob fail indistinguishably.

The transport contract the package names but does not build: store a
wrapped seed keyed by credential ID, return it at sign-in, accept a new
one at enrol.

An RPID move is a three-phase drill. Old name: normal operation, the
seed wrapped under old-RPID passkeys. Crossover: serve under the new
RPID with [webauthn](/docs/reference/webauthn)'s `Config.LegacyRPID`
set to the old one (assertions only); a fresh device signs in via the
legacy fallback, unwraps the seed, and enrols a new-RPID credential —
`WrapSeed`, same seed. Settled: `LegacyRPID` removed; only new-name
credentials remain.

## Grants

```go
func (r Ring) Grant(memberBoxPub, contentKey []byte) ([]byte, error)
func (r Ring) OpenGrant(memberBoxPriv, sealed []byte) ([]byte, error)
```

A member is the box half of a keypair. `Grant` wraps a content key —
one key, one instance, never the seed — with `crypto.Seal` under
`ns/grant/v1`: `ephPub(65) ‖ iv(12) ‖ ciphertext`. `OpenGrant` takes
the raw 32-byte box private scalar, not a full `crypto.Keypair`,
because member identities are ECDH-only pairs; the JS twin accepts the
pkcs8/JWK import the app already holds.

Revocation is the server deleting the grant row — nothing
cryptographic. Re-keying after revocation, where an app wants it, is
mint a new content key and re-grant to the remaining members; the
package adds no machinery for that ceremony.

## Wraps

```go
type Wrap struct {
	ID, Kind, Label, UID string
	CredentialID         []byte
	Wrapped              []byte
}

func AddWrap(wraps []Wrap, w Wrap) []Wrap
func RemoveWrap(wraps []Wrap, id string) ([]Wrap, error)
```

The one lifecycle rule enforced in code, because getting it wrong loses
data forever: **the last wrap is unrevokable**. `RemoveWrap` returns
`ErrLastWrap` rather than leave a seed with zero wraps — a seed no one
can ever open again — and `ErrUnknownWrap` for an ID it cannot find.
`AddWrap` dedupes by ID, replacing an existing wrap in place. Both are
pure: the input slice is never mutated, and the app persists wraps
wherever it likes.

An event-sourced consumer folding untrusted input must treat
`ErrLastWrap` as a no-op, not a crash — errors are for interactive
callers.

## JS

```go
func JS() []byte
```

The WebCrypto twin as an embedded ES module (`js/keyring.mjs`). It
imports `./crypto.mjs`, so serve `crypto.JS()` beside it under the same
mount — the sibling layout is the deployment contract. It passes the
same golden vectors as the Go package, and one trade is made out loud:
`contentKey` and `wrapKey` return raw bytes, trading
non-extractable-CryptoKey hygiene for wrappability, because a grant
cannot wrap a key it cannot read.
