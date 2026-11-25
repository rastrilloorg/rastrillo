package granttoken

import (
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// fuzzKeypair is fixed, so a token saved in the corpus still verifies
// next run and in every fuzz worker.
const fuzzKeypair = `{"sign_priv_d":"` +
	"0909090909090909090909090909090909090909090909090909090909090909" +
	`","box_priv_d":"` +
	"0505050505050505050505050505050505050505050505050505050505050505" +
	`"}`

// FuzzVerify mutates a grant token. The root signed one set of claims,
// so a token that verifies must carry exactly those; the signature
// bytes may differ (ECDSA's other s), the claims may not.
func FuzzVerify(f *testing.F) {
	key, err := crypto.UnmarshalKeypair([]byte(fuzzKeypair))
	if err != nil {
		f.Fatal(err)
	}
	verifier, err := NewVerifier(rootOrigin, siteOrigin,
		map[string][]byte{"key_1": key.SignPub()})
	if err != nil {
		f.Fatal(err)
	}
	token, err := Sign(key, goodClaims())
	if err != nil {
		f.Fatal(err)
	}
	body := strings.TrimPrefix(token, Prefix)
	payload, signature, _ := strings.Cut(body, ".")
	f.Add(token)
	f.Add(body)
	f.Add(Prefix + payload + ".")
	f.Add(Prefix + payload + "=." + signature)
	f.Add(Prefix + payload + "." + signature + ".")
	f.Add(Prefix + strings.Repeat("A", maxToken))
	f.Add(Prefix + "e30." + signature)
	now := time.Unix(issuedAt+60, 0)
	// A seed that stopped verifying would leave the property below
	// with nothing to hold.
	if _, err := verifier.Verify(token, now); err != nil {
		f.Fatalf("the signed token: %v", err)
	}
	f.Fuzz(func(t *testing.T, got string) {
		claims, err := verifier.Verify(got, now)
		if err != nil {
			if claims != (Claims{}) {
				t.Fatalf("%q refused (%v) with claims %+v", got, err, claims)
			}
			return
		}
		if claims != goodClaims() {
			t.Fatalf("%q verified as %+v; the root only signed %+v",
				got, claims, goodClaims())
		}
	})
}

// FuzzRedact checks no grant token survives Redact, whatever surrounds
// it: a log line is the likeliest place for a bearer token to leak.
func FuzzRedact(f *testing.F) {
	f.Add("Authorization: Bearer " + Prefix + "abc.def")
	f.Add(Prefix + Prefix + "abc")
	f.Add("x" + Prefix + "a b" + Prefix)
	f.Add(Prefix)
	f.Add(strings.ToUpper(Prefix) + "abc")
	f.Fuzz(func(t *testing.T, text string) {
		redacted := Redact(text)
		if leaked := tokenPattern.FindString(redacted); leaked != "" {
			t.Fatalf("%q redacted to %q, which still holds %q",
				text, redacted, leaked)
		}
	})
}
