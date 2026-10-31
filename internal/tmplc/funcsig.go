package tmplc

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// fn is a function of a set's FuncMap as the generated file calls it:
// directly, as a field of its Go type in the file's funcs struct, or,
// when the file cannot spell that type, by reflection.
type fn struct {
	name string
	// field is the funcs struct's field holding it. Two sets whose
	// functions share a name and a type share it.
	field string
	// expr is its type, and params and results what it takes and
	// gives, when direct; variadic means the last of params is the
	// element type of a ...T.
	direct   bool
	expr     string
	params   []string
	variadic bool
	results  int
	// qualifiers are the imports expr needs, by package name.
	qualifiers map[string]string
	// sig is expr as a go/types signature, which types the results of
	// direct calls; nil when it cannot be resolved in the package.
	sig *types.Signature
}

// localName matches the names generated code gives its own variables
// and parameters. A type or package of the same name in a signature
// would be shadowed by them inside a render function.
var localName = regexp.MustCompile(
	`^(b|dot|root|fn|funcs|ok|data|depth|err|v|` +
		`(v|x|a|c|r|ran|key|end|typed)[0-9]+)$`,
)

// fn is how the file calls the function name of a set, whose captured
// signature is sig. Direct calls need every package the type names
// imported under its name, which the package must not declare and no
// other import may hold.
func (generated *file) fn(name string, sig tmpl.FuncSig) *fn {
	key := name + "\x00" + sig.Expr
	if prior, ok := generated.fns[key]; ok {
		return prior
	}
	sum := sha256.Sum256([]byte(sig.Expr))
	out := &fn{name: name, field: name + "_r"}
	if generated.direct(sig) {
		ft, ok := parseFuncType(sig.Expr)
		if ok {
			out.direct = true
			out.field = name + "_" + hex.EncodeToString(sum[:])[:8]
			out.expr = sig.Expr
			out.params, out.variadic = ft.params, ft.variadic
			out.results = ft.results
			out.qualifiers = sig.Qualifiers
			out.sig = generated.funcType(sig.Expr, sig.Qualifiers)
			for qualifier, path := range sig.Qualifiers {
				generated.imports[qualifier] = path
			}
		}
	}
	// A FuncMap name that ends like a field of another cannot take its
	// field.
	for generated.fields[out.field] != "" {
		out.field += "_"
	}
	generated.fields[out.field] = key
	generated.fns[key] = out
	return out
}

// direct reports whether generated code can name sig's type.
func (generated *file) direct(sig tmpl.FuncSig) bool {
	if !sig.Nameable || sig.Expr == "" {
		return false
	}
	if !generated.qualifiersFree(sig.Qualifiers) {
		return false
	}
	expr, err := parser.ParseExpr(sig.Expr)
	if err != nil {
		return false
	}
	ok := true
	own := map[string]bool{}
	for _, ref := range sig.Refs {
		if ref.PkgPath == generated.in.PkgPath ||
			ref.PkgPath == "main" && generated.in.PkgName == "main" {
			own[ref.Name] = true
		}
	}
	// The bare names in the type are the package's own types and the
	// predeclared ones. A local variable must not shadow either, nor
	// may the package declare a name that shadows a predeclared one.
	ast.Inspect(expr, func(node ast.Node) bool {
		switch concrete := node.(type) {
		case *ast.SelectorExpr:
			return false // a qualified name, checked above
		case *ast.Ident:
			shadowed := !own[concrete.Name] &&
				generated.declared(concrete.Name)
			if localName.MatchString(concrete.Name) || shadowed {
				ok = false
			}
		}
		return true
	})
	return ok
}

// qualifiersFree reports whether the file can import each package of
// qualifiers under its name: none is a local name or the package's
// own, nor imported already under that name from elsewhere.
func (generated *file) qualifiersFree(qualifiers map[string]string) bool {
	for qualifier, path := range qualifiers {
		if localName.MatchString(qualifier) || generated.declared(qualifier) {
			return false
		}
		if prior, taken := generated.imports[qualifier]; taken &&
			prior != path {
			return false
		}
	}
	return true
}

type funcType struct {
	params   []string
	variadic bool
	results  int
}

// parseFuncType reads a function type written as Go source: the type
// of each parameter, as source, and how many results it has.
func parseFuncType(src string) (funcType, bool) {
	expr, err := parser.ParseExpr(src)
	if err != nil {
		return funcType{}, false
	}
	ft, ok := expr.(*ast.FuncType)
	if !ok {
		return funcType{}, false
	}
	var out funcType
	for _, field := range ft.Params.List {
		typ := field.Type
		if ellipsis, ok := typ.(*ast.Ellipsis); ok {
			out.variadic = true
			typ = ellipsis.Elt
		}
		count := max(len(field.Names), 1)
		for range count {
			out.params = append(out.params, source(typ))
		}
	}
	if ft.Results != nil {
		for _, field := range ft.Results.List {
			out.results += max(len(field.Names), 1)
		}
	}
	return out, true
}

func source(node ast.Node) string {
	var builder strings.Builder
	printer.Fprint(&builder, token.NewFileSet(), node)
	return builder.String()
}
