package tmplc

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// writer writes one render function's statements. The function's
// parameters are b, the buffer; dot; fn, the bound FuncMap functions;
// and depth, how many {{template}} calls deep it runs. Every value is a
// local, and every error returns through tmplrt.ExecError with the
// template's name.
type writer struct {
	strings.Builder
	file    *file
	name    string           // the template, for errors
	bodies  map[string]*body // the templates it may call, by escaped name
	missing string           // the tmplrt MissingKey constant
	depth   int
	temps   int
	// rootRead and rootWritten are whether the code reads or sets root,
	// the Go variable for the template's own $.
	rootRead, rootWritten bool

	// dot is what . stands for where the writer is, and vars how each
	// template variable in scope is held, by Go variable. assigned are
	// the variables the template sets after declaring them, which are
	// held as reflect.Values.
	dot      val
	vars     map[string]val
	assigned map[string]bool
	// loops has, for each range the writer is in, innermost last,
	// whether it is a Go for loop, which {{break}} and {{continue}} end
	// with break and continue, rather than a function tmplrt.Range
	// calls, which they end by returning.
	loops []bool
	// run is the typed run the function is written for, which writes
	// the variants of the templates it calls; nil when the function
	// reads its data by reflection, and calls templates that do.
	run *typedRun
	// missed are the fields and methods typed code could not find.
	missed []string
	// assumed is what the code assumes of the types it reads, and seen
	// the paths of its facts.
	assumed
	seen map[string]bool
}

// implied reports whether what a fact would say of typ, reached by path,
// is said already: by the Go type of the FuncMap function it is the
// result of, which the bind asserted, or by the fact for the slice, array
// or map it is an element or key of, whose type spells it. Neither can
// say what a defined type is defined as.
func implied(typ types.Type, path *tpath) bool {
	if !predeclaredOnly(typ) {
		return false
	}
	if len(path.steps) == 0 {
		return path.fn != ""
	}
	last := path.steps[len(path.steps)-1]
	return last == "[]" || last == "[key]"
}

