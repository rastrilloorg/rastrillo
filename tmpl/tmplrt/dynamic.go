// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Ported from text/template/exec.go and funcs.go in Go 1.26.10 for
// Rastrillo's compiled templates; see LICENSE-go. Compiled code holds
// each value as text/template does, as a reflect.Value: an any would
// lose whether the value is addressable (and so has its pointer
// methods), and a nil interface value would become no value at all,
// which text/template treats differently. The checks, their order and
// the errors are text/template's.
//
// What compiled code knows when it is generated, text/template works out
// as it walks: which node comes next, how many arguments a builtin
// takes, which variable a name refers to. What it cannot know, because
// it depends on the data, is here: what a field name finds, how an
// argument converts to a parameter type known only at run time, and
// what a range ranges over.

//rastrillo:vendored go1.26.10 src/text/template/exec.go and funcs.go, adapted

package tmplrt

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strings"
	"text/template"
)

// errorType is declared in escape_content.go.
var reflectValueType = reflect.TypeFor[reflect.Value]()

// Data is the dot text/template starts from for the data passed to
// Execute: a reflect.Value is used as it is, so an addressable one keeps
// its pointer methods.
func Data(data any) reflect.Value {
	if v, ok := data.(reflect.Value); ok {
		return v
	}
	return reflect.ValueOf(data)
}

// Unwrap is what text/template's evalPipeline does to the value of each
// command: a value held in an empty interface is taken out of it, so
// that a nil one becomes no value. Ranging over no value runs {{else}};
// ranging over a nil interface would fail. An interface with methods,
// such as error, keeps its interface type.
func Unwrap(v reflect.Value) reflect.Value {
	// If the object has type interface{}, dig down one level to the thing inside.
	if v.Kind() == reflect.Interface && v.Type().NumMethod() == 0 {
		return v.Elem()
	}
	return v
}

// Arg is v as a function called from the template receives it as an
// any: nothing is nil. A value reflect will not hand out, read through
// an unexported field, panics, as the call would in text/template,
// whose recovery the compiled code repeats.
func Arg(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	return v.Interface()
}

// Truth is whether {{if}} takes its first branch for v: false for no
// value, nil, zero numbers, false, and empty strings, slices, arrays
// and maps; true for everything else, structs included. Every kind has a
// truth value, so text/template's "if/with can't use" cannot happen.
func Truth(v reflect.Value) bool {
	truth, _ := isTrue(indirectInterface(v))
	return truth
}

// isTrue reports whether the value is 'true', in the sense of not the zero of its type,
// and whether the value has a meaningful truth value. This is the definition of
// truth used by if and other such actions.
func isTrue(val reflect.Value) (truth, ok bool) {
	if !val.IsValid() {
		// Something like var x interface{}, never set. It's a form of nil.
		return false, true
	}
	switch val.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		truth = val.Len() > 0
	case reflect.Bool:
		truth = val.Bool()
	case reflect.Complex64, reflect.Complex128:
		truth = val.Complex() != 0
	case reflect.Chan, reflect.Func, reflect.Pointer, reflect.UnsafePointer, reflect.Interface:
		truth = !val.IsNil()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		truth = val.Int() != 0
	case reflect.Float32, reflect.Float64:
		truth = val.Float() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		truth = val.Uint() != 0
	case reflect.Struct:
		truth = true // Struct values are always true.
	default:
		return
	}
	return truth, true
}

// MissingKey is a set's missingkey option, which decides what a field
// name that is not a key of the map it reads gives.
type MissingKey uint8

const (
	// MissingKeyDefault, for "default" and "invalid", gives no value.
	MissingKeyDefault MissingKey = iota
	// MissingKeyZero gives the zero value of the map's element type.
	MissingKeyZero
	// MissingKeyError fails, and so does any name on no value.
	MissingKeyError
)

var mapStringAnyType = reflect.TypeFor[map[string]any]()

// Field is .fieldName on receiver: a method called with no arguments, a
// field of a struct, or the value under the key fieldName in a map. It
// follows pointers. A missing map key, and any name on no value, is no
// value, as under html/template's default missingkey option.
//
// It is text/template's evalField for a name with no arguments, no piped
// value, and missingkey=default.
func Field(receiver reflect.Value, fieldName string) (reflect.Value, error) {
	return field(receiver, fieldName, MissingKeyDefault)
}

