# 🤖 Signed identity assertions: a fourth identity plugin

Design, 2026-08-31. Filed as amadan `rastrillo/rastrillo` discussion #1
by the Sheets and Docs design sessions, jointly. **NOT RULED** — this
is the written form for Paul's review, and no code has been written.
§9 is the list of things only he can settle.

Instance-per-account makes single sign-in a day-one problem, and
`decisions.md` says it directly: "three instances must not mean three
credentials." Two apps arrived at the same answer in the same week
without seeing each other's code — one identity origin holds the
credentials and runs the ceremony; instances receive identity as a
signed assertion and verify a signature. Instances never run WebAuthn,
never hold a passkey RP, and never see a password.

That is Tito's recorded pattern, and the parenthetical is the whole
point:

> Identity lives beside the router, outside the per-account blast
> radius, and reaches instances only as signed assertions — headers are
> stripped-and-re-minted, and signing (not stripping alone) is what
> stops a peer socket from forging them.

Neither app is blocked. Both can hand-roll verification. The ask is
that they not do it twice, differently, at a trust boundary where the
failure modes are silent.

## 0. The one thing the report did not ask for, and needs

The discussion asks for a **verifier**. It does not mention a minter.

But the identity origin in both named deployments is itself a rastrillo
app — Sheets' home credential origin is where the passkey RP lives. So
somebody has to sign these assertions, and if the framework ships only
the verifying half, the signing half gets hand-rolled once per origin.
A signer that disagrees with its verifier about one byte of the signed
input is the classic interop bug, and it fails in the least legible way
available: intermittently, at a trust boundary, under load.

**Recommendation: ship both halves in one package, with golden
vectors pinning the wire format**, exactly as `crypto` does. That is
also what makes the format reviewable — a vector file is a thing a
person can check, where "both sides call the same function" is not.

This is the first thing to rule on, because it sizes everything else.

## 1. What this composes, and what it must not invent

The `keyring` package records the ruling this design inherits: *no new
cryptography*. Every operation composes `crypto`'s existing,
golden-vectored primitives.

That gets one failure mode off the list for free. The report names
**algorithm confusion** as "the evergreen one" — and it is, in a JOSE
world, because a JWT carries an `alg` header the attacker can edit.
`crypto` has no algorithm agility at all:

```go
func Sign(kp *Keypair, context string, msg []byte) ([]byte, error)
func Verify(signPub []byte, context string, msg, sig []byte) bool
```

One curve (ECDSA P-256), one hash (SHA-256), one encoding (raw r‖s,
32+32 zero-padded), and a caller-supplied `context` string that domain-
separates every use. There is no field in which to say "none", and no
second algorithm to downgrade to. **Algorithm confusion is designed
out here rather than defended against** — which is a reason to build
this on `crypto` rather than on a JWT library, and a reason the wire
format should not be a JWT even in shape.

The pinned context string is `rastrillo/assertion/v1`. Pinned means
pinned: changing it strands every origin already signing under it.

## 2. The wire format

An assertion is a compact, URL-safe string — it travels in a query
parameter or an `Authorization` header, so it must survive both:

```
base64url(payload JSON) "." base64url(signature)
```

The payload:

| Field | Type | Meaning |
|---|---|---|
| `v` | int | Format version. `1`. |
| `iss` | string | The identity origin, scheme included. |
| `aud` | string | The instance this assertion is for, scheme included. |
| `sub` | string | Opaque person ref, stable per person at this origin. |
| `kid` | string | Which signing key. |
| `jti` | string | 128 bits of randomness, base64url. Single use. |
| `iat` | int | Unix seconds, when minted. |
| `exp` | int | Unix seconds, when it stops being accepted. |

`kid` sits **inside** the signed payload, and is read from the
untrusted payload only to select which public key to verify against.
Swapping it is not an attack: the signature covers it, so a swapped
`kid` fails verification against every key. Reading it first just
avoids trial-verifying against each key in turn.

