// Command dsgen renders the design-system tree (internal/designsystem's
// Render) to disk. `go generate ./...` runs it via the repo root's
// gen.go; TestDesignSystemIsCurrent in internal/designsystem holds the
// committed tree to what Render produces, so drift between the two
// fails the build.
//
// dsgen deletes its output root before writing, so a partial removed
// from Render shows up as a deletion in the tree rather than a stale
// leftover file. That makes the output root dangerous to get wrong —
// task 2 of this plan pointed it at the wrong directory once and wiped
// 152 files that were not docs/design-system's. guardRoot below is the
// fix: refuse to delete anything unless the target is unmistakably
// docs/design-system inside this module's checkout.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo/internal/designsystem"
)

// wantModule is the only module dsgen will delete a docs/design-system
// directory inside of.
const wantModule = "github.com/carlosframework/rastrillo"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dsgen:", err)
		os.Exit(1)
	}
}

func run() error {
	root := flag.String("root", "", "output root (default: <repo>/docs/design-system, found from cwd's go.mod)")
	flag.Parse()

	out := *root
	if out == "" {
		def, err := defaultRoot()
		if err != nil {
			return err
		}
		out = def
	}

	if err := guardRoot(out); err != nil {
		return err
	}

	if err := os.RemoveAll(out); err != nil {
		return fmt.Errorf("removing %s: %w", out, err)
	}

	files, err := designsystem.Render()
	if err != nil {
		return fmt.Errorf("Render: %w", err)
	}

	var totalBytes int
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		body := files[name]
		dest := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", name, err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		totalBytes += len(body)
	}

	fmt.Printf("dsgen: wrote %d files, %d bytes, to %s\n", len(files), totalBytes, out)
	return nil
}

// defaultRoot walks up from the working directory looking for go.mod,
// so `go generate ./...` from anywhere in the repo lands on the same
// <repo>/docs/design-system regardless of the generator's own working
// directory.
func defaultRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "design-system"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s", cwd)
		}
		dir = parent
	}
}

// guardRoot refuses a dangerous output root before anything is deleted.
// The root must be an absolute path ending in docs/design-system, and
// the directory two levels up (docs/design-system's grandparent, i.e.
// the repo root) must hold a go.mod whose module line is
// github.com/carlosframework/rastrillo. Anything else is refused with
// an explanation and no filesystem change.
func guardRoot(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("refusing to touch %q: not an absolute path", root)
	}

	clean := filepath.Clean(root)
	if filepath.Base(clean) != "design-system" || filepath.Base(filepath.Dir(clean)) != "docs" {
		return fmt.Errorf("refusing to touch %q: does not end in docs/design-system", root)
	}

	repoRoot := filepath.Dir(filepath.Dir(clean))
	modPath := filepath.Join(repoRoot, "go.mod")
	module, err := readModule(modPath)
	if err != nil {
		return fmt.Errorf("refusing to touch %q: %w", root, err)
	}
	if module != wantModule {
		return fmt.Errorf("refusing to touch %q: %s declares module %q, want %q", root, modPath, module, wantModule)
	}

	return nil
}

// readModule reads the module directive out of a go.mod file. Hand-
// rolled rather than importing golang.org/x/mod, same call as
// cmd/rastrillo/modpath.go's modulePath: this is one fixed line shape,
// not an SDK's worth of work.
func readModule(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no module directive found", path)
}
