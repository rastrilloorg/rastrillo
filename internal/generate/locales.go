package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

// MissingKeys reports, per non-default locale, the keys present in the
// default locale's catalog and absent from that one — design doc §10's
// pre-ship check: "silent fallback while iterating, loud failure before
// ship." The other direction (a key a non-default locale has and the
// default does not) is deliberately not a failure; §10 names only this
// one.
//
// Only the app's own catalogs are compared. The framework's base
// component catalog is never an app's to translate, and it is not in
// localesDir, so it cannot be reported here by construction.
//
// An app with no locales/ directory at all is the common single-locale
// case and returns no findings.
func MissingKeys(localesDir, defaultCode string) (map[string][]string, error) {
	catalogs, err := readCatalogs(localesDir)
	if err != nil || len(catalogs) == 0 {
		return nil, err
	}

	def, ok := catalogs[defaultCode]
	if !ok {
		return nil, fmt.Errorf("no %s.toml in %s, but %d other locale catalog(s) are there — the default locale's catalog is what every other one is checked against", defaultCode, localesDir, len(catalogs))
	}

	out := map[string][]string{}
	for code, c := range catalogs {
		if code == defaultCode {
			continue
		}
		var missing []string
		for key := range def {
			if _, ok := c[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			out[code] = missing
		}
	}
	return out, nil
}

// readCatalogs decodes every <code>.toml in localesDir. nil, nil when
// the directory does not exist — the single-locale case.
func readCatalogs(localesDir string) (map[string]map[string]string, error) {
	entries, err := os.ReadDir(localesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	catalogs := map[string]map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(localesDir, name))
		if err != nil {
			return nil, err
		}
		m, err := catalog.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		catalogs[strings.TrimSuffix(name, ".toml")] = m
	}
	return catalogs, nil
}

// MissingFrameworkKeys is spec 2026-08-28 §3.4's rule: a non-default
// catalog for a locale the framework does NOT ship must translate every
// rastrillo.ui.* key, or its built-in components render in English and
// nobody is told. A locale in shipped is exempt — the framework's own
// catalog covers it. frameworkKeys and shipped come from the caller
// (rastrillo.BaseKeys, rastrillo.BaseLocales) so this package does not
// import the root.
func MissingFrameworkKeys(localesDir, defaultCode string, frameworkKeys, shipped []string) (map[string][]string, error) {
	catalogs, err := readCatalogs(localesDir)
	if err != nil || len(catalogs) == 0 {
		return nil, err
	}
	exempt := map[string]bool{}
	for _, s := range shipped {
		exempt[s] = true
	}
	out := map[string][]string{}
	for code, c := range catalogs {
		if code == defaultCode || exempt[code] {
			continue
		}
		var missing []string
		for _, k := range frameworkKeys {
			if _, ok := c[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			out[code] = missing
		}
	}
	return out, nil
}
