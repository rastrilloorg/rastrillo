package ui

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/live"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

const (
	needsLiteralMsg = "ui: page %q: put the script name in quotes, " +
		"such as {{needs \"select.js\"}}"
	needsUnknownMsg = "ui: page %q: needs %q, which is not a component " +
		"script; use one of: %s"
	needsRenderMsg = "ui: needs %q, which is not a component script; " +
		"use one of: %s"
)

// needs renders nothing. NewPages reads each call off the page's parse
// tree before the page runs, because the layout writes its scripts in
// <head>, ahead of the content that would call needs while rendering.
func needs(name string) (string, error) {
	scripts := vendored.ComponentScripts()
	if !slices.Contains(scripts, name) {
		return "", fmt.Errorf(needsRenderMsg, name,
			strings.Join(scripts, ", "))
	}
	return "", nil
}

// allComponentScripts is every component script, as the static paths
// the layouts link them by. A render outside ui.Pages, which cannot
// know what its page needs, loads them all, as every layout did before.
func allComponentScripts() []string {
	var paths []string
	for _, name := range vendored.ComponentScripts() {
		paths = append(paths, "static/"+name)
	}
	return paths
}

// scriptFuncs are the template functions behind component scripts:
// needs, and neededScripts, which ui.Pages binds per page.
func scriptFuncs() template.FuncMap {
	return template.FuncMap{
		"needs":         needs,
		"neededScripts": allComponentScripts,
	}
}

// addPage parses the page's file into its own clone of base, and binds
// neededScripts there to the component scripts that page needs.
func (pages *Pages) addPage(
	base *tmpl.Template, name string, file PageFile, app fs.FS,
) error {
	fsys := file.FS
	if fsys == nil {
		fsys = app
	}
	page, err := base.Clone() // at boot, per page, not per language or request
	if err == nil {
		page, err = page.ParseFS(fsys, file.Path)
	}
	if err != nil {
		return fmt.Errorf(pageParseMsg, name, err)
	}
	scripts, err := pageScripts(name, page, pages.entry)
	if err != nil {
		return err
	}
	page.Funcs(tmpl.FuncMap{
		"neededScripts": func() []string { return scripts },
	})
	pages.pages[name] = page
	return nil
}

// pageScripts is the static paths of the component scripts the page
// called name needs, in load order, each once. A live or polling page
// gets every one, since a fragment it swaps in later may carry a field
// that needs a script nothing loaded.
func pageScripts(
	name string, page *tmpl.Template, entry string,
) ([]string, error) {
	root := page.Lookup(entry)
	if root == nil {
		return nil, nil
	}
	wanted := map[string]bool{}
	polls := false
	var problem error
	root.Inspect(func(node parse.Node) {
		switch concrete := node.(type) {
		case *parse.TextNode:
			polls = markupNeeds(concrete.Text, wanted) || polls
		case *parse.CommandNode:
			script, err := neededScript(name, concrete)
			if err != nil && problem == nil {
				problem = err
			}
			wanted[script] = wanted[script] || script != ""
		}
	})
	if problem != nil {
		return nil, problem
	}
	if polls || hasLiveRegion(page) {
		return allComponentScripts(), nil
	}
	if wanted["datetime.js"] {
		wanted["calendar.js"] = true
	}
	var paths []string
	for _, script := range vendored.ComponentScripts() {
		if wanted[script] {
			paths = append(paths, "static/"+script)
		}
	}
	return paths, nil
}

// hookScripts maps each attribute that arms a component script to that
// script. A page's markup carrying one loads its script with no needs,
// so an app's own field markup, or its own definition of a field
// partial, keeps its enhancement when the layout stops loading every
// script.
var hookScripts = map[string]string{
	"data-rst-select": "select.js",
	"data-rst-date":   "datetime.js",
	"data-rst-time":   "datetime.js",
	"data-rst-range":  "datetime.js",
}

// markupNeeds marks in wanted the script each hook in text arms, and
// reports whether text polls. HTML attribute names ignore case, so the
// match does too.
func markupNeeds(text []byte, wanted map[string]bool) (polls bool) {
	for _, match := range hookAttribute.FindAllSubmatch(text, -1) {
		wanted[hookScripts[strings.ToLower(string(match[1]))]] = true
	}
	return bytes.Contains(bytes.ToLower(text), []byte("data-poll"))
}

// hookAttribute is one of hookScripts written as an attribute: after
// whitespace and ending the name, so text that merely names a hook, or
// a longer attribute such as data-rst-select-ceiling, is not one. A
// name at the end of the text is one, since an action may follow it.
var hookAttribute = regexp.MustCompile(
	`(?i)\s(data-rst-select|data-rst-date|data-rst-time|data-rst-range)` +
		`(?:[\s=/>]|$)`)

// neededScript is the script a {{needs}} command names, "" for any
// other command.
func neededScript(page string, command *parse.CommandNode) (string, error) {
	first, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok || first.Ident != "needs" {
		return "", nil
	}
	if len(command.Args) != 2 {
		return "", fmt.Errorf(needsLiteralMsg, page)
	}
	literal, ok := command.Args[1].(*parse.StringNode)
	if !ok {
		return "", fmt.Errorf(needsLiteralMsg, page)
	}
	scripts := vendored.ComponentScripts()
	if !slices.Contains(scripts, literal.Text) {
		return "", fmt.Errorf(needsUnknownMsg, page, literal.Text,
			strings.Join(scripts, ", "))
	}
	return literal.Text, nil
}

// hasLiveRegion reports whether the page defines a region other than
// the notice, which every layout defines.
func hasLiveRegion(page *tmpl.Template) bool {
	for _, defined := range page.Templates() {
		region, ok := strings.CutPrefix(defined.Name(), "region:")
		if ok && region != live.Notice {
			return true
		}
	}
	return false
}

// reservedFunc reports whether an app function of that name would
// replace one the framework's pages depend on.
func reservedFunc(name string) bool {
	return slices.Contains(LocaleFuncs, name) ||
		slices.Contains([]string{"localizer", "withL", "needs",
			"neededScripts"}, name)
}
