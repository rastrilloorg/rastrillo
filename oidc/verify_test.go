package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"
)

const testClientID = "test-client-id"

func testEngine(f *fakeIdP, now time.Time) *Engine {
	return &Engine{HTTP: f.client(), Now: func() time.Time { return now }}
}

func discoverOrFatal(t *testing.T, e *Engine, issuer string) Metadata {
	t.Helper()
	m, err := e.Discover(context.Background(), issuer)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return m
}

func TestVerifyAcceptsAValidToken(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)

	tok := f.mint(t, baseClaims(f.issuer, testClientID, "nonce-1", now))
	id, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Address != "alice@example.com" || id.AddressVerified != Asserted || id.Subject != "user-123" {
		t.Fatalf("unexpected identity: %+v", id)
	}
}

func TestVerifyRefusesEveryAlgorithmButRS256(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	claims := baseClaims(f.issuer, testClientID, "nonce-1", now)
	key, kid := f.currentKey()

	cases := []struct {
		name  string
		token string
	}{
		{"none, empty signature", rawToken(map[string]any{"alg": "none", "typ": "JWT"}, claims, "")},
		{"none, garbage signature", rawToken(map[string]any{"alg": "none", "typ": "JWT"}, claims, "AAAA")},
		{
			// The algorithm-confusion attack: HMAC-SHA256 using the
			// provider's own PUBLIC modulus as the secret, which is
			// exactly the classic "verifier that takes alg from the
			// token can be fooled" case this package's whole design
			// exists to refuse by construction.
			"HS256 signed with the RSA public modulus",
			signHS256(map[string]any{"alg": "HS256", "typ": "JWT", "kid": kid}, claims, key.PublicKey.N.Bytes()),
		},
		{"RS384", rawToken(map[string]any{"alg": "RS384", "typ": "JWT", "kid": kid}, claims, "AAAA")},
		{"ES256", rawToken(map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid}, claims, "AAAA")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tc.token, "nonce-1")
			if !errors.Is(err, ErrToken) {
				t.Fatalf("expected ErrToken, got %v", err)
			}
		})
	}
}

func TestVerifyRefusesWrongNonce(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	tok := f.mint(t, baseClaims(f.issuer, testClientID, "nonce-1", now))

	_, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-2")
	if !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken for a replayed/mismatched nonce, got %v", err)
	}
}

func TestVerifyRefusesExpiredToken(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	cl := baseClaims(f.issuer, testClientID, "nonce-1", now)
	cl["exp"] = now.Add(-5 * time.Minute).Unix() // well past the 2-minute skew
	tok := f.mint(t, cl)

	_, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1")
	if !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken for an expired token, got %v", err)
	}
}

func TestVerifyTolerateClockSkewWithinTwoMinutes(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	cl := baseClaims(f.issuer, testClientID, "nonce-1", now)
	cl["exp"] = now.Add(-90 * time.Second).Unix() // expired by less than the skew
	tok := f.mint(t, cl)

	if _, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1"); err != nil {
		t.Fatalf("expected skew to tolerate this, got %v", err)
	}
}

func TestVerifyRefusesFutureIssuedToken(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	cl := baseClaims(f.issuer, testClientID, "nonce-1", now)
	cl["iat"] = now.Add(time.Hour).Unix()
	tok := f.mint(t, cl)

	_, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1")
	if !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken for a token issued in the future, got %v", err)
	}
}

func TestVerifyRefusesWrongAudience(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	cl := baseClaims(f.issuer, "a-different-client", "nonce-1", now)
	tok := f.mint(t, cl)

	_, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1")
	if !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken for a token minted for a different application, got %v", err)
	}
}

func TestVerifyRequiresAzpWhenAudienceHasSeveralValues(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	cl := baseClaims(f.issuer, testClientID, "nonce-1", now)
	cl["aud"] = []string{testClientID, "some-other-audience"}
	// No azp at all.
	tok := f.mint(t, cl)
	if _, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1"); !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken when azp is missing on a multi-audience token, got %v", err)
	}

	cl["azp"] = "not-this-client"
	tok = f.mint(t, cl)
	if _, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1"); !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken when azp names a different client, got %v", err)
	}

	cl["azp"] = testClientID
	tok = f.mint(t, cl)
	if _, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1"); err != nil {
		t.Fatalf("expected azp naming this client to pass, got %v", err)
	}
}

func TestVerifyKeyRotationRefetchesOnUnknownKeyID(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)

	// Prime the cache under the OLD key id, then rotate the fake IdP's
	// key and kid before minting the real token: the cache must miss on
	// the new kid and refetch rather than serving the stale set.
	primer := f.mint(t, baseClaims(f.issuer, testClientID, "priming", now))
	if _, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, primer, "priming"); err != nil {
		t.Fatalf("priming verify: %v", err)
	}

	_, oldKid := f.currentKey()
	newKey := mustGenerateKey(t)
	f.rotateKey(newKey, "test-key-2")
	if oldKid == "test-key-2" {
		t.Fatal("test setup: kid did not actually change")
	}

	tok := f.mint(t, baseClaims(f.issuer, testClientID, "nonce-rot", now))
	if _, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-rot"); err != nil {
		t.Fatalf("expected the new key to be fetched on an unknown kid, got %v", err)
	}
}

func TestVerifyRefusesSignatureThatFailsAfterOneRetry(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)

	// A token naming a kid the provider has never published and never
	// will: the one retry still finds nothing, and this is a refusal
	// rather than an infinite retry loop.
	key, _ := f.currentKey()
	tok := signRS256(t, key, "a-kid-nobody-published", map[string]any{"alg": "RS256", "typ": "JWT", "kid": "a-kid-nobody-published"},
		baseClaims(f.issuer, testClientID, "nonce-1", now))
	_, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1")
	if !errors.Is(err, ErrToken) {
		t.Fatalf("expected ErrToken, got %v", err)
	}
}

