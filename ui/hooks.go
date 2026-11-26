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
// scripts it loads, as static paths (fiesta.js, then its optional
// ones), and whether it is live, which links the whole app's slice in
// place of its own. components are the component scripts among them:
// what neededScripts gives a layout that links busy.js, live.js,
// light.js and anchor.js itself, so it never loads one of them twice.
// fragments are the hooks its regions show when one is sent alone,
// which the whole app's slice keeps too.
type pagePlan struct {
	hooks      []string
	fragments  []string
	scripts    []string
	components []string
	live       bool
}

// appFiles is what a page's plan asks of the app's static files: has
// reports whether the app still has a file, so a page never links a
// script the app has deleted, and unchanged whether a vendored script
// is served as Rastrillo wrote it, rather than as the app edited it.
type appFiles struct {
	has, unchanged func(name string) bool
}

// planPage reads the page called name for its plan. The hooks include
// those rastrillo.js and the page's scripts create at run time, which
// no template shows. A page that loads a script the app has edited
// runs code of its own.
func planPage(
	name string, page *tmpl.Template, entry string, files appFiles,
) (pagePlan, error) {
	reading, err := readPage(name, page, entry)
	if err != nil {
		return pagePlan{}, err
	}
	plan := pagePlan{live: reading.live}
	hooks := maps.Clone(reading.hooks)
	components := vendored.ComponentScripts()
	for _, script := range pageScriptNames(reading) {
		if !files.has("static/" + script) {
			continue
		}
		plan.scripts = append(plan.scripts, "static/"+script)
		if slices.Contains(components, script) {
			plan.components = append(plan.components, "static/"+script)
		}
	}
	creators := append([]string{"static/rastrillo.js"}, plan.scripts...)
	created := map[string]bool{}
	for _, script := range creators {
		for _, hook := range vendored.CreatedHooks(path.Base(script)) {
			hooks[hook], created[hook] = true, true
		}
	}
	loaded := slices.Concat(
		slices.Sorted(maps.Keys(reading.scripts.named)), plan.scripts)
	edited := func(script string) bool { return !files.unchanged(script) }
	if slices.ContainsFunc(loaded, edited) {
		reading.scripts.own = true
	}
	plan.hooks = slices.Concat(slices.Sorted(maps.Keys(hooks)),
		reading.shownValues(created), reading.shownClasses(created))
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
	hooks      map[string]bool            // every hook the templates name
	values     map[string]map[string]bool // each hook's literal values
	unknown    map[string]bool            // hooks written with any value
	classes    map[string]bool            // hooks written as classes
	anyClass   bool                       // a class it cannot read
	open       map[string]bool            // hooks a text ends at
	following  map[string]bool            // values after an action
	anyFollows bool                       // one of them not whole
	scripts    scriptReading              // the scripts it loads
	wanted     map[string]bool            // the scripts it asks for
	live       bool                       // a region or a poll
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
	roots := map[string]*parse.ListNode{}
	root.InspectNamed(func(template string, node parse.Node) {
		if list, ok := node.(*parse.ListNode); ok && roots[template] == nil {
			roots[template] = list
		}
		switch concrete := node.(type) {
		case *parse.TextNode:
			reading.readMarkup(template, concrete.Text)
		case *parse.StringNode:
			reading.addHooks([]byte(concrete.Text))
			reading.anyValueIn([]byte(concrete.Text))
			reading.anyClassIn([]byte(concrete.Text))
		case *parse.CommandNode:
			err := reading.readCommand(name, template, concrete)
			if problem == nil {
				problem = err
			}
		}
		reading.scripts.readNode(template, node)
	})
	reading.scripts.settle()
	reading.anyClass = reading.anyClass || writesUnseenClass(roots, entry)
	if problem != nil {
		return pageReading{}, problem
	}
	reading.live = reading.live || hasLiveRegion(page)
	return reading, nil
}

// readCommand reads one of the page's commands, in the template named
// template: a script it needs, the hooks a framework function writes,
// and a range over the scripts it loads.
func (reading *pageReading) readCommand(
	page, template string, command *parse.CommandNode,
) error {
	needed, err := neededName(page, command)
	if needed != "" && !vendored.IsHook(needed) {
		reading.wanted[needed] = true
	}
	reading.addWrittenHooks(command)
	reading.scripts.readCommand(template, command)
	return err
}

// menuTemplates hold the layout's language and scheme menus, whose
// forms busy.js has nothing to do for: each submits a single choice.
var menuTemplates = map[string]bool{"locale-menu": true, "scheme-menu": true}

