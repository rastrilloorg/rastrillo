package vendored_test

import (
	"slices"
	"testing"

	"amadan.net/rastrillo/fiesta"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

func TestDataCSSIsAVendoredComponentStylesheet(t *testing.T) {
	sheets := vendored.ComponentStylesheets()
	if !slices.Equal(sheets, []string{"data.css"}) {
		t.Fatalf("component stylesheets are %q, want data.css", sheets)
	}
	if !slices.Contains(vendored.Names(), "data.css") {
		t.Error("data.css is not vendored, so no app has it to link")
	}
}

// A page asks for data.css by showing any hook it styles, in either
// spelling: the table's, a chart's or a sparkline's.
func TestEveryHookDataCSSStylesAsksForIt(t *testing.T) {
	styled, err := vendored.StyledHooks(fiesta.DataCSS())
	if err != nil {
		t.Fatal(err)
	}
	for hook := range styled {
		if got := vendored.NeededStylesheets([]string{hook}); !slices.Equal(
			got, []string{"data.css"}) {
			t.Errorf("%s asks for %q, want data.css", hook, got)
		}
	}
	for _, hook := range []string{"rst-card", "rst-stat", "rst-lgrid"} {
		if got := vendored.NeededStylesheets([]string{hook}); len(got) != 0 {
			t.Errorf("%s, which tokens.css styles, asks for %q", hook, got)
		}
	}
}
