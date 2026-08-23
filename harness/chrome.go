//go:build browser

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ChromePath finds the Chromium a browser-tagged test drives:
// RASTRILLO_CHROME first, then the usual PATH names, then the
// Playwright cache. Moved here verbatim from ui/browser_test.go so
// every browser test shares one discovery story.
//
// A skip is not a pass: with no browser this fails, unless
// RASTRILLO_BROWSER_OPTIONAL is set, which makes the skip a deliberate
// visible choice rather than an accident.
func ChromePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("RASTRILLO_CHROME"); p != "" {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		hits, _ := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux64", "chrome"))
		if len(hits) > 0 {
			return hits[len(hits)-1]
		}
	}
	if os.Getenv("RASTRILLO_BROWSER_OPTIONAL") != "" {
		t.Skip("no chromium found, RASTRILLO_BROWSER_OPTIONAL set — SKIPPED, not passed")
	}
	t.Fatal("no chromium found: set RASTRILLO_CHROME, or RASTRILLO_BROWSER_OPTIONAL to skip deliberately")
	return ""
}
