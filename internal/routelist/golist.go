package routelist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strconv"
)

// listedPackage is the part of go list's answer that is read here.
type listedPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Module     *struct{ Path, Dir string }
}

// goList asks the go command which packages match pattern in dir. It
// inherits the environment and adds only modFlag (goSettings'), the
// flag the build adds too, so GOFLAGS, tags and GOOS choose the same
// files here as they do when the program is compiled. Either -mod
// fails outside module mode, so every package listed has a Module.
// It runs in a group of its own, as the build does, so a cancelled ctx
// reads as a stop, not as go list failing of the signal.
func goList(
	ctx context.Context, dir, modFlag, pattern string,
) ([]listedPackage, error) {
	command := inOwnGroup(ctx, "go", "list", modFlag, "-json", pattern)
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.run(); err != nil {
		if ctx.Err() != nil {
			return nil, stopped{stoppedBeforeListing, ctx.Err()}
		}
		return nil, say(goListFailed, dir, reported(&stderr, err))
	}
	var packages []listedPackage
	decoder := json.NewDecoder(&stdout)
	for {
		var listed listedPackage
		err := decoder.Decode(&listed)
		if errors.Is(err, io.EOF) {
			return packages, nil
		}
		if err != nil {
			return nil, say(goListFailed, dir, err)
		}
		packages = append(packages, listed)
	}
}

// sourceFile is one parsed file and the names it gives the action
// package, so action.Visit is known by where "action" points, not by
// what it is called: a package named action in another module has a
// Visit that is not this one.
type sourceFile struct {
	syntax  *ast.File
	fileSet *token.FileSet

	// actionNames holds each name the file imports the action package
	// under; "." when it is dot-imported, so a bare Visit is its.
	actionNames map[string]bool
}

// parse reads the files the build compiles, and no others: test files
// and files for another platform or tag are in neither list. A file
// that imports "C" is in CgoFiles, not GoFiles, and is compiled when
// cgo is on, so a Routes or an action in it counts.
func (listed listedPackage) parse() ([]sourceFile, error) {
	fileSet := token.NewFileSet()
	var files []sourceFile
	for _, name := range slices.Concat(listed.GoFiles, listed.CgoFiles) {
		path := filepath.Join(listed.Dir, name)
		syntax, err := parser.ParseFile(fileSet, path, nil,
			parser.SkipObjectResolution)
		if err != nil {
			return nil, say(unreadableFile, path, err)
		}
		files = append(files, sourceFile{
			syntax:      syntax,
			fileSet:     fileSet,
			actionNames: importNames(syntax, actionPackage),
		})
	}
	return files, nil
}

// importNames holds each name the file imports importPath under, "."
// when it is dot-imported. An import without a name is known by the
// path's last element, which is the package's name for the action and
// op packages, the two looked for here.
func importNames(file *ast.File, importPath string) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil || imported != importPath {
			continue
		}
		if spec.Name != nil {
			names[spec.Name.Name] = true
		} else {
			names[path.Base(importPath)] = true
		}
	}
	return names
}

// isActionType reports whether expr names a type of the action package
// in this file.
func (file sourceFile) isActionType(expr ast.Expr, name string) bool {
	return refersTo(expr, file.actionNames, name)
}

// refersTo reports whether expr is name in the package a file imports
// under names: pkg.name, an alias of it, or a bare name where the
// package is dot-imported.
func refersTo(expr ast.Expr, names map[string]bool, name string) bool {
	switch typed := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return typed.Name == name && names["."]
	case *ast.SelectorExpr:
		qualifier, ok := typed.X.(*ast.Ident)
		return ok && typed.Sel.Name == name && names[qualifier.Name]
	}
	return false
}

// isPointerTo reports whether expr is a pointer to the action package's
// type name. gofmt keeps parentheses, so *(Visit) and (*Visit) are
// looked through.
func (file sourceFile) isPointerTo(expr ast.Expr, name string) bool {
	pointer, ok := ast.Unparen(expr).(*ast.StarExpr)
	return ok && file.isActionType(pointer.X, name)
}
