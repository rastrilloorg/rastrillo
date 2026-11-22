# Root calls: root-signed requests into instances

**Date:** 2026-11-22
**Status:** draft for review on branch `rootcall`
**Asked for in:** rastrillo/rastrillo #332 (Slopbox, Paul 2026-11-22:
build it upstream rather than wait). Slopbox's design is
`slopbox/slopbox` `docs/superpowers/specs/2026-11-22-carloku-plaza-design.md`
§12 and gap R2 in §17.
**Builds on:** the multi-instance design
(`2026-11-19-multi-instance-apps-design.md`, §6.2 handoff and §6.3
roster delivery).

## 1. Why

A root app already reaches its instances in two signed ways: a handoff
assertion (a person signing in) and a roster snapshot (who belongs).
Slopbox's root also has to tell an instance its plan (trial, pending,
paid, paused) and pull the instance's daily MB-day readings. Neither
existing token fits, and neither may be stretched to fit:

- A handoff assertion signs a person in. A captured plan push must never
  be usable as one, nor the reverse.
- A roster snapshot replaces the whole member list. It has no room for
  "this request, once".

Every app with a root will want calls like these (billing, usage,
settings pushed from the root), so the primitive belongs in core.

## 2. Goals and non-goals

**Goals**
- The root signs one HTTP request: method, request target, body. The
  instance can tell it came from the root, was meant for this instance,
  is fresh, has not been seen before, and was not altered.
- The signature cannot be replayed as a handoff or a roster, nor they as
  it, although one key signs all three.
- The instance trusts the key material it already has:
  `instance.Setup.Root` and `instance.Setup.Keys`
  (`RASTRILLO_ROOT`, `RASTRILLO_ROOT_KEYS`).
- One wrapper on the instance side; one request builder on the root
  side; a fake root in `handofftest`.

**Non-goals**
- Calls from an instance to the root. That needs an instance identity
  the platform does not issue yet (Slopbox gap C4).
- Signing responses. The root reaches the instance over HTTPS through
  the platform's edge; a response is as trustworthy as the TLS
  connection, as it is for roster delivery today.
- Authorising inside the call. The signature says "the root sent this";
  what the root may ask is the handler's business.

## 3. The token

The envelope is the one assertion and roster use: base64url (no padding)
of the JSON payload, a dot, base64url of a 64-byte P-256 ECDSA signature
over `context || 0x00 || payload` (`crypto.Sign`). The context is
`rastrillo/rootcall/v1`, distinct from `rastrillo/assertion/v1` and
`rastrillo/roster/v1`. That is the whole defence against cross-use: the
same payload bytes signed under another context do not verify here.

```json
{
  "v": 1,
  "iss": "https://root.example.com",
  "aud": "https://box1.example.net",
  "kid": "key_1",
  "jti": "AAECAwQFBgcICQoLDA0ODw",
  "iat": 1700000000,
  "exp": 1700000060,
  "method": "POST",
  "target": "/_slopbox/plan",
  "body": "<base64url SHA-256 of the exact body bytes>"
}
```

| Field | Rule |
| --- | --- |
| `v` | `1`. |
| `iss` | The root's origin (`wire.Origin`). Must equal the verifier's issuer. |
| `aud` | The instance's platform origin, `https://` and `carlos.Host()`, as for roster. Not the issuer. |
| `kid` | 1–64 of `[A-Za-z0-9_-]`, a key the instance trusts. |
| `jti` | 16 random bytes, base64url: 22 characters. Single use. |
| `iat`, `exp` | Unix seconds. `0 < iat < exp`, `exp − iat ≤ 120`. |
| `method` | 1–16 upper-case letters. |
| `target` | The request target as sent on the request line: path and query, 1–2048 bytes of printable ASCII, starting with `/`. |
| `body` | 43 characters: base64url SHA-256 of the body, the hash of zero bytes for none. |

The token is at most 8 KiB. It rides in `Authorization: Rootcall
<token>`. `Authorization`, not a header of our own, because Go's client
drops it on a redirect to another host, and shared caches do not store
a response to a request that carries it.

