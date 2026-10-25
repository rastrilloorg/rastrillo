// Package lint is the parity check `rastrillo generate --check` runs
// over hand-written code, so a page and the API door beside it cannot
// quietly drift apart (spec §7). It reads two kinds of file:
//
//   - A hand-written operation, ops/<resource>/<op>.go, which replaces
//     the generated one. Every exported function in it must begin with
//     the op.Check guard, exactly as the generated operation does, and
//     the file may not run code before that guard through a
//     package-level var. Authorization lives in op.Check, so an
//     operation that skips it is open to every caller.
//   - A hand-written action that overrides a generated door of an
//     exposed resource. It must call the operation that door calls
//     (List, Get, Create, Update or Delete), unconditionally, and use
//     the error. An action with no API twin says why with
//     `//rastrillo:page-only <reason>` above its package clause.
//
// It is a lint, not a proof. It does not catch:
//
//   - a write the action makes after the operation returns, such as a
//     second store call the API door never makes;
//   - a handler registered outside actions/, such as a chi handler the
//     app mounts by hand, which overrides nothing and so is never read.
//
// Those are why authorization lives in op.Check and not in the doors:
// a path the lint cannot see still goes through the operation's check
// when it calls the operation at all, and section 8's behavioural tests
// catch the rest.
package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate"
	"amadan.net/rastrillo/rastrillo/internal/generate/opsgen"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
)

// Finding is one place a hand-written file is off its operation.
type Finding struct {
	File string // relative to the app root: actions/... or ops/...
	Line int
	Msg  string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Msg) }

// The findings, as the operator reads them. Each says what to do.
const (
	msgGuard     = "%s: begin with `if err := op.Check(...); err != nil { return ..., err }`, so this operation enforces the same rules as the generated one"
	msgVar       = "var %s: no work may run before the guard. Give it its value inside the operation, after op.Check"
	msgCall      = "call %s.%s from Handle, at the top level, and use its error, so this page and the API do the same thing; or say why it has no API twin with `//rastrillo:page-only <reason>` above the package clause"
	msgBranch    = "%s.%s runs only in a branch. Call it at the top level of Handle, so every request goes through the operation"
	msgErrUnused = "use the error %s.%s returns. A refusal must reach the page"
	msgReason    = "say why this action has no API twin: `//rastrillo:page-only <reason>`"
)

// opImportPath is the package every hand operation takes op.Check from.
const opImportPath = "amadan.net/rastrillo/rastrillo/op"

// pageOnly is the directive that exempts an action from the door rule.
const pageOnly = "//rastrillo:page-only"

