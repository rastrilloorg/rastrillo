package tmplc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/types"
	"slices"
)

// An entry's typed variants. For each of a few of the owner package's
// types that the entry reads like (its candidates), the file has the
// entry written for a dot of that type, and of a pointer to it, with
// each template it calls written for the static type of what it is
// given. The bind runs a variant when the data has its type, and the
// code that reads everything by reflection when it has any other.

// maxCandidates is how many of the owner package's types an entry gets
// typed variants for, each as a value and as a pointer. Every variant
// costs generated code, and an app's handlers pass one type to a page.
const maxCandidates = 4

// candidates are the types an entry's code is worth specialising for:
// the named types the owner package declares that have each field or
// method the entry, and the templates it calls with its dot, read on
// that dot. The writer then decides, variant by variant, whether one
// covers every path exactly.
func (generated *file) candidates(
	entry *body,
	bodies map[string]*body,
) []types.Type {
	if generated.in.Types == nil {
		return nil
	}
	names := dotNames(entry, bodies)
	if len(names) == 0 {
		// Nothing to read on dot directly; nothing to gain.
		return nil
	}
	if generated.ownTypes == nil {
		generated.ownTypes = []types.Type{}
		scope := generated.in.Types.Scope()
		for _, name := range scope.Names() { // sorted
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			switch tn.Type().Underlying().(type) {
			case *types.Interface, *types.Pointer:
				continue
			}
			if _, ok := generated.spell(tn.Type()); ok {
				generated.ownTypes = append(generated.ownTypes, tn.Type())
			}
		}
	}
	var out []types.Type
	for _, typ := range generated.ownTypes {
		if !slices.ContainsFunc(names, func(name string) bool {
			obj, index, _ := types.LookupFieldOrMethod(
				typ,
				true,
				generated.in.Types,
				name,
			)
			return obj == nil && index == nil
		}) {
			out = append(out, typ)
		}
	}
	return out
}

