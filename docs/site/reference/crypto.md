# 🤖 crypto

`amadan.net/rastrillo/rastrillo/crypto`

The family envelope: asymmetric sealing, signing, and the symmetric
half — every operation domain-separated by a caller-supplied context
string.

This is a compatibility contract. It is byte-compatible with keymail's
`crypto.go`, amadan's `internal/envelope` and `internal/repokey`, and
seapointish's `internal/seal` — the hand-rolled copies it retires.
`testdata/golden.json` is amadan's pinned cross-implementation fixture,
and this package and its JavaScript twin both have to pass it.

If you change anything here, the vectors are the specification.

## The wire formats

```text
Seal/Open        ephPub(65, uncompressed point) ‖ iv(12) ‖ AES-256-GCM ciphertext
Sign/Verify      ECDSA P-256 over SHA-256(context ‖ 0x00 ‖ msg), raw r‖s (32+32, zero-padded)
SealSym/OpenSym  iv(12) ‖ AES-256-GCM ciphertext
Derive           HKDF-SHA256, salt=nil, info=context, 32 bytes
```

Signatures are raw r‖s, not ASN.1 DER. A verifier expecting DER will
reject every signature this package produces.

## Keys

```go
func Generate() (Keypair, error)
func NewKey() ([]byte, error)
func MarshalKeypair(k Keypair) ([]byte, error)
func UnmarshalKeypair(b []byte) (Keypair, error)
```

A `Keypair` carries both halves. `Keypair.BoxPub` is the public key for
`Seal`, `Keypair.SignPub` the one for `Verify`. They are separate keys
with separate jobs, and using one where the other belongs fails loudly
instead of quietly weakening something.

`NewKey` mints raw symmetric key bytes for `SealSym`.

## Asymmetric

```go
func Seal(recipientBoxPub []byte, context string, msg []byte) ([]byte, error)
func Open(k Keypair, context string, sealed []byte) ([]byte, error)
```

ECDH P-256 with an ephemeral key, HKDF-SHA256 to a content key, then
AES-256-GCM.

The `context` string is domain separation. The same bytes sealed under
two contexts produce independent ciphertexts, and opening with the wrong
context fails. Pick one per purpose and keep it stable: changing it is a
format break for everything already sealed.

## Signing

```go
func Sign(k Keypair, context string, msg []byte) ([]byte, error)
func Verify(signPub []byte, context string, msg, sig []byte) bool
```

The signed digest is `SHA-256(context ‖ 0x00 ‖ msg)`. That `0x00` stops
the context/message boundary being ambiguous: without it, signing
`("ab", "c")` and `("a", "bc")` would produce the same digest.

## Symmetric

```go
func Derive(secret []byte, context string) ([]byte, error)
func SealSym(key []byte, msg []byte) ([]byte, error)
func OpenSym(key []byte, sealed []byte) ([]byte, error)
```

`Derive` is HKDF-SHA256 with the context as `info`, producing 32 bytes.
It is how one root secret becomes many purpose-specific keys that cannot
be used interchangeably.

## Invites

```go
func NewInviteSecret() ([]byte, error)
func DeriveInvite(secret []byte, context string) (Invite, error)
func WrapKey(key, secret []byte, context string) (string, error)
func UnwrapKey(wrapped string, secret []byte, context string) ([]byte, error)
```

`DeriveInvite` derives an invite's `id`, `wrapKey` and `claimSecret`
from one root secret by context suffix — `-id`, `-wrap`, `-claim` — with
`claimHash` the hex SHA-256 of the claim secret. `WrapKey` and
`UnwrapKey` are `SealSym` in base64url.

These waited until a consumer pinned their contract. Eleven's messenger
did, and `testdata/invites.json` carries its vectors verbatim — context
`"lchat-invite"` reproduces its wire format byte for byte.

## JS

```go
func JS() []byte
```

The WebCrypto twin as an embedded ES module, so a browser can open what
a server sealed. It passes the same golden vectors, which is what makes
it a twin instead of a second implementation.
