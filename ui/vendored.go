package ui

import (
	"path"
	"sync"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// The vendored set: the files `rastrillo new` copies into a new app's
// static/ directory once, and that are the app's own from that moment.
//
// This file exists so that the list is written down exactly once. Three
// callers need it and they must never disagree: the scaffold that
// writes the files, the vendored_test.go the scaffold generates into
// the app, and `rastrillo doctor`, which compares an app's copies
// against these. Two of those lists drifting apart — a file added to
// the scaffold and forgotten by the pin — is the failure this closes.

// VendoredNames returns the names the vendored files take in an app's
// static/ directory, in a stable order, for a caller that wants to
// report on them one at a time. The list and its order live in
// internal/vendored, where the root package can read them too. The
// returned slice is a copy.
func VendoredNames() []string { return vendored.Names() }

// VendoredAssets returns every file rastrillo new writes into a new
// app's static/ directory, keyed by the name it takes there, for the
// named theme — reporting false for a theme that is not shipped.
//
// It is the one definition of "the vendored set". The scaffold writes
// these bytes, the vendored_test.go the scaffold generates compares the
// app's copies against them, and `rastrillo doctor` reports on the
// difference. All three read this function, so a file added here
// reaches all three at once, which is the point of it.
//
// The stylesheets and every script but rastrillo.js and live.js are
// the pinned Fiesta's own bytes; those two are Rastrillo's. They are
// delivered once and app-owned from then on: nothing in the framework
// rewrites them, and an app is free to edit or delete any of them.
func VendoredAssets(theme string) (map[string][]byte, bool) {
	themeCSS, ok := ThemeCSS(theme)
	if !ok {
		return nil, false
	}
	assets := map[string][]byte{
		"tokens.css": TokensCSS(),
		"theme.css":  themeCSS,
		"shell.css":  ShellCSS(),
	}
	for _, name := range VendoredNames() {
		if path.Ext(name) == ".js" {
			assets[name] = vendoredScript(name)
		}
	}
	return assets, true
}

// vendoredScript is the script the framework vendors as name: one of
// Rastrillo's own two, or one the pinned Fiesta ships.
func vendoredScript(name string) []byte {
	switch name {
	case "rastrillo.js":
		return ShimJS()
	case "live.js":
		return LiveJS()
	}
	return fiestaScript(name)
}

// frameworkScriptPaths is the path Assets links each vendored script by
// when the app serves the framework's own bytes for it: the path
// carries the fingerprint of the bytes served, minified.
var frameworkScriptPaths = sync.OnceValue(func() map[string]string {
	files := fstest.MapFS{}
	for _, name := range VendoredNames() {
		if path.Ext(name) == ".js" {
			files["static/"+name] = &fstest.MapFile{Data: vendoredScript(name)}
		}
	}
	framework := rastrillo.NewAssets(files)
	paths := map[string]string{}
	for name := range files {
		paths[name] = framework.Path(name)
	}
	return paths
})

// unchangedScripts reports, for a vendored script's static path,
// whether assets serve the framework's own bytes for it, read once, as
// Pages is built: a page that loads a script the app has edited, or
// one Assets cannot read, keeps every variant and class, since the
// app's code may write any. A script the app has deleted runs nothing.
func unchangedScripts(assets *rastrillo.Assets) func(string) bool {
	unchanged := map[string]bool{}
	for name, framework := range frameworkScriptPaths() {
		unchanged[name] = !assets.Has(name) || assets.Path(name) == framework
	}
	return func(name string) bool { return unchanged[name] }
}
