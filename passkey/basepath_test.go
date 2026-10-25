package passkey_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/internal/tablerows"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

func TestSignInRefusesUnderABasePath(t *testing.T) {
	const product = "https://example.com"
	var logs bytes.Buffer
	e := newEnvWith(t, func(c *passkey.Config) { c.Logger = slog.New(slog.NewTextHandler(&logs, nil)) })
	a, _ := authtest.New()
	session := enroll(t, e, e.signIn(t, "alice"), a)
	// held is a fresh half-session, held at the root.
	held := func(t *testing.T) *http.Cookie {
		w := httptest.NewRecorder()
		if done, err := e.g.Hold(w, httptest.NewRequest("POST", testOrigin+"/signin", nil), sessions.Session{Subject: "alice", Method: "magiclink"}); err != nil || !done {
			t.Fatalf("Hold: %v, %v", done, err)
		}
		return w.Result().Cookies()[0]
	}

	// Each finish gets a ceremony that would pass at the root, against
	// a challenge minted there.
	assertion := func(t *testing.T, begin http.HandlerFunc, cookie *http.Cookie) map[string]string {
		clientData, authData, sig, err := a.Get(challengeFrom(t, postJSON(t, begin, cookie, nil)), authtest.Options{RPID: testRPID, Origin: testOrigin})
		if err != nil {
			t.Fatal(err)
		}
		return map[string]string{"id": b64(a.CredID), "clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(sig)}
	}
	creation := func(t *testing.T) map[string]string {
		other, _ := authtest.New()
		clientData, attestation := other.Create(challengeFrom(t, postJSON(t, e.h.RegisterBegin, session, nil)), authtest.Options{RPID: testRPID, Origin: testOrigin})
		return map[string]string{"clientDataJSON": b64(clientData), "attestationObject": b64(attestation)}
	}

	for _, at := range []struct {
		name         string
		cfg          basepath.Config
		prefix, base string
	}{
		{"under /acme", basepath.Config{Origin: product, Slug: "acme"}, product + "/acme", "/acme"},
		{"on the direct host", basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}, "http://cv.paulca.com", ""},
	} {
		t.Run(at.name, func(t *testing.T) {
			pending := held(t)
			for _, c := range []struct {
				name, path string
				h          http.HandlerFunc
				cookie     *http.Cookie
				body       func(*testing.T) map[string]string
			}{
				{"SignInBegin", "/passkey/signin/begin", e.h.SignInBegin, pending, nil},
				{"SignInFinish", "/passkey/signin/finish", e.h.SignInFinish, pending, func(t *testing.T) map[string]string { return assertion(t, e.h.SignInBegin, pending) }},
				{"DiscoverBegin", "/passkey/discover/begin", e.h.DiscoverBegin, nil, nil},
				{"DiscoverFinish", "/passkey/discover/finish", e.h.DiscoverFinish, nil, func(t *testing.T) map[string]string { return assertion(t, e.h.DiscoverBegin, nil) }},
				{"RegisterBegin", "/passkey/register/begin", e.h.RegisterBegin, session, nil},
				{"RegisterFinish", "/passkey/register/finish", e.h.RegisterFinish, session, creation},
				{"StepUpBegin", "/passkey/stepup/begin", e.h.StepUpBegin, session, nil},
				{"StepUpFinish", "/passkey/stepup/finish", e.h.StepUpFinish, session, func(t *testing.T) map[string]string { return assertion(t, e.h.StepUpBegin, session) }},
			} {
				var body bytes.Buffer
				if c.body != nil {
					if err := json.NewEncoder(&body).Encode(c.body(t)); err != nil {
						t.Fatal(err)
					}
				}
				r := httptest.NewRequest("POST", at.prefix+c.path, &body)
				if c.cookie != nil {
					r.AddCookie(c.cookie)
				}
				logs.Reset()
				before := tablerows.Count(t, e.db)
				w := httptest.NewRecorder()
				at.cfg.Handler(c.h).ServeHTTP(w, r)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s: %d %s, want 404", c.name, w.Code, w.Body.String())
				}
				if cs := w.Result().Cookies(); len(cs) != 0 {
					t.Errorf("%s set %v: on a shared origin that cookie is every tenant's", c.name, cs)
				}
				tablerows.Unchanged(t, e.db, c.name, before)
				if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/passkey") || !strings.Contains(logs.String(), at.base+c.path) {
					t.Errorf("%s logged %q, want one error naming rastrillo/passkey and %s", c.name, logs.String(), at.base+c.path)
				}
			}
		})
	}
}
