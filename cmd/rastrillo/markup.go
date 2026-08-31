package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo/internal/markup"
	"github.com/carlosframework/rastrillo/ui"
)

// runMarkup implements `rastrillo markup [--fix] [dir]`: the class →
// attribute codemod for the markup grammar of design doc §6-v3.
//
// Rastrillo's UI vocabulary used to be classes and is now attributes —
// <div rst-box> where an app wrote <div class="rst-box">. tokens.css
// styles both spellings for one release so nothing breaks in the gap,
// and this is the tool that closes it: it rewrites an app's templates,
// its Go string literals and its documentation in one pass, and it is
// the same code the framework flipped itself with.
//
// It reports by default and writes with --fix, because a codemod that
// edits a tree on sight is one nobody runs on a repository they care
// about. Rewriting is idempotent, so running it twice, or over a tree
// half of which is already done, changes nothing the second time.
//
// Two things it deliberately will not do:
//
//   - The vendored static files. static/tokens.css, static/rastrillo.js
//     and their siblings are copies of this library's, refreshed with
//     `rastrillo doctor --fix`, never hand-migrated. Rewriting one here
//     would make the app's copy differ from the library's for good.
//   - A class list whose shape it cannot read. It leaves those exactly
//     as they were and prints them, because a wrong guess renders
//     unstyled and looks like markup somebody wrote on purpose.
//
// The app's OWN stylesheet is the thing that catches people out, so it
// is reported separately: a rule you wrote against .rst-lrow stops
// matching the moment your markup says rst-lrow, and no test in your
// app will notice.
func runMarkup(args []string) error {
	fset := flag.NewFlagSet("markup", flag.ContinueOnError)
	fix := fset.Bool("fix", false, "write the rewrite (default: report what would change)")
	if err := fset.Parse(args); err != nil {
		return exitError{code: 2}
	}
	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	return markupSweep(os.Stdout, dir, *fix)
}

// migratable is the set of files that can carry markup: templates, Go
// source with markup in its string literals or its doc comments,
// documentation, and the scripts and stylesheets that name the
// vocabulary.
var migratable = map[string]bool{
	".html": true, ".htm": true, ".gohtml": true, ".tmpl": true,
	".go": true, ".md": true, ".js": true, ".css": true,
}

// vendored names a file the app does not own: it is this library's,
// copied into static/ at scaffold time and refreshed by doctor.
func vendored(rel string) bool {
	dir, base := filepath.Split(filepath.ToSlash(rel))
	if !strings.HasSuffix(dir, "static/") {
		return false
	}
	switch base {
	case "tokens.css", "theme.css", "rastrillo.js", "select.js", "datetime.js":
		return true
	}
	return false
}

var appOwnClassSelector = regexp.MustCompile(`\.(rst-[A-Za-z0-9_-]+)`)

func markupSweep(w io.Writer, dir string, fix bool) error {
	type change struct {
		rel   string
		notes []markup.Note
	}
	var changed []change
	var notesOnly []change
	noted := 0
	ownCSS := map[string][]string{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".design-system", ".superpowers":
				return fs.SkipDir
			}
			return nil
		}
		if !migratable[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if vendored(rel) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, notes := markup.Rewrite(src)
		// The app's own rules, wherever they are written: a .css file,
		// or a <style> block in the layout template, which is the same
		// trap wearing a different extension.
		for _, css := range ownStylesheets(string(src), filepath.Ext(path), src) {
			for _, m := range appOwnClassSelector.FindAllStringSubmatch(css, -1) {
				if _, util := markup.Utilities[m[1]]; !util {
					ownCSS[rel] = appendOnce(ownCSS[rel], m[1])
				}
			}
		}
		noted += len(notes)
		switch {
		case string(out) != string(src):
			changed = append(changed, change{rel, notes})
			if fix {
				if err := os.WriteFile(path, out, 0o644); err != nil {
					return err
				}
			}
		case len(notes) > 0:
			notesOnly = append(notesOnly, change{rel, notes})
		}
		return nil
	})
	if err != nil {
		return err
	}

	verb := "would be rewritten"
	if fix {
		verb = "rewritten"
	}
	fmt.Fprintf(w, "rastrillo markup: %d file(s) %s in %s\n", len(changed), verb, dir)
	for _, c := range changed {
		fmt.Fprintf(w, "  %s\n", c.rel)
	}
	var left []change
	left = append(left, changed...)
	left = append(left, notesOnly...)
	printed := false
	for _, c := range left {
		for _, n := range c.notes {
			if !printed {
				fmt.Fprintf(w, "\nleft alone, for you to read — a class list this cannot take apart:\n")
				printed = true
			}
			fmt.Fprintf(w, "  %s:%d: %s\n", c.rel, n.Line, n.Text)
		}
	}
	if len(ownCSS) > 0 {
		fmt.Fprintf(w, "\nyour own stylesheet keys off the class spelling, which your markup no longer writes.\n"+
			"change these to attribute selectors — .rst-lrow becomes [rst-lrow]:\n")
		var files []string
		for f := range ownCSS {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			sort.Strings(ownCSS[f])
			fmt.Fprintf(w, "  %s: %s\n", f, strings.Join(ownCSS[f], " "))
		}
	}
	if len(changed) > 0 {
		fmt.Fprintf(w, "\nthe save bar renamed: an app that wrote class=\"rst-form-foot\" by hand now writes\n"+
			"rst-form-bar. rst-form-foot is the row the form-foot partial emits.\n")
	}
	if !fix && len(changed) > 0 {
		fmt.Fprintf(w, "\nrun again with --fix to write it.\n")
	}
	// Work left is work left, whether it is a rewrite waiting for --fix
	// or a class list only a human can take apart. A CI gate that got 0
	// here would wave through an app whose remaining markup is entirely
	// in shapes this tool reports rather than rewrites — which is
	// exactly the app that most needs to be stopped.
	if noted > 0 {
		fmt.Fprintf(w, "\n%d class attribute(s) above need a human. This exits %d until they are gone.\n",
			noted, exitMarkupPending)
	}
	if noted > 0 || (!fix && len(changed) > 0) {
		return exitError{code: exitMarkupPending}
	}
	return nil
}

// ownStylesheets returns the CSS in a file: the whole of it for a
// stylesheet the app owns, and every <style> block for anything else.
// A vendored copy of the library's tokens.css is not the app's, and
// naming its rules would be a page of noise that teaches people to
// ignore the report.
func ownStylesheets(src, ext string, raw []byte) []string {
	if strings.EqualFold(ext, ".css") {
		if bytes.Equal(raw, ui.TokensCSS()) {
			return nil
		}
		return []string{src}
	}
	var out []string
	for _, m := range styleBlock.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	return out
}

var styleBlock = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)

// exitMarkupPending: there is work here. It is drift's code, and for
// the same reason — a CI branching on it wants "there is something to
// do here", not "the command failed".
const exitMarkupPending = 3

func appendOnce(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}
