package guardtest

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/internal/checkconfig"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
)

// Every entry point that opens the database guards as its first
// statement. With the variable set there, the probe did not run first,
// so the entry point must exit 2 before creating or touching the file.
// Guard really exits, so each entry point runs in a re-executed copy of
// this test binary.
func TestGuardsRefuseBeforeOpening(t *testing.T) {
	if entry := os.Getenv("GUARD_HELPER"); entry != "" {
		path := os.Getenv("GUARD_DB")
		switch entry {
		case "db.Open":
			db.Open(path, slog.Default())
		case "rastrillo.OpenDB":
			rastrillo.OpenDB(path, nil)
		case "rastrillo.Handler":
			rastrillo.Handler(rastrillo.Options{DBPath: path, Mux: http.NewServeMux()})
		case "rastrillo.Serve":
			rastrillo.Serve(rastrillo.Options{DBPath: path, Mux: http.NewServeMux(), Addr: "127.0.0.1:0"})
		}
		os.Exit(0) // reaching here means the guard did not fire
	}
	for _, entry := range []string{"db.Open", "rastrillo.OpenDB", "rastrillo.Handler", "rastrillo.Serve"} {
		path := filepath.Join(t.TempDir(), "app.db")
		cmd := exec.Command(os.Args[0], "-test.run=^TestGuardsRefuseBeforeOpening$")
		cmd.Env = append(os.Environ(), "GUARD_HELPER="+entry, "GUARD_DB="+path, checkconfig.Env+"=1")
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			t.Errorf("%s: err %v, output %s; want exit 2", entry, err, out)
		}
		if !strings.Contains(string(out), entry+" was reached") || !strings.Contains(string(out), "ProbeConfig") {
			t.Errorf("%s: message %q must name the entry point and ProbeConfig", entry, out)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s created or touched %s before refusing", entry, path)
		}
	}
}
