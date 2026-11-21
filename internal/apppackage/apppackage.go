// Package apppackage finds an app's own package among the directories
// under its internal/: the one `rastrillo migration` loads models and
// migrations from. An app such as Patrix keeps helper and end-to-end
// packages beside it there, so the directory alone cannot say which.
package apppackage

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Find returns the name of the app's package under dir/internal: the
// one override names, if any, else the one package whose compiled
// files declare every name in needs as a package-level var. When none
// does, as in a fresh scaffold before its first migration, it falls
// back to the single directory that is not an older app's separate
// <name>test package.
func Find(dir, override string, needs []string) (string, error) {
	internal := filepath.Join(dir, "internal")
	if override == "" {
		return detect(dir, needs)
	}
	info, err := os.Stat(filepath.Join(internal, override))
	if override == "." || override == ".." ||
		override != filepath.Base(override) || err != nil || !info.IsDir() {
		return "", fmt.Errorf("-package %q names no directory directly "+
			"under %s; set it to a directory there", override, internal)
	}
	return override, nil
}

// detect picks the app package out of the directories in dir's
// internal/, or says which candidates it could not choose between, or
// which it could not read.
func detect(dir string, needs []string) (string, error) {
	internal := filepath.Join(dir, "internal")
	all, err := directoriesIn(internal)
	if err != nil {
		return "", fmt.Errorf("no internal/ directory in %s: %w", dir, err)
	}
	declaring, unreadable, err := inspectPackages(dir, all, needs)
	if err != nil {
		return "", err
	}
	names := strings.Join(needs, ", ")
	found := withoutOlderTests(all)
	switch {
	case len(unreadable) > 0:
		return "", fmt.Errorf("could not read every package under %s, "+
			"so it cannot identify the app's package: %s. Fix the "+
			"errors, or choose the app's package with -package <name>, "+
			"for example: rastrillo migration check -package <name>",
			internal, strings.Join(unreadable, "; "))
	case len(declaring) == 1:
		return declaring[0], nil
	case len(declaring) > 1:
		return "", fmt.Errorf("more than one package under %s declares "+
			"%s: %s. Name the app's package with -package, for "+
			"example: rastrillo migration check -package <name>",
			internal, names, strings.Join(declaring, ", "))
	case len(found) == 1:
		return found[0], nil
	case len(all) == 0:
		return "", fmt.Errorf("no package under %s; add the app's "+
			"package there", internal)
	}
	return "", fmt.Errorf("no package under %s declares %s; choose one "+
		"of these directories with -package: %s. For example: "+
		"rastrillo migration check -package <name>",
		internal, names, strings.Join(all, ", "))
}

// directoriesIn names the directories directly inside parent.
func directoriesIn(parent string) ([]string, error) {
	entries, err := os.ReadDir(parent)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, err
}

// withoutOlderTests drops an older app's separate <name>test package
// from names, keeping a package whose own name ends in test.
func withoutOlderTests(names []string) []string {
	var kept []string
	for _, name := range names {
		tested, isTests := strings.CutSuffix(name, "test")
		if !(isTests && slices.Contains(names, tested)) {
			kept = append(kept, name)
		}
	}
	return kept
}

// inspectPackages sorts the named directories under dir's internal/
// into those declaring every name in needs and those it could not
// read, each given as "name: reason". An unreadable package might be
// the app, so the caller must not choose around it.
func inspectPackages(dir string, names, needs []string) (declaring,
	unreadable []string, err error) {
	context, err := buildContext(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		files, err := compiledFiles(context,
			filepath.Join(dir, "internal", name))
		if err != nil {
			unreadable = append(unreadable, name+": "+err.Error())
		} else if declaresAll(files, needs) {
			declaring = append(declaring, name)
		}
	}
	return declaring, unreadable, nil
}

// contextFormat has go list print the build context it would compile
// with, one field per line, in the order buildContext reads them.
const contextFormat = "{{context.GOOS}}\n{{context.GOARCH}}\n" +
	"{{context.CgoEnabled}}\n{{context.Compiler}}\n" +
	"{{join context.BuildTags \",\"}}\n{{join context.ToolTags \",\"}}\n" +
	"{{join context.ReleaseTags \",\"}}"

// buildContext asks the go command for the context the loader's go
// run will build with in dir: GOFLAGS tags, GOOS and GOARCH, cgo, and
// the tool and release tags. -find skips resolving imports, which
// keeps it quick and offline.
func buildContext(dir string) (build.Context, error) {
	command := exec.Command("go", "list", "-e", "-find", "-f",
		contextFormat, "./internal")
	command.Dir = dir
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	fields := strings.Split(strings.TrimSpace(string(output)), "\n")
	if err != nil || len(fields) != 7 {
		return build.Context{}, fmt.Errorf("go list could not report "+
			"the build settings in %s. Fix the errors below and try "+
			"again:\n%s", dir, stderr.String())
	}
	context := build.Default
	context.GOOS, context.GOARCH = fields[0], fields[1]
	context.CgoEnabled, context.Compiler = fields[2] == "true", fields[3]
	context.BuildTags = strings.Split(fields[4], ",")
	context.ToolTags = strings.Split(fields[5], ",")
	context.ReleaseTags = strings.Split(fields[6], ",")
	return context, nil
}

// compiledFiles parses the files in dir that a build in context would
// compile into the package. Test files are skipped, since the loader
// never compiles them, so a problem in one changes nothing. A problem
// in any other file is an error rather than a skip, because that file
// might declare what the caller is looking for. So is a file that
// imports C: whether a build keeps it turns on cgo rules this does
// not reproduce, and rastrillo apps are pure Go.
func compiledFiles(context build.Context, dir string) ([]*ast.File,
	error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		if match, err := context.MatchFile(dir, name); err != nil {
			return nil, err
		} else if !match {
			continue
		}
		path := filepath.Join(dir, name)
		if usesCgo, err := importsC(fileSet, path); err != nil {
			return nil, err
		} else if usesCgo {
			return nil, fmt.Errorf("%s imports C, and app package "+
				"detection does not support cgo", name)
		}
		file, err := parser.ParseFile(fileSet, path, nil,
			parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 && file.Name.Name != files[0].Name.Name {
			return nil, fmt.Errorf("%s declares package %s, but the "+
				"other files declare package %s", name, file.Name.Name,
				files[0].Name.Name)
		}
		files = append(files, file)
	}
	return files, nil
}

// importsC reports whether the file at path imports C, in either
// spelling, reading no further than its imports so that the rest of a
// file a build might leave out cannot fail the check.
func importsC(fileSet *token.FileSet, path string) (bool, error) {
	file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, spec := range file.Imports {
		if importPath, err := strconv.Unquote(spec.Path.Value); err == nil &&
			importPath == "C" {
			return true, nil
		}
	}
	return false, nil
}

// declaresAll reports whether files declare every name as a
// package-level var.
func declaresAll(files []*ast.File, names []string) bool {
	declared := map[string]bool{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				for _, identifier := range spec.(*ast.ValueSpec).Names {
					declared[identifier.Name] = true
				}
			}
		}
	}
	for _, name := range names {
		if !declared[name] {
			return false
		}
	}
	return true
}
