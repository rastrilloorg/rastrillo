package main

import (
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo/internal/appname"
)

// writeScaffold measures the assembled app before it creates a single
// directory, so a name too long for the app's own make code is refused
// with the working directory still clean.
func writeScaffold(name string, files map[string]string) error {
	if err := appname.CheckColumns(name, files); err != nil {
		return err
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	// runNew writes the runner's steps here once the files are down.
	return os.MkdirAll(filepath.Join(name, ".amadan", "ci.d"), 0o755)
}
