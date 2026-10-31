package tmplrt

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl/internal/tmplhook"
)

// Typed compiled code is written for the types an app had when
// rastrillo generate ran, and fingerprints do not cover them. Changed
// since, in a way the code still builds against, a type could make the
// code read its data otherwise than text/template does: an int64 field
// made a uint64 is still converted with int64(), and an integer type
// given a String method still has its digits written. So the generator
// writes down, beside each function, the types it read and what it
// found, and a Bind checks them against the program's own types before
// it runs the code.

// Facts are what one compiled function assumes of the types it reads.
type Facts struct {
	// Dot is the type of the function's dot, which a fact's path starts
	// from unless it names a function; nil when the function reads its
	// dot by reflection.
	Dot reflect.Type
	// Funcs holds the type of each FuncMap function whose result a path
	// starts from, by the name the fact gives it.
	Funcs map[string]reflect.Type
	Facts []Fact
}

// Fact is one type the code read, and what it was.
type Fact struct {
	// Func, if set, starts the path from the function's result.
	Func string
	// Path is how the code reached the type, a step at a time: ".Name"
	// is a field, found as text/template finds one on a value, so that
	// a method of that name would have been found first, and "&.Name"
	// one found on an addressable value; ".Name() sig" is the result of
	// a method of that signature, found the same way; "[]" is an element
	// of a slice, array or map, or what a pointer points to, and "[key]"
	// a map's key.
	Path []string
	// Type is Describe's description of the type.
	Type string
}

// Holds reports, by returning nil, that every fact of each of facts
// is true of the types the program was built with, and otherwise names
// the first that is not. A Bind checks the facts of the code it binds
// before it returns: a typed variant whose facts fail is left out, so
// its data takes the code that reads by reflection, and code that fails
// any other way is refused, so the set runs html/template.
func Holds(facts ...*Facts) error {
	for _, fs := range facts {
		for _, fact := range fs.Facts {
			if err := fs.check(fact); err != nil {
				tmplhook.TypesChanged(err)
				return err
			}
		}
	}
	return nil
}

func (fs *Facts) check(fact Fact) error {
	goType := fs.Dot
	from := "the data"
	if fact.Func != "" {
		fn := fs.Funcs[fact.Func]
		if fn == nil || fn.Kind() != reflect.Func || fn.NumOut() == 0 {
			return fmt.Errorf(typesChanged,
				"the function "+fact.Func, "missing", fact.Type)
		}
		goType, from = fn.Out(0), "the result of "+fact.Func
	}
	if goType == nil {
		return fmt.Errorf(typesChanged, from, "missing", fact.Type)
	}
	where := from
	for _, step := range fact.Path {
		where += " " + step
		next, ok := follow(goType, step)
		if !ok {
			return fmt.Errorf(typesChanged,
				where, "not found on "+typeName(goType), fact.Type)
		}
		goType = next
	}
	if got := Describe(goType); got != fact.Type {
		return fmt.Errorf(typesChanged, where, got, fact.Type)
	}
	return nil
}

// typesChanged is Holds' error: where the type was read, what it is
// now, and what the code was written for.
const typesChanged = "tmplrt: the code was compiled for other types; " +
	"run rastrillo generate: %s is %s, was %s"

// follow is the type step reaches from start, as Fact.Path describes
// steps, and whether text/template's lookup would reach it the way the
// code does.
func follow(start reflect.Type, step string) (reflect.Type, bool) {
	switch step {
	case "[]":
		return element(start)
	case "[key]":
		if start.Kind() != reflect.Map {
			return nil, false
		}
		return start.Key(), true
	}
	step, sig, _ := strings.Cut(step, " ")
	addressable := strings.HasPrefix(step, "&")
	name, method := strings.CutSuffix(
		strings.TrimPrefix(strings.TrimPrefix(step, "&"), "."), "()")
	// The method set text/template looks in: a pointer's, through which
	// the value is addressable, or the value's own.
	holder, methods := start, start
	switch {
	case start.Kind() == reflect.Pointer:
		holder = start.Elem()
		if start.Name() != "" || holder.Kind() == reflect.Pointer ||
			holder.Kind() == reflect.Interface {
			return nil, false
		}
	case addressable:
		methods = reflect.PointerTo(start)
	}
	match, found := methods.MethodByName(name)
	if method {
		if !found || funcString(match.Type, 1) != sig {
			return nil, false
		}
		return match.Type.Out(0), true
	}
	if found {
		return nil, false
	}
	return directField(holder, name)
}

