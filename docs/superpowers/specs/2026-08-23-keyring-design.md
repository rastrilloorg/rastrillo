# rastrillo/keyring — seed lifecycle, member grants, re-wrap ceremonies

**Issue:** rastrilloorg/rastrillo#79 · **Date:** 2026-08-23 · **Status:** draft for review

## 0. The gap

`crypto` ships the primitives (envelope, symmetric half, HKDF derive, JS twin)
but not the **lifecycle**. Every E2EE app in the family re-solves, by hand: one
seed per person, HKDF purpose derivation, the seed PRF-wrapped under a passkey,
content keys wrapped to members' public keys, revocation, and re-wrapping the
same seed under a new credential. This package owns that lifecycle before a
third app writes a third variant.

Honesty about reach: **v1 targets kass's model, byte-compatibly.** Messenger's
vault is structurally different — it wraps a pkcs8 ECDH *private key* rather
than a seed, its PRF eval input (`eleven-vault-kek-v1`) and HKDF info
(`eleven-vault-kek`) don't share a suffix scheme any `Ring` can express, and
its "member" wraps are vault-key handoffs, not content-key grants. Messenger
adopting the keyring would be a re-key ceremony, not a rename; it is out of
scope here and unforced. What the keyring takes from messenger is its
hard-won *rules* (§2.3), not its bytes.

## 1. Ruled up front

- **`crypto.Seal` wins the grant envelope.** Kass's coach grants feed raw ECDH
  bits straight into AES-GCM (`sharing.js:40-43`); `crypto.Seal` inserts HKDF
  with a context string. Same outer layout (`ephPub(65) ‖ iv(12) ‖ ct`),
  byte-incompatible keys. The keyring standardises on the HKDF envelope; kass
  re-wraps its grants (a ceremony it needs anyway) and the framework carries
  **no legacy decode path**. (Ruled by Paul, 2026-08-23.)
- **No new cryptography.** Every operation composes `crypto`'s existing,
  golden-vectored primitives: `Derive`, `SealSym`/`OpenSym`, `Seal`/`Open`,
  `Generate`. The keyring adds names, formats, and ceremonies — not ciphers.
