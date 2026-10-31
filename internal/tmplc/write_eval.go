package tmplc

import (
	"fmt"
	"go/types"
	"slices"
	"text/template/parse"
)

// evaluated is an expr already written, held in val: the value piped
// into a command, which is evaluated before anything of the command.
type evaluated struct{ v val }

func (evaluated) isExpr() {}

// staged is expression with its piped value, the previous command of the
// pipeline, evaluated first and held in a variable: text/template
// evaluates a pipeline's commands in order, so the previous one fails,
// or panics, before anything of the next runs, even an argument the
// next would not evaluate.
func (emitter *writer) staged(expression expr) expr {
	stage := func(final expr) expr {
		if final == nil {
			return nil
		}
		value := emitter.eval(final)
		if !isIdent(value.code) {
			held := emitter.temp("v")
			emitter.line("%s := %s", held, value.code)
			value.code = held
		}
		return evaluated{value}
	}
	switch concrete := expression.(type) {
	case methodCall:
		concrete.final = stage(concrete.final)
		return concrete
	case funcCall:
		concrete.final = stage(concrete.final)
		return concrete
	case builtinCall:
		concrete.final = stage(concrete.final)
		return concrete
	case andOr:
		concrete.final = stage(concrete.final)
		return concrete
	case escaperCall:
		concrete.final = stage(concrete.final)
		return concrete
	case failure:
		concrete.final = stage(concrete.final)
		return concrete
	}
	return expression
}

// eval writes the statements expression needs and returns its value.
func (emitter *writer) eval(expression expr) val {
	switch staged := emitter.staged(expression).(type) {
	case evaluated:
		return staged.v
	case dot:
		return emitter.dot
	case variable:
		emitter.read(staged.name)
		if value, ok := emitter.vars[staged.name]; ok {
			return value
		}
		return dynamic(staged.name)
	case constant:
		return emitter.constVal(staged.node)
	case commandValue:
		return emitter.unwrap(emitter.eval(staged.x))
	case fieldOf:
		value := emitter.eval(staged.receiver)
		for _, name := range staged.names {
			value = emitter.field(value, name, staged.at)
		}
		return value
	case methodCall:
		return emitter.methodCall(staged)
	case funcCall:
		return emitter.funcCall(staged)
	case builtinCall:
		return emitter.builtin(staged)
	case andOr:
		return emitter.andOr(staged)
	case escaperCall:
		return emitter.escaped(staged)
	case failure:
		return emitter.failure(staged)
	}
	panic(fmt.Sprintf("tmplc: no code for %T", expression))
}

// failure writes a command that fails as it is written.
func (emitter *writer) failure(command failure) val {
	// The value piped in was evaluated, by staged, for its errors;
	// the failing command does not use it.
	if final, ok := command.final.(evaluated); ok {
		emitter.line("_ = %s", final.v.code)
	}
	variable := emitter.temp("v")
	emitter.line("%s, err := %s.Fail(%q)",
		variable, emitter.file.rt, command.msg)
	emitter.check(command.at, "err")
	return dynamic(variable)
}

// field is .name on value: read directly when value's type has it, and looked
// up
// at run time when only the data can say what it is.
func (emitter *writer) field(value val, name, at string) val {
	rt := emitter.file.rt
	if value.t != nil {
		if _, isPtr := value.t.Underlying().(*types.Pointer); isPtr &&
			value.addr {
			// text/template goes through the pointer, not to its
			// variable's own methods; a pointer has none. It goes
			// through the pointer the location holds now, whatever a
			// later argument stores there.
			value = val{
				code: emitter.stable(load(value)),
				t:    value.t,
				path: value.path,
			}
		}
		sel, ok, miss := emitter.file.selectName(value, name)
		if ok {
			if typed, ok := emitter.typedField(value, sel, name, at); ok {
				return typed
			}
			miss = true
		}
		if miss {
			emitter.missed = append(emitter.missed, name)
		}
	}
	next := emitter.temp("v")
	if emitter.missing == "MissingKeyDefault" {
		emitter.line(
			"%s, err := %s.Field(%s, %q)",
			next,
			rt,
			emitter.dyn(value),
			name,
		)
	} else {
		emitter.line("%s, err := %s.FieldMissing(%s, %q, %s.%s)",
			next, rt, emitter.dyn(value), name, rt, emitter.missing)
	}
	emitter.check(at, "err")
	return dynamic(next)
}

