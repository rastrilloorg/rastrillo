package counts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mibk/dupl/syntax"
	"github.com/mibk/dupl/syntax/golang"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/authored"
	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// app writes a module of the files given and returns its directory.
func app(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module fixture\n\ngo 1.26\n"
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// measureWhole counts an app whose files are all the agent's: there is
// no scaffold, so every file is whole.
func measureWhole(t *testing.T, files map[string]string) RunCounts {
	t.Helper()
	dir := app(t, files)
	code, err := authored.Authored(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run, err := Measure(dir, code)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func funcOf(t *testing.T, run RunCounts, key string) FuncCounts {
	t.Helper()
	for _, one := range run.Funcs {
		if one.Key == key {
			return one
		}
	}
	t.Fatalf("no function %q in %+v", key, run.Funcs)
	return FuncCounts{}
}

func source(body string) string { return "package fixture\n\n" + body }

func TestCognitiveComplexityMatchesGocognit(t *testing.T) {
	run := measureWhole(t, map[string]string{"a.go": source(`func Total(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			if x%2 == 0 {
				total += x
			}
		}
	}
	return total
}
`)})
	// gocognit: for 1, if 2 (one nested), if 3 (two nested).
	if got := funcOf(t, run, "Total").Cognitive; got != 6 {
		t.Fatalf("cognitive = %d, want 6", got)
	}
}

func TestNestingCountsTheDeepestBlock(t *testing.T) {
	run := measureWhole(t, map[string]string{"a.go": source(`func Deep(x int) {
	if x > 0 {
		for i := 0; i < x; i++ {
			switch i {
			case 1:
				println(i)
			}
		}
	} else if x < 0 {
		println(x)
	}
}

func Flat(x int) {
	if x > 0 {
		println(x)
	} else if x < 0 {
		println(-x)
	} else {
		println(0)
	}
}
`)})
	if got := funcOf(t, run, "Deep").Nesting; got != 3 {
		t.Errorf("Deep nesting = %d, want 3", got)
	}
	if got := funcOf(t, run, "Flat").Nesting; got != 1 {
		t.Errorf("Flat nesting = %d, want 1", got)
	}
}

// farName is a function that declares r, then uses it after gap lines
// of other code.
func farName(gap int) string {
	return source("func Far() {\n\tr := 1\n" +
		strings.Repeat("\tprintln(\"x\")\n", gap-1) +
		"\tprintln(r)\n}\n")
}

func TestAShortNameUsedFarAwayIsCounted(t *testing.T) {
	far := measureWhole(t, map[string]string{"a.go": farName(11)})
	if got := funcOf(t, far, "Far").FarShortNames; got != 1 {
		t.Errorf("used 11 lines on: %d, want 1", got)
	}
	near := measureWhole(t, map[string]string{"a.go": farName(10)})
	if got := funcOf(t, near, "Far").FarShortNames; got != 0 {
		t.Errorf("used 10 lines on: %d, want 0", got)
	}
}

const wide = "\tprintln(\"" +
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\")\n"

func TestOnlyAuthoredOffencesAreCounted(t *testing.T) {
	text := source("func Old() {\n" + wide + "}\n\nfunc New() {\n" + wide + "}\n\nfunc Fine() {}\n")
	dir := app(t, map[string]string{"a.go": text})
	// Old is the scaffold's, New is the agent's.
	only := func(key string, start int) authored.Code {
		return authored.Code{Files: []authored.File{{
			Path: "a.go", Funcs: []authored.Func{{Key: key, Start: start,
				End: start + 2}},
		}}}
	}
	untouched, err := Measure(dir, only("New", 7))
	if err != nil {
		t.Fatal(err)
	}
	if got := untouched.App.Offences[codelimits.Columns]; got != 1 {
		t.Errorf("wide lines counted = %d, want 1 (New's)", got)
	}
	if got := funcOf(t, untouched, "New").OverLimitLines; got != 1 {
		t.Errorf("New over-limit lines = %d, want 1", got)
	}
	none := authored.Code{Files: []authored.File{{
		Path: "a.go", Funcs: []authored.Func{{Key: "Fine", Start: 11, End: 11}},
	}}}
	run, err := Measure(dir, none)
	if err != nil {
		t.Fatal(err)
	}
	if got := run.App.Offences[codelimits.Columns]; got != 0 {
		t.Errorf("wide lines in untouched functions = %d, want 0", got)
	}
}

// clone is a function of n statements, each a call of the same shape.
func clone(name string, statements int) string {
	var out strings.Builder
	out.WriteString("func " + name + "(first, second int) int {\n")
	for range statements {
		out.WriteString("\tfirst = first + second*2\n")
	}
	out.WriteString("\treturn first\n}\n")
	return out.String()
}

func TestDuplicatedBlocksCountTheirLines(t *testing.T) {
	run := measureWhole(t, map[string]string{
		"a.go": source(clone("One", 17) + "\n" + clone("Two", 17)),
	})
	one, two := funcOf(t, run, "One"), funcOf(t, run, "Two")
	if one.DuplicatedLines != 20 || two.DuplicatedLines != 20 {
		t.Fatalf("One %+v, Two %+v: want two copies of 20 lines", one, two)
	}
	total := one.DuplicatedLines + two.DuplicatedLines
	if total != 40 {
		t.Fatalf("duplicated lines = %d, want 40", total)
	}
}

func TestAnInterfaceWithOneImplementationIsCounted(t *testing.T) {
	one := measureWhole(t, map[string]string{"a.go": source(`type Speaker interface{ Speak() string }

type Dog struct{}

func (Dog) Speak() string { return "woof" }
`)})
	if one.SingleImplementationInterfaces == nil ||
		*one.SingleImplementationInterfaces != 1 {
		t.Fatalf("got %v, want 1", one.SingleImplementationInterfaces)
	}
	two := measureWhole(t, map[string]string{"a.go": source(`type Speaker interface{ Speak() string }

type Dog struct{}

func (Dog) Speak() string { return "woof" }

type Cat struct{}

func (*Cat) Speak() string { return "meow" }
`)})
	if two.SingleImplementationInterfaces == nil ||
		*two.SingleImplementationInterfaces != 0 {
		t.Fatalf("two implementations: got %v, want 0",
			two.SingleImplementationInterfaces)
	}
}

func TestAFileThatDoesNotCompileIsStillCounted(t *testing.T) {
	run := measureWhole(t, map[string]string{"a.go": source(`func Broken(x int) int {
	if x > 0 {
		return missing(x)
	}
	return 0
}
`)})
	got := funcOf(t, run, "Broken")
	if got.Cognitive != 1 || got.Nesting != 1 || got.Lines != 6 {
		t.Fatalf("counts of a file that does not compile: %+v", got)
	}
}

func TestInterfacesAreUnavailableWhenTheAppDoesNotTypeCheck(t *testing.T) {
	run := measureWhole(t, map[string]string{"a.go": source(`type Speaker interface{ Speak() string }

func Broken() int { return missing }
`)})
	if run.SingleImplementationInterfaces != nil {
		t.Fatalf("got %d, want unavailable (nil)",
			*run.SingleImplementationInterfaces)
	}
}

func TestTestFunctionsAreSummarisedApart(t *testing.T) {
	run := measureWhole(t, map[string]string{
		"a.go": source("func Small() {}\n"),
		"a_test.go": source(`import "testing"

func TestBig(t *testing.T) {
	println(1)
	println(2)
	println(3)
	println(4)
}
`),
	})
	app, tests := Summarise(run), SummariseTests(run)
	if app["lines"].Median != 1 || tests["lines"].Median != 6 {
		t.Fatalf("app lines %+v, test lines %+v", app["lines"], tests["lines"])
	}
}

func TestAFileGrownPastFourHundredLinesIsCounted(t *testing.T) {
	text := source("func Old() {}\n") + strings.Repeat("// padding\n", 400) +
		"\nfunc Added() {}\n"
	dir := app(t, map[string]string{"big.go": text})
	added := 3 + 400 + 2
	run, err := Measure(dir, authored.Code{Files: []authored.File{{
		Path:  "big.go",
		Funcs: []authored.Func{{Key: "Added", Start: added, End: added}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := run.App.Offences[codelimits.FileLines]; got != 1 {
		t.Fatalf("file offences = %d, want 1", got)
	}
}

func TestBlankLinesAndCommentsChangeNoCount(t *testing.T) {
	plain := source(`func Walk(xs []int) int {
	r := 0
	for _, x := range xs {
		if x > 0 {
			r += x
		}
	}
	println("done")
	return r
}
`)
	padded := source(`// Walk adds the positive numbers.
func Walk(xs []int) int {

	r := 0 // the total

	// each one
	for _, x := range xs {

		if x > 0 {
			r += x
		}
	}

	println("done")
	return r
}
`)
	before := funcOf(t, measureWhole(t, map[string]string{"a.go": plain}), "Walk")
	after := funcOf(t, measureWhole(t, map[string]string{"a.go": padded}), "Walk")
	if after.Lines <= before.Lines {
		t.Fatalf("padding added no lines: %d then %d", before.Lines, after.Lines)
	}
	after.Lines, after.End = before.Lines, before.End
	if before != after {
		t.Fatalf("padding changed counts: %+v then %+v", before, after)
	}
}

// sizedClone is a function of exactly nodes syntax nodes in dupl's
// serialization, built from statements of three and four nodes.
func sizedClone(t *testing.T, name string, nodes int) string {
	t.Helper()
	build := func(threes, fours int) string {
		var out strings.Builder
		out.WriteString("func " + name + "(first, second int) {\n")
		for range threes {
			out.WriteString("\tfirst = second\n")
		}
		for range fours {
			out.WriteString("\tfirst = -second\n")
		}
		out.WriteString("}\n")
		return out.String()
	}
	empty := nodesIn(t, build(0, 0))
	extra := nodes - empty
	fours := extra % 3
	threes := (extra - 4*fours) / 3
	if threes < 0 || nodesIn(t, build(threes, fours)) != nodes {
		t.Fatalf("cannot build %d nodes from %d", nodes, empty)
	}
	return build(threes, fours)
}

// nodesIn is how many nodes dupl serializes for the function: the
// number its threshold of 100 is held against.
func nodesIn(t *testing.T, function string) int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte(source(function)), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, err := golang.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range syntax.Serialize(tree) {
		if node.Type == golang.FuncDecl {
			return node.Owns + 1
		}
	}
	t.Fatal("no function")
	return 0
}

func TestDuplicationEitherSideOfTheThreshold(t *testing.T) {
	for _, test := range []struct {
		nodes, want int
	}{{99, 0}, {100, 1}} {
		t.Run(fmt.Sprint(test.nodes), func(t *testing.T) {
			text := sizedClone(t, "One", test.nodes) + "\n" +
				sizedClone(t, "Two", test.nodes)
			run := measureWhole(t, map[string]string{"a.go": source(text)})
			lines := funcOf(t, run, "One").Lines
			if got := funcOf(t, run, "One").DuplicatedLines; test.want == 0 &&
				got != 0 || test.want == 1 && got != lines {
				t.Fatalf("%d nodes: %d of %d lines counted",
					test.nodes, got, lines)
			}
		})
	}
}

const speaker = `type Speaker interface{ Speak() string }

type Dog struct{}

func (Dog) Speak() string { return "woof" }
`

func TestInterfacesAreFoundWhateverWayTheAppIsNamed(t *testing.T) {
	dir := app(t, map[string]string{"a.go": source(speaker)})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	code, err := authored.Authored(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(here, dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"relative": relative, "symlink": link, "absolute": dir,
	} {
		run, err := Measure(path, code)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if run.SingleImplementationInterfaces == nil ||
			*run.SingleImplementationInterfaces != 1 {
			t.Errorf("%s: got %v, want 1", name,
				run.SingleImplementationInterfaces)
		}
	}
}

func TestAUnitWithNoFunctionIsAnError(t *testing.T) {
	dir := app(t, map[string]string{"a.go": source("func One() {}\n")})
	_, err := Measure(dir, authored.Code{Files: []authored.File{{
		Path: "a.go", Funcs: []authored.Func{{Key: "Ghost", Start: 3, End: 9}},
	}}})
	if err == nil || !strings.Contains(err.Error(), "Ghost") {
		t.Fatalf("got %v, want an error naming Ghost", err)
	}
}

func TestNestedUnitsAreCountedOnce(t *testing.T) {
	text := source(`var Table = func() {
	inner := func() {
` + wide + `	}
	inner()
}
`)
	dir := app(t, map[string]string{"a.go": text})
	run, err := Measure(dir, authored.Code{Files: []authored.File{{
		Path: "a.go", Funcs: []authored.Func{
			{Key: "Table", Start: 3, End: 8},
			{Key: "Table.func1", Start: 4, End: 6},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	over := 0
	for _, one := range run.Funcs {
		over += one.OverLimitLines
	}
	if over != 1 {
		t.Errorf("over-limit lines summed over units = %d, want 1", over)
	}
	if run.App.Lines != 6 {
		t.Errorf("authored lines = %d, want the 6 code lines of lines 3-8",
			run.App.Lines)
	}
}

// An app's own totals never include its tests: an agent that writes
// more tests has not written more app code.
func TestAppAndTestTotalsAreApart(t *testing.T) {
	appCode := source("// Serve serves.\nfunc Serve() {\n" + wide + "}\n")
	testCode := "package fixture\n\n// TestServe checks Serve.\n" +
		"func TestServe() {\n" + wide + wide + "\tServe()\n}\n"
	dir := app(t, map[string]string{"a.go": appCode, "a_test.go": testCode})
	run, err := Measure(dir, authored.Code{Files: []authored.File{
		{Path: "a.go", Whole: true},
		{Path: "a_test.go", Test: true, Whole: true},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if run.App.Files != 1 || run.Tests.Files != 1 {
		t.Errorf("files: app %d, tests %d, want 1 and 1", run.App.Files,
			run.Tests.Files)
	}
	if got := run.App.Offences[codelimits.Columns]; got != 1 {
		t.Errorf("app wide lines = %d, want 1", got)
	}
	if got := run.Tests.Offences[codelimits.Columns]; got != 2 {
		t.Errorf("test wide lines = %d, want 2", got)
	}
	if run.App.Lines != 4 || run.Tests.Lines != 6 {
		t.Errorf("code lines: app %d, tests %d, want 4 and 6", run.App.Lines,
			run.Tests.Lines)
	}
	if !near(run.App.CommentShare, 1.0/4) || !near(run.Tests.CommentShare, 1.0/6) {
		t.Errorf("comment share: app %v, tests %v, want 1/4 and 1/6",
			run.App.CommentShare, run.Tests.CommentShare)
	}
}

func near(got, want float64) bool { return got-want < 1e-9 && want-got < 1e-9 }
