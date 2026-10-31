package scaffoldcheck

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// longestName is the longest app name rastrillo new accepts: the name and
// its package name share one import line, which must fit in 80 columns.
const longestName = "warehouseinventorytrackingapp"

// A new app passes rastrillo budget code with no records, whatever its
// name and shell: the first code an agent copies already meets the
// limits it will be held to.
func TestNewAppsMeetTheCodeLimits(t *testing.T) {
	t.Parallel()
	long := []variant{
		{longestName, nil},
		{longestName, []string{"--auth=magic"}},
		{longestName, []string{"--shell=sidebar"}},
		{longestName, []string{"--auth=magic", "--shell=console"}},
	}
	for _, variant := range append(everyShell(), long...) {
		directory := newApp(t, variant.name, variant.flags...)
		output, err := budgetCodeBeforeGitInit(directory)
		if err != nil || !strings.Contains(output, codelimits.NotARepository) {
			t.Errorf("%s %v: budget code: %v\n%s", variant.name,
				variant.flags, err, output)
		}
		for _, path := range unformatted(t, directory) {
			t.Errorf("%s %v: %s is not gofmt'd", variant.name,
				variant.flags, path)
		}
	}
}

// budgetCodeBeforeGitInit runs budget code as a new app's first make ci
// does. Git may not look above the app's parent, so a temporary directory
// inside someone's repository cannot hand the app a base.
func budgetCodeBeforeGitInit(directory string) (string, error) {
	command := exec.Command(rastrilloBinary, "budget", "code")
	command.Dir = directory
	command.Env = append(goEnvironment(),
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(directory))
	output, err := command.CombinedOutput()
	return string(output), err
}

// unformatted is every .go file outside gen/ that gofmt would change: a
// long name moves the alignment of the lines around it.
func unformatted(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry,
		err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(source)
		if err != nil || !bytes.Equal(formatted, source) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// A name one letter longer is refused before anything is written, with
// the reason, rather than scaffolding an app that fails its own gate.
func TestNewRefusesANameTooLongForItsImportLines(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	output, err := runIn(parent, rastrilloBinary, "new", longestName+"s")
	if err == nil || !strings.Contains(output, "choose a shorter name") {
		t.Errorf("rastrillo new %ss: %v, want a refusal:\n%s",
			longestName, err, output)
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 0 {
		t.Errorf("a refused name left %v behind", entries)
	}
}

// Punctuation is kept in the module path and dropped from the package
// name, so a name's lines are not all as long as one sum says. Each of
// these is the longest of its kind that fits: the first fills a comment
// in harness_test.go that holds the name alone, the second an import
// line in render.go that holds the name and its package name. Each
// builds and passes budget code, plain and signing people in.
var namesThatJustFit = []string{
	"my-shop.stock-and.orders-v2.eu",
	"warehouseinventory-trackingap",
}

// One character more than each, and the name the review found
// overflowing harness_test.go, are refused before anything is written.
var namesJustTooLong = []string{
	"my-shop.stock-and.orders-v2.eux",
	"warehouseinventory-trackingapp",
	"warehouse-inventory-track-app-x",
}

func TestNewMeasuresEveryLineOfAPunctuatedName(t *testing.T) {
	t.Parallel()
	for _, name := range namesJustTooLong {
		parent := t.TempDir()
		output, err := runIn(parent, rastrilloBinary, "new", name)
		if err == nil || !strings.Contains(output, "choose a shorter name") {
			t.Errorf("rastrillo new %s: %v, want a refusal:\n%s", name, err,
				output)
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 0 {
			t.Errorf("refusing %s left %v behind", name, entries)
		}
	}
	for _, name := range namesThatJustFit {
		for _, each := range variants {
			directory := buildableApp(t, name, each.flags...)
			run(t, directory, "go", "vet", "./...")
			output, err := budgetCodeBeforeGitInit(directory)
			if err != nil {
				t.Errorf("%s %v: budget code: %v\n%s", name, each.flags,
					err, output)
			}
			for _, path := range unformatted(t, directory) {
				t.Errorf("%s %v: %s is not gofmt'd", name, each.flags, path)
			}
		}
	}
}
