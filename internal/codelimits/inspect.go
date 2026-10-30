package codelimits

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// unit is one parsed file and what is known about it from outside its
// syntax: where it is, which names its imports give to packages.
type unit struct {
	path string
	test bool
	fset *token.FileSet
	file *ast.File

	// imports maps the name a file uses for a package to its import
	// path, so action.Visit is resolved by where "action" points and not
	// by what it is called.
	imports map[string]string
	// bareVisit is whether an unqualified Visit is the action package's:
	// in that package, or in a file that dot-imports it.
	bareVisit bool

	// generated and data come from the comments before the package
	// clause. A generated file is not measured at all; a data file is
	// not measured for columns or file length.
	generated, data bool
}

var generatedLine = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

const dataMarker = "//rastrillo:data "

func parseUnit(name, module string, source []byte) (*unit, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	unit := &unit{
		path:    name,
		test:    strings.HasSuffix(name, "_test.go"),
		fset:    fset,
		file:    file,
		imports: importNames(file),
	}
	unit.bareVisit = packageImportPath(module, name) == actionPackage ||
		dotImports(file, actionPackage)
	unit.readHeader()
	return unit, nil
}

func packageImportPath(module, name string) string {
	if module == "" {
		return ""
	}
	if directory := path.Dir(name); directory != "." {
		return module + "/" + directory
	}
	return module
}

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

// dotImports is whether the file imports wanted with a dot. It is asked
// apart from importNames, where a second dot import would overwrite the
// first under the one name ".".
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

// readHeader looks only above the package clause, where a build tag, a
// generated-code line or a data marker is meaningful. The same text in
// a function body is just a comment.
func (unit *unit) readHeader() {
	for _, group := range unit.file.Comments {
		if group.Pos() >= unit.file.Package {
			return
		}
		for _, comment := range group.List {
			if generatedLine.MatchString(comment.Text) {
				unit.generated = true
			}
			reason, ok := strings.CutPrefix(comment.Text, dataMarker)
			if ok && strings.TrimSpace(reason) != "" {
				unit.data = true
			}
		}
	}
}

// line is where pos sits in the file as written. A //line directive
// renumbers what Position reports: one before a closing brace made a
// long body measure as negative, and it would send a message to a line
// that is not there.
func (unit *unit) line(pos token.Pos) int {
	return unit.fset.PositionFor(pos, false).Line
}

// declName is how a function is told apart within its file.
func declName(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}
	return receiverName(decl.Recv.List[0].Type) + "." + decl.Name.Name
}

func receiverName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.StarExpr:
		return receiverName(typed.X)
	case *ast.ParenExpr:
		return receiverName(typed.X)
	case *ast.IndexExpr:
		return receiverName(typed.X)
	case *ast.IndexListExpr:
		return receiverName(typed.X)
	case *ast.Ident:
		return typed.Name
	}
	return "?"
}

// functionOffences measures non-test files only: a test names what it
// checks and may take as many lines as it needs to set that up.
func (unit *unit) functionOffences() []Offence {
	if unit.test {
		return nil
	}
	var found []Offence
	seen := numbering{}
	for _, decl := range unit.file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			found = append(found, unit.declaration(seen, typed)...)
		case *ast.GenDecl:
			found = append(found, unit.variables(seen, typed)...)
		}
	}
	return found
}

// numbering tells namesakes apart. Two func init(), or two var _ holding
// callbacks, share a name a record cannot tell apart, so the second and
// later in source order get "~2", "~3". Every holder of a name is
// counted, long or short: numbering only the offenders would hand a
// fixed function's record, and its ceiling, to the namesake after it.
type numbering map[string]int

func (seen numbering) next(name string) string {
	seen[name]++
	if count := seen[name]; count > 1 {
		return name + "~" + strconv.Itoa(count)
	}
	return name
}

// declaration measures a function and, on their own, the action
// literals inside it: an action is short wherever it is written.
func (unit *unit) declaration(seen numbering, decl *ast.FuncDecl) []Offence {
	if decl.Body == nil {
		return nil
	}
	name := seen.next(declName(decl))
	found := unit.bodyOffence(
		name, decl.Name.Pos(), decl.Body, unit.limitFor(decl.Type))
	scan := &literals{unit: unit, enclosing: name}
	ast.Inspect(decl.Body, scan.visit)
	return append(found, scan.found...)
}

func (unit *unit) variables(seen numbering, decl *ast.GenDecl) []Offence {
	if decl.Tok != token.VAR {
		return nil
	}
	var found []Offence
	for _, spec := range decl.Specs {
		value := spec.(*ast.ValueSpec)
		for index, initializer := range value.Values {
			if !holdsLiteral(initializer) {
				continue
			}
			name := value.Names[min(index, len(value.Names)-1)].Name
			found = append(found,
				unit.initializer(seen.next(name), initializer)...)
		}
	}
	return found
}

// holdsLiteral is whether a variable's value has a body to measure. A
// value with none takes no number, so adding var _ Interface = (*T)(nil)
// above a var _ of callbacks does not rename the callbacks' records.
func holdsLiteral(value ast.Expr) bool {
	found := false
	ast.Inspect(value, func(node ast.Node) bool {
		_, isLiteral := node.(*ast.FuncLit)
		found = found || isLiteral
		return !found
	})
	return found
}

// initializer measures a package-level variable's value. A function
// literal that is the whole value is named by the variable; literals
// nested in it are numbered, because a map of callbacks has nothing else
// to tell them apart.
func (unit *unit) initializer(name string, value ast.Expr) []Offence {
	scan := &literals{unit: unit, enclosing: name, variable: true}
	var found []Offence
	if literal, ok := value.(*ast.FuncLit); ok {
		scan.whole = literal
		found = unit.bodyOffence(
			name, literal.Pos(), literal.Body, unit.limitFor(literal.Type))
	}
	ast.Inspect(value, scan.visit)
	return append(found, scan.found...)
}

// bodyOffence counts the lines strictly between the one holding the
// opening brace and the one holding the closing brace, so a signature
// wrapped over several lines costs the body nothing.
func (unit *unit) bodyOffence(
	name string, at token.Pos, body *ast.BlockStmt, limit int,
) []Offence {
	opening := unit.line(body.Lbrace)
	closing := unit.line(body.Rbrace)
	size := closing - opening - 1
	if size <= limit {
		return nil
	}
	return []Offence{{
		Kind: FuncLines, Path: unit.path, Key: name, Size: size,
		Limit: limit, Line: unit.line(at),
	}}
}

// literals finds the function literals worth measuring on their own
// inside one declaration or one variable's value.
type literals struct {
	unit      *unit
	enclosing string
	variable  bool
	whole     *ast.FuncLit
	actions   int
	others    int
	found     []Offence
}

func (scan *literals) visit(node ast.Node) bool {
	literal, ok := node.(*ast.FuncLit)
	if !ok || literal == scan.whole {
		return true
	}
	switch {
	case scan.unit.isAction(literal.Type):
		scan.actions++
		scan.measure(literal, "action", scan.actions, MaxActionLines)
	case scan.variable:
		scan.others++
		scan.measure(literal, "func", scan.others, MaxFuncLines)
	}
	return true
}

func (scan *literals) measure(
	literal *ast.FuncLit, word string, number, limit int,
) {
	name := fmt.Sprintf("%s.%s%d", scan.enclosing, word, number)
	scan.found = append(scan.found,
		scan.unit.bodyOffence(name, literal.Pos(), literal.Body, limit)...)
}