// FieldMissing is Field in a set whose missingkey option is missing.
func FieldMissing(receiver reflect.Value, fieldName string, missing MissingKey) (reflect.Value, error) {
	return field(receiver, fieldName, missing)
}

// NoEntry is the error missingkey=error gives for a name on no value.
// Compiled code checks for no value itself before a method call, whose
// arguments text/template does not evaluate then.
func NoEntry(fieldName string) error {
	return fmt.Errorf("nil data; no entry for key %q", fieldName)
}

func field(receiver reflect.Value, fieldName string, missing MissingKey) (reflect.Value, error) {
	if !receiver.IsValid() {
		if missing == MissingKeyError { // Treat invalid value as missing map key.
			return reflect.Value{}, NoEntry(fieldName)
		}
		return reflect.Value{}, nil
	}
	typ := receiver.Type()
	receiver, isNil := indirectValue(receiver)
	// The data of a dict, and of most error pages, has no methods to
	// look for, and is read with a map lookup.
	if receiver.Type() == mapStringAnyType && receiver.CanInterface() {
		return anyMapField(receiver.Interface().(map[string]any), fieldName, missing)
	}
	if receiver.Kind() == reflect.Interface && isNil {
		// Calling a method on a nil interface can't work. The
		// MethodByName method call below would panic.
		return reflect.Value{}, fmt.Errorf("nil pointer evaluating %s.%s", typ, fieldName)
	}

	// Unless it's an interface, need to get to a value of type *T to guarantee
	// we see all methods of T and *T.
	ptr := receiver
	if ptr.Kind() != reflect.Interface && ptr.Kind() != reflect.Pointer && ptr.CanAddr() {
		ptr = ptr.Addr()
	}
	if method := ptr.MethodByName(fieldName); method.IsValid() {
		return callValue(method, fieldName, nil)
	}
	// It's not a method; must be a field of a struct or an element of a map.
	switch receiver.Kind() {
	case reflect.Struct:
		tField, ok := receiver.Type().FieldByName(fieldName)
		if ok {
			field, err := receiver.FieldByIndexErr(tField.Index)
			if !tField.IsExported() {
				return reflect.Value{}, fmt.Errorf("%s is an unexported field of struct type %s", fieldName, typ)
			}
			if err != nil {
				return reflect.Value{}, err
			}
			return field, nil
		}
	case reflect.Map:
		// If it's a map, attempt to use the field name as a key.
		nameVal := reflect.ValueOf(fieldName)
		if nameVal.Type().AssignableTo(receiver.Type().Key()) {
			return mapField(receiver, fieldName, missing)
		}
	case reflect.Pointer:
		etyp := receiver.Type().Elem()
		if etyp.Kind() == reflect.Struct {
			if _, ok := etyp.FieldByName(fieldName); !ok {
				// If there's no such field, say "can't evaluate"
				// instead of "nil pointer evaluating".
				break
			}
		}
		if isNil {
			return reflect.Value{}, fmt.Errorf("nil pointer evaluating %s.%s", typ, fieldName)
		}
	}
	return reflect.Value{}, fmt.Errorf("can't evaluate field %s in type %s", fieldName, typ)
}

var anyType = reflect.TypeFor[any]()

// anyMapField is mapField for a map[string]any, by a Go map lookup. The
// element is held as MapIndex holds it: an interface value, and not
// addressable, which an element of an array that is not addressable is
// not either.
func anyMapField(m map[string]any, fieldName string, missing MissingKey) (reflect.Value, error) {
	v, ok := m[fieldName]
	if !ok {
		switch missing {
		case MissingKeyZero:
			return reflect.Zero(anyType), nil
		case MissingKeyError:
			return reflect.Value{}, fmt.Errorf("map has no entry for key %q", fieldName)
		}
		return reflect.Value{}, nil
	}
	return reflect.ValueOf([1]any{v}).Index(0), nil
}

// mapField is the element of receiver, a map whose key type a string
// is assignable to, under fieldName.
func mapField(receiver reflect.Value, fieldName string, missing MissingKey) (reflect.Value, error) {
	result := receiver.MapIndex(reflect.ValueOf(fieldName))
	if !result.IsValid() {
		switch missing {
		case MissingKeyDefault:
			// Just use the invalid value.
		case MissingKeyZero:
			result = reflect.Zero(receiver.Type().Elem())
		case MissingKeyError:
			return reflect.Value{}, fmt.Errorf("map has no entry for key %q", fieldName)
		}
	}
	return result, nil
}

