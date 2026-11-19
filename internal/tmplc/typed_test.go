package tmplc_test

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/tmplc"
	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// bakeoffTypes is the bake-off's view package, as far as its templates
// see it: the types the page draws and the methods it calls.
const bakeoffTypes = `package web

type filter string

func (f filter) path() string { return "/" + string(f) }

type view struct {
	Filter    filter
	Items     []item
	Total     int
	Left      int
	Completed int
	Editing   bool
}

func (v view) AllCompleted() bool { return v.Total > 0 && v.Left == 0 }

type item struct {
	ID        int64
	Title     string
	Completed bool
	Editing   bool
	EditHref  string
}

func (it item) Class() string { return "" }

// other has none of view's fields, so it is no candidate.
type other struct{ Name string }
`

// typedInput is the Input for a set parsed from files, owned by a
// package type-checked from src. A template's fingerprint is the hash of
// its name.
func typedInput(
	t *testing.T,
	src string,
	files map[string]string,
	funcs map[string]tmpl.FuncSig,
) tmplc.Input {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "web.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := new(types.Config).Check(pkgPath, fset, []*ast.File{parsed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	trees := map[string]*parse.Tree{}
	for name, text := range files {
		tree := parse.New(name)
		tree.Mode = parse.SkipFuncCheck
		if _, err := tree.Parse(text, "", "", trees); err != nil {
			t.Fatal(err)
		}
	}
	set := tmpl.CapturedSet{
		Name:         "app",
		Owner:        pkgPath,
		Templates:    map[string]string{},
		Fingerprints: map[string]string{},
		Funcs:        funcs,
		Option:       "default",
		Uncompilable: map[string]string{},
	}
	for name, tree := range trees {
		set.Templates[name] = tree.Root.String()
		set.Fingerprints[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
	}
	return tmplc.Input{
		PkgPath: pkgPath,
		PkgName: "web",
		Types:   pkg,
		Sets:    []tmpl.CapturedSet{set},
		Escaped: []map[string]tmplescape.Result{
			tmplescape.EscapeAll(
				trees,
				tmplescape.Options{MetaContentURLEscape: true},
			),
		},
	}
}

// function is the source of the generated function whose doc comment
// says it renders what, or "".
func function(src, what string) string {
	start := strings.Index(src, " renders "+what+"\n")
	if start < 0 {
		return ""
	}
	body := src[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	return body
}

// bind is the source of the bind function for the template name.
func bind(src, name string) string {
	fp := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
	start := strings.Index(src, "\nfunc bind_"+fp[:16])
	if start < 0 {
		return ""
	}
	body := src[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	return body
}

// TestTypedPathChosen compiles the bake-off page. Its index entry gets a
// typed variant for view and *view, which its Exec chooses by the data's
// type, and the app template they call reads every field and method
// directly.
func TestTypedPathChosen(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"layout.html", "index.html", "errors.html"} {
		text, err := os.ReadFile(
			filepath.Join("../../tmpl/internal/corpus/testdata/todomvc", name),
		)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(text)
	}
	in := typedInput(t, bakeoffTypes, files, map[string]tmpl.FuncSig{
		"asset":  {Expr: "func(string) string", Nameable: true},
		"plural": {Expr: "func(int) string", Nameable: true},
	})
	out, err := tmplc.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Unsupported) > 0 {
		t.Fatalf("unsupported: %+v", out.Unsupported)
	}
	src := string(out.Source)

	index := bind(src, "index")
	for _, fragment := range []string{"case view:", "case *view:"} {
		if !strings.Contains(index, fragment) {
			t.Errorf("index's bind has no %q:\n%s", fragment, index)
		}
	}
	if strings.Contains(index, "case other:") ||
		strings.Contains(index, "case *other:") {
		t.Errorf(
			"index's bind has a case for other, "+
				"which has none of its fields:\n%s",
			index,
		)
	}
	for _, typ := range []string{"view", "*view"} {
		app := function(src, `"app" for a dot of type `+typ+".")
		if app == "" {
			t.Errorf("no typed app for %s in:\n%s", typ, src)
			continue
		}
		for _, dynamic := range []string{
			"tmplrt.Field", "tmplrt.Method", "tmplrt.Range", "reflect.",
		} {
			if strings.Contains(app, dynamic) {
				t.Errorf("typed app for %s calls %s:\n%s", typ, dynamic, app)
			}
		}
	}
	// errors reads .Title and .Ref, which no type of the package has.
	if errs := bind(src, "errors"); strings.Contains(errs, "switch") {
		t.Errorf("errors has typed variants:\n%s", errs)
	}
}

// pointerTypes has Ptr only as a pointer method. text/template finds it
// on a value only if the value is addressable: through a pointer, or as
// an element of a slice, but not as a map value or as the data itself.
const pointerTypes = `package web

type box struct{ Name string }

func (b *box) Ptr() string { return "ptr " + b.Name }

type holder struct {
	List   []box
	ByName map[string]box
}
`

// TestPointerMethodNeedsAnAddress: a variant that would call a pointer
// method on a value text/template cannot take the address of is no
// candidate, and that data runs the dynamic path, which fails as
// text/template does.
func TestPointerMethodNeedsAnAddress(t *testing.T) {
	in := typedInput(t, pointerTypes, map[string]string{
		"top":   `{{.Ptr}}`,
		"slice": `{{range .List}}{{.Ptr}}{{end}}`,
		"map":   `{{range .ByName}}{{.Ptr}}{{end}}`,
	}, map[string]tmpl.FuncSig{})
	out, err := tmplc.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	src := string(out.Source)
	cases := map[string][]string{
		"top":   {"case *box:"},
		"slice": {"case holder:", "case *holder:"},
		"map":   {},
	}
	for name, want := range cases {
		function := bind(src, name)
		got := regexp.MustCompile(`case [^:]+:`).FindAllString(function, -1)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: cases %v, want %v:\n%s", name, got, want, function)
		}
	}
}

