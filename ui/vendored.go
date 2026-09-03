package ui

// The vendored set: the files `rastrillo new` copies into a new app's
// static/ directory once, and that are the app's own from that moment.
//
// This file exists so that the list is written down exactly once. Three
// callers need it and they must never disagree: the scaffold that
// writes the files, the vendored_test.go the scaffold generates into
// the app, and `rastrillo doctor`, which compares an app's copies
// against these. Two of those lists drifting apart — a file added to
// the scaffold and forgotten by the pin — is the failure this closes.

// vendoredNames lists the vendored files in the order they are worth
// reading: the structural stylesheet, the theme that colours it, then
// the four scripts, shim first — with calendar.js last because it is
// the only one that is not an enhancement in its own right: it draws
// the month grid datetime.js asks it for. TestVendoredNamesMatchVendoredAssets
// holds this and VendoredAssets to the same set.
var vendoredNames = []string{"tokens.css", "theme.css", "rastrillo.js", "select.js", "datetime.js", "calendar.js"}

// VendoredNames returns the names the vendored files take in an app's
// static/ directory, in a stable order, for a caller that wants to
// report on them one at a time. The returned slice is a copy.
func VendoredNames() []string { return append([]string(nil), vendoredNames...) }

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
// The bytes are the library's own embedded copies. They are delivered
// once and app-owned from then on: nothing in the framework rewrites
// them, and an app is free to edit or delete any of them.
func VendoredAssets(theme string) (map[string][]byte, bool) {
	themeCSS, ok := ThemeCSS(theme)
	if !ok {
		return nil, false
	}
	return map[string][]byte{
		"tokens.css":   TokensCSS(),
		"theme.css":    themeCSS,
		"rastrillo.js": ShimJS(),
		"select.js":    SelectJS(),
		"datetime.js":  DatetimeJS(),
		"calendar.js":  CalendarJS(),
	}, true
}
