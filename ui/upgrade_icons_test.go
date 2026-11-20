package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"html/template"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// oldAppIcons is the icon map of an app scaffolded before 0.38, read
// out of testdata/icons-0.37/icons.go, the file rastrillo new wrote
// then. It has none of the slugs the new controls draw: sun, moon,
// monitor, chevron-left.
func oldAppIcons(t testing.TB) map[string]template.HTML {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(),
		"testdata/icons-0.37/icons.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]template.HTML{}
	ast.Inspect(file, func(node ast.Node) bool {
		pair, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, keyOK := pair.Key.(*ast.BasicLit)
		value, valueOK := pair.Value.(*ast.BasicLit)
		if keyOK && valueOK {
			slug, _ := strconv.Unquote(key.Value)
			svg, _ := strconv.Unquote(value.Value)
			out[slug] = template.HTML(svg)
		}
		return true
	})
	if len(out) != 12 || out["sun"] != "" {
		t.Fatalf("the 0.37 icon map has %d slugs (sun %q); want the 12 "+
			"it shipped, without sun", len(out), out["sun"])
	}
	return out
}

// appPage renders layout through ui.Pages the way a scaffolded app's
// render.go does, with its own icon package in the icon seam.
func appPage(
	t testing.TB, layout []byte, icons map[string]template.HTML,
) string {
	return phoneShellPage(t, layout, "en",
		`{{define "up"}}/posts{{end}}{{define "upLabel"}}Posts{{end}}`,
		map[string]any{"Locales": threeLocales}, template.FuncMap{
			"icon": func(slug string) template.HTML {
				return icons[slug]
			},
			"iconAssets": func() template.HTML { return "" },
		})
}

var schemeButton = regexp.MustCompile(
	`<button type="submit" aria-label="([^"]*)"[^>]*>(.*?)</button>`)

// TestAnOldAppsIconsDrawTheNewControls: an app scaffolded before 0.38
// owns an icon package with no sun, moon, monitor or chevron-left, and
// rastrillo doctor does not rewrite it. Upgraded, with its 0.37 layout
// or the new one, its scheme control still shows three named buttons
// with a drawing in each, and the up link its chevron: a slug the app's
// map does not answer is drawn from the framework's own set. A slug it
// does answer stays the app's, so a custom icon still wins.
func TestAnOldAppsIconsDrawTheNewControls(t *testing.T) {
	custom := maps.Clone(oldAppIcons(t))
	custom["sun"] = `<svg class="icon" data-app-sun aria-hidden="true"></svg>`
	custom["chevron-down"] =
		`<svg class="icon" data-app-caret aria-hidden="true"></svg>`
	for _, upgrade := range []struct {
		name   string
		layout []byte
		icons  map[string]template.HTML
	}{
		{"0.37 topbar, 0.37 icons", oldLayout(t, "topbar"), oldAppIcons(t)},
		{"new topbar, 0.37 icons", shippedLayout(t, "topbar"), oldAppIcons(t)},
		{"0.37 topbar, custom icons", oldLayout(t, "topbar"), custom},
	} {
		out := appPage(t, upgrade.layout, upgrade.icons)
		_, control, _ := strings.Cut(out, `<div rst-scheme="icons"`)
		control, _, _ = strings.Cut(control, "</div>")
		buttons := schemeButton.FindAllStringSubmatch(control, -1)
		if len(buttons) != 3 {
			t.Errorf("%s: %d scheme buttons; want three",
				upgrade.name, len(buttons))
		}
		for _, button := range buttons {
			if button[1] == "" || !strings.Contains(button[2], "<svg") {
				t.Errorf("%s: the scheme button %q draws %q; want a name "+
					"and an icon", upgrade.name, button[1], button[2])
			}
		}
		if links := upLink.FindAllString(out, -1); len(links) == 1 &&
			!strings.Contains(links[0], "<svg") {
			t.Errorf("%s: the up link draws no chevron: %s",
				upgrade.name, links[0])
		}
		if upgrade.icons["sun"] != oldAppIcons(t)["sun"] {
			if !strings.Contains(control, "data-app-sun") {
				t.Errorf("%s: the app's own sun lost to the framework's",
					upgrade.name)
			}
			if !strings.Contains(out, "data-app-caret") {
				t.Errorf("%s: the app's own chevron-down lost to the "+
					"framework's", upgrade.name)
			}
		}
	}
}
