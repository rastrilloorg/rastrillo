package chart_test

import (
	"html/template"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/de"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	"amadan.net/rastrillo/rastrillo/ui"
)

// partials is ui's partials as an app's own template tree holds them.
func partials(t *testing.T) *template.Template {
	t.Helper()
	set, err := template.New("").Funcs(ui.Funcs()).
		ParseFS(ui.Templates(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// localizerFor is a page's localizer in code, with the framework's own
// catalogs.
func localizerFor(t *testing.T, code string) *ui.Localizer {
	t.Helper()
	catalogs, err := rastrillo.NewLocales([]string{code}, code,
		rastrillo.BaseCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return ui.NewLocalizer(code, code,
		func(key string, args ...any) string {
			return catalogs.Tf(code, key, args...)
		})
}

// renderIn renders partial with data, given the localizer for code as
// its L.
func renderIn(t *testing.T, code, partial string, data map[string]any) string {
	t.Helper()
	with := map[string]any{"L": localizerFor(t, code)}
	for key, value := range data {
		with[key] = value
	}
	var out strings.Builder
	if err := partials(t).ExecuteTemplate(&out, partial, with); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// renderError is the error rendering partial with data stops on.
func renderError(t *testing.T, partial string, data map[string]any) error {
	t.Helper()
	with := map[string]any{"L": localizerFor(t, "en")}
	for key, value := range data {
		with[key] = value
	}
	return partials(t).ExecuteTemplate(&strings.Builder{}, partial, with)
}
