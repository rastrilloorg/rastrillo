package ui

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/live"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

const (
	needsLiteralMsg = "ui: page %q: put the script or hook in quotes, " +
		"such as {{needs \"select.js\"}} or {{needs \"rst-card\"}}"
	needsUnknownMsg = "ui: page %q: needs %q, which is neither a script " +
		"nor a hook; use one of the scripts (%s) or a hook: rst- and " +
		"a component's name, such as \"rst-card\""
	needsRenderMsg = "ui: needs %q, which is neither a script nor a " +
		"hook; use one of the scripts (%s) or a hook: rst- and a " +
		"component's name, such as \"rst-card\""
)

// needs renders nothing. NewPages reads each call off the page's parse
// tree before the page runs, because the layout writes its scripts and
// stylesheet in <head>, ahead of the content that would call needs
// while rendering.
func needs(name string) (string, error) {
	if !needable(name) {
		return "", fmt.Errorf(needsRenderMsg, name,
			strings.Join(optionalScripts(), ", "))
	}
	return "", nil
}

// needable reports whether a page may ask for name: a hook whose rules
// its stylesheet must keep, or a script it loads only where it is used.
func needable(name string) bool {
	return vendored.IsHook(name) || slices.Contains(optionalScripts(), name)
}

// optionalScripts are the vendored scripts a page loads only where it
// uses them, in the order the layouts load them: live.js after busy.js,
// which hands it a live form, and the component scripts last, in their
// own order. fiesta.js loads on every page, through pageScripts;
// rastrillo.js and shell.js on every page whose layout links them.
func optionalScripts() []string {
	return append([]string{"busy.js", "live.js", "light.js", "anchor.js"},
		vendored.ComponentScripts()...)
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

// everyPageScript is Fiesta's fiesta.js: light dismiss, the switch's
// drag and the scheme menu's in-place apply. Any page can show a menu
// or a switch, so every page loads it, ahead of its optional scripts.
const everyPageScript = "fiesta.js"

// pageScriptNames is everyPageScript, then the optional scripts a
// page's templates show, in load order.
func pageScriptNames(reading pageReading) []string {
	return append([]string{everyPageScript}, plannedScripts(reading)...)
}

// allPageScripts is everyPageScript and every optional script, as
// static paths, in load order: what a render outside ui.Pages loads.
func allPageScripts() []string {
	paths := []string{"static/" + everyPageScript}
	for _, name := range optionalScripts() {
		paths = append(paths, "static/"+name)
	}
	return paths
}

// scriptFuncs are the template functions behind a page's scripts and
// stylesheet: needs, and neededScripts, pageScripts and pageStylesheet,
// which ui.Pages binds per page. Outside ui.Pages, which alone reads
// the page, they give every script and the whole stylesheet.
func scriptFuncs() template.FuncMap {
	return template.FuncMap{
		"needs":          needs,
		"neededScripts":  allComponentScripts,
		"pageScripts":    allPageScripts,
		"pageStylesheet": func() string { return "/" + stylesheet },
	}
}

// addPages adds each of the app's pages. With the app's assets, a page
// links only the scripts the app still has, and its stylesheet's slice
// once every page's hooks are known. Without them every page links the
// whole stylesheet, so no script is taken to be unchanged: none could
// be checked, and nothing is cut on the strength of it.
func (pages *Pages) addPages(base *tmpl.Template, config PagesConfig) error {
	files := appFiles{
		has:       func(string) bool { return true },
		unchanged: func(string) bool { return false },
	}
	if config.Assets != nil {
		files = appFiles{
			has:       config.Assets.Has,
			unchanged: unchangedScripts(config.Assets),
		}
	}
	plans := map[string]pagePlan{}
	for name, file := range config.Pages {
		plan, err := pages.addPage(base, name, file, config.App, files)
		if err != nil {
			return err
		}
		plans[name] = plan
	}
	if config.Assets != nil {
		pages.bindStylesheets(plans, config.Assets)
	}
	return nil
}

// addPage parses the page's file into its own clone of base, binds
// pageScripts and neededScripts there to the scripts that page needs,
// and returns its plan.
func (pages *Pages) addPage(
	base *tmpl.Template, name string, file PageFile, app fs.FS,
	files appFiles,
) (pagePlan, error) {
	fsys := file.FS
	if fsys == nil {
		fsys = app
	}
	page, err := base.Clone() // at boot, per page, not per language or request
	if err == nil {
		page, err = page.ParseFS(fsys, file.Path)
	}
	if err != nil {
		return pagePlan{}, fmt.Errorf(pageParseMsg, name, err)
	}
	plan, err := planPage(name, page, pages.entry, files)
	if err != nil {
		return pagePlan{}, err
	}
	page.Funcs(tmpl.FuncMap{
		"pageScripts":   func() []string { return plan.scripts },
		"neededScripts": func() []string { return plan.components },
	})
	pages.pages[name] = page
	return plan, nil
}

// plannedScripts is the optional scripts a page loads, by what its
// templates show, in load order:
//
//   - busy.js with a form of the page's own;
//   - light.js with a hook it lights;
//   - anchor.js with a menu;
//   - a component script where a field or a snippet arms it, and
//     calendar.js with datetime.js, which draws its month grid with it;
//   - each the page asks for with needs;
//   - live.js on a live or polling page, which loads every other one
//     too, since a fragment it swaps in later may carry a form, a field
//     or a lit hook that no template the walk reached shows: the page
//     already links the whole app's slice for the same reason.
func plannedScripts(reading pageReading) []string {
	wanted := maps.Clone(reading.wanted)
	if reading.live {
		for _, script := range optionalScripts() {
			wanted[script] = true
		}
	}
	wanted["calendar.js"] = wanted["calendar.js"] || wanted["datetime.js"]
	lit := slices.ContainsFunc(vendored.LitHooks(),
		func(hook string) bool { return reading.hooks[hook] })
	wanted["light.js"] = wanted["light.js"] || lit
	menu := reading.hooks["rst-dropdown"] || reading.hooks["rst-menus"]
	wanted["anchor.js"] = wanted["anchor.js"] || menu
	return slices.DeleteFunc(optionalScripts(), func(script string) bool {
		return !wanted[script]
	})
}

// hookScripts maps each attribute that arms a component script to that
// script: a field's data-rst- attribute, or the snippet's box, whose
// Copy button copy.js shows. A page's markup carrying one loads its
// script with no needs, so an app's own field markup, or its own
// definition of a field partial, keeps its enhancement when the layout
// stops loading every script.
var hookScripts = map[string]string{
	"data-rst-select": "select.js",
	"data-rst-date":   "datetime.js",
	"data-rst-time":   "datetime.js",
	"data-rst-range":  "datetime.js",
	"rst-snippet":     "copy.js",
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
	`(?i)\s(data-rst-select|data-rst-date|data-rst-time|data-rst-range|` +
		`rst-snippet)(?:[\s=/>]|$)`)

// neededName is the script or hook a {{needs}} command names, "" for
// any other command.
func neededName(page string, command *parse.CommandNode) (string, error) {
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
	if !needable(literal.Text) {
		return "", fmt.Errorf(needsUnknownMsg, page, literal.Text,
			strings.Join(optionalScripts(), ", "))
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
			"neededScripts", "pageScripts", "pageStylesheet"}, name)
}
