package oidc

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// newTestDB is a temp database with sessions.Schema applied — the same
// pattern sessions' own tests use.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, sessions.Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return d.Writer()
}

const testOrigin = "https://app.test"

// ── fail-before/pass-after evidence for review findings 1 and 2 ───────
//
// CONTRIBUTING rule 3 asks for a test shown failing before a fix and
// passing after. There is no earlier commit of THIS package to run
// these against — it is new — so "before" is the naive approach spec
// revision 1 actually proposed (Method with a colon, AuthTime taken on
// faith from the provider), run against this checkout's REAL sessions
// package rather than asserted in prose. "After" is this package's own
// Return, exercised end to end.

func newSignedModeSessions(t *testing.T, now *time.Time) *sessions.Sessions {
	t.Helper()
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatalf("NewSigningKey: %v", err)
	}
	s, err := sessions.New(sessions.Config{
		Origin: testOrigin,
		Now:    func() time.Time { return *now },
		Signed: &sessions.Signed{Name: "app", Key: key,
			Epoch: func(context.Context, string) (int64, error) { return 1, nil }},
	})
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}
	return s
}

func TestBeforeTheFix_NaiveMethodAndAuthTimeFailSignedModeSignIn(t *testing.T) {
	now := time.Now()
	sess := newSignedModeSessions(t, &now)

	t.Run("a colon in Method, as spec revision 1 proposed", func(t *testing.T) {
		err := sess.SignIn(httptest.NewRecorder(), httptest.NewRequest("GET", testOrigin+"/", nil),
			sessions.Session{Subject: "opaque-subject-1", Method: "oidc:microsoft", AuthTime: now})
		if err == nil {
			t.Fatal("expected Signed mode to refuse a Method containing a colon, but it was accepted")
		}
	})

	t.Run("AuthTime taken on faith, slightly ahead of the server's own clock", func(t *testing.T) {
		err := sess.SignIn(httptest.NewRecorder(), httptest.NewRequest("GET", testOrigin+"/", nil),
			sessions.Session{Subject: "opaque-subject-1", Method: "oidc-microsoft", AuthTime: now.Add(90 * time.Second)})
		if err == nil {
			t.Fatal("expected Signed mode to refuse an AuthTime after its own issue time, but it was accepted")
		}
	})
}

