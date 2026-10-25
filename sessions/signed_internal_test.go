package sessions

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
)

// fmtc keeps the claim templates below on one line each.
var fmtc = fmt.Sprintf

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
	if k.kp == nil || k.kp.SignPriv == nil {
		t.Fatal("the signing key holds no private key")
	}
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
	v, err := k1.sign(goodClaims())
	p, s, ok := strings.Cut(v, ".")
	if err != nil || !ok || len(p) < 4 || len(s) < 4 {
		t.Fatalf("sign gave %q, %v; want payload.signature", v, err)
	}
	flip := func(x string) string { b := []byte(x); b[3] ^= 1; return string(b) }
	for name, bad := range map[string]string{
		"payload": flip(p) + "." + s, "signature": p + "." + flip(s),
		"two dots": v + ".x", "empty": "", "oversized": strings.Repeat("a", 4097),
	} {
		// Exactly invalid, empty included: an empty value is a cookie
		// that was sent malformed, not one that is absent.
		if _, st := verifyCookie(keysOf(t, k1), bad, t0); st != cookieInvalid {
			t.Errorf("%s: %v, want invalid", name, st)
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

// An assertion is already refused for its members, so this is the test
// that the signing context itself separates the two: a well-formed
// cookie payload signed under assertion's context must not verify.
func TestSignedCookieRefusesAnotherContext(t *testing.T) {
	k := testKey(t, "k1")
	v, err := k.sign(goodClaims())
	p, _, ok := strings.Cut(v, ".")
	if err != nil || !ok {
		t.Fatalf("sign gave %q, %v; want payload.signature", v, err)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(p)
	sig, err := crypto.Sign(k.kp, "rastrillo/assertion/v1", payload)
	if err != nil {
		t.Fatal(err)
	}
	forged := p + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, st := verifyCookie(keysOf(t, k), forged, t0); st != cookieInvalid {
		t.Fatalf("a payload signed under assertion's context verified: %v", st)
	}
}

func TestSignedCookieRefusesBadClaims(t *testing.T) {
	k := testKey(t, "k1")
	ok := `"kid":"k1","sub":"u1","sid":"AAAAAAAAAAAAAAAAAAAAAA","epoch":1,"iat":%d,"exp":%d,"at":%d,"m":"magiclink","auth_time":0`
	iat, exp, at := t0.Unix(), t0.Unix()+900, t0.Unix()-60
	for name, payload := range map[string]string{
		"version 2":             `{"v":2,` + fmtc(ok, iat, exp, at) + `}`,
		"no version":            `{` + fmtc(ok, iat, exp, at) + `}`,
		"unknown member":        `{"v":1,"admin":true,` + fmtc(ok, iat, exp, at) + `}`,
		"duplicate sub":         `{"v":1,"sub":"other",` + fmtc(ok, iat, exp, at) + `}`,
		"life over 15m":         `{"v":1,` + fmtc(ok, iat, iat+901, at) + `}`,
		"starts after iat":      `{"v":1,` + fmtc(ok, iat, exp, iat+1) + `}`,
		"iat 31s ahead":         `{"v":1,` + fmtc(ok, iat+31, iat+31+900, at) + `}`,
		"subject is an address": `{"v":1,` + strings.Replace(fmtc(ok, iat, exp, at), `"u1"`, `"a@b.c"`, 1) + `}`,
		"short sid":             `{"v":1,` + strings.Replace(fmtc(ok, iat, exp, at), "AAAAAAAAAAAAAAAAAAAAAA", "AAAA", 1) + `}`,
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
	if len(point) != 65 {
		t.Fatalf("Public() carries %d bytes, want a 65-byte point", len(point))
	}
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
