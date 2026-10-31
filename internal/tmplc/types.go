package tmplc

import (
	"go/types"
	"regexp"
	"slices"
	"strings"
)

// val is a value as the generated code holds it: a Go expression, and
// what the compiler knows of its type. With t nil the expression is a
// reflect.Value, as in text/template, and the code calls tmplrt for
// everything that depends on the data. With t set the expression has
// that static Go type, and the code reads fields, calls methods and
// escapes strings directly.
//
// text/template also knows whether a value is addressable, which
// decides whether it finds pointer methods on it. A typed val that is
// addressable is held as a pointer to it (addr), so that a pointer
// method gets the value itself and not a copy, and turning it back into
// a reflect.Value gives an addressable one.
type val struct {
	code string
	t    types.Type
	addr bool
	// path is how typed code came to t, which a Bind retraces to check
	// that t is still the type the code was written for; nil for a type
	// no data decides, a constant's or a builtin's result, which is
	// predeclared.
	path *tpath
}

// tpath is a path as tmplrt.Fact has it: from the dot of the function
// being written, or from the result of the FuncMap function whose funcs
// struct field is fn, a step at a time.
type tpath struct {
	fn    string
	steps []string
}

// to is path with step after it.
func (path *tpath) to(step string) *tpath {
	if path == nil {
		return nil
	}
	return &tpath{fn: path.fn, steps: append(slices.Clip(path.steps), step)}
}

// key is path as one string, to tell two paths apart.
func (path *tpath) key() string {
	return path.fn + "\x00" + strings.Join(path.steps, "\x00")
}

// member is the step to .name on value, a field, or with sig a method's
// result, as tmplrt.Fact.Path writes it: text/template looks for it in
// the method set of an addressable value's pointer.
func member(value val, name string, sig *types.Signature) string {
	step := "." + name
	if _, isPtr := value.t.Underlying().(*types.Pointer); !isPtr && value.addr {
		step = "&" + step
	}
	if sig != nil {
		text, _ := sigString(sig)
		step += "() " + text
	}
	return step
}

func dynamic(code string) val { return val{code: code} }

// pure matches the code of a val that can be written again where it is
// used instead of being held in a variable: a variable, or fields of
// one, perhaps behind &.
var pure = regexp.MustCompile(
	`^&?[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`,
)

// load is the Go expression for what value stands for, not a pointer
// to it.
func load(value val) string {
	if !value.addr {
		return value.code
	}
	if value.code[0] == '&' && pure.MatchString(value.code) {
		return value.code[1:]
	}
	return "(*" + value.code + ")"
}

// selector is value's code where a field or method selector follows it.
func selector(value val) string {
	if value.addr {
		// Go takes an addressable value's address itself for a pointer
		// method, and dereferences a pointer for a value method or a
		// field, so the pointer, or what it points to, will do.
		switch {
		case value.code[0] == '&' && pure.MatchString(value.code):
			return value.code[1:]
		case isIdent(value.code):
			return value.code
		}
		return "(" + value.code + ")"
	}
	if pure.MatchString(value.code) {
		return value.code
	}
	return "(" + value.code + ")"
}

func isInterface(typ types.Type) bool { return types.IsInterface(typ) }

func isEmptyInterface(typ types.Type) bool {
	iface, ok := typ.Underlying().(*types.Interface)
	return ok && iface.Empty()
}

// isNamed reports whether typ is the named type pkg.name.
func isNamed(typ types.Type, pkg, name string) bool {
	named, ok := types.Unalias(typ).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == pkg &&
		named.Obj().Name() == name
}

// isReflectValue reports whether typ is reflect.Value, which text/template
// treats as the value it holds when a function returns one: the code
// keeps such a value dynamic.
func isReflectValue(
	typ types.Type,
) bool {
	return isNamed(typ, "reflect", "Value")
}

// printsItself reports whether fmt would print a value of type typ by a
// method of its own rather than as its underlying value, as an escaper
// prints anything that is not a string. Only the value's own method set
// counts: an escaper gets the value, never a pointer to it.
func printsItself(typ types.Type) bool {
	ms := types.NewMethodSet(typ)
	for _, name := range []string{"String", "Error", "Format", "GoString"} {
		if ms.Lookup(nil, name) != nil {
			return true
		}
	}
	return false
}

// plainString is value as a Go string, if an escaper treats it as one: a
// string, or a named string type with no method fmt prints it by and
// not one of html/template's content types, whose type tells the
// escaper the value is already safe there. exact is whether value's type is
// string itself.
func plainString(value val) (text string, exact, ok bool) {
	if value.t == nil {
		return "", false, false
	}
	basic, isBasic := value.t.Underlying().(*types.Basic)
	if !isBasic || basic.Info()&types.IsString == 0 ||
		basic.Info()&types.IsUntyped != 0 {
		return "", false, false
	}
	if types.Identical(value.t, types.Typ[types.String]) {
		return load(value), true, true
	}
	named, isNamedType := types.Unalias(value.t).(*types.Named)
	if !isNamedType ||
		named.Obj().Pkg() != nil &&
			named.Obj().Pkg().Path() == "html/template" ||
		printsItself(value.t) {
		return "", false, false
	}
	return "string(" + load(value) + ")", false, true
}

