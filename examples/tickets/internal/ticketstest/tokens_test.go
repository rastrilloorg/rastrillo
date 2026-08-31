package ticketstest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
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

// tokens.css declares the colour tokens; a theme fills them in, so a
// vendored theme can fall behind exactly the way the tokens once did.
// Tickets is scaffolded with day — swap static/theme.css and this
// constant together.
func TestVendoredThemeCSSMatchesTheLibrary(t *testing.T) {
	const vendoredTheme = "day"
	lib, ok := ui.ThemeCSS(vendoredTheme)
	if !ok {
		t.Fatalf("ui.ThemeCSS(%q) reports no such theme", vendoredTheme)
	}
	vendored, err := os.ReadFile(filepath.Join("..", "..", "static", "theme.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vendored, lib) {
		t.Errorf("static/theme.css differs from ui.ThemeCSS(%q); re-copy the library file", vendoredTheme)
	}
}
