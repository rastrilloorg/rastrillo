# 🤖 webauthn

`amadan.net/rastrillo/rastrillo/webauthn`

The passkey identity half: the WebAuthn ceremonies, a CBOR subset
reader, and the browser module. Most apps mount
[`passkey`](/docs/reference/passkey) instead. This is what that is built
on, and what you reach for to build something else.

## What it does and does not check

ES256 only. No attestation checking.

Skipping attestation is a decision. Attestation tells you which
manufacturer made the authenticator, which matters for enterprise device
policy and not for "is this the same key as last time". Verifying it
means shipping and maintaining a root certificate store, and getting
that wrong locks people out of their own accounts.

The CBOR reader implements the subset WebAuthn actually uses. It rejects
anything outside it rather than attempting a general decode.

## Config

```go
type Config struct{ /* RPID, Origin, LegacyRPID, ... */ }
```

`LegacyRPID` accepts credentials minted under a previous hostname, so
moving domains does not invalidate everybody's passkeys. A credential
cannot be minted under the old name, only used, which keeps the escape
hatch from becoming a second permanent identity.

## Register

```go
func (c Config) Register(challenge, clientDataJSON, attestationObject []byte) (Credential, error)
```

Verifies a registration ceremony and returns the `Credential` to store —
its id, public key and initial signature counter. A credential's public
key is public material: it verifies signatures and nothing else.

## Verify

```go
func (c Config) Verify(cred Credential, challenge, clientDataJSON, authData, signature []byte) (uint32, error)
```

Verifies an assertion and returns the new signature counter.

A counter going backwards is refused: that is the cloned-authenticator
signal the spec provides. An authenticator that never counts, reporting
zero every time, is allowed, because plenty do.

## The errors

Each names one refusal, so you can tell them apart without matching
message text:

| Error | Meaning |
|---|---|
| `ErrChallenge` | the challenge did not match |
| `ErrOrigin` | the client data's origin was wrong |
| `ErrRPID` | the relying-party id did not match |
| `ErrSignature` | the signature did not verify |
| `ErrCounter` | the signature counter went backwards |
| `ErrNotVerified` | the authenticator did not report user verification |

## NewChallenge

```go
func NewChallenge() ([]byte, error)
```

Fresh random challenge bytes. Store it server-side and use it once. The
challenge is what makes an assertion un-replayable, so one that outlives
a single ceremony undoes the whole protocol.

## JS

```go
func JS() []byte
```

The browser half as an embedded ES module. Serve it as a static asset.
It exposes `register()` and `authenticate()`, which produce exactly the
payloads `Register` and `Verify` expect.

## authtest

`webauthn/authtest` is a fake authenticator, public precisely so your
own tests can drive a full ceremony without hardware.