func TestAfterTheFix_HandlersReturnMintsAWorkingSignedModeSession(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	sess := newSignedModeSessions(t, &now)

	h, err := New(Config{
		Sessions:    sess,
		InstanceKey: strings.Repeat("k", 32),
		Now:         func() time.Time { return now },
		HTTP:        f.client(),
		Connections: func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error) {
			return Connection{Issuer: f.issuer, ClientID: testClientID, ClientSecret: "secret", RedirectURI: testOrigin + "/signin/oidc/microsoft/return"}, true, nil
		},
		Admit: func(ctx context.Context, conn Connection, id Identity) (string, error) {
			return "opaque-subject-1", nil // never the address itself (review finding 1)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/signin/oidc/{provider}", h.Begin)
	r.Get("/signin/oidc/{provider}/return", h.Return)

	// Begin, to capture the real ceremony cookie and the real state this
	// package minted (rather than fabricating one, which would not be
	// testing this package's own AuthCodeURL/Start wiring).
	beginW := httptest.NewRecorder()
	r.ServeHTTP(beginW, httptest.NewRequest("GET", testOrigin+"/signin/oidc/microsoft", nil))
	if beginW.Code != http.StatusSeeOther {
		t.Fatalf("Begin: %d %s", beginW.Code, beginW.Body)
	}
	loc, err := url.Parse(beginW.Result().Header.Get("Location"))
	if err != nil {
		t.Fatalf("Begin redirected to an unparseable URL: %v", err)
	}
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	cookies := beginW.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one ceremony cookie, got %d", len(cookies))
	}

	// An AuthTime one second AHEAD of the server's own clock — exactly
	// the shape finding 2 showed breaking Signed mode on faith — minted
	// by a real token through this package's real Verify, so the clamp
	// in Return is what has to save it, not a test fabricating success.
	cl := baseClaims(f.issuer, testClientID, nonce, now)
	cl["auth_time"] = now.Add(time.Second).Unix()
	f.setToken(f.mint(t, cl))

	returnReq := httptest.NewRequest("GET", testOrigin+"/signin/oidc/microsoft/return?code=abc&state="+state, nil)
	returnReq.AddCookie(cookies[0])
	returnW := httptest.NewRecorder()
	r.ServeHTTP(returnW, returnReq)
	if returnW.Code != http.StatusSeeOther {
		t.Fatalf("Return: %d %s", returnW.Code, returnW.Body)
	}

	sessionCookies := returnW.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range sessionCookies {
		if c.Name == sess.CookieName() {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatalf("Return did not mint a session cookie: %+v", sessionCookies)
	}

	// Prove the cookie actually verifies as a real session — the point
	// of Signed mode — not merely that something with the right name
	// was set.
	checkReq := httptest.NewRequest("GET", testOrigin+"/", nil)
	checkReq.AddCookie(sessionCookie)
	got, ok := sess.From(checkReq)
	if !ok {
		t.Fatal("the minted cookie does not verify as a session")
	}
	if got.Subject != "opaque-subject-1" {
		t.Fatalf("Subject = %q, want the opaque id Admit returned", got.Subject)
	}
	if got.Method != "oidc-microsoft" {
		t.Fatalf("Method = %q, want oidc-microsoft (hyphen, Signed mode's alphabet)", got.Method)
	}
}

// ── review finding 6: Admit must run before SecondFactor ───────────────

func TestAdmitRefusalMeansSecondFactorIsNeverCalled(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	sess := newDBModeSessions(t, &now)

	secondFactorCalled := false
	h, err := New(Config{
		Sessions:    sess,
		InstanceKey: strings.Repeat("k", 32),
		Now:         func() time.Time { return now },
		HTTP:        f.client(),
		Connections: func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error) {
			return Connection{Issuer: f.issuer, ClientID: testClientID, ClientSecret: "secret", RedirectURI: testOrigin + "/signin/oidc/microsoft/return"}, true, nil
		},
		Admit: func(ctx context.Context, conn Connection, id Identity) (string, error) {
			return "", ErrNotAdmitted
		},
		SecondFactor: func(w http.ResponseWriter, r *http.Request, s sessions.Session) (bool, error) {
			secondFactorCalled = true
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/signin/oidc/{provider}", h.Begin)
	r.Get("/signin/oidc/{provider}/return", h.Return)

	beginW := httptest.NewRecorder()
	r.ServeHTTP(beginW, httptest.NewRequest("GET", testOrigin+"/signin/oidc/microsoft", nil))
	loc, _ := url.Parse(beginW.Result().Header.Get("Location"))
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	cookies := beginW.Result().Cookies()

	f.setToken(f.mint(t, baseClaims(f.issuer, testClientID, nonce, now)))
	returnReq := httptest.NewRequest("GET", testOrigin+"/signin/oidc/microsoft/return?code=abc&state="+state, nil)
	returnReq.AddCookie(cookies[0])
	returnW := httptest.NewRecorder()
	r.ServeHTTP(returnW, returnReq)

	if returnW.Code == http.StatusSeeOther {
		t.Fatalf("expected the refusal to answer something other than a sign-in redirect, got %d", returnW.Code)
	}
	if secondFactorCalled {
		t.Fatal("SecondFactor was called after Admit refused — the exact route to an unauthorised session finding 6 found")
	}
}

func newDBModeSessions(t *testing.T, now *time.Time) *sessions.Sessions {
	t.Helper()
	s, err := sessions.New(sessions.Config{Origin: testOrigin, DB: newTestDB(t), Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}
	return s
}

// ── review finding 4: the hub tenant key threads through, not the URL ──

func TestConnectionKeyFromBeginSurvivesToReturnUnchanged(t *testing.T) {
	f := newFakeIdP(t)
	now := time.Now()
	sess := newDBModeSessions(t, &now)

	var beginKey, returnKey ConnectionKey
	nextKey := ConnectionKey("tenant-a")
	h, err := New(Config{
		Sessions:      sess,
		InstanceKey:   strings.Repeat("k", 32),
		Now:           func() time.Time { return now },
		HTTP:          f.client(),
		ConnectionKey: func(r *http.Request) ConnectionKey { return nextKey },
		Connections: func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error) {
			if beginKey == "" {
				beginKey = key
			} else {
				returnKey = key
			}
			return Connection{Issuer: f.issuer, ClientID: testClientID, ClientSecret: "secret", RedirectURI: testOrigin + "/signin/oidc/microsoft/return"}, true, nil
		},
		Admit: func(ctx context.Context, conn Connection, id Identity) (string, error) { return "subj", nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/signin/oidc/{provider}", h.Begin)
	r.Get("/signin/oidc/{provider}/return", h.Return)

	beginW := httptest.NewRecorder()
	r.ServeHTTP(beginW, httptest.NewRequest("GET", testOrigin+"/signin/oidc/microsoft", nil))
	loc, _ := url.Parse(beginW.Result().Header.Get("Location"))
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	cookies := beginW.Result().Cookies()

	// Change what ConnectionKey would answer NOW, simulating a second,
	// unrelated ceremony starting for a different tenant on the same
	// server between this Begin and this Return — the provider's own
	// callback carries nothing that would let Return notice.
	nextKey = ConnectionKey("tenant-b")

	f.setToken(f.mint(t, baseClaims(f.issuer, testClientID, nonce, now)))
	returnReq := httptest.NewRequest("GET", testOrigin+"/signin/oidc/microsoft/return?code=abc&state="+state, nil)
	returnReq.AddCookie(cookies[0])
	returnW := httptest.NewRecorder()
	r.ServeHTTP(returnW, returnReq)
	if returnW.Code != http.StatusSeeOther {
		t.Fatalf("Return: %d %s", returnW.Code, returnW.Body)
	}

	if beginKey != "tenant-a" {
		t.Fatalf("Begin resolved key %q, want tenant-a", beginKey)
	}
	if returnKey != "tenant-a" {
		t.Fatalf("Return resolved key %q, want tenant-a (the one Begin actually used) — not tenant-b, which is what Config.ConnectionKey would answer if Return called it again", returnKey)
	}
}

// ── ceremony cookie properties (design D4) ──────────────────────────────

func TestCeremonySealedForOneProviderCannotBeOpenedForAnother(t *testing.T) {
	instanceKey := strings.Repeat("k", 32)
	now := time.Now()
	sealed, err := sealCeremony(instanceKey, Microsoft, ceremony{
		Start: Start{State: "s", Nonce: "n", Verifier: "v"}, Expires: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("sealCeremony: %v", err)
	}
	if _, err := openCeremony(instanceKey, Google, sealed, now); err == nil {
		t.Fatal("a ceremony sealed for microsoft opened under google's derived key")
	}
	if _, err := openCeremony(instanceKey, Microsoft, sealed, now); err != nil {
		t.Fatalf("the correct kind failed to open: %v", err)
	}
}

func TestCeremonyExpiryInsideTheBlobIsTheRealBound(t *testing.T) {
	// ips's own D88.5 bug: a cookie's Max-Age is the BROWSER's bound.
	// This asserts the one inside the sealed blob is what this package
	// actually checks, independent of anything the cookie layer does.
	instanceKey := strings.Repeat("k", 32)
	start := time.Now()
	sealed, err := sealCeremony(instanceKey, Microsoft, ceremony{
		Start: Start{State: "s", Nonce: "n", Verifier: "v"}, Expires: start.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("sealCeremony: %v", err)
	}
	if _, err := openCeremony(instanceKey, Microsoft, sealed, start.Add(30*time.Second)); err != nil {
		t.Fatalf("expected this to still be open: %v", err)
	}
	if _, err := openCeremony(instanceKey, Microsoft, sealed, start.Add(2*time.Minute)); err == nil {
		t.Fatal("expected a ceremony past its sealed expiry to be refused, regardless of any cookie Max-Age")
	}
}

func TestCeremonyTamperedSealFailsToOpen(t *testing.T) {
	instanceKey := strings.Repeat("k", 32)
	now := time.Now()
	sealed, err := sealCeremony(instanceKey, Microsoft, ceremony{
		Start: Start{State: "s", Nonce: "n", Verifier: "v"}, Expires: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("sealCeremony: %v", err)
	}
	tampered := sealed[:len(sealed)-4] + "AAAA"
	if _, err := openCeremony(instanceKey, Microsoft, tampered, now); err == nil {
		t.Fatal("a tampered ceremony seal opened")
	}
}
