# 🤖 Stateless sign-in Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A central app (a home or a hub) can run rastrillo's `auth` with no database: its users and magic links are `objrec` records, its sessions are short-lived signed cookies with an `epoch` and a refresh route, and a path-routed tenant verifies those cookies without ever minting one.

**Architecture:** `sessions` gains a signed mode on the same `*Sessions` type, so every plugin and helper that takes a `*sessions.Sessions` or reads `sessions.Current` works unchanged. With a signing key it mints (the hub); with public keys only it verifies (a tenant). `auth` gains a records mode: a `signin.LinkStore` on `objrec`, `users/` and `emails/` records, an optional blind mode that stores only the email's HMAC, and the `epoch` lookup the refresh route needs. `pow` gains a nonce store on `objrec` so the sign-in front door needs no table either. Both modes are opt-in; an app that sets neither is byte-for-byte today's.

**Tech Stack:** Go stdlib, `amadan.net/rastrillo/rastrillo/crypto` (P-256 sign/verify, HKDF, AES-GCM), `objrec`, `github.com/go-json-experiment/json` (already a dependency, used by `assertion`), `github.com/keymaildev/signin` v0.1.1.

**Spec:** pegamento `docs/superpowers/specs/2026-09-27-user-models-design.md` (branch `design-user-models`) §4, §5, §6.1, §6.3, §8, §9, §11. Roadmap: same branch, `docs/superpowers/plans/2026-09-28-user-models-roadmap.md`, Plan 2. First consumer: ppl, `ppl/docs/superpowers/specs/2026-10-17-ppl-design.md` §4.2, §8, §9, and rastrillo discussion #75.

## Global Constraints

- Module `amadan.net/rastrillo/rastrillo`; Go 1.25; `CGO_ENABLED=0`; `GOFLAGS=-mod=mod` (the Makefile sets both).
- No new dependencies.
- The gate is `make ci`, green before every push.
- **Existing apps keep working unchanged** (Paul, 2026-10-17). With `sessions.Config.Signed` nil and `auth.Config.Records` nil, every existing test passes unmodified and no existing code path changes behaviour. The new modes are opt-in.
- **Blind mode** (Paul, 2026-10-17): the user record stores the address by default; `auth.Config.Blind` stores only `<eh>`. It applies to a home and a hub alike.
- Spec values, verbatim: cookie `__Host-<product>`; P-256 signature, "the same key type and claim discipline as `assertion`"; claims include `sub, sid, epoch, iat, exp, kid`; **15 minutes**; refresh at `/_carlos/refresh?next=<path>`; "up to two `kid`s during rotation"; `<uid>` "a random 128-bit id in base32"; `<eh>` = `HMAC-SHA256(pepper, lowercase(email))`; "Tenants verify, never mint".
- `objrec` keys are lowercase only (`objrec.ValidKey`), so `<uid>` is lowercase unpadded base32 (26 chars, `[a-z2-7]`) and every hash is lowercase hex.
- Every exported symbol appears on its package's reference page (`internal/docsite/symbols_test.go`).
- `SKILL.md` is 31,615 of its 31,630-byte budget. Trim genuinely redundant prose first; if the addition still does not fit, raise `skillBudget` by exactly the overflow and say why in its comment (AGENTS.md).
- Comments say why, naming the failure prevented. Commits: imperative subject, body gives the why.
- User-facing copy (doc prose, CLI output, the refresh error text) is **draft** in Tasks 1 to 8: write it plainly, no em dashes, and expect it to change. Copy review runs once, after the code tasks and before the docs task. Task 9 starts with the `copy-review` pass that rewrites it, so no commit waits on Paul and the prose is reviewed describing what was built. No step in Tasks 1 to 8 runs `copy-review`.
- CONTRIBUTING.md: run each new test against the code before its change, in a separate worktree, and post both runs on the branch. A build failure does not count. Each task's Step 2 is the TDD red step and fails to build. Each task (Task 9 adds no test) therefore ends with an **Evidence** step, done by that task's implementer: in a separate worktree at the task's base commit, add stub versions of the task's new declarations (zero values, `errors.New("stub")`, empty handlers; stubs accept rather than refuse, and never panic), so each new test fails as `--- FAIL: <name>` with its own message. Post the stubs, the commands and both outputs in one "Test evidence" discussion on the branch (`amadan discuss`; Task 1 opens it and each later task appends). Have the branch reviewed by another provider's strongest model before merging.
- Each task adds its own new exported symbols to their reference page, so `go test ./internal/docsite` and `make ci` stay green at every commit. Task 9 writes the guides, `SKILL.md` and the changelog.
- One clock per mode. In records mode, `auth`'s `a.now` drives `signin.Flow.Now`, the link store's `now`, the nonce store's tests, `sessions` (through `Config.Now`) and `RequireFreshSession`. `Flow.Now`, the link store's `now` and `Config.Now` are each the closure `func() time.Time { return a.now() }`, never the value of `a.now`: Task 5's tests reassign `a.now` after `New`, and a captured value would judge links expired while the cookie clock moved (the jar already does this). Row mode keeps every `time.Now` call it has today.
- Land with `amadan branch merge -squash` (AGENTS.md requires squash), then `make mirror`.
- **No live Carloku check in this plan.** Plan 1 proved the store's conditional writes live (#61), and everything here runs on top of them or on cookies and HTTP that fakes cover exactly. If a step turns out to need a live Carloku action, stop and ask Paul for scoped approval at that time: approving this plan is not approving a production action. A path-routed hub and tenant on one real origin needs Plan 5 and platform path routes; that run is Plan 7's end-to-end check in the `demo` account.

## Deviations from the spec (approved by Paul, 2026-10-18)

Paul approved all six ("Sounds good. Go ahead."). Keep them as written; the spec stays the authority for everything else. Each is small, and each is argued in the task that makes it. None changes what a person sees compared with today's `auth`.

1. **Index first, then the user** (§4 says authoritative first). `emails/<eh>.json` is created before `users/<uid>.json`. The email claim is what must be unique, and a claim whose user record is missing is repaired by the next sign-in. Writing the user first leaves orphan users that nothing removes.
2. **`links-used/<yyyy-mm-dd>/<hash>`** instead of `links-used/<hash>`. `objrec.List` returns keys only, so an undated marker can't be swept without reading every one. The date is the link's own, read from where its record was found, so single use holds.
3. **The link record holds `eh` and the address sealed**, not `next`. `signin`'s `TakeLink` must return the address, so the record has to hold it; sealing it under the instance key keeps it out of a bucket listing. `next` stays in auth's `return_to` cookie, as it is today. A link opened on another device lands on `SignedInPath`, exactly as with SQLite `auth`; §6.1 says the sign-in experience does not change. Binding `next` to the link would be a new feature for both modes, not part of this plan.
4. **Claims add `v`, `at`, `m` and `auth_time`.** `at` is the session's start, which bounds refresh to the 30-day session life; `m` and `auth_time` keep `sessions.Session.Method` and `AuthTime` (which `RequireFresh` reads) the same in both modes.
5. **`epoch` starts at a random 62-bit value**, not 0, and "sign out everywhere" adds one. A user record that is deleted and re-created would otherwise revive every old cookie that said `epoch: 0`. A clock value would repeat after a clock step or a restore; a random start collides with an old epoch with negligible probability.
6. **A `pow` nonce store on `objrec`.** Not in the spec, but `auth` refuses to start without a front door, and `pow.SQLNonces` is a table.

## Cross-plan interfaces

