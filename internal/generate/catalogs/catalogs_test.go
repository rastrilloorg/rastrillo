package catalogs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// Catalog keys are bare: internal/catalog's flat format refuses a
// quoted key, so a quoted fixture would fail to decode, not to match.
func TestCheck(t *testing.T) {
	for name, c := range map[string]struct {
		app   manifest.App
		files map[string]string // path under the app -> content
		want  string            // substring of the joined problems; "" means none
	}{
		"one locale, no app strings":         {manifest.App{Locales: []string{"en"}, DefaultLocale: "en"}, nil, ""},
		"framework language, no app strings": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"}, nil, ""},
		"framework language, app strings, no file": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\n"}, "locales/es.toml"},
		// The framework's Portuguese ships as pt-PT; an app that offered
		// pt before the retag still gets it, so it needs no file.
		"retagged pt still covered": {manifest.App{Locales: []string{"en", "pt"}, DefaultLocale: "en"}, nil, ""},
		// And an app that moved its manifest to pt-PT keeps reading the
		// pt.toml it already has (NewLocales layers it under pt-PT).
		"pt-PT served from an old pt.toml": {manifest.App{Locales: []string{"en", "pt-PT"}, DefaultLocale: "en"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\n", "locales/pt.toml": "resource.x.name = \"Coisas\"\n"}, ""},
		"Portuguese default served from an old pt.toml": {manifest.App{Locales: []string{"pt-PT", "en"}, DefaultLocale: "pt-PT"},
			map[string]string{"locales/pt.toml": "a = \"A\"\n", "locales/en.toml": "a = \"A\"\n"}, ""},
		"language the framework lacks": {manifest.App{Locales: []string{"en", "de"}, DefaultLocale: "en"}, nil, "locales/de.toml"},
		"Spanish default, file missing": {manifest.App{Locales: []string{"es", "en"}, DefaultLocale: "es"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\n"}, "locales/es.toml"},
		"Spanish default, complete": {manifest.App{Locales: []string{"es", "en"}, DefaultLocale: "es"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\n", "locales/es.toml": "resource.x.name = \"Cosas\"\n"}, ""},
		"manifest app adds Spanish, complete": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\n", "locales/es.toml": "resource.x.name = \"Cosas\"\n"}, ""},
		"manifest app adds Spanish, behind": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\nresource.x.empty = \"None\"\n", "locales/es.toml": "resource.x.name = \"Cosas\"\n"}, "resource.x.empty"},
		"hand and generated English are both the reference": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"},
			map[string]string{"gen/locales/en.toml": "resource.x.name = \"Things\"\n", "locales/en.toml": "a = \"A\"\n", "locales/es.toml": "a = \"A\"\n"}, "resource.x.name"},
		"Spanish default, en catalog behind": {manifest.App{Locales: []string{"es", "en"}, DefaultLocale: "es"},
			map[string]string{"locales/es.toml": "a = \"A\"\nb = \"B\"\n", "locales/en.toml": "a = \"A\"\n"}, "b"},
	} {
		dir := t.TempDir()
		for path, content := range c.files {
			os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755)
			os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644)
		}
		problems, err := Check(dir, c.app)
		got := strings.Join(problems, "\n")
		if err != nil || (c.want == "") != (got == "") || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, %v; want %q", name, got, err, c.want)
		}
	}
}
