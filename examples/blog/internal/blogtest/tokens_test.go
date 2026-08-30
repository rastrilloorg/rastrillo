package blogtest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

// The app vendors the library stylesheet as static/tokens.css and
// serves it itself (rastrillo never serves CSS — F8). A vendored copy
// can silently fall behind the library — it did once, leaving the F10
// fix styled in the library and unstyled here — so pin the two
// byte-identical.
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
// The blog is scaffolded with day — swap static/theme.css and this
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

// The embedded static tree serves through the fingerprinting handler
// exactly as the old FileServerFS did for a bare name — /static/
// tokens.css resolves against the embedded paths, which carry the
// static/ prefix (F8) — it just adds no-cache so a changed file always
// shows on an ordinary reload.
func TestEmbeddedStaticServesTokensCSS(t *testing.T) {
	h, _ := newApp(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/tokens.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/tokens.css = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "--rst-bg") {
		t.Errorf("served tokens.css lacks the token block: %d bytes", rec.Body.Len())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache on a bare name", got)
	}
}

// Every rendered screen links its stylesheets by fingerprinted URL,
// and that URL is served cacheable-forever: with the platform's edge
// cache in front, a hibernating blog's assets never wake it.
func TestScreensLinkFingerprintedStylesheets(t *testing.T) {
	h, _ := newApp(t)
	body := get(t, h, "/").Body.String()
	href := regexp.MustCompile(`/static/tokens\.[0-9a-f]{16}\.css`).FindString(body)
	if href == "" {
		t.Fatalf("front page links no fingerprinted tokens.css:\n%s", body)
	}
	rec := get(t, h, href)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", href, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("GET %s: Cache-Control = %q, want immutable year", href, got)
	}
	if bare := regexp.MustCompile(`/static/blog\.[0-9a-f]{16}\.css`).FindString(body); bare == "" {
		t.Error("front page links no fingerprinted blog.css")
	}
}
