package rastrillo

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/carlos"
)

// bpCalls records what the app's own handlers saw.
type bpCalls struct {
	notes, theme, catchAll int
	items                  []LocaleItem
	prefix                 string
	errBase                string
}

// bpMux is the app every base-path test serves.
func bpMux(c *bpCalls) *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		c.items, c.prefix = LocaleItems(r), LocalePrefix(r)
		io.WriteString(w, "index")
	})
	m.HandleFunc("GET /files/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "files") })
	m.HandleFunc("GET /go", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/done", http.StatusSeeOther) })
	m.HandleFunc("GET /rel/x", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "y", http.StatusSeeOther) })
	m.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		c.notes++
		w.WriteHeader(http.StatusCreated)
	})
	m.HandleFunc("PUT /notes", func(w http.ResponseWriter, r *http.Request) { c.notes++ })
	m.HandleFunc("PATCH /notes", func(w http.ResponseWriter, r *http.Request) { c.notes++ })
	m.HandleFunc("DELETE /notes", func(w http.ResponseWriter, r *http.Request) { c.notes++ })
	m.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	return m
}

// bpOptions is the base-path setup the tests share: example.com/acme,
// two locales, the given direct-host mode.
func bpOptions(m *http.ServeMux, c *bpCalls, mode basepath.DirectHost) Options {
	return Options{
		Mux:     m,
		Locales: []string{"en", "fr"},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		ErrorPage: func(w http.ResponseWriter, r *http.Request, status int, _ string) {
			c.errBase = basepath.Base(r)
			w.WriteHeader(status)
		},
		BasePath: basepath.Config{Origin: "https://example.com", Slug: "acme", DirectHost: mode},
	}
}

// bpServe builds the serving handler. buildHandler rather than Handler,
// because this binary links only the en pack (packs_test.go), so
// Handler's locale check would refuse fr; the assembly under test is
// the same one Handler returns.
func bpServe(t *testing.T, opts Options) http.Handler {
	t.Helper()
	h, err := buildHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// bpDo sends method target on host. A POST carries form as its body and
// Sec-Fetch-Site: same-origin, as a page of ours would send it.
func bpDo(h http.Handler, method, host, target string, form url.Values, header http.Header) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, target, body)
	r.Host = host
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for k, vs := range header {
		r.Header[k] = vs
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFrameworkRoutesAnswerUnderTheBase(t *testing.T) {
	t.Setenv("CARLOS_HOST", "acme-abc234.bdf.oncarlos.com")
	c := &bpCalls{}
	opts := bpOptions(bpMux(c), c, basepath.RedirectDirect)
	opts.NameInstance = true
	h := bpServe(t, opts)

	if w := bpDo(h, "GET", "example.com", "/acme/healthz", nil, nil); w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("GET /acme/healthz = %d %q, want 200 ok: a probe on the product host cannot reach the instance", w.Code, w.Body.String())
	}
	w := bpDo(h, "GET", "example.com", "/acme/api/version", nil, nil)
	if w.Code != 200 || w.Body.String() != BuildVersion {
		t.Errorf("GET /acme/api/version = %d %q, want 200 %q", w.Code, w.Body.String(), BuildVersion)
	}
	if got := w.Header().Get(carlos.HostHeader); got != "acme-abc234.bdf.oncarlos.com" {
		t.Errorf("%s = %q under the base: provision.CheckRoute reads Routed only from it, so every tenant would stay RoutePending", carlos.HostHeader, got)
	}
	w = bpDo(h, "GET", "example.com", "/acme/_speculation-rules", nil, nil)
	if ct := w.Header().Get("Content-Type"); w.Code != 200 || ct != "application/speculationrules+json" {
		t.Errorf("GET /acme/_speculation-rules = %d %q, want 200 application/speculationrules+json", w.Code, ct)
	}
	if got := bpDo(h, "GET", "example.com", "/acme", nil, nil).Header().Get("Speculation-Rules"); got != `"/acme/_speculation-rules"` {
		t.Errorf("Speculation-Rules = %s, want \"/acme/_speculation-rules\": the browser would fetch the hub's path", got)
	}
	for _, p := range []string{"/healthz", "/api/version"} {
		if w := bpDo(h, "GET", "example.com", p, nil, nil); w.Code != http.StatusNotFound {
			t.Errorf("GET %s on the product host = %d, want 404: that path is the hub's, not this tenant's", p, w.Code)
		}
	}
}

