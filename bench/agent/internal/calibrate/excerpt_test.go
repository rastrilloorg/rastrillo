package calibrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/authored"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
)

// measured writes a module of the files given, every one of them the
// agent's, and counts it. It returns the app's directory and its counts.
func measured(
	t *testing.T, files map[string]string,
) (string, []counts.FuncCounts) {
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
	code, err := authored.Authored(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run, err := counts.Measure(dir, code)
	if err != nil {
		t.Fatal(err)
	}
	return dir, run.Funcs
}

func source(body string) string { return "package fixture\n\n" + body }

// straight is a function of exactly lines lines with no branches.
func straight(name string, lines int) string {
	var body strings.Builder
	fmt.Fprintf(&body, "func %s() int {\n\ttotal := 0\n", name)
	for range lines - 4 {
		body.WriteString("\ttotal++\n")
	}
	body.WriteString("\treturn total\n}\n")
	return body.String()
}

func TestAnExcerptOverSixtyLinesIsDropped(t *testing.T) {
	dir, funcs := measured(t, map[string]string{
		"long.go":  source(straight("Long", 61)),
		"short.go": source(straight("Short", 40)),
		"edge.go":  source(straight("Edge", 58)),
	})

	if excerpt, fits := Cut(dir, []string{"long.go:Long"}, funcs); fits {
		t.Errorf("a 61-line function fits:\n%s", excerpt.Text)
	} else if excerpt.Lines() <= MaxLines {
		t.Errorf("the dropped excerpt says it has %d lines", excerpt.Lines())
	}
	// 58 lines and its file's header line: 59.
	if excerpt, fits := Cut(dir, []string{"edge.go:Edge"}, funcs); !fits {
		t.Errorf("a %d-line excerpt was dropped", excerpt.Lines())
	}
	// 40 and 58 make 98 with their headers and the blank between them.
	if _, fits := Cut(dir,
		[]string{"short.go:Short", "edge.go:Edge"}, funcs); fits {
		t.Error("two functions over 60 lines together fit")
	}
}

func TestAnExcerptKeepsSourceOrder(t *testing.T) {
	dir, funcs := measured(t, map[string]string{
		"a.go": source("// First is first.\nfunc First() {}\n\n" +
			"func Second() {}\n\nfunc NotAsked() {}\n"),
		"b.go": source("func Third() {}\n"),
	})

	excerpt, fits := Cut(dir,
		[]string{"b.go:Third", "a.go:Second", "a.go:First", "a.go:Missing"},
		funcs)

	if !fits {
		t.Fatalf("a short excerpt was dropped:\n%s", excerpt.Text)
	}
	want := "// a.go\n// First is first.\nfunc First() {}\n\n" +
		"func Second() {}\n\n// b.go\nfunc Third() {}\n"
	if excerpt.Text != want {
		t.Errorf("text =\n%s\nwant\n%s", excerpt.Text, want)
	}
	var keys []string
	for _, one := range excerpt.Funcs {
		keys = append(keys, one.Key)
	}
	if strings.Join(keys, " ") != "First Second Third" {
		t.Errorf("funcs = %v, want First Second Third", keys)
	}
}

// counts gives a wide line to the innermost unit holding it. A literal
// inside a function the excerpt shows is shown with it, and its
// offences still count towards the excerpt.
func TestAnExcerptCountsTheOffencesOfUnitsInsideIt(t *testing.T) {
	wide := "\t\tprintln(\"" + strings.Repeat("a", 74) + "\")\n"
	dir, funcs := measured(t, map[string]string{
		"a.go": source("var Table = func() {\n\tinner := func() {\n" +
			wide + "\t}\n\tinner()\n}\n"),
	})

	excerpt, fits := Cut(dir, []string{"a.go:Table", "a.go:Table.func1"},
		funcs)

	if !fits || len(excerpt.Funcs) != 1 {
		t.Fatalf("funcs = %+v, want Table alone, its literal shown in it",
			excerpt.Funcs)
	}
	if got := Composite(excerpt, Weights{OverLimit: 1}); got != 1 {
		t.Errorf("over-limit composite = %v, want the literal's 1 wide line\n%+v",
			got, funcs)
	}
	alone, _ := Cut(dir, []string{"a.go:Table"}, funcs)
	if got := Composite(alone, Weights{OverLimit: 1}); got != 1 {
		t.Errorf("an excerpt naming only Table: over-limit composite = %v, "+
			"want 1: the literal is shown in it", got)
	}
}
