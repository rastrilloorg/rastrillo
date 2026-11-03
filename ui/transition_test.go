package ui

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/nodetest"
)

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
	for name, script := range map[string][]byte{
		"source": source, "minified": minified,
	} {
		t.Run(name, func(t *testing.T) {
			nodetest.Run(t, nodetest.Cmd{
				Args:  []string{"shell_transition_node.mjs", "observer"},
				Stdin: script,
			})
		})
	}
}
