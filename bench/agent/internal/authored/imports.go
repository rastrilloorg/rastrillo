package authored

import (
	"go/ast"
	"path"
	"strconv"
)

// actionPackage is where an action's *Visit comes from, as in codelimits.
const actionPackage = "amadan.net/rastrillo/rastrillo/action"

func importNames(file *ast.File) map[string]string {
	names := map[string]string{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name != nil {
			names[spec.Name.Name] = importPath
		} else {
			names[path.Base(importPath)] = importPath
		}
	}
	return names
}

// dotImports is whether the file imports wanted with a dot, as in
// codelimits: then an unqualified Visit is the action package's.
func dotImports(file *ast.File, wanted string) bool {
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err == nil && importPath == wanted &&
			spec.Name != nil && spec.Name.Name == "." {
			return true
		}
	}
	return false
}

// isAction mirrors codelimits: one *Visit parameter from the action
// package, and no result. An agent's app is never the action package,
// so an unqualified Visit is its only where the file dot-imports it.
func (read *reader) isAction(function *ast.FuncType) bool {
	if function.Results != nil && len(function.Results.List) > 0 {
		return false
	}
	parameters := function.Params.List
	if len(parameters) != 1 || len(parameters[0].Names) > 1 {
		return false
	}
	pointer, ok := unparen(parameters[0].Type).(*ast.StarExpr)
	if !ok {
		return false
	}
	switch typed := unparen(pointer.X).(type) {
	case *ast.Ident:
		return typed.Name == "Visit" && dotImports(read.file, actionPackage)
	case *ast.SelectorExpr:
		qualifier, ok := typed.X.(*ast.Ident)
		return ok && typed.Sel.Name == "Visit" &&
			read.imports[qualifier.Name] == actionPackage
	}
	return false
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}
