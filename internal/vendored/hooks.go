package vendored

import (
	"maps"
	"slices"
)

// createdHooks lists, per script, the hooks it puts on elements it
// builds at run time. They never appear in a page's templates, so a
// stylesheet slice that kept only the rules the markup names would
// strip the script's own widgets bare. A script that only reads hooks
// (rastrillo.js finds [rst-dropdown] a page already carries) creates
// none and has no entry. ui's TestEachScriptCreatesExactlyItsListedHooks
// holds each list to its script.
var createdHooks = map[string][]string{
	"busy.js": {"rst-spin"},
	"live.js": {"rst-spin"},
	"select.js": {
		"rst-combo", "rst-combo-compact", "rst-combo-desc",
		"rst-combo-empty", "rst-combo-has-lead", "rst-combo-lead",
		"rst-combo-list", "rst-combo-name", "rst-combo-option",
		"rst-combo-rows", "rst-combo-sep", "rst-combo-up", "rst-input",
		"rst-select-group", "rst-sr-only",
	},
	"datetime.js": {
		"rst-dtp", "rst-dtp-hint", "rst-dtp-input", "rst-dtp-label",
		"rst-dtp-list", "rst-dtp-pick", "rst-dtp-quick", "rst-dtp-row",
		"rst-dtp-set", "rst-input", "rst-sr-only",
	},
	"calendar.js": {
		"rst-cal", "rst-cal-day", "rst-cal-grid", "rst-cal-head",
		"rst-cal-nav", "rst-cal-title", "rst-sr-only",
	},
}

// litHooks are the elements light.js writes its custom properties on:
// its BTN and CARD selectors. A page with none of them has nothing for
// the script to light, so it need not load it. ui's
// TestLightLightsExactlyLitHooks holds the list to the script.
var litHooks = []string{
	"rst-btn", "rst-box", "rst-card", "rst-stats", "rst-list",
}

// CreatedHooks returns the hooks the named script creates, sorted. It
// is nil for a script that creates none and for a name it does not
// know. The returned slice is a copy.
func CreatedHooks(script string) []string {
	return slices.Clone(createdHooks[script])
}

// LitHooks returns the hooks light.js lights. The returned slice is a
// copy.
func LitHooks() []string { return slices.Clone(litHooks) }

// writtenHooks lists, per framework template function, the hooks its
// output carries. A page's templates show only the call, so without
// the list the stage shell's backdrop, {{stageArt "rastrillo"}}, would
// lose its rules from every stage page's slice. menuGroup writes the
// shared menu group's name when a partial gives none. ui's
// TestEachFunctionWritesExactlyItsListedHooks holds each list to its
// function's output.
var writtenHooks = map[string][]string{
	"menuGroup": {"rst-menus"},
	"stageArt":  {"rst-stage-art", "rst-stage-art-glow", "rst-stage-art-lines"},
}

// WrittenHooks returns the hooks the named framework template function
// writes, sorted. It is nil for a function that writes none and for a
// name it does not know. The returned slice is a copy.
func WrittenHooks(function string) []string {
	return slices.Clone(writtenHooks[function])
}

// WritingFunctions returns the names of the template functions that
// write hooks, sorted, so a test can hold the list to the functions.
func WritingFunctions() []string {
	return slices.Sorted(maps.Keys(writtenHooks))
}
