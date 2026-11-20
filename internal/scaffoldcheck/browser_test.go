//go:build browser

package scaffoldcheck

import (
	"os"
	"path/filepath"
	"strings"
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
				"./internal/"+variant.name+"/", "TestBrowserWalk")
		})
	}
}

// A topbar app on a phone, as rastrillo new writes it: its home page's
// "‹ Menu" reaches /menu#nav-home, and the Home row there leads back
// (testdata/topbar_walk_test.go.txt). Its own browser drive passes too.
func TestTheScaffoldedTopbarPhoneWalksToItsMenu(t *testing.T) {
	t.Parallel()
	harness.ChromePath(t)
	const name = "walktopbarapp"
	directory := buildableApp(t, name, "--shell=topbar")
	addDrive(t, directory, name, "topbar_walk_test.go.txt",
		"phone_walk_test.go")
	runTaggedTests(t, directory, "browser", "./internal/"+name+"/",
		"TestBrowserWalk", "TestPhoneWalksToTheMenuAndBack")
}

// A topbar app that signs people in signs out with scripts off: the
// account card's sign-out opts out of busy.js, so it is a plain form,
// and it must work as one (testdata/signout_test.go.txt).
func TestTheAuthTopbarSignsOutWithScriptsOff(t *testing.T) {
	t.Parallel()
	harness.ChromePath(t)
	const name = "signoutapp"
	directory := buildableApp(t, name, "--auth=magic", "--shell=topbar")
	addDrive(t, directory, name, "signout_test.go.txt",
		"signout_browser_test.go")
	runTaggedTests(t, directory, "browser", "./internal/"+name+"/",
		"TestSignOutWorksWithScriptsOff")
}

// addDrive copies testdata/<fixture> beside the app's code as file,
// and with it testdata/settle_test.go.txt, the polling helper the
// drives share, with the app's test package, <name>_test, in its clause.
func addDrive(t *testing.T, directory, name, fixture, file string) {
	t.Helper()
	appDirectory := filepath.Join(directory, "internal", name)
	for source, target := range map[string]string{
		fixture: file, "settle_test.go.txt": "settle_browser_test.go",
	} {
		content, err := os.ReadFile(filepath.Join("testdata", source))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(content), "\npackage drivetest\n",
			"\npackage "+name+"_test\n", 1)
		write(t, filepath.Join(appDirectory, target), text)
	}
}

// menuPage is the upgraded app's home page with a dropdown on it.
const menuPage = `{{define "content"}}<h1>Upgraded</h1>
{{template "dropdown" dict "Label" "Sort" "Items" (list
  (dict "Href" "/" "Label" "Newest" "Current" true)
  (dict "Href" "/" "Label" "Oldest"))}}
{{end}}
`

// An app from before fiesta.js, upgraded as the changelog says: doctor
// --fix, then the layout line it names. A menu left with Escape closes
// and gives focus back to its summary, which is fiesta.js's to do now
// that rastrillo.js is the slim shim, at the root and under a base path
// (testdata/escape_test.go.txt).
func TestAnUpgradedOldAppClosesMenusOnEscape(t *testing.T) {
	t.Parallel()
	harness.ChromePath(t)
	directory, _ := olderApp(t, "upgradedapp")
	// A page menu to leave with Escape. The old layout's scheme
	// control is icon buttons now, not a menu, and the drive hung
	// waiting for one; with one language it has no other.
	write(t, filepath.Join(directory, "internal", "upgradedapp",
		"templates", "index.html"), menuPage)
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
	write(t, filepath.Join(directory, "internal", "upgradedapp",
		"escape_extra_test.go"), string(drive))
	runTaggedTests(t, directory, "browser", "./internal/upgradedapp/",
		"TestAMenuClosesOnEscape", "TestAMenuClosesOnEscapeUnderABasePath")
}
