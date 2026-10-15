package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// A hibernate route's activator starts replicating the DB path from the
// moment the instance boots (see OpenDB's comment) — so a zero-migration
// app must still leave a file on disk for it to stream, not just an
// in-memory sql.DB handle that never touched the driver.
func TestOpenDBCreatesFileWithNoMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	db, err := OpenDB(path, nil)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist after OpenDB, got: %v", path, err)
	}
}

// A second OpenDB against the same path (the activator's restore/wake
// cycle re-execs the binary against an already-replicated file) must
// still succeed — Ping against an existing, already-migrated database is
// not itself destructive or failure-prone.
func TestOpenDBIdempotentAgainstExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	db1, err := OpenDB(path, nil)
	if err != nil {
		t.Fatalf("first OpenDB: %v", err)
	}
	db1.Close()

	db2, err := OpenDB(path, nil)
	if err != nil {
		t.Fatalf("second OpenDB against existing file: %v", err)
	}
	defer db2.Close()
}

// TestBaseCatalogLayersUnderAppCatalog proves buildHandler actually
// threads Options.BaseCatalog into NewLocales' base argument (it used
// to be hardcoded nil) — not just that Locales.T itself layers
// correctly, which locale_test.go already covers. A generated
// gen/locales/locales.go var BaseCatalog is what an app wires here
// (design doc §9's manifest system); the app's own catalog must still
// win when both declare the same key, and a base-only key must still
// surface when the app catalog is silent on it.
func TestBaseCatalogLayersUnderAppCatalog(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/en.toml": {Data: []byte("resource.notes.name = \"My Notes\"\n")},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(T(r, "resource.notes.name") + "|" + T(r, "ui.save")))
	})

	handler, err := buildHandler(Options{
		Mux:         mux,
		Locales:     []string{"en"},
		LocaleFS:    fsys,
		BaseCatalog: Catalog{"resource.notes.name": "Notes", "ui.save": "Save"},
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	want := "My Notes|Save"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q (app catalog must win over the base; a base-only key must still surface)", got, want)
	}
}

// Spec §12 item 9: boot refuses every CheckConfig failure, and accepts
// an app with no manifest or with only DefaultLocale. This binary links
// only the en pack (packs_test.go), so "de" is refused the way it would
// be in an app that forgot the import.
func TestHandlerRefusesEveryCheckConfigFailure(t *testing.T) {
	for name, opts := range map[string]Options{
		"no pack for the default": {DefaultLocale: "de"},
		"default not offered":     {DefaultLocale: "es", Locales: []string{"en"}},
		"non-canonical identity":  {Locales: []string{"en", "pt-pt"}},
		"duplicate":               {Locales: []string{"en", "en"}},
		"malformed":               {Locales: []string{"en", "de--CH"}},
	} {
		opts.Mux = http.NewServeMux()
		if _, closeFn, err := Handler(opts); err == nil {
			closeFn()
			t.Errorf("%s: Handler accepted %+v", name, opts)
		} else if !strings.Contains(err.Error(), "refusing to start") {
			t.Errorf("%s: %v does not say it refused to start", name, err)
		}
	}
	for name, opts := range map[string]Options{
		"no manifest, nothing set": {},
		"DefaultLocale only":       {DefaultLocale: "en"},
	} {
		opts.Mux = http.NewServeMux()
		_, closeFn, err := Handler(opts)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		closeFn()
	}
}

// The check runs before the database opens: a refused configuration
// must leave no file behind, or a misconfigured deploy would already
// have created (and, with migrations, changed) the database it refuses
// to serve.
func TestHandlerRefusesBeforeOpeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	if _, _, err := Handler(Options{DefaultLocale: "de", DBPath: path, Mux: http.NewServeMux()}); err == nil {
		t.Fatal("Handler accepted a default whose pack is not linked")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a refused configuration touched %s: %v", path, err)
	}
}
