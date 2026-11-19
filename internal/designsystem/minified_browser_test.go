//go:build browser

package designsystem

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// TestComponentsEnhanceFromTheMinifiedFiles drives a select and a date
// field on the framework's files as apps now serve them, minified, so
// a minifier that changed what a script does fails here and not in an
// app.
func TestComponentsEnhanceFromTheMinifiedFiles(t *testing.T) {
	served := httptest.NewRecorder()
	treeHandler(t).ServeHTTP(served,
		httptest.NewRequest(http.MethodGet, assetHref("tokens.css"), nil))
	tokens, _ := io.ReadAll(served.Body)
	if strings.Contains(string(tokens), "/*") {
		t.Fatal("the tree serves tokens.css with its comments")
	}
	selectDoc := previewCarrying(t, "data-rst-select ")
	dateDoc := previewCarrying(t, "data-rst-date")
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	checks := []struct{ doc, ready string }{
		{selectDoc, `!!document.querySelector("[rst-combo]")`},
		{dateDoc, `!!document.querySelector("[rst-dtp-pick]") &&
			typeof window.fiestaCalendar === "function"`},
	}
	for _, check := range checks {
		err := chromedp.Do(ctx, chromedp.Navigate(rig.Origin+check.doc))
		if err != nil {
			t.Fatalf("loading %s: %v", check.doc, err)
		}
		until(t, ctx, check.doc, check.ready)
		rig.Screen("body", check.doc)
	}
}

// previewCarrying is the address of the first day/en preview document
// whose markup contains marker.
func previewCarrying(t *testing.T, marker string) string {
	t.Helper()
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, kind := range componentPages() {
		href := pageHref(mountPath, "day", "en", kind.File)
		page := string(files[strings.TrimPrefix(href, mountPrefix)])
		for _, frame := range frameDocs(t, files, page) {
			if strings.Contains(frame.Doc, marker) {
				return frame.Src
			}
		}
	}
	t.Fatalf("no preview carries %q", marker)
	return ""
}
