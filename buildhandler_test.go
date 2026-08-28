package rastrillo

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// captured records what the app mux's handler saw for a request that
// reached it: the path after any locale-prefix strip, the locale
// Middleware resolved (empty when no locale middleware ran), and a
// translation lookup — the three things buildHandler's assembly can
// change without a live socket. translatedBase is a second, independent
// translation lookup slot some tests use for a key the base catalog (not
// an app catalog) resolves — kept separate from translated so a test can
// assert on both an app-catalog override and an unrelated base-layer hit
// from the same request.
type captured struct {
	path, locale, translated, translatedBase string
}

// helloMux is an app mux with one route, /hello, whose handler stashes
// what it saw into cap.
func helloMux(cap *captured) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.locale = LocaleFrom(r)
		cap.translated = T(r, "greeting")
		fmt.Fprint(w, "hi")
	})
	return mux
}

// get runs a GET against h and returns the recorder, so tests can assert
// on both the app-visible capture and the framework's own response body.
func get(h http.Handler, path, acceptLanguage string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// frCatalog is the MapFS the brief specifies: a French catalog with one
// key, no English catalog — so English-locale lookups exercise the
// key-verbatim fallback while French exercises a real catalog hit.
var frCatalog = fstest.MapFS{
	"locales/fr.toml": &fstest.MapFile{Data: []byte("greeting = \"Bonjour\"\n")},
}

func TestBuildHandlerLocaleAssembly(t *testing.T) {
	tests := []struct {
		name           string
		locales        []string
		defaultLocale  string
		fsys           fstest.MapFS
		path           string
		acceptLanguage string
		wantPath       string
		wantLocale     string
		wantTranslated string
	}{
		{
			name:           "no locales: app sees the request untouched",
			path:           "/hello",
			wantPath:       "/hello",
			wantLocale:     "",
			wantTranslated: "greeting",
		},
		{
			name:           "prefix strip and catalog hit",
			locales:        []string{"en", "fr"},
			defaultLocale:  "en",
			fsys:           frCatalog,
			path:           "/fr/hello",
			wantPath:       "/hello",
			wantLocale:     "fr",
			wantTranslated: "Bonjour",
		},
		{
			name:           "Accept-Language negotiation, no prefix",
			locales:        []string{"en", "fr"},
			defaultLocale:  "en",
			fsys:           frCatalog,
			path:           "/hello",
			acceptLanguage: "fr-CA, en;q=0.5",
			wantPath:       "/hello",
			wantLocale:     "fr",
			wantTranslated: "Bonjour",
		},
		{
			name:           "default locale fallback, no catalog for it",
			locales:        []string{"en", "fr"},
			defaultLocale:  "en",
			fsys:           frCatalog,
			path:           "/hello",
			wantPath:       "/hello",
			wantLocale:     "en",
			wantTranslated: "greeting",
		},
		{
			name:           "empty DefaultLocale defaults to Locales[0]",
			locales:        []string{"fr"},
			fsys:           frCatalog,
			path:           "/hello",
			wantPath:       "/hello",
			wantLocale:     "fr",
			wantTranslated: "Bonjour",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cap captured
			opts := Options{
				Mux:           helloMux(&cap),
				Locales:       tt.locales,
				DefaultLocale: tt.defaultLocale,
				LocaleFS:      tt.fsys,
			}
			h, err := buildHandler(opts)
			if err != nil {
				t.Fatalf("buildHandler: %v", err)
			}
			get(h, tt.path, tt.acceptLanguage)
			if cap.path != tt.wantPath {
				t.Errorf("path = %q, want %q", cap.path, tt.wantPath)
			}
			if cap.locale != tt.wantLocale {
				t.Errorf("locale = %q, want %q", cap.locale, tt.wantLocale)
			}
			if cap.translated != tt.wantTranslated {
				t.Errorf("T = %q, want %q", cap.translated, tt.wantTranslated)
			}
		})
	}
}

