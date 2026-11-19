package tmplc

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"
)

// builtin calls one of text/template's builtins: directly where the
// arguments' types decide the result, as for comparing two strings, and
// through tmplrt's port otherwise. Its result is held at once, as
// text/template has it before the next argument of whatever it is an
// argument of.
func (emitter *writer) builtin(call builtinCall) val {
	return emitter.hold(emitter.builtinValue(call))
}

func (emitter *writer) builtinValue(builtin builtinCall) val {
	rt := emitter.file.rt
	str := types.Typ[types.String]
	vals, format := emitter.builtinArgs(builtin)
	call := builtinWriter{emitter: emitter, builtin: builtin, vals: vals}
	switch builtin.name {
	case "not":
		return call.not()
	case "eq", "ne", "lt", "le", "gt", "ge":
		return call.compared()
	case "len":
		return call.lengthOf()
	case "index":
		return call.result(
			fmt.Sprintf("%s.Index(%s)", rt, call.values(vals)), nil)
	case "slice":
		return call.result(
			fmt.Sprintf("%s.Slice(%s)", rt, call.values(vals)), nil)
	case "call":
		return call.called()
	case "print", "println":
		fn := map[string]string{
			"print": "Print", "println": "Println",
		}[builtin.name]
		return val{
			code: fmt.Sprintf("%s.%s(%s)", rt, fn, call.anys(vals)),
			t:    str,
		}
	case "printf":
		rest := ""
		if len(vals) > 1 {
			rest = ", " + call.anys(vals[1:])
		}
		return val{
			code: fmt.Sprintf("%s.Printf(%s%s)", rt, format, rest),
			t:    str,
		}
	case "html", "js", "urlquery":
		return call.escaped()
	}
	panic("tmplc: no code for the builtin " + builtin.name)
}

// builtinArgs evaluates a builtin's arguments. Every parameter of a
// builtin takes any value, so only a constant converts, and may fail:
// as a number too large for an int. printf's format is a string, and
// converts as a FuncMap function's would; format is its code.
func (emitter *writer) builtinArgs(
	builtin builtinCall,
) (vals []val, format string) {
	emitter.inOrder(
		builtin.args,
		builtin.final,
		func(index int, argument argVal, later bool) string {
			value := argument.v
			switch {
			case builtin.name == "printf" && index == 0:
				format = emitter.typedParam(
					argument,
					types.Typ[types.String],
					"string",
					builtin.at,
					later,
				)
			case argument.c != nil:
				value = emitter.constVal(argument.c)
			}
			vals = append(vals, value)
			return ""
		},
	)
	return vals, format
}

// builtinWriter writes a builtin's call once its arguments, vals, are
// evaluated. Without typed values, a builtin's result is a
// reflect.Value too.
type builtinWriter struct {
	emitter *writer
	builtin builtinCall
	vals    []val
}

// values is vals as reflect.Values.
func (call builtinWriter) values(vals []val) string {
	codes := make([]string, len(vals))
	for index, value := range vals {
		codes[index] = call.emitter.dyn(value)
	}
	return strings.Join(codes, ", ")
}

// anys is vals as interface values.
func (call builtinWriter) anys(vals []val) string {
	codes := make([]string, len(vals))
	for index, value := range vals {
		codes[index] = call.emitter.anyOf(value)
	}
	return strings.Join(codes, ", ")
}

// result writes code, a call that returns a value of type typ, nil for
// a reflect.Value, and an error.
func (call builtinWriter) result(code string, typ types.Type) val {
	emitter := call.emitter
	variable := emitter.temp("v")
	emitter.line("%s, err := %s", variable, code)
	emitter.check(
		call.builtin.at,
		fmt.Sprintf("%s.CallError(%q, err)",
			emitter.file.rt, call.builtin.name),
	)
	if typ != nil && !emitter.file.typed {
		return dynamic(emitter.file.reflect + ".ValueOf(" + variable + ")")
	}
	return val{code: variable, t: typ}
}

