package main

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/ui"
)

// doctorDataAdvisory is the line doctor prints for a layout that does
// not link the component stylesheets while a page shows a table or a
// chart. It is user-facing copy, so a change to it goes through the
// copy review.
const doctorDataAdvisory = `A page shows a data table or a chart, ` +
	`and this layout doesn't link data.css, so the table or chart is ` +
	`unstyled. Add ` +
	`{{range pageComponentStylesheets}}<link rel="stylesheet" ` +
	`href="{{path $ .}}">{{end}} after the theme's link, and run ` +
	`rastrillo doctor -fix if static/data.css is missing.`

// definesContent finds a page: a file that defines the layout's
// content block.
var definesContent = regexp.MustCompile(`\{\{-?\s*define\s+"content"`)

// leavesDataUnstyled reports whether a page shows a table or a chart
// while the layout never asks for data.css. Each page is read with the
// layout and every other file by ui's own page planning, so a hook in
// a shared partial or a template call's argument counts as ui.Pages
// counts it, and a partial no page calls does not.
func leavesDataUnstyled(layout string, files map[string]string) bool {
	if strings.Contains(layout, "pageComponentStylesheets") {
		return false
	}
	var pages, shared []string
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if definesContent.MatchString(files[name]) {
			pages = append(pages, files[name])
		} else {
			shared = append(shared, files[name])
		}
	}
	for _, page := range pages {
		sheets, err := ui.ComponentStylesheetsShown(layout, shared, page)
		if err == nil && slices.Contains(sheets, "data.css") {
			return true
		}
	}
	return false
}
