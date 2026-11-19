// Package cspcheck finds markup that Rastrillo's default
// Content-Security-Policy drops: style attributes, <style> blocks,
// inline scripts, on* handler attributes and javascript: URLs.
//
// The browser drops each of them without an error the page can see, so
// only a browser test that visits the page notices. This reads
// the app's source instead: its templates, the string literals of its
// Go files and its own scripts. Test files, Markdown and stylesheets
// are not read, and neither are the vendored copies of the library's
// files in static/, which doctor keeps. Directories named testdata,
// vendor or node_modules, any starting with "." or "_", and nested
// modules are not walked.
//
// It reads markup with regular expressions, not a parser, because a
// template is not HTML until it runs. Template actions and comments are
// blanked first, so {{if .Hidden}}style="..."{{end}} is still found and
// a comparison inside an action is not taken for a tag.
package cspcheck

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// Finding is one piece of markup the CSP drops.
type Finding struct {
	// Path is slash-separated and relative to the directory walked.
	Path string
	Line int
	// Problem says what was found and what to write instead.
	Problem string
}

func (finding Finding) String() string {
	return fmt.Sprintf("%s:%d: %s", finding.Path, finding.Line,
		finding.Problem)
}

// Find inspects every file of the module at root. An error is a failure
// to read, never a verdict on the markup.
func Find(root string) ([]Finding, error) {
	var findings []Finding
	err := filepath.WalkDir(root, func(
		full string, entry fs.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			return enter(full, relative)
		}
		if !inspected(relative) || isVendored(relative) {
			return nil
		}
		source, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		findings = append(findings, Inspect(relative, source)...)
		return nil
	})
	return findings, err
}

// enter skips a directory that holds no source of this module's own.
func enter(full, relative string) error {
	if relative == "." {
		return nil
	}
	name := path.Base(relative)
	switch {
	case strings.HasPrefix(name, "."), strings.HasPrefix(name, "_"),
		name == "testdata", name == "vendor", name == "node_modules":
		return fs.SkipDir
	}
	if _, err := os.Stat(filepath.Join(full, "go.mod")); err == nil {
		return fs.SkipDir
	}
	return nil
}

// isVendored reports a copy of the library's own file in a static/
// directory: doctor keeps it, so a finding there is not the app's.
func isVendored(relative string) bool {
	directory, file := path.Split(relative)
	return strings.HasSuffix("/"+directory, "/static/") &&
		vendored.Is("static/"+file)
}

// readers says how to read each kind of file, by extension.
var readers = map[string]func(source []byte) []problem{
	".html": inspectTemplate, ".htm": inspectTemplate,
	".gohtml": inspectTemplate, ".tmpl": inspectTemplate,
	".go": inspectGo, ".js": inspectScript, ".mjs": inspectScript,
}

// inspected reports a file Inspect reads, so Find opens no other.
func inspected(name string) bool {
	_, known := readers[strings.ToLower(path.Ext(name))]
	return known && !strings.HasSuffix(name, "_test.go")
}

// Inspect reads one file, choosing how by name's extension, and returns
// what in it the CSP drops. name is only reported and matched, never
// opened.
func Inspect(name string, source []byte) []Finding {
	if !inspected(name) {
		return nil
	}
	problems := readers[strings.ToLower(path.Ext(name))](source)
	findings := make([]Finding, 0, len(problems))
	for _, found := range problems {
		findings = append(findings, Finding{
			Path: name, Line: found.line, Problem: found.text,
		})
	}
	return findings
}

// problem is a finding before it knows its file.
type problem struct {
	line int
	text string
}
