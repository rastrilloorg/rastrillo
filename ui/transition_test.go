package ui

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// transition.js loads before the stylesheets and without defer, so
// every byte of it holds up the first paint: it stays a few hundred
// bytes as an app is served it. What it does is Fiesta's to test.
func TestTransitionObserverIsVendoredBelow600MinifiedBytes(t *testing.T) {
	files, _ := VendoredAssets("day")
	source := files["transition.js"]
	if len(source) == 0 || !vendored.Is("static/transition.js") {
		t.Fatal("transition.js must be in the scaffold and minified asset set")
	}
	minified, err := vendored.Minify("transition.js", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(minified) >= 600 {
		t.Fatalf("transition.js is %d minified bytes, want <600", len(minified))
	}
	t.Logf("transition.js: %d minified bytes", len(minified))
}
