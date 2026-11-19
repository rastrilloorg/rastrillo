package oidc

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMain turns skipIssuerShapeCheck (providers.go) on for the whole
// suite: almost every test here drives a connection against a fake
// provider on a loopback address, which cannot be disguised as
// login.microsoftonline.com or accounts.google.com.
// TestIssuerMustMatchKind (providers_test.go) is the one test that turns
// it back off, to prove the check it stands in for still fires.
func TestMain(m *testing.M) {
	skipIssuerShapeCheck = true
	os.Exit(m.Run())
}

// fakeIdP stands up a real TLS loopback server speaking just enough of
// discovery, JWKS and the token endpoint to drive this package's flow —
// ips's own design insists every test exercises real TLS rather than an
// http exception for "local", and this keeps that property.
type fakeIdP struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	kid    string
	issuer string

	mu        sync.Mutex
	nextToken string // what the token endpoint hands back as id_token
	tokenErr  string // if set, the token endpoint answers this OAuth error instead

	// reportedIssuer, when set, is what the discovery document's own
	// "issuer" field says — independent of f.issuer, which is always the
	// real server URL Discover fetches FROM. Used to test Discover's own
	// mismatch and multi-tenant-placeholder refusals.
	reportedIssuer string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	f := &fakeIdP{t: t, key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("/jwks", f.jwks)
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the fake IdP does not serve a real authorize page", http.StatusNotImplemented)
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	f.issuer = f.srv.URL
	return f
}

// newFakeIdPWithIssuer is newFakeIdP, except the discovery document's own
// "issuer" field reports the given value rather than the server's real
// URL — for testing Discover's mismatch and multi-tenant-placeholder
// refusals, which are about what the DOCUMENT claims, not where it was
// fetched from.
func newFakeIdPWithIssuer(t *testing.T, reportedIssuer string) *fakeIdP {
	t.Helper()
	f := newFakeIdP(t)
	f.reportedIssuer = reportedIssuer
	return f
}

// client is an http.Client that trusts the fake server's own certificate
// — what Engine.HTTP is set to in every test using this fixture.
func (f *fakeIdP) client() *http.Client { return f.srv.Client() }

func (f *fakeIdP) discovery(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	issuer := f.issuer
	if f.reportedIssuer != "" {
		issuer = f.reportedIssuer
	}
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":                 issuer,
		"authorization_endpoint": issuer + "/authorize",
		"token_endpoint":         issuer + "/token",
		"jwks_uri":               issuer + "/jwks",
	})
}

func (f *fakeIdP) jwks(w http.ResponseWriter, r *http.Request) {
	key, kid := f.currentKey()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(bigEndianTrimmed(key.PublicKey.E)),
		}},
	})
}

// currentKey and rotateKey guard f.key/f.kid with the same mutex as the
// rest of this fixture's mutable state: the httptest server handles each
// request on its own goroutine, so a test rotating the key while a
// request is in flight is a real data race without this.
func (f *fakeIdP) currentKey() (*rsa.PrivateKey, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.key, f.kid
}

func (f *fakeIdP) rotateKey(key *rsa.PrivateKey, kid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.key, f.kid = key, kid
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokenErr != "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": f.tokenErr})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id_token": f.nextToken})
}

// setToken is what a test calls to control what Exchange receives back.
func (f *fakeIdP) setToken(tok string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextToken, f.tokenErr = tok, ""
}

func (f *fakeIdP) setTokenErr(oauthErr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenErr, f.nextToken = oauthErr, ""
}

// mint builds and signs a real RS256 ID token from the given claims,
// defaulting iss/aud/exp/iat/nonce from the arguments so a test only
// states what it wants to be unusual about.
func (f *fakeIdP) mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	key, kid := f.currentKey()
	return signRS256(t, key, kid, map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}, claims)
}

func baseClaims(issuer, clientID, nonce string, now time.Time) map[string]any {
	return map[string]any{
		"iss": issuer, "sub": "user-123", "aud": clientID, "nonce": nonce,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"email": "alice@example.com", "email_verified": true, "name": "Alice Example",
	}
}

func bigEndianTrimmed(e int) []byte {
	b := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	i := 0
	for i < len(b)-1 && b[i] == 0 {
		i++
	}
	return b[i:]
}

func b64url(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// signRS256 builds header.claims and signs it with key.
func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, header, claims map[string]any) string {
	t.Helper()
	signed := b64url(header) + "." + b64url(claims)
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// signHS256 is the algorithm-confusion attack's own construction: sign
// with HMAC-SHA256 using a provider's PUBLIC RSA modulus as the HMAC
// secret, which is exactly what a verifier that takes its algorithm from
// the token's own header can be fooled into checking against.
func signHS256(header, claims map[string]any, hmacSecret []byte) string {
	signed := b64url(header) + "." + b64url(claims)
	mac := hmac.New(sha256.New, hmacSecret)
	mac.Write([]byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// rawToken builds header.claims.sig verbatim, for constructing malformed
// tokens (an empty or garbage signature segment, alg=none, ...).
func rawToken(header, claims map[string]any, sig string) string {
	return b64url(header) + "." + b64url(claims) + "." + sig
}