// Method is .fieldName on receiver given arguments, the last of them the
// value piped in if there is one: a method called with them. It is
// MethodOf and then Call, for arguments already evaluated.
func Method(receiver reflect.Value, fieldName string, args ...Operand) (reflect.Value, error) {
	m, err := MethodOf(receiver, fieldName)
	if err != nil {
		return reflect.Value{}, err
	}
	return m.Call(args...)
}

// BoundMethod is a method found on a receiver, to be called with
// arguments evaluated after it was found.
type BoundMethod struct {
	fn   reflect.Value
	name string
}

// MethodOf is .fieldName on receiver for a call given arguments, found
// before they are evaluated, as text/template's evalField finds it: the
// method is bound to the receiver as it is then, through any pointer, so
// an argument that changes what the receiver's location holds does not
// change which value is called. A field or map key cannot take
// arguments, so finding one is an error. receiver must be a value; on no
// value text/template evaluates none of the arguments, and compiled code
// does not call MethodOf.
func MethodOf(receiver reflect.Value, fieldName string) (BoundMethod, error) {
	fail := func(err error) (BoundMethod, error) { return BoundMethod{}, err }
	typ := receiver.Type()
	receiver, isNil := indirectValue(receiver)
	if receiver.Kind() == reflect.Interface && isNil {
		return fail(fmt.Errorf("nil pointer evaluating %s.%s", typ, fieldName))
	}
	ptr := receiver
	if ptr.Kind() != reflect.Interface && ptr.Kind() != reflect.Pointer && ptr.CanAddr() {
		ptr = ptr.Addr()
	}
	if method := ptr.MethodByName(fieldName); method.IsValid() {
		return BoundMethod{fn: method, name: fieldName}, nil
	}
	switch receiver.Kind() {
	case reflect.Struct:
		tField, ok := receiver.Type().FieldByName(fieldName)
		if ok {
			_, err := receiver.FieldByIndexErr(tField.Index)
			if !tField.IsExported() {
				return fail(fmt.Errorf("%s is an unexported field of struct type %s", fieldName, typ))
			}
			if err != nil {
				return fail(err)
			}
			return fail(fmt.Errorf("%s has arguments but cannot be invoked as function", fieldName))
		}
	case reflect.Map:
		nameVal := reflect.ValueOf(fieldName)
		if nameVal.Type().AssignableTo(receiver.Type().Key()) {
			return fail(fmt.Errorf("%s is not a method but has arguments", fieldName))
		}
	case reflect.Pointer:
		etyp := receiver.Type().Elem()
		if etyp.Kind() == reflect.Struct {
			if _, ok := etyp.FieldByName(fieldName); !ok {
				break
			}
		}
		if isNil {
			return fail(fmt.Errorf("nil pointer evaluating %s.%s", typ, fieldName))
		}
	}
	return fail(fmt.Errorf("can't evaluate field %s in type %s", fieldName, typ))
}

// Call calls the method with args, each converted to its parameter's
// type as text/template converts it.
func (m BoundMethod) Call(args ...Operand) (reflect.Value, error) {
	return callValue(m.fn, m.name, args)
}

// Call calls fn, a function from the set's FuncMap whose type the
// generated code cannot spell, with args, each converted to its
// parameter's type as text/template converts it, and with
// text/template's handling of a panic and of a second, error, result.
func Call(name string, fn reflect.Value, args ...Operand) (reflect.Value, error) {
	return callValue(fn, name, args)
}

// callValue is text/template's evalCall for a method or a function that
// is not a builtin, with its arguments evaluated. The piped value, if
// any, is the last of args: text/template checks it against the last
// parameter as it does the others.
func callValue(fun reflect.Value, name string, args []Operand) (reflect.Value, error) {
	call, err := startCall(fun, name, len(args))
	if err != nil {
		return reflect.Value{}, err
	}
	for _, op := range args {
		if err := call.Add(op); err != nil {
			return reflect.Value{}, err
		}
	}
	return call.Call()
}

