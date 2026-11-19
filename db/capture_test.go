package db

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// rastrillo generate starts the app to capture its templates, and the
// app opens its database at startup. In capture mode the configured
// path is ignored, however it is written, and the database is a
// capture*.db in the working directory, the capture's scratch one, so
// a real one is never opened or migrated.
func TestOpenInCaptureModeStaysInTheWorkingDirectory(t *testing.T) {
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
			t.Setenv(
				"RASTRILLO_TEMPLATE_CAPTURE",
				filepath.Join(work, "capture.json"),
			)
			database, err := Open(path(real), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
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
