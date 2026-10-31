package tmplc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
)

// selection is what .name finds on a typed value, as text/template's
// evalField looks: a method in its method set, or else a field.
type selection struct {
	method *types.Signature // nil for a field
	field  types.Type
	// nilable is whether the receiver is a pointer the code must check,
	// as text/template does before it reads a field through one.
	nilable bool
}

// selectName looks name up on value. ok is false when the code must look it
// up at run time instead, and miss is whether that is because the type
// has no such field or method to find, as opposed to a value, such as an
// interface or a map, that only the data can answer for. A miss fails at
// run time in text/template, and is why a type is not a candidate.
func (generated *file) selectName(
	value val,
	name string,
) (sel selection, ok, miss bool) {
	typ := value.t
	if isInterface(typ) || isReflectValue(typ) {
		return sel, false, false
	}
	if _, isMap := typ.Underlying().(*types.Map); isMap {
		return sel, false, false
	}
	if !token.IsExported(name) {
		return sel, false, true
	}
	recv, addr := typ, value.addr
	if pointer, isPtr := typ.Underlying().(*types.Pointer); isPtr {
		if _, named := types.Unalias(typ).(*types.Named); named {
			return sel, false, true
		}
		recv, addr, sel.nilable = pointer.Elem(), true, true
		switch recv.Underlying().(type) {
		case *types.Pointer, *types.Interface:
			// text/template goes on indirecting; nothing the code needs
			// to be fast for.
			return sel, false, true
		}
	}
	return generated.lookupMember(recv, addr, name, sel)
}

// lookupMember finds name on recv, through a pointer to it if addr: a
// method in its method set, or a field of a struct. sel is what
// selectName has found so far.
func (generated *file) lookupMember(
	recv types.Type,
	addr bool,
	name string,
	sel selection,
) (found selection, ok, miss bool) {
	ms := types.NewMethodSet(recv)
	if addr {
		ms = types.NewMethodSet(types.NewPointer(recv))
	}
	if method := ms.Lookup(nil, name); method != nil {
		sel.method = method.Obj().Type().(*types.Signature)
		return sel, true, false
	}
	if _, isStruct := recv.Underlying().(*types.Struct); !isStruct {
		return sel, false, true
	}
	// A pointer method not in the method set is not found, nor is a
	// field reached through an embedded pointer, which could be nil.
	obj, _, indirect := types.LookupFieldOrMethod(
		recv,
		false,
		generated.in.Types,
		name,
	)
	field, isVar := obj.(*types.Var)
	if !isVar || !field.IsField() || indirect {
		return sel, false, true
	}
	sel.field = field.Type()
	return sel, true, false
}

// goodResults reports whether text/template can call a function or
// method with sig's results: one, or a value and an error.
func goodResults(sig *types.Signature) bool {
	results := sig.Results()
	return results.Len() == 1 ||
		results.Len() == 2 &&
			types.Identical(
				results.At(1).Type(),
				types.Universe.Lookup("error").Type(),
			)
}

// spell is typ as Go source in the generated file, or ok false when the
// file cannot write it, or a name it uses could be shadowed there: the
// package's own types and the predeclared ones, and pointers, slices,
// arrays, maps and channels of those. An alias is written as the type it
// stands for, which is what the file can name: an alias another package
// declares is not in scope there, even for a type that is.
func (generated *file) spell(typ types.Type) (string, bool) {
	switch resolved := types.Unalias(typ).(type) {
	case *types.Basic:
		if resolved.Kind() == types.UnsafePointer ||
			resolved.Info()&types.IsUntyped != 0 ||
			generated.declared(resolved.Name()) {
			return "", false
		}
		return resolved.Name(), true
	case *types.Named:
		return generated.spellNamed(resolved)
	case *types.Pointer:
		elem, ok := generated.spell(resolved.Elem())
		return "*" + elem, ok
	case *types.Slice:
		elem, ok := generated.spell(resolved.Elem())
		return "[]" + elem, ok
	case *types.Array:
		elem, ok := generated.spell(resolved.Elem())
		return "[" + strconv.FormatInt(resolved.Len(), 10) + "]" + elem, ok
	case *types.Map:
		key, okK := generated.spell(resolved.Key())
		value, okV := generated.spell(resolved.Elem())
		return "map[" + key + "]" + value, okK && okV
	case *types.Chan:
		return generated.spellChan(resolved)
	}
	return "", false
}

// spellNamed is a defined type as the generated file names it: one of
// the package's own, at its top level, or error.
func (generated *file) spellNamed(named *types.Named) (string, bool) {
	obj := named.Obj()
	own := generated.in.Types
	switch {
	case named.TypeArgs().Len() > 0 || named.TypeParams().Len() > 0:
		return "", false
	case obj.Pkg() == nil: // error
		return obj.Name(), !generated.declared(obj.Name())
	case own == nil || obj.Pkg() != own || obj.Parent() != own.Scope() ||
		localName.MatchString(obj.Name()):
		return "", false
	}
	return obj.Name(), true
}

// spellChan is a channel type as the generated file writes it.
func (generated *file) spellChan(channel *types.Chan) (string, bool) {
	if _, nested := types.Unalias(channel.Elem()).(*types.Chan); nested {
		// Which arrow binds to which chan would need parentheses.
		return "", false
	}
	elem, ok := generated.spell(channel.Elem())
	switch channel.Dir() {
	case types.SendOnly:
		return "chan<- " + elem, ok
	case types.RecvOnly:
		return "<-chan " + elem, ok
	}
	return "chan " + elem, ok
}

