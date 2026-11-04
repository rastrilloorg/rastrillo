package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Two pairs of vendored files refresh together or not at all. Fiesta
// does not support fiesta.js beside the full rastrillo.js an app had
// before it: both answer the same menus. And the old datetime.js calls
// rastrilloCalendar, which the new calendar.js names fiestaCalendar, so
// either file alone breaks the date field.
var vendoredPairs = map[string]string{
	"rastrillo.js": "fiesta.js", "fiesta.js": "rastrillo.js",
	"calendar.js": "datetime.js", "datetime.js": "calendar.js",
}

// rewrites reports whether --fix writes file on its own account, before
// its pair is considered.
func rewrites(file vendoredFile, force bool) bool {
	switch file.state {
	case fileDiffers, fileAbsent:
		return true
	case fileMine, fileRemoved:
		return force
	}
	return false
}

// heldBy names the partner that keeps file as it is: --fix would write
// file, but leaves its partner, which is not the library's, as it is.
func (findings *report) heldBy(file vendoredFile, force bool) string {
	partner, paired := vendoredPairs[file.name]
	if !paired || !rewrites(file, force) {
		return ""
	}
	for _, other := range findings.files {
		if other.name == partner && other.state != fileOK &&
			!rewrites(other, force) {
			return partner
		}
	}
	return ""
}

// pairNote says why --fix leaves a file for its partner, and how to
// settle it in the words of the app's own vendored test, which goes on
// failing on the file until it is refreshed or recorded.
func (findings *report) pairNote(name, partner string) string {
	record := "add " + name + " to vendoredIsMine too"
	if findings.pinLegacy {
		record = "delete " + name + "'s line from the pin too"
	}
	return fmt.Sprintf("goes with %s, left alone above: %s, "+
		"or pass --force for both", partner, record)
}

// leavesForItsPair says so and reports true when --fix leaves file for
// its partner.
func (findings *report) leavesForItsPair(
	output io.Writer, file vendoredFile, force bool,
) bool {
	partner := findings.heldBy(file, force)
	if partner != "" {
		fmt.Fprintf(output, "  left alone  %s (%s)\n", file.name,
			findings.pairNote(file.name, partner))
	}
	return partner != ""
}

// pageScriptsLine loads the scripts a page uses, in ui's order, which
// puts live.js after busy.js, which hands it a live form. The tags are
// deferred so they never block paint, and written through path so they
// do not 404 under a base path.
const pageScriptsLine = `{{range pageScripts}}` +
	`<script defer src="{{path $ (asset .)}}"></script>{{end}}`

// doctorFiestaAdvisory is the line doctor prints for a layout that will
// load the slim rastrillo.js and no fiesta.js, which leaves its menus
// without light dismiss and its switches without drag. It is
// user-facing copy, so a change to it goes through the copy review.
const doctorFiestaAdvisory = "layout loads no fiesta.js: add " +
	pageScriptsLine + " (CHANGELOG)"

var rangesPageScripts = regexp.MustCompile(`\{\{-?\s*range\s+pageScripts\b`)

// layoutsWithoutFiestaJS lists the templates beside static/ that load
// rastrillo.js but neither range pageScripts nor link fiesta.js.
func layoutsWithoutFiestaJS(dir, templates string) []string {
	var layouts []string
	names, _ := filepath.Glob(filepath.Join(templates, "*.html"))
	for _, name := range names {
		body, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		source := templateCommentRE.ReplaceAllString(string(body), "")
		if strings.Contains(source, "rastrillo.js") &&
			!strings.Contains(source, "fiesta.js") &&
			!rangesPageScripts.MatchString(source) {
			layouts = append(layouts, rel(dir, name))
		}
	}
	return layouts
}

// slimAfterFix reports whether rastrillo.js is the slim shim once --fix
// has run with force: already, or because --fix writes it.
func (findings *report) slimAfterFix(force bool) bool {
	refuses := findings.preMove != "" || findings.skewed() && !force
	for _, file := range findings.files {
		if file.name == "rastrillo.js" {
			writes := !refuses && rewrites(file, force) &&
				findings.heldBy(file, force) == ""
			return file.state == fileOK || writes
		}
	}
	return false
}

// adviseFiestaJS names each layout the slim shim leaves without
// fiesta.js, judged on the files --fix leaves rather than those it
// found, so the run that slims the shim is the run that says so.
func (findings *report) adviseFiestaJS(output io.Writer, force bool) {
	if !findings.slimAfterFix(force) {
		return
	}
	for _, layout := range findings.fiestaAdvice {
		fmt.Fprintf(output, "%s: %s\n\n", layout, doctorFiestaAdvisory)
	}
}