// Args is a call by reflection, of a method or of a FuncMap function,
// given its arguments one at a time. text/template converts each
// argument to its parameter's type as soon as it has evaluated it, and
// before it evaluates the next, so the conversion fails before a later
// argument runs. Compiled code adds each argument as it evaluates it,
// where a later one could have an effect; Call and BoundMethod.Call take
// them all at once where none could.
type Args struct {
	fn   reflect.Value
	name string
	argv []reflect.Value
}

// Args starts a call of the method with n arguments, the piped value
// among them. It fails, before any argument is evaluated, where
// text/template does: for the wrong number of arguments, or a method
// whose results a template cannot use.
func (m BoundMethod) Args(n int) (Args, error) { return startCall(m.fn, m.name, n) }

// CallArgs starts a call of fn, the FuncMap function called name, with
// n arguments, as BoundMethod.Args does.
func CallArgs(name string, fn reflect.Value, n int) (Args, error) { return startCall(fn, name, n) }

func startCall(fun reflect.Value, name string, n int) (Args, error) {
	typ := fun.Type()
	if typ.IsVariadic() {
		if n < typ.NumIn()-1 {
			return Args{}, fmt.Errorf("wrong number of args for %s: want at least %d got %d", name, typ.NumIn()-1, n)
		}
	} else if n != typ.NumIn() {
		return Args{}, fmt.Errorf("wrong number of args for %s: want %d got %d", name, typ.NumIn(), n)
	}
	if err := goodFunc(name, typ); err != nil {
		return Args{}, err
	}
	return Args{fn: fun, name: name, argv: make([]reflect.Value, 0, n)}, nil
}

// Add converts op to the type of the next parameter, as text/template's
// evalArg does, and keeps it for the call. The value is kept where it
// is: an addressable one is read when the call is made, as reflect's
// Call reads it, so an argument after it that changes it changes what
// the function gets.
func (a *Args) Add(op Operand) error {
	typ := a.fn.Type()
	i := len(a.argv)
	var param reflect.Type
	if typ.IsVariadic() && i >= typ.NumIn()-1 {
		param = typ.In(typ.NumIn() - 1).Elem() // Argument is a slice.
	} else {
		param = typ.In(i)
	}
	v, err := evalArg(param, op)
	if err != nil {
		return err
	}
	a.argv = append(a.argv, v)
	return nil
}

// Call makes the call with the arguments added, with text/template's
// handling of a panic and of a second, error, result.
func (a *Args) Call() (reflect.Value, error) {
	v, err := safeCall(a.fn, a.argv)
	if err != nil {
		return reflect.Value{}, CallError(a.name, err)
	}
	if v.Type() == reflectValueType {
		v = v.Interface().(reflect.Value)
	}
	return v, nil
}

// CallError is the error a function, method or builtin called name
// returned or panicked with, as execution reports it.
func CallError(name string, err error) error {
	return fmt.Errorf("error calling %s: %w", name, err)
}

// goodFunc reports whether the function or method has the right result types.
func goodFunc(name string, typ reflect.Type) error {
	// We allow functions with 1 result or 2 results where the second is an error.
	switch numOut := typ.NumOut(); {
	case numOut == 1:
		return nil
	case numOut == 2 && typ.Out(1) == errorType:
		return nil
	case numOut == 2:
		return fmt.Errorf("invalid function signature for %s: second return value should be error; is %s", name, typ.Out(1))
	default:
		return fmt.Errorf("function %s has %d return values; should be 1 or 2", name, typ.NumOut())
	}
}

// safeCall runs fun.Call(args), and returns the resulting value and error, if
// any. If the call panics, the panic value is returned as an error.
func safeCall(fun reflect.Value, args []reflect.Value) (val reflect.Value, err error) {
	defer Catch(&err)
	ret := fun.Call(args)
	if len(ret) == 2 && !ret[1].IsNil() {
		return ret[0], ret[1].Interface().(error)
	}
	return ret[0], nil
}

// Catch, deferred around a call of a template's function, turns a panic
// into the error text/template's safeCall makes of it. It must be
// deferred directly, so that its recover stops the panic.
func Catch(errp *error) {
	if r := recover(); r != nil {
		if e, ok := r.(error); ok {
			*errp = e
		} else {
			*errp = fmt.Errorf("%v", r)
		}
	}
}

// Operand is an argument for a parameter whose type compiled code does
// not know: a method's, or a function's it calls by reflection. It is a
// value the template evaluated, or a constant the template wrote, which
// text/template reads according to the parameter's type: 3 is an int for
// an int and a float64 for a float64.
type Operand struct {
	value reflect.Value
	c     *constant
}