// typeName is typ as text/template's errors write a type, qualified by
// package name.
func typeName(typ types.Type) string {
	return types.TypeString(
		typ,
		func(pkg *types.Package) string { return pkg.Name() },
	)
}

// pkg is the package at path, as the owner package sees it among its
// imports, or nil.
func (generated *file) pkg(path string) *types.Package {
	if generated.in.Types == nil {
		return nil
	}
	if generated.pkgs == nil {
		generated.pkgs = map[string]*types.Package{}
		queue := []*types.Package{generated.in.Types}
		for len(queue) > 0 {
			path := queue[0]
			queue = queue[1:]
			if _, seen := generated.pkgs[path.Path()]; seen {
				continue
			}
			generated.pkgs[path.Path()] = path
			queue = append(queue, path.Imports()...)
		}
	}
	return generated.pkgs[path]
}

// funcType is the go/types signature of a FuncMap function's type as
// capture wrote it, or nil when it cannot be resolved: the code then
// calls the function directly but holds its result as a reflect.Value.
func (generated *file) funcType(
	expr string,
	qualifiers map[string]string,
) *types.Signature {
	if !generated.typed {
		return nil
	}
	expression, err := parser.ParseExpr(expr)
	if err != nil {
		return nil
	}
	sig, _ := generated.resolve(expression, qualifiers).(*types.Signature)
	return sig
}

// resolve is the type the expression that stands for in the owner package,
// with its package names standing for qualifiers' import paths.
func (generated *file) resolve(
	expression ast.Expr,
	qualifiers map[string]string,
) types.Type {
	switch concrete := expression.(type) {
	case *ast.ParenExpr:
		return generated.resolve(concrete.X, qualifiers)
	case *ast.Ident:
		scope := generated.in.Types.Scope()
		if obj := scope.Lookup(concrete.Name); obj != nil {
			return typeOf(obj)
		}
		return typeOf(types.Universe.Lookup(concrete.Name))
	case *ast.SelectorExpr:
		return generated.resolveSelector(concrete, qualifiers)
	case *ast.StarExpr:
		if elem := generated.resolve(concrete.X, qualifiers); elem != nil {
			return types.NewPointer(elem)
		}
	case *ast.ArrayType:
		return generated.resolveArray(concrete, qualifiers)
	case *ast.MapType:
		key := generated.resolve(concrete.Key, qualifiers)
		value := generated.resolve(concrete.Value, qualifiers)
		if key != nil && value != nil {
			return types.NewMap(key, value)
		}
	case *ast.ChanType:
		return generated.resolveChan(concrete, qualifiers)
	case *ast.InterfaceType:
		if concrete.Methods == nil || len(concrete.Methods.List) == 0 {
			return types.NewInterfaceType(nil, nil).Complete()
		}
	case *ast.FuncType:
		return generated.resolveFunc(concrete, qualifiers)
	}
	return nil
}

// resolveSelector is the type a package-qualified name stands for.
func (generated *file) resolveSelector(
	selector *ast.SelectorExpr,
	qualifiers map[string]string,
) types.Type {
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return nil
	}
	pkg := generated.pkg(qualifiers[ident.Name])
	if pkg == nil {
		return nil
	}
	return typeOf(pkg.Scope().Lookup(selector.Sel.Name))
}

// typeOf is the type obj names, or nil when obj is not a type.
func typeOf(obj types.Object) types.Type {
	if tn, ok := obj.(*types.TypeName); ok {
		return tn.Type()
	}
	return nil
}

func (generated *file) resolveArray(
	array *ast.ArrayType,
	qualifiers map[string]string,
) types.Type {
	elem := generated.resolve(array.Elt, qualifiers)
	if elem == nil {
		return nil
	}
	if array.Len == nil {
		return types.NewSlice(elem)
	}
	lit, ok := array.Len.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return nil
	}
	length, err := strconv.ParseInt(lit.Value, 0, 64)
	if err != nil {
		return nil
	}
	return types.NewArray(elem, length)
}

func (generated *file) resolveChan(
	channel *ast.ChanType,
	qualifiers map[string]string,
) types.Type {
	elem := generated.resolve(channel.Value, qualifiers)
	if elem == nil {
		return nil
	}
	dir := types.SendRecv
	switch channel.Dir {
	case ast.SEND:
		dir = types.SendOnly
	case ast.RECV:
		dir = types.RecvOnly
	}
	return types.NewChan(dir, elem)
}

func (generated *file) resolveFunc(
	function *ast.FuncType,
	qualifiers map[string]string,
) types.Type {
	params, variadic, ok := generated.resolveTuple(
		function.Params, qualifiers)
	if !ok {
		return nil
	}
	results, _, ok := generated.resolveTuple(function.Results, qualifiers)
	if !ok {
		return nil
	}
	return types.NewSignatureType(nil, nil, nil, params, results, variadic)
}

// resolveTuple is the parameters or results fields declares, and
// whether the last is variadic; ok is false when one's type cannot be
// resolved.
func (generated *file) resolveTuple(
	fields *ast.FieldList,
	qualifiers map[string]string,
) (tuple *types.Tuple, variadic, ok bool) {
	var vars []*types.Var
	if fields == nil {
		return types.NewTuple(), false, true
	}
	for _, field := range fields.List {
		te := field.Type
		if el, ok := te.(*ast.Ellipsis); ok {
			variadic = true
			te = &ast.ArrayType{Elt: el.Elt}
		}
		typ := generated.resolve(te, qualifiers)
		if typ == nil {
			return nil, false, false
		}
		for range max(len(field.Names), 1) {
			vars = append(vars, types.NewParam(token.NoPos, nil, "", typ))
		}
	}
	return types.NewTuple(vars...), variadic, true
}
