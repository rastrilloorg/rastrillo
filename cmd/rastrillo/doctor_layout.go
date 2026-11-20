package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/ui"
)

// doctorPhoneMenuAdvisory is the line doctor prints for a layout that
// still has the phone Menu card or the back strip. It is user-facing
// copy, so a change to it goes through the copy review. "Upgrading to
// one header row" is the section of docs/site/templates.md that gives
// the edit.
const doctorPhoneMenuAdvisory = `This layout still has the old phone menu ` +
	`card or back strip. See "Upgrading to one header row" in the ` +
	`templates guide.`

// oldPhoneMenu reports whether a layout still carries the phone Menu
// card or back strip and has no rst-shell-up, the one header row's
// back link. It reads attributes on tags, so a comment, a quoted value
// (a title, or a details group name="rst-shell-menu") naming either
// side is not read as the markup.
func oldPhoneMenu(src string) bool {
	old := false
	for _, attribute := range ui.Attributes(src) {
		switch attribute.Name {
		case "rst-shell-up":
			return false
		case "rst-shell-menu", "rst-shell-tail", "rst-shell-back":
			old = true
		case "class":
			for _, class := range strings.Fields(attribute.Value) {
				if class == "rst-shell__menu" || class == "rst-shell__back" {
					old = true
				}
			}
		}
	}
	return old
}

// adviseOnLayout records which advisories the app's layout earns. The
// layout is read once, and a layout that cannot be read earns none.
func (rep *report) adviseOnLayout(dir, layout string) {
	body, err := os.ReadFile(layout)
	if err != nil {
		return
	}
	src := string(body)
	if oldPhoneMenu(src) {
		rep.phoneAdvice = rel(dir, layout)
	}
	if oldShellLayout(src) {
		rep.layoutAdvice = rel(dir, layout)
		return
	}
	pages := map[string]string{}
	names, _ := filepath.Glob(filepath.Join(filepath.Dir(layout), "*.html"))
	for _, name := range names {
		if name == layout {
			continue
		}
		if page, err := os.ReadFile(name); err == nil {
			pages[name] = string(page)
		}
	}
	if profileAdvisory(src, pages) {
		rep.profileAdvice = rel(dir, layout)
	}
}

// printLayoutAdvice prints the one line each layout advisory earns.
func (rep *report) printLayoutAdvice(out io.Writer) {
	for _, advice := range []struct{ file, line string }{
		{rep.layoutAdvice, doctorLayoutAdvisory},
		{rep.profileAdvice, doctorProfileAdvisory},
		{rep.phoneAdvice, doctorPhoneMenuAdvisory},
	} {
		if advice.file != "" {
			fmt.Fprintf(out, "%s: %s\n\n", advice.file, advice.line)
		}
	}
}
