package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// modulePath reads the module directive from dir/go.mod. Hand-rolled
// rather than importing golang.org/x/mod: the module line is one fixed
// shape, this is a page of code, not an SDK's worth — the family's own
// convention for when to hand-roll (carlosframework/skills, blueprint.md).
func modulePath(dir string) (string, error) {
	f, err := os.Open(dir + "/go.mod")
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
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
	return "", fmt.Errorf("go.mod: no module directive found")
}

// rastrilloModule is the framework's own module path — what an app's
// go.mod requires, and what rastrillo doctor compares its own version
// against.
const rastrilloModule = "amadan.net/rastrillo/rastrillo"

// preMoveModule is the path rastrillo had until v0.25.0 moved it to
// amadan. Nothing after v0.23.0 was released under it that Go will
// build — the mirror's later tags declare the new path — so an app
// still importing from here is frozen at v0.23.0 or earlier, and its
// own vendored test cannot notice, because it compares against the
// same frozen module (issue #52).
const preMoveModule = "github.com/carlosframework/rastrillo"

// moduleRequirement reports the version dir/go.mod requires of the
// named module, and the target of any replace directive pointing at it.
// Both are empty when the file does not mention the module at all.
func moduleRequirement(dir, module string) (version, replacement string, err error) {
	req, err := readRequirement(dir, module)
	return req.version, req.replaced, err
}

// requirement is what go.mod says about one module.
type requirement struct {
	version  string
	replaced string
	// indirect is the "// indirect" mark on the require line: nothing in
	// the app itself imports the module, a dependency does. Doctor's
	// pre-move check turns on it, because only a direct import is one
	// the app can rewrite.
	indirect bool
}

// readRequirement reads go.mod with the go command's own parser.
//
// This used to be hand-rolled, like modulePath still is, on the theory
// that require and replace have two fixed shapes. That held while the
// answer only labelled a report. Once the pre-move check made it a
// safety check, each spelling the go command accepts and this missed
// (a quoted path, "require(", a tab, "// indirect;" with nothing after
// it, one replace listed after another) let a frozen app through to a
// clean report and a --fix, and two review rounds kept finding more.
// Go's grammar is not ours to re-derive.
func readRequirement(dir, module string) (requirement, error) {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return requirement{}, fmt.Errorf("read go.mod: %w", err)
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return requirement{}, fmt.Errorf("read go.mod: %w", err)
	}
	var req requirement
	for _, r := range f.Require {
		if r.Mod.Path == module {
			req.version, req.indirect = r.Mod.Version, r.Indirect
		}
	}
	// A replace that names a version applies only to that version, and
	// beats one that names none, whatever order they are listed in.
	// "old v0.22.0 => ../x" leaves an app on v0.23.0 building against
	// the real, frozen v0.23.0. With no requirement at all there is no
	// version to scope by, so any replace is reported.
	var exact, unscoped string
	for _, r := range f.Replace {
		if r.Old.Path != module {
			continue
		}
		target := r.New.Path
		if r.New.Version != "" {
			target += " " + r.New.Version
		}
		switch {
		case r.Old.Version == "":
			unscoped = target
		case r.Old.Version == req.version || req.version == "":
			exact = target
		}
	}
	req.replaced = exact
	if req.replaced == "" {
		req.replaced = unscoped
	}
	return req, nil
}