// Val is the operand v.
func Val(v reflect.Value) Operand { return Operand{value: v} }

type constKind uint8

const (
	constBool constKind = iota + 1
	constString
	constNumber
	constNil
)

type constant struct {
	kind   constKind
	text   string // as written, for errors
	bool   bool
	string string
	number Number
}

// Number is a number constant, as text/template/parse's NumberNode has it.
type Number struct {
	IsInt, IsUint, IsFloat, IsComplex bool
	Int64                             int64
	Uint64                            uint64
	Float64                           float64
	Complex128                        complex128
	Text                              string // as written
}

// ConstBool, ConstString, ConstNumber and ConstNil are constants the
// template wrote. quoted is the string as written.
func ConstBool(b bool) Operand {
	return Operand{c: &constant{kind: constBool, bool: b, text: fmt.Sprint(b)}}
}

func ConstString(s, quoted string) Operand {
	return Operand{c: &constant{kind: constString, string: s, text: quoted}}
}

func ConstNumber(n Number) Operand {
	return Operand{c: &constant{kind: constNumber, number: n, text: n.Text}}
}

func ConstNil() Operand { return Operand{c: &constant{kind: constNil, text: "nil"}} }

// Param is op as a parameter of type T, which the generated code spells,
// converted as text/template converts an argument to a parameter of
// that type.
func Param[T any](op Operand) (T, error) {
	var t T
	v, err := evalArg(reflect.TypeFor[T](), op)
	if err != nil {
		return t, err
	}
	reflect.ValueOf(&t).Elem().Set(v)
	return t, nil
}

// Convert is Param for an argument that more arguments follow. It fails
// where Param does, which is where text/template does, before the next
// argument is evaluated; but it keeps the value where it is, as Add
// does, and As reads it when the call is made: an argument after it may
// change what an addressable value holds, and the function gets the
// change.
func Convert[T any](op Operand) (reflect.Value, error) {
	return evalArg(reflect.TypeFor[T](), op)
}

// As is v, which Convert converted for a parameter of type T, as a T.
func As[T any](v reflect.Value) T {
	var t T
	reflect.ValueOf(&t).Elem().Set(v)
	return t
}

// Result is r, the result of a function of the set's FuncMap that
// compiled code called directly, as text/template holds the result of a
// call: of the function's result type, interface or not, and never
// addressable. A reflect.Value result is the value it holds.
func Result[T any](r T) reflect.Value {
	typ := reflect.TypeFor[T]()
	switch {
	case typ == reflectValueType:
		return any(r).(reflect.Value)
	case typ.Kind() == reflect.Interface:
		// An element of an array that is not addressable is not either.
		return reflect.ValueOf([1]T{r}).Index(0)
	}
	return reflect.ValueOf(r)
}

// Constant is a number written as a command or as an argument of type
// any: text/template's idealConstant, which reads it as Go would an
// untyped constant.
func Constant(n Number) (reflect.Value, error) {
	return idealConstant(n)
}

// evalArg is text/template's evalArg for an evaluated value or a
// constant: op as an argument of type typ.
func evalArg(typ reflect.Type, op Operand) (reflect.Value, error) {
	c := op.c
	if c == nil {
		return validateType(op.value, typ)
	}
	if c.kind == constNil {
		if canBeNil(typ) {
			return reflect.Zero(typ), nil
		}
		return reflect.Value{}, fmt.Errorf("cannot assign nil to %s", typ)
	}
	switch typ.Kind() {
	case reflect.Bool:
		if c.kind == constBool {
			value := reflect.New(typ).Elem()
			value.SetBool(c.bool)
			return value, nil
		}
		return reflect.Value{}, fmt.Errorf("expected bool; found %s", c.text)
	case reflect.Complex64, reflect.Complex128:
		if c.kind == constNumber && c.number.IsComplex {
			value := reflect.New(typ).Elem()
			value.SetComplex(c.number.Complex128)
			return value, nil
		}
		return reflect.Value{}, fmt.Errorf("expected complex; found %s", c.text)
	case reflect.Float32, reflect.Float64:
		if c.kind == constNumber && c.number.IsFloat {
			value := reflect.New(typ).Elem()
			value.SetFloat(c.number.Float64)
			return value, nil
		}
		return reflect.Value{}, fmt.Errorf("expected float; found %s", c.text)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if c.kind == constNumber && c.number.IsInt {
			value := reflect.New(typ).Elem()
			value.SetInt(c.number.Int64)
			return value, nil
		}
		return reflect.Value{}, fmt.Errorf("expected integer; found %s", c.text)
	case reflect.Interface:
		if typ.NumMethod() == 0 {
			return evalEmptyInterface(c)
		}
	case reflect.Struct:
		if typ == reflectValueType {
			v, err := evalEmptyInterface(c)
			return reflect.ValueOf(v), err
		}
	case reflect.String:
		if c.kind == constString {
			value := reflect.New(typ).Elem()
			value.SetString(c.string)
			return value, nil
		}
		return reflect.Value{}, fmt.Errorf("expected string; found %s", c.text)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if c.kind == constNumber && c.number.IsUint {
			value := reflect.New(typ).Elem()
			value.SetUint(c.number.Uint64)
			return value, nil
		}
		return reflect.Value{}, fmt.Errorf("expected unsigned integer; found %s", c.text)
	}
	return reflect.Value{}, fmt.Errorf("can't handle %s for arg of type %s", c.text, typ)
}

