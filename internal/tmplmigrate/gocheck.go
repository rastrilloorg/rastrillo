package tmplmigrate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// UIPath is the import path whose WithT and FuncsWith bind T per request.
const UIPath = "amadan.net/rastrillo/rastrillo/ui"

// Appendix C.2's op.migrate.* lines for the Go preflight.
const (
	msgTypecheck      = "type-checking %s: %s"
	msgNotConstant    = "%s key %s is not a constant"
	msgNotConstantFix = "make it a constant, or check by hand that it can never be T, Tf, Tbdi, number, displayValue, dateWords or money"
	msgEntry          = "%s %q"
	msgEntryFix       = "remove it: the localizer's catalog now serves %s; give that catalog to ui.Pages as the locale's T"
	kindEntry         = "function map entry"
	kindAssignment    = "function map assignment"
	msgWithT          = "ui.%s (a per-request T binding)"
	msgWithTFix       = "render through ui.Pages with the request's localizer in the data; drop the per-request Clone"
)

// reservedFuncs are the names ui.Pages refuses from an app's function
// map: the locale functions, and localizer and withL, which the
// migrated templates call to find and pass $L. An app registering any
// of them fails boot, so the preflight reports all of them, not only
// the locale functions the template rewrite edits.
var reservedFuncs = func() map[string]bool {
	m := map[string]bool{"localizer": true, "withL": true}
	for name := range localeFuncs {
		m[name] = true
	}
	return m
}()

// GoFinding is one Go site the template migration cannot fix by editing
// templates. A Review finding is one the check could not decide: a
// function-map key that is not a constant string. Review findings fail
// the dry run like any other.
type GoFinding struct {
	Pos    string // file:line, relative to the scanned directory
	What   string
	Fix    string
	Review bool
}

// CheckGo type-checks the Go packages under dir (go/packages, so the
// app's own module, build constraints and imports decide) and reports:
//
//   - every key written into a template function map: any composite
//     literal or index assignment whose TYPE is template.FuncMap (text
//     or html), however the map was created (a literal, make, ui.Funcs,
//     a helper's parameter); and any map[string]any that flows into a
//     template's Funcs, ui.FuncsWith or ui.WithT, directly or through
//     helper functions in the package. A key counts as resolved only
//     when the type checker gives it a constant string value
//     (types.Info.Types[key].Value), which handles shadowing,
//     conversions and constant expressions exactly. A resolved key in
//     reservedFuncs is a finding; any key that is not a constant is a
//     review finding.
//   - every call of ui.WithT or ui.FuncsWith, identified by the object
//     the call resolves to, so aliases, dot-imports and a different
//     package (or a local variable) named ui are all handled.
//
// This is a preflight: advisory. The guarantee is at run time, where
// ui.Pages refuses an app function named after a locale function,
// localizer or withL, and strict dispatch fails a call without a localizer.
func CheckGo(dir string) ([]GoFinding, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports,
		Dir: dir,
		Env: append(os.Environ(), "GOFLAGS=-mod=mod"),
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	var problems []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			problems = append(problems, e.Error())
		}
	})
	if len(problems) > 0 {
		return nil, fmt.Errorf(msgTypecheck, dir, strings.Join(problems, "; "))
	}
	abs, _ := filepath.Abs(dir)
	var out []GoFinding
	for _, p := range pkgs {
		out = append(out, checkPackage(p, abs)...)
	}
	sort.Slice(out, func(i, j int) bool { return lessPos(out[i].Pos, out[j].Pos) })
	return out, nil
}

func lessPos(a, b string) bool {
	fa, la, _ := strings.Cut(a, ":")
	fb, lb, _ := strings.Cut(b, ":")
	if fa != fb {
		return fa < fb
	}
	na, _ := strconv.Atoi(la)
	nb, _ := strconv.Atoi(lb)
	return na < nb
}

func isFuncMap(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Name() != "FuncMap" {
		return false
	}
	p := n.Obj().Pkg().Path()
	return p == "text/template" || p == "html/template"
}

func isAnyMap(t types.Type) bool {
	if t == nil || isFuncMap(t) {
		return false
	}
	m, ok := types.Unalias(t).Underlying().(*types.Map)
	if !ok {
		return false
	}
	k, ok := m.Key().Underlying().(*types.Basic)
	iface, isIface := m.Elem().Underlying().(*types.Interface)
	return ok && k.Kind() == types.String && isIface && iface.Empty()
}

func callee(info *types.Info, call *ast.CallExpr) *types.Func {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		fn, _ := info.Uses[f].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		fn, _ := info.Uses[f.Sel].(*types.Func)
		return fn
	}
	return nil
}

