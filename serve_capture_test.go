package rastrillo

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// In capture mode Serve builds the handler, so the app's Router runs and
// its sets exist, writes them, and returns without listening. Addr is in
// the TEST-NET-1 documentation range so that a Serve that went on to
// listen would fail to bind instead of passing.
func TestServeCaptureMode(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "capture.json")
	t.Setenv(tmpl.CaptureEnv, out)
	// Capture opens the database in the working directory, as generate
	// runs it in a scratch one.
	t.Chdir(dir)

	var db *sql.DB
	err := Serve(Options{
		Addr:   "192.0.2.1:1",
		DBPath: filepath.Join(dir, "app.db"),
		Router: func(database *sql.DB) (*http.ServeMux, error) {
			db = database
			tmpl.Must(tmpl.New("servecapture").Parse(`<p>{{.}}</p>`))
			return http.NewServeMux(), nil
		},
	})
	if err != nil {
		t.Fatalf("Serve in capture mode = %v, want nil", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("Serve wrote no capture file: %v", err)
	}
	var captured tmpl.Capture
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, set := range captured.Sets {
		if set.Name != "servecapture" {
			continue
		}
		found = true
		if want := "amadan.net/rastrillo/rastrillo"; set.Owner != want {
			t.Errorf("Owner = %q, want %q", set.Owner, want)
		}
		if set.Templates["servecapture"] != "<p>{{.}}</p>" {
			t.Errorf("Templates = %v", set.Templates)
		}
	}
	if !found {
		t.Errorf("the set the Router built is not in the capture: %s", data)
	}
	if db == nil || db.Ping() == nil {
		t.Error("Serve left the database open")
	}
}

// emptyRouter is an app with no routes of its own.
func emptyRouter(*sql.DB) (*http.ServeMux, error) {
	return http.NewServeMux(), nil
}

// A capture that cannot be written is the app's startup failing, and
// generate has to hear about it.
func TestServeCaptureModeReturnsTheWriteError(t *testing.T) {
	t.Setenv(
		tmpl.CaptureEnv,
		filepath.Join(t.TempDir(), "no", "such", "dir", "c.json"),
	)
	err := Serve(Options{
		Addr:   "192.0.2.1:1",
		Router: emptyRouter,
	})
	if err == nil {
		t.Fatal("Serve swallowed the write error")
	}
}

// The app's DBPath may name its real database, as a path or a file:
// URI SQLite decodes. In capture mode Serve ignores it and opens a
// capture*.db in its working directory, generate's scratch directory,
// so capture can never migrate a real database.
func TestServeCaptureModeKeepsTheDatabaseInTheWorkingDirectory(t *testing.T) {
	for name, path := range map[string]func(real string) string{
		"absolute": func(real string) string { return real },
		"encoded uri": func(real string) string {
			return "file:" + url.PathEscape(real)
		},
		"uri query": func(real string) string {
			return "file:" + real + "?mode=rwc"
		},
	} {
		t.Run(name, func(t *testing.T) {
			real := filepath.Join(t.TempDir(), "app.db")
			work := t.TempDir()
			t.Chdir(work)
			t.Setenv(tmpl.CaptureEnv, filepath.Join(work, "capture.json"))
			err := Serve(Options{
				Addr:       "192.0.2.1:1",
				DBPath:     path(real),
				Migrations: []string{"CREATE TABLE t (id INTEGER)"},
				Router:     emptyRouter,
			})
			if err != nil {
				t.Fatalf("Serve in capture mode = %v, want nil", err)
			}
			if _, err := os.Stat(real); !os.IsNotExist(err) {
				t.Errorf(
					"capture mode opened the configured database (stat err %v)",
					err,
				)
			}
			got, _ := filepath.Glob(filepath.Join(work, "capture*.db"))
			if len(got) != 1 {
				t.Errorf("capture mode opened %v in its working "+
					"directory, want one capture*.db", got)
			}
		})
	}
}