// TestBuildHandlerHealthzUnaffectedByLocales covers the framework's own
// endpoints, both with no locale middleware installed and with one
// installed and a locale prefix present — /healthz must answer "ok"
// either way, and the prefix must strip before the framework mux sees
// the path, not just before the app mux does.
func TestBuildHandlerHealthzUnaffectedByLocales(t *testing.T) {
	tests := []struct {
		name    string
		locales []string
		path    string
	}{
		{"no locales", nil, "/healthz"},
		{"locales installed, no prefix", []string{"en", "fr"}, "/healthz"},
		{"locales installed, prefix strips first", []string{"en", "fr"}, "/fr/healthz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{
				Mux:           helloMux(&captured{}),
				Locales:       tt.locales,
				DefaultLocale: "en",
			}
			h, err := buildHandler(opts)
			if err != nil {
				t.Fatalf("buildHandler: %v", err)
			}
			rec := get(h, tt.path, "")
			if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
				t.Errorf("GET %s = %d %q, want 200 \"ok\"", tt.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestBuildHandlerNoLocalesNeverErrors documents the additive-only
// constraint from the other direction: Options.Locales unset must
// never reach NewLocales, so a nil LocaleFS or an empty DefaultLocale
// can never surface an error when no locale codes are declared.
func TestBuildHandlerNoLocalesNeverErrors(t *testing.T) {
	h, err := buildHandler(Options{Mux: helloMux(&captured{})})
	if err != nil {
		t.Fatalf("buildHandler with no Locales: %v", err)
	}
	if h == nil {
		t.Fatal("buildHandler returned a nil handler with no error")
	}
}

// TestBuildHandlerOverridesFrameworkBaseCatalog covers the layering order
// buildHandler now wires (Step 2): an app catalog entry for a
// rastrillo.ui.* key must win over the framework base layer BaseCatalog()
// supplies, through the same public request path an action uses —
// T(r, key) — not through Locales.T directly. It also covers a key the
// app catalog does NOT override, which must still resolve through the
// base layer rather than falling all the way back to the key itself —
// this is the assertion that actually pins buildHandler's
// NewLocales(..., BaseCatalog(), ...) wiring down: reverting that call's
// third argument back to nil leaves the override assertion passing (an
// app catalog entry always wins regardless of what sits under it) but
// fails this one, since with a nil base layer "rastrillo.ui.pagination"
// has nothing to fall back to but the key itself.
func TestBuildHandlerOverridesFrameworkBaseCatalog(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/en.toml": &fstest.MapFile{Data: []byte(`rastrillo.ui.cancel = "Never mind"` + "\n")},
	}
	var cap captured
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		cap.translated = T(r, "rastrillo.ui.cancel")
		cap.translatedBase = T(r, "rastrillo.ui.pagination")
		fmt.Fprint(w, "hi")
	})
	h, err := buildHandler(Options{
		Mux:      mux,
		Locales:  []string{"en"},
		LocaleFS: fsys,
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	get(h, "/hello", "")
	if cap.translated != "Never mind" {
		t.Errorf("app catalog override lost to the framework base layer: got %q, want %q", cap.translated, "Never mind")
	}
	if cap.translatedBase != "Pagination" {
		t.Errorf("base layer not wired into buildHandler: T(r, %q) = %q, want %q (nil base would yield the key itself)", "rastrillo.ui.pagination", cap.translatedBase, "Pagination")
	}
}

// TestBuildHandlerPropagatesLocaleErrors covers a malformed catalog file:
// buildHandler must surface NewLocales' error, and Serve's caller must
// be able to trust that message is not double-prefixed. NewLocales
// already wraps its errors as "rastrillo: ...", so buildHandler must
// return it as-is rather than wrapping a second time.
func TestBuildHandlerPropagatesLocaleErrors(t *testing.T) {
	badFS := fstest.MapFS{
		"locales/en.toml": &fstest.MapFile{Data: []byte("bad line\n")},
	}
	_, err := buildHandler(Options{
		Mux:      helloMux(&captured{}),
		Locales:  []string{"en"},
		LocaleFS: badFS,
	})
	if err == nil {
		t.Fatal("expected an error for a malformed catalog file")
	}
	if !strings.Contains(err.Error(), "locales/en.toml") {
		t.Errorf("error %q does not mention the offending file", err.Error())
	}
	if n := strings.Count(err.Error(), "rastrillo:"); n != 1 {
		t.Errorf("error %q has %d \"rastrillo:\" prefixes, want exactly 1", err.Error(), n)
	}
}

// The baseline security headers ride every response — the app's routes
// and the framework's own endpoints alike — because the wrapper is the
// outermost layer of the assembly.
func TestBuildHandlerSetsSecurityHeaders(t *testing.T) {
	h, err := buildHandler(Options{Mux: helloMux(&captured{})})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	for _, path := range []string{"/hello", "/healthz"} {
		rec := get(h, path, "")
		hd := rec.Header()
		if got := hd.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
			t.Errorf("%s: Content-Security-Policy = %q, want the default-src 'self' policy", path, got)
		}
		if got := hd.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if got := hd.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q, want DENY", path, got)
		}
		if got := hd.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
			t.Errorf("%s: Referrer-Policy = %q, want strict-origin-when-cross-origin", path, got)
		}
	}
}

