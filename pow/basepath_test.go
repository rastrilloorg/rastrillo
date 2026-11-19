package pow

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
)

// The module and its worker are fetched by the browser from the page,
// so under a base path they carry it; on a served direct host, where
// the same process answers at the root, they do not.
func TestFormURLsFollowTheRequest(t *testing.T) {
	g := newTestGuard(t, func(c *Config) {
		c.ScriptURL = "/pow/pow.abc.js"
		c.WorkerURL = "/pow/pow-worker.abc.js"
	})
	check := func(where string, r *http.Request, prefix string) {
		t.Helper()
		f := g.Form(t0, "apply").At(r)
		if want := `data-pow-worker="` + prefix + `/pow/pow-worker.abc.js"`; !strings.Contains(string(f.Attrs()), want) {
			t.Errorf("%s: Attrs() = %s, want %s", where, f.Attrs(), want)
		}
		if want := `src="` + prefix + `/pow/pow.abc.js"`; !strings.Contains(string(f.Script()), want) {
			t.Errorf("%s: Script() = %s, want %s", where, f.Script(), want)
		}
	}
	through := func(c basepath.Config, host string) *http.Request {
		var got *http.Request
		req := httptest.NewRequest("GET", "/acme/apply", nil)
		if host != "" {
			req = httptest.NewRequest("GET", "/apply", nil)
			req.Host = host
		}
		c.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r })).ServeHTTP(httptest.NewRecorder(), req)
		if got == nil {
			t.Fatalf("the middleware did not serve %s%s", req.Host, req.URL)
		}
		return got
	}
	acme := basepath.Config{Origin: "https://example.com", Slug: "acme"}
	check("under /acme", through(acme, ""), "/acme")
	direct := acme
	direct.DirectHost = basepath.ServeDirect
	check("on a served direct host", through(direct, "acme-x7k2pq.example.net"), "")
	check("outside the middleware", httptest.NewRequest("GET", "/apply", nil), "")
}