- **No storage.** The keyring is pure functions plus wire formats, in both
  languages. Tables (wrapped seeds, grant rows, member public keys) belong to
  the app or to the sealed store (#77).

## 2. Shape

New package `keyring/` (Go) and a vendored ES module `keyring/js/keyring.mjs`
embedded via `keyring/js.go` (`func JS() []byte`) — the `crypto` pattern
exactly: apps serve it as a static asset, app-owned after delivery.

Everything is namespaced. A `Ring` carries the app's namespace and derives
every context string from it, so two apps on one keyring package can never
collide, and kass's existing strings fall out as the `ns = "kass"` case:

```go
type Ring struct{ Namespace string } // e.g. "kass"

func (r Ring) PRFSalt() string          // ns + "/prf/v1"
func (r Ring) ContentKey(seed []byte) []byte  // crypto.Derive(seed, ns+"/content/v1")
func (r Ring) WrapKey(prf []byte) []byte      // crypto.Derive(prf, ns+"/wrap/v1")
```

Byte-compatibility note, verified against kass: kass's `deriveBytes` is
HKDF-SHA256 with empty salt and a UTF-8 info string; `crypto.Derive` is
`hkdf.Key(sha256, key, nil, context, 32)` — identical output (RFC 5869 treats
absent and zero-length salt the same), and the Go↔JS agreement is already
pinned by crypto's golden vectors. Kass's stored `wrapped_seeds` and
seed-derived content keys survive unchanged under `Ring{"kass"}`.

### 2.1 Seed lifecycle

```go
func NewSeed() ([]byte, error)                             // 32 random bytes (crypto.NewKey's shape)
func (r Ring) WrapSeed(prf, seed []byte) ([]byte, error)   // SealSym(WrapKey(prf), seed) → iv(12)‖ct
func (r Ring) UnwrapSeed(prf, wrapped []byte) ([]byte, error)
```

`WrapSeed` under a *different* credential's PRF output is the whole of device
add and RPID move — same seed, new wrap. The JS twin exports the same three
plus `contentKey`/`wrapKey`, consuming `webauthn.mjs`'s `prf` output directly.
(One doc note the twin carries: returning raw content-key bytes trades away
kass's non-extractable-CryptoKey hygiene for wrappability — deliberate, said
out loud.)

**The transport contract the package does not build but must name.** Wrapped
seeds live server-side, per credential: kass's `finishEnrol` accepts a
`wrappedSeed`, its sign-in finish returns one. The keyring's README documents
this as the required app contract — *store a wrapped seed keyed by credential
ID; return it on sign-in; accept a new one on enrol* — and the sealed store
(#77) is where the generated version of that surface belongs. Without naming
it, §2.4's drill would silently assume a route pair every app hand-rolls,
which is the exact complaint this issue exists to close.

### 2.2 Member identity and grants

A member (a coach, a second device acting as a peer, the sidecar in #81) is a
`crypto.Keypair`'s box half. Grants wrap a content key — one key, one
instance, never the seed:

```go
func (r Ring) Grant(memberBoxPub, contentKey []byte) ([]byte, error)
    // crypto.Seal(memberBoxPub, ns+"/grant/v1", contentKey) → ephPub(65)‖iv(12)‖ct
func (r Ring) OpenGrant(memberBoxPriv, sealed []byte) ([]byte, error)
```

`OpenGrant` takes the box *half* only, not a full `crypto.Keypair`: kass
member identities are ECDH-only pairs stored as pkcs8, and forcing a dummy
sign half on them would make the migration path a lie. (The JS twin accepts
the pkcs8/JWK import the app already holds.)

Revocation is the server deleting the grant row — nothing cryptographic here,
and the doc comment says so (kass's rule: "taking it back is deleting one
row"). Re-keying after revocation, when an app wants it, is mint-new-content-
key + re-grant to remaining members; the spec documents the ceremony but the
package adds no machinery for it in v1.

### 2.3 The wraps rule

One lifecycle rule is worth enforcing in code because getting it wrong loses
data forever: **the last wrap is unrevokable** (messenger's `vault.revoke`
guard — a seed with zero wraps is a seed no one can ever open again).

```go
type Wrap struct{ ID, Kind, Label, UID string; CredentialID []byte; Wrapped []byte }
func AddWrap(wraps []Wrap, w Wrap) []Wrap                // dedupe by ID (messenger's addWrap)
func RemoveWrap(wraps []Wrap, id string) ([]Wrap, error) // ErrLastWrap on len==1; ErrUnknownWrap on miss
```

`UID` names the member a wrap belongs to — messenger's enroll/give flows need
it, and dropping it would leave the type kass-shaped only. Messenger's two
sibling invariants travel as documentation on the type (durable enrolment
replaces a member handoff; handoffs are `Kind: "member"`): they are policy
about *which* wraps coexist, which the app's flow owns. One caution stated
plainly: an event-sourced consumer folding untrusted input (messenger's
model) must treat `ErrLastWrap` as a no-op, not a crash — errors are for
interactive callers. Pure — the app persists wraps wherever it likes; the
guard travels with the type.

### 2.4 The RPID-move drill

`webauthn.Config.LegacyRPID` is already upstream. The keyring documents the
three-phase drill (from kass's `make rpid-move`) as the package's longest doc
comment plus a README section, phase by phase:

1. **Old name** — normal operation, seed wrapped under old-RPID passkeys.
2. **Crossover** — serve under the new RPID with `LegacyRPID` set to the old
   one (assertions only); a fresh device signs in via the legacy fallback,
   unwraps the seed, and enrols a new-RPID credential — `WrapSeed`, same seed.
3. **Settled** — `LegacyRPID` removed; only new-name credentials remain.

The *executable* drill (a browser walking all three phases) is a harness
scenario and lands with #80; this spec only commits the API (`WrapSeed` is
sufficient) and the documentation.

## 3. Vectors

`keyring/testdata/golden.json`, generated by a small in-repo generator. Wrap
and grant mint random IVs and ephemeral keys, so wrap *outputs* are not
vectorable — the vectors pin the deterministic directions, crypto's own
golden pattern: purpose derivation for a fixed seed and namespace, `UnwrapSeed`
of a pinned wrapped blob under a fixed PRF output, `OpenGrant` of a pinned
sealed grant with a fixed member key, round-trips for the randomised
directions, and the `ns = "kass"` strings verbatim. Go test replays them; the JS twin
replays the same file via `node --test` with the `js_test.go` exec shim
(crypto's pattern — skip without node). Because grant bytes are
wire-format-forever, the vectors file is **hash-pinned** (eventlog's
`TestMergeVectorsFileUntouched` pattern): fix implementations, not vectors.

## 4. Kass migration (informative, not in this repo)

Under `Ring{"kass"}`: seeds, wrapped seeds, PRF salt, and content keys are
byte-identical — no data migration. Two things do migrate: coach **grants**
(each client re-wraps their content key to each coach with `Grant`,
client-side at next unlock, replacing the grant row; old grants become
unreadable to new code and the window is managed app-side) and coach
**identities** (kass stores ECDH pairs as pkcs8; the keyring's JS twin
imports pkcs8 directly per §2.2, so this is an import-path change, not a
re-mint — but it is a migration step and belongs on kass's checklist). This
lands in kass as a follow-up, not here.

## 5. Out of scope

- Grant/membership tables and routes — #77 (sealed store).
- The sidecar's keypair handling — #81 (consumes `Grant`/`OpenGrant` as-is).
- Invite ceremonies — `crypto.DeriveInvite` exists; the flow is #82.
- Passphrase recovery — ruled out family-wide ("recovery is a passkey, never
  a passphrase", messenger #995).

## 6. Testing

Golden vectors both languages (§3); unit tests for `ErrLastWrap`, wrong-PRF
unwrap failure, wrong-member grant opening failure; SKILL.md gains a keyring
paragraph only if the byte budget allows (currently 14 bytes of headroom —
any addition must trim elsewhere first).
