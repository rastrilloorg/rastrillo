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
	own      bool            // a script not Rastrillo's, code or a handler
	named    map[string]bool // Rastrillo's scripts it loads by name
	pending  bool            // a script tag whose src actions write
	quote    string          // the quote that src's value opened with
	inTag    bool            // inside a script's start tag
	tagQuote byte            // the quote open there, 0 for none
	ours     bool            // an action names one of Rastrillo's scripts
	foreign  bool            // one may write any other src
	inRange  bool            // in a range over pageScripts or neededScripts
	template string          // the template the last node was in
}

var (
	scriptTag = regexp.MustCompile(`(?i)<script\b`)
	// handler is an event handler attribute or a javascript: URL. Text
	// that only looks like one costs the cut, never a rule.
	handler   = regexp.MustCompile(`(?i)(\son[a-z]+\s*=|javascript:)`)
	srcOpen   = regexp.MustCompile(`(?i)\ssrc\s*=\s*(["']?)$`)
	srcValue  = regexp.MustCompile(`(?i)\ssrc\s*=\s*["']?([^"'\s>]+)`)
	typeValue = regexp.MustCompile(`(?i)\stype\s*=\s*["']?([^"'\s>]+)`)
)

// dataTypes are script types a browser reads as data, not code.
var dataTypes = map[string]bool{
	"application/json": true, "application/ld+json": true,
	"importmap": true, "speculationrules": true,
}

// readText reads a run of markup in the named template: the end of a
// script tag's src that actions wrote, and the script tags and handlers
// in text. Text that goes on with the src, rather than closing it, is
// a src the actions do not name whole.
func (scripts *scriptReading) readText(template string, text []byte) {
	scripts.enter(template)
	scripts.foreign = scripts.foreign || !scripts.closesSource(text)
	scripts.settle()
	scripts.own = scripts.own || handler.Match(text)
	scripts.inTag, scripts.tagQuote = afterText(
		text, scripts.inTag, scripts.tagQuote)
	for _, at := range scriptTag.FindAllIndex(text, -1) {
		tag := text[at[1]:]
		end := bytes.IndexByte(tag, '>')
		opening := srcOpen.FindSubmatch(tag)
		switch {
		case end < 0 && opening != nil:
			scripts.pending, scripts.quote = true, string(opening[1])
		case end < 0:
			scripts.own = true
		default:
			scripts.readWholeTag(tag[:end])
		}
	}
}

// readBranch reads an if, range or with. One that opens inside a
// script's start tag, or whose branch ends inside one, may end the src
// in one branch and write another in the next, so the tag is the app's.
func (scripts *scriptReading) readBranch(
	template string, branch *parse.BranchNode,
) {
	scripts.enter(template)
	scripts.own = scripts.own || scripts.inTag ||
		endsInScriptTag(branch.List) || endsInScriptTag(branch.ElseList)
}

// endsInScriptTag reports whether a branch's own text leaves a script's
// start tag open. Its actions write no tag's end, and a branch nested
// in it is read when the walk reaches it.
func endsInScriptTag(list *parse.ListNode) bool {
	if list == nil {
		return false
	}
	open, quote := false, byte(0)
	for _, node := range list.Nodes {
		if text, ok := node.(*parse.TextNode); ok {
			open, quote = afterText(text.Text, open, quote)
		}
	}
	return open
}

// afterText reports whether a script's start tag is open after text,
// given whether one was open before it, and which quote is open in it.
func afterText(text []byte, open bool, quote byte) (bool, byte) {
	for {
		if open {
			end, after := tagEnd(text, quote)
			if end < 0 {
				return true, after
			}
			text = text[end+1:]
		}
		at := scriptTag.FindIndex(text)
		if at == nil {
			return false, 0
		}
		text, open, quote = text[at[1]:], true, 0
	}
}

// tagEnd is the index of the > that ends a start tag in text, outside
// any quoted value, or -1 and the quote still open at the text's end.
func tagEnd(text []byte, quote byte) (int, byte) {
	for index, character := range text {
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			}
		case character == '"' || character == '\'':
			quote = character
		case character == '>':
			return index, 0
		}
	}
	return -1, quote
}

// closesSource reports whether text, after an action, ends the src
// value the pending tag's actions write: its closing quote, or for an
// unquoted value, a space or the tag's end.
func (scripts *scriptReading) closesSource(text []byte) bool {
	if !scripts.pending {
		return true
	}
	if scripts.quote != "" {
		return bytes.HasPrefix(text, []byte(scripts.quote))
	}
	return bytes.ContainsAny(text[:1], htmlSpaces+">")
}

// readCommand reads an action's command for a range over the scripts a
// page loads, whose element a script tag's src may name.
func (scripts *scriptReading) readCommand(
	template string, command *parse.CommandNode,
) {
	scripts.enter(template)
	first, ok := command.Args[0].(*parse.IdentifierNode)
	if ok && (first.Ident == "pageScripts" || first.Ident == "neededScripts") {
		scripts.inRange = true
	}
}

// readNode reads one of the page's nodes, in the template named
// template, for what it writes into a script tag: an action, a
// template call, or a branch.
func (scripts *scriptReading) readNode(template string, node parse.Node) {
	switch concrete := node.(type) {
	case *parse.ActionNode:
		scripts.readAction(template, concrete.Pipe)
	case *parse.TemplateNode:
		scripts.readTemplateCall(template)
	case *parse.IfNode:
		scripts.readBranch(template, &concrete.BranchNode)
	case *parse.RangeNode:
		scripts.readBranch(template, &concrete.BranchNode)
	case *parse.WithNode:
		scripts.readBranch(template, &concrete.BranchNode)
	}
}

