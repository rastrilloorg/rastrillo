package oidc

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
)

func TestSignInRefusesUnderABasePath(t *testing.T) {
	const product = "https://example.com"
	var logs bytes.Buffer
	rig := newBeginRig(t, func(c *Config) {
		c.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})

	for _, at := range []struct {
		name         string
		cfg          basepath.Config
		prefix, base string
	}{
		{"under /acme", basepath.Config{Origin: product, Slug: "acme"}, product + "/acme", "/acme"},
		{"on the direct host", basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}, "http://cv.paulca.com", ""},
	} {
		t.Run(at.name, func(t *testing.T) {
			for _, e := range []struct{ name, path string }{
				{"Begin", "/signin/oidc/microsoft"},
				{"Return", "/signin/oidc/microsoft/return?code=abc&state=" + rig.state},
			} {
				r := httptest.NewRequest("GET", at.prefix+e.path, nil)
				r.AddCookie(rig.cookie)
				logs.Reset()
				w := httptest.NewRecorder()
				at.cfg.Handler(rig.router).ServeHTTP(w, r)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s: %d %q, want 404", e.name, w.Code, w.Header().Get("Location"))
				}
				if cs := w.Result().Cookies(); len(cs) != 0 {
					t.Errorf("%s set %v: on a shared origin that cookie is every tenant's", e.name, cs)
				}
				path, _, _ := strings.Cut(e.path, "?")
				if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/oidc") || !strings.Contains(logs.String(), at.base+path) {
					t.Errorf("%s logged %q, want one error naming rastrillo/oidc and %s", e.name, logs.String(), at.base+path)
				}
			}
		})
	}
}
