package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// rebuildApp is one turn of dev's loop: generate when the app has
// anything to generate, then build appPkg into bin.
func rebuildApp(dir, appPkg, bin string) error {
	if generates(dir) {
		if err := runGenerate(devGenerateArgs(dir)); err != nil {
			return err
		}
	}
	cmd := exec.Command("go", devBuildArgs(bin, appPkg)...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

// generates reports whether dir has generation input (manifest/,
// actions/) or output (gen/). A bare app has none, and generating
// anyway would write it a gen/ it never imports; an app with any of
// them must generate, or it builds against stale generated code.
func generates(dir string) bool {
	for _, input := range []string{"manifest", "actions", "gen"} {
		if _, err := os.Stat(filepath.Join(dir, input)); err == nil {
			return true
		}
	}
	return false
}
