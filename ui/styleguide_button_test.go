package ui

import (
	"regexp"
	"strings"
	"testing"
)

// The button sample is the set that behaves differently, not every
// combination: four variants at the default size, small and large on
// primary, a link wearing the look, a full-width one and a disabled
// one. The busy state is form-foot's idle/working pair, so it is not
// drawn twice. A combination missing here is a size or variant a
// reader of the gallery never sees on a button.
func TestTheButtonSampleShowsEachDistinctBehaviour(t *testing.T) {
	sample, ok := Styleguide()["button"]
	if !ok {
		t.Fatal("Styleguide has no button sample")
	}
	for _, want := range []string{
		`<button rst-btn type="button">`,
		`<button rst-btn="primary" type="button">`,
		`<button rst-btn="ghost" type="button">`,
		`<button rst-btn="danger" type="button">`,
		`<button rst-btn="primary sm" type="button">`,
		`<button rst-btn="primary lg" type="button">`,
		`<a rst-btn href="`,
		`<button rst-btn="primary block" type="button">`,
		`<button rst-btn type="button" disabled>`,
	} {
		if !strings.Contains(sample, want) {
			t.Errorf("the button sample has no %s", want)
		}
	}
	if n := len(regexp.MustCompile(`\srst-btn[\s=>]`).FindAllString(sample, -1)); n != 9 {
		t.Errorf("the button sample draws %d buttons, want the 9 that behave differently", n)
	}
	if strings.Contains(sample, "aria-busy") || strings.Contains(sample, "rst-spin") {
		t.Error("the button sample draws the busy state, which form-foot's pair already shows")
	}
}
