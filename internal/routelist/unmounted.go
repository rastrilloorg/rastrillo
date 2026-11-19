package routelist

import (
	"context"
	"go/ast"

	"amadan.net/rastrillo/rastrillo/action"
)

// Unmounted is every action in the app's module that no route mounts:
// each function or method taking one *action.Visit and returning
// nothing, in the packages go list ./... builds. They are compared by
// full identity, so two methods called Show on different types stay
// apart, and a Handle given an action's name mounts nothing.
//
// Two limits come from runtime.FuncForPC. A promoted method is named
// for the embedded type that declares it, which is the declaration
// found here, so it counts. A method value taken through an interface
// is named for the interface, so the concrete method counts as not
// mounted.
func Unmounted(
	ctx context.Context, target Target, routes []action.Route,
) ([]string, error) {
	mounted := map[string]bool{}
	for _, route := range routes {
		mounted[route.Action] = true
	}
	modFlag, err := goSettings(ctx, target.ModuleDir)
	var packages []listedPackage
	if err == nil {
		packages, err = goList(ctx, target.ModuleDir, modFlag, "./...")
	}
	if ctx.Err() != nil {
		// The table is already printed, so goList's message, that the
		// routes were not listed, would be wrong.
		return nil, stopped{stoppedBeforeChecking, ctx.Err()}
	}
	if err != nil {
		return nil, err
	}
	var unmounted []string
	for _, listed := range packages {
		files, err := listed.parse()
		if err != nil {
			return nil, err
		}
		for _, identity := range actionsIn(listed, files) {
			if !mounted[identity] {
				unmounted = append(unmounted, identity)
			}
		}
	}
	return unmounted, nil
}

// actionsIn names each action as runtime.FuncForPC names the function
// a route mounts. A main package is named "main" there, not by its
// import path.
func actionsIn(listed listedPackage, files []sourceFile) []string {
	qualifier := listed.ImportPath
	if listed.Name == "main" {
		qualifier = "main"
	}
	var identities []string
	for _, file := range files {
		for _, declaration := range file.syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && file.isAction(function.Type) {
				identities = append(identities,
					qualifier+"."+identityIn(function))
			}
		}
	}
	return identities
}

// isAction is an action's shape: exactly one parameter, a *Visit of the
// action package, and no result.
func (file sourceFile) isAction(function *ast.FuncType) bool {
	if function.Results.NumFields() > 0 {
		return false
	}
	parameters := parameterTypes(function.Params)
	return len(parameters) == 1 && file.isPointerTo(parameters[0], "Visit")
}

// identityIn is the function's name after its package: "show",
// "(*Tags).Show", "Report.Show", and "[...]" for type parameters, as
// the runtime writes them.
func identityIn(function *ast.FuncDecl) string {
	name := function.Name.Name
	if function.Type.TypeParams != nil {
		name += "[...]"
	}
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return name
	}
	receiver := ast.Unparen(function.Recv.List[0].Type)
	pointer, isPointer := receiver.(*ast.StarExpr)
	if isPointer {
		receiver = ast.Unparen(pointer.X)
	}
	typeName := receiverName(receiver)
	if isPointer {
		return "(*" + typeName + ")." + name
	}
	return typeName + "." + name
}

func receiverName(receiver ast.Expr) string {
	switch typed := receiver.(type) {
	case *ast.IndexExpr:
		return receiverName(typed.X) + "[...]"
	case *ast.IndexListExpr:
		return receiverName(typed.X) + "[...]"
	case *ast.Ident:
		return typed.Name
	}
	return ""
}
