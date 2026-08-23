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
```

`NewSeed` mints the one per-person root everything else derives from.
