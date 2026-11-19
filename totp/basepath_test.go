package totp_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/tablerows"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/totp"
)

func TestSignInRefusesUnderABasePath(t *testing.T) {
	const product = "https://example.com"
	e := newEnv(t)
	var logs bytes.Buffer
	h, err := totp.New(totp.Config{DB: e.db, Sessions: e.sess, Gate: e.g, Key: crypto.Derive([]byte("instance"), "rastrillo/totp"), Issuer: "Acme",
		Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	key := enrol(t, e, "7")
	session := signIn(t, e, "7", time.Now().Add(-time.Hour))

	for _, at := range []struct {
		name         string
		cfg          basepath.Config
		prefix, base string
	}{
		{"under /acme", basepath.Config{Origin: product, Slug: "acme"}, product + "/acme", "/acme"},
		{"on the direct host", basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}, "http://cv.paulca.com", ""},
	} {
		t.Run(at.name, func(t *testing.T) {
			pending := hold(t, e, sessions.Session{Subject: "7", Method: "password"}, "")
			code, err := totp.Code(key, time.Now().Add(period))
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range []struct {
				name, path string
				h          http.HandlerFunc
				cookie     *http.Cookie
			}{
				{"SignIn", "/totp/signin", h.SignIn, pending},
				{"StepUp", "/totp/stepup", h.StepUp, session},
			} {
				r := httptest.NewRequest("POST", at.prefix+c.path, strings.NewReader(url.Values{"code": {code}}.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(c.cookie)
				logs.Reset()
				before := tablerows.Count(t, e.db)
				w := httptest.NewRecorder()
				at.cfg.Handler(c.h).ServeHTTP(w, r)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s: %d %q, want 404", c.name, w.Code, w.Header().Get("Location"))
				}
				if cs := w.Result().Cookies(); len(cs) != 0 {
					t.Errorf("%s set %v: on a shared origin that cookie is every tenant's", c.name, cs)
				}
				tablerows.Unchanged(t, e.db, c.name, before)
				if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/totp") || !strings.Contains(logs.String(), at.base+c.path) {
					t.Errorf("%s logged %q, want one error naming rastrillo/totp and %s", c.name, logs.String(), at.base+c.path)
				}
			}
		})
	}
}