// evalEmptyInterface is a constant as an argument of type any.
func evalEmptyInterface(c *constant) (reflect.Value, error) {
	switch c.kind {
	case constBool:
		return reflect.ValueOf(c.bool), nil
	case constNumber:
		return idealConstant(c.number)
	case constString:
		return reflect.ValueOf(c.string), nil
	}
	return reflect.Value{}, fmt.Errorf("can't handle assignment of %s to empty interface argument", c.text)
}

// idealConstant is called to return the value of a number in a context where
// we don't know the type. In that case, the syntax of the number tells us
// its type, and we use Go rules to resolve. Note there is no such thing as
// a uint ideal constant in this situation - the value must be of int type.
func idealConstant(constant Number) (reflect.Value, error) {
	// These are ideal constants but we don't know the type
	// and we have no context.  (If it was a method argument,
	// we'd know what we need.) The syntax guides us to some extent.
	switch {
	case constant.IsComplex:
		return reflect.ValueOf(constant.Complex128), nil // incontrovertible.

	case constant.IsFloat &&
		!isHexInt(constant.Text) && !isRuneInt(constant.Text) &&
		strings.ContainsAny(constant.Text, ".eEpP"):
		return reflect.ValueOf(constant.Float64), nil

	case constant.IsInt:
		n := int(constant.Int64)
		if int64(n) != constant.Int64 {
			return reflect.Value{}, fmt.Errorf("%s overflows int", constant.Text)
		}
		return reflect.ValueOf(n), nil

	case constant.IsUint:
		return reflect.Value{}, fmt.Errorf("%s overflows int", constant.Text)
	}
	return reflect.Value{}, nil
}

func isRuneInt(s string) bool {
	return len(s) > 0 && s[0] == '\''
}

func isHexInt(s string) bool {
	return len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') && !strings.ContainsAny(s, "pP")
}

// canBeNil reports whether an untyped nil can be assigned to the type. See reflect.Zero.
func canBeNil(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	case reflect.Struct:
		return typ == reflectValueType
	}
	return false
}

// validateType guarantees that the value is valid and assignable to the type.
func validateType(value reflect.Value, typ reflect.Type) (reflect.Value, error) {
	if !value.IsValid() {
		if typ == nil {
			// An untyped nil interface{}. Accept as a proper nil value.
			return reflect.ValueOf(nil), nil
		}
		if canBeNil(typ) {
			// Like above, but use the zero value of the non-nil type.
			return reflect.Zero(typ), nil
		}
		return reflect.Value{}, fmt.Errorf("invalid value; expected %s", typ)
	}
	if typ == reflectValueType && value.Type() != typ {
		return reflect.ValueOf(value), nil
	}
	if typ != nil && !value.Type().AssignableTo(typ) {
		if value.Kind() == reflect.Interface && !value.IsNil() {
			value = value.Elem()
			if value.Type().AssignableTo(typ) {
				return value, nil
			}
			// fallthrough
		}
		// Does one dereference or indirection work? We could do more, as we
		// do with method receivers, but that gets messy and method receivers
		// are much more constrained, so it makes more sense there than here.
		// Besides, one is almost always all you need.
		switch {
		case value.Kind() == reflect.Pointer && value.Type().Elem().AssignableTo(typ):
			value = value.Elem()
			if !value.IsValid() {
				return reflect.Value{}, fmt.Errorf("dereference of nil pointer of type %s", typ)
			}
		case reflect.PointerTo(value.Type()).AssignableTo(typ) && value.CanAddr():
			value = value.Addr()
		default:
			return reflect.Value{}, fmt.Errorf("wrong type for value; expected %s; got %s", typ, value.Type())
		}
	}
	return value, nil
}