// Options.CSP replaces the policy string wholesale; the other headers
// stay.
func TestBuildHandlerCSPOverride(t *testing.T) {
	h, err := buildHandler(Options{Mux: helloMux(&captured{}), CSP: "default-src 'none'"})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := get(h, "/hello", "")
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("Content-Security-Policy = %q, want the override verbatim", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff (override must not clear it)", got)
	}
}

// The wrapper sets headers before the app runs, so an app that sets its
// own value — here through Options.Wrap, the documented middleware seam
// — simply wins. No off-switch needed.
func TestBuildHandlerAppHeaderWins(t *testing.T) {
	h, err := buildHandler(Options{
		Mux: helloMux(&captured{}),
		Wrap: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src *")
				w.Header().Del("X-Frame-Options")
				next.ServeHTTP(w, r)
			})
		},
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := get(h, "/hello", "")
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'; img-src *" {
		t.Errorf("Content-Security-Policy = %q, want the app's own value", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("X-Frame-Options = %q, want removed by the app", got)
	}
}

// panicMux is an app mux whose /boom route panics with v, and whose
// /hello route is the ordinary one — so a test can prove the process
// (and the handler) survive the panic and keep serving.
func panicMux(v any) *http.ServeMux {
	mux := helloMux(&captured{})
	mux.HandleFunc("/boom", func(http.ResponseWriter, *http.Request) { panic(v) })
	return mux
}

// A panicking handler is a 500 with the app's own error page, a logged
// stack, and a reference joining the two — not a dropped connection.
func TestBuildHandlerRecoversPanicsThroughErrorPage(t *testing.T) {
	var buf bytes.Buffer
	var gotStatus int
	var gotRef, gotPath string
	h, err := buildHandler(Options{
		Mux:    panicMux("kaboom"),
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
		ErrorPage: func(w http.ResponseWriter, r *http.Request, status int, ref string) {
			gotStatus, gotRef, gotPath = status, ref, r.URL.Path
			w.WriteHeader(status)
			fmt.Fprintf(w, "our own 500 page, reference %s", ref)
		},
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	rec := get(h, "/boom", "")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if gotStatus != http.StatusInternalServerError || gotPath != "/boom" {
		t.Errorf("ErrorPage got (%d, %q), want (500, \"/boom\")", gotStatus, gotPath)
	}
	if !regexp.MustCompile(`^[a-z2-7]{6}$`).MatchString(gotRef) {
		t.Errorf("ErrorPage ref = %q, want a NewRef", gotRef)
	}
	if !strings.Contains(rec.Body.String(), gotRef) {
		t.Errorf("body %q does not carry the reference %q", rec.Body.String(), gotRef)
	}
	// The header wrapper runs outside the app, so a recovered response
	// is still a hardened one.
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q on a recovered response", got)
	}
	// One log line, carrying everything an operator needs to find it:
	// the reference the user can quote, the panic value, and the stack.
	out := buf.String()
	for _, want := range []string{"panic", gotRef, "kaboom", "buildhandler_test.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("log line is missing %q:\n%s", want, out)
		}
	}
	// And the handler keeps serving.
	if again := get(h, "/hello", ""); again.Code != http.StatusOK {
		t.Errorf("after a panic, /hello = %d, want 200", again.Code)
	}
}

// With no ErrorPage wired — the default for every app that has not
// thought about it yet — recovery still turns the panic into a plain
// 500 rather than an empty response.
func TestBuildHandlerRecoversPanicsWithoutAnErrorPage(t *testing.T) {
	h, err := buildHandler(Options{Mux: panicMux(errors.New("kaboom"))})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := get(h, "/boom", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "Something went wrong." {
		t.Errorf("body = %q, want the plain 500", got)
	}
}

// http.ErrAbortHandler is net/http's own "stop, quietly" signal — the
// server suppresses its stack and closes the connection. Swallowing it
// here would turn every aborted stream into a bogus 500 page appended
// to a half-written response, so it goes straight back up.
func TestBuildHandlerRepanicsErrAbortHandler(t *testing.T) {
	h, err := buildHandler(Options{
		Mux:       panicMux(http.ErrAbortHandler),
		ErrorPage: func(http.ResponseWriter, *http.Request, int, string) { t.Error("ErrorPage ran for ErrAbortHandler") },
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler back", v)
		}
	}()
	get(h, "/boom", "")
	t.Error("ErrAbortHandler did not propagate")
}
