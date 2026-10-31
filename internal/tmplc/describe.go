package tmplc

import (
	"go/types"
	"strconv"
	"strings"
)

// describe is typ as tmplrt.Describe describes it at run time: its name
// (factType); for a defined type, what it is defined as; and which of
// the methods that decide how fmt prints it the type has. ok is false for
// a type tmplrt could not name the same: a generic one, one declared
// inside a function, which reflect does not tell from one declared
// outside, and a struct or interface literal other than an empty one.
func describe(typ types.Type) (string, bool) {
	text, ok := factType(typ)
	if !ok {
		return "", false
	}
	typ = types.Unalias(typ)
	if named, isNamed := typ.(*types.Named); isNamed &&
		named.Obj().Pkg() != nil {
		var underlying string
		switch under := named.Underlying().(type) {
		case *types.Basic:
			underlying, ok = factType(under)
		case *types.Struct:
			underlying = "struct"
		case *types.Interface:
			underlying = "interface"
			if under.Empty() {
				underlying = "interface {}"
			}
		default:
			underlying, ok = factType(under)
		}
		if !ok {
			return "", false
		}
		text += " " + underlying
	}
	ms := types.NewMethodSet(typ)
	for _, name := range []string{"Error", "Format", "GoString", "String"} {
		if ms.Lookup(nil, name) != nil {
			text += " " + name
		}
	}
	return text, true
}

// basicNames are reflect's names for the basic kinds, which tmplrt uses
// too: byte is uint8 and rune is int32.
var basicNames = map[types.BasicKind]string{
	types.Bool:          "bool",
	types.String:        "string",
	types.UnsafePointer: "unsafe.Pointer",
	types.Int:           "int",
	types.Int8:          "int8",
	types.Int16:         "int16",
	types.Int32:         "int32",
	types.Int64:         "int64",
	types.Uint:          "uint",
	types.Uint8:         "uint8",
	types.Uint16:        "uint16",
	types.Uint32:        "uint32",
	types.Uint64:        "uint64",
	types.Uintptr:       "uintptr",
	types.Float32:       "float32",
	types.Float64:       "float64",
	types.Complex64:     "complex64",
	types.Complex128:    "complex128",
}

// factType is typ as tmplrt names a type in a fact, if it can (see
// describe): as Go writes it, but with each defined type named by its
// import path, not its package name, which two packages can share. A
// main package's types are "main"'s at run time, whatever its import
// path is.
func factType(typ types.Type) (string, bool) {
	switch resolved := types.Unalias(typ).(type) {
	case *types.Basic:
		name, ok := basicNames[resolved.Kind()]
		return name, ok
	case *types.Named:
		return namedFactType(resolved)
	case *types.Pointer:
		return elemFactType("*", resolved.Elem())
	case *types.Slice:
		return elemFactType("[]", resolved.Elem())
	case *types.Array:
		length := strconv.FormatInt(resolved.Len(), 10)
		return elemFactType("["+length+"]", resolved.Elem())
	case *types.Map:
		key, ok := factType(resolved.Key())
		if !ok {
			return "", false
		}
		return elemFactType("map["+key+"]", resolved.Elem())
	case *types.Chan:
		return chanFactType(resolved)
	case *types.Signature:
		return sigString(resolved)
	case *types.Interface:
		return "interface {}", resolved.Empty()
	case *types.Struct:
		return "struct {}", resolved.NumFields() == 0
	}
	return "", false
}

// elemFactType is the fact type of a composite type: prefix, then the
// fact type of its element.
func elemFactType(prefix string, element types.Type) (string, bool) {
	text, ok := factType(element)
	return prefix + text, ok
}

// namedFactType is the fact type of a defined type, which must be
// declared at its package's top level and not be generic.
func namedFactType(named *types.Named) (string, bool) {
	obj := named.Obj()
	switch {
	case named.TypeArgs().Len() > 0 || named.TypeParams().Len() > 0:
		return "", false
	case obj.Pkg() == nil: // error
		return obj.Name(), true
	case obj.Parent() != obj.Pkg().Scope():
		return "", false
	}
	path := obj.Pkg().Path()
	if obj.Pkg().Name() == "main" {
		path = "main"
	}
	return path + "." + obj.Name(), true
}

// chanFactType is the fact type of a channel type.
func chanFactType(channel *types.Chan) (string, bool) {
	if _, nested := types.Unalias(channel.Elem()).(*types.Chan); nested {
		// Which arrow binds to which chan would need parentheses.
		return "", false
	}
	switch channel.Dir() {
	case types.SendOnly:
		return elemFactType("chan<- ", channel.Elem())
	case types.RecvOnly:
		return elemFactType("<-chan ", channel.Elem())
	}
	return elemFactType("chan ", channel.Elem())
}

// sigString is sig as factType writes a function type, leaving out a
// method's receiver.
func sigString(sig *types.Signature) (string, bool) {
	if sig.TypeParams().Len() > 0 {
		return "", false
	}
	tuple := func(tup *types.Tuple, variadic bool) ([]string, bool) {
		out := make([]string, tup.Len())
		for index := range tup.Len() {
			typ := tup.At(index).Type()
			prefix := ""
			if variadic && index == tup.Len()-1 {
				typ, prefix = typ.(*types.Slice).Elem(), "..."
			}
			text, ok := factType(typ)
			if !ok {
				return nil, false
			}
			out[index] = prefix + text
		}
		return out, true
	}
	params, ok := tuple(sig.Params(), sig.Variadic())
	if !ok {
		return "", false
	}
	results, ok := tuple(sig.Results(), false)
	if !ok {
		return "", false
	}
	text := "func(" + strings.Join(params, ", ") + ")"
	switch len(results) {
	case 0:
	case 1:
		text += " " + results[0]
	default:
		text += " (" + strings.Join(results, ", ") + ")"
	}
	return text, true
}
