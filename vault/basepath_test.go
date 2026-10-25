package vault_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/vault"
)

func TestSignInRefusesUnderABasePath(t *testing.T) {
	const product = "https://example.com"
	// Handoff has no logger of its own, so it logs to the default.
	var logs bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

	s, d := testSessions(t)
	h := vault.Handoff{Sessions: s, Home: "https://home.test", Origin: product}
	token, err := s.Mint(sessions.Session{Subject: "7", Method: "vault"})
	if err != nil {
		t.Fatal(err)
	}
	// Enrol behind a guard that admitted someone, as a signed tenant's
	// Require would under its base path.
	enrol := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Enrol(w, sessions.WithSession(r, sessions.Session{Subject: "7", Method: "password"}))
	})
	rows := func() int {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	for _, at := range []struct {
		name         string
		cfg          basepath.Config
		prefix, base string
	}{
		{"under /acme", basepath.Config{Origin: product, Slug: "acme"}, product + "/acme", "/acme"},
		{"on the direct host", basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}, "http://cv.paulca.com", ""},
	} {
		for _, c := range []struct {
			name, path string
			h          http.Handler
			form       url.Values
		}{
			{"Enrol", "/vault/enrol", enrol, url.Values{}},
			{"Restore", "/vault/restore", http.HandlerFunc(h.Restore), url.Values{"token": {token}, "return_to": {"/notes"}}},
		} {
			r := httptest.NewRequest("POST", at.prefix+c.path, strings.NewReader(c.form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			logs.Reset()
			before := rows()
			w := httptest.NewRecorder()
			at.cfg.Handler(c.h).ServeHTTP(w, r)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s, %s: %d %q, want 404", at.name, c.name, w.Code, w.Header().Get("Location"))
			}
			if cs := w.Result().Cookies(); len(cs) != 0 {
				t.Errorf("%s, %s set %v: on a shared origin that cookie is every tenant's", at.name, c.name, cs)
			}
			if n := rows(); n != before {
				t.Errorf("%s, %s: %d session rows, want %d", at.name, c.name, n, before)
			}
			if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/vault") || !strings.Contains(logs.String(), at.base+c.path) {
				t.Errorf("%s, %s logged %q, want one error naming rastrillo/vault and %s", at.name, c.name, logs.String(), at.base+c.path)
			}
		}
	}
}
