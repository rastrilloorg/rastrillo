package capturemode

import (
	"strings"
	"testing"
)

func TestDBPathOutsideCaptureIsUnchanged(t *testing.T) {
	if got := DBPath("/srv/app/app.db"); got != "/srv/app/app.db" {
		t.Errorf("outside capture, DBPath = %q, want the path unchanged", got)
	}
}

// In capture the configured value is never used: SQLite decodes a
// file: URI, so file:%2Fsrv%2Fapp.db is the real database however its
// base name reads, and a query can reopen it read-write. Each distinct
// path gets a plain name of its own in the working directory; the same
// path, opened through both entry points, gets the same one.
func TestDBPathInCaptureIgnoresTheConfiguredPath(t *testing.T) {
	t.Setenv(Env, "/scratch/capture.json")
	t.Cleanup(reset)
	reset()
	paths := []string{
		"/srv/app/app.db",
		"file:%2Fsrv%2Fapp%2Fapp.db",
		"file:/srv/app/app.db?mode=rwc&cache=shared",
		"data/app.db",
	}
	seen := map[string]bool{}
	for index, path := range paths {
		got := DBPath(path)
		if strings.ContainsAny(got, `/\:?%`) ||
			!strings.HasPrefix(got, "capture") ||
			!strings.HasSuffix(got, ".db") {
			t.Errorf(
				"DBPath(%q) = %q, want a plain capture*.db name",
				path,
				got,
			)
		}
		if index == 0 && got != "capture.db" {
			t.Errorf("the first path got %q, want capture.db", got)
		}
		if seen[got] {
			t.Errorf(
				"DBPath(%q) = %q, a name another path already has",
				path,
				got,
			)
		}
		seen[got] = true
		if again := DBPath(path); again != got {
			t.Errorf("DBPath(%q) gave %q, then %q", path, got, again)
		}
	}
	if got := DBPath(""); got != "" {
		t.Errorf("DBPath(\"\") = %q, want no database", got)
	}
}