- **Plan 5 (base path).** This plan needs two things from it, and neither needs it landed first: (a) it leaves `r.RequestURI` as the client sent it, which `http.StripPrefix` already does, because the tenant builds `next` and `return_to` from it so they keep the `/<slug>` prefix; and (b) it does not rewrite absolute-URL `Location` headers, because a signed-mode tenant redirects to `Origin + "/_carlos/refresh"` and `Origin + SigninPath`, absolute so a base-path rewrite of relative redirects cannot turn them into `/<slug>/_carlos/refresh`. The tests here mount the tenant under `http.StripPrefix("/acme", …)` to stand in for Plan 5.
- **Plan 4 (provision), Plan 3 (assertion adapter):** nothing.
- **Plan 7:** the hub's membership copy and roster delivery (spec §6.4) are Plan 7's and out of scope here, although ppl needs them early (#75).
- **What ppl gets** (#75, asks 2 and 4): hub-minted cookies verified by each profile instance, and a hub with magic link and Keymail on `objrec` storing only the blind index. Passkeys and second factors on a stateless centre are out of scope: both keep tables.

## Review Focus

1. **A key rotation reaches the hub before a tenant.** The hub signs with a `kid` the tenant doesn't hold yet. The tenant must treat the request as signed out, set no cookie and send nobody to refresh. Clearing the shared `__Host-` cookie would sign the person out of the whole product, and redirecting would loop. Pinned in Task 2 (`TestSignedTenantNeverClearsAnUnknownKeyCookie`).
2. **A long form posted after the 15 minutes, or two tabs refreshing at once.** Someone writes a post for 20 minutes and submits. The POST must survive the refresh: the tenant answers 307, the hub answers 307, and the same body reaches the same action. A second tab that reaches refresh after the first has already reissued the cookie must go through, not fail. Pinned in Tasks 2, 3 and 7.
3. **A hostile `next`** (`//evil.example`, `/\evil.example`, `https://evil.example/`, `/%09/evil.example` decoded to a tab, the refresh route itself). Each must land on `/`, never off-origin, never back on refresh. Pinned in Task 3.
4. **A user record deleted and created again.** Every cookie minted before the deletion must stay dead. Pinned in Task 5 (`TestRecordsRecreatedUserDoesNotReviveOldSessions`).
5. **One new address signing in on two devices at once.** Exactly one `uid`, both sessions carry it, and one `users/` record exists. Pinned in Task 5.

---

### Task 1: The signed cookie and its keys

**Files:**
- Create: `sessions/signed.go`
- Test: `sessions/signed_internal_test.go` (package `sessions`)

**Interfaces:**
- Produces (package `sessions`):
  - `const SignedLifetime = 15 * time.Minute`
  - `type SigningKey struct{ /* kid string; kp *crypto.Keypair */ }`
  - `func NewSigningKey(kid string) (*SigningKey, error)`; `kid` is 1 to 32 of `[a-z0-9_-]`
  - `func ParseSigningKey(s string) (*SigningKey, error)`: `"<kid>.<base64url of the 32-byte P-256 scalar>"`
  - `func (k *SigningKey) KeyID() string`, `func (k *SigningKey) Secret() string` (the form `ParseSigningKey` reads), `func (k *SigningKey) Public() string` (`"<kid>.<base64url of the 65-byte uncompressed point>"`)
  - `func ParsePublicKeys(s string) (map[string][]byte, error)`: comma-separated `Public()` values, 1 or 2, distinct kids, each a valid P-256 point
  - unexported, for Tasks 2 and 3:
    - `type signedClaims struct { V int64 \`json:"v"\`; KID string \`json:"kid"\`; Sub string \`json:"sub"\`; SID string \`json:"sid"\`; Epoch int64 \`json:"epoch"\`; IAT int64 \`json:"iat"\`; EXP int64 \`json:"exp"\`; At int64 \`json:"at"\`; Method string \`json:"m"\`; AuthTime int64 \`json:"auth_time"\` }`
    - `func (k *SigningKey) sign(c signedClaims) (string, error)`: sets `c.V = 1`, `c.KID = k.kid`, then applies `checkClaims` and returns its error without signing. Task 2's `SignIn` relies on this to refuse an address as the subject. `sign` has no clock, so the clock rules stay in `verifyCookie`
    - `func checkClaims(c signedClaims) error`: the claim rules of Step 3 (shape and ranges, no clock); `sign` and `verifyCookie` both call it
    - `func validPublicKey(kid string, point []byte) error`: the kid pattern `[a-z0-9_-]{1,32}` and a 65-byte point that `ecdsa.ParseUncompressedPublicKey` accepts. `ParsePublicKeys` and Task 2's `New` both call it, so there is one definition of a valid key
    - `type cookieState int` with `cookieNone`, `cookieValid`, `cookieExpired`, `cookieInvalid`
    - `func verifyCookie(keys map[string][]byte, value string, now time.Time) (signedClaims, cookieState)`
    - `func newSID() (string, error)`: 16 random bytes, base64url, 22 chars. Task 3 calls it; Task 1 tests it so staticcheck's unused check passes at this commit

The wire form is `assertion`'s: `base64url(payload) "." base64url(64-byte r‖s)`, signed with `crypto.Sign` under the context `"rastrillo/sessions/signed/v1"`. The context differs from `assertion`'s `"rastrillo/assertion/v1"`, so a handoff token can never pass as a session cookie, or the other way round.

- [ ] **Step 1: Write the failing tests**

```go
package sessions

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
)

var t0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func testKey(t *testing.T, kid string) *SigningKey {
	t.Helper()
	k, err := NewSigningKey(kid)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func keysOf(t *testing.T, ks ...*SigningKey) map[string][]byte {
	t.Helper()
	var parts []string
	for _, k := range ks {
		parts = append(parts, k.Public())
	}
	m, err := ParsePublicKeys(strings.Join(parts, ","))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func goodClaims() signedClaims {
	return signedClaims{Sub: "a2b3c4d5e6f7a2b3c4d5e6f7a2", SID: "AAAAAAAAAAAAAAAAAAAAAA", Epoch: 7,
		IAT: t0.Unix(), EXP: t0.Add(SignedLifetime).Unix(), At: t0.Add(-time.Hour).Unix(), Method: "magiclink"}
}

// signRaw signs arbitrary payload bytes, so a test can present claims
// sign() would never build and prove the verifier refuses them anyway.
func signRaw(t *testing.T, k *SigningKey, payload string) string {
	t.Helper()
	sig, err := crypto.Sign(k.kp, "rastrillo/sessions/signed/v1", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestSignedCookieRoundTrips(t *testing.T) {
	k := testKey(t, "k1")
	v, err := k.sign(goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	c, st := verifyCookie(keysOf(t, k), v, t0.Add(time.Minute))
	if st != cookieValid || c.Sub != goodClaims().Sub || c.Epoch != 7 || c.KID != "k1" || c.V != 1 {
		t.Fatalf("got %+v, %v", c, st)
	}
}

func TestSignedCookieExpiresAtExp(t *testing.T) {
	k := testKey(t, "k1")
	v, _ := k.sign(goodClaims())
	exp := t0.Add(SignedLifetime)
	if _, st := verifyCookie(keysOf(t, k), v, exp.Add(-time.Second)); st != cookieValid {
		t.Fatalf("a second before exp: %v", st)
	}
	if c, st := verifyCookie(keysOf(t, k), v, exp); st != cookieExpired || c.Sub == "" {
		t.Fatalf("at exp: %v; an expired cookie must still yield its claims for refresh", st)
	}
}

func TestSignedCookieRefusesTampering(t *testing.T) {
	k1, other := testKey(t, "k1"), testKey(t, "k1")
	v, _ := k1.sign(goodClaims())
	p, s, _ := strings.Cut(v, ".")
	flip := func(x string) string { b := []byte(x); b[3] ^= 1; return string(b) }
	for name, bad := range map[string]string{
		"payload": flip(p) + "." + s, "signature": p + "." + flip(s),
		"two dots": v + ".x", "empty": "", "oversized": strings.Repeat("a", 4097),
	} {
		if _, st := verifyCookie(keysOf(t, k1), bad, t0); st == cookieValid || st == cookieExpired {
			t.Errorf("%s: %v", name, st)
		}
	}
	if _, st := verifyCookie(keysOf(t, other), v, t0); st != cookieInvalid {
		t.Errorf("same kid, different key: %v", st)
	}
	if _, st := verifyCookie(keysOf(t, testKey(t, "k2")), v, t0); st != cookieInvalid {
		t.Errorf("unknown kid: %v", st)
	}
}

func TestSignedCookieRefusesAnAssertion(t *testing.T) {
	k := testKey(t, "k1")
	id1, _ := assertion.RandomID()
	id2, _ := assertion.RandomID()
	tok, err := assertion.Sign(k.kp, assertion.Claims{Version: 1, Issuer: "https://hub.test", Audience: "https://t.test",
		Subject: "u", KeyID: "k1", ID: id1, Nonce: id2, IssuedAt: t0.Unix(), ExpiresAt: t0.Unix() + 60, AuthTime: t0.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, st := verifyCookie(keysOf(t, k), tok, t0); st != cookieInvalid {
		t.Fatalf("an assertion signed by the session key passed as a session: %v", st)
	}
}

func TestSignedCookieRefusesBadClaims(t *testing.T) {
	k := testKey(t, "k1")
	ok := `"kid":"k1","sub":"u1","sid":"AAAAAAAAAAAAAAAAAAAAAA","epoch":1,"iat":%d,"exp":%d,"at":%d,"m":"magiclink","auth_time":0`
	iat, exp, at := t0.Unix(), t0.Unix()+900, t0.Unix()-60
	for name, payload := range map[string]string{
		"version 2":       `{"v":2,` + fmtc(ok, iat, exp, at) + `}`,
		"no version":      `{` + fmtc(ok, iat, exp, at) + `}`,
		"unknown member":  `{"v":1,"admin":true,` + fmtc(ok, iat, exp, at) + `}`,
		"duplicate sub":   `{"v":1,"sub":"other",` + fmtc(ok, iat, exp, at) + `}`,
		"life over 15m":   `{"v":1,` + fmtc(ok, iat, iat+901, at) + `}`,
		"starts after iat": `{"v":1,` + fmtc(ok, iat, exp, iat+1) + `}`,
		"iat 31s ahead":   `{"v":1,` + fmtc(ok, iat+31, iat+31+900, at) + `}`,
		"subject is an address": `{"v":1,` + strings.Replace(fmtc(ok, iat, exp, at), `"u1"`, `"a@b.c"`, 1) + `}`,
		"short sid":       `{"v":1,` + strings.Replace(fmtc(ok, iat, exp, at), "AAAAAAAAAAAAAAAAAAAAAA", "AAAA", 1) + `}`,
	} {
		if _, st := verifyCookie(keysOf(t, k), signRaw(t, k, payload), t0); st != cookieInvalid {
			t.Errorf("%s: %v, want invalid", name, st)
		}
	}
}

func TestSigningKeyFormats(t *testing.T) {
	k := testKey(t, "k1")
	back, err := ParseSigningKey(k.Secret())
	if err != nil || back.Public() != k.Public() || back.KeyID() != "k1" {
		t.Fatalf("round trip: %v", err)
	}
	for _, bad := range []string{"", "k1", "K1." + strings.SplitN(k.Secret(), ".", 2)[1], "k1.AAAA",
		"k1." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))} {
		if _, err := ParseSigningKey(bad); err == nil {
			t.Errorf("ParseSigningKey(%q) accepted", bad)
		}
	}
	if _, err := NewSigningKey("Has Space"); err == nil {
		t.Error("a kid outside [a-z0-9_-] was accepted")
	}
}

func TestParsePublicKeys(t *testing.T) {
	a, b, c := testKey(t, "a"), testKey(t, "b"), testKey(t, "c")
	if m, err := ParsePublicKeys(a.Public() + "," + b.Public()); err != nil || len(m) != 2 {
		t.Fatalf("two keys: %v %v", m, err)
	}
	for name, bad := range map[string]string{
		"none": "", "three": a.Public() + "," + b.Public() + "," + c.Public(),
		"same kid twice": a.Public() + "," + a.Public(), "not a point": "a." + base64.RawURLEncoding.EncodeToString(make([]byte, 65)),
	} {
		if _, err := ParsePublicKeys(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
```

```go
// Every member is required, as assertion requires its own: a missing
// or null epoch must not decode to a zero that some cookie really has.
func TestSignedCookieRefusesAMissingOrNullClaim(t *testing.T) {
	k := testKey(t, "k1")
	full := map[string]string{"v": "1", "kid": `"k1"`, "sub": `"u1"`, "sid": `"AAAAAAAAAAAAAAAAAAAAAA"`, "epoch": "1",
		"iat": fmtc("%d", t0.Unix()), "exp": fmtc("%d", t0.Unix()+900), "at": fmtc("%d", t0.Unix()-60), "m": `""`, "auth_time": "0"}
	for name := range full {
		for _, how := range []string{"missing", "null"} {
			var parts []string
			for n, v := range full {
				switch {
				case n == name && how == "missing":
					continue
				case n == name:
					v = "null"
				}
				parts = append(parts, `"`+n+`":`+v)
			}
			if _, st := verifyCookie(keysOf(t, k), signRaw(t, k, "{"+strings.Join(parts, ",")+"}"), t0); st != cookieInvalid {
				t.Errorf("%s %s: %v", name, how, st)
			}
		}
	}
}

// sign applies the claim rules, so a hub cannot mint a cookie that
// verifyCookie would refuse and Task 2's SignIn can refuse an address as
// the subject. It does not judge the clock: it has none.
func TestSignRefusesClaimsVerifyWouldRefuse(t *testing.T) {
	k := testKey(t, "k1")
	for name, mut := range map[string]func(*signedClaims){
		"subject is an address": func(c *signedClaims) { c.Sub = "a@b.c" },
		"empty subject":         func(c *signedClaims) { c.Sub = "" },
		"short sid":             func(c *signedClaims) { c.SID = "AAAA" },
		"negative epoch":        func(c *signedClaims) { c.Epoch = -1 },
		"life over 15m":         func(c *signedClaims) { c.EXP = c.IAT + 901 },
		"no life":               func(c *signedClaims) { c.EXP = c.IAT },
		"starts after iat":      func(c *signedClaims) { c.At = c.IAT + 1 },
		"auth_time after iat":   func(c *signedClaims) { c.AuthTime = c.IAT + 1 },
		"method with a space":   func(c *signedClaims) { c.Method = "magic link" },
	} {
		c := goodClaims()
		mut(&c)
		if v, err := k.sign(c); err == nil || v != "" {
			t.Errorf("%s: signed (%q, %v)", name, v, err)
		}
	}
	old := goodClaims()
	old.IAT, old.At, old.EXP = 100, 100, 1000 // long past: whether it is live is verifyCookie's call
	if _, err := k.sign(old); err != nil {
		t.Errorf("sign judged the clock: %v", err)
	}
}

func TestNewSID(t *testing.T) {
	a, err := newSID()
	if err != nil || len(a) != 22 {
		t.Fatalf("%q %v", a, err)
	}
	if b, err := base64.RawURLEncoding.DecodeString(a); err != nil || len(b) != 16 {
		t.Fatalf("%q does not decode to 16 bytes: %v", a, err)
	}
	if b, _ := newSID(); b == a {
		t.Fatal("two calls returned one id")
	}
}

func TestValidPublicKey(t *testing.T) {
	_, enc, _ := strings.Cut(testKey(t, "k1").Public(), ".")
	point, _ := base64.RawURLEncoding.DecodeString(enc)
	if err := validPublicKey("k1", point); err != nil {
		t.Fatalf("a real key: %v", err)
	}
	for name, c := range map[string]struct {
		kid string
		pt  []byte
	}{
		"kid in capitals": {"K1", point}, "empty kid": {"", point}, "kid of 33 chars": {strings.Repeat("a", 33), point},
		"short point": {"k1", point[:64]}, "zero point": {"k1", make([]byte, 65)},
	} {
		if validPublicKey(c.kid, c.pt) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
```

`fmtc` is `fmt.Sprintf` under a short name; add it to the file. The zero scalar case is the fifth `ParseSigningKey` input.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions -run 'Sign|ParsePublicKeys|NewSID|ValidPublicKey'`
Expected: FAIL, undefined `NewSigningKey`, `signedClaims`, `verifyCookie`, `newSID`, `validPublicKey`.

- [ ] **Step 3: Implement `sessions/signed.go`**

Verification order, each failure `cookieInvalid`: length ≤ 4096 and exactly one `.`; signature part exactly 86 chars decoding to 64 bytes; both parts strict base64url that re-encode to themselves (as `assertion.decode`); payload a JSON object, decoded first into `map[string]jsontext.Value` with `go-json-experiment` (v2 refuses duplicate names by default), where all ten members (`v kid sub sid epoch iat exp at m auth_time`) must be present and none `null`, and no other member may appear, then into `signedClaims`; numbers are plain integers (no `+`, no fraction), as `assertion.parse` reads them; `v == 1`; `kid` in `keys`; `crypto.Verify`; then the claim rules: `sub` 1 to 128 of `[A-Za-z0-9_-]`, `sid` 22 chars decoding to 16 bytes, `m` empty or 1 to 32 of `[a-z0-9_-]`, `epoch ≥ 0`, `iat > 0`, `0 < at ≤ iat`, `iat < exp ≤ iat + 900`, `0 ≤ auth_time ≤ iat`. Those are `checkClaims`, which `sign` runs too, so whatever `sign` mints passes them. The clock rules are `verifyCookie`'s alone: `iat - 30 ≤ now`, and only then `exp ≤ now`, which is `cookieExpired` with the claims returned.

`ParseSigningKey` refuses a scalar of 0 or ≥ the curve order (build the key with `ecdsa.ParseRawPrivateKey`, which checks both) and leaves `Keypair.BoxPriv` nil: a session key never opens a box. `Secret` is a method, not `String`, so a `%v` in a log line never prints the key.

`validPublicKey` holds the kid pattern and the point check once. `ParsePublicKeys` calls it for each entry and adds only the count (1 or 2) and distinct-kid rules.

- [ ] **Step 4: Add the symbols to `docs/site/reference/sessions.md`**

A new section, "Signing keys": `SignedLifetime`, `SigningKey`, `NewSigningKey`, `ParseSigningKey`, `KeyID`, `Secret`, `Public`, `ParsePublicKeys`, with both string formats. The prose is draft (Global Constraints).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions ./internal/docsite`
Expected: PASS, the existing row-mode tests included.

- [ ] **Step 6: Commit**

```bash
git add sessions/signed.go sessions/signed_internal_test.go docs/site/reference/sessions.md
git commit -m "sessions: a P-256 signed session cookie and its key formats" -m "A hub and its path-routed tenants share one origin, so per-tenant session rows would overwrite each other's cookie (spec §6.3). The cookie follows assertion's wire form and strict parsing under its own signing context, so a handoff token can never pass as a session."
```

- [ ] **Step 7: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task1" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: the `SigningKey` and `signedClaims` types, the `cookieState` constants, `NewSigningKey` and `ParseSigningKey` (return `&SigningKey{}`, nil), `KeyID`, `Secret` and `Public` (empty strings), `ParsePublicKeys` (nil, nil), `sign` (`""`, nil), `checkClaims` and `validPublicKey` (nil), `verifyCookie` (zero claims, `cookieValid`) and `newSID` (`""`, nil). Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `sessions/signed_internal_test.go`, then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions -run 'Sign|ParsePublicKeys|NewSID|ValidPublicKey'`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 5's passing output in the branch's one "Test evidence" discussion (Task 1 opens it with `amadan discuss`, titled "Test evidence"). Then `git worktree remove --force "$TMPDIR/ev-task1"`.

---

### Task 2: Signed mode in `Sessions`: the tenant verifier

**Files:**
- Modify: `sessions/sessions.go` (`Config`, `New`, `CookieName`, `setCookie`, `SignIn`, `Mint`, `Adopt`, `SignOut`, `resolveContext`, `Middleware`, `Require`, `RequireFresh`, `admitting`, `refuse`, `Sweep`)
- Modify: `sessions/inventory.go` (`Hash`, `List`, `Revoke`, `RevokeOthers`, `RevokeAll`)
- Create: `sessions/signedmode.go`
- Test: `sessions/signedmode_test.go` (package `sessions_test`)

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `Config.Signed *Signed` and `Config.Now func() time.Time` (nil is `time.Now`; it exists so `auth` can move both clocks together, as `lastsignin.Config.Now` does)
  - `type Signed struct { Name string; Key *SigningKey; Keys map[string][]byte; Epoch func(ctx context.Context, subject string) (int64, error); RefreshPath string }`
  - `const DefaultRefreshPath = "/_carlos/refresh"`
  - `var ErrSigned = errors.New("rastrillo/sessions: signed sessions keep no rows")`
  - `var ErrVerifyOnly = errors.New("rastrillo/sessions: this app verifies sessions and cannot mint them")`
  - `var ErrNoUser = errors.New("rastrillo/sessions: no such subject")`: what `Epoch` returns for a subject that no longer exists
  - `func (s *Sessions) RefreshIfExpired(w http.ResponseWriter, r *http.Request) bool`: true when it wrote a redirect. In row mode it does nothing and returns false (Task 5 calls it unconditionally)
  - unexported `func requestPath(r *http.Request) string`: `r.RequestURI` when it starts with `/`, else `r.URL.RequestURI()`. It carries the query, so it is for building `next` and `return_to`, never for comparing with `RefreshPath`

Rules `New` enforces in signed mode: `DB` must be nil ("signed sessions keep no rows; leave DB nil"); `Name` matches `^[a-z0-9][a-z0-9_-]{0,31}$`; every `Keys` entry goes through Task 1's `validPublicKey`, the check `ParsePublicKeys` uses (kid pattern, a 65-byte point that `ecdsa.ParseUncompressedPublicKey` accepts), because a caller may build the map by hand and a second copy of the check would drift; the kids of `Keys` plus `Key` (when set) number 1 or 2, and `Key`'s kid in `Keys` must carry `Key`'s own public key; `Key` set requires `Epoch`; `RefreshPath` defaults to `DefaultRefreshPath` and must start with a single `/`. `New` keeps a deep copy of `Signed` (the struct, the map and each key's bytes), as `assertion.NewVerifier` copies its trust set, so a caller editing its map during a rotation cannot race verification. Without `Signed`, `New` is unchanged.

Every signed cookie's `exp` is `min(iat + 15m, at + TTL)`. A TTL shorter than 15 minutes, or a refresh near the end of the session life, never grants a cookie past the session's absolute end, which is what `TTL` means today.

Behaviour in signed mode:

| Call | Hub (`Key` set) | Tenant (`Keys` only) |
|---|---|---|
| `CookieName()` | `"__Host-"+Name` on https, `Name` on http | same |
| `SignIn` | `Epoch(r.Context(), Subject)`, then a cookie with `iat=now`, `at=now`, `exp` as above, a fresh `sid`; `Max-Age` = `TTL` | `ErrVerifyOnly` |
| `SignOut` | clears this browser's cookie. A copy of it stays refreshable until "sign out everywhere" or the end of the session life | same |
| `From`, `FromContext` | valid cookies only | same |
| `Middleware` | valid: stash. Expired: `RefreshIfExpired`. Otherwise pass through. Never clears | same |
| `Require`, `RequireFresh` | as `Middleware`, then refuse to `Origin + SigninPath + "?return_to=" + requestPath(r)` | same |
| `Mint`, `List`, `Revoke`, `RevokeOthers`, `RevokeAll` | `ErrSigned` | `ErrSigned` |
| `Adopt` | `(Session{}, false)` | same |
| `Hash` | `("", false)`: there is no row to find | same |
| `Sweep` | `nil` | `nil` |

The resolved `Session` is `{Subject: sub, Method: m, AuthTime: auth_time (zero when 0), At: at}`.

`RefreshIfExpired` does nothing and returns false in row mode. In signed mode it acts only on a `cookieExpired` cookie whose `r.URL.Path` is not `RefreshPath`. It compares `r.URL.Path`, not `requestPath(r)`: `requestPath` carries the query (`/_carlos/refresh?next=/`), so it never equals `RefreshPath` and the refresh route would be sent to itself. It redirects to `Origin + RefreshPath + "?next=" + url.QueryEscape(requestPath(r))`: 303 for GET and HEAD, 307 for every other method so a posted form keeps its body. Task 3's `Refresh` therefore answers every method, and the hub mounts it with `r.Handle`, not per method. Signed mode never clears a cookie it can't verify. On one origin that cookie is the whole product's session, and a tenant whose key set lags a rotation would otherwise sign everyone out.

- [ ] **Step 1: Write the failing tests**

```go
package sessions_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

const origin = "https://ppl.test"

// pair is a hub that mints and a tenant that verifies, sharing one
// clock, as a product and its path-routed instances share one box.
type pair struct {
	hub, tenant *sessions.Sessions
	now         time.Time
	epoch       int64
	hubKey      *sessions.SigningKey
}

func newPair(t *testing.T) *pair {
	t.Helper()
	p := &pair{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), epoch: 1}
	k, _ := sessions.NewSigningKey("k1")
	p.hubKey = k
	keys, _ := sessions.ParsePublicKeys(k.Public())
	clock := func() time.Time { return p.now }
	var err error
	p.hub, err = sessions.New(sessions.Config{Origin: origin, Now: clock, Signed: &sessions.Signed{Name: "ppl", Key: k,
		Epoch: func(context.Context, string) (int64, error) { return p.epoch, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	p.tenant, err = sessions.New(sessions.Config{Origin: origin, Now: clock, Signed: &sessions.Signed{Name: "ppl", Keys: keys}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *pair) signIn(t *testing.T, sub string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	if err := p.hub.SignIn(w, httptest.NewRequest("POST", origin+"/auth/verify", nil), sessions.Session{Subject: sub, Method: "magiclink"}); err != nil {
		t.Fatal(err)
	}
	return w.Result().Cookies()[0]
}

// underSlug mounts h as Plan 5 will: the tenant sees its path without
// /acme, while r.RequestURI keeps what the browser asked for.
func underSlug(h http.Handler) http.Handler { return http.StripPrefix("/acme", h) }

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	s, _ := sessions.Current(r)
	w.Write([]byte("hello " + s.Subject))
})

func TestSignedHubMintsAndTenantVerifies(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	if c.Name != "__Host-ppl" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*24*3600 {
		t.Fatalf("cookie attributes: %+v", c)
	}
	r := httptest.NewRequest("GET", origin+"/acme/", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	underSlug(p.tenant.Require(ok)).ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "hello u1" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
}

func TestSignedTenantCannotMint(t *testing.T) {
	p := newPair(t)
	err := p.tenant.SignIn(httptest.NewRecorder(), httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "u1"})
	if !errors.Is(err, sessions.ErrVerifyOnly) {
		t.Fatalf("got %v", err)
	}
}

func TestSignedSubjectMustBeOpaque(t *testing.T) {
	p := newPair(t)
	if err := p.hub.SignIn(httptest.NewRecorder(), httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "ana@example.com"}); err == nil {
		t.Fatal("an address as the subject was signed into a cookie")
	}
}

func TestSignedNewRefusesBadConfig(t *testing.T) {
	k1, _ := sessions.NewSigningKey("k1")
	k2, _ := sessions.NewSigningKey("k2")
	k3, _ := sessions.NewSigningKey("k3")
	other, _ := sessions.NewSigningKey("k1")
	two, _ := sessions.ParsePublicKeys(k2.Public() + "," + k3.Public())
	clash, _ := sessions.ParsePublicKeys(other.Public())
	epoch := func(context.Context, string) (int64, error) { return 0, nil }
	for name, s := range map[string]*sessions.Signed{
		"no name":           {Keys: two},
		"bad name":          {Name: "Ppl!", Keys: two},
		"no keys":           {Name: "ppl"},
		"key without epoch": {Name: "ppl", Key: k1},
		"three kids":        {Name: "ppl", Key: k1, Keys: two, Epoch: epoch},
		"kid clash":         {Name: "ppl", Key: k1, Keys: clash, Epoch: epoch},
		"relative refresh":  {Name: "ppl", Keys: two, RefreshPath: "_carlos/refresh"},
	} {
		if _, err := sessions.New(sessions.Config{Origin: origin, Signed: s}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := sessions.New(sessions.Config{Origin: origin, DB: newDB(t), Signed: &sessions.Signed{Name: "ppl", Keys: two}}); err == nil {
		t.Error("a DB with signed mode was accepted")
	}
	byHand := map[string][]byte{"k9": make([]byte, 65)}
	if _, err := sessions.New(sessions.Config{Origin: origin, Signed: &sessions.Signed{Name: "ppl", Keys: byHand}}); err == nil {
		t.Error("a hand-built map holding no valid point was accepted")
	}
}

func TestSignedConfigIsCopied(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	keys, _ := sessions.ParsePublicKeys(p.hubKey.Public())
	tenant, _ := sessions.New(sessions.Config{Origin: origin, Now: func() time.Time { return p.now },
		Signed: &sessions.Signed{Name: "ppl", Keys: keys}})
	clear(keys["k1"]) // edit the caller's map after New
	delete(keys, "k1")
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	if _, ok := tenant.From(r); !ok {
		t.Fatal("editing the caller's map after New changed what the tenant trusts")
	}
}

func TestSignedCookieNeverOutlivesTheSession(t *testing.T) {
	// a hub with TTL 5*time.Minute (build it by hand, as newPair does): the cookie's
	// exp is sign-in + 5m, not + 15m; the tenant's From accepts it at +4m59s and
	// refuses it at +5m
}

func TestSignedExpiredGETGoesToRefreshWithTheFullPath(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	r := httptest.NewRequest("GET", "/acme/posts?x=1", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	underSlug(p.tenant.Require(ok)).ServeHTTP(w, r)
	want := origin + "/_carlos/refresh?next=" + url.QueryEscape("/acme/posts?x=1")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
		t.Fatalf("%d %q, want 303 %q", w.Code, w.Header().Get("Location"), want)
	}
}

func TestSignedExpiredPOSTKeepsItsBody(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		r := httptest.NewRequest(method, "/acme/posts", strings.NewReader("body=draft"))
		r.AddCookie(c)
		w := httptest.NewRecorder()
		underSlug(p.tenant.Middleware(ok)).ServeHTTP(w, r)
		if w.Code != http.StatusTemporaryRedirect {
			t.Errorf("%s: %d, want 307 so the browser repeats the request with its body", method, w.Code)
		}
	}
}

func TestSignedTenantNeverClearsAnUnknownKeyCookie(t *testing.T) {
	p := newPair(t)
	newer, _ := sessions.NewSigningKey("k2")
	rotated, _ := sessions.New(sessions.Config{Origin: origin, Now: func() time.Time { return p.now },
		Signed: &sessions.Signed{Name: "ppl", Key: newer, Epoch: func(context.Context, string) (int64, error) { return 1, nil }}})
	w := httptest.NewRecorder()
	rotated.SignIn(w, httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "u1"})
	c := w.Result().Cookies()[0]
	for _, h := range []http.Handler{p.tenant.Middleware(ok), p.tenant.Require(ok)} {
		r := httptest.NewRequest("GET", "/acme/", nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		underSlug(h).ServeHTTP(w, r)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("the tenant cleared a cookie it could not verify: that signs the person out of the whole product")
		}
		if strings.Contains(w.Header().Get("Location"), "/_carlos/refresh") {
			t.Fatal("an unverifiable cookie was sent to refresh: that loops")
		}
	}
}

// A hub that mounts Refresh behind its own Middleware must reach it:
// the handler here stands in for Refresh, which Task 3 writes.
func TestSignedRefreshRouteIsNotSentToItself(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	r := httptest.NewRequest("GET", "/_carlos/refresh?next=/", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	p.hub.Middleware(ok).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %q: Middleware redirected the refresh route to itself", w.Code, w.Header().Get("Location"))
	}
}

func TestSignedSigninRedirectIsAbsoluteAndKeepsTheSlug(t *testing.T) {
	p := newPair(t)
	w := httptest.NewRecorder()
	underSlug(p.tenant.Require(ok)).ServeHTTP(w, httptest.NewRequest("GET", "/acme/settings", nil))
	want := origin + "/signin?return_to=" + url.QueryEscape("/acme/settings")
	if w.Header().Get("Location") != want {
		t.Fatalf("got %q, want %q", w.Header().Get("Location"), want)
	}
}

func TestSignedRowMethodsSayNoRows(t *testing.T) {
	p := newPair(t)
	if _, err := p.hub.Mint(sessions.Session{Subject: "u1"}); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("Mint: %v", err)
	}
	if _, err := p.hub.List("u1"); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("List: %v", err)
	}
	if _, err := p.hub.RevokeAll("u1"); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("RevokeAll: %v", err)
	}
	if _, ok := p.hub.Adopt(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "x"); ok {
		t.Error("Adopt adopted a token in signed mode")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(p.signIn(t, "u1"))
	if h, ok := p.hub.Hash(r); ok || h != "" {
		t.Errorf("Hash returned %q, %v for a signed cookie: there is no row to find", h, ok)
	}
	if err := p.hub.Sweep(p.now); err != nil {
		t.Errorf("Sweep: %v", err)
	}
}

// Task 5 calls RefreshIfExpired in row mode too; there it must do nothing.
func TestRowModeRefreshIfExpiredIsANoOp(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	r := httptest.NewRequest("GET", "/posts", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: "x.y"})
	w := httptest.NewRecorder()
	if s.RefreshIfExpired(w, r) || w.Code != http.StatusOK || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("row mode answered %d %q", w.Code, w.Header().Get("Location"))
	}
}
```

`newDB(t)` is the opened, migrated `*sql.DB` half of the existing `newTestSessions`; factor it out rather than copy it. Every test above whose body is only a comment (`TestSignedCookieNeverOutlivesTheSession`) is written in full by the implementer, with the assertion its comment names; a comment-only body fails review. That test stops at the cookie's validity here, since `Refresh` does not exist until Task 3; `TestRefreshNeverOutlivesTheSession` covers the refresh side.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions -run 'Signed|RowMode'`
Expected: FAIL, unknown field `Signed` in `sessions.Config`.

- [ ] **Step 3: Implement signed mode**

Put the mode's own code (`Signed`, the `New` checks, `RefreshIfExpired`, `requestPath`, the signed branches of resolve and sign-in) in `sessions/signedmode.go`. In `sessions.go` and `inventory.go`, each public method gains one `if s.cfg.Signed != nil` branch at its top that calls into it. Row-mode code paths are not reordered or rewritten. Replace the direct `time.Now()` calls with `s.now()` (`cfg.Now`, defaulting to `time.Now`), which leaves row mode's behaviour the same.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions ./auth ./passkey ./password ./totp ./secondfactor ./vault`
Expected: PASS. The other packages prove row mode is untouched.

- [ ] **Step 5: Reference page**

Add to `docs/site/reference/sessions.md` (draft prose): the `Signed` struct, `Config.Signed` and `Config.Now`, the table above, `DefaultRefreshPath`, `ErrSigned`, `ErrVerifyOnly`, `ErrNoUser` and `RefreshIfExpired`. Then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./internal/docsite`. Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add sessions docs/site/reference/sessions.md
git commit -m "sessions: a signed mode that a tenant verifies and only the hub mints" -m "Path routing puts a hub and its tenants on one origin, so one cookie must serve them all (spec §6.3). Keeping it a mode of *Sessions means passkey, password, scope and every sessions.Current reader work unchanged. A tenant never clears a cookie it cannot verify: a key set that lags a rotation would otherwise sign everyone out of the product."
```

- [ ] **Step 7: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task2" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: the `Config.Signed` and `Config.Now` fields, the `Signed` type, `DefaultRefreshPath`, `ErrSigned`, `ErrVerifyOnly`, `ErrNoUser`, `RefreshIfExpired` (false) and `requestPath` (empty); `New` stays as it is, so it ignores `Signed`. Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `sessions/signedmode_test.go` and `sessions/sessions_test.go` (the `newDB` factoring), then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions -run 'Signed|RowMode'`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 4's passing output in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task). Then `git worktree remove --force "$TMPDIR/ev-task2"`.

---

### Task 3: The refresh route

**Files:**
- Create: `sessions/refresh.go`
- Test: `sessions/refresh_test.go` (package `sessions_test`, reusing Task 2's `pair`)

**Interfaces:**
- Consumes: Tasks 1 and 2.
- Produces: `func (s *Sessions) Refresh(w http.ResponseWriter, r *http.Request)`, mounted by the hub for every method at `RefreshPath` (`r.Handle`), because Task 2 sends any unsafe method here by 307.

Behaviour, in order. "Go back" means redirect to `next`: 303 for GET and HEAD, 307 otherwise. Refresh never reads the body, so a 307 hands it back to the browser unread. "Sign out" means clear the cookie and 303 to `SigninPath + "?return_to=" + url.QueryEscape(next)`. The hub is the authority on its own cookie, so clearing here is safe where a tenant's clearing is not.

1. Not signed mode, or a tenant: 404.
2. Any method other than GET and HEAD must pass `csrf.SameOrigin(r, Origin)`, or the answer is 403 with no cookie touched. Without this check, a cross-site form could post here and have the response clear the person's cookie. Lax keeps the cookie off that request, but the browser still accepts the deletion.
3. `next := SafeReturnTo(r.URL.Query().Get("next"), "/")`; a `next` whose path is `RefreshPath` becomes `/`.
4. No cookie: 303 to sign-in, and **no `Set-Cookie`**. There is nothing to clear, and a deletion here is what a cross-site GET could otherwise trigger.
5. `cookieInvalid`: sign out.
6. `cookieValid` (not expired on the hub's clock): another tab or a parallel request refreshed first, so go back without reissuing or reading the store. To catch a real loop (a tenant whose clock is ahead keeps bouncing a cookie the hub calls fresh), count these visits in a cookie `CookieName()+"_rf"` (HttpOnly, `Max-Age` 10, value a small integer). The tenth within 10 seconds answers 500 with the text "This session could not be refreshed. Check that the hub and its apps agree on the time.", logs an error, and clears the counter. A real loop bounces many times a second, and browsers give up after about 20 redirects, so this fires first and leaves a log line. Ten tabs all refreshing in the same 10 seconds is the cost, and a reload clears it. A count per redirect chain would need a marker in `next`, which is the app's URL, and the plan does not touch app URLs.
7. `cookieExpired` with `now ≥ at + TTL`: past the session life. Sign out.
8. `Epoch(ctx, sub)`: `ErrNoUser`, or a value other than the cookie's `epoch`, means sign out. Any other error is 503 with the cookie kept: a store outage must not sign everyone out.
9. Reissue under the hub's current `Key`, with the same `sub`, `sid`, `epoch`, `at`, `m` and `auth_time`, `iat=now` and `exp=min(now+15m, at+TTL)`. Set `Max-Age` to `at + TTL - now`, clear the counter if the request carried one, and go back.

A cookie signed under a retired kid that is still in `Keys` refreshes and comes back under the current key. That is how routine rotation moves people onto the new key without signing them out.

- [ ] **Step 1: Write the failing tests**

```go
func refresh(t *testing.T, p *pair, method, next string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/_carlos/refresh?next="+url.QueryEscape(next), strings.NewReader("body=draft"))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	p.hub.Refresh(w, r)
	return w
}

func TestRefreshReissuesWhenTheEpochMatches(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	w := refresh(t, p, "GET", "/acme/posts", c)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/posts" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Location"))
	}
	fresh := w.Result().Cookies()[0]
	r := httptest.NewRequest("GET", "/acme/", nil)
	r.AddCookie(fresh)
	if s, ok := p.tenant.From(r); !ok || s.Subject != "u1" {
		t.Fatal("the tenant does not accept the reissued cookie")
	}
}

func TestRefreshKeepsAPOSTAPOST(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		w := refresh(t, p, method, "/acme/posts", c)
		if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/acme/posts" {
			t.Errorf("%s: %d %q, want 307 back to the action", method, w.Code, w.Header().Get("Location"))
		}
		p.now = p.now.Add(16 * time.Minute)
		c = w.Result().Cookies()[0]
	}
}

func TestRefreshAfterAnEpochBumpSignsOut(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now, p.epoch = p.now.Add(16*time.Minute), 2
	w := refresh(t, p, "GET", "/acme/", c)
	if w.Header().Get("Location") != "/signin?return_to=%2Facme%2F" || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("%q %+v: want sign-in and a cleared cookie", w.Header().Get("Location"), w.Result().Cookies())
	}
}

func TestRefreshForADeletedUserSignsOut(t *testing.T) {
	// as above, with Epoch returning sessions.ErrNoUser
}

func TestRefreshOnAStoreErrorKeepsTheCookie(t *testing.T) {
	// Epoch returns errors.New("store down"): want 503 and no Set-Cookie at all
}

func TestRefreshPastTheSessionLifeSignsOut(t *testing.T) {
	// sign in at t0; now = t0 + 30*24h + time.Second: want sign-in and a cleared cookie
}

func TestRefreshRefusesAHostileNext(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	for _, next := range []string{"//evil.example", "/\\evil.example", "https://evil.example/", "/\t/evil.example",
		"/_carlos/refresh?next=/x", "", "evil"} {
		p.now = p.now.Add(16 * time.Minute)
		w := refresh(t, p, "GET", next, c)
		if w.Header().Get("Location") != "/" {
			t.Errorf("next %q: went to %q, want /", next, w.Header().Get("Location"))
		}
		c = w.Result().Cookies()[0]
	}
}

// Two tabs both sent to refresh with one expired cookie: the first
// reissues it, the second arrives with the new one and must go through.
func TestRefreshOfAFreshCookieJustGoesBack(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	fresh := refresh(t, p, "GET", "/acme/a", c).Result().Cookies()[0]
	w := refresh(t, p, "GET", "/acme/b", fresh)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/b" {
		t.Fatalf("second tab: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestRefreshBreaksALoop(t *testing.T) {
	// a fresh cookie sent to refresh ten times within 10 seconds, carrying the
	// counter cookie each response sets: the first nine go back, the tenth is
	// 500 with no Location; four overlapping chains (four fresh-cookie arrivals)
	// all go back
}

func TestRefreshRefusesACrossSitePost(t *testing.T) {
	p := newPair(t)
	r := httptest.NewRequest("POST", "/_carlos/refresh?next=/", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	p.hub.Refresh(w, r)
	if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%d %v: a cross-site post must change nothing", w.Code, w.Result().Cookies())
	}
}

func TestRefreshWithNoCookieSetsNoCookie(t *testing.T) {
	p := newPair(t)
	w := refresh(t, p, "GET", "/acme/", nil)
	if !strings.HasPrefix(w.Header().Get("Location"), "/signin") || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%q %v", w.Header().Get("Location"), w.Result().Cookies())
	}
}

func TestRefreshNeverOutlivesTheSession(t *testing.T) {
	// TTL 30 days; refresh at at+TTL-1m: the new cookie is valid at at+TTL-1s and
	// expired at at+TTL, and refresh at at+TTL signs out
}

func TestRefreshMovesARetiredKeyOntoTheCurrentOne(t *testing.T) {
	// sign in on a hub whose Key is k1; build a second hub with Key k2 and Keys{k1};
	// 16 minutes on, refresh through the second hub; the reissued cookie verifies
	// on a tenant holding only k2's public key
}

func TestRefreshOnATenantIsNotFound(t *testing.T) {
	p := newPair(t)
	w := httptest.NewRecorder()
	p.tenant.Refresh(w, httptest.NewRequest("GET", "/_carlos/refresh?next=/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
}
```

Each test with a comment body (`TestRefreshForADeletedUserSignsOut`, `TestRefreshOnAStoreErrorKeepsTheCookie`, `TestRefreshPastTheSessionLifeSignsOut`, `TestRefreshBreaksALoop`, `TestRefreshNeverOutlivesTheSession`, `TestRefreshMovesARetiredKeyOntoTheCurrentOne`) is written in full by the implementer, with the assertion its comment names; a comment-only body fails review. The `pair` in `sessions/signedmode_test.go` gains an `epochErr error` field so `Epoch` can fail on demand.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions -run Refresh`
Expected: FAIL, `p.hub.Refresh undefined`.

- [ ] **Step 3: Implement `Refresh` in `sessions/refresh.go`**

- [ ] **Step 4: Reference page**

Add `Refresh` and its outcomes, in the order above, to `docs/site/reference/sessions.md` (draft prose). The 500 text above is draft copy too; Task 9's copy-review pass rewrites both.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add sessions docs/site/reference/sessions.md
git commit -m "sessions: the hub's refresh route, with epoch and a loop breaker" -m "A 15-minute cookie needs a cheap way to live on, and the epoch check there is what makes sign-out-everywhere reach every tenant within 15 minutes (spec §6.3). A store error answers 503 and keeps the cookie, because an outage must not sign everyone out. A POST is answered 307 so a form written for 20 minutes survives. A fresh cookie goes straight back, because two tabs refresh at once; only ten bounces in 10 seconds are called a loop."
```

- [ ] **Step 7: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task3" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: `(*Sessions).Refresh`, an empty handler that writes nothing. Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `sessions/refresh_test.go` and `sessions/signedmode_test.go` (the `epochErr` field), then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions -run Refresh`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 5's passing output in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task). Then `git worktree remove --force "$TMPDIR/ev-task3"`.

---

### Task 4: Magic links on `objrec`

**Files:**
- Create: `auth/recordlinks.go`
- Test: `auth/recordlinks_test.go` (package `auth`)

**Interfaces:**
- Consumes: `objrec.Store`, `objrec.GetJSON`, `objrec.CreateJSON`, `objrec.ErrExists`, `objrec.ErrNotFound`; `crypto.Derive`, `crypto.SealSym`, `crypto.OpenSym`.
- Produces (unexported, for Task 5):
  - `type recordLinks struct { store objrec.Store; pepper, seal []byte; now func() time.Time; log *slog.Logger }`, implementing `signin.LinkStore`
  - `func (l *recordLinks) sweep(ctx context.Context, now time.Time) error`
  - `func emailHash(pepper []byte, address string) string`: lowercase hex `HMAC-SHA256(pepper, strings.ToLower(address))`

Records:

| Key | Body |
|---|---|
| `links/<yyyy-mm-dd>/<hash>.json` | `{"eh": "<eh>", "addr": "<base64 SealSym of the address>", "purpose": "signin", "exp": <unix>}` |
| `links-used/<yyyy-mm-dd>/<hash>` | `{}` |

`<hash>` is the hash `signin` passes in (lowercase hex SHA-256 of the token). `<yyyy-mm-dd>` is the UTC day on `l.now()` when `PutLink` runs. `seal` is `crypto.Derive([]byte(InstanceKey), "rastrillo/auth/link-address/v1")`.

`TakeLink(ctx, hash, purpose)`:

1. Read `links/<today>/<hash>.json`, then `links/<yesterday>/<hash>.json` (UTC, on `l.now()`). `auth.LinkTTL` is 15 minutes, so a live link is always in one of the two. Neither found: `("", false, nil)` with nothing written. An unknown token must cost no write, or anyone can fill the bucket by posting random tokens.
2. Wrong `purpose`: `("", false, nil)` and the link is not consumed, as the SQL store's `WHERE purpose = ?` behaves.
3. Create `links-used/<day where found>/<hash>`. `ErrExists` means someone redeemed it first: `("", false, nil)`. This create is the single use. Two redeemers who both read the link both reach it, and only one wins.
4. Delete the link record, logging a failure without failing the sign-in.
5. `exp` passed: `("", false, nil)`. The link is spent anyway, as in the SQL store.
6. Open `addr` and return it.

`sweep` lists `links/` and `links-used/` (following the cursor) and deletes every key whose day segment is before yesterday.

`now` is read at call time and never captured as a value: Task 5 passes `func() time.Time { return a.now() }`, so a test that moves `a.now` after `New` moves the link store with it.

- [ ] **Step 1: Write the failing tests**

```go
func newRecordLinks(t *testing.T) (*recordLinks, *objrec.DirStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	store := objrec.Dir(t.TempDir())
	return &recordLinks{store: store, pepper: bytes.Repeat([]byte{7}, 32),
		seal: crypto.Derive([]byte("test-instance-key"), "rastrillo/auth/link-address/v1"),
		now: func() time.Time { return now }, log: slog.Default()}, store, &now
}

func put(t *testing.T, l *recordLinks, hash, addr string) {
	t.Helper()
	if err := l.PutLink(context.Background(), hash, addr, signin.PurposeSignin, l.now().Add(LinkTTL)); err != nil {
		t.Fatal(err)
	}
}

// allKeys(t, s) []string: every key in s, paging List(ctx, "", cursor, 0)
// until next is "". Task 5's tests use it too.

func TestRecordLinksAreSingleUse(t *testing.T) {
	l, _, _ := newRecordLinks(t)
	h := signin.HashToken("tok")
	put(t, l, h, "ana@example.com")
	if a, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); !ok || err != nil || a != "ana@example.com" {
		t.Fatalf("first take: %q %v %v", a, ok, err)
	}
	if _, ok, _ := l.TakeLink(context.Background(), h, signin.PurposeSignin); ok {
		t.Fatal("a link was redeemed twice")
	}
}

func TestRecordLinksRaceHasOneWinner(t *testing.T) {
	// 16 goroutines TakeLink the same hash: exactly one ok, no errors
}

func TestRecordLinksExpiredIsSpent(t *testing.T) {
	// put; *now += LinkTTL + time.Second; take is !ok; *now back; take is still !ok
}

func TestRecordLinksSurviveMidnight(t *testing.T) {
	l, _, now := newRecordLinks(t)
	*now = time.Date(2026, 10, 17, 23, 58, 0, 0, time.UTC)
	h := signin.HashToken("late")
	put(t, l, h, "ana@example.com")
	*now = time.Date(2026, 10, 18, 0, 5, 0, 0, time.UTC)
	if _, ok, _ := l.TakeLink(context.Background(), h, signin.PurposeSignin); !ok {
		t.Fatal("a link sent at 23:58 UTC was lost at 00:05")
	}
}

func TestRecordLinksWrongPurposeIsNotConsumed(t *testing.T) {
	// put; take with purpose "other" is !ok; take with PurposeSignin is ok
}

func TestRecordLinksUnknownTokenWritesNothing(t *testing.T) {
	l, store, _ := newRecordLinks(t)
	if _, ok, _ := l.TakeLink(context.Background(), signin.HashToken("never-sent"), signin.PurposeSignin); ok {
		t.Fatal("unknown token accepted")
	}
	if keys := allKeys(t, store); len(keys) != 0 {
		t.Fatalf("an unknown token wrote %v", keys)
	}
}

func TestRecordLinksHoldNoReadableAddress(t *testing.T) {
	l, store, _ := newRecordLinks(t)
	put(t, l, signin.HashToken("tok"), "Ana@Example.com")
	for _, k := range allKeys(t, store) {
		b, _, _ := store.Get(context.Background(), k)
		if bytes.Contains(bytes.ToLower(b), []byte("example.com")) {
			t.Fatalf("%s holds the address: %s", k, b)
		}
	}
}

func TestRecordLinksSweepKeepsYesterday(t *testing.T) {
	// put and take one link on each of 2026-10-15, -16 and -17; sweep at 2026-10-17 12:00;
	// allKeys holds only the -16 and -17 links/ and links-used/ keys
}
```

Each test with a comment body (`TestRecordLinksRaceHasOneWinner`, `TestRecordLinksExpiredIsSpent`, `TestRecordLinksWrongPurposeIsNotConsumed`, `TestRecordLinksSweepKeepsYesterday`) is written in full by the implementer, with the assertion its comment names; a comment-only body fails review. `allKeys` is a real helper in the test file with the signature its comment gives.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth -run RecordLinks`
Expected: FAIL, undefined `recordLinks`.

- [ ] **Step 3: Implement `auth/recordlinks.go`**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth -run RecordLinks`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add auth/recordlinks.go auth/recordlinks_test.go
git commit -m "auth: magic links as objrec records, single use by a conditional create" -m "A central app keeps no database (spec §4), so links move to its store (spec §5). The used marker's create-once is what makes a link single-use. Its key is dated, against the spec's undated form, so a daily sweep can delete whole days without reading each object. The address is sealed so a bucket listing shows none."
```

- [ ] **Step 6: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task4" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: the `recordLinks` struct as written, `PutLink` (`errors.New("stub")`), `TakeLink` (`""`, true, nil), `sweep` (nil) and `emailHash` (`""`). Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `auth/recordlinks_test.go`, then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth -run RecordLinks`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 4's passing output in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task). Then `git worktree remove --force "$TMPDIR/ev-task4"`.

---

### Task 5: Users on `objrec`, blind mode, and records mode in `auth`

**Files:**
- Create: `auth/users.go`
- Modify: `auth/auth.go` (`Config`, `Auth`, `New`)
- Modify: `auth/handlers.go` (`admit`, `Callback`, `RequireSession`, `RequireFreshSession`; new `SignoutEverywhere`, `Refresh`)
- Modify: `auth/continuation.go` (`continuationFor`, and the `pendingValue` helper)
- Modify: `auth/store.go` (`Sweep`, `SpendLinks`)
- Modify: `docs/site/reference/auth.md`
- Test: `auth/records_test.go` (package `auth`)

**Interfaces:**
- Consumes: Tasks 2 to 4.
- Produces:
  - `Config.Records objrec.Store`, `Config.Signed *sessions.Signed`, `Config.Pepper []byte`, `Config.Blind bool`
  - `type User struct { ID string \`json:"uid"\`; Email string \`json:"email,omitempty"\`; EH string \`json:"eh"\`; Created time.Time \`json:"created"\`; Epoch int64 \`json:"epoch"\` }`
  - `func (a *Auth) User(ctx context.Context, uid string) (User, error)`: wraps `objrec.ErrNotFound`
  - `func (a *Auth) EndSessions(ctx context.Context, uid string) error`: `epoch++` through `objrec.Mutate`, whose callback returns `sessions.ErrNoUser` when `exists` is false, so a deleted user is never re-created as a record with no id or hash. It also refuses a malformed `uid` with `sessions.ErrNoUser`.
  - `func (a *Auth) SignoutEverywhere(w http.ResponseWriter, r *http.Request)`: POST, same-origin checked. It runs `a.sessions.RefreshIfExpired` first, so a page idle past 15 minutes is refreshed (a 307 to refresh and back, which repeats the POST) instead of looking signed out everywhere. With no valid session it answers 303 to `SigninPath` and does not call `EndSessions`. Otherwise it calls `EndSessions` for the signed-in subject. On success or `ErrNoUser` (nothing left to end): clear the cookie, 303 to `SigninPath`. On any other error: 503, cookie kept, error logged. It must never report success for a revocation that did not happen.
  - `func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request)`: delegates to `sessions.Refresh`
  - `var ErrRecords = errors.New("rastrillo/auth: SpendLinks is not available with Records: links are stored by token, not by address")`
  - unexported `type users struct{…}` with `ensure(ctx, address string) (User, error)` and `epoch(ctx, uid string) (int64, error)`, and `newUID() (string, error)`
  - unexported `func (a *Auth) pendingValue(r *http.Request) (string, bool)`: the pending cookie's value as `signin` reads it; the cookie value as it is in row mode, the opened blob in records mode, and `false` when the cookie is absent or does not open

Records:

| Key | Body |
|---|---|
| `users/<uid>.json` | `User`. `Email` is empty in blind mode |
| `emails/<eh>.json` | `{"uid": "<uid>"}` |

`New` with `Records` set: `Signed` is required and must have a `Key`, because the centre mints; `Signed.Epoch` must be nil, since `auth` supplies it from `users/`; `DB` must be nil; `len(Pepper) >= 32`; `SubjectFor` must be nil, since the subject is the user id. In blind mode `Mailer` must be set and `mail.IsConfigured(Mailer)` must be true. Both the nil fallback and an explicit `mail.Logged` (which `mail.FromEnv` returns when no relay is set) write the recipient and the live link to the log. For local development, pass a `Sender` of your own. `New` copies `*cfg.Signed` before setting `Epoch` on its copy to `users.epoch`, so the caller's struct is never written. It then calls `sessions.New` with that copy and `Now: func() time.Time { return a.now() }`. `signin.Flow.Now` and the link store's `now` are the same closure, `func() time.Time { return a.now() }`, and not the value of `a.now`: the tests below reassign `a.now` after `New`, and a captured value would judge links expired while the cookie clock moved. `RequireFreshSession` judges freshness on `a.now` in records mode. `Pepper`, `Blind` or `Signed` without `Records` is refused, so half a mode can't start. Without `Records`, `New` is unchanged.

`ensure(ctx, address)`. `emails/` is written before `users/` (deviation 1). Both branches use one `createOrLoadUser(ctx, uid, eh, address)`: create `users/<uid>.json`, and on `ErrExists` read it. A writer that won the email claim may otherwise lose the user create to another device that repaired it first, and fail a sign-in that should succeed.

1. `eh := emailHash(pepper, address)`. Read `emails/<eh>.json`.
2. Found: `createOrLoadUser` if `users/<uid>.json` is missing (the repair), else return it.
3. Not found: `uid := newUID()`, then `CreateJSON(emails/<eh>.json, {uid})`. On `ErrExists`, another device won: go back to 1 once. Then `createOrLoadUser`.
4. A new record is `User{ID: uid, Email: address unless Blind, EH: eh, Created: now, Epoch: <random in [0, 2^62)>}` from `crypto/rand` (deviation 5), so a re-created record never matches an old cookie, whatever the clock does.

**The cookies on a shared origin.** Every cookie the centre sets reaches every tenant, so in records mode no cookie may carry a readable address. The attempt, continuation and remembered cookies are already `internal/sealedcookie` envelopes. `signin`'s pending blob is not: it is base64 JSON with an HMAC, and it holds the address (`signin@v0.1.1/pending.go:36`). In records mode, auth seals it with `sealedcookie.Seal` under `crypto.Derive([]byte(InstanceKey), "rastrillo/auth/pending-cookie/v1")` before setting the pending cookie. One helper, `pendingValue`, reads it back, and both places that compare the pending cookie with the continuation's `PH` use it: `Callback` (`handlers.go`, `p.PH == digest(c.Value)`, and the value it passes to `flow.CompleteKeymail`) and `continuationFor` (`continuation.go`, `digest(pending.Value) != p.PH`). `openContinuation` compares nothing and needs no change. Both sites therefore hash the opened blob, the same bytes `sealContinuation` hashed when the continuation was made; if either hashed the sealed cookie, a keymail sign-in would fail in records mode only. A pending cookie that does not open is treated as missing.

**Logs in blind mode.** An SMTP error can carry the recipient (`mail/mail.go:163`), and `Begin` logs errors whole. In blind mode, every `Logger` call in `auth` that would carry an error from the flow, the mailer or the keymail exchange logs a fixed message and the error's type (`fmt.Sprintf("%T", err)`), never its text.

`newUID` is 16 random bytes in lowercase unpadded base32, 26 chars matching `^[a-z2-7]{26}$`. `users.epoch` refuses a `uid` that doesn't match that pattern with `sessions.ErrNoUser` before building a key.

`admit` in records mode: after `Authorize` admits, so a refused address writes no user, call `ensure` and sign in with `sessions.Session{Subject: user.ID, Method, AuthTime}`. The rest of `admit` (the jar, `SecondFactor`, the redirect) is unchanged. `Identity.Address` is therefore the user id, as it already is under `SubjectFor`; the address is `a.User(ctx, uid).Email`, which is empty in blind mode.

`RequireSession` and `RequireFreshSession` call `a.sessions.RefreshIfExpired(w, r)` before refusing, so an expired cookie on the centre refreshes like one on a tenant. That call is a no-op in row mode. `Sweep` in records mode sweeps the link store and the (no-op) sessions. `SpendLinks` in records mode returns `0, ErrRecords`: its callers are password and passkey sign-ins, which a stateless centre doesn't have.

- [ ] **Step 1: Write the failing tests**

```go
// Helpers to write first:
//
// newRecordsAuth(t, mut func(*Config)) (*Auth, *captureMailer, *objrec.DirStore)
// is newTestAuth for a centre with no database: a Dir store, a fresh
// signing key, a 32-byte pepper, ProofOff, origin https://ppl.test.
//
// signInAt(t, a, m, address) *http.Cookie runs beginSignin, follows the
// emailed link through GET and POST Verify, and returns the cookie set.

func TestRecordsNewRefusesHalfAMode(t *testing.T) {
	k, _ := sessions.NewSigningKey("k1")
	pepper := bytes.Repeat([]byte{1}, 32)
	store := objrec.Dir(t.TempDir())
	base := func() Config {
		return Config{Origin: "https://ppl.test", InstanceKey: "ik", ProofOff: true, Records: store, Pepper: pepper,
			Signed: &sessions.Signed{Name: "ppl", Key: k}}
	}
	for name, mut := range map[string]func(*Config){
		"no Signed":          func(c *Config) { c.Signed = nil },
		"verify-only Signed": func(c *Config) { keys, _ := sessions.ParsePublicKeys(k.Public()); c.Signed = &sessions.Signed{Name: "ppl", Keys: keys} },
		"Epoch set":          func(c *Config) { c.Signed.Epoch = func(context.Context, string) (int64, error) { return 0, nil } },
		"short pepper":       func(c *Config) { c.Pepper = pepper[:31] },
		"DB too":             func(c *Config) { c.DB = newTestAuthDB(t) },
		"SubjectFor":         func(c *Config) { c.SubjectFor = func(a string) (string, error) { return a, nil } },
		// "Blind, no Records" carries a working Mailer, so only the Records rule can refuse it.
		"Blind, no Records":  func(c *Config) { c.Records, c.Signed, c.Pepper, c.DB, c.Blind, c.Mailer = nil, nil, nil, newTestAuthDB(t), true, &captureMailer{} },
		"Pepper, no Records": func(c *Config) { c.Records, c.Signed, c.DB = nil, nil, newTestAuthDB(t) },
		"Signed, no Records": func(c *Config) { c.Records, c.Pepper, c.DB = nil, nil, newTestAuthDB(t) },
		"Blind, no Mailer":   func(c *Config) { c.Blind, c.Mailer = true, nil },
		"Blind, log mailer":  func(c *Config) { c.Blind, c.Mailer = true, mail.Logged(slog.Default()) },
	} {
		c := base()
		mut(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRecordsFirstSigninCreatesTheUserAndItsIndex(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	c := signInAt(t, a, m, "Ana@Example.com")
	r := httptest.NewRequest("GET", "https://ppl.test/", nil)
	r.AddCookie(c)
	id, ok := a.SessionFrom(r)
	if !ok || !regexp.MustCompile(`^[a-z2-7]{26}$`).MatchString(id.Address) {
		t.Fatalf("subject %q: want the 26-char user id", id.Address)
	}
	u, err := a.User(context.Background(), id.Address)
	if err != nil || !strings.EqualFold(u.Email, "ana@example.com") || u.EH != emailHash(a.cfg.Pepper, "ana@example.com") {
		t.Fatalf("%+v %v", u, err)
	}
	if keys := allKeys(t, store); !slices.Contains(keys, "emails/"+u.EH+".json") || !slices.Contains(keys, "users/"+u.ID+".json") {
		t.Fatalf("keys: %v", keys)
	}
}

func TestRecordsSecondSigninIsTheSameUser(t *testing.T) {
	// sign in twice as ana@example.com, the second time as ANA@example.com: same subject
}

func TestRecordsTwoDevicesAtOnceShareOneUser(t *testing.T) {
	a, _, store := newRecordsAuth(t, nil)
	var wg sync.WaitGroup
	ids := make([]string, 16)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := a.users.ensure(context.Background(), "new@example.com")
			if err != nil {
				t.Error(err)
			}
			ids[i] = u.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("two users for one address: %v", ids)
		}
	}
	n := 0
	for _, k := range allKeys(t, store) {
		if strings.HasPrefix(k, "users/") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d users/ records, want 1", n)
	}
}

func TestRecordsRepairsAMissingUserRecord(t *testing.T) {
	// CreateJSON emails/<eh>.json {uid: "aaaaaaaaaaaaaaaaaaaaaaaaaa"} by hand; sign in;
	// the subject is that uid and users/aaaaaaaaaaaaaaaaaaaaaaaaaa.json now exists
}

func TestRecordsRecreatedUserDoesNotReviveOldSessions(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen } // one unchanged clock: a time-based epoch would repeat
	old := signInAt(t, a, m, "ana@example.com")
	uid := subjectOf(t, a, old)
	store.Delete(context.Background(), "users/"+uid+".json")
	signInAt(t, a, m, "ana@example.com") // the repair writes a new record, and a new epoch
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://ppl.test/_carlos/refresh?next=/", nil)
	r.AddCookie(old)
	a.Refresh(w, r)
	if !strings.HasPrefix(w.Header().Get("Location"), "/signin") {
		t.Fatalf("a cookie from before the deletion refreshed: %q", w.Header().Get("Location"))
	}
}

func TestRecordsBlindStoresNoAddress(t *testing.T) {
	a, m, store := newRecordsAuth(t, func(c *Config) { c.Blind = true })
	beginSignin(t, a, "Ana@Example.com")
	assertNoAddress(t, store, "example.com") // while the link waits
	signInAt(t, a, m, "Ana@Example.com")
	assertNoAddress(t, store, "example.com") // after sign-in
	// and users/<uid>.json has eh set and no "email" member
}

// Every cookie reaches every tenant on the shared origin, and the logs
// are the operator's: neither may hold the address in blind mode.
func TestRecordsBlindLeavesNoAddressInCookiesOrLogs(t *testing.T) {
	// a Logger writing to a buffer; a Mailer that fails once with
	// fmt.Errorf("send to %s: refused", to), then succeeds.
	// Run: a magic-link Begin that fails to send, a magic-link sign-in, and a
	// keymail sign-in against keymailfake_test.go's fake (KeymailServers set to it).
	// Every Set-Cookie value on every response, and the whole log buffer, are
	// free of "example.com" in any case. So is every cookie value after decoding
	// it as base64url, base64 and URL-escaped text, every way that applies: the
	// unsealed pending blob is base64 JSON and would pass a literal check.
	// The pending cookie's value starts with sealedcookie's "v1." prefix.
}

func TestRecordsWinnerOfTheClaimStillSignsIn(t *testing.T) {
	// a store wrapper that, right after the emails/ create succeeds, runs a second
	// ensure for the same address to completion (it repairs the user), then lets
	// the first continue: both ensures return the same User and no error
}

func TestEndSessionsOnADeletedUserWritesNothing(t *testing.T) {
	// sign in; delete users/<uid>.json; EndSessions returns sessions.ErrNoUser and
	// no users/ key exists afterwards; SignoutEverywhere still clears the cookie
}

// Pins what the docs promise: ordinary sign-out ends this browser only.
func TestSignoutLeavesACopyRefreshableUntilEverywhere(t *testing.T) {
	// sign in; copy the cookie; POST /signout (the cookie is cleared);
	// 16 minutes on, Refresh with the copy reissues it;
	// then SignoutEverywhere, 16 minutes on, Refresh with the copy goes to /signin
}

func TestSignoutEverywhereOnAStoreErrorSaysSo(t *testing.T) {
	// a store wrapper failing every Update: SignoutEverywhere answers 503 and sets no cookie
}

func TestRecordsRefusedAddressWritesNoUser(t *testing.T) {
	// Authorize returns false; after the link is redeemed, no users/ or emails/ key exists
}

func TestSignoutEverywhereEndsEveryCopyOfTheSession(t *testing.T) {
	// sign in; copy the cookie; POST /signout-everywhere with the cookie (Sec-Fetch-Site: same-origin);
	// the response clears the cookie and User(uid).Epoch went up by one;
	// 16 minutes on, Refresh with the copy goes to /signin
}

// A POST with no valid session must change nothing: a 303 to sign-in that
// has quietly ended no one's sessions is a false success.
func TestSignoutEverywhereWithNoSessionEndsNothing(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	uid := subjectOf(t, a, signInAt(t, a, m, "ana@example.com"))
	before, _ := a.User(context.Background(), uid)
	r := httptest.NewRequest("POST", "https://ppl.test/signout-everywhere", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	a.SignoutEverywhere(w, r) // no cookie
	after, _ := a.User(context.Background(), uid)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != a.cfg.SigninPath || after.Epoch != before.Epoch {
		t.Fatalf("%d %q, epoch %d to %d: want 303 to sign-in and no epoch change", w.Code, w.Header().Get("Location"), before.Epoch, after.Epoch)
	}
}

// A page idle past 15 minutes holds an expired cookie. It must be refreshed
// first (the POST is repeated after the 307), not answered as signed out.
func TestSignoutEverywhereOnAnExpiredCookieRefreshesFirst(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	c := signInAt(t, a, m, "ana@example.com")
	uid := subjectOf(t, a, c)
	before, _ := a.User(context.Background(), uid)
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	r := httptest.NewRequest("POST", "https://ppl.test/signout-everywhere", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	a.SignoutEverywhere(w, r)
	after, _ := a.User(context.Background(), uid)
	want := "https://ppl.test/_carlos/refresh?next=" + url.QueryEscape("/signout-everywhere")
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != want || after.Epoch != before.Epoch {
		t.Fatalf("%d %q, epoch %d to %d: want 307 to %q with the epoch unchanged", w.Code, w.Header().Get("Location"), before.Epoch, after.Epoch, want)
	}
}

func TestRecordsSpendLinksSaysUnavailable(t *testing.T) {
	a, _, _ := newRecordsAuth(t, nil)
	if _, err := a.SpendLinks(context.Background(), "ana@example.com"); !errors.Is(err, ErrRecords) {
		t.Fatalf("got %v", err)
	}
}
```

Each test with a comment body (`TestRecordsSecondSigninIsTheSameUser`, `TestRecordsRepairsAMissingUserRecord`, `TestRecordsBlindLeavesNoAddressInCookiesOrLogs`, `TestRecordsWinnerOfTheClaimStillSignsIn`, `TestEndSessionsOnADeletedUserWritesNothing`, `TestSignoutLeavesACopyRefreshableUntilEverywhere`, `TestSignoutEverywhereOnAStoreErrorSaysSo`, `TestRecordsRefusedAddressWritesNoUser`, `TestSignoutEverywhereEndsEveryCopyOfTheSession`), and the trailing comment line of `TestRecordsBlindStoresNoAddress`, is written in full by the implementer, with the assertion its comment names; a comment-only body fails review, since it passes without testing anything. `assertNoAddress(t, store, needle)` reads every object and fails if any lowercased body contains `needle`. `subjectOf(t, a, c)` is `a.SessionFrom` on a request carrying `c`. The `a.now` override relies on Task 5 passing `Now: func() time.Time { return a.now() }` to `sessions.New`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth -run 'Records|Signout|EndSessions'`
Expected: FAIL, unknown field `Records` in `Config`.

- [ ] **Step 3: Implement `auth/users.go` and the records-mode wiring**

- [ ] **Step 4: Reference page**

`docs/site/reference/auth.md` (draft prose): the four `Config` fields and what `New` refuses, the records tables from Tasks 4 and 5, `User`, `EndSessions`, `SignoutEverywhere`, `Refresh`, `ErrRecords`, and the leak profile:

- Default: the store holds each person's address, their user id and their epoch.
- Blind: the store holds only the HMAC. The centre can email someone only in reply to an address they just typed: no notices, and no "your sign-in address changed" mail. Logs carry no address, and a `Mailer` is required.
- Both: a waiting link holds the address sealed under the instance key until it is redeemed or swept. No cookie carries a readable address.
- Sign-out ends this browser's session. A copy of its cookie can still be refreshed until "sign out everywhere" or the end of the 30-day session life. "Sign out everywhere" ends every copy within 15 minutes.
- Never rotate the pepper. Every `emails/` key is computed from it.
- The page a person returns to after sign-in is kept in this browser, as with SQLite `auth`. A link opened on another device lands on `SignedInPath`, and a newer sign-in attempt in the same browser replaces the destination. Both modes share `keepReturnTo` unchanged, so no separate parity test is needed.

- [ ] **Step 5: Run the whole package**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth ./internal/docsite`
Expected: PASS, every existing row-mode test included.

- [ ] **Step 6: Commit**

```bash
git add auth docs/site/reference/auth.md
git commit -m "auth: a records mode with users on objrec and an optional blind index" -m "A home or hub signs people in with no database (spec §4, §6.1). The email claim is written before the user, against §4's authoritative-first order, because the claim is what must be unique and a dangling one repairs itself at the next sign-in. Blind mode (Paul, 2026-10-17) keeps only the HMAC, so the centre can mail someone only in reply to an address they just typed; on a shared origin that also means sealing signin's pending cookie and keeping error text out of the logs. The epoch starts at a random value so a re-created user cannot revive old cookies."
```

- [ ] **Step 7: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task5" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: the four `Config` fields, the `User` type, `Auth.users` (`ensure` returns `User{}` and `errors.New("stub")`, `epoch` returns 0 and nil), `newUID` (`""`, nil), `pendingValue` (returns the cookie value unchanged), `(*Auth).User` (`User{}` and `errors.New("stub")`), `EndSessions` (nil), `SignoutEverywhere` and `Refresh` (empty handlers) and `ErrRecords`; `New` stays as it is, so it ignores `Records`. Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `auth/records_test.go`, then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth -run 'Records|Signout|EndSessions'`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 5's passing output in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task). Then `git worktree remove --force "$TMPDIR/ev-task5"`.

---

### Task 6: A `pow` nonce store on `objrec`

**Files:**
- Create: `pow/recordnonces.go`
- Test: `pow/recordnonces_test.go` (package `pow`)

**Interfaces:**
- Consumes: `objrec.Store`, `objrec.ErrExists`.
- Produces: `func RecordNonces(store objrec.Store) NonceStore`; unexported `type recordNonces struct { store objrec.Store; now func() time.Time }`.

The key is `pow-nonces/<lowercase hex sha256(nonce)>/<expires as unix milliseconds>`. `Spend` refuses a passed `expires` on `now()`, then creates the key; `ErrExists` means spent. After a successful create it reads `now()` again and returns false if `expires` has passed by then. The interface requires the expiry check and the spend to be one step (`pow/store.go:42`), and a remote create can't be. Without the second check, a request that stalls past expiry while another spends the nonce and a sweep deletes the marker would create it afresh and be admitted. A marker left by a refused late create is swept like any other. `Spent` is a `List` of `pow-nonces/<hash>/` with limit 1, so it needs no date. `Ready` is nil. `Sweep(now)` lists `pow-nonces/`, parses the last segment and deletes every key older than `now - sweepMargin`, with no `Get`. `ex` is ignored, as `MemoryNonces` ignores it. A stateless centre has no transaction to ride, and the reference page says so.

- [ ] **Step 1: Write the failing tests**

| Test | Assertion |
|---|---|
| `TestRecordNoncesSpendOnce` | first `Spend` true, second false |
| `TestRecordNoncesRaceHasOneWinner` | 16 goroutines `Spend` one nonce: exactly one true |
| `TestRecordNoncesRefuseAnExpiredSpend` | `now` after `expires`: false, and the store is empty |
| `TestRecordNoncesSpentSeesASpend` | `Spent` false before the spend, true after |
| `TestRecordNoncesRefuseASpendThatStalledPastExpiry` | a store wrapper whose `Create` moves `now` past `expires` before it returns: `Spend` is false |
| `TestRecordNoncesSweepKeepsTheMargin` | expired 11 minutes ago: gone; expired 9 minutes ago: kept (`sweepMargin` is 10 minutes) |
| `TestGuardStartsOnRecordNonces` | `New(Config{…, Nonces: RecordNonces(objrec.Dir(t.TempDir()))})` succeeds |

Write each on `objrec.Dir(t.TempDir())` with a settable `now`. The nonce in every test is mixed-case base64url, to show the hash makes it a valid `objrec` key.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./pow -run RecordNonces`
Expected: FAIL, undefined `RecordNonces`.

- [ ] **Step 3: Implement `pow/recordnonces.go`**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./pow`
Expected: PASS.

- [ ] **Step 5: Reference page**

Add `RecordNonces` to `docs/site/reference/pow.md` (draft prose), saying that it ignores the caller's transaction. Run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./internal/docsite`. Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pow/recordnonces.go pow/recordnonces_test.go docs/site/reference/pow.md
git commit -m "pow: a nonce store on objrec, for a front door with no table" -m "auth refuses to start without a proof-of-work front door, and SQLNonces is a table a stateless centre does not have. MemoryNonces would make every deploy a replay window. The expiry rides in the key so Spent and Sweep never read an object, and Spend checks it again after the create, because a remote create cannot share one statement with the clock check."
```

- [ ] **Step 7: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task6" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: `RecordNonces` (returns a `recordNonces` whose `Spend` returns true, `Spent` false, `Ready` nil and `Sweep` nil) and the `recordNonces` struct. Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `pow/recordnonces_test.go`, then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./pow -run 'RecordNonces|GuardStartsOnRecordNonces'`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 4's passing output in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task). Then `git worktree remove --force "$TMPDIR/ev-task6"`.

---

### Task 7: A hub and a tenant on one origin, end to end

**Files:**
- Test: `auth/stateless_e2e_test.go` (package `auth`)

**Interfaces:**
- Consumes: everything above; Task 5's `newRecordsAuth` and `captureMailer`.

One `http.ServeMux` stands in for the product origin `https://ppl.test`: the hub's routes at the root (`POST /signin` → `Begin`, `GET`/`POST /auth/verify` → `Verify`, `POST /signout-everywhere` → `SignoutEverywhere`, `/_carlos/refresh` → `Refresh`), and a tenant under `/acme/` mounted with `http.StripPrefix("/acme", …)`. The tenant's own `sessions.Sessions` is verify-only, built from the hub key's `Public()`. It serves `GET /` → `"hello <sub>"` and `POST /posts` → echo of `body`, both behind `tenant.Require`. The hub and the tenant share one clock. `newRecordsAuth` does not return the hub's signing key, so the test takes it through `mut` (`func(c *Config) { hubKey = c.Signed.Key }`) and builds the tenant's `Signed.Keys` from `hubKey.Public()`.

The whole mux is wrapped in `csrf.Protect("https://ppl.test")`, as a real app mounts it. A small `browser` helper keeps a cookie jar. It applies each `Set-Cookie` (a negative `MaxAge` deletes), sends `Sec-Fetch-Site: same-origin`, serves both relative and `https://ppl.test/…` locations from the mux and fails on any other host, follows a 303 as a GET and a 307 with the same method and body, and stops after 10 hops with a failure. It builds every request in origin-form (`httptest.NewRequest(method, "/path?query", body)` with `r.Host` set to `ppl.test`, and an absolute `https://ppl.test/…` Location reduced to its path and query first), never from an absolute URL. An absolute target leaves `r.RequestURI` absolute, so after `http.StripPrefix` `requestPath` falls back to `URL.RequestURI()` and `next` loses the `/acme` prefix.

- [ ] **Step 1: Write the test**

```go
func TestStatelessHubAndTenant(t *testing.T) {
	// 1. Sign in as ana@example.com through Begin and the emailed link.
	//    GET /acme/ answers 200 "hello <uid>".
	// 2. Clock +16m. GET /acme/ answers 200 "hello <uid>" after exactly two hops:
	//    303 to https://ppl.test/_carlos/refresh?next=%2Facme%2F, then 303 to /acme/.
	// 3. Clock +16m. POST /acme/posts with body=draft answers 200 "draft"
	//    after a 307 to refresh and a 307 back.
	// 4. Copy the jar's cookie. POST /signout-everywhere.
	//    Clock +16m. With the copy alone, GET /acme/ ends at /signin?return_to=%2Facme%2F,
	//    and the jar no longer holds a session cookie.
	// 5. The store holds exactly one users/ and one emails/ key.
	// 6. Sign in again. Mount the hub's Refresh and SignoutEverywhere on a second
	//    mux with no csrf.Protect: csrf.Protect answers a cross-site POST 403 on its
	//    own, so through the main mux this step would pass with the handlers' own
	//    SameOrigin checks deleted. On that bare mux, a POST to /_carlos/refresh and
	//    one to /signout-everywhere, each sent with Sec-Fetch-Site: cross-site and
	//    without the cookie (as Lax withholds it), answer 403 from the handler and
	//    leave the jar unchanged; GET /acme/ is still 200.
}
```

Write it in full with the assertions each numbered line names; a comment-only body fails review.

- [ ] **Step 2: Run it**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./auth -run TestStatelessHubAndTenant -v`
Expected: PASS. The test covers behaviour Tasks 2 to 5 already built, so it passes on first run. CONTRIBUTING lets a new test for behaviour that already works say so instead of failing first. Say so in the branch's test-evidence discussion.

- [ ] **Step 3: Commit**

```bash
git add auth/stateless_e2e_test.go
git commit -m "auth: drive a hub and a path-routed tenant on one origin end to end" -m "Each piece has unit tests; this proves they compose the way spec §6.3 describes: one cookie across the product, a refresh that keeps a posted form, and sign-out-everywhere reaching a copied cookie within 15 minutes."
```

- [ ] **Step 4: Evidence**

This task adds no declarations, so there is nothing to stub: its base commit already has the behaviour, and Step 2's first-run pass is the evidence CONTRIBUTING allows for it. Post Step 2's command and passing output, and say it passed first time because Tasks 2 to 5 built the behaviour, in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task).

---

### Task 8: `rastrillo session-key`

**Files:**
- Create: `cmd/rastrillo/sessionkey.go`
- Test: `cmd/rastrillo/sessionkey_test.go`
- Modify: `cmd/rastrillo/main.go` (dispatch and `usage`)
- Modify: `docs/site/cli.md` (a minimal `## rastrillo session-key` section)

**Interfaces:**
- Consumes: `sessions.NewSigningKey`, `sessions.ParseSigningKey`, `sessions.ParsePublicKeys`.
- Produces: `func runSessionKey(args []string, stdout io.Writer) error`; `main` passes `os.Stdout`.

`rastrillo session-key <kid>` prints exactly two lines, `secret <Secret()>` and `public <Public()>`, so a script can take either with `awk`. A missing or invalid kid returns `exitError{code: 2, msg: <usage line>}` (the type in `cmd/rastrillo/doctor.go:161`), so `main` prints it on stderr and exits 2, the usage status `cli.md` tables. An ordinary error would exit 1.

- [ ] **Step 1: Write the failing test**

```go
func TestSessionKeyPrintsAPairThatParses(t *testing.T) {
	var out bytes.Buffer
	if err := runSessionKey([]string{"k2"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "secret k2.") || !strings.HasPrefix(lines[1], "public k2.") {
		t.Fatalf("output: %q", out.String())
	}
	k, err := sessions.ParseSigningKey(strings.TrimPrefix(lines[0], "secret "))
	if err != nil || k.Public() != strings.TrimPrefix(lines[1], "public ") {
		t.Fatalf("the two lines are not one key pair: %v", err)
	}
}

func TestSessionKeyRefusesABadKid(t *testing.T) {
	for _, args := range [][]string{nil, {"K 2"}, {"a", "b"}} {
		var out bytes.Buffer
		err := runSessionKey(args, &out)
		var ex exitError
		if !errors.As(err, &ex) || ex.code != 2 || ex.msg == "" || out.Len() != 0 {
			t.Errorf("%q: %v, stdout %q; want exit 2 with usage and nothing on stdout", args, err, out.String())
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./cmd/rastrillo -run SessionKey`
Expected: FAIL, undefined `runSessionKey`.

- [ ] **Step 3: Implement it and add the usage line**

Usage line (draft): `rastrillo session-key <kid>   print a new signing key for signed sessions, and its public key`.

Also add the minimal `## rastrillo session-key` section to `docs/site/cli.md`: the usage line, the two lines it prints, and where each goes (the secret to the hub, the public key to every tenant). `internal/docsite`'s `TestCLICommandsAreDocumented` requires "rastrillo session-key" in that file as soon as `main.go` dispatches it, so without the section the docsite test is red from this commit until Task 9. The prose is draft; Task 9 Step 3 rewrites it after the copy-review pass.

- [ ] **Step 4: Run them to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./cmd/rastrillo ./internal/docsite`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/sessionkey.go cmd/rastrillo/sessionkey_test.go cmd/rastrillo/main.go docs/site/cli.md
git commit -m "rastrillo session-key: print a signing key and its public half" -m "A hub needs a P-256 key in a format sessions can parse, and a rotation needs a second one. Without a command, each operator would write a throwaway program, and the format would drift."
```

- [ ] **Step 6: Evidence**

CONTRIBUTING wants each new test seen failing against the old code. You do this, not the orchestrator. `git worktree add "$TMPDIR/ev-task8" HEAD~1` (this task's base commit), and in it add stubs of this task's new declarations: `runSessionKey` (writes nothing, returns nil). Stubs accept rather than refuse and never panic, so each test fails on its own assertion and the run reaches every test. Copy in `cmd/rastrillo/sessionkey_test.go`, then run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./cmd/rastrillo -run SessionKey`. Expected: every Step 1 test fails as `--- FAIL: <name>` with its own message, and none by build error or panic. A test that passes against its stub is either vacuous (fix the test) or covers behaviour that already existed (say so in the post). Post the stubs, the command, this output and Step 4's passing output in the branch's one "Test evidence" discussion (Task 1 opened it; append one comment for this task). Then `git worktree remove --force "$TMPDIR/ev-task8"`.

---

### Task 9: Docs, SKILL.md and the changelog

**Files:**
- Modify: `docs/site/reference/sessions.md` (the rotation order)
- Modify: `docs/site/sessions.md` (new section "Signed sessions for a hub")
- Modify: `docs/site/magic-links.md` (new section "Without a database")
- Modify: `docs/site/cli.md` (rewrite the `## rastrillo session-key` section Task 8 drafted)
- Modify: `SKILL.md` §5, and `skillmd_test.go` if the ceiling must rise
- Modify: `CHANGELOG.md` (`## Unreleased`)

This task starts with the one copy-review pass over everything Tasks 1 to 8 wrote as draft. The facts each page must carry are listed here; the wording is the review's. The task adds no test, so it has no evidence step.

- [ ] **Step 1: The copy-review pass**

Run the `copy-review` skill once, before anything below is committed. Extract: the prose added to `docs/site/reference/sessions.md` (Tasks 1 to 3), `docs/site/reference/auth.md` (Task 5) and `docs/site/reference/pow.md` (Task 6); the `docs/site/cli.md` section and the usage line in `cmd/rastrillo/main.go` (Task 8); the refresh 500 text in `sessions/refresh.go` (Task 3); and, so the pass stays one, a first draft of what Steps 2 to 5 write (the guides, the SKILL.md paragraph, the changelog entries), written from the facts listed below. Apply Paul's edits verbatim. Strings in Go source are changed in the source. Run `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./sessions ./cmd/rastrillo ./internal/docsite`. Expected: PASS. Steps 2 to 5 then put the reviewed text in place. Commit the reviewed Task 1 to 8 prose on its own: `git commit -m "Review the stateless sign-in copy" -m "Tasks 1 to 8 shipped draft prose so no commit waited on a review. This is the one pass over all of it."`

- [ ] **Step 2: The rotation order**

Tasks 1 to 6 put every new symbol on its reference page. Add the rotation order to `docs/site/reference/sessions.md`, and repeat it in the guide below. It matters because a hub that signs with a kid its tenants lack signs everyone out of those tenants:

1. Run `rastrillo session-key k2`. Set both public keys on every tenant (`carlos env set`).
2. On the hub, set `k2` as the signing key and keep `k1`'s public key in `Keys`.
3. After the 30-day session life, drop `k1` everywhere. After a compromise, drop it at once: every cookie under it dies within 15 minutes, and its holders sign in again.

- [ ] **Step 3: The guides**

`sessions.md` "Signed sessions for a hub": when to use it (model C, one origin), the hub's and the tenant's `Config` as below, mounting `Refresh`, what sign-out does (this browser only: a copy of the cookie stays refreshable until "sign out everywhere" or the end of the session life) against `SignoutEverywhere` (every copy, within 15 minutes), and the rotation order.

```go
// The hub
key, err := sessions.ParseSigningKey(os.Getenv("SESSION_KEY")) // carlos secrets set --app hub
a, err := auth.New(auth.Config{
	Origin: "https://ppl.example", InstanceKey: instanceKey, Proof: guard, Mailer: mailer,
	Records: store, Pepper: pepper, Blind: true,
	Signed:  &sessions.Signed{Name: "ppl", Key: key},
})
r.Handle("/_carlos/refresh", http.HandlerFunc(a.Refresh))
r.Post("/signout-everywhere", a.SignoutEverywhere)

// Each tenant
keys, err := sessions.ParsePublicKeys(os.Getenv("SESSION_KEYS")) // carlos env set --app tenant
s, err := sessions.New(sessions.Config{Origin: "https://ppl.example", Signed: &sessions.Signed{Name: "ppl", Keys: keys}})
r.Use(s.Middleware)
```

`magic-links.md` "Without a database": `Records`, `Pepper`, `Blind`, `pow.RecordNonces`, and a daily tick calling `a.Sweep` and `guard.Sweep`.

`cli.md` "rastrillo session-key": replace Task 8's draft with the reviewed text: what it prints, and where each line goes.

- [ ] **Step 4: SKILL.md**

Add to §5, after the magic-links paragraph:

```markdown
**No database (a home or hub):** `auth.Config{Records, Pepper, Signed:
&sessions.Signed{Name, Key}}` keeps users and links in `objrec` and signs
15-minute cookies; mount `a.Refresh` at `/_carlos/refresh`. A path-routed
tenant only verifies: `sessions.Config{Signed: &sessions.Signed{Name,
Keys}}`. `Blind` stores only the email's HMAC. docs/site/sessions.md
```

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test -run TestSkillMDStaysWithinBudget .`
Expected: PASS. It fails as written, since the file has 15 bytes of slack. Look for genuinely redundant prose first. If none, raise `skillBudget` by exactly the overflow and add a dated paragraph to its comment. The reason: a central app built from the source alone would take the SQLite default, and tenants on one origin would sign each other out.

- [ ] **Step 5: Changelog**

Under `## Unreleased`: one entry for stateless sign-in (records mode, signed sessions, refresh, the tenant verifier, blind mode), one for `pow.RecordNonces`, one for `rastrillo session-key`. Each entry ends with "Nothing changes for an app that does not set it."

- [ ] **Step 6: Run the gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add docs/site SKILL.md skillmd_test.go CHANGELOG.md
git commit -m "Document stateless sign-in, signed sessions and session-key" -m "The rotation order is written down because a hub that signs with a kid its tenants lack signs everyone out of them. SKILL.md gets one paragraph so an agent building a centre does not reach for the SQLite default."
```

---

## Self-review notes

- **Spec coverage.** §4 stateless: Tasks 4 to 6, with no `--db` opened in records mode (`New` refuses `DB`). §5 shared records: Tasks 4 and 5. The home-only and hub-only records belong to Plans 6 and 7. §6.1 "only where it's stored changes": Task 5 keeps `admit`'s order, with the screen, jar and second-factor seam untouched. §6.3: the cookie (Task 1), 15 minutes, refresh with epoch (Task 3), up to two kids (Tasks 1 and 2), verify-only tenants (Task 2). §8.2 "a signature proves who, never what": the tenant only learns `sub`, and access is its roster's (Plan 7). §9 leak profiles: Task 9's reference page. §11 session tests (a bumped epoch, rotation across two keys, an expired cookie refreshing): Task 3, `TestRefreshAfterAnEpochBumpSignsOut`, `TestRefreshMovesARetiredKeyOntoTheCurrentOne`, `TestRefreshReissuesWhenTheEpochMatches`.
- **Review Focus pins.** `TestSignedTenantNeverClearsAnUnknownKeyCookie`; `TestSignedExpiredPOSTKeepsItsBody`, `TestRefreshKeepsAPOSTAPOST`, `TestRefreshOfAFreshCookieJustGoesBack` and Task 7 step 3; `TestRefreshRefusesAHostileNext`; `TestRecordsRecreatedUserDoesNotReviveOldSessions`; `TestRecordsTwoDevicesAtOnceShareOneUser`.
- **Not built here.** The hub's membership copy (§6.4, Plan 7). The home's instance records and the hub's tenant records (Plans 6 and 7). Passkeys and second factors on a stateless centre, because both keep tables. Rebuilding indexes from `users/` in a daily tick: with index-first creation there is nothing to rebuild.

## Execution

Subagent-driven is recommended. Tasks 2, 3 and 5 each consume the previous task's unexported names, and a shipped mistake in signature checking or `next` handling is a sign-in bypass or an open redirect, so each one is worth its own reviewer.
