package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// App is manifest/app.toml: the locale codes an app offers. They are
// identities (routing, the locale cookie, catalog file names), never
// pack names; the packs are derived from them (internal/generate/packs).
type App struct {
	Locales       []string `toml:"locales"`
	DefaultLocale string   `toml:"default_locale"`
}

// noLocales is the message for an app.toml that lists no locales. It
// is one constant so the copy review's approved text lands in one
// place; LoadApp prefixes it with the file's path.
const noLocales = `no locales are listed; add locales = ["en"], one code per language the app offers, or delete the file for an English-only app`

// LoadApp reads <dir>/app.toml strictly: an unknown key is an error
// naming it, so a misspelt default_locale fails instead of silently
// leaving the default at locales[0]. A missing file is an English-only
// app, the shape every app had before app.toml existed, so older apps
// generate unchanged.
func LoadApp(dir string) (App, error) {
	path := filepath.Join(dir, "app.toml")
	var app App
	md, err := toml.DecodeFile(path, &app)
	if os.IsNotExist(err) {
		return App{Locales: []string{"en"}, DefaultLocale: "en"}, nil
	}
	if err != nil {
		return App{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return App{}, fmt.Errorf("%s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	// A present file with no locales is a mistake, not an English-only
	// app: left alone it loads with an empty default, and the first
	// thing to notice is packs.render, whose message names neither the
	// file nor the fix.
	if len(app.Locales) == 0 {
		return App{}, fmt.Errorf("%s: %s", path, noLocales)
	}
	if app.DefaultLocale == "" {
		app.DefaultLocale = app.Locales[0]
	}
	return app, nil
}