// dotNames are the names entry reads on its own dot, or on $ while that
// is its dot, and so do the templates it calls with that dot.
func dotNames(entry *body, bodies map[string]*body) []string {
	walk := dotWalk{
		names:  map[string]bool{},
		seen:   map[*body]bool{entry: true},
		bodies: bodies,
	}
	walk.block(entry.code, true)
	out := make([]string, 0, len(walk.names))
	for name := range walk.names {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// dotWalk is dotNames's walk of the code: the names it has found, the
// bodies it has entered, and every body by name. Each step's own is
// whether dot is still the entry's dot.
type dotWalk struct {
	names  map[string]bool
	seen   map[*body]bool
	bodies map[string]*body
}

// isDot reports whether expression is the entry's dot: dot while own,
// or $.
func isDot(expression expr, own bool) bool {
	if command, ok := expression.(commandValue); ok {
		expression = command.x
	}
	switch concrete := expression.(type) {
	case dot:
		return own
	case variable:
		return concrete.name == "root"
	}
	return false
}

func (walk dotWalk) args(arguments []arg, final expr, own bool) {
	for _, argument := range arguments {
		if argument.x != nil {
			walk.expr(argument.x, own)
		}
	}
	if final != nil {
		walk.expr(final, own)
	}
}

func (walk dotWalk) expr(expression expr, own bool) {
	switch expression := expression.(type) {
	case commandValue:
		walk.expr(expression.x, own)
	case fieldOf:
		if isDot(expression.receiver, own) {
			walk.names[expression.names[0]] = true
		}
		walk.expr(expression.receiver, own)
	case methodCall:
		if isDot(expression.receiver, own) {
			walk.names[expression.name] = true
		}
		walk.expr(expression.receiver, own)
		walk.args(expression.args, expression.final, own)
	case funcCall:
		walk.args(expression.args, expression.final, own)
	case builtinCall:
		walk.args(expression.args, expression.final, own)
	case andOr:
		walk.args(expression.args, expression.final, own)
	case escaperCall:
		walk.args(expression.args, expression.final, own)
	case failure:
		walk.args(nil, expression.final, own)
	}
}

func (walk dotWalk) block(statements block, own bool) {
	for _, statement := range statements {
		switch concrete := statement.(type) {
		case writeValue:
			walk.expr(concrete.value, own)
		case printValue:
			walk.expr(concrete.value, own)
		case declare:
			walk.expr(concrete.value, own)
		case assign:
			walk.expr(concrete.value, own)
		case ifStmt:
			walk.expr(concrete.cond, own)
			walk.block(concrete.then, own)
			walk.block(concrete.els, own)
		case withStmt:
			walk.expr(concrete.value, own)
			walk.block(concrete.then, false)
			walk.block(concrete.els, own)
		case rangeStmt:
			walk.expr(concrete.over, own)
			walk.block(concrete.body, false)
			walk.block(concrete.els, own)
		case templateCall:
			walk.templateCall(concrete, own)
		}
	}
}

// templateCall walks the callee of a {{template}} given the entry's
// dot, once.
func (walk dotWalk) templateCall(call templateCall, own bool) {
	if call.arg == nil {
		return
	}
	walk.expr(call.arg, own)
	callee := walk.bodies[call.callee]
	if isDot(call.arg, own) && callee != nil && !walk.seen[callee] {
		walk.seen[callee] = true
		walk.block(callee.code, true)
	}
}

// maxVariants bounds the typed functions one entry's variant may need:
// a template that calls itself with a value of a new type each time,
// through a generic type, would otherwise need them without end.
const maxVariants = 64

// typedRun writes one typed variant of an entry, and each variant of a
// template the entry calls that it needs: the callee specialised for
// the static type of the value it is given. The variant is kept only if
// every one of them reads every field and method directly.
type typedRun struct {
	file    *file
	lowerer *lowerer
	queue   []variant
	written map[string]emitted
	failed  bool
}

// variant is one template, to be written for a dot of one type.
type variant struct {
	name string // the Go function
	b    *body
	dot  val
}

// typedEntry writes entry's typed variants: for each candidate type,
// one for the type and one for a pointer to it, of which the writer
// keeps those that read their dot directly throughout. The bind chooses
// between them by the data's dynamic type.
func (generated *file) typedEntry(
	entry string,
	lowerer *lowerer,
) ([]typedCase, error) {
	if !generated.typed {
		return nil, nil
	}
	body := lowerer.bodies[entry]
	var cases []typedCase
	kept := 0
	for _, typ := range generated.candidates(body, lowerer.bodies) {
		if kept == maxCandidates {
			break
		}
		found := false
		for _, typ := range []types.Type{typ, types.NewPointer(typ)} {
			spelled, ok := generated.spell(typ)
			if !ok {
				continue
			}
			run := &typedRun{
				file:    generated,
				lowerer: lowerer,
				written: map[string]emitted{},
			}
			name := run.variant(body, val{code: "dot", t: typ})
			if !run.finish() {
				continue
			}
			for fn, function := range run.written {
				if err := generated.put(fn, function); err != nil {
					return nil, err
				}
			}
			cases = append(cases, typedCase{typ: spelled, fn: name})
			found = true
		}
		if found {
			kept++
		}
	}
	return cases, nil
}

// variant is the function for the template lowered as body, specialised
// for dot, which the run writes if no run has.
func (run *typedRun) variant(body *body, dot val) string {
	spelled, _ := run.file.spell(dot.t)
	key := spelled
	if dot.addr {
		key = "&" + key
	}
	sum := sha256.Sum256([]byte(key))
	fn := body.fn + "_" + hex.EncodeToString(sum[:])[:8]
	dot = val{code: "dot", t: dot.t, addr: dot.addr}
	_, done := run.file.bodies[fn]
	_, queued := run.written[fn]
	switch {
	case run.file.failed[fn]:
		run.failed = true
	case !done && !queued:
		run.written[fn] = emitted{}
		run.queue = append(run.queue, variant{name: fn, b: body, dot: dot})
		if len(run.written) > maxVariants {
			run.failed = true
		}
	}
	return fn
}

// finish writes the variants the run has queued, and reports whether all
// of them read every field and method directly.
func (run *typedRun) finish() bool {
	for len(run.queue) > 0 && !run.failed {
		variant := run.queue[0]
		run.queue = run.queue[1:]
		spelled, _ := run.file.spell(variant.dot.t)
		param, doc := spelled, "for a dot of type "+spelled+"."
		if variant.dot.addr {
			param = "*" + spelled
			doc = "for an addressable dot of type " + spelled + "."
		}
		out := run.file.render(
			variant.b.name,
			variant.b,
			run.lowerer,
			variant.dot,
			param,
			run,
		)
		if len(out.missed) > 0 {
			run.file.failed[variant.name] = true
			run.failed = true
			break
		}
		out.dot = spelled
		run.written[variant.name] = emitted{
			doc: fmt.Sprintf(
				"renders %q %s",
				baseName(variant.b.name),
				doc,
			),
			code:    out.code,
			assumed: out.assumed,
		}
	}
	return !run.failed
}
