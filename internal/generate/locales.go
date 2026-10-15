package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
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
//
// generated is more of the default locale's strings that live outside
// localesDir (the manifest's gen/locales/en.toml); it joins the default
// catalog as the reference, and with it a missing default catalog is
// not an error. nil means the default catalog alone.
func MissingKeys(localesDir, defaultCode string, generated map[string]string) (map[string][]string, error) {
	catalogs, err := readCatalogs(localesDir)
	if err != nil || len(catalogs) == 0 {
		return nil, err
	}
	layerRetagged(catalogs, defaultCode)

	def, ok := catalogs[defaultCode]
	if !ok && len(generated) == 0 {
		return nil, fmt.Errorf("no %s.toml in %s, but %d other locale catalog(s) are there — the default locale's catalog is what every other one is checked against", defaultCode, localesDir, len(catalogs))
	}
	ref := make(map[string]string, len(def)+len(generated))
	for _, m := range []map[string]string{generated, def} {
		for k, v := range m {
			ref[k] = v
		}
	}
	def = ref

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

// layerRetagged makes catalogs what rastrillo.NewLocales serves: a
// retagged code's file (pt.toml) read under its target (pt-PT), with
// the target's own file layered over it. Compared file by file, a
// Portuguese-default app that kept its pt.toml failed with no
// pt-PT.toml, and a pt-PT.toml holding only overrides was missing
// every key pt.toml supplies. The old file stays a language of its own
// only when nothing else claims it: no target file and a different
// default. An app offering both pt and pt-PT beside both files then
// has pt.toml checked as part of pt-PT rather than on its own, which
// misses only a key pt-PT.toml has and pt.toml lacks.
func layerRetagged(catalogs map[string]map[string]string, defaultCode string) {
	for from, to := range rastrillo.Retagged() {
		src, ok := catalogs[from]
		dst, hasDst := catalogs[to]
		if !ok || (!hasDst && to != defaultCode) {
			continue
		}
		merged := make(map[string]string, len(src)+len(dst))
		for _, m := range []map[string]string{src, dst} {
			for k, v := range m {
				merged[k] = v
			}
		}
		catalogs[to] = merged
		delete(catalogs, from)
	}
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
// nobody is told. A locale in shipped is exempt, because the framework's
// own catalog covers it, and so is a code retagged to one in shipped
// (rastrillo.RetagOf): pt.toml gets the shipped pt-PT strings under
// rastrillo.NewLocales, so demanding every key there would fail a
// catalog that worked the day before the retag. frameworkKeys and
// shipped come from the caller (rastrillo.BaseKeys,
// rastrillo.BaseLocales).
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
		to, _ := rastrillo.RetagOf(code)
		if code == defaultCode || exempt[code] || exempt[to] {
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
