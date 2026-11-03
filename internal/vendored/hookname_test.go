package vendored_test

import (
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

func TestIsHookTakesOnlyAWholeName(t *testing.T) {
	cases := map[string]bool{
		"rst-btn":           true,
		"rst-switch__track": true,
		"rst-":              false,
		"data-rst-select":   false,
		"--rst-press":       false,
		"rst-Btn":           false,
		"rst-a.rst-b":       false,
	}
	for name, want := range cases {
		if got := vendored.IsHook(name); got != want {
			t.Errorf("IsHook(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestHooksInReadsWholeNames(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		// A hook in a class attribute is the case here.
		// markup-spelling: old-spelling begin
		{`class="rst-b__c rst-a rst-a"`, []string{"rst-a", "rst-b__c"}},
		// markup-spelling: old-spelling end
		{`<div data-rst-select>`, nil},
		{`el.matches("rst-a.rst-b")`, []string{"rst-a", "rst-b"}},
		{`var(--rst-press)`, nil},
		{`[rst-switch__track]`, []string{"rst-switch__track"}},
	}
	for _, test := range cases {
		got := vendored.HooksIn([]byte(test.text))
		if !slices.Equal(got, test.want) {
			t.Errorf("HooksIn(%s) = %v, want %v", test.text, got, test.want)
		}
	}
}
