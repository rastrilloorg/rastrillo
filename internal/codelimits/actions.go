package codelimits

import "go/ast"

// actionPackage is where an action's *Visit comes from. The gate holds
// an action to 15 lines because that is all one should need when
// Visit does the common steps.
const actionPackage = "amadan.net/rastrillo/rastrillo/action"

// isAction reports whether a function type is an action: exactly one
// parameter, a *Visit from the action package, and no result. Only the
// import path says so. A package that is merely named action, in
// another module, has a Visit that is not this one.
func (unit *unit) isAction(function *ast.FuncType) bool {
	if function.Results != nil && len(function.Results.List) > 0 {
		return false
	}
	parameters := function.Params.List
	if len(parameters) != 1 || len(parameters[0].Names) > 1 {
		return false
	}
	return unit.isVisitPointer(parameters[0].Type)
}

// isVisitPointer looks through parentheses, which gofmt keeps: *(Visit)
// and (*Visit) are the same type, and must not pass for an ordinary
// function and its longer limit.
func (unit *unit) isVisitPointer(expr ast.Expr) bool {
	pointer, ok := unparen(expr).(*ast.StarExpr)
	if !ok {
		return false
	}
	switch typed := unparen(pointer.X).(type) {
	case *ast.Ident:
		// Unqualified, Visit is the action package's only inside it or
		// where it is dot-imported.
		return typed.Name == "Visit" && unit.bareVisit
	case *ast.SelectorExpr:
		qualifier, ok := typed.X.(*ast.Ident)
		return ok && typed.Sel.Name == "Visit" &&
			unit.imports[qualifier.Name] == actionPackage
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

// limitFor is the longest a body may be: an action has less to do.
func (unit *unit) limitFor(function *ast.FuncType) int {
	if unit.isAction(function) {
		return MaxActionLines
	}
	return MaxFuncLines
}
