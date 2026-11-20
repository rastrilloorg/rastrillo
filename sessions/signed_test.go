package sessions_test

import (
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func TestKeypairSignsForPublic(t *testing.T) {
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	if key.Keypair().BoxPriv != nil {
		t.Fatal("a session key must not hold a box key")
	}
	issuer, audience := "https://root.test", "https://tenant.test"
	id, _ := assertion.RandomID()
	nonce, _ := assertion.RandomID()
	now := time.Now()
	token, err := assertion.Sign(key.Keypair(), assertion.Claims{
		Version: 1, Issuer: issuer, Audience: audience, Subject: "person",
		KeyID: key.KeyID(), ID: id, Nonce: nonce,
		IssuedAt: now.Unix(), ExpiresAt: now.Unix() + 60, AuthTime: now.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := sessions.ParsePublicKeys(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := assertion.NewVerifier(issuer, audience, keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(token, now); err != nil {
		t.Fatalf("the assertion did not verify: %v", err)
	}
}
