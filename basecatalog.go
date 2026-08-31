package rastrillo

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

// baseFS carries the framework's own strings, one flat TOML catalog per
// shipped locale (design spec 2026-08-28 §3). Keys are namespaced
// rastrillo.ui.* so an app catalog can override any of them per locale
// without colliding with app keys.
//
// en.toml is the source of truth: every other file must hold exactly
// its key set (TestBaseCatalogsShareOneKeySet). ui's partials fall back
// to these keys via {{T "rastrillo.ui.*"}} — see ui/funcs.go's defaultT.
//
//go:embed locales/*.toml
var baseFS embed.FS

// baseLocales is the shipped set, en first, then spec §3.1's order:
// Ethnologue's L1 top ten, Irish, and Arabic for right-to-left coverage.
var baseLocales = []string{"en", "ga", "zh-Hans", "es", "hi", "pt", "bn", "ru", "ja", "yue", "vi", "ar"}

var baseCatalogs = func() map[string]Catalog {
	out := make(map[string]Catalog, len(baseLocales))
	for _, code := range baseLocales {
		data, err := fs.ReadFile(baseFS, "locales/"+code+".toml")
		if err != nil {
			panic(fmt.Sprintf("rastrillo: base catalog %s: %v", code, err))
		}
		m, err := catalog.Decode(data)
		if err != nil {
			panic(fmt.Sprintf("rastrillo: base catalog %s: %v", code, err))
		}
		out[code] = Catalog(m)
	}
	return out
}()

// BaseCatalog returns a copy of the framework's English strings — the
// view every existing caller (serve.go, ui's defaultT) already relies
// on. A copy, so a caller's edits cannot reach the shared table.
func BaseCatalog() Catalog { return copyCatalog(baseCatalogs["en"]) }

// BaseCatalogs returns a copy of every shipped catalog, keyed by locale
// code exactly as declared in BaseLocales.
func BaseCatalogs() map[string]Catalog {
	out := make(map[string]Catalog, len(baseCatalogs))
	for code, c := range baseCatalogs {
		out[code] = copyCatalog(c)
	}
	return out
}

// BaseLocales returns the shipped locale codes, en first.
func BaseLocales() []string { return append([]string(nil), baseLocales...) }

// BaseKeys returns the sorted rastrillo.ui.* key set — what an app
// declaring a locale the framework does not ship has to translate
// before `rastrillo generate --check` passes (spec §3.4).
func BaseKeys() []string {
	keys := make([]string, 0, len(baseCatalogs["en"]))
	for k := range baseCatalogs["en"] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// IsBaseKey reports whether key is one the framework ships.
func IsBaseKey(key string) bool {
	_, ok := baseCatalogs["en"][key]
	return ok && strings.HasPrefix(key, "rastrillo.ui.")
}

func copyCatalog(c Catalog) Catalog {
	out := make(Catalog, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}