// Parity checks every hand-written operation under appRoot/ops and
// every hand-written override of an exposed resource's door under
// appRoot/actions. Findings are sorted by file and line. A file that
// does not parse is an error, not a finding: the compiler will say
// where, and --check must not add a second, vaguer report.
func Parity(appRoot string, rs []rastrillo.Resource) ([]Finding, error) {
	var out []Finding
	hand, err := opsgen.HandFiles(appRoot, rs)
	if err != nil {
		return nil, err
	}
	for _, h := range hand {
		f, err := parse(appRoot, h.Source())
		if err != nil {
			return nil, err
		}
		out = append(out, checkOperation(f)...)
	}

	module, err := shape.ModulePath(appRoot)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		if !r.Exposed() {
			continue
		}
		for _, d := range generate.Doors(r) {
			if d.Op == "" {
				continue
			}
			rel := "actions/" + d.Source
			if _, err := os.Stat(filepath.Join(appRoot, filepath.FromSlash(rel))); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			f, err := parse(appRoot, rel)
			if err != nil {
				return nil, err
			}
			out = append(out, checkDoor(f, module+"/gen/ops/"+r.Name, "ops_"+r.Name, d.Op)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// parsed is one hand file with what every check needs to name a line.
type parsed struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

func parse(appRoot, rel string) (*parsed, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(appRoot, filepath.FromSlash(rel)), nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	return &parsed{rel: rel, fset: fset, file: file}, nil
}

func (p *parsed) finding(at ast.Node, format string, args ...any) Finding {
	return Finding{File: p.rel, Line: p.fset.Position(at.Pos()).Line, Msg: fmt.Sprintf(format, args...)}
}

// localName is the name the file uses for the package at path: its
// alias, or the default name when it imports the path bare. "" when
// the file does not import it, so a check that needs the package can
// fail with its message rather than find nothing and stay silent.
func (p *parsed) localName(path, def string) string {
	for _, imp := range p.file.Imports {
		got, err := strconv.Unquote(imp.Path.Value)
		if err != nil || got != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return def
	}
	return ""
}

// checkOperation is the guard rule for one hand operation file.
func checkOperation(p *parsed) []Finding {
	var out []Finding
	opPkg := p.localName(opImportPath, "op")
	for _, decl := range p.file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, v := range vs.Values {
					if hasCall(v) {
						out = append(out, p.finding(vs, msgVar, vs.Names[0].Name))
						break
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil || !d.Name.IsExported() {
				continue
			}
			if d.Body == nil || len(d.Body.List) == 0 || !isGuard(d.Body.List[0], opPkg) {
				out = append(out, p.finding(d, msgGuard, d.Name.Name))
			}
		}
	}
	return out
}

// hasCall reports whether any call sits in e, a function literal's
// body included: `var x = func() T { ... }()` runs at init like any
// other call, and a literal that is never called is harmless, but
// telling the two apart is not worth the rule's cost in clarity.
func hasCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}

// isGuard reports whether s is exactly
//
//	if err := op.Check(a, b, c); err != nil { return ..., err }
//
// with any name for err: one ident on the left, the call on the right,
// the condition that ident against nil, and a body of one return whose
// last result is the ident. A guard that returns nil, or does more in
// its body, or has an else, is not the generated one and does not
// refuse what op.Check refuses. Nor may the name be a predeclared
// identifier: `if nil := op.Check(...); nil != nil` shadows nil, so
// the condition compares the error with itself and never refuses.
func isGuard(s ast.Stmt, opPkg string) bool {
	ifs, ok := s.(*ast.IfStmt)
	if !ok || ifs.Else != nil {
		return false
	}
	init, ok := ifs.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 1 || len(init.Rhs) != 1 {
		return false
	}
	errName, ok := init.Lhs[0].(*ast.Ident)
	if !ok || errName.Name == "_" || types.Universe.Lookup(errName.Name) != nil {
		return false
	}
	call, ok := init.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 3 || !isSelector(call.Fun, opPkg, "Check") {
		return false
	}
	cond, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ || !isIdent(cond.X, errName.Name) || !isIdent(cond.Y, "nil") {
		return false
	}
	if len(ifs.Body.List) != 1 {
		return false
	}
	ret, ok := ifs.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	return isIdent(ret.Results[len(ret.Results)-1], errName.Name)
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && pkg != "" && isIdent(sel.X, pkg) && sel.Sel.Name == name
}

// checkDoor is the door rule for one hand action overriding a
// generated door: Handle, or a same-file function Handle calls at its
// top level, must call opsPath's op as a top-level statement and use
// the error. opsDefault is the package name the generator gives the
// import, for when the file imports it bare.
func checkDoor(p *parsed, opsPath, opsDefault, op string) []Finding {
	reason, declared := pageOnlyReason(p)
	if declared {
		if reason == "" {
			return []Finding{p.finding(reasonNode(p), msgReason)}
		}
		return nil
	}
	pkg := p.localName(opsPath, opsDefault)
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range p.file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Body != nil {
			funcs[fd.Name.Name] = fd
		}
	}
	handle := funcs["Handle"]
	if handle == nil {
		// No Handle is a compile error in gen/; nothing to name here.
		return nil
	}
	c := doorCheck{p: p, pkg: pkg, op: op, funcs: funcs, seen: map[string]bool{}}
	c.walk(handle)
	switch {
	case c.used != nil:
		return nil
	case c.unused != nil:
		return []Finding{p.finding(c.unused, msgErrUnused, opsDefault, op)}
	case c.nested != nil:
		return []Finding{p.finding(c.nested, msgBranch, opsDefault, op)}
	}
	return []Finding{p.finding(handle, msgCall, opsDefault, op)}
}

// doorCheck collects where one door's operation is called: a
// top-level call whose error is used (which settles it), the first
// top-level call whose error is dropped, and the first call anywhere
// deeper. The last two only choose the message when there is no good
// call, so a door that calls the operation properly once and again
// inside a retry branch, as the money edit doors do, is clean.
type doorCheck struct {
	p                    *parsed
	pkg, op              string
	funcs                map[string]*ast.FuncDecl
	seen                 map[string]bool
	used, unused, nested ast.Node
}

func (c *doorCheck) walk(fd *ast.FuncDecl) {
	if c.seen[fd.Name.Name] {
		return
	}
	c.seen[fd.Name.Name] = true
	for _, s := range fd.Body.List {
		if c.topLevel(s) {
			continue
		}
		// A top-level call to a same-file function is Handle's own
		// body by another name: the generated money doors are
		// `Handle` calling `handle(ctx, w, r, 0)`, and a hand door
		// may write `err := save(...)` or `if err := save(...); ...`.
		if h := c.helper(s); h != nil {
			c.walk(h)
			continue
		}
		c.deeper(s)
	}
}

// helper is the same-file function s calls unconditionally, or nil: a
// bare call, a single-call assignment or var, or either as an if or
// switch's init clause, which runs on every request. The bodies of
// such an if or switch are still searched for a nested operation
// call, so a call inside them is named as a branch and not missed.
func (c *doorCheck) helper(s ast.Stmt) *ast.FuncDecl {
	switch s := s.(type) {
	case *ast.ExprStmt:
		return c.callee(s.X)
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			return c.callee(s.Rhs[0])
		}
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR && len(gd.Specs) == 1 {
			if vs := gd.Specs[0].(*ast.ValueSpec); len(vs.Values) == 1 {
				return c.callee(vs.Values[0])
			}
		}
	case *ast.IfStmt:
		if s.Init != nil {
			if h := c.helper(s.Init); h != nil {
				c.deeper(s.Body)
				if s.Else != nil {
					c.deeper(s.Else)
				}
				return h
			}
		}
	case *ast.SwitchStmt:
		if s.Init != nil {
			if h := c.helper(s.Init); h != nil {
				c.deeper(s.Body)
				return h
			}
		}
	}
	return nil
}