// isSink reports a call that registers template functions: a Template's
// Funcs method (text or html), or ui.FuncsWith / ui.WithT.
func isSink(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() == UIPath && (fn.Name() == "FuncsWith" || fn.Name() == "WithT") {
		return true
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || fn.Name() != "Funcs" {
		return false
	}
	p := fn.Pkg().Path()
	return p == "text/template" || p == "html/template"
}

func objOf(info *types.Info, e ast.Expr) types.Object {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		if o := info.Uses[id]; o != nil {
			return o
		}
		return info.Defs[id]
	}
	return nil
}

func checkPackage(p *packages.Package, root string) []GoFinding {
	info := p.TypesInfo
	rel := func(n ast.Node) string {
		pos := p.Fset.Position(n.Pos())
		r, err := filepath.Rel(root, pos.Filename)
		if err != nil {
			r = pos.Filename
		}
		return fmt.Sprintf("%s:%d", filepath.ToSlash(r), pos.Line)
	}

	// Which map[string]any values flow into a sink: objects passed to
	// one, literals passed directly, and, to a fixpoint, the parameters
	// of package functions those objects are passed to.
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if fn, ok := info.Defs[fd.Name].(*types.Func); ok {
					decls[fn] = fd
				}
			}
		}
	}
	flowObj := map[types.Object]bool{}
	flowLit := map[*ast.CompositeLit]bool{}
	var calls []*ast.CallExpr
	for _, f := range p.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				calls = append(calls, c)
			}
			return true
		})
	}
	for _, c := range calls {
		if !isSink(callee(info, c)) {
			continue
		}
		for _, a := range c.Args {
			if !isAnyMap(info.TypeOf(a)) {
				continue
			}
			if lit, ok := ast.Unparen(a).(*ast.CompositeLit); ok {
				flowLit[lit] = true
			} else if o := objOf(info, a); o != nil {
				flowObj[o] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, c := range calls {
			fd := decls[callee(info, c)]
			if fd == nil {
				continue
			}
			var params []types.Object
			for _, field := range fd.Type.Params.List {
				for _, n := range field.Names {
					params = append(params, info.Defs[n])
				}
			}
			for i, a := range c.Args {
				if i < len(params) && params[i] != nil && flowObj[objOf(info, a)] && !flowObj[params[i]] {
					flowObj[params[i]] = true
					changed = true
				}
			}
		}
	}
	// A literal initialising a flowing variable flows too.
	for _, f := range p.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					if i < len(x.Rhs) && flowObj[objOf(info, l)] {
						if lit, ok := ast.Unparen(x.Rhs[i]).(*ast.CompositeLit); ok {
							flowLit[lit] = true
						}
					}
				}
			case *ast.ValueSpec:
				for i, n := range x.Names {
					if i < len(x.Values) && flowObj[info.Defs[n]] {
						if lit, ok := ast.Unparen(x.Values[i]).(*ast.CompositeLit); ok {
							flowLit[lit] = true
						}
					}
				}
			}
			return true
		})
	}

	var out []GoFinding
	key := func(at ast.Node, k ast.Expr, kind string) {
		tv, ok := info.Types[k]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			out = append(out, GoFinding{Pos: rel(at), Review: true,
				What: fmt.Sprintf(msgNotConstant, kind, types.ExprString(k)),
				Fix:  msgNotConstantFix})
			return
		}
		if name := constant.StringVal(tv.Value); reservedFuncs[name] {
			out = append(out, GoFinding{Pos: rel(at), What: fmt.Sprintf(msgEntry, kind, name),
				Fix: fmt.Sprintf(msgEntryFix, name)})
		}
	}
	for _, f := range p.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				t := info.TypeOf(x)
				if isFuncMap(t) || isAnyMap(t) && flowLit[x] {
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							key(kv, kv.Key, kindEntry)
						}
					}
				}
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					ix, ok := ast.Unparen(l).(*ast.IndexExpr)
					if !ok {
						continue
					}
					t := info.TypeOf(ix.X)
					if isFuncMap(t) || isAnyMap(t) && flowObj[objOf(info, ix.X)] {
						key(x, ix.Index, kindAssignment)
					}
				}
			case *ast.CallExpr:
				if fn := callee(info, x); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == UIPath &&
					(fn.Name() == "WithT" || fn.Name() == "FuncsWith") {
					out = append(out, GoFinding{Pos: rel(x), What: fmt.Sprintf(msgWithT, fn.Name()),
						Fix: msgWithTFix})
				}
			}
			return true
		})
	}
	return out
}
