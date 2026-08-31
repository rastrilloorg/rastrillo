// generatecheck_test.go runs the real `rastrillo generate --check`
// against this app's own committed gen/. `--check` runs the whole
// manifest pipeline into a scratch directory and diffs it byte-for-
// byte against the committed output (cmd/rastrillo/generate.go's own
// doc comment on generate.GenerateManifests) — that IS this app's
// double-regen byte-identity proof for everything the pipeline's own
// emitters write, not a separate thing to also write: a second
// hand-rolled diff script would only re-implement what --check
// already does. The one thing it does NOT diff is sqlc's own compiled
// output (gen/store/ticket_types/{queries.sql.go,models.go,db.go}) —
// internal/generate/manifestgen.go's emitPipeline runs check-only with
// runSqlc=false on purpose, so a --check never forces network/tool
// access and never flags absent sqlc output as a false idempotency
// failure. A hand-edited queries.sql.go would therefore slip past this
// test; nothing here or in the framework catches that today.
package ticketstest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateCheckIsGreen(t *testing.T) {
	cmd := exec.Command("go", "run", "amadan.net/rastrillo/rastrillo/cmd/rastrillo", "generate", "--check", ".")
	cmd.Dir = appRoot(t)
	cmd.Env = withModMode(os.Environ())

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("rastrillo generate --check .: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "route(s)") {
		t.Errorf("unexpected --check output: %s", out.String())
	}
}

// appRoot resolves examples/tickets's own absolute path from this test
// file's location (its dir's grandparent), independent of whatever
// directory `go test` happens to be invoked from.
func appRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// withModMode strips any inherited GOFLAGS and sets -mod=mod: the tool
// directive (go.mod's `tool github.com/sqlc-dev/sqlc/cmd/sqlc`) needs
// it the same way the module's own regen commands do (see the module
// README).
func withModMode(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "GOFLAGS=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GOFLAGS=-mod=mod")
}
