package codelimits

import (
	"go/ast"
	"go/token"
	"slices"
	"unicode"
	"unicode/utf8"
)

// testingTypes are the types that excuse a one-letter parameter, the
// way every Go test already names them.
var testingTypes = map[string][]string{
	"t": {"*testing.T", "testing.TB"},
	"b": {"*testing.B"},
	"f": {"*testing.F"},
}

// nameOffences finds every one-letter name the file declares, keyed by
// the top-level function holding it, or "-" at package level.
func (unit *unit) nameOffences() []Offence {
	var found []Offence
	for _, decl := range unit.file.Decls {
		scan := &namer{unit: unit, enclosing: "-"}
		if function, ok := decl.(*ast.FuncDecl); ok {
			scan.enclosing = declName(function)
		}
		ast.Inspect(decl, scan.visit)
		found = append(found, scan.found...)
	}
	return found
}

type namer struct {
	unit      *unit
	enclosing string
	found     []Offence
}

func (scan *namer) visit(node ast.Node) bool {
	switch typed := node.(type) {
	case *ast.ValueSpec:
		scan.idents(typed.Names...)
	case *ast.AssignStmt:
		if typed.Tok == token.DEFINE {
			scan.defined(typed)
		}
	case *ast.RangeStmt:
		if typed.Tok == token.DEFINE {
			scan.idents(asIdent(typed.Key), asIdent(typed.Value))
		}
	case *ast.FuncDecl:
		scan.fields(typed.Recv, false)
		scan.receiverTypeParameters(typed.Recv)
	case *ast.FuncType:
		scan.fields(typed.Params, true)
		scan.fields(typed.Results, false)
		scan.typeParameters(typed.TypeParams)
	case *ast.TypeSpec:
		scan.typeParameters(typed.TypeParams)
	}
	return true
}

func asIdent(expr ast.Expr) *ast.Ident {
	ident, _ := expr.(*ast.Ident)
	return ident
}

// defined counts only the names a := brings into being. In `a, err :=`
// where err already exists, := assigns it, and the parser's resolution
// says so: an identifier it declared points back at this statement.
func (scan *namer) defined(assign *ast.AssignStmt) {
	for _, left := range assign.Lhs {
		ident := asIdent(left)
		if ident != nil && ident.Obj != nil && ident.Obj.Decl == assign {
			scan.idents(ident)
		}
	}
}

func (scan *namer) idents(idents ...*ast.Ident) {
	for _, ident := range idents {
		if ident != nil && isOneLetter(ident.Name) {
			scan.add(ident)
		}
	}
}

func isOneLetter(name string) bool {
	return name != "_" && utf8.RuneCountInString(name) == 1
}

// fields checks the names in a parameter, receiver or result list.
func (scan *namer) fields(list *ast.FieldList, mayBeTesting bool) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		for _, ident := range field.Names {
			if mayBeTesting && scan.unit.isTestingName(ident.Name, field.Type) {
				continue
			}
			scan.idents(ident)
		}
	}
}

// typeParameters allows one capital letter, the convention for Opt[T].
func (scan *namer) typeParameters(list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		for _, ident := range field.Names {
			scan.typeParameter(ident)
		}
	}
}

// receiverTypeParameters checks the T in func (box Box[T]): the
// receiver declares it afresh, so a lowercase t there is a new name,
// not a use of the one the type was declared with.
func (scan *namer) receiverTypeParameters(list *ast.FieldList) {
	if list == nil || len(list.List) == 0 {
		return
	}
	receiver := list.List[0].Type
	if pointer, ok := unparen(receiver).(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	var parameters []ast.Expr
	switch typed := unparen(receiver).(type) {
	case *ast.IndexExpr:
		parameters = []ast.Expr{typed.Index}
	case *ast.IndexListExpr:
		parameters = typed.Indices
	}
	for _, parameter := range parameters {
		if ident := asIdent(parameter); ident != nil {
			scan.typeParameter(ident)
		}
	}
}

func (scan *namer) typeParameter(ident *ast.Ident) {
	letter, _ := utf8.DecodeRuneInString(ident.Name)
	if isOneLetter(ident.Name) && !unicode.IsUpper(letter) {
		scan.add(ident)
	}
}

func (scan *namer) add(ident *ast.Ident) {
	scan.found = append(scan.found, Offence{
		Kind: Names, Path: scan.unit.path,
		Key:  scan.enclosing + "/" + ident.Name,
		Size: 1, Line: scan.unit.line(ident.Pos()),
	})
}

func (unit *unit) isTestingName(name string, expr ast.Expr) bool {
	return slices.Contains(testingTypes[name], unit.typeText(expr))
}

// typeText spells a parameter's type with the testing package under its
// real name, however the file imports it.
func (unit *unit) typeText(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.StarExpr:
		return "*" + unit.typeText(typed.X)
	case *ast.SelectorExpr:
		qualifier := asIdent(typed.X)
		if qualifier != nil && unit.imports[qualifier.Name] == "testing" {
			return "testing." + typed.Sel.Name
		}
	}
	return ""
}
