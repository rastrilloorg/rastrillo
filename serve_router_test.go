package rastrillo

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exactly one of Mux and Router: Serve must refuse both-set and
// neither-set before it ever calls listen. Addr is set to an address in
// the TEST-NET-1 documentation range (RFC 5737) — never assigned to a
// real interface — so that if the guard is ever moved past listen, the
// mutant fails to bind instead of silently succeeding (both-set leaking
// a live listener on :8080, neither-set only coincidentally failing with
// "address already in use" from the leaked one). Asserting the error
// text names the guard, not just non-nil, means only the actual guard
// satisfies this test — any other failure (e.g. a listen error) does not.
func TestServeRequiresExactlyOneOfMuxAndRouter(t *testing.T) {
	const wantErr = "exactly one of"
	unbindable := "192.0.2.1:1"

	both := Options{
		Addr:   unbindable,
		Mux:    http.NewServeMux(),
		Router: func(*sql.DB) (*http.ServeMux, error) { return http.NewServeMux(), nil },
	}
	if err := Serve(both); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("Serve(both Mux and Router) = %v, want an error containing %q", err, wantErr)
	}

	neither := Options{Addr: unbindable}
	if err := Serve(neither); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("Serve(neither Mux nor Router) = %v, want an error containing %q", err, wantErr)
	}
}

// Pins the wiring order inside Serve itself, not just buildMux in
// isolation: the database must be open — file materialized, migrations
// applied — before Router runs, so an app can rely on the handle Router
// receives instead of racing its own open against Serve's. Returning an
// error from Router aborts Serve before it ever calls listen, so this
// never binds a socket.
func TestServeOpensTheDatabaseBeforeRouter(t *testing.T) {
	var got *sql.DB
	path := filepath.Join(t.TempDir(), "x.db")
	err := Serve(Options{DBPath: path, Router: func(d *sql.DB) (*http.ServeMux, error) {
		got = d
		return nil, errors.New("stop")
	}})
	if err == nil {
		t.Fatal("Serve ignored the Router error")
	}
	if got == nil {
		t.Error("Router ran before the database opened")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("db not materialized before Router: %v", err)
	}
}

func TestBuildMuxCallsRouterWithTheHandle(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "x.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var got *sql.DB
	mux, err := buildMux(Options{Router: func(d *sql.DB) (*http.ServeMux, error) {
		got = d
		return http.NewServeMux(), nil
	}}, db)
	if err != nil {
		t.Fatal(err)
	}
	if mux == nil {
		t.Fatal("buildMux returned a nil mux")
	}
	if got != db {
		t.Error("Router did not receive the opened handle")
	}
}

func TestBuildMuxPropagatesRouterError(t *testing.T) {
	boom := errors.New("boom")
	_, err := buildMux(Options{Router: func(*sql.DB) (*http.ServeMux, error) {
		return nil, boom
	}}, nil)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("want the Router error wrapped, got %v", err)
	}
}

func TestBuildMuxRefusesANilMuxFromRouter(t *testing.T) {
	_, err := buildMux(Options{Router: func(*sql.DB) (*http.ServeMux, error) {
		return nil, nil
	}}, nil)
	if err == nil {
		t.Error("buildMux accepted a nil mux with a nil error")
	}
}