Verification order matters, and the order is: decode, check `v`, select
key by `kid`, **verify the signature**, and only then look at any other
claim. Nothing before the signature check may have a side effect.

## 3. Audience binding

The report expects this to be the one two independent implementations
get subtly different, and it is right to.

**`aud` must equal this instance's own origin, compared byte for
byte.** Not a suffix match, not a prefix match, not a host-only
comparison that ignores the scheme, no wildcards, and no list of
trusted audiences.

The value comes from the same `Origin` the app already hands
`csrf.Protect` and `sessions.New` — one origin string per app,
configured once, already load-bearing for two other security
decisions. The plugin refuses to construct with an empty one, the way
`auth` refuses an empty `InstanceKey`.

`iss` is checked the same way against a single configured identity
origin. A *list* of accepted issuers is how a multi-tenant deployment
quietly becomes cross-tenant, so the config takes one — and if
migrating between origins ever needs two, that is a dated, deliberate
addition with its own review, not a slice in the config from day one.

Both apps are multi-tenant by construction, so every deployment has
thousands of sibling instances to replay against. This check is what
stands between them.

## 4. Replay: single use, short expiry, and a store that prunes itself

Two mechanisms, neither sufficient alone.

**Short expiry.** `exp - iat` measured in seconds, not minutes. The
assertion is a handoff, not a credential; the long-lived thing is the
session minted afterwards. Proposed default 60s, with a **hard ceiling
enforced in code** (300s) so that a config typo cannot turn a handoff
into a bearer token with a long tail. `iat` more than 30s in the future
is refused — clock skew is real, but unbounded skew tolerance is just a
longer expiry wearing a hat.

**Single use.** `jti` recorded in the instance's own table:

```sql
CREATE TABLE IF NOT EXISTS assertion_seen (
  jti        TEXT PRIMARY KEY,
  expires_at TEXT NOT NULL
);
```

The insert *is* the check: a `PRIMARY KEY` conflict means this
assertion has been presented before, and the answer is refusal. No
read-then-write, so two simultaneous presentations cannot both win.

**The pruning requirement is Docs', and it is the sharp one.** Docs'
guest population is dominated by single-purpose visitors — somebody
shared one document, signed in once to comment, never returned. So the
replay store churns against a population far larger than any membership
list and unbounded in time.

The property that makes this fine, stated explicitly because it is the
thing an implementer needs to believe: **the table's steady-state size
is (assertions per second × assertion lifetime), which is bounded by
traffic and independent of how many people have ever signed in.** A
minute of expiry and a hundred sign-ins a second is six thousand rows.
Ten million lifetime visitors do not appear in that number.

That is only true if rows actually leave. Two mechanisms, and the
design wants both:

- An exported `Sweep(now)`, the shape `sessions.Sweep` already set, for
  an app that wires a scheduled tick.
- **Opportunistic deletion of expired rows in the same transaction as
  the insert.** This is the part that must not be left to the app,
  because "single-use" is the framework's promise, not the app's, and a
  deployment that never wires the sweep would otherwise grow a table
  for the life of the deployment. The report's phrasing is exactly
  right: correct on the first day, and a growing table forever after.

## 5. Keys: pinned, with a rotation window

Config carries public keys by `kid`:

```go
Keys map[string][]byte   // kid -> ECDSA P-256 public key
```

Several are valid at once, which is what makes rotation possible
without a flag day: the origin keeps signing under `kid1` while every
instance learns `kid2`; the origin switches to `kid2`; `kid1` comes out
of the instances later. Three deploys, no coordinated instant.

**Recommendation: pinned keys in config, not a fetched JWKS.** The
argument is doctrinal, not just operational. The vault handoff records
its own version of it — *"The instance and the home never speak —
Eleven's model, kept deliberately."* A fetch would put a network call,
a cache, a cache-invalidation policy and an SSRF surface into the
sign-in path, and would make verification fail when the origin is
merely slow. Pinned keys keep an instance able to verify entirely
offline.

