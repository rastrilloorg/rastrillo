// Package vendored is the one list of the files rastrillo new copies
// into an app's static/ directory, and the minifier Assets serves them
// through. It sits below ui so the root package can read the list
// without importing ui, which imports it.
package vendored

import (
	"path"
	"slices"
)

// names lists the vendored files in the order they are worth reading:
// the structural stylesheet, the theme that colours it and the shells'
// slide, then the scripts: the shim, the busy rule beside it, the live
// pages' script (inert until a page opts in), and the light the depth
// styles read, which an app may delete; the menus' fallback for a
// browser without anchor positioning, the shells' navigation, and
// calendar.js last because it is the only one that is not an
// enhancement in its own right: it draws the month grid datetime.js
// asks it for. ui's TestVendoredNamesMatchVendoredAssets holds this and
// ui.VendoredAssets to the same set.
var names = []string{
	"tokens.css", "theme.css", "shell.css",
	"rastrillo.js", "busy.js", "live.js", "light.js", "anchor.js",
	"shell.js", "transition.js", "select.js", "datetime.js", "calendar.js",
}

// componentScripts are the scripts a page loads only when a field on
// it needs them, in the order the layouts load them: calendar.js
// before datetime.js, which calls it.
var componentScripts = []string{"select.js", "calendar.js", "datetime.js"}

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