// typedField reads what selectName found for .name on value: a method called
// with no arguments, or a field, through a pointer that is checked for
// nil first. ok is false for a method text/template cannot call that
// way.
func (emitter *writer) typedField(
	value val,
	sel selection,
	name, at string,
) (val, bool) {
	if sig := sel.method; sig != nil {
		if sig.Params().Len() != 0 || sig.Variadic() || !goodResults(sig) {
			return val{}, false
		}
		return emitter.call(
			selector(value)+"."+name+"()",
			sig.Results(),
			name,
			at,
			value.path.to(member(value, name, sig)),
		), true
	}
	path := value.path.to(member(value, name, nil))
	if sel.nilable {
		return emitter.nilableField(value, sel, name, at, path), true
	}
	if value.addr {
		return emitter.typed(val{
			code: "&" + selector(value) + "." + name,
			t:    sel.field,
			addr: true,
			path: path,
		}), true
	}
	return emitter.typed(
		val{code: selector(value) + "." + name, t: sel.field, path: path},
	), true
}

// nilableField reads the field name through value, a pointer, failing
// as text/template does when it is nil. The field is the one of what
// the pointer points to now.
func (emitter *writer) nilableField(
	value val,
	sel selection,
	name, at string,
	path *tpath,
) val {
	pointer := emitter.stable(value.code)
	emitter.line("if %s == nil {", pointer)
	emitter.depth++
	emitter.line(
		"_, err := %s.Fail(%q)",
		emitter.file.rt,
		fmt.Sprintf("nil pointer evaluating %s.%s", typeName(value.t), name),
	)
	emitter.line("%s", emitter.fail(at, "err"))
	emitter.depth--
	emitter.line("}")
	return emitter.typed(val{
		code: "&" + pointer + "." + name,
		t:    sel.field,
		addr: true,
		path: path,
	})
}

// call writes call, a direct call of a function or method called name
// with these results, and is its value: of the result's type, and not
// addressable, as reflect's Call gives it, which the code came to by
// path. A panic is the bind's to recover; text/template's safeCall makes
// the same error of it, in other words.
func (emitter *writer) call(
	call string,
	results *types.Tuple,
	name, at string,
	path *tpath,
) val {
	variable := emitter.temp("v")
	if results.Len() == 2 {
		emitter.line("%s, err := %s", variable, call)
		emitter.check(
			at,
			fmt.Sprintf("%s.CallError(%q, err)", emitter.file.rt, name),
		)
	} else {
		emitter.line("%s := %s", variable, call)
	}
	typ := results.At(0).Type()
	if isReflectValue(typ) {
		// text/template takes the result out of a reflect.Value.
		return dynamic(variable)
	}
	return emitter.typed(val{code: variable, t: typ, path: path})
}

// argVal is an evaluated argument: a constant as the template wrote it,
// which a call reads according to the parameter's type, or a value.
type argVal struct {
	c parse.Node
	v val
}

// inOrder evaluates args and then final as text/template's evalCall
// does: each argument to a value and then through conv, which writes its
// conversion to the parameter's type, before the next argument is
// evaluated. A conversion that fails then fails before a later argument
// runs, and one that reads the value reads it before a later argument
// can change it. later is whether an argument after this one is still to
// be evaluated: a constant is not, and final was, by staged, before the
// command began. conv returns the argument as the call is given it.
func (emitter *writer) inOrder(
	args []arg,
	final expr,
	conv func(index int, argument argVal, later bool) string,
) []string {
	codes := make([]string, 0, len(args)+1)
	for index, argument := range args {
		av := argVal{c: argument.c}
		if argument.c == nil {
			av.v = emitter.eval(argument.x)
		}
		later := slices.ContainsFunc(
			args[index+1:],
			func(argument arg) bool { return argument.c == nil },
		)
		codes = append(codes, conv(index, av, later))
	}
	if final != nil {
		codes = append(
			codes,
			conv(len(args), argVal{v: emitter.eval(final)}, false),
		)
	}
	return codes
}

// aside is a writer in emitter's state whose code is thrown away. A call's
// arguments must be converted as they are evaluated, so how the call is
// written is chosen before the first is; but what an argument's type is
// the writer learns only by writing the code that evaluates it, which it
// does aside first.
func (emitter *writer) aside() *writer {
	return &writer{
		file:     emitter.file,
		name:     emitter.name,
		bodies:   emitter.bodies,
		missing:  emitter.missing,
		depth:    emitter.depth,
		temps:    emitter.temps,
		dot:      emitter.dot,
		vars:     emitter.vars,
		assigned: emitter.assigned,
		loops:    emitter.loops,
		run:      emitter.run,
	}
}

// hold is value in a variable, when value's code computes something: a call's
// result, a comparison, a conversion. text/template evaluates each
// command, and each argument of a call, to a value before it goes on,
// so an argument after it that changes what the computation reads must
// not change its result. A variable, or a field read through one, is
// left as it is: text/template holds an addressable field where it is
// too, and reads it when it uses it.
func (emitter *writer) hold(value val) val {
	if pure.MatchString(value.code) {
		return value
	}
	held := emitter.temp("v")
	emitter.line("%s := %s", held, value.code)
	value.code = held
	return value
}
