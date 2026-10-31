package tmplc

import (
	"fmt"
	"go/types"
	"math"
	"reflect"
	"strconv"
	"strings"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// operand writes argument as a tmplrt.Operand, for a call whose parameter
// types are known only at run time.
func (emitter *writer) operand(argument argVal) string {
	rt := emitter.file.rt
	switch constant := argument.c.(type) {
	case nil:
		return fmt.Sprintf("%s.Val(%s)", rt, emitter.dyn(argument.v))
	case *parse.BoolNode:
		return fmt.Sprintf("%s.ConstBool(%t)", rt, constant.True)
	case *parse.StringNode:
		return fmt.Sprintf("%s.ConstString(%s, %s)", rt,
			strconv.Quote(constant.Text), strconv.Quote(constant.Quoted))
	case *parse.NumberNode:
		return fmt.Sprintf("%s.ConstNumber(%s)", rt, emitter.number(constant))
	case *parse.NilNode:
		return rt + ".ConstNil()"
	}
	panic(fmt.Sprintf("tmplc: no operand for %T", argument.c))
}

// number is node as a tmplrt.Number literal.
func (emitter *writer) number(node *parse.NumberNode) string {
	var fields []string
	if node.IsInt {
		fields = append(
			fields,
			"IsInt: true",
			"Int64: "+strconv.FormatInt(node.Int64, 10),
		)
	}
	if node.IsUint {
		fields = append(
			fields,
			"IsUint: true",
			"Uint64: "+strconv.FormatUint(node.Uint64, 10),
		)
	}
	if node.IsFloat {
		fields = append(
			fields,
			"IsFloat: true",
			"Float64: "+emitter.float(node.Float64),
		)
	}
	if node.IsComplex {
		fields = append(
			fields,
			"IsComplex: true",
			"Complex128: "+emitter.complex(node.Complex128),
		)
	}
	fields = append(fields, "Text: "+strconv.Quote(node.Text))
	return fmt.Sprintf(
		"%s.Number{%s}",
		emitter.file.rt,
		strings.Join(fields, ", "),
	)
}

// float is number as a Go expression of type float64.
func (emitter *writer) float(number float64) string {
	if number == 0 && math.Signbit(number) {
		return emitter.file.rt + ".NegativeZero"
	}
	return "float64(" + strconv.FormatFloat(number, 'g', -1, 64) + ")"
}

func (emitter *writer) complex(number complex128) string {
	return fmt.Sprintf(
		"complex(%s, %s)",
		emitter.float(real(number)),
		emitter.float(imag(number)),
	)
}

// ideal is the value text/template gives the constant node where any type
// will do, as a Go expression of type typ, or ok false if the constant
// is an error there. typ is nil for no value.
func (emitter *writer) ideal(
	node parse.Node,
) (lit string, typ types.Type, ok bool, msg string) {
	switch concrete := node.(type) {
	case *parse.BoolNode:
		lit = strconv.FormatBool(concrete.True)
		return lit, types.Typ[types.Bool], true, ""
	case *parse.StringNode:
		lit = strconv.Quote(concrete.Text)
		return lit, types.Typ[types.String], true, ""
	case *parse.NumberNode:
		value, err := tmplrt.Constant(tmplrt.Number{
			IsInt:      concrete.IsInt,
			IsUint:     concrete.IsUint,
			IsFloat:    concrete.IsFloat,
			IsComplex:  concrete.IsComplex,
			Int64:      concrete.Int64,
			Uint64:     concrete.Uint64,
			Float64:    concrete.Float64,
			Complex128: concrete.Complex128,
			Text:       concrete.Text,
		})
		switch {
		case err != nil:
			return "", nil, false, err.Error()
		case !value.IsValid():
			return "", nil, true, ""
		}
		switch value.Kind() {
		case reflect.Int:
			lit = strconv.FormatInt(value.Int(), 10)
			return lit, types.Typ[types.Int], true, ""
		case reflect.Float64:
			lit = emitter.float(value.Float())
			return lit, types.Typ[types.Float64], true, ""
		case reflect.Complex128:
			lit = emitter.complex(value.Complex())
			return lit, types.Typ[types.Complex128], true, ""
		}
	}
	panic(fmt.Sprintf("tmplc: no constant for %s", node))
}

// constVal is node as a value of its own, as a command or an argument
// whose parameter takes any value: of the type text/template gives it.
func (emitter *writer) constVal(node parse.Node) val {
	if _, ok := node.(*parse.NilNode); ok {
		return dynamic(emitter.file.reflect + ".Value{}")
	}
	lit, typ, ok, msg := emitter.ideal(node)
	switch {
	case !ok:
		return emitter.eval(failure{at: node.String(), msg: msg})
	case typ == nil:
		return dynamic(emitter.file.reflect + ".Value{}")
	case !emitter.file.typed:
		return dynamic(emitter.file.reflect + ".ValueOf(" + lit + ")")
	}
	return val{code: lit, t: typ}
}

// value is argument as an argument of type reflect.Value.
func (emitter *writer) value(argument arg) string {
	if argument.c != nil {
		return emitter.dyn(emitter.constVal(argument.c))
	}
	return emitter.dyn(emitter.eval(argument.x))
}

// anyArg is argument as an argument of type any.
func (emitter *writer) anyArg(argument argVal) string {
	if argument.c != nil {
		if _, ok := argument.c.(*parse.NilNode); ok {
			return "nil"
		}
		return emitter.anyOf(emitter.constVal(argument.c))
	}
	return emitter.anyOf(argument.v)
}

// direct is argument as an argument of the parameter type typ, if Go
// converts it as text/template does: a value assignable to typ, an
// addressable value whose pointer typ is, or a constant of typ's kind
// that typ can hold.
func (emitter *writer) direct(argument argVal, typ types.Type) (string, bool) {
	if argument.c != nil {
		return emitter.directConstant(argument.c, typ)
	}
	value := argument.v
	switch {
	case value.t == nil:
		if isEmptyInterface(typ) {
			return emitter.file.rt + ".Arg(" + value.code + ")", true
		}
	case types.AssignableTo(value.t, typ):
		return load(value), true
	case value.addr:
		if ptr, ok := typ.(*types.Pointer); ok &&
			types.Identical(ptr.Elem(), value.t) {
			return value.code, true
		}
	}
	return "", false
}

// directConstant is the constant node as an argument of the parameter
// type typ, if it is of typ's kind and typ can hold it.
func (emitter *writer) directConstant(
	node parse.Node,
	typ types.Type,
) (string, bool) {
	is := func(kind types.BasicKind) bool {
		return types.Identical(typ, types.Typ[kind])
	}
	basic, _ := typ.Underlying().(*types.Basic)
	info := types.BasicInfo(0)
	if basic != nil {
		info = basic.Info()
	}
	named := func(lit string) (string, bool) {
		if src, ok := emitter.file.spell(typ); ok {
			return src + "(" + lit + ")", true
		}
		return "", false
	}
	switch constant := node.(type) {
	case *parse.StringNode:
		switch {
		case is(types.String):
			return strconv.Quote(constant.Text), true
		case info&types.IsString != 0:
			return named(strconv.Quote(constant.Text))
		}
	case *parse.BoolNode:
		switch {
		case is(types.Bool):
			return strconv.FormatBool(constant.True), true
		case info&types.IsBoolean != 0:
			return named(strconv.FormatBool(constant.True))
		}
	case *parse.NumberNode:
		return emitter.directNumber(constant, is)
	}
	return "", false
}

// directNumber is the number constant as an argument of the type is
// says yes to, of int, int64 or float64, if that type can hold it.
func (emitter *writer) directNumber(
	constant *parse.NumberNode,
	is func(kind types.BasicKind) bool,
) (string, bool) {
	switch {
	case is(types.Int) && constant.IsInt &&
		constant.Int64 >= math.MinInt32 && constant.Int64 <= math.MaxInt32:
		return strconv.FormatInt(constant.Int64, 10), true
	case is(types.Int64) && constant.IsInt:
		return "int64(" + strconv.FormatInt(constant.Int64, 10) + ")", true
	case is(types.Float64) && constant.IsFloat:
		return emitter.float(constant.Float64), true
	}
	return "", false
}

// typedParam is argument as an argument of the Go type target, which the file
// writes as src when argument needs converting at run time, as text/template
// converts an argument to a parameter of that type. later is whether an
// argument after argument is still to be evaluated.
func (emitter *writer) typedParam(
	argument argVal,
	target types.Type,
	src, at string,
	later bool,
) string {
	if code, ok := emitter.direct(argument, target); ok {
		return code
	}
	return emitter.param(argument, src, at, later)
}

// param is argument as an argument of the Go type typ, converted at run time
// as text/template converts an argument to a parameter of that type.
// The conversion is written where argument is evaluated, and fails there; when
// an argument after argument is still to be evaluated, the value it converts is
// read only at the call, as text/template reads it.
func (emitter *writer) param(
	argument argVal,
	typ, at string,
	later bool,
) string {
	switch {
	case typ == "any" || typ == "interface{}":
		return emitter.anyArg(argument)
	case typ == "string":
		if text, ok := argument.c.(*parse.StringNode); ok {
			return strconv.Quote(text.Text)
		}
	case typ == "bool":
		if constant, ok := argument.c.(*parse.BoolNode); ok {
			return strconv.FormatBool(constant.True)
		}
	}
	variable := emitter.temp("a")
	if argument.c == nil && later {
		emitter.line(
			"%s, err := %s.Convert[%s](%s)",
			variable,
			emitter.file.rt,
			typ,
			emitter.operand(argument),
		)
		emitter.check(at, "err")
		return fmt.Sprintf("%s.As[%s](%s)", emitter.file.rt, typ, variable)
	}
	emitter.line(
		"%s, err := %s.Param[%s](%s)",
		variable,
		emitter.file.rt,
		typ,
		emitter.operand(argument),
	)
	emitter.check(at, "err")
	return variable
}
