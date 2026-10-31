package tmplc

import (
	"fmt"
	"go/types"
	"slices"
	"strings"
)

// reflectCall writes a call by reflection, of a FuncMap function or a
// method found by tmplrt.MethodOf, with args and then final, and is its
// result. whole is the tmplrt call given every operand at once, which
// converts them all after they are all evaluated; start is the one that
// begins a tmplrt.Args of n arguments, to which each is added, and
// converted, as it is evaluated. whole will do when no argument after
// the first is evaluated, as nothing can then run between a conversion
// and the evaluation text/template does after it.
func (emitter *writer) reflectCall(
	whole func(ops string) string,
	start func(count int) string,
	args []arg,
	final expr,
	at string,
) val {
	variable := emitter.temp("v")
	if !slices.ContainsFunc(
		args[min(1, len(args)):],
		func(argument arg) bool { return argument.c == nil },
	) {
		ops := emitter.inOrder(
			args,
			final,
			func(_ int, argument argVal, _ bool) string {
				return emitter.operand(argument)
			},
		)
		emitter.line("%s, err := %s",
			variable, whole(strings.Join(ops, ", ")))
		emitter.check(at, "err")
		return dynamic(variable)
	}
	count := len(args)
	if final != nil {
		count++
	}
	call := emitter.temp("c")
	emitter.line("%s, err := %s", call, start(count))
	emitter.check(at, "err")
	emitter.inOrder(args, final, func(_ int, argument argVal, _ bool) string {
		emitter.line(
			"if err := %s.Add(%s); err != nil {",
			call,
			emitter.operand(argument),
		)
		emitter.depth++
		emitter.line("%s", emitter.fail(at, "err"))
		emitter.depth--
		emitter.line("}")
		return ""
	})
	emitter.line("%s, err := %s.Call()", variable, call)
	emitter.check(at, "err")
	return dynamic(variable)
}

// methodCall is text/template's evalField for a name given arguments.
func (emitter *writer) methodCall(call methodCall) val {
	recv := emitter.eval(call.receiver)
	if recv.t == nil {
		return emitter.dynamicMethod(recv, call)
	}
	recv, loc := emitter.bindReceiver(recv)
	method, typed := emitter.typedMethod(recv, call)
	if !typed {
		if loc.code != "" {
			// MethodOf binds through the location as text/template does.
			return emitter.dynamicMethod(loc, call)
		}
		return emitter.dynamicMethod(recv, call)
	}
	return emitter.typedMethodCall(recv, loc, method, call)
}

// bindReceiver is recv as text/template binds a method to it, and the
// location it was read from, or a zero val. text/template binds the
// method to its receiver, through any pointer, before it evaluates the
// arguments, which may change what the receiver's location holds. A
// nil pointer it cannot go through stays its location, which it reads
// again at the call, so a pointer an argument stores there is the one
// called.
func (emitter *writer) bindReceiver(recv val) (bound, loc val) {
	_, isPtr := recv.t.Underlying().(*types.Pointer)
	if !isPtr || !recv.addr {
		return recv, val{}
	}
	loc = val{
		code: emitter.stable(recv.code),
		t:    recv.t,
		addr: true,
		path: recv.path,
	}
	return val{code: load(loc), t: recv.t, path: recv.path}, loc
}

// typedMethod is the method of recv that call calls, and whether typed
// code can call it.
func (emitter *writer) typedMethod(
	recv val,
	call methodCall,
) (*types.Signature, bool) {
	count := len(call.args)
	if call.final != nil {
		count++
	}
	sel, ok, miss := emitter.file.selectName(recv, call.name)
	typed := ok && sel.method != nil && arityOK(sel.method, count) &&
		goodResults(sel.method)
	if !typed && (ok || miss) {
		// A field given arguments, or a method given the wrong number,
		// fails.
		emitter.missed = append(emitter.missed, call.name)
	}
	if typed && !emitter.convertible(sel.method, call.args, call.final) {
		// A conversion needs a type the file cannot write.
		typed = false
	}
	return sel.method, typed
}

// typedMethodCall writes call of method on recv, bound from loc if
// loc is not a zero val.
func (emitter *writer) typedMethodCall(
	recv, loc val,
	method *types.Signature,
	call methodCall,
) val {
	recv.code = emitter.stable(recv.code)
	codes := emitter.inOrder(
		call.args,
		call.final,
		func(index int, argument argVal, later bool) string {
			parameter := paramType(method, index)
			src, _ := emitter.file.spell(parameter)
			return emitter.typedParam(argument, parameter, src, call.at, later)
		},
	)
	if loc.code != "" {
		emitter.line("if %s == nil {", recv.code)
		emitter.line("\t%s = %s", recv.code, load(loc))
		emitter.line("}")
	}
	return emitter.call(
		selector(recv)+"."+call.name+"("+strings.Join(codes, ", ")+")",
		method.Results(),
		call.name,
		call.at,
		recv.path.to(member(recv, call.name, method)),
	)
}

// convertible reports whether the file can write every conversion a call
// of sig with args and final needs: one that is not direct is to the
// parameter's type, which the file must be able to spell.
func (emitter *writer) convertible(
	sig *types.Signature,
	args []arg,
	final expr,
) bool {
	ok := true
	emitter.aside().
		inOrder(args, final, func(index int, argument argVal, _ bool) string {
			parameter := paramType(sig, index)
			if _, direct := emitter.direct(argument, parameter); !direct {
				if _, spelled := emitter.file.spell(parameter); !spelled {
					ok = false
				}
			}
			return ""
		})
	return ok
}

// dynamicMethod is a method call by reflection: on no value it is no
// value, or under missingkey=error an error, without evaluating any
// argument. text/template finds the method, through any pointer, before
// it evaluates the arguments, which may change the receiver's location.
func (emitter *writer) dynamicMethod(recv val, call methodCall) val {
	rt := emitter.file.rt
	receiver := emitter.stable(emitter.dyn(recv))
	variable := emitter.temp("v")
	emitter.line("var %s %s.Value", variable, emitter.file.reflect)
	emitter.line("if %s.IsValid() {", receiver)
	emitter.depth++
	method := emitter.temp("v")
	emitter.line(
		"%s, err := %s.MethodOf(%s, %q)",
		method,
		rt,
		receiver,
		call.name,
	)
	emitter.check(call.at, "err")
	got := emitter.reflectCall(
		func(ops string) string { return method + ".Call(" + ops + ")" },
		func(count int) string {
			return fmt.Sprintf("%s.Args(%d)", method, count)
		},
		call.args,
		call.final,
		call.at,
	)
	emitter.line("%s = %s", variable, got.code)
	emitter.depth--
	if emitter.missing == "MissingKeyError" {
		emitter.line("} else {")
		emitter.depth++
		emitter.line(
			"%s",
			emitter.fail(call.at, fmt.Sprintf("%s.NoEntry(%q)", rt, call.name)),
		)
		emitter.depth--
	}
	emitter.line("}")
	return dynamic(variable)
}
