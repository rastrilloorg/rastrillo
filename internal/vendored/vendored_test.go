package vendored_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/ui"
)

func TestMinifyDropsCommentsAndWhitespace(t *testing.T) {
	cases := []struct{ name, body, comment string }{
		{"tokens.css", "/* why */\nbody {\n  color: red;\n}\n", "why"},
		{"select.js", "// line note\nfunction add(a, b) {\n" +
			"  /* block note */\n  return a + b;\n}\n", "note"},
	}
	for _, test := range cases {
		minified, err := vendored.Minify(test.name, []byte(test.body))
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if bytes.Contains(minified, []byte(test.comment)) {
			t.Errorf("%s kept its comment: %q", test.name, minified)
		}
		if len(minified) >= len(test.body) {
			t.Errorf("%s is no shorter: %q", test.name, minified)
		}
	}
}

func TestMinifyKeepsRegexAndTemplateLiterals(t *testing.T) {
	kept := []string{`/\/\*kept\*\//g`, "`a /* b */ ${x} // c`", `"http://x"`}
	body := "var r = " + kept[0] + ";\nvar s = " + kept[1] +
		";\nvar u = " + kept[2] + ";\nconsole.log(r, s, u);\n"
	minified, err := vendored.Minify("x.js", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range kept {
		if !bytes.Contains(minified, []byte(literal)) {
			t.Errorf("lost %s: %q", literal, minified)
		}
	}
}

func TestMinifyKeepsLocalNames(t *testing.T) {
	body := "function f(){ var longName = 1; return longName }\nf();\n"
	minified, err := vendored.Minify("x.js", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(minified, []byte("longName")) {
		t.Errorf("renamed a local: %q", minified)
	}
}

func TestMinifyLeavesOtherFilesAlone(t *testing.T) {
	for _, name := range []string{"LICENSE", "x.svg"} {
		body := []byte("<!-- keep -->\n  spaced  \n")
		minified, err := vendored.Minify(name, body)
		if err != nil || !bytes.Equal(minified, body) {
			t.Errorf("%s: got %q, %v; want it unchanged", name, minified, err)
		}
	}
}

func TestMinifyRefusesBrokenJS(t *testing.T) {
	_, err := vendored.Minify("select.js", []byte("function ("))
	if err == nil {
		t.Error("broken JavaScript minified without an error")
	}
}

func TestIsMatchesOnlyTheScaffoldsPlace(t *testing.T) {
	cases := map[string]bool{
		"static/tokens.css":      true,
		"static/theme.css":       true,
		"tokens.css":             false,
		"static/admin/select.js": false,
		"static/app.css":         false,
	}
	for name, want := range cases {
		if got := vendored.Is(name); got != want {
			t.Errorf("Is(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestEveryShippedFileMinifies(t *testing.T) {
	for _, theme := range ui.ThemeNames() {
		files, ok := ui.VendoredAssets(theme)
		if !ok {
			t.Fatalf("theme %s is not shipped", theme)
		}
		for name, body := range files {
			minified, err := vendored.Minify(name, body)
			switch {
			case err != nil:
				t.Errorf("%s (%s): %v", name, theme, err)
			case len(minified) == 0 || len(minified) >= len(body):
				t.Errorf("%s (%s): %d bytes from %d", name, theme,
					len(minified), len(body))
			}
		}
	}
}

func TestComponentScriptsAreVendored(t *testing.T) {
	scripts := vendored.ComponentScripts()
	if len(scripts) == 0 {
		t.Fatal("no component scripts")
	}
	for _, name := range scripts {
		if !slices.Contains(vendored.Names(), name) ||
			!strings.HasSuffix(name, ".js") {
			t.Errorf("%s is not a vendored script", name)
		}
	}
}
