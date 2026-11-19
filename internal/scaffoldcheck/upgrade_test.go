package scaffoldcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// olderShim stands in for the full rastrillo.js an app vendored before
// fiesta.js. doctor only reads that it is not the slim shim.
const olderShim = "// rastrillo.js before fiesta.js: menus, switches, schemes\n"

// olderApp is a scaffolded app as an app from before fiesta.js has it:
// the full rastrillo.js, neither fiesta.js nor copy.js, and its own
// rastrillo.js, which it never recorded as its own.
func olderApp(t *testing.T, name string) (directory, static string) {
	t.Helper()
	directory = buildableApp(t, name)
	static = filepath.Join(directory, "internal", name, "static")
	write(t, filepath.Join(static, "rastrillo.js"), olderShim)
	for _, added := range []string{"fiesta.js", "copy.js"} {
		if err := os.Remove(filepath.Join(static, added)); err != nil {
			t.Fatal(err)
		}
	}
	return directory, static
}

// An app whose rastrillo.js is not its own needs no hand edit after
// doctor --fix: doctor writes the slim shim and fiesta.js together, so
// the app's own vendored test, which requires both, passes again.
func TestDoctorFixLeavesAnOlderAppsVendoredTestPassing(t *testing.T) {
	t.Parallel()
	directory, _ := olderApp(t, "olderapp")
	vendored := "TestVendoredAssetsMatchTheLibrary"
	if output, err := runIn(directory, "go", "test", "-count=1",
		"-run", "^"+vendored+"$", "./internal/olderapptest/"); err == nil {
		t.Fatalf("the older app's vendored test passes before the fix, "+
			"so it cannot show the fix:\n%s", output)
	}
	run(t, directory, rastrilloBinary, "doctor", "--fix")
	runTests(t, directory, "./internal/olderapptest/", vendored)
}

// fiestaAdvice finds doctor's line for a layout that loads no fiesta.js
// and the tag it says to add.
var fiestaAdvice = regexp.MustCompile(`(?m)^(\S+): layout loads no ` +
	`fiesta\.js: add (<script .+</script>) right after the layout's ` +
	`rastrillo\.js tag \(CHANGELOG\)$`)

// shimTag is the old column layout's rastrillo.js tag.
const shimTag = `<script defer ` +
	`src="{{path $ (asset "static/rastrillo.js")}}"></script>`

// followTheFiestaAdvice makes the one edit doctor names and nothing
// else, so the browser drive proves the advice as printed.
func followTheFiestaAdvice(t *testing.T, directory, advice string) {
	t.Helper()
	found := fiestaAdvice.FindStringSubmatch(advice)
	if found == nil {
		t.Fatalf("doctor --fix names no layout line for fiesta.js:\n%s",
			advice)
	}
	path := filepath.Join(directory, found[1])
	layout, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(layout), shimTag) != 1 {
		t.Fatalf("%s has no single rastrillo.js tag to add after", path)
	}
	write(t, path, strings.Replace(string(layout), shimTag,
		shimTag+"\n"+found[2], 1))
}
