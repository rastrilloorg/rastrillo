package tmplc

import (
	"fmt"
	"slices"
	"strings"
)

// funcCall calls a function of the set's FuncMap: directly, with its
// result typed, when the file can name its type; else by reflection.
func (emitter *writer) funcCall(invocation funcCall) val {
	fn := invocation.fn
	if !fn.direct {
		return emitter.reflectFuncCall(invocation)
	}
	if failed, ok := emitter.arityFailure(invocation); ok {
		return failed
	}
	codes := emitter.inOrder(
		invocation.args,
		invocation.final,
		func(index int, argument argVal, later bool) string {
			src := fn.params[min(index, len(fn.params)-1)]
			if fn.sig != nil {
				return emitter.typedParam(
					argument,
					paramType(fn.sig, index),
					src,
					invocation.at,
					later,
				)
			}
			return emitter.param(argument, src, invocation.at, later)
		},
	)
	call := fmt.Sprintf("fn.%s(%s)", fn.field, strings.Join(codes, ", "))
	if fn.sig != nil {
		return emitter.call(
			call,
			fn.sig.Results(),
			fn.name,
			invocation.at,
			&tpath{fn: fn.field},
		)
	}
	return emitter.heldResult(invocation, call)
}

// reflectFuncCall calls a FuncMap function by reflection.
func (emitter *writer) reflectFuncCall(invocation funcCall) val {
	rt, fn := emitter.file.rt, invocation.fn
	return emitter.reflectCall(
		func(ops string) string {
			if ops != "" {
				ops = ", " + ops
			}
			return fmt.Sprintf("%s.Call(%q, fn.%s%s)",
				rt, fn.name, fn.field, ops)
		},
		func(count int) string {
			return fmt.Sprintf("%s.CallArgs(%q, fn.%s, %d)",
				rt, fn.name, fn.field, count)
		},
		invocation.args,
		invocation.final,
		invocation.at,
	)
}

// arityFailure is the failure of a call given the wrong number of
// arguments, and true, or false for a call given the right number.
func (emitter *writer) arityFailure(invocation funcCall) (val, bool) {
	fn := invocation.fn
	count := len(invocation.args)
	if invocation.final != nil {
		count++
	}
	fixed := len(fn.params)
	var msg string
	switch {
	case fn.variadic && count < fixed-1:
		msg = fmt.Sprintf(
			"wrong number of args for %s: want at least %d got %d",
			fn.name, fixed-1, len(invocation.args))
	case !fn.variadic && count != fixed:
		msg = fmt.Sprintf("wrong number of args for %s: want %d got %d",
			fn.name, fixed, count)
	default:
		return val{}, false
	}
	return emitter.eval(failure{
		at:    invocation.at,
		msg:   msg,
		final: invocation.final,
	}), true
}

// heldResult writes call, of a function whose result's type cannot be
// resolved, so it is held as a reflect.Value, as text/template holds
// it, and a panic recovered here as text/template's safeCall does.
func (emitter *writer) heldResult(invocation funcCall, call string) val {
	rt, fn := emitter.file.rt, invocation.fn
	variable := emitter.temp("v")
	emitter.line(
		"%s, err := func() (v %s.Value, err error) {",
		variable,
		emitter.file.reflect,
	)
	emitter.depth++
	emitter.line("defer %s.Catch(&err)", rt)
	if fn.results == 2 {
		result := emitter.temp("r")
		emitter.line("%s, err := %s", result, call)
		emitter.line("return %s.Result(%s), err", rt, result)
	} else {
		emitter.line("return %s.Result(%s), nil", rt, call)
	}
	emitter.depth--
	emitter.line("}()")
	emitter.check(
		invocation.at,
		fmt.Sprintf("%s.CallError(%q, err)", rt, fn.name),
	)
	return dynamic(variable)
}

// andOr is and or or: each argument is evaluated only if the one before
// did not decide the result. The piped value comes last, and is the
// result if nothing before it decided.
func (emitter *writer) andOr(call andOr) val {
	rt := emitter.file.rt
	items := slices.Clone(call.args)
	if call.final != nil {
		items = append(items, arg{x: call.final})
	}
	variable := emitter.temp("v")
	emitter.line("%s := %s.ValueArg(%s)", variable, rt, emitter.value(items[0]))
	test := "%s.Truth(%s)"
	if call.or {
		test = "!%s.Truth(%s)"
	}
	for _, item := range items[1:] {
		emitter.line("if "+test+" {", rt, variable)
		emitter.depth++
		emitter.line("%s = %s.ValueArg(%s)", variable, rt, emitter.value(item))
	}
	for range items[1:] {
		emitter.depth--
		emitter.line("}")
	}
	return dynamic(variable)
}

// not is the negation of the Go condition cond.
func not(cond string) string {
	if pure.MatchString(cond) && cond[0] != '&' {
		return "!" + cond
	}
	return "!(" + cond + ")"
}