func TestRedirectsTheAppDidNotWriteStayUnderTheBase(t *testing.T) {
	c := &bpCalls{}
	h := bpServe(t, bpOptions(bpMux(c), c, basepath.RedirectDirect))
	for target, want := range map[string]string{
		"/acme/files": "/acme/files/", // ServeMux's trailing-slash 301
		"/acme/go":    "/acme/done",   // the app's own root-relative redirect
		"/acme/rel/x": "/acme/rel/y",  // http.Redirect resolving a relative target
	} {
		if got := bpDo(h, "GET", "example.com", target, nil, nil).Header().Get("Location"); got != want {
			t.Errorf("GET %s: Location = %q, want %q", target, got, want)
		}
	}
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/acme/_locale", url.Values{"locale": {"fr"}, "return": {"/files/"}}},
		{"/acme/_theme", url.Values{"scheme": {"dark"}, "return": {"/fr/files/"}}},
	} {
		w := bpDo(h, "POST", "example.com", tc.path, tc.form, nil)
		if loc := w.Header().Get("Location"); w.Code != http.StatusSeeOther || loc != "/acme/fr/files/" {
			t.Errorf("POST %s = %d %q, want 303 /acme/fr/files/", tc.path, w.Code, loc)
		}
	}
}

func TestLocaleItemsCarryTheBase(t *testing.T) {
	c := &bpCalls{}
	h := bpServe(t, bpOptions(bpMux(c), c, basepath.RedirectDirect))
	if w := bpDo(h, "GET", "example.com", "/acme/fr/", nil, nil); w.Code != 200 {
		t.Fatalf("GET /acme/fr/ = %d", w.Code)
	}
	var hrefs []string
	for _, it := range c.items {
		hrefs = append(hrefs, it.Href)
	}
	if strings.Join(hrefs, " ") != "/acme/en/ /acme/fr/" {
		t.Errorf("LocaleItems hrefs = %v, want [/acme/en/ /acme/fr/]: the switcher's links would land on the hub", hrefs)
	}
	if c.prefix != "/fr" {
		t.Errorf("LocalePrefix = %q, want /fr: it is an app path, and the middleware adds the base to a Location built from it", c.prefix)
	}
}

func TestAPanicUnderTheBaseRendersWithTheBase(t *testing.T) {
	c := &bpCalls{}
	h := bpServe(t, bpOptions(bpMux(c), c, basepath.RedirectDirect))
	if w := bpDo(h, "GET", "example.com", "/acme/boom", nil, nil); w.Code != 500 {
		t.Fatalf("GET /acme/boom = %d, want 500", w.Code)
	}
	if c.errBase != "/acme" {
		t.Errorf("ErrorPage saw base %q, want /acme: the panic page's links would leave the tenant", c.errBase)
	}
}

func TestPlatformRoutesAnswerOnTheDirectHost(t *testing.T) {
	t.Setenv("CARLOS_ADMIN_TOKEN", "tok")
	c := &bpCalls{}
	opts := bpOptions(bpMux(c), c, basepath.RedirectDirect)
	opts.NextDue = func() time.Time { return time.Time{} }
	h := bpServe(t, opts)
	bearer := http.Header{"Authorization": {"Bearer tok"}}

	for _, tc := range []struct {
		host, path string
		header     http.Header
		body       string
	}{
		{"carlos", "/", nil, "index"},
		{"carlos", "/healthz", nil, "ok"},
		{"carlos", "/api/version", nil, BuildVersion},
		{"carlos", "/api/next-due", bearer, `{"due":0}` + "\n"},
		{"acme-abc234.bdf.oncarlos.com", "/healthz", nil, "ok"},
		{"acme-abc234.bdf.oncarlos.com", "/api/version", nil, BuildVersion},
	} {
		w := bpDo(h, "GET", tc.host, tc.path, nil, tc.header)
		if w.Code != 200 || w.Body.String() != tc.body {
			t.Errorf("GET %s on Host %s = %d %q, want 200 %q: a redirected platform call fails a wake or a deploy", tc.path, tc.host, w.Code, w.Body.String(), tc.body)
		}
	}
	w := bpDo(h, "GET", "acme-abc234.bdf.oncarlos.com", "/files/", nil, nil)
	if loc := w.Header().Get("Location"); w.Code != http.StatusTemporaryRedirect || loc != "https://example.com/acme/files/" {
		t.Errorf("GET /files/ on the instance host = %d %q, want 307 https://example.com/acme/files/", w.Code, loc)
	}
}