func TestAddressVerifiedTristateDoesNotFoldSilenceToFalse(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)

	cases := []struct {
		name string
		set  func(map[string]any)
		want Tristate
	}{
		{"asserted (bool true)", func(c map[string]any) { c["email_verified"] = true }, Asserted},
		{"denied (bool false)", func(c map[string]any) { c["email_verified"] = false }, Denied},
		{"asserted (string \"true\", as some deployments send it)", func(c map[string]any) { c["email_verified"] = "true" }, Asserted},
		{"unstated (claim absent entirely, as Entra sends it)", func(c map[string]any) { delete(c, "email_verified") }, Unstated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cl := baseClaims(f.issuer, testClientID, "nonce-"+tc.name, now)
			tc.set(cl)
			tok := f.mint(t, cl)
			id, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-"+tc.name)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if id.AddressVerified != tc.want {
				t.Fatalf("got %v, want %v", id.AddressVerified, tc.want)
			}
		})
	}
}

func TestAddressExtractionRejectsTwoAtSigns(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)

	cl := baseClaims(f.issuer, testClientID, "nonce-1", now)
	// A second @ makes a naive "read the domain from the last segment"
	// allowlist checkable by an address whose local part is somebody
	// else's whole address.
	cl["email"] = "attacker@evil.example@firm.co.uk"
	tok := f.mint(t, cl)
	id, err := e.Verify(context.Background(), m, Connection{ClientID: testClientID}, tok, "nonce-1")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Address != "" {
		t.Fatalf("expected the two-@ address to be rejected (empty Address), got %q", id.Address)
	}
}

func TestDiscoverRefusesMultiTenantPlaceholderIssuer(t *testing.T) {
	f := newFakeIdPWithIssuer(t, "https://login.microsoftonline.com/{tenantid}/v2.0")
	e := testEngine(f, time.Now())
	_, err := e.Discover(context.Background(), f.issuer)
	if !errors.Is(err, ErrMultiTenant) {
		t.Fatalf("expected ErrMultiTenant, got %v", err)
	}
}

func TestDiscoverRefusesIssuerMismatch(t *testing.T) {
	f := newFakeIdPWithIssuer(t, "https://not-the-url-this-was-fetched-from.example")
	e := testEngine(f, time.Now())
	_, err := e.Discover(context.Background(), f.issuer)
	if err == nil || !strings.Contains(err.Error(), "the two must be the same") {
		t.Fatalf("expected an issuer-mismatch refusal, got %v", err)
	}
}

func TestAdmittedRefusesNoAddress(t *testing.T) {
	p, _ := ProviderFor(Microsoft)
	err := Admitted(p, Connection{}, Identity{Address: ""})
	if !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("expected ErrNotAdmitted, got %v", err)
	}
}

func TestAdmittedRefusesDeniedVerified(t *testing.T) {
	p, _ := ProviderFor(Microsoft)
	err := Admitted(p, Connection{}, Identity{Address: "a@b.com", AddressVerified: Denied})
	if !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("expected ErrNotAdmitted, got %v", err)
	}
}

func TestAdmittedAllowsUnstatedVerified(t *testing.T) {
	p, _ := ProviderFor(Microsoft) // Microsoft never asserts the claim
	err := Admitted(p, Connection{}, Identity{Address: "a@b.com", AddressVerified: Unstated})
	if err != nil {
		t.Fatalf("expected Unstated to be let through, got %v", err)
	}
}

func TestAdmittedRequiresHostedDomainForGoogleLikeProvider(t *testing.T) {
	p, _ := ProviderFor(Google)
	conn := Connection{AllowedDomains: []string{"firm.example"}}

	t.Run("no hd at all (a personal account)", func(t *testing.T) {
		err := Admitted(p, conn, Identity{Address: "a@gmail.com", AddressVerified: Asserted, HostedDomain: ""})
		if !errors.Is(err, ErrNotAdmitted) {
			t.Fatalf("expected ErrNotAdmitted, got %v", err)
		}
	})
	t.Run("hd present but not an allowed domain", func(t *testing.T) {
		err := Admitted(p, conn, Identity{Address: "a@other.example", AddressVerified: Asserted, HostedDomain: "other.example"})
		if !errors.Is(err, ErrNotAdmitted) {
			t.Fatalf("expected ErrNotAdmitted, got %v", err)
		}
	})
	t.Run("hd matches an allowed domain", func(t *testing.T) {
		err := Admitted(p, conn, Identity{Address: "a@firm.example", AddressVerified: Asserted, HostedDomain: "firm.example"})
		if err != nil {
			t.Fatalf("expected this to be admitted, got %v", err)
		}
	})
}

func TestExchangeSurfacesTheProvidersOwnWordsOnAFailedCodeExchange(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	e := testEngine(f, now)
	m := discoverOrFatal(t, e, f.issuer)
	f.setTokenErr("invalid_grant")

	start, err := NewStart()
	if err != nil {
		t.Fatalf("NewStart: %v", err)
	}
	_, err = e.Exchange(context.Background(), m, Connection{ClientID: testClientID, ClientSecret: "s", RedirectURI: "https://app.test/return"}, "a-code", start)
	var oerr *Error
	if !errors.As(err, &oerr) || oerr.Message != "invalid_grant" {
		t.Fatalf("expected the provider's own error message surfaced, got %v", err)
	}
}

func mustGenerateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}