// readAction reads an action that may write a pending script tag's
// src. The tag is Rastrillo's only when every such action names one of
// its scripts: an action in any branch that writes another, or data,
// runs the app's script whenever that branch is taken.
func (scripts *scriptReading) readAction(
	template string, pipe *parse.PipeNode,
) {
	scripts.enter(template)
	if !scripts.pending {
		return
	}
	name, ours := scripts.sourceOf(pipe)
	scripts.ours = scripts.ours || ours
	scripts.foreign = scripts.foreign || !ours
	if ours && name != "" {
		scripts.load(name)
	}
}

// readTemplateCall reads a {{template}} call, which writes what the
// walk reads in another template: inside a src, a source it cannot
// name, and inside a script's start tag, attributes it cannot read.
func (scripts *scriptReading) readTemplateCall(template string) {
	scripts.enter(template)
	scripts.foreign = scripts.foreign || scripts.pending
	scripts.own = scripts.own || scripts.inTag
}

// sourceOf reads a pipeline that writes a script's src for one of
// Rastrillo's scripts: asset naming one, or the element of a range
// over the scripts a page loads, as it is or through asset, any of
// them through path, as an argument or piped. name is the script's
// static path, "" for the range's element, which planPage counts among
// the page's scripts.
func (scripts *scriptReading) sourceOf(
	pipe *parse.PipeNode,
) (name string, ours bool) {
	commands := pipe.Cmds
	if len(pipe.Decl) > 0 || len(commands) == 0 || len(commands) > 2 {
		return "", false
	}
	if len(commands) == 2 && !throughPath(commands[1], 2) {
		return "", false
	}
	command := commands[0]
	if len(commands) == 1 && throughPath(command, 3) {
		inner, ok := command.Args[2].(*parse.PipeNode)
		if !ok || len(inner.Decl) > 0 || len(inner.Cmds) != 1 {
			return "", false
		}
		command = inner.Cmds[0]
	}
	return scripts.scriptOf(command)
}

// throughPath reports whether command is path $, with arguments in all
// counting path's own name: 3 with the source as an argument, 2 with
// it piped in.
func throughPath(command *parse.CommandNode, arguments int) bool {
	function, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok || function.Ident != "path" || len(command.Args) != arguments {
		return false
	}
	root, ok := command.Args[1].(*parse.VariableNode)
	return ok && len(root.Ident) == 1 && root.Ident[0] == "$"
}

// scriptOf reads a command that names a script: asset with one of
// Rastrillo's, or the range's element, as it is or through asset.
func (scripts *scriptReading) scriptOf(
	command *parse.CommandNode,
) (name string, ours bool) {
	switch first := command.Args[0].(type) {
	case *parse.DotNode:
		return "", len(command.Args) == 1 && scripts.inRange
	case *parse.IdentifierNode:
		if first.Ident != "asset" || len(command.Args) != 2 {
			return "", false
		}
		switch argument := command.Args[1].(type) {
		case *parse.StringNode:
			return argument.Text, ourAsset(argument.Text)
		case *parse.DotNode:
			return "", scripts.inRange
		}
	}
	return "", false
}

// enter settles a script tag left open when the walk moves to another
// template, which may write the rest of it: the tag is the app's.
func (scripts *scriptReading) enter(template string) {
	if template != scripts.template {
		scripts.foreign = scripts.foreign || scripts.pending
		scripts.own = scripts.own || scripts.inTag
		scripts.inTag, scripts.tagQuote = false, 0
		scripts.settle()
		scripts.template = template
	}
}

// settle decides a script tag whose src actions wrote: Rastrillo's only
// when they named its scripts and nothing else.
func (scripts *scriptReading) settle() {
	if scripts.pending {
		scripts.own = scripts.own || scripts.foreign || !scripts.ours
		scripts.inRange = false
	}
	scripts.pending, scripts.ours, scripts.foreign = false, false, false
}

// readWholeTag reads a script tag whose attributes the text holds
// whole: it loads one of Rastrillo's scripts, holds data rather than
// code, or runs code of the app's own.
func (scripts *scriptReading) readWholeTag(attributes []byte) {
	if source := srcValue.FindSubmatch(attributes); source != nil {
		name, ours := ourPath(string(source[1]))
		if ours {
			scripts.load(name)
		}
		scripts.own = scripts.own || !ours
		return
	}
	kind := typeValue.FindSubmatch(attributes)
	data := kind != nil && dataTypes[strings.ToLower(string(kind[1]))]
	scripts.own = scripts.own || !data
}

// load records that the page loads one of Rastrillo's scripts by name,
// as its static path: an app's edit of one is the app's code.
func (scripts *scriptReading) load(name string) {
	if scripts.named == nil {
		scripts.named = map[string]bool{}
	}
	scripts.named[name] = true
}

// ourAsset reports whether an asset name is one of the scripts
// Rastrillo vendors, as vendored.Is names them: static/ and the file.
func ourAsset(name string) bool {
	return path.Ext(name) == ".js" && vendored.Is(name)
}

// ourPath reports whether a script's literal src is exactly where
// Rastrillo serves one of its scripts, /static/ and the file's name,
// and names that script's static path. A file of the same name
// anywhere else is the app's, and may hold its code, and so is a
// fingerprinted name, which Assets serves from a file of that exact
// name ahead of the vendored one.
func ourPath(source string) (string, bool) {
	name, served := strings.CutPrefix(source, "/static/")
	ours := served && !strings.Contains(name, "/") &&
		ourAsset("static/"+name)
	return "static/" + name, ours
}