// indirectValue is text/template's indirect; escape_content.go has
// html/template's under the name. It returns the item at the end of
// indirection, and a bool to indicate if it's nil. If the returned bool
// is true, the returned value's kind will be either a pointer or
// interface.
func indirectValue(v reflect.Value) (rv reflect.Value, isNil bool) {
	for ; v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface; v = v.Elem() {
		if v.IsNil() {
			return v, true
		}
	}
	return v, false
}

// indirectInterface returns the concrete value in an interface value,
// or else the zero reflect.Value. That is, if v represents the interface
// value x, the result is the same as reflect.ValueOf(x): the fact that x
// was an interface value is forgotten.
func indirectInterface(v reflect.Value) reflect.Value {
	if v.Kind() != reflect.Interface {
		return v
	}
	if v.IsNil() {
		return reflect.Value{}
	}
	return v.Elem()
}

// WriteValue writes v as text/template prints the value of an action.
// An escaped action ends in an escaper, whose string compiled code
// writes itself; this is for one ending in html or urlquery, which
// escaping keeps when they would do the escaper's work.
func WriteValue(b *bytes.Buffer, at string, v reflect.Value) error {
	iface, ok := printableValue(v)
	if !ok {
		return fmt.Errorf("can't print %s of type %s", at, v.Type())
	}
	fmt.Fprint(b, iface)
	return nil
}

// printableValue returns the, possibly indirected, interface value inside v that
// is best for a call to formatted printer.
func printableValue(v reflect.Value) (any, bool) {
	if v.Kind() == reflect.Pointer {
		v, _ = indirectValue(v) // fmt.Fprint handles nil.
	}
	if !v.IsValid() {
		return "<no value>", true
	}

	if !v.Type().Implements(errorType) && !v.Type().Implements(fmtStringerType) {
		if v.CanAddr() && (reflect.PointerTo(v.Type()).Implements(errorType) || reflect.PointerTo(v.Type()).Implements(fmtStringerType)) {
			v = v.Addr()
		} else {
			switch v.Kind() {
			case reflect.Chan, reflect.Func:
				return nil, false
			}
		}
	}
	return v.Interface(), true
}

// ErrBreak and ErrContinue, returned by the function Range calls, are
// {{break}} and {{continue}}: the range stops, or goes on to the next
// element. Range consumes them; they never reach the caller.
var (
	ErrBreak    = errors.New("tmplrt: {{break}} outside {{range}}")
	ErrContinue = errors.New("tmplrt: {{continue}} outside {{range}}")
)

// Range calls fn for each element of v, as {{range}} with one variable
// or none iterates: over a slice or array by index, a map in sorted key
// order, an integer counting up from zero, a channel until it closes,
// and an iterator function, all through pointers. k is the index or
// key, no value where there is none, and e is the element as
// text/template has it: a slice element is addressable, an element of
// []any is an interface value. For an iter.Seq2, e is the iterator's
// first value. Nothing (no value, an empty map, a nil channel) runs fn
// and is no error; anything else that cannot be ranged over is.
//
// An error from fn stops the range and is returned as it is, except
// ErrBreak, which stops it without one, and ErrContinue.
func Range(v reflect.Value, fn func(k, e reflect.Value) error) error {
	return iterate(v, false, fn)
}

// Range2 is Range for {{range $k, $e := ...}}: an iter.Seq2 gives both
// its values, and an integer or an iter.Seq, which have only one, is an
// error.
func Range2(v reflect.Value, fn func(k, e reflect.Value) error) error {
	return iterate(v, true, fn)
}

