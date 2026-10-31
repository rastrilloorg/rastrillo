package tmplc

import (
	"fmt"
	"go/types"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

const (
	tmplrtPath = "amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
	// prefixLen is how much of a fingerprint or content hash names a
	// function: enough that two never share one in a file, which source
	// checks rather than assumes.
	prefixLen = 16
)

// escapers maps each name escaping gives an escaper to the name of
// tmplrt's port. It is read from tmplrt.Escapers, so the two cannot
// disagree.
var escapers = func() map[string]string {
	byName := make(map[string]string, len(tmplrt.Escapers))
	for name, fn := range tmplrt.Escapers {
		full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
		byName[name] = strings.TrimPrefix(full, tmplrtPath+".")
	}
	return byName
}()

// file collects what the generated file holds.
type file struct {
	in Input
	// binds maps each registered fingerprint to its entry, the function
	// that renders it and the FuncMap functions it binds.
	binds map[string]bind
	// bodies maps a render function's name to its source. Two entries
	// whose templates are the same, as across clones, share one.
	bodies map[string]emitted
	// fns holds how each FuncMap function is called, by name and type.
	fns map[string]*fn
	// imports maps each package name the file uses, or has kept for a
	// function's type, to its import path.
	imports map[string]string
	// fields maps each funcs struct field to the fns key that has it.
	fields map[string]string
	// The names the file imports bytes, reflect and tmplrt under, and
	// gives its funcs struct, chosen so as not to clash with the
	// package's own.
	bytes, reflect, rt, funcs string

	// pkgs are the packages the owner package imports, directly or not,
	// by path; ownTypes are its candidate types, in order of name; and
	// failed holds the typed variants that could not read their dot
	// directly. Each is filled when first needed.
	pkgs     map[string]*types.Package
	ownTypes []types.Type
	failed   map[string]bool
	// typed is whether the file may hold typed values at all.
	typed bool
}

type (
	bind struct {
		entry, body string
		fns         []*fn // sorted by field
		// bodies are the functions of every template the entry reaches,
		// which read the data by reflection.
		bodies []string
		// typed are the entry's typed variants, in the order the bind
		// tries them.
		typed []typedCase
	}
	// typedCase is a typed variant: the Go type the data must have, and
	// the function that renders it.
	typedCase struct{ typ, fn string }
	// emitted is a function's doc comment, after its name, and its code
	// after that, with what it assumes of the types it reads.
	emitted struct {
		doc, code string
		assumed
	}
	// assumed is what a function's code assumes of the types it reads,
	// which its bind checks with tmplrt.Holds: the facts, and the
	// FuncMap functions they start from, by funcs field, with their Go
	// types; dot, the function's dot's type, which the rest start from,
	// "" for a dot read by reflection; and calls, the typed functions it
	// calls, whose facts the bind checks too.
	assumed struct {
		facts []fact
		funcs map[string]string
		dot   string
		calls []string
	}
	// fact is a tmplrt.Fact.
	fact struct {
		path *tpath
		desc string
	}
)

func newFile(in Input) *file {
	generated := &file{
		in:      in,
		binds:   map[string]bind{},
		bodies:  map[string]emitted{},
		fns:     map[string]*fn{},
		fields:  map[string]string{},
		failed:  map[string]bool{},
		typed:   typedOK(in.Types),
		bytes:   freeName(in.Types, "bytes"),
		reflect: freeName(in.Types, "reflect"),
		rt:      freeName(in.Types, "tmplrt"),
		funcs:   freeName(in.Types, "templateFuncs"),
	}
	generated.imports = map[string]string{
		generated.bytes:   "bytes",
		generated.reflect: "reflect",
		generated.rt:      tmplrtPath,
	}
	return generated
}

// freeName is base, or base with a number after it, whichever the
// package does not declare: an import's name shares the package's
// scope.
func freeName(pkg *types.Package, base string) string {
	name := base
	for suffix := 2; pkg != nil && pkg.Scope().Lookup(name) != nil; suffix++ {
		name = base + strconv.Itoa(suffix)
	}
	return name
}

// add registers the code lowerer lowered for entry under fingerprint fp: a
// function for each template it reaches, which reads the data by
// reflection, and typed variants for the entry's candidate types.
func (generated *file) add(fp, entry string, lowerer *lowerer) error {
	fns := map[string]*fn{}
	var bodies []string
	for _, name := range slices.Sorted(maps.Keys(lowerer.bodies)) {
		body := lowerer.bodies[name]
		maps.Copy(fns, body.funcs)
		rendered := generated.render(
			name,
			body,
			lowerer,
			dynamic("dot"),
			generated.reflect+".Value",
			nil,
		)
		function := emitted{
			doc:     fmt.Sprintf("renders %q.", baseName(name)),
			code:    rendered.code,
			assumed: rendered.assumed,
		}
		if err := generated.put(body.fn, function); err != nil {
			return err
		}
		bodies = append(bodies, body.fn)
	}
	bd := bind{
		bodies: bodies,
		entry:  entry,
		body:   lowerer.bodies[entry].fn,
		fns: slices.SortedFunc(maps.Values(fns), func(first, second *fn) int {
			return strings.Compare(first.field, second.field)
		}),
	}
	typed, err := generated.typedEntry(entry, lowerer)
	if err != nil {
		return err
	}
	bd.typed = typed
	generated.binds[fp] = bd
	return nil
}

// put adds a function to the file, under a name that stands for its
// code, which decides what it assumes.
func (generated *file) put(name string, function emitted) error {
	if prior, taken := generated.bodies[name]; taken &&
		prior.code != function.code {
		return fmt.Errorf("tmplc: two templates' code hashes to %s", name)
	}
	generated.bodies[name] = function
	return nil
}

// render writes the function for the template called name, lowered as
// body, with dot held as dot, whose Go type is param. run is the typed run
// the function belongs to, or nil for one that reads everything by
// reflection. It returns the function's code after its name, and the
// fields and methods typed code could not find, and what the code
// assumes of the types it reads.
func (generated *file) render(
	name string,
	body *body,
	lowerer *lowerer,
	dot val,
	param string,
	run *typedRun,
) rendered {
	emitter := &writer{
		file:     generated,
		name:     baseName(name),
		bodies:   lowerer.bodies,
		missing:  lowerer.missing,
		dot:      dot,
		vars:     map[string]val{},
		assigned: assignedVars(body.code),
		run:      run,
	}
	if dot.t != nil {
		dot.path = &tpath{}
		emitter.dot = emitter.typed(dot)
		if emitter.dot.t == nil {
			emitter.missed = append(emitter.missed, "its dot's type")
		}
	}
	root := val{code: "root", t: dot.t, addr: dot.addr, path: dot.path}
	if emitter.assigned["root"] {
		root = dynamic("root")
	}
	emitter.vars["root"] = root
	emitter.block(body.code)
	code := fmt.Sprintf(
		"(b *%s.Buffer, dot %s, fn *%s, depth int) error {\n"+
			"%s%s\treturn nil\n}\n",
		generated.bytes,
		param,
		generated.funcs,
		emitter.rootDecl(root, dot),
		emitter.String(),
	)
	return rendered{
		code:    code,
		missed:  emitter.missed,
		assumed: emitter.assumed,
	}
}

// rootDecl declares root, the variable for $, whose value is dot. $ is
// the dot the template started with, declared only if the code written
// mentions it: a $ the template only looked up, for a command that
// fails as written, or declared afresh with {{$ := ...}}, leaves it
// out, and Go refuses an unused variable.
func (emitter *writer) rootDecl(root, dot val) string {
	var decl string
	if emitter.rootRead || emitter.rootWritten {
		init := "dot"
		if root.t == nil {
			init = emitter.dyn(dot)
		}
		decl = "\troot := " + init + "\n"
	}
	if emitter.rootWritten && !emitter.rootRead {
		decl += "\t_ = root\n"
	}
	return decl
}

// rendered is what render wrote.
type rendered struct {
	code   string
	missed []string
	assumed
}

// typedOK reports whether typed code can be written in the package:
// it uses predeclared names, which the package must not declare itself.
// Where it does, nothing is typed, and the code is all reflection.
func typedOK(pkg *types.Package) bool {
	if pkg == nil {
		return false
	}
	for _, name := range []string{
		"bool", "complex", "complex128", "false", "float64", "int",
		"int64", "len", "nil", "string", "true", "uint64",
	} {
		if pkg.Scope().Lookup(name) != nil {
			return false
		}
	}
	return true
}
