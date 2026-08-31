# 🤖 keyring

`amadan.net/rastrillo/rastrillo/keyring`

The E2EE seed lifecycle over [crypto](/docs/reference/crypto)'s
primitives: one 32-byte seed per person, namespaced purpose derivation,
the seed wrapped under a passkey, content keys granted to members, and
the guard that keeps the last wrap unrevokable.

Two rulings bind everything here.

No new cryptography. Every operation composes crypto's golden-vectored
primitives — `Derive`, `SealSym`/`OpenSym`, `Seal`/`Open`. The keyring
adds names, formats and ceremonies, never ciphers.

No storage. Pure functions plus wire formats, in both languages. The
tables for wrapped seeds, grant rows and member public keys are
yours.

## The ring

```go
type Ring struct{ Namespace string }

func (r Ring) PRFSalt() string
func (r Ring) ContentKey(seed []byte) []byte
func (r Ring) WrapKey(prf []byte) []byte
func (r Ring) BlobKey(seed []byte, name string) []byte
```

A `Ring` carries your namespace and derives every context string from
it: `PRFSalt` is `ns/prf/v1`, `ContentKey` derives with `ns/content/v1`,
`WrapKey` with `ns/wrap/v1`, and `BlobKey` derives one sealing key per
named [vault](/docs/reference/vault) blob with `ns/blob/<name>/v1` —
the name lands inside the context string, so the vault's closed
namespace validates it before the ring ever sees it. Two apps on one keyring can never
collide. Kass's existing strings fall out as the
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
`WrapSeed` seals it under the key derived from a passkey's PRF output,
as `iv(12) ‖ AES-256-GCM ciphertext`. Wrapping the same seed under a
different credential's PRF output is the whole of device add and RPID
move. `UnwrapSeed` reverses it, and a wrong credential, a wrong
namespace and a tampered blob all fail the same way.

The transport is yours to build, and the contract is short: store a
wrapped seed keyed by credential ID, return it at sign-in, accept a new
one at enrol.

An RPID move runs in three phases.

Under the old name, nothing special happens: the seed is wrapped under
old-RPID passkeys.

At crossover you serve under the new RPID with
[webauthn](/docs/reference/webauthn)'s `Config.LegacyRPID` set to the
old one, for assertions only. A fresh device signs in through the legacy
fallback, unwraps the seed, and enrols a new-RPID credential — same
seed, another `WrapSeed`.

Once settled, drop `LegacyRPID`. Only new-name credentials remain.

## Grants

```go
func (r Ring) Grant(memberBoxPub, contentKey []byte) ([]byte, error)
func (r Ring) OpenGrant(memberBoxPriv, sealed []byte) ([]byte, error)
```

A member is the box half of a keypair. `Grant` wraps a content key —
one key, one instance, never the seed — with `crypto.Seal` under
`ns/grant/v1`, as `ephPub(65) ‖ iv(12) ‖ ciphertext`.

`OpenGrant` takes the raw 32-byte box private scalar instead of a full
`crypto.Keypair`, because member identities are ECDH-only pairs. The JS
twin accepts the pkcs8/JWK import you already hold.

Revocation is your server deleting the grant row. Nothing cryptographic
happens. If you want to re-key afterwards, mint a new content key and
re-grant to the remaining members; the package adds no machinery for
that ceremony.

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

One lifecycle rule is enforced in code, because getting it wrong loses
data forever: the last wrap cannot be revoked. `RemoveWrap` returns
`ErrLastWrap` instead of leaving a seed with zero wraps, which would be
a seed nobody can ever open again. An ID it cannot find gets
`ErrUnknownWrap`.

`AddWrap` dedupes by ID, replacing an existing wrap in place. Both
functions are pure: your input slice is never mutated, and you persist
wraps wherever you like.

If you fold untrusted input in an event-sourced consumer, treat
`ErrLastWrap` as a no-op rather than a crash. These errors are for
interactive callers.

## JS

```go
func JS() []byte
```

The WebCrypto twin as an embedded ES module, `js/keyring.mjs`. It
imports `./crypto.mjs`, so serve `crypto.JS()` beside it under the same
mount — that sibling layout is the deployment contract.

It passes the same golden vectors as the Go package. One trade is made
out loud: `contentKey` and `wrapKey` return raw bytes, giving up
non-extractable-CryptoKey hygiene for wrappability, because a grant
cannot wrap a key it cannot read.