// iterate is text/template's walkRange without the body: two is
// whether the range declares two variables.
func iterate(v reflect.Value, two bool, fn func(k, e reflect.Value) error) error {
	val, _ := indirectValue(v)
	// one runs one iteration, and reports whether to stop and the error
	// to stop with.
	one := func(k, e reflect.Value) (stop bool, err error) {
		switch err := fn(k, e); err {
		case nil, ErrContinue:
			return false, nil
		case ErrBreak:
			return true, nil
		default:
			return true, err
		}
	}
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if two {
			return fmt.Errorf("can't use %v to iterate over more than one variable", val)
		}
		for e := range val.Seq() {
			// Pass element as second value, as we do for channels.
			if stop, err := one(reflect.Value{}, e); stop {
				return err
			}
		}
		return nil
	case reflect.Array, reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			if stop, err := one(reflect.ValueOf(i), val.Index(i)); stop {
				return err
			}
		}
		return nil
	case reflect.Map:
		for _, m := range sortMap(val) {
			if stop, err := one(m.Key, m.Value); stop {
				return err
			}
		}
		return nil
	case reflect.Chan:
		if val.IsNil() {
			return nil
		}
		if val.Type().ChanDir() == reflect.SendDir {
			return fmt.Errorf("range over send-only channel %v", val)
		}
		for i := 0; ; i++ {
			elem, ok := val.Recv()
			if !ok {
				return nil
			}
			if stop, err := one(reflect.ValueOf(i), elem); stop {
				return err
			}
		}
	case reflect.Invalid:
		return nil // An invalid value is likely a nil map, etc. and acts like an empty map.
	case reflect.Func:
		if val.Type().CanSeq() {
			if two {
				return fmt.Errorf("can't use %v iterate over more than one variable", val)
			}
			for e := range val.Seq() {
				if stop, err := one(reflect.Value{}, e); stop {
					return err
				}
			}
			return nil
		}
		if val.Type().CanSeq2() {
			for k, e := range val.Seq2() {
				if !two {
					// With one variable, the element is the
					// iterator's first value.
					k, e = reflect.Value{}, k
				}
				if stop, err := one(k, e); stop {
					return err
				}
			}
			return nil
		}
	}
	return fmt.Errorf("range can't iterate over %v", val)
}

// MaxExecDepth is text/template's limit on templates calling templates,
// which an accidentally recursive template reaches: execution fails
// there instead of overflowing the stack.
var MaxExecDepth = initMaxExecDepth()

func initMaxExecDepth() int {
	if runtime.GOARCH == "wasm" {
		return 1000
	}
	return 100000
}

// DepthError is the error for a {{template}} call at MaxExecDepth.
func DepthError() error {
	return fmt.Errorf("exceeded maximum template depth (%v)", MaxExecDepth)
}

// Fail is an error compiled code knows of when it is generated: a call
// with the wrong number of arguments, say. Execution meets it only if
// it gets there, as in text/template, and it takes the place of the
// value the call would have had.
func Fail(text string) (reflect.Value, error) { return reflect.Value{}, errors.New(text) }

// ValueArg is v as text/template passes it to a parameter of type
// reflect.Value, as and and or take theirs: as itself, unless its own
// type is reflect.Value, when it is the value it holds.
func ValueArg(v reflect.Value) reflect.Value { return param(v) }

// NegativeZero is -0.0, which a template can write and Go source cannot.
var NegativeZero = math.Copysign(0, -1)

// BindError is Bind's error when funcs has no function called name of
// the type the code was compiled against.
func BindError(name string, fn any) error {
	return fmt.Errorf("tmplrt: function %s is %T, not the type the code was compiled for", name, fn)
}

// ExecError is err, which compiled code met executing the template
// called name at the action at, as text/template returns it. An err
// that already is one, from inside a {{range}} or a {{template}}, is
// returned unchanged.
func ExecError(name, at string, err error) error {
	if e, ok := err.(template.ExecError); ok {
		return e
	}
	return template.ExecError{
		Name: name,
		Err:  fmt.Errorf("template: %s: executing %q at <%s>: %w", name, name, at, err),
	}
}

// Recover, deferred by compiled code, turns a panic in a function the
// template called into the error text/template returns for one. It must
// be deferred directly, so that its recover stops the panic.
func Recover(errp *error, name string) {
	r := recover()
	if r == nil {
		return
	}
	err, ok := r.(error)
	if !ok {
		err = fmt.Errorf("%v", r)
	}
	*errp = template.ExecError{Name: name, Err: fmt.Errorf("template: %s: %w", name, err)}
}
