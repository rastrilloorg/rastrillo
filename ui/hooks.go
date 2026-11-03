package ui

import (
	"bytes"
	"maps"
	"path"
	"slices"
	"strings"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// A pagePlan is what a page shows and loads, read off its templates at
// boot: the hooks its stylesheet slice must keep the rules for, the
// optional scripts it loads, as static paths, and whether it is live,
// which links the whole app's slice in place of its own. components
// are the component scripts among them: what neededScripts gives a
// layout that links busy.js, live.js, light.js and anchor.js itself,
// so it never loads one of them twice. fragments are the hooks its
// regions show when one is sent alone, which the whole app's slice
// keeps too.
type pagePlan struct {
	hooks      []string
	fragments  []string
	scripts    []string
	components []string
	live       bool
}

// planPage reads the page called name for its plan. has reports whether
// the app still has a file, so a page never links a script the app has
// deleted. The hooks include those rastrillo.js and the page's scripts
// create at run time, which no template shows.
func planPage(
	name string, page *tmpl.Template, entry string, has func(string) bool,
) (pagePlan, error) {
	reading, err := readPage(name, page, entry)
	if err != nil {
		return pagePlan{}, err
	}
	plan := pagePlan{live: reading.live}
	hooks := maps.Clone(reading.hooks)
	components := vendored.ComponentScripts()
	for _, script := range plannedScripts(reading) {
		if !has("static/" + script) {
			continue
		}
		plan.scripts = append(plan.scripts, "static/"+script)
		if slices.Contains(components, script) {
			plan.components = append(plan.components, "static/"+script)
		}
	}
	creators := append([]string{"static/rastrillo.js"}, plan.scripts...)
	for _, script := range creators {
		for _, hook := range vendored.CreatedHooks(path.Base(script)) {
			hooks[hook] = true
		}
	}
	plan.hooks = slices.Sorted(maps.Keys(hooks))
	plan.fragments, err = regionHooks(name, page)
	if err != nil {
		return pagePlan{}, err
	}
	return plan, nil
}

// regionHooks is every hook the page's regions can show, each region
// read from its own definition: live.ExecuteRegions sends any
// region:<name> alone, as a fragment, whether or not the page's first
// render calls it.
func regionHooks(name string, page *tmpl.Template) ([]string, error) {
	hooks := map[string]bool{}
	for _, defined := range page.Templates() {
		if !strings.HasPrefix(defined.Name(), "region:") {
			continue
		}
		reading, err := readPage(name, page, defined.Name())
		if err != nil {
			return nil, err
		}
		maps.Copy(hooks, reading.hooks)
	}
	return slices.Sorted(maps.Keys(hooks)), nil
}

// pageHooks is every hook the page called name can show: those named in
// its templates' markup and string arguments, in every branch, taken or
// not, and those it asks for with needs.
func pageHooks(
	name string, page *tmpl.Template, entry string,
) (map[string]bool, error) {
	reading, err := readPage(name, page, entry)
	return reading.hooks, err
}

// A pageReading is what one walk of a page's templates finds.
type pageReading struct {
	hooks  map[string]bool // every hook the templates name
	wanted map[string]bool // the scripts its markup and needs ask for
	live   bool            // a region besides the notice, or a poll
}

// readPage walks every template the page reaches from entry.
func readPage(
	name string, page *tmpl.Template, entry string,
) (pageReading, error) {
	reading := pageReading{hooks: map[string]bool{}, wanted: map[string]bool{}}
	root := page.Lookup(entry)
	if root == nil {
		return reading, nil
	}
	var problem error
	root.InspectNamed(func(template string, node parse.Node) {
		switch concrete := node.(type) {
		case *parse.TextNode:
			reading.readMarkup(template, concrete.Text)
		case *parse.StringNode:
			reading.addHooks([]byte(concrete.Text))
		case *parse.CommandNode:
			needed, err := neededName(name, concrete)
			if problem == nil {
				problem = err
			}
			if needed != "" && !vendored.IsHook(needed) {
				reading.wanted[needed] = true
			}
			reading.addWrittenHooks(concrete)
		}
	})
	if problem != nil {
		return pageReading{}, problem
	}
	reading.live = reading.live || hasLiveRegion(page)
	return reading, nil
}

// menuTemplates hold the layout's language and scheme menus, whose
// forms busy.js has nothing to do for: each submits a single choice.
var menuTemplates = map[string]bool{"locale-menu": true, "scheme-menu": true}

// readMarkup reads a run of the page's markup, in the template named
// template. Any "<form" outside the menus loads busy.js; text that only
// looks like one costs a script, never a feature.
func (reading *pageReading) readMarkup(template string, text []byte) {
	reading.addHooks(text)
	reading.live = markupNeeds(text, reading.wanted) || reading.live
	form := bytes.Contains(bytes.ToLower(text), []byte("<form"))
	if form && !menuTemplates[template] {
		reading.wanted["busy.js"] = true
	}
}

// addHooks reads text for hooks. Attribute values count: a class list,
// a menu's name="rst-menus", and the classes an option's data-rst-class
// gives the rows select.js builds.
func (reading *pageReading) addHooks(text []byte) {
	for _, hook := range vendored.HooksIn(text) {
		reading.hooks[hook] = true
	}
}

// addWrittenHooks adds the hooks a framework function writes when the
// command calls one, such as the stage shell's {{stageArt "rastrillo"}}:
// the template shows only the call, never the markup it returns.
func (reading *pageReading) addWrittenHooks(command *parse.CommandNode) {
	function, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok {
		return
	}
	for _, hook := range vendored.WrittenHooks(function.Ident) {
		reading.hooks[hook] = true
	}
}
