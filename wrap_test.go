package rastrillo

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// markerWrap tags every response that traversed the middleware, and
// refuses requests to paths containing "forbidden" without calling
// next — the short-circuit shape sessions/auth middleware actually
// have.
func markerWrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Wrapped", "yes")
		if strings.Contains(r.URL.Path, "forbidden") {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestWrapObservesAppRequests(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("orders"))
	})
	handler, err := buildHandler(Options{Mux: mux, Wrap: markerWrap})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/orders", nil))
	if rec.Header().Get("X-Wrapped") != "yes" {
		t.Error("app route response missing middleware marker")
	}
	if rec.Body.String() != "orders" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "orders")
	}
}

func TestWrapShortCircuitSkipsAppHandler(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/forbidden/thing", func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler, err := buildHandler(Options{Mux: mux, Wrap: markerWrap})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/forbidden/thing", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if called {
		t.Error("app handler ran despite middleware short-circuit")
	}
}

func TestWrapNeverTouchesFrameworkChrome(t *testing.T) {
	handler, err := buildHandler(Options{Mux: http.NewServeMux(), Wrap: markerWrap})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	for _, path := range []string{"/healthz", "/api/version"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Header().Get("X-Wrapped") == "yes" {
			t.Errorf("%s traversed app middleware; platform probes must not", path)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestWrapReturningNilIsABootError(t *testing.T) {
	_, err := buildHandler(Options{
		Mux:  http.NewServeMux(),
		Wrap: func(http.Handler) http.Handler { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "Options.Wrap returned a nil handler") {
		t.Errorf("err = %v, want nil-handler boot error", err)
	}
}

func TestWrapComposesWithRouter(t *testing.T) {
	opts := Options{
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			mux := http.NewServeMux()
			mux.HandleFunc("/orders", func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte("via router"))
			})
			return mux, nil
		},
		Wrap: markerWrap,
	}
	// Serve's own order: resolve the mux, then assemble the handler.
	var err error
	opts.Mux, err = buildMux(opts, nil)
	if err != nil {
		t.Fatalf("buildMux: %v", err)
	}
	handler, err := buildHandler(opts)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/orders", nil))
	if rec.Header().Get("X-Wrapped") != "yes" || rec.Body.String() != "via router" {
		t.Errorf("marker=%q body=%q; Wrap and Router must be orthogonal",
			rec.Header().Get("X-Wrapped"), rec.Body.String())
	}
}

func TestWrapRunsInsideLocaleStripping(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/en.toml": {Data: []byte("greet = \"hello\"\n")},
		"locales/fr.toml": {Data: []byte("greet = \"bonjour\"\n")},
	}
	var sawPath, sawGreet string
	spy := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sawPath = r.URL.Path
			sawGreet = T(r, "greet")
			next.ServeHTTP(w, r)
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", func(http.ResponseWriter, *http.Request) {})
	handler, err := buildHandler(Options{
		Mux: mux, Wrap: spy,
		Locales: []string{"en", "fr"}, LocaleFS: fsys,
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr/orders", nil))
	if sawPath != "/orders" {
		t.Errorf("middleware saw path %q, want %q (stripped)", sawPath, "/orders")
	}
	if sawGreet != "bonjour" {
		t.Errorf("middleware saw T(greet)=%q, want %q (translator must already ride the context)", sawGreet, "bonjour")
	}
}
