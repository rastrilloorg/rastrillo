package tmplc

import (
	"go/types"
	"maps"
)

func (emitter *writer) rangeStmt(statement rangeStmt) {
	over := emitter.eval(statement.over)
	if loop := emitter.loopKind(over, statement); loop != "" {
		emitter.typedRange(statement, over, loop)
		return
	}
	ranged := emitter.rangeVars(statement, emitter.dyn(over))
	ran := ""
	if len(statement.els) > 0 {
		ran = emitter.temp("ran")
		emitter.line("%s := false", ran)
	}
	fn := "Range"
	if statement.two {
		fn = "Range2"
	}
	key := "_"
	if len(statement.vars) == 2 {
		key = emitter.temp("key")
	}
	emitter.line(
		"if err := %s.%s(%s, func(%s, dot %s.Value) error {",
		emitter.file.rt,
		fn,
		ranged,
		key,
		emitter.file.reflect,
	)
	emitter.depth++
	emitter.rangeBody(statement, ran, key)
	emitter.depth--
	emitter.line("}); err != nil {")
	emitter.depth++
	emitter.line("%s", emitter.fail(statement.at, "err"))
	emitter.depth--
	emitter.line("}")
	emitter.rangeEnd(statement, ran)
}

// rangeEnd writes the range's {{else}}, run when ran is still false,
// and the label a {{break}} out of a range's {{else}} goes to.
func (emitter *writer) rangeEnd(statement rangeStmt, ran string) {
	if ran != "" {
		emitter.line("if !%s {", ran)
		emitter.nested(statement.els)
		emitter.line("}")
	}
	if statement.end != "" {
		emitter.line("%s:", statement.end)
	}
}

// rangeVars declares or assigns the range's variables, which hold the
// value ranged over until the first element, and in {{else}}, and
// returns what to range over: ranged, or the first variable holding it.
func (emitter *writer) rangeVars(statement rangeStmt, ranged string) string {
	if len(statement.vars) == 0 {
		return ranged
	}
	if statement.assign {
		emitter.line(
			"%s = %s",
			emitter.written(statement.vars),
			repeat(ranged, len(statement.vars)),
		)
	} else {
		emitter.declare(statement.vars, dynamic(ranged))
	}
	return emitter.read(statement.vars[0])
}

// rangeBody writes the body of a tmplrt.Range callback, which gets
// each key and element as reflect.Values; ran, if not "", records that
// it ran.
func (emitter *writer) rangeBody(statement rangeStmt, ran, key string) {
	if ran != "" {
		emitter.line("%s = true", ran)
	}
	switch len(statement.vars) {
	case 1:
		emitter.line("%s = dot", emitter.written(statement.vars))
	case 2:
		emitter.line("%s = %s, dot", emitter.written(statement.vars), key)
	}
	saved := emitter.dot
	emitter.dot = dynamic("dot")
	emitter.loops = append(emitter.loops, false)
	emitter.block(statement.body)
	emitter.loops = emitter.loops[:len(emitter.loops)-1]
	emitter.dot = saved
	if !terminates(statement.body) {
		emitter.line("return nil")
	}
}

// loopKind is how a range over over is written as a Go for loop: over a
// "slice", an "array", a "map" with string or integer keys, which it
// visits in text/template's order, or an "int". It is "" when the range
// is left to tmplrt.Range: for a value whose type only the data
// decides, for any other kind, and for variables the template also
// assigns, which are held as reflect.Values.
func (emitter *writer) loopKind(over val, loop rangeStmt) string {
	if over.t == nil || loop.assign {
		return ""
	}
	for _, name := range loop.vars {
		if emitter.assigned[name] {
			return ""
		}
	}
	switch underlying := over.t.Underlying().(type) {
	case *types.Slice:
		return "slice"
	case *types.Array:
		return "array"
	case *types.Map:
		key, ok := underlying.Key().Underlying().(*types.Basic)
		if ok && key.Info()&(types.IsString|types.IsInteger) != 0 {
			return "map"
		}
	case *types.Basic:
		// An integer gives only one value, and two variables are an
		// error, which tmplrt.Range2 reports.
		if underlying.Info()&types.IsInteger != 0 && !loop.two {
			return "int"
		}
	}
	return ""
}

// typedRange writes a range as a Go for loop. Elements are what
// text/template's walkRange hands its body: a slice's are addressable,
// as are an array's when the array is; a map's are not.
func (emitter *writer) typedRange(statement rangeStmt, over val, loop string) {
	emitter.declare(statement.vars, over)
	shape := emitter.shape(over, loop)
	if len(statement.els) > 0 {
		emitter.line("if %s {", shape.empty(over))
		emitter.nested(statement.els)
		emitter.line("} else {")
		emitter.depth++
	}
	elem, key := emitter.loopHead(over, shape)
	emitter.typedBody(statement, emitter.typed(elem), emitter.typed(key))
	if len(statement.els) > 0 {
		emitter.depth--
		emitter.line("}")
	}
	if statement.end != "" {
		emitter.line("%s:", statement.end)
	}
}

