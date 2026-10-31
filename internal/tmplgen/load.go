package tmplgen

import (
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// generatedDirs is every directory of the module that holds a
// generated file, skipping what the go command skips: nested modules,
// testdata, vendor, and names starting with . or _.
func generatedDirs(moduleDir string) (map[string]bool, error) {
	dirs := map[string]bool{}
	err := filepath.WalkDir(
		moduleDir,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path == moduleDir {
					return nil
				}
				name := entry.Name()
				if name == "testdata" || name == "vendor" ||
					strings.HasPrefix(name, ".") ||
					strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				goMod := filepath.Join(path, "go.mod")
				if _, err := os.Stat(goMod); err == nil {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == GenFile {
				dirs[filepath.Dir(path)] = true
			}
			return nil
		},
	)
	return dirs, err
}

// loadMains loads the main packages patterns name, or every one under
// cmd/, with the packages each imports.
func loadMains(
	moduleDir string,
	patterns []string,
) ([]*packages.Package, error) {
	explicit := len(patterns) > 0
	if !explicit {
		if _, err := os.Stat(filepath.Join(moduleDir, "cmd")); err != nil {
			return nil, nil
		}
		patterns = []string{"./cmd/..."}
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedModule,
		Dir:        moduleDir,
		BuildFlags: buildFlags,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf(
			"tmplgen: listing %s: %w",
			strings.Join(patterns, " "),
			err,
		)
	}
	if err := firstError(pkgs); err != nil {
		return nil, err
	}
	var mains []*packages.Package
	for _, pkg := range pkgs {
		if pkg.Name == "main" {
			mains = append(mains, pkg)
		}
	}
	if explicit && len(mains) == 0 {
		return nil, fmt.Errorf(
			"tmplgen: no main package in %s",
			strings.Join(patterns, " "),
		)
	}
	slices.SortFunc(mains, func(left, right *packages.Package) int {
		return cmp.Compare(left.PkgPath, right.PkgPath)
	})
	return mains, nil
}

// tmplPath is tmpl's import path, the package every set is built by.
var tmplPath = reflect.TypeFor[tmpl.Template]().PkgPath()

// buildsNoSets reports whether root certainly builds no set: every
// package it imports, directly or not, loaded, and none is tmpl. One
// that failed to load may be the one that imports tmpl, and taking such
// a main for one without templates would prune the generated files of
// the packages it imports; it is captured instead, and fails to build.
// Imports are listed with the capture tag, so a generated file does not
// count.
func buildsNoSets(root *packages.Package) bool {
	none := true
	packages.Visit([]*packages.Package{root}, func(pkg *packages.Package) bool {
		if pkg.PkgPath == tmplPath || len(pkg.Errors) > 0 {
			none = false
		}
		return none
	}, nil)
	return none
}

// moduleDeps maps root and every package of the main module it imports to
// its directory.
func moduleDeps(root *packages.Package) map[string]string {
	deps := map[string]string{}
	packages.Visit([]*packages.Package{root}, nil, func(pkg *packages.Package) {
		if pkg.Module != nil && pkg.Module.Main && pkg.Dir != "" {
			deps[pkg.PkgPath] = pkg.Dir
		}
	})
	return deps
}

// loadTypes type-checks each package that owns sets, and each package
// a set's functions name a type from.
func loadTypes(
	moduleDir string,
	sets map[string][]tmpl.CapturedSet,
) (map[string]*packages.Package, error) {
	paths := map[string]bool{}
	for owner, list := range sets {
		paths[owner] = true
		for _, set := range list {
			for _, sig := range set.Funcs {
				for _, ref := range sig.Refs {
					paths[ref.PkgPath] = true
				}
			}
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	// From source, not export data: export data leaves out unexported
	// declarations, and the generated file must see them, both to call a
	// function whose type names one and not to declare a name the package
	// already has.
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedFiles | packages.NeedModule,
		Dir:        moduleDir,
		BuildFlags: buildFlags,
	}
	pkgs, err := packages.Load(cfg, slices.Sorted(maps.Keys(paths))...)
	if err != nil {
		return nil, fmt.Errorf(
			"tmplgen: loading the packages that build template sets: %w",
			err,
		)
	}
	if err := firstError(pkgs); err != nil {
		return nil, err
	}
	byPath := make(map[string]*packages.Package, len(pkgs))
	for _, pkg := range pkgs {
		byPath[pkg.PkgPath] = pkg
	}
	return byPath, nil
}

func firstError(pkgs []*packages.Package) error {
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return fmt.Errorf("tmplgen: %s: %v", pkg.PkgPath, pkg.Errors[0])
		}
	}
	return nil
}