func (call builtinWriter) not() val {
	emitter, first := call.emitter, call.vals[0]
	rt, boolean := emitter.file.rt, types.Typ[types.Bool]
	if !emitter.file.typed {
		return dynamic(fmt.Sprintf("%s.ValueOf(%s.Not(%s))",
			emitter.file.reflect, rt, emitter.dyn(first)))
	}
	if first.t != nil {
		return val{code: not(emitter.truth(first)), t: boolean}
	}
	return val{code: fmt.Sprintf("%s.Not(%s)", rt, first.code), t: boolean}
}

func (call builtinWriter) compared() val {
	name, boolean := call.builtin.name, types.Typ[types.Bool]
	if cmp, ok := compare(name, call.vals); ok && call.emitter.file.typed {
		return val{code: cmp, t: boolean}
	}
	fn := strings.ToUpper(name[:1]) + name[1:]
	return call.result(fmt.Sprintf("%s.%s(%s)",
		call.emitter.file.rt, fn, call.values(call.vals)), boolean)
}

func (call builtinWriter) lengthOf() val {
	integer := types.Typ[types.Int]
	if length, ok := length(call.vals[0]); ok && call.emitter.file.typed {
		return val{code: length, t: integer}
	}
	return call.result(fmt.Sprintf("%s.Len(%s)",
		call.emitter.file.rt, call.values(call.vals)), integer)
}

func (call builtinWriter) called() val {
	emitter, vals := call.emitter, call.vals
	callee := strconv.Quote(call.builtin.callee)
	fnv := emitter.stable(emitter.dyn(vals[0]))
	if call.builtin.callee == "" {
		callee = fnv + ".String()"
	}
	rest := ""
	if len(vals) > 1 {
		rest = ", " + call.values(vals[1:])
	}
	return call.result(fmt.Sprintf("%s.CallBuiltin(%s, %s%s)",
		emitter.file.rt, callee, fnv, rest), nil)
}

func (call builtinWriter) escaped() val {
	fn := map[string]string{
		"html":     "HTMLEscaperBuiltin",
		"js":       "JSEscaper",
		"urlquery": "URLQueryEscaper",
	}[call.builtin.name]
	return val{
		code: fmt.Sprintf("%s.%s(%s)",
			call.emitter.file.rt, fn, call.anys(call.vals)),
		t: types.Typ[types.String],
	}
}

// compare is eq or ne written as a Go comparison, when every argument
// has a static type of the same basic kind, which is when text/template
// compares the underlying values with Go's ==.
func compare(name string, vals []val) (string, bool) {
	if name != "eq" && name != "ne" || len(vals) < 2 {
		return "", false
	}
	kind := compareClass(vals[0])
	if kind == "" {
		return "", false
	}
	for _, value := range vals[1:] {
		if compareClass(value) != kind {
			return "", false
		}
	}
	conv := func(value val) string { return kind + "(" + load(value) + ")" }
	if name == "ne" {
		return conv(vals[0]) + " != " + conv(vals[1]), true
	}
	parts := make([]string, len(vals)-1)
	for index, value := range vals[1:] {
		parts[index] = conv(vals[0]) + " == " + conv(value)
	}
	if len(parts) == 1 {
		return parts[0], true
	}
	return "(" + strings.Join(parts, " || ") + ")", true
}

// compareClass is the Go type compare converts value to, or "" for a
// value whose static type is not of a typed basic kind.
func compareClass(value val) string {
	if value.t == nil {
		return ""
	}
	basic, ok := value.t.Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsUntyped != 0 {
		return ""
	}
	switch info := basic.Info(); {
	case info&types.IsBoolean != 0:
		return "bool"
	case info&types.IsString != 0:
		return "string"
	case info&types.IsUnsigned != 0:
		return "uint64"
	case info&types.IsInteger != 0:
		return "int64"
	case info&types.IsFloat != 0:
		return "float64"
	case info&types.IsComplex != 0:
		return "complex128"
	}
	return ""
}

// length is len of value written as Go's len, for a type whose length Go
// and text/template agree on.
func length(value val) (string, bool) {
	if value.t == nil {
		return "", false
	}
	switch underlying := value.t.Underlying().(type) {
	case *types.Basic:
		info := underlying.Info()
		if info&types.IsString == 0 || info&types.IsUntyped != 0 {
			return "", false
		}
	case *types.Slice, *types.Map, *types.Array, *types.Chan:
	default:
		return "", false
	}
	return "len(" + load(value) + ")", true
}
