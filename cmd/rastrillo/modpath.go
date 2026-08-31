package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// moduleRequirement reports the version dir/go.mod requires of the
// named module, and the target of any replace directive pointing at it.
// Both are empty when the file does not mention the module at all.
//
// Hand-rolled for the same reason modulePath is: require and replace
// are two fixed line shapes in the two forms gofmt writes them, single
// and block. A require inside a block is indented; a replace is
// "replace <path> => <target>", optionally with a version on either
// side. Anything this parser does not recognise reports empty, and
// doctor says it could not read the version rather than guessing one.
func moduleRequirement(dir, module string) (version, replacement string, err error) {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", "", fmt.Errorf("read go.mod: %w", err)
	}
	defer f.Close()

	inRequire, inReplace := false, false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == ")":
			inRequire, inReplace = false, false
			continue
		case line == "require (":
			inRequire = true
			continue
		case line == "replace (":
			inReplace = true
			continue
		case strings.HasPrefix(line, "require "):
			line, inRequire = strings.TrimSpace(strings.TrimPrefix(line, "require ")), false
			if v := requireVersion(line, module); v != "" {
				version = v
			}
			continue
		case strings.HasPrefix(line, "replace "):
			line, inReplace = strings.TrimSpace(strings.TrimPrefix(line, "replace ")), false
			if r := replaceTarget(line, module); r != "" {
				replacement = r
			}
			continue
		}
		if inRequire {
			if v := requireVersion(line, module); v != "" {
				version = v
			}
		}
		if inReplace {
			if r := replaceTarget(line, module); r != "" {
				replacement = r
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	return version, replacement, nil
}

// requireVersion reads "<module> <version>" from one require line,
// reporting empty for any other module.
func requireVersion(line, module string) string {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != module {
		return ""
	}
	return fields[1]
}

// replaceTarget reads the right-hand side of "<module> [version] =>
// <target> [version]", reporting empty for any other module. The target
// is what the app actually builds against, so doctor names it rather
// than the version it displaced.
func replaceTarget(line, module string) string {
	left, right, ok := strings.Cut(line, "=>")
	if !ok {
		return ""
	}
	lf, rf := strings.Fields(left), strings.Fields(right)
	if len(lf) == 0 || lf[0] != module || len(rf) == 0 {
		return ""
	}
	return strings.Join(rf, " ")
}
