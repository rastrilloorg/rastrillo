package routelist

import (
	"context"
	"go/ast"
)

// opPackage is where Define, which declares a named operation, is.
const opPackage = "amadan.net/rastrillo/rastrillo/op"

// DeclaresOperations reports whether a file the app's build compiles
// calls op.Define, which is when Check holds every route to the marking
// rule.
func DeclaresOperations(ctx context.Context, target Target) (bool, error) {
	packages, err := modulePackages(ctx, target)
	if err != nil {
		return false, err
	}
	for _, listed := range packages {
		files, err := listed.parse()
		if err != nil {
			return false, err
		}
		for _, file := range files {
			if file.callsDefine() {
				return true, nil
			}
		}
	}
	return false, nil
}

// callsDefine looks for Define by the name the file imports the op
// package under, so another package's Define does not count.
func (file sourceFile) callsDefine() bool {
	names := importNames(file.syntax, opPackage)
	found := false
	ast.Inspect(file.syntax, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && isDefine(call.Fun, names) {
			found = true
		}
		return !found
	})
	return found
}

// isDefine looks through explicit type arguments, as in
// op.Define[Input, Note](...).
func isDefine(function ast.Expr, names map[string]bool) bool {
	switch typed := ast.Unparen(function).(type) {
	case *ast.IndexExpr:
		return isDefine(typed.X, names)
	case *ast.IndexListExpr:
		return isDefine(typed.X, names)
	}
	return refersTo(function, names, "Define")
}
