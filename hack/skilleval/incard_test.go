package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSchemeInCardReadsTheHTML holds the cases a line pattern got
// wrong: a closing tag with a space before its >, an opening tag across
// lines, a details nested in the card, and a control after the card.
func TestSchemeInCardReadsTheHTML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, template string
		want           bool
	}{
		{"the shipped topbar's card", topbarLayout, true},
		{"the sidebar's profile card on one line", sidebarLayout, true},
		{"a card opened across lines", multiLineCard, true},
		{"a footer after a spaced end tag", spacedEndTag, false},
		{"a footer after a card holding a details", schemeOutsideCard,
			false},
		{"after a details nested in the card", `<details rst-shell-account>
<details name="rst-menus"><summary>Language</summary></details>
{{template "scheme-menu" .}}
</details>`, true},
		{"a hand-written [rst-scheme] in the card", `<details
rst-shell-profile><div rst-scheme="icons"></div></details>`, true},
		{"the call only in a template comment", `<details rst-shell-account>
{{/* {{template "scheme-menu" .}} */}}</details>`, false},
		{"the call only in an HTML comment", `<details rst-shell-account>
<!-- {{template "scheme-menu" .}} --></details>`, false},
		{"a card name only as a value", `<details name="rst-shell-account">
{{template "scheme-menu" .}}</details>`, false},
		{"an action's quotes inside an attribute", `<details rst-shell-account
data-up="{{path $ "/menu"}}">{{- template "scheme-menu" .}}</details>`,
			true},
		{"a void element in the card", `<details rst-shell-account><input
name="q"><br>{{template "scheme-menu" .}}</details>`, true},
	}
	for _, tc := range cases {
		if got := schemeInCard(tc.template); got != tc.want {
			t.Errorf("%s: schemeInCard = %v, want %v", tc.name, got,
				tc.want)
		}
	}
}

// TestSchemeInCardUnderSkipsGen: generated templates are not the app's
// own, and an .htm template is read like an .html one.
func TestSchemeInCardUnderSkipsGen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(path, body string) {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/gen/templates/layout.html", topbarLayout)
	if found, err := schemeInCardUnder(root); err != nil || found {
		t.Fatalf("gen only: found %v, err %v; want false", found, err)
	}
	write("app/templates/layout.htm", topbarLayout)
	if found, err := schemeInCardUnder(root); err != nil || !found {
		t.Fatalf("an .htm layout: found %v, err %v; want true", found, err)
	}
}
