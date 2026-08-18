package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