The cost is honest and should be stated: rotation becomes a config
deploy to every instance rather than a change at the origin. With
thousands of instances that is a real operational burden, and it is the
strongest argument for fetching. It is §9's second question.

## 6. The admission surface

The plugin's contract with the core is the one every identity plugin
already has — verify a credential, call `sessions.SignIn` — so this is
a fourth plugin, not a new seam.

```go
type Assertion struct {
    Subject  string
    Issuer   string
    IssuedAt time.Time
}
```

The minted session is `sessions.Session{Subject, Method: "assertion",
AuthTime: iat}`.

Two hooks, mirroring `auth` so an app moving between them meets the
same shapes:

- `Authorize func(subject string) bool` — admission. Nil admits every
  verified subject. Membership stays app policy, as it is everywhere
  else.
- `SubjectFor func(subject string) (string, error)` — remap the
  origin's ref to whatever the app keys its own rows by. This mirrors
  the hook that just landed on `auth.Config` for the server-blind case
  (discussion #2), and the two want to be the same shape for the same
  reason: an app should not have to care which plugin minted the
  session before it can scope a query.

Note that `sub` is already opaque here, so `SubjectFor` is a
convenience rather than a privacy fix — the origin, not the instance,
decided what the ref is.

## 7. Assertions carry identity, never authorization

Recording the report's argument, because it is right and because a
reviewer will otherwise propose the opposite as an improvement.

A guest's assertion is the same shape as an owner's. The instance
decides capability from its own grants.

It is tempting to narrow a comment-only visitor's assertion so it
cannot do more. That narrowing belongs on the wrong side of the
boundary: encoding capability requires the identity origin to hold an
authorization model, which is a much larger change than it looks. And
the security argument does not survive contact — a leaked assertion
buys exactly the grants of the person it names, which is what a stolen
session buys anyway. So narrowing reduces no blast radius while adding
a second assertion type, its key handling, and a second surface to get
audience binding right on.

It is also what keeps this small enough to be a fourth plugin.

## 8. What this is not: the vault handoff

`vault/handoff.go` already moves a credential between an instance and a
home, and the two must not be conflated.

The vault handoff makes the home a **token custodian**: the instance
mints its own `Method: "vault"` session token and the home stores it,
to be replayed later. Authority stays with the instance; the home holds
an opaque string it cannot mint.

This design makes the origin an **identity authority**: the origin
signs a claim about who someone is, and the instance mints a session on
the strength of that signature. Authority moves to the origin.

They share primitives, the single-use nonce discipline, and the
property that the two servers never speak. They answer different
questions, and an app can want both.

## 9. Open questions — for Paul

1. **Both halves, or verify only?** §0. The report asks for a verifier;
   the origin is itself a rastrillo app, so something must sign.
   Recommendation: both, with golden vectors.
2. **Pinned keys or fetched?** §5. Recommendation: pinned, on the
   never-speak doctrine. The counter-argument is rotation across
   thousands of instances, and it is not weak.
3. **Package name.** `assertion`? `identity`? `handoff` is taken by
   `vault`.
4. **A JS twin?** `crypto`, `keyring` and `vault` all ship one.
   Verification is server-side, so probably not — but if a browser ever
   needs to check an assertion before presenting it, that answer
   changes.
5. **Does it get the `SecondFactor` seam?** `auth` has one. Stepping up
   a federated sign-in with a local passkey is coherent, but it is also
   the thing the whole design is trying to avoid instances doing.
6. **Expiry default and ceiling** — 60s and 300s proposed. Real
   deployments may need more; the ceiling is the part worth arguing
   about, because it is the one a typo runs into.

## 10. Out of scope

- Authorization, roles and membership. §7, and `idear` is where they
  live.
- The origin's own credential ceremony — that is `auth`, `password` and
  `passkey`, already shipped, running at the origin.
- The sealed link store from discussion #2. Same design conversation,
  different change; `auth.Config.SubjectFor` has landed and the link
  store has not.