func TestBuildMuxPassesThroughAPlainMux(t *testing.T) {
	m := http.NewServeMux()
	mux, err := buildMux(Options{Mux: m}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mux != m {
		t.Error("buildMux did not return Options.Mux unchanged")
	}
}

// OpenDB is openDB exported: same pragmas, same eager materialization,
// same idempotent migrations. The file-exists check is the hibernate
// contract (the activator replicates the path from boot).
func TestOpenDBMaterializesAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	db, err := OpenDB(path, []string{
		`CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY, name TEXT)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not materialized at boot: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t (name) VALUES ('a')`); err != nil {
		t.Errorf("migrated table not usable: %v", err)
	}
	db.Close()

	// Re-open: additive migrations must be idempotent.
	again, err := OpenDB(path, []string{
		`CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY, name TEXT)`,
		`ALTER TABLE t ADD COLUMN name TEXT`, // duplicate column: tolerated
	})
	if err != nil {
		t.Fatalf("re-open with idempotent migrations failed: %v", err)
	}
	again.Close()
}

func TestLocaleSwitchRouteMountedOnlyWithLocales(t *testing.T) {
	mux := http.NewServeMux()
	without, err := buildHandler(Options{Mux: mux})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	without.ServeHTTP(rec, httptest.NewRequest("POST", LocaleSwitchPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no locales: status %d, want 404", rec.Code)
	}

	with, err := buildHandler(Options{Mux: http.NewServeMux(), Locales: []string{"en", "ga"}})
	if err != nil {
		t.Fatal(err)
	}
	form := strings.NewReader("locale=ga&return=/x")
	req := httptest.NewRequest("POST", LocaleSwitchPath, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	with.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ga/x" {
		t.Errorf("with locales: status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// Under a locale prefix the same route still answers (the
	// middleware strips the prefix first).
	req = httptest.NewRequest("POST", "/ga"+LocaleSwitchPath, strings.NewReader("locale=en&return=/x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	with.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("prefixed: status %d", rec.Code)
	}
}

// The scheme switch is mounted with or without locales: a one-language
// app has colours too. Under a locale prefix the same route answers,
// and the return path goes back exactly as given, prefix included, so
// a reader on /ga/x who picks Dark is still reading Irish afterwards.
func TestSchemeSwitchRouteMountedAlways(t *testing.T) {
	post := func(h http.Handler, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	without, err := buildHandler(Options{Mux: http.NewServeMux()})
	if err != nil {
		t.Fatal(err)
	}
	if rec := post(without, SchemeSwitchPath, "scheme=dark&return=/x"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/x" {
		t.Errorf("no locales: status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	with, err := buildHandler(Options{Mux: http.NewServeMux(), Locales: []string{"en", "ga"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{SchemeSwitchPath, "/ga" + SchemeSwitchPath} {
		if rec := post(with, path, "scheme=dark&return=/ga/x"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ga/x" {
			t.Errorf("%s: status %d Location %q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// An app's own POST /_theme wins. Four family apps hand-rolled a theme
// switch on that path before the framework had one, and an upgrade
// must not take their route over: the framework's is mounted only when
// the app's mux has nothing at that path. /_locale keeps its own
// precedence (the framework's wins), which is pinned above.
func TestAnAppsOwnSchemeRouteWins(t *testing.T) {
	for _, pattern := range []string{"POST " + SchemeSwitchPath, SchemeSwitchPath} {
		mux := http.NewServeMux()
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
		h, err := buildHandler(Options{Mux: mux})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", SchemeSwitchPath, strings.NewReader("theme=dark"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusTeapot {
			t.Errorf("%q: status %d, want the app's 418", pattern, rec.Code)
		}
	}
}

// A host-qualified route is the app's for that host only. Ownership is
// decided per request, because a probe at startup has no host to ask
// with: it missed "POST example.com/_theme", so the framework took that
// host's posts and answered the app's existing forms with 400.
func TestAnAppsHostQualifiedSchemeRouteWinsForItsHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST example.com"+SchemeSwitchPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h, err := buildHandler(Options{Mux: mux})
	if err != nil {
		t.Fatal(err)
	}
	post := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://"+host+SchemeSwitchPath, strings.NewReader("scheme=dark&return=/x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("example.com"); rec.Code != http.StatusTeapot {
		t.Errorf("example.com: status %d, want the app's 418", rec.Code)
	}
	if rec := post("other.example"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/x" {
		t.Errorf("other.example: status %d Location %q, want the framework's 303 to /x", rec.Code, rec.Header().Get("Location"))
	}
}

// A router mounted under "/" (chi, say) cannot be seen through the mux,
// so NoSchemeSwitch is the opt-out: with it set, the path reaches the
// app like any other.
func TestNoSchemeSwitchLeavesThePathToTheApp(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == SchemeSwitchPath {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		http.NotFound(w, r)
	})
	req := func() *http.Request {
		r := httptest.NewRequest("POST", SchemeSwitchPath, strings.NewReader("theme=dark"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		return r
	}
	on, _ := buildHandler(Options{Mux: mux})
	rec := httptest.NewRecorder()
	on.ServeHTTP(rec, req())
	if rec.Code != http.StatusBadRequest {
		t.Errorf("without the opt-out the framework's route answers: status %d, want its 400 for theme=", rec.Code)
	}
	off, _ := buildHandler(Options{Mux: mux, NoSchemeSwitch: true})
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, req())
	if rec.Code != http.StatusTeapot {
		t.Errorf("with NoSchemeSwitch: status %d, want the app's 418", rec.Code)
	}
}
