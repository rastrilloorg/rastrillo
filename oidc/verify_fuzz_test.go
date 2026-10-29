package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

// FuzzTokenShape fuzzes the raw token string Verify is handed — the
// pre-signature parsing (part count, base64url, JSON header/claims
// shape). Random bytes almost never pass an RS256 check, so this reaches
// the split/decode/unmarshal code but not much past it; FuzzClaims below
// is what reaches the code that runs AFTER a signature checks out
// (review finding 12: a raw-bytes fuzzer alone barely touches flexBool,
// the aud string-or-array decode, or address()).
func FuzzTokenShape(f *testing.F) {
	f.Add("")
	f.Add("...")
	f.Add("a.b.c")
	f.Add("a.b")
	f.Add("a.b.c.d")
	f.Add(rawToken(map[string]any{"alg": "none"}, map[string]any{"sub": "x"}, ""))
	f.Add(rawToken(map[string]any{"alg": "RS256", "kid": "k1"}, map[string]any{}, "AAAA"))
	f.Add(rawToken(map[string]any{}, map[string]any{}, ""))

	e := &Engine{Now: func() time.Time { return time.Now() }}
	m := Metadata{Issuer: "https://issuer.example", JWKSURI: "https://issuer.example/jwks"}
	conn := Connection{ClientID: "client"}

	f.Fuzz(func(t *testing.T, token string) {
		// The only property under test: this never panics, on any input
		// whatsoever. A syntactically-plausible RS256 header DOES reach
		// a real network dial to issuer.example on a cache miss — this
		// is not offline, and does not claim to be; the 10ms context
		// below is what keeps that bounded and fast rather than
		// avoiding it (found inaccurate in code review: the comment
		// this replaces claimed the network was never reached, which is
		// not true of every input in this corpus).
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Verify panicked on %q: %v", token, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, _ = e.Verify(ctx, m, conn, token, "nonce")
	})
}

// FuzzClaims fuzzes the CLAIMS payload and signs it with a fixed test
// key, so the signature check passes and the fuzzer actually reaches
// flexBool's string/bool decoding, audience's string/array decoding, and
// address()'s two-@ guard — the code a raw-bytes fuzzer cannot reach.
func FuzzClaims(f *testing.F) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		f.Fatalf("generate key: %v", err)
	}
	kid := "fuzz-key"
	issuer := "https://issuer.example"

	seeds := [][]byte{
		[]byte(`{"iss":"https://issuer.example","sub":"u1","aud":"client","nonce":"n","exp":9999999999,"email":"a@b.com","email_verified":true}`),
		[]byte(`{"iss":"https://issuer.example","sub":"u1","aud":["client","other"],"azp":"client","nonce":"n","exp":9999999999}`),
		[]byte(`{"iss":"https://issuer.example","sub":"u1","aud":"client","nonce":"n","exp":9999999999,"email_verified":"true"}`),
		[]byte(`{"iss":"https://issuer.example","sub":"u1","aud":"client","nonce":"n","exp":9999999999,"email":"a@b@c.com"}`),
		[]byte(`not json at all`),
		[]byte(`{}`),
		[]byte(`null`),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	e := &Engine{
		Now: func() time.Time { return time.Unix(1700000000, 0) },
	}
	m := Metadata{Issuer: issuer, JWKSURI: issuer + "/jwks"}
	conn := Connection{ClientID: "client"}

	f.Fuzz(func(t *testing.T, claimsJSON []byte) {
		header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
		token := b64url(header) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
		sum := sha256.Sum256([]byte(token))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if err != nil {
			t.Skip()
		}
		token += "." + base64.RawURLEncoding.EncodeToString(sig)

		// Prime the key cache directly rather than through a network
		// fetch, so this fuzz target runs offline and fast: the slot's
		// shape is internal to the package, which is exactly why this
		// test lives inside it.
		slot := e.cache.keySlotFor(m.JWKSURI)
		slot.mu.Lock()
		slot.keys = map[string]*rsa.PublicKey{kid: &key.PublicKey}
		slot.fetched = time.Now()
		slot.mu.Unlock()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Verify panicked on claims %q: %v", claimsJSON, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, _ = e.Verify(ctx, m, conn, token, "n")
	})
}
