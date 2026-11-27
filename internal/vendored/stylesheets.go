package vendored

import (
	"slices"
	"sync"

	"amadan.net/rastrillo/fiesta"
)

// componentStylesheets are the stylesheets a page links only where it
// shows what they style, after the theme, each with the pinned Fiesta's
// bytes. Fiesta 1.10 keeps the data table and the charts out of
// fiesta.css, so a page without one pays nothing for them.
var componentStylesheets = []struct {
	name  string
	bytes func() []byte
}{
	{"data.css", fiesta.DataCSS},
}

// stylesheetHooks is, per component stylesheet, every hook the pinned
// Fiesta's copy styles, in either spelling, read once. A page showing
// any of them links the stylesheet; ui's slice of it keeps only the
// page's rules.
var stylesheetHooks = sync.OnceValue(func() map[string]map[string]bool {
	hooks := map[string]map[string]bool{}
	for _, sheet := range componentStylesheets {
		hooks[sheet.name], _ = StyledHooks(sheet.bytes())
	}
	return hooks
})

// ComponentStylesheets returns the component stylesheets' names, in the
// order a page links them.
func ComponentStylesheets() []string {
	var names []string
	for _, sheet := range componentStylesheets {
		names = append(names, sheet.name)
	}
	return names
}

// NeededStylesheets returns the component stylesheets a page showing
// hooks links, in link order.
func NeededStylesheets(hooks []string) []string {
	var needed []string
	for _, name := range ComponentStylesheets() {
		styled := stylesheetHooks()[name]
		if slices.ContainsFunc(hooks, func(hook string) bool {
			return styled[hook]
		}) {
			needed = append(needed, name)
		}
	}
	return needed
}
