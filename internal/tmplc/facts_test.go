package tmplc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"html/template"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// factTypes is the source of the types below, which the test
// type-checks: the generator describes them from go/types, and the
// runtime from reflect, and the two must agree.
const factTypes = `package tmplc

type named int

type str string

func (s str) String() string { return string(s) }

type errT struct{ Msg string }

func (e errT) Error() string { return e.Msg }

type ptrStr struct{ A int }

func (*ptrStr) String() string { return "" }

func (p ptrStr) Pair(a int, rest ...string) (str, error) { return "", nil }

type list []named

type grid [3]str

type dict map[str]*named

type fnT func(int, ...string) (bool, error)

type recv <-chan int

type iface interface{ M() }

type empty interface{}

type base struct{ ID named }

func (base) Label() str { return "" }

type holder struct {
	base
	A any
	B []byte
	C map[string][]rune
	D func() named
	E chan<- str
	F struct{}
	G *[2]named
	H error
	I uintptr
	J complex64
	K iface
	L list
	M *ptrStr
}

func (h *holder) Ptr() *base { return nil }
`

type named int

type str string

func (text str) String() string { return string(text) }

type errT struct{ Msg string }

func (err errT) Error() string { return err.Msg }

type ptrStr struct{ A int }

func (*ptrStr) String() string { return "" }

func (text ptrStr) Pair(
	count int,
	rest ...string,
) (str, error) {
	return "", nil
}

type list []named

type grid [3]str

type dict map[str]*named

type fnT func(int, ...string) (bool, error)

type recv <-chan int

type iface interface{ M() }

type empty interface{}

type base struct{ ID named }

func (base) Label() str { return "" }

type holder struct {
	base
	A any
	B []byte
	C map[string][]rune
	D func() named
	E chan<- str
	F struct{}
	G *[2]named
	H error
	I uintptr
	J complex64
	K iface
	L list
	M *ptrStr
}

func (holder *holder) Ptr() *base { return nil }

var factReflect = map[string]reflect.Type{
	"named":  reflect.TypeFor[named](),
	"str":    reflect.TypeFor[str](),
	"errT":   reflect.TypeFor[errT](),
	"ptrStr": reflect.TypeFor[ptrStr](),
	"list":   reflect.TypeFor[list](),
	"grid":   reflect.TypeFor[grid](),
	"dict":   reflect.TypeFor[dict](),
	"fnT":    reflect.TypeFor[fnT](),
	"recv":   reflect.TypeFor[recv](),
	"iface":  reflect.TypeFor[iface](),
	"empty":  reflect.TypeFor[empty](),
	"base":   reflect.TypeFor[base](),
	"holder": reflect.TypeFor[holder](),
}