// predeclaredOnly reports whether typ is built only of predeclared types,
// so that its Go type says all of it.
func predeclaredOnly(typ types.Type) bool {
	switch resolved := types.Unalias(typ).(type) {
	case *types.Basic:
		return true
	case *types.Named:
		return resolved.Obj().Pkg() == nil
	case *types.Pointer:
		return predeclaredOnly(resolved.Elem())
	case *types.Slice:
		return predeclaredOnly(resolved.Elem())
	case *types.Array:
		return predeclaredOnly(resolved.Elem())
	case *types.Chan:
		return predeclaredOnly(resolved.Elem())
	case *types.Map:
		return predeclaredOnly(resolved.Key()) &&
			predeclaredOnly(resolved.Elem())
	case *types.Interface:
		return resolved.Empty()
	case *types.Struct:
		return resolved.NumFields() == 0
	case *types.Signature:
		tuples := []*types.Tuple{resolved.Params(), resolved.Results()}
		for _, tup := range tuples {
			for index := range tup.Len() {
				if !predeclaredOnly(tup.At(index).Type()) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// typed records what typed code assumes of value's type, which it came to
// by value's path, and is value; or, for a type describe cannot describe, value
// as
// a reflect.Value, as text/template holds it, so that no code assumes
// anything of the type.
func (emitter *writer) typed(value val) val {
	if value.t == nil || value.path == nil {
		return value
	}
	desc, ok := describe(value.t)
	if !ok {
		return dynamic(emitter.dyn(value))
	}
	if emitter.seen == nil {
		emitter.seen = map[string]bool{}
	}
	if key := value.path.key(); !emitter.seen[key] &&
		!implied(value.t, value.path) {
		emitter.seen[key] = true
		emitter.facts = append(
			emitter.facts,
			fact{path: value.path, desc: desc},
		)
		if field := value.path.fn; field != "" {
			if emitter.funcs == nil {
				emitter.funcs = map[string]string{}
			}
			called := emitter.file.fns[emitter.file.fields[field]]
			emitter.funcs[field] = called.expr
		}
	}
	return value
}

// read is the Go variable goName, read.
func (emitter *writer) read(goName string) string {
	if goName == "root" {
		emitter.rootRead = true
	}
	return goName
}

// written is the Go variables vars, set.
func (emitter *writer) written(vars []string) string {
	if slices.Contains(vars, "root") {
		emitter.rootWritten = true
	}
	return strings.Join(vars, ", ")
}

func (emitter *writer) line(format string, args ...any) {
	emitter.WriteString(strings.Repeat("\t", emitter.depth+1))
	fmt.Fprintf(emitter, format, args...)
	emitter.WriteByte('\n')
}

func (emitter *writer) temp(prefix string) string {
	emitter.temps++
	return prefix + strconv.Itoa(emitter.temps)
}

// fail is the statement that returns err from the template at node at.
func (emitter *writer) fail(at, err string) string {
	return fmt.Sprintf(
		"return %s.ExecError(%q, %q, %s)",
		emitter.file.rt,
		emitter.name,
		at,
		err,
	)
}

// check writes the return of err, an error variable, if it is not nil.
func (emitter *writer) check(at, err string) {
	emitter.line("if err != nil {")
	emitter.depth++
	emitter.line("%s", emitter.fail(at, err))
	emitter.depth--
	emitter.line("}")
}

func (emitter *writer) block(block block) {
	for _, statement := range block {
		emitter.stmt(statement)
	}
}

func (emitter *writer) nested(block block) {
	emitter.depth++
	emitter.block(block)
	emitter.depth--
}

func (emitter *writer) stmt(statement stmt) {
	switch concrete := statement.(type) {
	case writeText:
		emitter.line("b.WriteString(%s)", strconv.Quote(concrete.text))
	case writeValue:
		emitter.writeEscaped(concrete.value)
	case printValue:
		emitter.printValue(concrete)
	case declare:
		emitter.declare(concrete.vars, emitter.eval(concrete.value))
	case assign:
		value := emitter.dyn(emitter.eval(concrete.value))
		emitter.line("%s = %s", emitter.written(concrete.vars),
			repeat(value, len(concrete.vars)))
	case ifStmt:
		emitter.ifStmt(concrete)
	case withStmt:
		emitter.withStmt(concrete)
	case rangeStmt:
		emitter.rangeStmt(concrete)
	case breakStmt:
		emitter.breakStmt(concrete)
	case continueStmt:
		if emitter.loops[len(emitter.loops)-1] {
			emitter.line("continue")
		} else {
			emitter.line("return %s.ErrContinue", emitter.file.rt)
		}
	case templateCall:
		emitter.templateCall(concrete)
	default:
		panic(fmt.Sprintf("tmplc: no code for %T", concrete))
	}
}

func (emitter *writer) printValue(statement printValue) {
	value := emitter.dyn(emitter.eval(statement.value))
	emitter.line("if err := %s.WriteValue(b, %q, %s); err != nil {",
		emitter.file.rt, statement.at, value)
	emitter.depth++
	emitter.line("%s", emitter.fail(statement.at, "err"))
	emitter.depth--
	emitter.line("}")
}

func (emitter *writer) ifStmt(statement ifStmt) {
	emitter.line("if %s {", emitter.truth(emitter.eval(statement.cond)))
	emitter.nested(statement.then)
	if len(statement.els) > 0 {
		emitter.line("} else {")
		emitter.nested(statement.els)
	}
	emitter.line("}")
}

func (emitter *writer) withStmt(statement withStmt) {
	value := emitter.eval(statement.value)
	value.code = emitter.stable(value.code)
	emitter.line("if %s {", emitter.truth(value))
	emitter.depth++
	emitter.line("dot := %s", value.code)
	emitter.line("_ = dot")
	saved := emitter.dot
	emitter.dot = val{
		code: "dot", t: value.t, addr: value.addr, path: value.path,
	}
	emitter.block(statement.then)
	emitter.dot = saved
	emitter.depth--
	if len(statement.els) > 0 {
		emitter.line("} else {")
		emitter.nested(statement.els)
	}
	emitter.line("}")
}

// breakStmt leaves the loop: by goto to its label, by break from a Go
// loop, or else by returning tmplrt.ErrBreak.
func (emitter *writer) breakStmt(statement breakStmt) {
	switch {
	case statement.to != "":
		emitter.line("goto %s", statement.to)
	case emitter.loops[len(emitter.loops)-1]:
		emitter.line("break")
	default:
		emitter.line("return %s.ErrBreak", emitter.file.rt)
	}
}

// declare declares vars, each holding value: typed if value is, unless the
// template assigns the variable later.
func (emitter *writer) declare(vars []string, value val) {
	for _, name := range vars {
		if value.t == nil || emitter.assigned[name] {
			emitter.line("%s := %s", name, emitter.dyn(value))
			emitter.vars[name] = dynamic(name)
		} else {
			emitter.line("%s := %s", name, value.code)
			emitter.vars[name] = val{
				code: name, t: value.t, addr: value.addr, path: value.path,
			}
		}
		// A variable the template never reads is still declared.
		emitter.line("_ = %s", name)
	}
}

func (emitter *writer) templateCall(call templateCall) {
	rt := emitter.file.rt
	emitter.line("if depth == %s.MaxExecDepth {", rt)
	emitter.depth++
	emitter.line("%s", emitter.fail(call.at, rt+".DepthError()"))
	emitter.depth--
	emitter.line("}")
	callee := emitter.bodies[call.callee]
	fn, arg := callee.fn, emitter.file.reflect+".Value{}"
	if call.arg != nil {
		value := emitter.eval(call.arg)
		if emitter.run != nil && emitter.monomorphic(value) {
			// The callee, specialised for what it is given.
			fn, arg = emitter.run.variant(callee, value), value.code
			emitter.calls = append(emitter.calls, fn)
		} else {
			arg = emitter.dyn(value)
		}
	}
	emitter.line("if err := %s(b, %s, fn, depth+1); err != nil {", fn, arg)
	emitter.depth++
	emitter.line("return err")
	emitter.depth--
	emitter.line("}")
}

// monomorphic reports whether a template given value can be called in a
// variant specialised for value's type.
func (emitter *writer) monomorphic(value val) bool {
	if value.t == nil || isInterface(value.t) || isReflectValue(value.t) {
		return false
	}
	_, ok := emitter.file.spell(value.t)
	return ok
}

func repeat(text string, times int) string {
	vs := make([]string, times)
	for index := range vs {
		vs[index] = text
	}
	return strings.Join(vs, ", ")
}

// stable is name, or a variable holding it if evaluating name again would
// repeat work.
func (emitter *writer) stable(name string) string {
	if isIdent(name) {
		return name
	}
	held := emitter.temp("v")
	emitter.line("%s := %s", held, name)
	return held
}

func isIdent(text string) bool {
	for index, letter := range text {
		lower := 'a' <= letter && letter <= 'z'
		upper := 'A' <= letter && letter <= 'Z'
		digit := index > 0 && '0' <= letter && letter <= '9'
		if !(letter == '_' || lower || upper || digit) {
			return false
		}
	}
	return text != ""
}