// loopShape is how typedRange reaches what it ranges over: kind is
// loopKind's answer, ranged the code for the value, coll and length
// the code for the collection and its length, and reread whether those
// read through ranged's location on every iteration.
type loopShape struct {
	kind, ranged, coll, length string
	reread                     bool
}

func (emitter *writer) shape(over val, loop string) loopShape {
	// text/template reads an addressable slice's length, and each
	// element, through the slice's location on every iteration, so the
	// body may shorten or replace it; any other value is ranged over as
	// it was when the range began.
	reread := loop == "slice" && over.addr
	// An addressable slice or array is ranged over through a pointer to
	// it, and anything else as a copy.
	ranged := load(over)
	if over.addr && (loop == "slice" || loop == "array") {
		ranged = over.code
	}
	ranged = emitter.stable(ranged)
	shape := loopShape{
		kind:   loop,
		ranged: ranged,
		coll:   ranged,
		length: "len(" + ranged + ")",
		reread: reread,
	}
	if reread {
		shape.coll, shape.length = "(*"+ranged+")", "len(*"+ranged+")"
	}
	return shape
}

// empty is the condition under which a range over over runs {{else}}.
func (shape loopShape) empty(over val) string {
	if shape.kind != "int" {
		return shape.length + " == 0"
	}
	basic := over.t.Underlying().(*types.Basic)
	if basic.Info()&types.IsUnsigned != 0 {
		return shape.ranged + " == 0"
	}
	return shape.ranged + " <= 0"
}

// loopHead writes the for statement, and returns the element and key
// each iteration hands the body.
func (emitter *writer) loopHead(over val, shape loopShape) (elem, key val) {
	index := emitter.temp("v")
	ranged := shape.ranged
	switch shape.kind {
	case "slice":
		if shape.reread {
			emitter.line("for %s := 0; %s < %s; %s++ {",
				index, index, shape.length, index)
		} else {
			emitter.line("for %s := range %s {", index, ranged)
		}
		elem = val{
			code: "&" + shape.coll + "[" + index + "]",
			t:    over.t.Underlying().(*types.Slice).Elem(),
			addr: true,
			path: over.path.to("[]"),
		}
		key = val{code: index, t: types.Typ[types.Int]}
	case "array":
		emitter.line("for %s := range %s {", index, ranged)
		elem = val{
			code: ranged + "[" + index + "]",
			t:    over.t.Underlying().(*types.Array).Elem(),
			path: over.path.to("[]"),
		}
		if over.addr {
			elem.code, elem.addr = "&"+elem.code, true
		}
		key = val{code: index, t: types.Typ[types.Int]}
	case "map":
		return emitter.mapHead(over, index, ranged)
	case "int":
		emitter.line("for %s := range %s {", index, ranged)
		elem = val{code: index, t: over.t, path: over.path}
	}
	return elem, key
}

// mapHead writes the for statement over a map, in text/template's
// order of its keys.
func (emitter *writer) mapHead(over val, index, ranged string) (elem, key val) {
	mapType := over.t.Underlying().(*types.Map)
	emitter.line("for _, %s := range %s.SortedMap(%s) {",
		index, emitter.file.rt, ranged)
	elem = val{
		code: index + ".Value",
		t:    mapType.Elem(),
		path: over.path.to("[]"),
	}
	key = val{
		code: index + ".Key",
		t:    mapType.Key(),
		path: over.path.to("[key]"),
	}
	return elem, key
}

// typedBody writes the loop's body, with dot the element and the
// range's variables holding key and elem, and closes the loop.
func (emitter *writer) typedBody(statement rangeStmt, elem, key val) {
	emitter.depth++
	emitter.line("dot := %s", elem.code)
	emitter.line("_ = dot")
	savedDot, savedVars := emitter.dot, maps.Clone(emitter.vars)
	emitter.dot = val{code: "dot", t: elem.t, addr: elem.addr, path: elem.path}
	// Inside the loop the variables are new Go variables of the
	// element's type, shadowing those that hold what is ranged over.
	switch len(statement.vars) {
	case 1:
		emitter.declare(statement.vars, emitter.dot)
	case 2:
		emitter.declare(statement.vars[:1], key)
		emitter.declare(statement.vars[1:], emitter.dot)
	}
	emitter.loops = append(emitter.loops, true)
	emitter.block(statement.body)
	emitter.loops = emitter.loops[:len(emitter.loops)-1]
	emitter.dot, emitter.vars = savedDot, savedVars
	emitter.depth--
	emitter.line("}")
}
