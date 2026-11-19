package cspcheck

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// The style guide's samples are the markup agents copy, so they write
// nothing the default CSP drops: a grid card's columns come from a
// class in the app's stylesheet, never a style attribute.
func TestStyleguideSamplesWriteNothingTheCSPDrops(t *testing.T) {
	for name, sample := range ui.Styleguide() {
		for _, finding := range Inspect(name+".html", []byte(sample)) {
			t.Errorf("the %s sample, line %d: %s", name, finding.Line,
				finding.Problem)
		}
	}
}
