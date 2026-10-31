//go:build browser

package scaffoldcheck

import (
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
