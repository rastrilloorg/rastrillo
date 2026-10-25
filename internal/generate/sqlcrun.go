// This file runs sqlc against Task 5's emitted gen/store/ tree. It
// never edits that tree's contents — sqlc.yaml and the per-resource
// schema.sql/queries.sql are consumed as-is; sqlc's own output lands
// alongside them (queries.sql.go, models.go, db.go per resource).
package generate

import (
	"bytes"
	"encoding/json"
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
// tool directive. It reads `go mod edit -json`, not the listing `go
// tool` prints: that listing is for people, and Go 1.27 changed it from
// the bare import path to "sqlc (path)", which made every app with the
// directive fail here with advice to add it. The JSON is go.mod's own
// structure, needs nothing downloaded, and lets go do the parsing.
func checkSqlcTool(moduleRoot string) error {
	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = moduleRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("go mod edit: %s", msg)
	}
	var mod struct {
		Tool []struct{ Path string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &mod); err != nil {
		return fmt.Errorf("go mod edit -json: %w", err)
	}
	for _, tool := range mod.Tool {
		if tool.Path == sqlcToolPath {
			return nil
		}
	}
	return fmt.Errorf("sqlc tool directive missing from go.mod; add it with:\n\tgo get -tool %s", sqlcToolPath)
}