// element is what a "[]" step reaches from container.
func element(container reflect.Type) (reflect.Type, bool) {
	switch container.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Pointer,
		reflect.Chan:
		return container.Elem(), true
	}
	return nil, false
}

// directField is the type of holder's field called name, unless holder
// is not a struct or the field is promoted through an embedded pointer,
// which may be nil, so is not one the code reads directly.
func directField(holder reflect.Type, name string) (reflect.Type, bool) {
	if holder.Kind() != reflect.Struct {
		return nil, false
	}
	field, ok := holder.FieldByName(name)
	if !ok {
		return nil, false
	}
	for _, index := range field.Index[:len(field.Index)-1] {
		embedded := holder.Field(index).Type
		if embedded.Kind() != reflect.Struct {
			return nil, false
		}
		holder = embedded
	}
	return field.Type, true
}

// printing are the methods that decide how fmt, and so an escaper,
// prints a value, in the order Describe lists them.
var printing = []string{"Error", "Format", "GoString", "String"}

// Describe is goType as a fact records it: its name (typeName); for a
// defined type, what it is defined as, which its name does not say; and
// which of the methods that decide how it prints it has. The generator
// writes the same description from go/types.
func Describe(goType reflect.Type) string {
	var description strings.Builder
	description.WriteString(typeName(goType))
	if goType.Name() != "" && goType.PkgPath() != "" {
		description.WriteString(" " + underlying(goType))
	}
	for _, name := range printing {
		if _, ok := goType.MethodByName(name); ok {
			description.WriteString(" " + name)
		}
	}
	return description.String()
}

// typeName is goType as Go writes a type, with each defined type named
// by its import path ("main" for a main package's) rather than its
// package name, as reflect's String names it. Two packages may share a name: an
// app's own package template can declare an HTML type, which a field
// can trade for html/template's without its reflect string changing,
// though an escaper treats the two differently. A struct or interface
// literal with members is written as reflect writes it, which the
// generator never writes, so a fact that met one fails.
func typeName(goType reflect.Type) string {
	switch {
	case goType.Kind() == reflect.UnsafePointer:
		return "unsafe.Pointer"
	case goType.Name() != "" && goType.PkgPath() == "":
		return goType.Name() // predeclared, error included
	case goType.Name() != "":
		return goType.PkgPath() + "." + goType.Name()
	}
	switch goType.Kind() {
	case reflect.Interface:
		if goType.NumMethod() == 0 {
			return "interface {}"
		}
	case reflect.Struct:
		if goType.NumField() == 0 {
			return "struct {}"
		}
	default:
		return underlying(goType)
	}
	return goType.String()
}

// underlying is what the defined type goType is defined as, written as
// Go writes a type literal, except a struct, whose fields facts of their
// own describe, and an interface, which only emptiness distinguishes for
// the code.
func underlying(goType reflect.Type) string {
	switch goType.Kind() {
	case reflect.Struct:
		return "struct"
	case reflect.Interface:
		if goType.NumMethod() == 0 {
			return "interface {}"
		}
		return "interface"
	case reflect.Slice:
		return "[]" + typeName(goType.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(goType.Len()) + "]" + typeName(goType.Elem())
	case reflect.Map:
		return "map[" + typeName(goType.Key()) + "]" + typeName(goType.Elem())
	case reflect.Pointer:
		return "*" + typeName(goType.Elem())
	case reflect.Chan:
		return goType.ChanDir().String() + " " + typeName(goType.Elem())
	case reflect.Func:
		return funcString(goType, 0)
	}
	return goType.Kind().String()
}

// funcString is the function type function as Go writes an unnamed one,
// with TypeName's names, without its first skip parameters: 1 leaves out
// the receiver of a method found on a type.
func funcString(function reflect.Type, skip int) string {
	var written strings.Builder
	written.WriteString("func(")
	for index := skip; index < function.NumIn(); index++ {
		if index > skip {
			written.WriteString(", ")
		}
		if function.IsVariadic() && index == function.NumIn()-1 {
			written.WriteString("..." + typeName(function.In(index).Elem()))
		} else {
			written.WriteString(typeName(function.In(index)))
		}
	}
	written.WriteString(")")
	switch function.NumOut() {
	case 0:
	case 1:
		written.WriteString(" " + typeName(function.Out(0)))
	default:
		written.WriteString(" (")
		for index := range function.NumOut() {
			if index > 0 {
				written.WriteString(", ")
			}
			written.WriteString(typeName(function.Out(index)))
		}
		written.WriteString(")")
	}
	return written.String()
}