// readMarkup reads a run of the page's markup, in the template named
// template. Any "<form" outside the menus loads busy.js unless it opts
// out; text that only looks like one costs a script, never a feature.
func (reading *pageReading) readMarkup(template string, text []byte) {
	reading.addHooks(text)
	reading.readValues(text)
	reading.readClasses(text)
	reading.scripts.readText(template, text)
	reading.live = markupNeeds(text, reading.wanted) || reading.live
	if guardedForm(text) && !menuTemplates[template] {
		reading.wanted["busy.js"] = true
	}
}

// guardedForm reports whether text opens a form busy.js would guard:
// a "<form" whose own tag does not opt out with data-busy="false", as
// the account card's sign-out does (a POST that redirects: a spinner
// adds nothing). The tag is read up to its ">" or the end of this run
// of markup, so an opt-out after an action in the tag is not seen and
// the form counts as guarded.
func guardedForm(text []byte) bool {
	rest := text
	for {
		at := bytes.Index(bytes.ToLower(rest), []byte("<form"))
		if at < 0 {
			return false
		}
		optedOut, after, closed := readFormTag(rest[at+len("<form"):])
		if !optedOut {
			return true
		}
		if !closed {
			return false
		}
		rest = after
	}
}

// readFormTag reads a form's start tag attribute by attribute, as HTML
// does, and reports whether its first data-busy attribute is busy.js's
// opt-out (the name in any case, the value exactly "false", as busy.js
// compares it), the markup after the tag, and whether the tag closed
// in this run. An attribute it cannot read to its end, such as a value
// cut by a template action, ends the reading with what was settled.
func readFormTag(tag []byte) (optedOut bool, after []byte, closed bool) {
	settled := false
	for {
		tag = bytes.TrimLeft(tag, htmlSpace+"/")
		if len(tag) == 0 {
			return optedOut, nil, false
		}
		if tag[0] == '>' {
			return optedOut, tag[1:], true
		}
		name, value, rest, complete := readAttribute(tag)
		if !complete {
			return optedOut, nil, false
		}
		if !settled && strings.EqualFold(name, "data-busy") {
			settled, optedOut = true, value == "false"
		}
		tag = rest
	}
}

// htmlSpace is what separates attributes in a tag.
const htmlSpace = " \t\n\r\f"

// readAttribute reads one attribute from the start of tag: its name,
// its value (empty when it has none) and what follows. complete is
// false when the run ends before the attribute does.
func readAttribute(tag []byte) (name, value string, rest []byte,
	complete bool) {
	end := bytes.IndexAny(tag, htmlSpace+"/>=")
	if end < 0 {
		return "", "", nil, false
	}
	name, rest = string(tag[:end]), bytes.TrimLeft(tag[end:], htmlSpace)
	if len(rest) == 0 {
		return name, "", nil, false
	}
	if rest[0] != '=' {
		return name, "", rest, true
	}
	rest = bytes.TrimLeft(rest[1:], htmlSpace)
	if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
		value, after, found := bytes.Cut(rest[1:], rest[:1])
		return name, string(value), after, found
	}
	end = bytes.IndexAny(rest, htmlSpace+">")
	if end < 0 {
		return name, "", nil, false
	}
	return name, string(rest[:end]), rest[end:], true
}

// addHooks reads text for hooks. Attribute values count: a class list,
// a menu's name="rst-menus", and the classes an option's data-rst-class
// gives the rows select.js builds.
func (reading *pageReading) addHooks(text []byte) {
	for _, hook := range vendored.HooksIn(text) {
		reading.hooks[hook] = true
	}
}

// addWrittenHooks adds the hooks a framework function or method writes
// when the command calls one, such as the stage shell's {{stageArt
// "rastrillo"}} or pow's {{.Proof.StatusLine ...}}: the template shows
// only the call, never the markup it returns.
func (reading *pageReading) addWrittenHooks(command *parse.CommandNode) {
	var hooks []string
	switch called := command.Args[0].(type) {
	case *parse.IdentifierNode:
		hooks = vendored.WrittenHooks(called.Ident)
	case *parse.FieldNode:
		hooks = methodHooks(called.Ident)
	case *parse.VariableNode:
		hooks = methodHooks(called.Ident[1:])
	case *parse.ChainNode:
		hooks = methodHooks(called.Field)
	}
	for _, hook := range hooks {
		reading.hooks[hook] = true
		reading.anyValue(hook)
		reading.classes = setOf(reading.classes)
		reading.classes[hook] = true
	}
}

// methodHooks is what the method at the end of a field chain writes:
// .Proof.StatusLine, $proof.StatusLine, (.Proof).StatusLine.
func methodHooks(chain []string) []string {
	if len(chain) == 0 {
		return nil
	}
	return vendored.MethodHooks(chain[len(chain)-1])
}