// TestFactsMatchReflect: every fact the generator writes of a type is
// what tmplrt finds with reflect in the same program. For each type, as
// a value, as an addressable value and as a pointer, the description
// must match, and every field and method typed code can read on it must
// be found where the step says, with the type the generator gave it.
// A mismatch would leave typed code disabled where nothing changed.
func TestFactsMatchReflect(t *testing.T) {
	fset := token.NewFileSet()
	src, err := parser.ParseFile(fset, "types.go", factTypes, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := new(
		types.Config,
	).Check(reflect.TypeFor[named]().PkgPath(), fset, []*ast.File{src}, nil)
	if err != nil {
		t.Fatal(err)
	}
	generated := &file{in: Input{Types: pkg}}
	for name, rt := range factReflect {
		gt := pkg.Scope().Lookup(name).Type()
		for _, testCase := range []struct {
			val
			rt reflect.Type
		}{
			{val{t: gt, path: &tpath{}}, rt},
			{val{t: gt, addr: true, path: &tpath{}}, rt},
			{
				val{t: types.NewPointer(gt), path: &tpath{}},
				reflect.PointerTo(rt),
			},
		} {
			facts := tmplrt.Facts{Dot: testCase.rt}
			add := func(path *tpath, typ types.Type) {
				if desc, ok := describe(typ); ok {
					facts.Facts = append(
						facts.Facts,
						tmplrt.Fact{Path: path.steps, Type: desc},
					)
				}
			}
			add(testCase.path, testCase.t)
			if desc, _ := describe(testCase.t); desc != tmplrt.Describe(
				testCase.rt,
			) {
				t.Errorf(
					"%s: the generator describes it as %q, reflect as %q",
					testCase.rt,
					desc,
					tmplrt.Describe(testCase.rt),
				)
			}
			for _, member := range readable(testCase.t) {
				sel, ok, _ := generated.selectName(testCase.val, member)
				switch {
				case !ok:
				case sel.method != nil:
					if goodResults(sel.method) {
						add(
							testCase.path.to(
								memberStep(testCase.val, member, sel.method),
							),
							sel.method.Results().At(0).Type(),
						)
					}
				default:
					add(
						testCase.path.to(memberStep(testCase.val, member, nil)),
						sel.field,
					)
				}
			}
			if len(facts.Facts) < 2 && name == "holder" {
				t.Fatalf("%s: only %v", testCase.rt, facts.Facts)
			}
			if err := tmplrt.Holds(&facts); err != nil {
				t.Errorf(
					"%s (addressable %t): %v",
					testCase.rt,
					testCase.addr,
					err,
				)
			}
		}
	}
}

// memberStep is member, under a name that does not clash with the
// loop variable.
var memberStep = member

// readable is every field and method name of typ's struct, or of what it
// points to, and of its method sets.
func readable(typ types.Type) []string {
	var names []string
	under := typ
	if pointer, ok := typ.Underlying().(*types.Pointer); ok {
		under = pointer.Elem()
	}
	if structure, ok := under.Underlying().(*types.Struct); ok {
		for index := range structure.NumFields() {
			names = append(names, structure.Field(index).Name())
		}
		names = append(names, "ID") // promoted from base
	}
	for _, mt := range []types.Type{under, types.NewPointer(under)} {
		ms := types.NewMethodSet(mt)
		for index := range ms.Len() {
			names = append(names, ms.At(index).Obj().Name())
		}
	}
	return names
}

// HTML shares its name and its package's name, as the fact names
// neither, with html/template's HTML, which an escaper writes as it is.
type HTML string

// TestFactsCatchChanges: a fact the generator wrote fails against a type
// that differs where the code depends on it. Each fact is what the
// generator writes but for the one change.
func TestFactsCatchChanges(t *testing.T) {
	pkg := reflect.TypeFor[named]().PkgPath()
	for _, testCase := range []struct {
		name string
		dot  reflect.Type
		fact tmplrt.Fact
	}{
		{
			"an int64 field made unsigned",
			reflect.TypeFor[struct{ N uint64 }](),
			tmplrt.Fact{Path: []string{".N"}, Type: "int64"},
		},
		{
			"a named string given String",
			reflect.TypeFor[str](),
			tmplrt.Fact{Type: pkg + ".str string"},
		},
		{
			"a value method made a pointer method",
			reflect.TypeFor[holder](),
			tmplrt.Fact{
				Path: []string{".Ptr() func() *" + pkg + ".base"},
				Type: "*" + pkg + ".base",
			},
		},
		{
			"a field read where a method is found first",
			reflect.TypeFor[holder](),
			tmplrt.Fact{
				Path: []string{".Label"},
				Type: pkg + ".str string String",
			},
		},
		{
			"a field through an embedded pointer",
			reflect.TypeFor[struct{ *base }](),
			tmplrt.Fact{Path: []string{".ID"}, Type: pkg + ".named int"},
		},
		{
			"a method of another signature",
			reflect.TypeFor[ptrStr](),
			tmplrt.Fact{
				Path: []string{
					".Pair() func(int, string) (" + pkg + ".str, error)",
				},
				Type: pkg + ".str string String",
			},
		},
		{
			"an element of another type",
			reflect.TypeFor[list](),
			tmplrt.Fact{
				Path: []string{"[]"},
				Type: pkg + ".str string String",
			},
		},
		{
			"html/template's HTML for one of the same names",
			reflect.TypeFor[struct{ B template.HTML }](),
			tmplrt.Fact{Path: []string{".B"}, Type: pkg + ".HTML string"},
		},
		{
			"a package's own HTML for html/template's",
			reflect.TypeFor[struct{ B HTML }](),
			tmplrt.Fact{
				Path: []string{".B"},
				Type: "html/template.HTML string",
			},
		},
	} {
		err := tmplrt.Holds(
			&tmplrt.Facts{
				Dot:   testCase.dot,
				Facts: []tmplrt.Fact{testCase.fact},
			},
		)
		if err == nil ||
			!strings.Contains(err.Error(), "run rastrillo generate") {
			t.Errorf(
				"%s: Holds = %v, want the change found",
				testCase.name,
				err,
			)
		}
	}
}
