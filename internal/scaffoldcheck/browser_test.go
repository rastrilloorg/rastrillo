//go:build browser

package scaffoldcheck

import (
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The scaffold's own browser drive passes in a real Chromium, for a
// plain app and one that signs people in: a page that renders to a
// recorder can still fail in a browser on a script, a stylesheet or a
// request. The second is on a two-page shell, whose phone navigation
// is a script of its own.
func TestTheScaffoldsBrowserDrivePasses(t *testing.T) {
	t.Parallel()
	// The repository's skip rule: with no browser this fails, unless
	// RASTRILLO_BROWSER_OPTIONAL makes the skip deliberate.
	harness.ChromePath(t)
	for _, variant := range []variant{
		{"walkplainapp", nil},
		{"walkmagicapp", []string{"--auth=magic", "--shell=sidebar"}},
	} {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			directory := buildableApp(t, variant.name, variant.flags...)
			runTaggedTests(t, directory, "browser",
				"./internal/"+variant.name+"test/", "TestBrowserWalk")
		})
	}
}

// An app from before fiesta.js, upgraded as the changelog says: doctor
// --fix, then the layout line it names. A menu left with Escape closes
// and gives focus back to its summary, which is fiesta.js's to do now
// that rastrillo.js is the slim shim, at the root and under a base path
// (testdata/escape_test.go.txt).
func TestAnUpgradedOldAppClosesMenusOnEscape(t *testing.T) {
	t.Parallel()
	harness.ChromePath(t)
	directory, _ := olderApp(t, "upgradedapp")
	older, err := os.ReadFile(filepath.Join(repositoryRoot(),
		"ui", "testdata", "older", "fixed-tags.html"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(directory, "internal", "upgradedapp",
		"templates", "layout.html"), string(older))
	advice := run(t, directory, rastrilloBinary, "doctor", "--fix")
	followTheFiestaAdvice(t, directory, advice)
	drive, err := os.ReadFile(filepath.Join("testdata", "escape_test.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(directory, "internal", "upgradedapptest",
		"escape_extra_test.go"), string(drive))
	runTaggedTests(t, directory, "browser", "./internal/upgradedapptest/",
		"TestAMenuClosesOnEscape", "TestAMenuClosesOnEscapeUnderABasePath")
}