// TestCandidatesAreCapped: of five types that cover the template, the
// first four by name get typed variants.
func TestCandidatesAreCapped(t *testing.T) {
	src := "package web\n"
	for _, name := range []string{"a", "c", "d", "e", "f"} {
		src += "type " + name + " struct{ Title string }\n"
	}
	in := typedInput(
		t,
		src,
		map[string]string{"page": `<h1>{{.Title}}</h1>`},
		map[string]tmpl.FuncSig{},
	)
	out, err := tmplc.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	function := bind(string(out.Source), "page")
	got := regexp.MustCompile(`case [^:]+:`).FindAllString(function, -1)
	want := []string{
		"case a:",
		"case *a:",
		"case c:",
		"case *c:",
		"case d:",
		"case *d:",
		"case e:",
		"case *e:",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("cases %v, want %v:\n%s", got, want, function)
	}
}

// TestShadowedPredeclaredNamesTurnTypingOff: typed code writes len,
// int64 and other predeclared names, so a package that declares one of
// them gets code that holds every value as a reflect.Value, its
// constants and builtins' results included.
func TestShadowedPredeclaredNamesTurnTypingOff(t *testing.T) {
	src := "package web\n\nfunc len(any) int { return 0 }\n\n" +
		"type page struct {\n\tTitle string\n\tItems []string\n}\n"
	in := typedInput(
		t,
		src,
		map[string]string{
			"page": `<a title="{{.Title}}">` +
				`{{if eq 1 2}}{{len .Items}}{{end}}{{3}}</a>`,
		},
		map[string]tmpl.FuncSig{},
	)
	out, err := tmplc.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	src = string(out.Source)
	if strings.Contains(src, "case page:") {
		t.Errorf("typed variants in a package that declares len:\n%s", src)
	}
	called := regexp.MustCompile(`\b(len|int64|uint64|string)\(`)
	if match := called.FindString(src); match != "" {
		t.Errorf(
			"the code calls %s, which the package declares or might:\n%s",
			match,
			src,
		)
	}
}
