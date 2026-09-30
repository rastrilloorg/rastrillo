package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
//
// Hand-rolled for the same reason modulePath is: require and replace
// are two fixed line shapes in the two forms gofmt writes them, single
// and block. A require inside a block is indented; a replace is
// "replace <path> => <target>", optionally with a version on either
// side. Anything this parser does not recognise reports empty, and
// doctor says it could not read the version rather than guessing one.
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

// readRequirement is moduleRequirement with the indirect mark kept.
//
// It reads go.mod's tokens the way the go command does, not its
// gofmt'd shape, because the pre-move check is a safety check: a line
// it fails to read lets a frozen app through to a clean report and a
// --fix. So a tab after the verb, a quoted path, and a comment that
// merely mentions "indirect" all read as Go reads them.
func readRequirement(dir, module string) (requirement, error) {
	var req requirement
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return req, fmt.Errorf("read go.mod: %w", err)
	}
	defer f.Close()

	// A replace can name the version it applies to, and then it
	// replaces only that one: "old v0.22.0 => ../x" leaves an app on
	// v0.23.0 building against the real v0.23.0. So the left-hand
	// version is kept until the require is known.
	var replaced, replacedFor string
	block := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line, comment, _ := strings.Cut(scanner.Text(), "//")
		// Go's own test for the mark: the comment is "indirect", or
		// starts "indirect;" with more after it.
		c := strings.TrimSpace(comment)
		indirect := c == "indirect" || strings.HasPrefix(c, "indirect;")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		verb := block
		switch {
		case len(fields) == 1 && fields[0] == ")":
			block = ""
			continue
		case (fields[0] == "require" || fields[0] == "replace") && len(fields) == 2 && fields[1] == "(":
			block = fields[0]
			continue
		case block == "":
			verb, fields = fields[0], fields[1:]
		}
		for i := range fields {
			fields[i] = unquote(fields[i])
		}
		switch verb {
		case "require":
			if len(fields) == 2 && fields[0] == module {
				req.version, req.indirect = fields[1], indirect
			}
		case "replace":
			if target, forVersion, ok := replaceTarget(fields, module); ok {
				replaced, replacedFor = target, forVersion
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return requirement{}, err
	}
	if replacedFor == "" || req.version == "" || replacedFor == req.version {
		req.replaced = replaced
	}
	return req, nil
}

// unquote reads a go.mod token that may be written as a Go string,
// which the go command accepts for any path.
func unquote(tok string) string {
	if len(tok) >= 2 && (tok[0] == '"' || tok[0] == '`') {
		if u, err := strconv.Unquote(tok); err == nil {
			return u
		}
	}
	return tok
}

// replaceTarget reads "<module> [version] => <target> [version]" from a
// replace's tokens, reporting ok false for any other module. The target
// is what the app actually builds against, so doctor names it rather
// than the version it displaced; forVersion is the left-hand version,
// empty when the replace covers every version.
func replaceTarget(fields []string, module string) (target, forVersion string, ok bool) {
	arrow := -1
	for i, f := range fields {
		if f == "=>" {
			arrow = i
		}
	}
	if arrow < 1 || arrow > 2 || fields[0] != module || arrow == len(fields)-1 {
		return "", "", false
	}
	if arrow == 2 {
		forVersion = fields[1]
	}
	return strings.Join(fields[arrow+1:], " "), forVersion, true
}