// callee is the same-file function e calls, or nil.
func (c *doorCheck) callee(e ast.Expr) *ast.FuncDecl {
	if call, ok := e.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok {
			return c.funcs[id.Name]
		}
	}
	return nil
}

// topLevel reports whether s is a top-level statement that calls the
// operation, recording how. An if or switch's init clause counts as
// top level: it runs on every request, as `if err := ops.Delete(...);
// err != nil` does.
func (c *doorCheck) topLevel(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 && c.isOpCall(s.Rhs[0]) {
			if last, ok := s.Lhs[len(s.Lhs)-1].(*ast.Ident); ok && last.Name != "_" {
				c.used = s
			} else if c.unused == nil {
				c.unused = s
			}
			return true
		}
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if len(vs.Values) == 1 && c.isOpCall(vs.Values[0]) {
					if vs.Names[len(vs.Names)-1].Name != "_" {
						c.used = s
					} else if c.unused == nil {
						c.unused = s
					}
					return true
				}
			}
		}
	case *ast.ExprStmt:
		if c.isOpCall(s.X) {
			if c.unused == nil {
				c.unused = s
			}
			return true
		}
	case *ast.IfStmt:
		if s.Init != nil && c.topLevel(s.Init) {
			c.deeper(s.Body)
			if s.Else != nil {
				c.deeper(s.Else)
			}
			return true
		}
	case *ast.SwitchStmt:
		if s.Init != nil && c.topLevel(s.Init) {
			c.deeper(s.Body)
			return true
		}
	}
	return false
}

// deeper records the first operation call anywhere inside n.
func (c *doorCheck) deeper(n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		if c.nested != nil {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok && c.isOpCall(call) {
			c.nested = call
			return false
		}
		return true
	})
}

func (c *doorCheck) isOpCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	return ok && isSelector(call.Fun, c.pkg, c.op)
}

// pageOnlyReason reads the directive from the comments above the
// package clause: declared says whether it is there at all, reason is
// what follows it. Both are read from the comment groups rather than
// file.Doc, because the directive may sit in the build-constraint
// group or on its own.
func pageOnlyReason(p *parsed) (reason string, declared bool) {
	for _, g := range p.file.Comments {
		if g.End() >= p.file.Package {
			break
		}
		for _, c := range g.List {
			if rest, ok := strings.CutPrefix(c.Text, pageOnly); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
				return strings.TrimSpace(rest), true
			}
		}
	}
	return "", false
}

// reasonNode is the directive's comment, for the finding's line.
func reasonNode(p *parsed) ast.Node {
	for _, g := range p.file.Comments {
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, pageOnly) {
				return c
			}
		}
	}
	return p.file
}
