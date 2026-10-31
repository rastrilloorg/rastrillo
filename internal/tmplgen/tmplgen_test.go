package tmplgen

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// TestNameableChecksRefs gives capture's provisional Nameable to the
// type checker: reflect names a type declared inside a function as if
// it were declared at the top level.
func TestNameableChecksRefs(t *testing.T) {
	const owner = "example.com/app"
	const src = `package app

type Item struct{}

type hidden int

func f() {
	type Local int
	_ = Local(0)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "app.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	conf := types.Config{Importer: importer.Default()}
	app, err := conf.Check(owner, fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := types.NewPackage("example.com/other", "other")
	for _, name := range []string{"Exported", "unexported"} {
		other.Scope().Insert(types.NewTypeName(
			token.NoPos, other, name, types.Typ[types.Int]))
	}
	lookup := func(path string) *types.Package {
		return map[string]*types.Package{owner: app, other.Path(): other}[path]
	}
	ref := func(pkgPath, name string) tmpl.TypeRef {
		return tmpl.TypeRef{PkgPath: pkgPath, Name: name}
	}
	refs := func(list ...tmpl.TypeRef) []tmpl.TypeRef { return list }
	otherPath := other.Path()

	for _, tc := range []struct {
		name string
		refs []tmpl.TypeRef
		want bool
	}{
		{"no named types", nil, true},
		{"the owner's exported type", refs(ref(owner, "Item")), true},
		{"the owner's unexported type", refs(ref(owner, "hidden")), true},
		{"a type local to a function", refs(ref(owner, "Local")), false},
		{
			"another package's exported type",
			refs(ref(otherPath, "Exported")), true,
		},
		{
			"another package's unexported type",
			refs(ref(otherPath, "unexported")), false,
		},
		{
			"a package that did not load",
			refs(ref("example.com/gone", "T")), false,
		},
		{
			"one bad ref among good",
			refs(ref(owner, "Item"), ref(owner, "Local")), false,
		},
	} {
		sig := tmpl.FuncSig{Expr: "func() T", Refs: tc.refs, Nameable: true}
		if got := nameable(sig, owner, lookup); got != tc.want {
			t.Errorf("%s: nameable = %v, want %v", tc.name, got, tc.want)
		}
	}
	if nameable(tmpl.FuncSig{}, owner, lookup) {
		t.Error("a signature capture could not write is nameable")
	}
}

// TestTailIsOneLine: a capture failure is printed as one line, however
// many lines the command wrote.
func TestTailIsOneLine(t *testing.T) {
	out := []byte("panic: boom\n\ngoroutine 1 [running]:\n\tmain.main()\n")
	want := "panic: boom goroutine 1 [running]: main.main()"
	if got := tail(out, nil); got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
}

// The capture child starts with what running a Go program needs and
// the capture file, nothing else: an app reads its database address,
// secrets and service URLs from the environment, and capture must not
// reach any of them.
func TestCaptureEnvIsMinimal(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://prod")
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/home/x")
	t.Setenv("TMPDIR", "/tmp/x")
	t.Setenv("LANG", "C.UTF-8")
	got := captureEnv("/scratch/capture.json")
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/x",
		"TMPDIR=/tmp/x",
		"LANG=C.UTF-8",
		tmpl.CaptureEnv + "=/scratch/capture.json",
	}
	if !slices.Equal(got, want) {
		t.Errorf("captureEnv = %q, want %q", got, want)
	}
}
