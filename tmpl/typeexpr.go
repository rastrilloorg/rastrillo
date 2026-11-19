package tmpl

import (
	"cmp"
	"fmt"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// FuncSig describes the Go type of one function in a set's FuncMap, as
// the generated code has to spell it to call the function directly.
type FuncSig struct {
	// Expr is the type as Go source, such as "func(int) string". It is
	// empty when Nameable is false.
	Expr string
	// Imports are the import paths Expr needs, sorted.
	Imports []string
	// Qualifiers maps each package name Expr qualifies a type with to
	// the import path it stands for, which generated code imports under
	// that name.
	Qualifiers map[string]string `json:",omitempty"`
	// Refs are the named types Expr mentions, sorted. reflect cannot tell
	// a type declared inside a function from a package-level one of the
	// same name, so Nameable is provisional: rastrillo generate looks
	// each Ref up in its package's scope and treats the function as not
	// nameable when any is missing.
	Refs []TypeRef
	// Nameable is false when the generated code cannot write the type:
	// it mentions a type another package does not export, a package it
	// cannot import, an embedded alias, or two packages that share a
	// name. The compiler then calls the function by reflection.
	Nameable bool
}

// TypeRef is a defined type a FuncSig mentions.
type TypeRef struct {
	PkgPath, Name string
}

// typeExpr renders typ as Go source for code in the package owner, which
// is where rastrillo generate writes compiled code. Types of owner are
// written bare, so that an unexported one is nameable there and nowhere
// else.
func typeExpr(typ reflect.Type, owner string) FuncSig {
	writer := &typeWriter{
		owner:   owner,
		imports: map[string]string{},
		refs:    map[TypeRef]bool{},
		ok:      true,
	}
	expr := writer.write(typ)
	if !writer.ok {
		return FuncSig{}
	}
	var imports []string
	for _, path := range writer.imports {
		imports = append(imports, path)
	}
	slices.Sort(imports)
	refs := slices.SortedFunc(
		maps.Keys(writer.refs),
		func(left, right TypeRef) int {
			return cmp.Or(
				cmp.Compare(left.PkgPath, right.PkgPath),
				cmp.Compare(left.Name, right.Name),
			)
		},
	)
	var qualifiers map[string]string
	if len(writer.imports) > 0 {
		qualifiers = maps.Clone(writer.imports)
	}
	return FuncSig{
		Expr:       expr,
		Imports:    imports,
		Qualifiers: qualifiers,
		Refs:       refs,
		Nameable:   true,
	}
}

type typeWriter struct {
	owner   string
	imports map[string]string // package name → import path
	refs    map[TypeRef]bool
	ok      bool
}

func (writer *typeWriter) write(typ reflect.Type) string {
	if typ.Name() != "" {
		return writer.named(typ)
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return "*" + writer.write(typ.Elem())
	case reflect.Slice:
		return "[]" + writer.write(typ.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(typ.Len()) + "]" + writer.write(typ.Elem())
	case reflect.Map:
		return "map[" + writer.write(typ.Key()) + "]" + writer.write(typ.Elem())
	case reflect.Chan:
		return writer.channel(typ)
	case reflect.Func:
		return "func" + writer.signature(typ)
	case reflect.Interface:
		return writer.iface(typ)
	case reflect.Struct:
		return writer.structure(typ)
	}
	writer.ok = false
	return ""
}

// named writes a defined type: a predeclared one, or one that belongs
// to a package.
func (writer *typeWriter) named(typ reflect.Type) string {
	pkg := typ.PkgPath()
	if pkg == "" {
		return typ.Name() // int, string, error
	}
	// The name of an instantiated generic type has the type arguments'
	// full import paths in it.
	if strings.Contains(typ.Name(), "[") {
		writer.ok = false
		return ""
	}
	if pkg == writer.owner {
		writer.refs[TypeRef{pkg, typ.Name()}] = true
		return typ.Name()
	}
	if !token.IsExported(typ.Name()) || !importable(pkg, writer.owner) {
		writer.ok = false
		return ""
	}
	// reflect knows the package's name only through String, which
	// spells it "name.Type".
	qualifier := strings.TrimSuffix(typ.String(), "."+typ.Name())
	if prior, taken := writer.imports[qualifier]; taken && prior != pkg {
		writer.ok = false
		return ""
	}
	writer.imports[qualifier] = pkg
	writer.refs[TypeRef{pkg, typ.Name()}] = true
	return qualifier + "." + typ.Name()
}

// importable reports whether code in owner may import pkg: not a main
// package, and not an internal one owner is outside of.
func importable(pkg, owner string) bool {
	if pkg == "main" {
		return false
	}
	path := "/" + pkg + "/"
	index := strings.LastIndex(path, "/internal/")
	if index < 0 {
		return true
	}
	parent := path[1:index]
	return parent != "" &&
		(owner == parent || strings.HasPrefix(owner, parent+"/"))
}

// embeddedName is the field name Go gives an embedded field of type typ.
func embeddedName(typ reflect.Type) string {
	if typ.Kind() == reflect.Pointer && typ.Name() == "" {
		typ = typ.Elem()
	}
	return typ.Name()
}

func (writer *typeWriter) channel(typ reflect.Type) string {
	elem := writer.write(typ.Elem())
	switch typ.ChanDir() {
	case reflect.RecvDir:
		return "<-chan " + elem
	case reflect.SendDir:
		return "chan<- " + elem
	}
	// chan <-chan T would read as a receive-only channel of T.
	if typ.Elem().Kind() == reflect.Chan && typ.Elem().Name() == "" &&
		typ.Elem().ChanDir() == reflect.RecvDir {
		elem = "(" + elem + ")"
	}
	return "chan " + elem
}

// signature is a function type without its "func" keyword, which is also
// how an interface method is written.
func (writer *typeWriter) signature(typ reflect.Type) string {
	params := make([]string, typ.NumIn())
	for index := range params {
		if typ.IsVariadic() && index == len(params)-1 {
			params[index] = "..." + writer.write(typ.In(index).Elem())
		} else {
			params[index] = writer.write(typ.In(index))
		}
	}
	results := make([]string, typ.NumOut())
	for index := range results {
		results[index] = writer.write(typ.Out(index))
	}
	text := "(" + strings.Join(params, ", ") + ")"
	switch len(results) {
	case 0:
	case 1:
		text += " " + results[0]
	default:
		text += " (" + strings.Join(results, ", ") + ")"
	}
	return text
}

func (writer *typeWriter) iface(typ reflect.Type) string {
	if typ.NumMethod() == 0 {
		return "any"
	}
	methods := make([]string, typ.NumMethod())
	for index := range methods {
		method := typ.Method(index)
		if !method.IsExported() {
			writer.ok = false
			return ""
		}
		methods[index] = method.Name + writer.signature(method.Type)
	}
	return "interface{ " + strings.Join(methods, "; ") + " }"
}

func (writer *typeWriter) structure(typ reflect.Type) string {
	if typ.NumField() == 0 {
		return "struct{}"
	}
	fields := make([]string, typ.NumField())
	for index := range fields {
		field := typ.Field(index)
		if !field.IsExported() && field.PkgPath != writer.owner {
			writer.ok = false
			return ""
		}
		var builder strings.Builder
		if !field.Anonymous {
			builder.WriteString(field.Name + " ")
		}
		builder.WriteString(writer.write(field.Type))
		// An embedded field is named for its type, so the alias
		// "type Alias = time.Time" would come back as time.Time, a
		// field with another name.
		if field.Anonymous && field.Name != embeddedName(field.Type) {
			writer.ok = false
			return ""
		}
		if field.Tag != "" {
			builder.WriteString(" " + strconv.Quote(string(field.Tag)))
		}
		fields[index] = builder.String()
	}
	return fmt.Sprintf("struct{ %s }", strings.Join(fields, "; "))
}
