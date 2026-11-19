// Package vendored is the one list of the files rastrillo new copies
// into an app's static/ directory, and the minifier Assets serves them
// through. It sits below ui so the root package can read the list
// without importing ui, which imports it.
package vendored

import (
	"path"
	"slices"

	"amadan.net/rastrillo/fiesta"
)

// names lists the vendored files in the order they are worth reading:
// the structural stylesheet, the theme that colours it and the shells'
// slide, then Rastrillo's own two scripts, the fragment shim and the
// live pages' script (inert until a page opts in), then every script
// the pinned Fiesta ships, in Fiesta's load order. A script a Fiesta
// release adds is vendored by the bump that pins it. ui's
// TestVendoredNamesMatchVendoredAssets holds this and ui.VendoredAssets
// to the same set.
var names = append([]string{
	"tokens.css", "theme.css", "shell.css", "rastrillo.js", "live.js",
}, fiesta.ScriptNames()...)

// componentScripts are the scripts a page loads only where it shows
// what they enhance, in the order the layouts load them: the snippet's
// copy button, then the fields', calendar.js before datetime.js, which
// calls it.
var componentScripts = []string{
	"copy.js", "select.js", "calendar.js", "datetime.js",
}

// Names returns the vendored files' names, in a stable order. The
// returned slice is a copy.
func Names() []string { return slices.Clone(names) }

// ComponentScripts returns the scripts a page loads only where it uses
// them, in load order. The returned slice is a copy.
func ComponentScripts() []string { return slices.Clone(componentScripts) }

// Is reports whether name, a path in an app's static tree, is where the
// scaffold puts a vendored file. A file of the same name anywhere else
// is the app's own and is served as written.
func Is(name string) bool {
	dir, file := path.Split(name)
	return dir == "static/" && slices.Contains(names, file)
}
