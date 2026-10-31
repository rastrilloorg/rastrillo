package routelist

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
)

// foundRoutes is one Routes function: the target it gives when it has
// the shape to call, or else misfit, its signature as written.
type foundRoutes struct {
	target Target
	misfit string
}

// findRoutes looks only at package-level functions. A method called
// Routes, such as the router an app built before the route table, is
// some other thing and is not reported.
func findRoutes(files []sourceFile) []foundRoutes {
	declared := declaredTypes(files)
	var found []foundRoutes
	for _, file := range files {
		for _, declaration := range file.syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil ||
				function.Name.Name != "Routes" {
				continue
			}
			target, fits := file.callable(function.Type, declared)
			if !fits {
				found = append(found, foundRoutes{
					misfit: signature(file.fileSet, function),
				})
				continue
			}
			found = append(found, foundRoutes{target: target})
		}
	}
	return found
}

// callable checks the contract: no type parameters, no results, and
// exactly (*action.Routes, T) or (*action.Routes, *T), where T is an
// exported type of this package. The listing program calls Routes from
// outside the package and declares the T it passes, so an unexported
// or foreign T could not be written there.
func (file sourceFile) callable(
	function *ast.FuncType, declared map[string]bool,
) (Target, bool) {
	if function.TypeParams != nil || function.Results.NumFields() > 0 {
		return Target{}, false
	}
	parameters := parameterTypes(function.Params)
	if len(parameters) != 2 ||
		!file.isPointerTo(parameters[0], "Routes") {
		return Target{}, false
	}
	appType := ast.Unparen(parameters[1])
	pointer, isPointer := appType.(*ast.StarExpr)
	if isPointer {
		appType = ast.Unparen(pointer.X)
	}
	name, ok := appType.(*ast.Ident)
	if !ok || !name.IsExported() || !declared[name.Name] {
		return Target{}, false
	}
	return Target{AppType: name.Name, Pointer: isPointer}, true
}

// parameterTypes gives one type per parameter: (a, b T) is two.
func parameterTypes(fields *ast.FieldList) []ast.Expr {
	var types []ast.Expr
	for _, field := range fields.List {
		for range max(len(field.Names), 1) {
			types = append(types, field.Type)
		}
	}
	return types
}

func declaredTypes(files []sourceFile) map[string]bool {
	declared := map[string]bool{}
	for _, file := range files {
		for _, declaration := range file.syntax.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				declared[spec.(*ast.TypeSpec).Name.Name] = true
			}
		}
	}
	return declared
}

// signature is the function as its author wrote it, without its body
// or comment, so the message shows them the line they will change.
func signature(fileSet *token.FileSet, function *ast.FuncDecl) string {
	var written bytes.Buffer
	format.Node(&written, fileSet, &ast.FuncDecl{
		Name: function.Name,
		Type: function.Type,
	})
	return written.String()
}
