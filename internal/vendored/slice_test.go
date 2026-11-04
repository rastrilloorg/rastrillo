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
		// The minifier writes a container query's block as it found
		// it, so the block's last newline is kept with its brace.
		{"@container (max-width: 34rem) {\n  [rst-a] { a: b; }\n}",
			[]string{"rst-a"},
			"@container (max-width: 34rem) {\n  [rst-a] { a: b; }\n}"},
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

// A page whose templates write a hook's every value as a literal shows
// only those values, so a rule for a variant it never writes goes. A
// hook with no value written down, because a template writes it from
// data, keeps every variant's rule.
func TestSliceKeepsOnlyTheVariantsAPageShows(t *testing.T) {
	variants := `[rst-btn]{a:b}[rst-btn~=primary]{c:d}` +
		`[rst-btn~=ghost]{e:f}`
	primary := vendored.ShownValue("rst-btn", "primary")
	bare := vendored.ShownValue("rst-btn", "")
	cases := []struct {
		name       string
		stylesheet string
		hooks      []string
		want       string
	}{
		{"one variant shown", variants, []string{"rst-btn", primary},
			`[rst-btn]{a:b}[rst-btn~=primary]{c:d}`},
		{"a value written from data", variants, []string{"rst-btn"},
			variants},
		{"only the bare attribute", variants, []string{"rst-btn", bare},
			`[rst-btn]{a:b}`},
		{"a variant the rule excludes",
			`[rst-btn]:not([rst-btn~=ghost]){a:b}`,
			[]string{"rst-btn", primary},
			`[rst-btn]:not([rst-btn~=ghost]){a:b}`},
		{"one branch of a list", `[rst-btn~=ghost],[rst-btn~=primary]{a:b}`,
			[]string{"rst-btn", primary},
			`[rst-btn~=ghost],[rst-btn~=primary]{a:b}`},
		{"inside :is()", `:is([rst-btn~=ghost]) p{a:b}`,
			[]string{"rst-btn", primary}, ``},
		{"a quoted value", `[rst-btn~="primary"]{a:b}`,
			[]string{"rst-btn", primary}, `[rst-btn~="primary"]{a:b}`},
		{"one of several words",
			`[rst-btn~=sm]{a:b}[rst-btn~=lg]{c:d}`,
			[]string{"rst-btn", vendored.ShownValue("rst-btn", "ghost sm")},
			`[rst-btn~=sm]{a:b}`},
		{"exact", `[rst-btn=primary]{a:b}[rst-btn="ghost sm"]{c:d}`,
			[]string{"rst-btn", vendored.ShownValue("rst-btn", "ghost sm")},
			`[rst-btn="ghost sm"]{c:d}`},
		{"exact, any case", `[rst-btn=PRIMARY i]{a:b}`,
			[]string{"rst-btn", primary}, `[rst-btn=PRIMARY i]{a:b}`},
		{"dash", `[rst-x|=a]{a:b}[rst-x|=b]{c:d}`,
			[]string{"rst-x", vendored.ShownValue("rst-x", "a-1")},
			`[rst-x|=a]{a:b}`},
		{"prefix, suffix and substring",
			`[rst-x^=ab]{a:b}[rst-x$=yz]{c:d}[rst-x*=mn]{e:f}` +
				`[rst-x^=yz]{g:h}`,
			[]string{"rst-x", vendored.ShownValue("rst-x", "abmnyz")},
			`[rst-x^=ab]{a:b}[rst-x$=yz]{c:d}[rst-x*=mn]{e:f}`},
		{"another hook's value", `[rst-a~=x]{a:b}`,
			[]string{"rst-a", vendored.ShownValue("rst-b", "y")},
			`[rst-a~=x]{a:b}`},
		{"the hook not shown at all", `[rst-btn~=primary]{a:b}`,
			[]string{primary}, ``},
	}
	for _, test := range cases {
		hooks := map[string]bool{}
		for _, hook := range test.hooks {
			hooks[hook] = true
		}
		sliced, err := vendored.Slice([]byte(test.stylesheet), hooks)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if string(sliced) != test.want {
			t.Errorf("%s: %s with %q: got %q, want %q", test.name,
				test.stylesheet, test.hooks, sliced, test.want)
		}
	}
}

// A page whose classes are listed cannot wear any other hook as a
// class, so a kept rule loses the selectors it cannot match, and keeps
// the rest as written. A page whose classes are not listed may wear
// any, so it keeps every selector of a rule it keeps.
func TestSliceDropsTheSelectorsAPageCannotMatch(t *testing.T) {
	listed := vendored.ListedClasses
	mono := vendored.ShownClass("rst-mono")
	button := vendored.ShownClass("rst-btn")
	pair := `.rst-btn,[rst-btn]{a:b}`
	cases := []struct {
		name       string
		stylesheet string
		hooks      []string
		want       string
	}{
		{"the attribute only", pair, []string{"rst-btn", listed},
			`[rst-btn]{a:b}`},
		{"the class only", pair, []string{"rst-btn", listed, button},
			pair},
		{"classes not listed", pair, []string{"rst-btn"}, pair},
		{"a class written", `.rst-mono{a:b}`,
			[]string{"rst-mono", listed, mono}, `.rst-mono{a:b}`},
		{"a class not written", `.rst-mono{a:b}`,
			[]string{"rst-mono", listed}, ``},
		{"a part never named", `.rst-page__foot,[rst-page-foot]{a:b}`,
			[]string{"rst-page-foot", listed}, `[rst-page-foot]{a:b}`},
		{"a variant never named",
			`.rst-btn--primary,[rst-btn~="primary"]{a:b}`,
			[]string{"rst-btn", listed}, `[rst-btn~="primary"]{a:b}`},
		{"a variant not shown", `[rst-btn~=ghost],[rst-btn~=primary]{a:b}`,
			[]string{"rst-btn", listed,
				vendored.ShownValue("rst-btn", "primary")},
			`[rst-btn~=primary]{a:b}`},
		{"the last selector kept", `[rst-a],[rst-b]{a:b}`,
			[]string{"rst-a", listed}, `[rst-a]{a:b}`},
		{"the middle selector dropped", `[rst-a],.rst-b,html{a:b}`,
			[]string{"rst-a", "rst-b", listed}, `[rst-a],html{a:b}`},
		{"a selector with no hook", `.rst-btn,::selection{a:b}`,
			[]string{"rst-btn", listed}, `::selection{a:b}`},
		{"commas inside a selector",
			`.rst-a:is(p,q),[rst-a]:is(p,q),[x=","]{a:b}`,
			[]string{"rst-a", listed}, `[rst-a]:is(p,q),[x=","]{a:b}`},
		{"spaced", "p {}\n.rst-a , [rst-a] > p {a:b}",
			[]string{"rst-a", listed}, "p {}\n[rst-a] > p{a:b}"},
		{"inside a group", `@media (x){.rst-a,[rst-a]{a:b}}`,
			[]string{"rst-a", listed}, `@media (x){[rst-a]{a:b}}`},
		{"nested rules come along",
			`.rst-a,[rst-a]{a:b;&:hover{c:d}}`,
			[]string{"rst-a", listed}, `[rst-a]{a:b;&:hover{c:d}}`},
	}
	for _, test := range cases {
		hooks := map[string]bool{}
		for _, hook := range test.hooks {
			hooks[hook] = true
		}
		sliced, err := vendored.Slice([]byte(test.stylesheet), hooks)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if string(sliced) != test.want {
			t.Errorf("%s: %s with %q: got %q, want %q", test.name,
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