// plainInt is value as an int64 or uint64 expression, if it is an integer an
// escaper prints as its digits: one whose type has no method fmt prints
// it by.
func plainInt(value val) (number string, signed, ok bool) {
	if value.t == nil {
		return "", false, false
	}
	basic, isBasic := value.t.Underlying().(*types.Basic)
	if !isBasic || basic.Info()&types.IsInteger == 0 ||
		basic.Info()&types.IsUntyped != 0 ||
		printsItself(value.t) {
		return "", false, false
	}
	if basic.Info()&types.IsUnsigned != 0 {
		return "uint64(" + load(value) + ")", false, true
	}
	return "int64(" + load(value) + ")", true, true
}

// truth is the Go condition {{if}} tests for value.
func (emitter *writer) truth(value val) string {
	if value.t == nil {
		return emitter.file.rt + ".Truth(" + value.code + ")"
	}
	loaded := load(value)
	switch underlying := value.t.Underlying().(type) {
	case *types.Basic:
		switch {
		case underlying.Info()&types.IsBoolean != 0:
			return loaded
		case underlying.Info()&types.IsString != 0:
			return loaded + ` != ""`
		case underlying.Info()&types.IsNumeric != 0:
			return loaded + " != 0"
		case underlying.Kind() == types.UnsafePointer:
			return loaded + " != nil"
		}
	case *types.Slice, *types.Map, *types.Array:
		return "len(" + loaded + ") > 0"
	case *types.Pointer, *types.Chan, *types.Signature:
		return loaded + " != nil"
	case *types.Struct:
		// A struct is always true; its variable must still be used.
		if isIdent(loaded) {
			emitter.line("_ = %s", loaded)
		}
		return "true"
	case *types.Interface:
		// reflect.ValueOf takes the value out of the interface, as Truth
		// does with an interface value.
		return emitter.file.rt + ".Truth(" +
			emitter.file.reflect + ".ValueOf(" + loaded + "))"
	}
	return emitter.file.rt + ".Truth(" + emitter.dyn(value) + ")"
}

// dyn is value as a reflect.Value, as text/template would hold it: of its
// static type, interface or not, and addressable if value is.
func (emitter *writer) dyn(value val) string {
	switch {
	case value.t == nil:
		return value.code
	case value.addr:
		return emitter.file.reflect + ".ValueOf(" + value.code + ").Elem()"
	case isInterface(value.t):
		return emitter.file.rt + ".Result(" + value.code + ")"
	}
	return emitter.file.reflect + ".ValueOf(" + value.code + ")"
}

// anyOf is value as an argument of type any, as text/template passes a
// value to a parameter of that type.
func (emitter *writer) anyOf(value val) string {
	if value.t == nil {
		return emitter.file.rt + ".Arg(" + value.code + ")"
	}
	return load(value)
}

// unwrap is what text/template's evalPipeline does at the end of every
// command: a value in an empty interface is taken out of it, there and
// then, so that an argument after the command that stores another value
// in the interface's location does not change it.
func (emitter *writer) unwrap(value val) val {
	switch {
	case value.t == nil:
		return emitter.hold(
			dynamic(emitter.file.rt + ".Unwrap(" + value.code + ")"),
		)
	case isEmptyInterface(value.t):
		return emitter.hold(
			dynamic(emitter.file.reflect + ".ValueOf(" + load(value) + ")"),
		)
	}
	return value
}

// paramType is the type of the index'th argument a call of sig passes: of
// the slice's element for a variadic one.
func paramType(sig *types.Signature, index int) types.Type {
	count := sig.Params().Len()
	if sig.Variadic() && index >= count-1 {
		return sig.Params().At(count - 1).Type().(*types.Slice).Elem()
	}
	return sig.Params().At(index).Type()
}

// arityOK reports whether sig takes count arguments.
func arityOK(sig *types.Signature, count int) bool {
	if sig.Variadic() {
		return count >= sig.Params().Len()-1
	}
	return count == sig.Params().Len()
}

// assignedVars are the Go variables a template sets after declaring
// them. The code holds those as reflect.Values: what is assigned need
// not have the type of what was declared.
func assignedVars(statements block) map[string]bool {
	out := map[string]bool{}
	var walk func(block)
	walk = func(statements block) {
		for _, statement := range statements {
			switch concrete := statement.(type) {
			case assign:
				for _, name := range concrete.vars {
					out[name] = true
				}
			case ifStmt:
				walk(concrete.then)
				walk(concrete.els)
			case withStmt:
				walk(concrete.then)
				walk(concrete.els)
			case rangeStmt:
				if concrete.assign {
					for _, name := range concrete.vars {
						out[name] = true
					}
				}
				walk(concrete.body)
				walk(concrete.els)
			}
		}
	}
	walk(statements)
	return out
}