func TestAServedDirectHostIsReadOnly(t *testing.T) {
	t.Setenv("CARLOS_ADMIN_TOKEN", "tok")
	const direct = "cv.paulca.com"
	c := &bpCalls{}
	h := bpServe(t, bpOptions(bpMux(c), c, basepath.ServeDirect))

	w := bpDo(h, "POST", direct, "/notes", url.Values{}, nil)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /notes on the direct host = %d Allow %q, want 405 Allow \"GET, HEAD\": an unscoped action writes with no session", w.Code, w.Header().Get("Allow"))
	}
	for _, m := range []string{"PUT", "PATCH", "DELETE"} {
		if w := bpDo(h, m, direct, "/notes", url.Values{}, nil); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /notes on the direct host = %d, want 405", m, w.Code)
		}
	}
	if c.notes != 0 {
		t.Errorf("the app's /notes handler ran %d times on the read-only direct host", c.notes)
	}

	if w := bpDo(h, "POST", direct, "/_locale", url.Values{"locale": {"fr"}, "return": {"/"}}, nil); w.Code != http.StatusSeeOther {
		t.Errorf("POST /_locale on the direct host = %d, want 303: the reader's language is not a write", w.Code)
	}
	if w := bpDo(h, "POST", direct, "/_theme", url.Values{"scheme": {"dark"}, "return": {"/"}}, nil); w.Code != http.StatusSeeOther {
		t.Errorf("POST /_theme on the direct host = %d, want 303: the reader's colours are not a write", w.Code)
	}

	if w := bpDo(h, "POST", direct, "/notes", url.Values{}, http.Header{"Authorization": {"Bearer tok"}}); w.Code != http.StatusCreated {
		t.Errorf("a scheduled tick POST /notes on the direct host = %d, want 201", w.Code)
	}
	if w := bpDo(h, "POST", direct, "/notes", url.Values{}, http.Header{"Authorization": {"Bearer user-token"}}); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("an API client's POST /notes on the direct host = %d, want 405: the served view is read-only for every client", w.Code)
	}
	if w := bpDo(h, "POST", "carlos", "/notes", url.Values{}, nil); w.Code != http.StatusCreated {
		t.Errorf("POST /notes on Host carlos = %d, want 201: the platform's socket call is not a visitor", w.Code)
	}
	if w := bpDo(h, "POST", "example.com", "/acme/notes", url.Values{}, nil); w.Code != http.StatusCreated {
		t.Errorf("POST /acme/notes on the product path = %d, want 201", w.Code)
	}

	t.Run("an app's own /_theme", func(t *testing.T) {
		c := &bpCalls{}
		m := bpMux(c)
		m.HandleFunc("POST /_theme", func(w http.ResponseWriter, r *http.Request) { c.theme++ })
		h := bpServe(t, bpOptions(m, c, basepath.ServeDirect))
		if w := bpDo(h, "POST", direct, "/_theme", url.Values{"scheme": {"dark"}}, nil); w.Code != http.StatusMethodNotAllowed || c.theme != 0 {
			t.Errorf("POST /_theme to the app's own handler on the direct host = %d (handler ran %d times), want 405 and not run", w.Code, c.theme)
		}
	})
	t.Run("NoSchemeSwitch", func(t *testing.T) {
		c := &bpCalls{}
		m := http.NewServeMux()
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { c.catchAll++ })
		opts := bpOptions(m, c, basepath.ServeDirect)
		opts.NoSchemeSwitch = true
		h := bpServe(t, opts)
		if w := bpDo(h, "POST", direct, "/_theme", url.Values{"scheme": {"dark"}}, nil); w.Code != http.StatusMethodNotAllowed || c.catchAll != 0 {
			t.Errorf("POST /_theme to the app's catch-all on the direct host = %d (catch-all ran %d times), want 405 and not run", w.Code, c.catchAll)
		}
	})
}

func TestAnInvalidBasePathRefusesToStart(t *testing.T) {
	_, _, err := Handler(Options{Mux: http.NewServeMux(), BasePath: basepath.Config{Slug: "acme"}})
	if err == nil || !strings.HasPrefix(err.Error(), "rastrillo: refusing to start: rastrillo/basepath:") {
		t.Fatalf("Handler with a slug and no origin: err = %v, want rastrillo: refusing to start: rastrillo/basepath: ...", err)
	}
}

func TestNoBasePathChangesNothing(t *testing.T) {
	c := &bpCalls{}
	opts := bpOptions(bpMux(c), c, basepath.RedirectDirect)
	opts.BasePath = basepath.Config{}
	h := bpServe(t, opts)
	if got := bpDo(h, "GET", "example.com", "/files", nil, nil).Header().Get("Location"); got != "/files/" {
		t.Errorf("GET /files: Location = %q, want /files/", got)
	}
	w := bpDo(h, "GET", "example.com", "/fr/", nil, nil)
	if got := w.Header().Get("Speculation-Rules"); got != `"/_speculation-rules"` {
		t.Errorf("Speculation-Rules = %s, want \"/_speculation-rules\"", got)
	}
	var hrefs []string
	for _, it := range c.items {
		hrefs = append(hrefs, it.Href)
	}
	if strings.Join(hrefs, " ") != "/en/ /fr/" {
		t.Errorf("LocaleItems hrefs = %v, want [/en/ /fr/]", hrefs)
	}
	if w := bpDo(h, "POST", "example.com", "/notes", url.Values{}, nil); w.Code != http.StatusCreated {
		t.Errorf("POST /notes = %d, want 201", w.Code)
	}
}