**The target is `Request.RequestURI` on the instance**, the request
line as received. An absolute-form target (from a proxy, or
`httptest.NewRequest`) is reduced to its path and query, and refused
unless the authority ends at a `/`: one ending at `?` would let a slash
inside the query pass for the path (Astra's review, P1). It is unchanged by `http.StripPrefix` and by
`basepath`, which rewrite `URL.Path` only, so a guard mounted under a
prefix still compares what the root signed. The root signs
`URL.RequestURI()` of the URL it sends to, which is what Go's client
writes on the request line.

Unknown fields are refused, after the signature has passed, as roster
does: an unsigned token for another purpose reads as a bad signature,
not as odd fields.

## 4. The instance side

```go
guard, err := rootcall.New(rootcall.Config{
	Root:     setup.Root,
	Keys:     setup.Keys,
	Audience: "https://" + carlos.Host(),
	DB:       db,
})
mux.Handle("POST /_carlos/slopbox/plan", guard.Wrap(planHandler))
```

`Wrap` answers, in order:

1. 404 when the request's host is not the audience's (roster's rule:
   nothing else is answered there).
2. 403 when there is no `Rootcall` credential, or it does not verify:
   signature, issuer, audience, version, lifetime, expiry (30 seconds of
   skew allowed for `iat`, as roster).
3. 413 when the body is over `Config.MaxBody` (default 1 MiB); 400 when
   it cannot be read.
4. 403 when the method, target or body hash differ from what was
   signed.
5. 403 when the jti has been spent. Spending is one `INSERT ... ON
   CONFLICT DO NOTHING` into `rootcall_used`, so two copies racing in
   two processes cannot both pass.
6. 403 when the call has expired by the time its ID is spent. Reading a
   stalled body, or waiting on the database, can outlast the call and
   the sweep that forgot its ID (Astra's review, P1); an ID spent before
   expiry cannot have been swept yet.
7. Otherwise the body is handed on intact, the verified claims go on the
   request context (`rootcall.FromContext`), and the handler runs.

Every check runs before the jti is spent, so a forged or altered request
cannot burn a genuine one. The jti is spent before the handler runs, so
a call is used once whatever the handler does; a root that retries signs
a new call. Plan pushes are idempotent by sequence and usage pulls are
reads, so a retry is safe for Slopbox's uses.

Refusals are logged with the error, which names no submitted value.

**The sweep.** After a call is spent, rows whose call expired more than
120 seconds earlier are deleted in the same request. The margin keeps a
replay refused by a second process whose clock is up to two minutes
behind. Calls are few (a handful per instance per day), so the delete is
cheap, and no app has to schedule a sweep.

**Mounting under `/_carlos/`.** `instance.RedirectToOrigin` sends every
path outside `/_carlos/` (and `/api/version`, `/healthz`) on the
platform host to the custom origin. A root call goes to the platform
host, so mount guarded handlers under `/_carlos/<app>/...`, or outside
`RedirectToOrigin`. A redirected call fails closed anyway: the audience
is the platform host, and Go's client drops `Authorization` on the
cross-host hop.

## 5. The root side

```go
caller := rootcall.Caller{Origin: root, Key: key, KeyID: "key_1"}
request, err := caller.NewRequest(ctx, http.MethodPost,
	"https://box1.example.net/_carlos/slopbox/plan", body)
response, err := client.Do(request)
```

`NewRequest` takes the audience from the URL's scheme and host, signs a
fresh jti with a 60-second lifetime, and sets the body and
`Authorization`. `rootcall.Sign` is there for a root that builds its own
requests. `handofftest.Root.Call` signs with the fake root's key.

## 6. Sharing the envelope

Assertion and roster each carry the envelope (join, split, the
86-character signature, canonical base64url) and the trust-set check
(1–8 keys, valid key ids, 65-byte points on P-256). A third copy would
be one more place to get it wrong, and `internal/wire` exists for
exactly this ("Fix them here once"). So `wire.Join`, `wire.Split` and
`wire.TrustSet` move there. Roster moves onto them, pinned by its
golden vector and tests. Assertion keeps its own copy for now: its
framing differs slightly (an encoded-length check before decoding), and
it is pinned by a Node fixture that this change has no reason to
disturb.

## 7. Testing

- Golden vector generated by Node WebCrypto, independent of Go: Go
  verifies the token and the binding of its recorded request. And Node
  verifies a token Go signed.
- Cross-context: the same payload signed under the assertion and roster
  contexts is refused; a root-call token is refused by the assertion and
  roster verifiers.
- Each refusal: wrong audience (in the token, and on the host), wrong
  issuer, unknown key, expired, too long a lifetime, from the future,
  each field malformed, unknown fields, method, target and body
  tampering, a reused jti (also from a second guard on the same
  database), a body over the limit.
- The sweep deletes only rows past the margin.

## 8. Order of work

1. This note.
2. `internal/wire`: `Join`, `Split`, `TrustSet`; roster onto them.
3. `rootcall`: claims, `Sign`, `Verifier`, vectors.
4. `rootcall`: `Caller`, `Guard`, `Schema`; `handofftest.Root.Call`.
5. Docs: reference page, nav, skill, CHANGELOG.
