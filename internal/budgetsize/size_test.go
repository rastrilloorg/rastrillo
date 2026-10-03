package budgetsize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

func write(t *testing.T, root, rel string, lines int) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("//\n", lines)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func module(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "go.mod", 1)
	return root
}

func check(t *testing.T, root, records string) []string {
	t.Helper()
	f, err := budgetfile.Parse(strings.NewReader(records))
	if err != nil {
		t.Fatal(err)
	}
	counts, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	return Check(counts, f)
}

func TestAtTheCeilingPasses(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 5000)
	write(t, root, "internal/app/a_test.go", 8000)
	if got := check(t, root, ""); len(got) != 0 {
		t.Fatalf("at exactly the ceiling: %v", got)
	}
}

func TestOneOverSourceFails(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 3000)
	write(t, root, "internal/app/b.go", 2001)
	got := check(t, root, "")
	if len(got) != 1 || !strings.Contains(got[0], "internal/app") || !strings.Contains(got[0], "5001") {
		t.Fatalf("got %v", got)
	}
}

func TestOneOverTestFails(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a_test.go", 8001)
	if got := check(t, root, ""); len(got) != 1 || !strings.Contains(got[0], "8001") {
		t.Fatalf("got %v", got)
	}
}

func TestRecordRaisesOneColumn(t *testing.T) {
	root := module(t)
	write(t, root, "ui/a_test.go", 9500)
	if got := check(t, root, "size ui - 9800 one test per component\n"); len(got) != 0 {
		t.Fatalf("test-only directory with a test-column record: %v", got)
	}
}

func TestStaleRecordFails(t *testing.T) {
	root := module(t)
	got := check(t, root, "size internal/gone 6000 - was big once\n")
	if len(got) != 1 || !strings.Contains(got[0], "no Go files") {
		t.Fatalf("got %v", got)
	}
}

func TestRatchetFails(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 5500)
	got := check(t, root, "size internal/app 10000 - split owed\n")
	if len(got) != 1 || !strings.Contains(got[0], "lower it") {
		t.Fatalf("5500 is under 60%% of 10000: got %v", got)
	}
}

func TestColumnBackAtDefaultAsksForDash(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 4000)
	got := check(t, root, "size internal/app 5100 - split owed\n")
	if len(got) != 1 || !strings.Contains(got[0], "write -") {
		t.Fatalf("got %v", got)
	}
}

func TestNestedModuleIsNotCountedIntoItsParent(t *testing.T) {
	root := module(t)
	write(t, root, "examples/notes/go.mod", 1)
	write(t, root, "examples/notes/internal/notes/a.go", 6000)
	if got := check(t, root, ""); len(got) != 0 {
		t.Fatalf("parent counted a nested module: %v", got)
	}
	got := check(t, filepath.Join(root, "examples", "notes"), "")
	if len(got) != 1 || !strings.Contains(got[0], "internal/notes") {
		t.Fatalf("nested module measured from its own root: %v", got)
	}
}

func TestSkipsWhatGoSkips(t *testing.T) {
	root := module(t)
	for _, d := range []string{".hidden", "_old", "testdata", "vendor", "node_modules"} {
		write(t, root, d+"/x/a.go", 9000)
	}
	if got := check(t, root, ""); len(got) != 0 {
		t.Fatalf("counted a skipped directory: %v", got)
	}
}
