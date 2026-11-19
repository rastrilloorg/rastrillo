package codelimits

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// hashOf spells out what the identity of a long line is, so that a test
// does not borrow the code it checks.
func hashOf(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:])[:12]
}

// order is the sort the contract promises. Sorting the wanted offences
// with it, then comparing for equality, checks the order too.
func order(left, right Offence) int {
	return cmp.Or(
		cmp.Compare(left.Path, right.Path),
		cmp.Compare(left.Kind, right.Kind),
		cmp.Compare(left.Key, right.Key),
		cmp.Compare(left.Line, right.Line),
	)
}

func measureFixture(t *testing.T, name string) []Offence {
	t.Helper()
	got, err := Measure(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func requireOffences(t *testing.T, got, want []Offence) {
	t.Helper()
	slices.SortFunc(want, order)
	if !slices.Equal(got, want) {
		t.Fatalf("offences differ\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMeasureFindsEachKindOfOffence(t *testing.T) {
	tabbed := "// a" + strings.Repeat("\t", 19) + "b"
	accented := "// " + strings.Repeat("é", 78)
	want := []Offence{
		{Columns, "wide.go", hashOf(tabbed), 81, 80, 3},
		{Columns, "wide.go", hashOf(accented), 81, 80, 5},
		{FileLines, "big.go", "", 401, 400, 1},
		{FuncLines, "long.go", "LongFunction", 41, 40, 6},
		{FuncLines, "long.go", "handlers.Show", 16, 15, 53},
		{FuncLines, "long.go", "Register.action1", 16, 15, 76},
		{FuncLines, "long.go", "handler", 41, 40, 97},
		{Names, "short.go", "-/x", 1, 0, 3},
		{Names, "short.go", "-/n", 1, 0, 5},
		{Names, "short.go", "Sum/p", 1, 0, 8},
		{Names, "short.go", "Sum/i", 1, 0, 10},
	}
	requireOffences(t, measureFixture(t, "offences"), want)
}

// leftOut are the directories the walk skips. Each holds a one-letter
// variable, so the check below that measures one as a root proves the
// fixture offends and the walk is what leaves it alone.
var leftOut = []string{
	"testdata", ".hidden", "_under", "vendor", "node_modules", "nested",
}

func TestMeasureLeavesAloneWhatIsAllowed(t *testing.T) {
	if got := measureFixture(t, "allowed"); len(got) != 0 {
		t.Fatalf("allowed code was measured: %+v", got)
	}
	for _, name := range leftOut {
		got := measureFixture(t, filepath.Join("allowed", name))
		if len(got) != 1 || got[0].Key != "-/x" {
			t.Errorf("%s measured on its own: %+v", name, got)
		}
	}
}

func TestActionsResolveThroughImports(t *testing.T) {
	want := []Offence{
		{FuncLines, "action/inside.go", "Inside", 16, 15, 7},
		{FuncLines, "aliased.go", "Aliased", 16, 15, 6},
	}
	requireOffences(t, measureFixture(t, "actions"), want)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// measureSource measures one file in a module of its own, so a test can
// state the code it is about in the test itself.
func measureSource(t *testing.T, source string) []Offence {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/probe\n")
	writeFile(t, filepath.Join(root, "probe.go"), source)
	got, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func keysOf(offences []Offence) []string {
	keys := []string{}
	for _, offence := range offences {
		keys = append(keys, offence.Key)
	}
	return keys
}

func requireKeys(t *testing.T, got []Offence, want ...string) {
	t.Helper()
	if keys := keysOf(got); !slices.Equal(keys, want) {
		t.Fatalf("keys %v, want %v (%+v)", keys, want, got)
	}
}

func TestOnlyTheRightTestingTypeExcusesALetter(t *testing.T) {
	got := measureSource(t, `package probe

import tst "testing"

func wrong(t int, b *tst.T, f tst.TB) {}

func right(t *tst.T, b *tst.B, f *tst.F) {}

func either(t tst.TB) {}
`)
	requireKeys(t, got, "wrong/b", "wrong/f", "wrong/t")
}

func TestARedeclaredVariableIsNotANewName(t *testing.T) {
	got := measureSource(t, `package probe

func step() (int, error) { return 0, nil }

func twice(value any) error {
	a, e := step()
	b, e := step()
	switch v := value.(type) {
	case int:
		_ = v
	}
	for k, c := range []int{a, b} {
		_, _ = k, c
	}
	return e
}
`)
	requireKeys(t, got,
		"twice/a", "twice/b", "twice/c", "twice/e", "twice/k", "twice/v")
}

func TestLiteralsInAVariableAreNamedByOrder(t *testing.T) {
	short := strings.Repeat("\t_ = 1\n", 16)
	long := strings.Repeat("\t_ = 1\n", 41)
	got := measureSource(t, `package probe

import "amadan.net/rastrillo/rastrillo/action"

var direct = func(visit *action.Visit) {
`+short+`}

var callbacks = map[string]func(){
	"first": func() {},
	"second": func() {
`+long+`	},
}

var wired = []any{
	func(visit *action.Visit) {
`+short+`	},
	func() {
`+long+`	},
}
`)
	requireKeys(t, got,
		"callbacks.func2", "direct", "wired.action1", "wired.func1")
}

func TestAColumnsKeyIgnoresTrailingWhiteSpaceOnly(t *testing.T) {
	long := "// " + strings.Repeat("x", 82)
	got := measureSource(t,
		"package probe\n\n"+long+"\n"+long+"  \n\t"+long+"\n")
	if len(got) != 3 {
		t.Fatalf("want three offences, got %+v", got)
	}
	byLine := map[int]Offence{}
	for _, offence := range got {
		byLine[offence.Line] = offence
	}
	if byLine[3].Key != hashOf(long) || byLine[4].Key != hashOf(long) {
		t.Errorf("trailing spaces changed the key: %+v", got)
	}
	if byLine[5].Key != hashOf("\t"+long) || byLine[5].Size != 89 {
		t.Errorf("an indented line is a new line: %+v", byLine[5])
	}
}

// A //line directive renumbers what the compiler reports, and one put
// just before a closing brace made a long body measure as negative. The
// gate counts the lines a reader scrolls past, so it ignores them.
func TestLineDirectivesDoNotMoveTheMeasure(t *testing.T) {
	got := measureSource(t, `package probe

import "amadan.net/rastrillo/rastrillo/action"

//line probe.go:900
func long() {
`+strings.Repeat("\t_ = 1\n", 41)+`//line probe.go:1
}

func act(visit *action.Visit) {
`+strings.Repeat("\t_ = 1\n", 16)+`//line probe.go:1
}

//line probe.go:700
var x = 1
`)
	want := []Offence{
		{FuncLines, "probe.go", "long", 42, 40, 6},
		{FuncLines, "probe.go", "act", 17, 15, 51},
		{Names, "probe.go", "-/x", 1, 0, 72},
	}
	requireOffences(t, got, want)
}

// A name repeated in a file is numbered over every function and
// variable holding it, short or long, so that an identity stays put
// when a namesake before it is shortened. A literal is named by the
// numbered name of what holds it.
func TestRepeatedNamesAreNumberedInSourceOrder(t *testing.T) {
	long := strings.Repeat("\t_ = 1\n", 41)
	action := "\t_ = func(visit *action.Visit) {\n" +
		strings.Repeat("\t\t_ = 1\n", 16) + "\t}\n"
	got := measureSource(t, `package probe

import "amadan.net/rastrillo/rastrillo/action"

func init() {}

func init() {
`+long+`}

func init() {
`+action+`}

var _ = func() {}

var _ = []func(){func() {
`+long+`}}
`)
	requireKeys(t, got, "_~2.func1", "init~2", "init~3.action1")
}

// Parentheses and a dot import spell the same *action.Visit; either
// let a 16-line action pass as an ordinary function.
func TestAnActionIsAnActionHoweverItsTypeIsSpelled(t *testing.T) {
	body := strings.Repeat("\t_ = 1\n", 16)
	for _, spelling := range []struct {
		name, imports, parameter string
		want                     []string
	}{
		{"pointer to parenthesised", `"amadan.net/rastrillo/rastrillo/action"`,
			"*(action.Visit)", []string{"save"}},
		{"parenthesised pointer", `"amadan.net/rastrillo/rastrillo/action"`,
			"(*action.Visit)", []string{"save"}},
		{"dot import", `. "amadan.net/rastrillo/rastrillo/action"`,
			"*Visit", []string{"save"}},
		{"dot import of another action", `. "example.com/other/action"`,
			"*Visit", []string{}},
	} {
		t.Run(spelling.name, func(t *testing.T) {
			got := measureSource(t, "package probe\n\nimport "+
				spelling.imports+"\n\nfunc save(visit "+
				spelling.parameter+") {\n"+body+"}\n")
			requireKeys(t, got, spelling.want...)
		})
	}
}

// A generic receiver declares its type parameters afresh, and they are
// held to the type-parameter rule: one capital letter, as in Opt[T].
func TestAReceiversTypeParametersAreNames(t *testing.T) {
	got := measureSource(t, `package probe

type Box[T any] struct{ value T }

type Pair[K, V any] struct{}

func (box Box[t]) Read(value t) {}

func (box *Box[T]) Write(value T) {}

func (pair Pair[k, V]) Keys() {}

func (pair *Pair[_, V]) Values() {}
`)
	requireKeys(t, got, "Box.Read/t", "Pair.Keys/k")
}

// A vendored file is a copy kept close to its upstream so it can be
// re-synced, so nothing in it is measured, however much of it offends.
func TestAVendoredFileIsNotMeasured(t *testing.T) {
	body := strings.Repeat("\t_ = 1\n", 41)
	got := measureSource(t, `// Copyright The Go Authors.

//rastrillo:vendored go1.26.10 src/html/template/escape.go

package probe

// `+strings.Repeat("x", 82)+`
var x = 1

func long() {
`+body+`}
`)
	requireKeys(t, got)
}

// A marker that names no source is no marker: the copy could not be
// traced back to the file it should be re-synced from.
func TestAVendoredMarkerNeedsASource(t *testing.T) {
	for _, marker := range []string{
		"//rastrillo:vendored ", "//rastrillo:vendored    ",
		"//rastrillo:vendored",
	} {
		got := measureSource(t, marker+"\n\npackage probe\n\nvar x = 1\n")
		requireKeys(t, got, "-/x")
	}
}

// Only the comments above the package clause can mark a file, so the
// same text lower down is just a comment.
func TestAVendoredMarkerAfterThePackageClauseDoesNotCount(t *testing.T) {
	got := measureSource(t, `package probe

//rastrillo:vendored go1.26.10 src/html/template/escape.go

var x = 1
`)
	requireKeys(t, got, "-/x")
}
