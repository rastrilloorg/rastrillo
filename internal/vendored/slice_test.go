package vendored_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"maps"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/ui"
)

func TestSliceKeepsTheRulesAPageCanShow(t *testing.T) {
	media := `{[rst-a]{a:b}[rst-b]{c:d}}`
	cases := []struct {
		stylesheet string
		hooks      []string
		want       string
	}{
		{`[rst-btn]{a:b}`, []string{"rst-btn"}, `[rst-btn]{a:b}`},
		{`[rst-btn]{a:b}`, nil, ``},
		{`[rst-a],[rst-b]{a:b}`, []string{"rst-b"}, `[rst-a],[rst-b]{a:b}`},
		{`[rst-a] [rst-b]{a:b}`, []string{"rst-a"}, ``},
		{`[rst-a] [rst-b]{a:b}`, []string{"rst-a", "rst-b"},
			`[rst-a] [rst-b]{a:b}`},
		{`[rst-btn=primary]{a:b}`, []string{"rst-btn"},
			`[rst-btn=primary]{a:b}`},
		{`.rst-btn:hover{a:b}`, []string{"rst-btn"}, `.rst-btn:hover{a:b}`},
		{`.rst-switch__track{a:b}`, nil, ``},
		{`.rst-switch__track{a:b}`, []string{"rst-switch__track"},
			`.rst-switch__track{a:b}`},
		{`:is([rst-a],[rst-b]) p{a:b}`, []string{"rst-a"},
			`:is([rst-a],[rst-b]) p{a:b}`},
		{`:is([rst-a],[rst-b]) p{a:b}`, nil, ``},
		{`:where([rst-a],[rst-b]) p{a:b}`, []string{"rst-a"},
			`:where([rst-a],[rst-b]) p{a:b}`},
		{`p:has([rst-a],[rst-b]){a:b}`, []string{"rst-b"},
			`p:has([rst-a],[rst-b]){a:b}`},
		{`p:not([rst-a]){a:b}`, nil, `p:not([rst-a]){a:b}`},
		{`[rst-a]:not([rst-b]){a:b}`, []string{"rst-a"},
			`[rst-a]:not([rst-b]){a:b}`},
		{`:root{--x:1}`, nil, `:root{--x:1}`},
		{`html{a:b}`, nil, `html{a:b}`},
		{`@property --rst-press{syntax:"*"}`, nil,
			`@property --rst-press{syntax:"*"}`},
		{`@keyframes rst-spin{to{a:b}}`, nil, `@keyframes rst-spin{to{a:b}}`},
		{`@font-face{a:b}`, nil, `@font-face{a:b}`},
		{`@counter-style x{system:cyclic}`, nil,
			`@counter-style x{system:cyclic}`},
		{`@page{margin:0}`, nil, `@page{margin:0}`},
		{`@media (min-width:40rem)` + media, []string{"rst-a"},
			`@media (min-width:40rem){[rst-a]{a:b}}`},
		{`@media (min-width:40rem)` + media, nil, ``},
		{`@supports (anchor-name:--a)` + media, []string{"rst-a"},
			`@supports (anchor-name:--a){[rst-a]{a:b}}`},
		{`@supports (anchor-name:--a)` + media, nil, ``},
		{`@layer x` + media, []string{"rst-a"}, `@layer x{[rst-a]{a:b}}`},
		{`@layer x` + media, nil, `@layer x;`},
		{`@layer{[rst-a]{a:b}}`, nil, ``},
		{`@layer a{[rst-x]{a:b}}@layer b{p{c:d}}@layer a{p{e:f}}`, nil,
			`@layer a;@layer b{p{c:d}}@layer a{p{e:f}}`},
		{`@layer a.b {[rst-x]{a:b}p{c:d}}`, nil, `@layer a.b {p{c:d}}`},
		{`@layer a.b {[rst-x]{a:b}}`, nil, `@layer a.b;`},
		{`@media print{@layer a{[rst-x]{a:b}}}`, nil,
			`@media print{@layer a;}`},
		{`@container x (min-width:1px)` + media, []string{"rst-a"},
			`@container x (min-width:1px){[rst-a]{a:b}}`},
		{`@container x (min-width:1px)` + media, nil, ``},
		{`@scope ([rst-card])` + media, []string{"rst-a"},
			`@scope ([rst-card]){[rst-a]{a:b}}`},
		{`[rst-a]{content:"}"}`, []string{"rst-a"}, `[rst-a]{content:"}"}`},
		{`[rst-a]{content:"\7d"}`, []string{"rst-a"}, `[rst-a]{content:"\7d"}`},
		{`[rst-a]{content:"}"}[rst-b]{a:b}`, []string{"rst-a"},
			`[rst-a]{content:"}"}`},
		{`:root{--x:1}[rst-a]{a:b}[rst-b]{c:d}`, []string{"rst-b"},
			`:root{--x:1}[rst-b]{c:d}`},
	}
	for _, test := range cases {
		hooks := map[string]bool{}
		for _, hook := range test.hooks {
			hooks[hook] = true
		}
		sliced, err := vendored.Slice([]byte(test.stylesheet), hooks)
		if err != nil {
			t.Errorf("%s with %v: %v", test.stylesheet, test.hooks, err)
			continue
		}
		if string(sliced) != test.want {
			t.Errorf("%s with %v: got %q, want %q",
				test.stylesheet, test.hooks, sliced, test.want)
		}
	}
}

func TestSliceRefusesWhatItCannotRead(t *testing.T) {
	unreadable := []string{
		`[rst-a]{a:b`,
		`@unknown-thing x{[rst-a]{a:b}}`,
		`@media (x){[rst-a]{a:b}`,
		`p{a:(]}[rst-a]{b:c}`,
	}
	for _, stylesheet := range unreadable {
		_, err := vendored.Slice([]byte(stylesheet), nil)
		if !errors.Is(err, vendored.ErrUnreadable) {
			t.Errorf("Slice(%s): got %v, want ErrUnreadable", stylesheet, err)
		}
		_, err = vendored.StyledHooks([]byte(stylesheet))
		if !errors.Is(err, vendored.ErrUnreadable) {
			t.Errorf("StyledHooks(%s): got %v, want ErrUnreadable",
				stylesheet, err)
		}
	}
}

func TestStyledHooksNamesEveryHookInASelector(t *testing.T) {
	hooks, err := vendored.StyledHooks([]byte(`[rst-a] .rst-b,[rst-c=x]{}`))
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(hooks))
	want := []string{"rst-a", "rst-b", "rst-c"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestSliceOfTheShippedTokensCSS holds the slicer to the stylesheet it
// exists for. Every hook keeps the file byte for byte, which catches a
// reader that misplaces a rule's edges, and the named hooks being
// non-empty catches one that silently reads nothing.
func TestSliceOfTheShippedTokensCSS(t *testing.T) {
	minified, err := vendored.Minify("tokens.css", ui.TokensCSS())
	if err != nil {
		t.Fatal(err)
	}
	sliced, err := vendored.Slice(minified, nil)
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	if _, err := zipper.Write(sliced); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	if compressed.Len() >= 2000 {
		t.Errorf("hookless slice is %d bytes gzipped, want under 2000",
			compressed.Len())
	}
	hooks, err := vendored.StyledHooks(minified)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) == 0 {
		t.Fatal("tokens.css styles no hooks")
	}
	whole, err := vendored.Slice(minified, hooks)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(whole, minified) {
		t.Errorf("every hook kept %d of %d bytes", len(whole), len(minified))
	}
}
