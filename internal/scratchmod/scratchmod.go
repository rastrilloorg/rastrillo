// Package scratchmod writes the throwaway modules the manifest and
// generator suites build in: a module under a temp directory that
// requires github.com/carlosframework/rastrillo and replaces it with
// the checkout under test, so a nested `go run` or `go build` has a
// real module context.
//
// It exists to keep those modules resolvable under the toolchain's
// default -mod=readonly. A scratch go.mod naming only rastrillo
// carries no module data for rastrillo's OWN dependencies, so every
// nested build inside it needs either a network fetch or
// GOFLAGS=-mod=mod to invent one. CI set -mod=mod tree-wide for
// exactly that reason, and the comment saying so was accurate.
//
// The reason not to keep doing that is what the setting costs
// elsewhere. Under -mod=mod a missing go.sum entry is not an error:
// the toolchain writes the requirement and carries on. That is
// precisely how a scaffolded app shipped for four releases with a
// go.mod that failed its own `make ci` on a user's machine — the suite
// ran the same gate, under -mod=mod, and watched it pass (CHANGELOG
// v0.24.0). A crutch that resolves scratch modules also hides the one
// bug class those scratch modules exist to catch.
//
// So the scratch module inherits the checkout's own requirements and
// go.sum verbatim instead. It resolves offline from the module cache
// the checkout already populated, needs no tidy step, and — the point
// — a missing entry stays an error everywhere.
package scratchmod

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path is the module path every scratch module requires and replaces.
const Path = "github.com/carlosframework/rastrillo"

// Write creates go.mod and go.sum in dir for a module named name whose
// rastrillo requirement is replaced by the checkout at repoRoot.
//
// The generated go.mod carries the checkout's own `go` directive and
// require blocks verbatim, so it tracks the root module without a
// second list to keep in step; directives, if any, are inserted as
// their own lines (the sqlc `tool` directive is the one caller that
// needs this).
func Write(dir, name, repoRoot string, directives ...string) error {
	rootMod, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return fmt.Errorf("read the checkout's go.mod: %w", err)
	}
	rootSum, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		return fmt.Errorf("read the checkout's go.sum: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n", name)
	b.WriteString(withoutModuleLine(string(rootMod)))
	for _, d := range directives {
		fmt.Fprintf(&b, "\n%s\n", d)
	}
	fmt.Fprintf(&b, "\nrequire %s v0.0.0\n", Path)
	fmt.Fprintf(&b, "\nreplace %s => %s\n", Path, repoRoot)

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.sum"), rootSum, 0o644)
}

// withoutModuleLine drops the `module` declaration and returns what
// follows — the `go` directive and the require blocks. Everything else
// is kept as written, comments included: this is the checkout's own
// dependency set, and reformatting it would only invite drift between
// what the scratch module resolves and what the checkout builds.
func withoutModuleLine(gomod string) string {
	lines := strings.Split(gomod, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return gomod
}
