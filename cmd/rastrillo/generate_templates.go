package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/tmplgen"
)

// checkSummary is what a passing generate --check prints, before
// checkTemplates adds its part.
const checkSummary = "rastrillo generate --check: %d route(s), " +
	"actions tagged, locale catalogs complete, icon slugs resolve, " +
	"hand-written code on its operation"

// checkTemplates runs the template step under --check, unless the
// templates flag turned it off, and returns what it adds to the
// summary. A main that could not be captured has templates nobody
// checked, so the summary claims nothing for them.
func checkTemplates(dir string, templates bool) (string, error) {
	if !templates {
		return "", nil
	}
	res, err := compileTemplates(dir, true)
	if err != nil || len(res.CaptureFailures) > 0 {
		return "", err
	}
	return ", compiled templates current", nil
}

// The template step's messages. A capture failure's reason reads as
// what the main did ("failed in capture mode: ..."), so it follows the
// main's import path.
const (
	templatesWrote   = "wrote %s: compiled templates"
	templatesRemoved = "removed %s: its package builds no template sets now"
	templatesStale   = "%s is out of date with the app's templates; " +
		"run rastrillo generate"
	templatesMissing     = "%s is missing; run rastrillo generate and commit it"
	templatesUnsupported = "template %q in %s stays on html/template: %s"
	templatesNotCaptured = "templates not compiled: %s %s"
	templatesNotChecked  = "templates not checked: %s %s"
)

// compileTemplates runs the template step on the module at dir, or
// with check compares its generated files with what it would write,
// and returns what it found.
//
// A template the compiler cannot handle, and a main that cannot start
// in capture mode, are one line each and never an error: those
// templates render through html/template, which is correct, only
// slower, and failing the command would block gen/ over a speed-up.
// A stale file is an error only under --check, where it is the gate.
// So is a missing one: a file deleted or never committed would ship
// its pages on html/template with the gate green. A new app is not red
// for it, because rastrillo new compiles its templates. So, under
// --check, is a main that cannot start beside a compiled file it
// builds sets for: the gate cannot say that file is current.
func compileTemplates(dir string, check bool) (tmplgen.Result, error) {
	res, err := tmplgen.Run(dir, tmplgen.Options{Check: check})
	if err != nil {
		return res, fmt.Errorf("compile templates: %w", err)
	}
	for _, path := range res.Written {
		fmt.Printf(templatesWrote+"\n", path)
	}
	for _, path := range res.Removed {
		fmt.Printf(templatesRemoved+"\n", path)
	}
	for _, note := range res.Unsupported {
		fmt.Fprintf(os.Stderr, templatesUnsupported+"\n",
			note.Template, note.Owner, note.Reason)
	}
	var lines []string
	for _, failure := range res.CaptureFailures {
		if slices.Contains(res.Unverified, failure) {
			lines = append(lines, fmt.Sprintf(templatesNotChecked,
				failure.Owner, failure.Reason))
			continue
		}
		fmt.Fprintf(os.Stderr, templatesNotCaptured+"\n",
			failure.Owner, failure.Reason)
	}
	for _, path := range res.Stale {
		msg := templatesStale
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			msg = templatesMissing
		} else if err != nil {
			return res, err
		}
		lines = append(lines, fmt.Sprintf(msg, path))
	}
	if len(lines) == 0 {
		return res, nil
	}
	return res, errors.New(strings.Join(lines, "\n"))
}
