package generate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"html/template"
	"strconv"

	rastrillo "github.com/carlosframework/rastrillo"
)

// This file extracts the static shape of a .go manifest — a top-level
// `var X = rastrillo.Resource{...}` in the app's manifest package — by
// AST, the same technique the action pipeline already uses for package
// clauses. Function values (Column.Render) stay runtime: the extraction
// only records that one exists, and the generated actions reference the
// app's own manifest.X value, so the closure runs unmodified.

// hasRenderMarker stands in for an extracted Render func so the static
// spec passes the same validation the runtime value will.
func hasRenderMarker(map[string]any) template.HTML { return "" }

// extractGoManifests parses one manifest .go file and returns a spec
// per rastrillo.Resource var it declares.
func extractGoManifests(path string) ([]ResourceSpec, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var specs []ResourceSpec
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok || !isRastrilloType(lit.Type, "Resource") {
				continue
			}
			res, err := extractResource(lit)
			if err != nil {
				return nil, fmt.Errorf("%s: var %s: %w", path, vs.Names[0].Name, err)
			}
			specs = append(specs, ResourceSpec{
				Res: res, VarName: vs.Names[0].Name, SourceFile: path, FromGo: true,
			})
		}
	}
	return specs, nil
}

func isRastrilloType(expr ast.Expr, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}

func extractResource(lit *ast.CompositeLit) (rastrillo.Resource, error) {
	var res rastrillo.Resource
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return res, fmt.Errorf("Resource literal must use Field: value form")
		}
		key := keyName(kv.Key)
		var err error
		switch key {
		case "Name":
			res.Name, err = stringLit(kv.Value)
		case "Route":
			res.Route, err = stringLit(kv.Value)
		case "Store":
			res.Store, err = storeLit(kv.Value)
		case "List":
			res.List, err = extractList(kv.Value)
		case "Form":
			res.Form, err = extractForm(kv.Value)
		case "Delete":
			res.Delete, err = extractDelete(kv.Value)
		default:
			err = fmt.Errorf("unknown Resource field %s", key)
		}
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func keyName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func stringLit(e ast.Expr) (string, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", fmt.Errorf("want a string literal (the generator reads manifests statically)")
	}
	return strconv.Unquote(bl.Value)
}

func storeLit(e ast.Expr) (rastrillo.StoreKind, error) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return 0, fmt.Errorf("Store must be rastrillo.Exclusive or rastrillo.Mergeable")
	}
	switch sel.Sel.Name {
	case "Exclusive":
		return rastrillo.Exclusive, nil
	case "Mergeable":
		return rastrillo.Mergeable, nil
	}
	return 0, fmt.Errorf("unknown store rastrillo.%s", sel.Sel.Name)
}

func kindLit(e ast.Expr) (rastrillo.Kind, error) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return 0, fmt.Errorf("Kind must be a rastrillo.<Kind> constant")
	}
	for _, name := range []string{"text", "longtext", "bool", "time", "money", "meter", "blob", "select"} {
		if k, _ := rastrillo.KindByName(name); k.GoName() == sel.Sel.Name {
			return k, nil
		}
	}
	return 0, fmt.Errorf("unknown kind rastrillo.%s", sel.Sel.Name)
}

func boolLit(e ast.Expr) (bool, error) {
	id, ok := e.(*ast.Ident)
	if !ok || (id.Name != "true" && id.Name != "false") {
		return false, fmt.Errorf("want true or false")
	}
	return id.Name == "true", nil
}

func stringsLit(e ast.Expr) ([]string, error) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("want a []string literal")
	}
	var out []string
	for _, el := range cl.Elts {
		s, err := stringLit(el)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func extractList(e ast.Expr) (rastrillo.List, error) {
	var l rastrillo.List
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return l, fmt.Errorf("List must be a rastrillo.List literal")
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		var err error
		switch keyName(kv.Key) {
		case "Columns":
			l.Columns, err = extractColumns(kv.Value)
		case "Search":
			l.Search, err = boolLit(kv.Value)
		case "Filter":
			l.Filter, err = stringsLit(kv.Value)
		}
		if err != nil {
			return l, err
		}
	}
	return l, nil
}

func extractColumns(e ast.Expr) ([]rastrillo.Column, error) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("Columns must be a []rastrillo.Column literal")
	}
	var out []rastrillo.Column
	for _, el := range cl.Elts {
		colLit, ok := el.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("each Column must be a literal")
		}
		var c rastrillo.Column
		for _, cel := range colLit.Elts {
			kv, ok := cel.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			var err error
			switch keyName(kv.Key) {
			case "Field":
				c.Field, err = stringLit(kv.Value)
			case "Kind":
				c.Kind, err = kindLit(kv.Value)
			case "Render":
				// A function value — runtime-only. The marker keeps the
				// static validation honest; the real closure runs from
				// the app's own manifest value.
				c.Render = hasRenderMarker
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func extractForm(e ast.Expr) (rastrillo.Form, error) {
	var f rastrillo.Form
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return f, fmt.Errorf("Form must be a rastrillo.Form literal")
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		var err error
		switch keyName(kv.Key) {
		case "Basics":
			f.Basics, err = extractFields(kv.Value)
		case "Advanced":
			f.Advanced, err = extractFields(kv.Value)
		}
		if err != nil {
			return f, err
		}
	}
	return f, nil
}

func extractFields(e ast.Expr) ([]rastrillo.Field, error) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("want a []rastrillo.Field literal")
	}
	var out []rastrillo.Field
	for _, el := range cl.Elts {
		fLit, ok := el.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("each Field must be a literal")
		}
		var f rastrillo.Field
		for _, fel := range fLit.Elts {
			kv, ok := fel.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			var err error
			switch keyName(kv.Key) {
			case "Name":
				f.Name, err = stringLit(kv.Value)
			case "Kind":
				f.Kind, err = kindLit(kv.Value)
			case "Required":
				f.Required, err = boolLit(kv.Value)
			case "Derived":
				f.Derived, err = boolLit(kv.Value)
			case "Options":
				f.Options, err = stringsLit(kv.Value)
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, f)
	}
	return out, nil
}

func extractDelete(e ast.Expr) (rastrillo.Delete, error) {
	var d rastrillo.Delete
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return d, fmt.Errorf("Delete must be a rastrillo.Delete literal")
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if keyName(kv.Key) == "Confirm" {
			var err error
			if d.Confirm, err = stringLit(kv.Value); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

