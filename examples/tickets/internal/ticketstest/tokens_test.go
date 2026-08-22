package ticketstest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

// The app vendors the library stylesheet as static/tokens.css and
// serves it itself (rastrillo never serves CSS). A vendored copy can
// silently fall behind the library — this one did, for months, which
// is exactly what an auditor meeting 999 lines of CSS as app diff
// cannot cheaply rule out — so pin the two byte-identical, the same
// pin examples/blog carries.
func TestVendoredTokensCSSMatchesTheLibrary(t *testing.T) {
	vendored, err := os.ReadFile(filepath.Join("..", "..", "static", "tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vendored, ui.TokensCSS()) {
		t.Error("static/tokens.css differs from ui.TokensCSS(); re-copy the library file")
	}
}
