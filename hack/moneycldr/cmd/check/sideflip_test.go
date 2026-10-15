package main

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
)

func TestMovesSymbol(t *testing.T) {
	for _, c := range []struct {
		standard, alpha string
		want            bool
	}{
		{"¤#,##0.00", "¤#,##0.00", false},
		{"¤#,##0.00", "¤ #,##0.00", false},            // same side, a space added
		{"#,##0.00 ¤", "¤ #,##0.00", true},            // after to before
		{"¤#,##0.00", "#,##0.00 ¤", true},             // before to after
		{"¤#,##0.00;(¤#,##0.00)", "#,##0.00 ¤", true}, // the positive subpattern decides
		{"#,##0.00 ¤", "#,##0.00 ¤;-#,##0.00 ¤", false},
	} {
		if got := movesSymbol(locales.Locale{Standard: c.standard, Alpha: c.alpha}); got != c.want {
			t.Errorf("movesSymbol(%q, %q) = %v, want %v", c.standard, c.alpha, got, c.want)
		}
	}
}

// The spec's claim, checked over the data apps link: no modern locale's
// alphaNextToNumber pattern moves the symbol. cmd/check fails on the
// same finding with op.check.side_flip; this test says so offline.
func TestNoModernLocaleMovesTheSymbol(t *testing.T) {
	if n := len(locales.LinkedIDs()); n < 400 {
		t.Fatalf("only %d linked ids: the modern pack is not linked, so the check would see nothing", n)
	}
	if got := sideFlips(); len(got) != 0 {
		t.Errorf(sideFlipMsg, got)
	}
}
