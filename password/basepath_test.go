package password_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/internal/tablerows"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func TestSignInRefusesUnderABasePath(t *testing.T) {
	const product = "https://example.com"
	var logs bytes.Buffer
	// Sessions over a database the test can read, to show no session
	// row is written.
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, sessions.Schema); err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.New(sessions.Config{DB: d.Writer(), Origin: "http://app.test"})
	if err != nil {
		t.Fatal(err)
	}
	env := newTestEnv(t, func(c *password.Config) {
		c.Sessions = sess
		c.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	env.sess = sess
	hash, err := password.Hash("s3cretpw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.create(context.Background(), "ana@example.com", hash); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := env.sess.SignIn(w, httptest.NewRequest("POST", "http://app.test/", nil), sessions.Session{Subject: "1"}); err != nil {
		t.Fatal(err)
	}
	session := w.Result().Cookies()[0]

	for _, at := range []struct {
		name         string
		cfg          basepath.Config
		prefix, base string
	}{
		{"under /acme", basepath.Config{Origin: product, Slug: "acme"}, product + "/acme", "/acme"},
		{"on the direct host", basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}, "http://cv.paulca.com", ""},
	} {
		t.Run(at.name, func(t *testing.T) {
			for _, e := range []struct {
				name, method, path string
				h                  http.HandlerFunc
				form               url.Values
			}{
				{"SigninPage", "GET", "/signin", env.h.SigninPage, nil},
				{"SignupPage", "GET", "/signup", env.h.SignupPage, nil},
				{"Signin", "POST", "/signin", env.h.Signin, url.Values{"email": {"ana@example.com"}, "password": {"s3cretpw"}}},
				{"Signup", "POST", "/signup", env.h.Signup, url.Values{"email": {"bea@example.com"}, "password": {"s3cretpw"}}},
				{"Signout", "POST", "/signout", env.h.Signout, url.Values{}},
			} {
				var body io.Reader
				if e.form != nil {
					body = strings.NewReader(e.form.Encode())
				}
				r := httptest.NewRequest(e.method, at.prefix+e.path, body)
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(session)
				logs.Reset()
				creates, pages := env.store.createCallCount(), env.signin.count()+env.signup.count()
				before := tablerows.Count(t, d.Writer())
				w := httptest.NewRecorder()
				at.cfg.Handler(e.h).ServeHTTP(w, r)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s: %d %q, want 404", e.name, w.Code, w.Header().Get("Location"))
				}
				if cs := w.Result().Cookies(); len(cs) != 0 {
					t.Errorf("%s set %v: on a shared origin that cookie is every tenant's", e.name, cs)
				}
				tablerows.Unchanged(t, d.Writer(), e.name, before)
				if n := env.store.createCallCount(); n != creates {
					t.Errorf("%s created %d users", e.name, n-creates)
				}
				if n := env.signin.count() + env.signup.count(); n != pages {
					t.Errorf("%s rendered %d pages", e.name, n-pages)
				}
				if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/password") || !strings.Contains(logs.String(), at.base+e.path) {
					t.Errorf("%s logged %q, want one error naming rastrillo/password and %s", e.name, logs.String(), at.base+e.path)
				}
			}
		})
	}
}
