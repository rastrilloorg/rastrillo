package secondfactor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/internal/tablerows"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func TestSignInRefusesUnderABasePath(t *testing.T) {
	const product = "https://example.com"
	e := newEnv(t)
	var logs bytes.Buffer
	g, err := secondfactor.New(secondfactor.Config{Sessions: e.sess, DB: e.db, Origin: testOrigin, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(e.f)
	e.g = g
	e.f.enrolled["7"] = true
	codes, err := g.RegenerateRecoveryCodes("7")
	if err != nil {
		t.Fatal(err)
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
			_, pendingCookie := hold(t, e, sessions.Session{Subject: "7", Method: "password"}, "")
			pending, ok := g.Pending(withPending(pendingCookie))
			if !ok {
				t.Fatal("no half-session at the root")
			}
			serve := func(name, path string, h http.HandlerFunc, form url.Values) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest("POST", at.prefix+path, strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(pendingCookie)
				before := tablerows.Count(t, e.db)
				w := httptest.NewRecorder()
				at.cfg.Handler(h).ServeHTTP(w, r)
				if cs := w.Result().Cookies(); len(cs) != 0 {
					t.Errorf("%s set %v: on a shared origin that cookie is every tenant's", name, cs)
				}
				tablerows.Unchanged(t, e.db, name, before)
				return w
			}

			// Hold and Complete have no response of their own: they say
			// why, and their caller answers.
			var holdErr, completeErr error
			var done bool
			w := serve("Hold", "/signin", func(w http.ResponseWriter, r *http.Request) {
				done, holdErr = g.Hold(w, r, sessions.Session{Subject: "7", Method: "password"})
			}, url.Values{})
			if done || !errors.Is(holdErr, sessions.ErrPathRouted) || w.Header().Get("Location") != "" {
				t.Errorf("Hold = %v, %v, Location %q; want false, ErrPathRouted, none", done, holdErr, w.Header().Get("Location"))
			}
			serve("Complete", "/signin/confirm", func(w http.ResponseWriter, r *http.Request) {
				completeErr = g.Complete(w, r, pending, "totp")
			}, url.Values{})
			if !errors.Is(completeErr, sessions.ErrPathRouted) {
				t.Errorf("Complete = %v, want ErrPathRouted", completeErr)
			}

			logs.Reset()
			w = serve("SignInRecovery", "/signin/recovery", g.SignInRecovery, url.Values{"code": {codes[0]}})
			if w.Code != http.StatusNotFound {
				t.Errorf("SignInRecovery: %d %q, want 404", w.Code, w.Header().Get("Location"))
			}
			if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/secondfactor") || !strings.Contains(logs.String(), at.base+"/signin/recovery") {
				t.Errorf("SignInRecovery logged %q, want one error naming rastrillo/secondfactor and %s", logs.String(), at.base+"/signin/recovery")
			}
		})
	}
}
