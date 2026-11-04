package ui

import (
	"bytes"
	"path"
	"regexp"
	"strings"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// scriptReading follows the scripts a page's templates load, to tell a
// page that runs only Rastrillo's scripts from one that runs code of
// its own, which may write any value of any hook.
type scriptReading struct {
	own      bool   // a script not Rastrillo's, inline code or a handler
	pending  bool   // a script tag whose src an action writes
	ours     bool   // that action names one of Rastrillo's scripts
	inRange  bool   // inside a range over pageScripts or neededScripts
	template string // the template the last node was in
}

var (
	scriptTag = regexp.MustCompile(`(?i)<script\b`)
	// handler is an event handler attribute or a javascript: URL. Text
	// that only looks like one costs the cut, never a rule.
	handler   = regexp.MustCompile(`(?i)(\son[a-z]+\s*=|javascript:)`)
	srcOpen   = regexp.MustCompile(`(?i)\ssrc\s*=\s*["']?$`)
	srcValue  = regexp.MustCompile(`(?i)\ssrc\s*=\s*["']?([^"'\s>]+)`)
	typeValue = regexp.MustCompile(`(?i)\stype\s*=\s*["']?([^"'\s>]+)`)
)

// dataTypes are script types a browser reads as data, not code.
var dataTypes = map[string]bool{
	"application/json": true, "application/ld+json": true,
	"importmap": true, "speculationrules": true,
}

// readText reads a run of markup in the named template: a script tag a
// previous action finished, and the script tags and handlers in text.
func (scripts *scriptReading) readText(template string, text []byte) {
	scripts.enter(template)
	scripts.settle()
	scripts.own = scripts.own || handler.Match(text)
	for _, at := range scriptTag.FindAllIndex(text, -1) {
		tag := text[at[1]:]
		end := bytes.IndexByte(tag, '>')
		switch {
		case end < 0 && srcOpen.Match(tag):
			scripts.pending = true
		case end < 0:
			scripts.own = true
		default:
			scripts.own = scripts.own || !oursOrData(tag[:end])
		}
	}
}

// readCommand reads an action's command: a range over the scripts a
// page loads, or asset naming one of Rastrillo's scripts, or the
// range's own element, with asset or as it is.
func (scripts *scriptReading) readCommand(
	template string, command *parse.CommandNode,
) {
	scripts.enter(template)
	if _, dot := command.Args[0].(*parse.DotNode); dot && scripts.pending {
		scripts.ours = scripts.ours || scripts.inRange
	}
	first, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok {
		return
	}
	switch first.Ident {
	case "pageScripts", "neededScripts":
		scripts.inRange = true
	case "asset":
		if len(command.Args) != 2 || !scripts.pending {
			return
		}
		switch argument := command.Args[1].(type) {
		case *parse.StringNode:
			scripts.ours = scripts.ours || ourScript(argument.Text)
		case *parse.DotNode:
			scripts.ours = scripts.ours || scripts.inRange
		}
	}
}

// enter settles a script tag left open when the walk moves to another
// template.
func (scripts *scriptReading) enter(template string) {
	if template != scripts.template {
		scripts.settle()
		scripts.template = template
	}
}

// settle decides a script tag an action finished: Rastrillo's only
// when that action named one of its scripts.
func (scripts *scriptReading) settle() {
	if scripts.pending {
		scripts.own = scripts.own || !scripts.ours
		scripts.inRange = false
	}
	scripts.pending, scripts.ours = false, false
}

// oursOrData reports whether a whole script tag's attributes load one
// of Rastrillo's scripts or hold data rather than code.
func oursOrData(attributes []byte) bool {
	if source := srcValue.FindSubmatch(attributes); source != nil {
		return ourScript(string(source[1]))
	}
	kind := typeValue.FindSubmatch(attributes)
	return kind != nil && dataTypes[strings.ToLower(string(kind[1]))]
}

// ourScript reports whether a script's path names one of the scripts
// Rastrillo vendors.
func ourScript(source string) bool {
	name := path.Base(source)
	return path.Ext(name) == ".js" && vendored.Is("static/"+name)
}
