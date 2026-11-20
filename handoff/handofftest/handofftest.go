// Package handofftest plays the root app in tests, so an instance's tests
// never hand-sign a token themselves.
package handofftest

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/instance"
	"amadan.net/rastrillo/rastrillo/roster"
)

const lifetime = 60 * time.Second

// Root is a fake root app with one signing key.
type Root struct {
	Origin string
	Key    *crypto.Keypair
	KeyID  string

	// Now is the root's clock. Nil means time.Now. Set it to the clock
	// the instance under test uses, or its checks will see tokens from
	// another time.
	Now func() time.Time
}

// New makes a root at https://root.test with a fresh key.
func New(t testing.TB) *Root {
	t.Helper()
	key, err := crypto.Generate()
	if err != nil {
		t.Fatalf("handofftest: key: %v", err)
	}
	return &Root{Origin: "https://root.test", Key: key, KeyID: "k1"}
}

func (root *Root) now() time.Time {
	if root.Now == nil {
		return time.Now()
	}
	return root.Now()
}

// Setup is what an instance under this root is started with.
func (root *Root) Setup() instance.Setup {
	return instance.Setup{
		Mode: instance.Rooted,
		Root: root.Origin,
		Keys: map[string][]byte{root.KeyID: root.Key.SignPub()},
	}
}

// Assert answers the instance's Enter redirect the way the root would for
// a signed-in subject. It returns the form to POST back to the instance.
func (root *Root) Assert(
	t testing.TB, enter *http.Response, subject string,
) url.Values {
	t.Helper()
	location, err := enter.Location()
	if err != nil {
		t.Fatalf("handofftest: enter redirect: %v", err)
	}
	query := location.Query()
	id, err := assertion.RandomID()
	if err != nil {
		t.Fatalf("handofftest: assertion id: %v", err)
	}
	now := root.now().Unix()
	token, err := assertion.Sign(root.Key, assertion.Claims{
		Version:   1,
		Issuer:    root.Origin,
		Audience:  query.Get("aud"),
		Subject:   subject,
		KeyID:     root.KeyID,
		ID:        id,
		Nonce:     query.Get("nonce"),
		IssuedAt:  now,
		ExpiresAt: now + int64(lifetime/time.Second),
		AuthTime:  now,
	})
	if err != nil {
		t.Fatalf("handofftest: sign assertion: %v", err)
	}
	return url.Values{"assertion": {token}}
}

// Roster signs a snapshot for one instance, ready to POST to roster.Path.
func (root *Root) Roster(
	t testing.TB, audience string, sequence int64,
	state roster.State, members ...roster.Member,
) string {
	t.Helper()
	now := root.now().Unix()
	token, err := roster.Sign(root.Key, roster.Snapshot{
		Version:   1,
		Issuer:    root.Origin,
		Audience:  audience,
		KeyID:     root.KeyID,
		Sequence:  sequence,
		IssuedAt:  now,
		ExpiresAt: now + int64(lifetime/time.Second),
		State:     state,
		Members:   members,
	})
	if err != nil {
		t.Fatalf("handofftest: sign roster: %v", err)
	}
	return token
}
