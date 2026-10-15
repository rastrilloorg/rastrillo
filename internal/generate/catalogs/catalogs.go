// Package catalogs is the declared-catalog contract: which
// locales/<code>.toml files an app's offered locales require, and
// whether the ones present are complete. generate --check and doctor
// both call Check, so the two can never disagree about a catalog.
package catalogs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/catalog"
	"amadan.net/rastrillo/rastrillo/internal/generate"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// Appendix C.2's op.catalog.missing and its two reasons.
const (
	missingMsg        = "manifest/app.toml offers %q, so locales/%s.toml is required: %s"
	reasonNoFramework = "the framework's own strings are not translated into it"
	reasonAppKeys     = "this app has its own strings, and they are in English"
)

// Check returns the problems with app's catalogs under appDir, one
// string per problem.
//
// An offered code needs locales/<code>.toml when the framework's own
// strings are not translated into it, or when the app has strings of
// its own and the code is not en: app strings are authored in English
// (the manifest emitters write gen/locales/en.toml), so every other
// language needs a file someone translated. Without this, a declared
// language serves English silently.
//
// The completeness findings are the ones --check always made, now
// measured against app.DefaultLocale rather than a flag that could
// disagree with what the app serves.
func Check(appDir string, app manifest.App) ([]string, error) {
	localesDir := filepath.Join(appDir, "locales")
	ownStrings, err := hasOwnStrings(appDir)
	if err != nil {
		return nil, err
	}
	shipped := rastrillo.BaseLocales()

	var problems []string
	for _, code := range app.Locales {
		// A code retagged to a shipped one (rastrillo.RetagOf: pt to
		// pt-PT) gets the shipped strings from rastrillo.NewLocales, so
		// an app that offered pt before the retag needs no file for it.
		to, _ := rastrillo.RetagOf(code)
		reason := ""
		switch {
		case !slices.Contains(shipped, code) && !slices.Contains(shipped, to):
			reason = reasonNoFramework
		case ownStrings && code != "en":
			reason = reasonAppKeys
		default:
			continue
		}
		if ok, err := hasAppCatalog(localesDir, code); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(missingMsg, code, code, reason))
	}

	// With an English default, the reference is every English string
	// the app has: its own locales/en.toml and the manifest's generated
	// gen/locales/en.toml. A manifest app keeps its English only in the
	// generated file, so against locales/en.toml alone, adding a
	// language and its locales/<code>.toml (which the rule above
	// demands) failed with "no en.toml in locales", and a hand en.toml
	// beside it would let a catalog lacking the manifest's keys pass
	// and serve them in English. Another default's catalog already
	// carries those keys translated, so it is the reference alone.
	var generated map[string]string
	if app.DefaultLocale == "en" {
		if generated, err = readGenerated(appDir); err != nil {
			return nil, err
		}
	}
	missing, err := generate.MissingKeys(localesDir, app.DefaultLocale, generated)
	if err != nil {
		return nil, err
	}
	for _, code := range sortedKeys(missing) {
		problems = append(problems, fmt.Sprintf("locales/%s.toml is missing:\n    %s", code, strings.Join(missing[code], "\n    ")))
	}

	fw, err := generate.MissingFrameworkKeys(localesDir, app.DefaultLocale, rastrillo.BaseKeys(), shipped)
	if err != nil {
		return nil, err
	}
	for _, code := range sortedKeys(fw) {
		problems = append(problems, fmt.Sprintf("locales/%s.toml is missing %d rastrillo.ui.* key(s):\n    %s\n  the framework ships %s; any other locale translates these itself — copy the keys from the rastrillo module's locales/en.toml",
			code, len(fw[code]), strings.Join(fw[code], "\n    "), strings.Join(shipped, ", ")))
	}
	return problems, nil
}

// hasAppCatalog reports whether rastrillo.NewLocales finds a catalog of
// the app's own for code: locales/<code>.toml, or the file of a code
// retagged to it. An app that moved its manifest from pt to pt-PT is
// served from the pt.toml it already has, and demanding a pt-PT.toml
// here would fail an app that works.
func hasAppCatalog(localesDir, code string) (bool, error) {
	entries, err := os.ReadDir(localesDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".toml")
		if !ok || e.IsDir() {
			continue
		}
		if to, _ := rastrillo.RetagOf(name); name == code || to == code {
			return true, nil
		}
	}
	return false, nil
}

// readGenerated decodes gen/locales/en.toml; nil when there is none.
func readGenerated(appDir string) (map[string]string, error) {
	path := filepath.Join(appDir, "gen", "locales", "en.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := catalog.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// hasOwnStrings reports whether the app has strings beyond the
// framework's: a generated manifest catalog, or any catalog of its own.
func hasOwnStrings(appDir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(appDir, "gen", "locales", "en.toml")); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	own, err := filepath.Glob(filepath.Join(appDir, "locales", "*.toml"))
	return len(own) > 0, err
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
