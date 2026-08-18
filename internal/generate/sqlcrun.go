// This file runs sqlc against Task 5's emitted gen/store/ tree. It
// never edits that tree's contents — sqlc.yaml and the per-resource
// schema.sql/queries.sql are consumed as-is; sqlc's own output lands
// alongside them (queries.sql.go, models.go, db.go per resource).
package generate

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// sqlcToolPath is the import path an app's go.mod must carry as a
// Go 1.24+ tool directive for `go tool sqlc` to resolve.
const sqlcToolPath = "github.com/sqlc-dev/sqlc/cmd/sqlc"

// RunSqlc executes `go tool sqlc generate -f gen/store/sqlc.yaml` in
// moduleRoot, turning Task 5's emitted schema.sql/queries.sql into
// compiled Go (queries.sql.go, models.go, db.go per resource). The
// app's go.mod must carry the sqlc tool directive; a missing tool is
// an error naming exactly what to add.
func RunSqlc(moduleRoot string) error {
	if err := checkSqlcTool(moduleRoot); err != nil {
		return err
	}

	yamlPath := filepath.Join("gen", "store", "sqlc.yaml")
	cmd := exec.Command("go", "tool", "sqlc", "generate", "-f", yamlPath)
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("sqlc generate: %s", msg)
	}
	return nil
}

// checkSqlcTool reports whether moduleRoot's go.mod carries the sqlc
// tool directive. `go tool` (no arguments) lists every tool directive
// by its full import path regardless of whether the module has been
// downloaded yet, so this needs no go.mod parsing of its own.
func checkSqlcTool(moduleRoot string) error {
	cmd := exec.Command("go", "tool")
	cmd.Dir = moduleRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("go tool: %s", msg)
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.TrimSpace(line) == sqlcToolPath {
			return nil
		}
	}
	return fmt.Errorf("sqlc tool directive missing from go.mod; add it with:\n\tgo get -tool %s", sqlcToolPath)
}
