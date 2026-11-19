package scaffoldcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

// A new app on every shell writes nothing the default CSP drops, and
// make csp fails the first style attribute an agent adds, naming where.
// The browser drops it without an error, so nothing else in the gate
// would.
func TestCSPPassesANewAppAndFailsAnInlineStyle(t *testing.T) {
	t.Parallel()
	for _, variant := range everyShell() {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			directory := newApp(t, variant.name, variant.flags...)
			run(t, directory, rastrilloBinary, "csp")
			index := filepath.Join("internal", variant.name, "templates",
				"index.html")
			appendTo(t, filepath.Join(directory, index),
				"\n<p style=\"color: red\">red</p>\n")
			output, err := runIn(directory, rastrilloBinary, "csp")
			if err == nil {
				t.Fatalf("csp passed an inline style:\n%s", output)
			}
			want := filepath.ToSlash(index) + ":"
			if !strings.Contains(output, want) ||
				!strings.Contains(output, "a style attribute") {
				t.Errorf("csp does not name %s's style attribute:\n%s",
					index, output)
			}
		})
	}
}
