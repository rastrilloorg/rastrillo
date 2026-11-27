package ui

import (
	"html/template"
	"maps"
	"slices"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// ComponentStylesheetsShown returns the component stylesheets, such as
// data.css, that a page made of layout, the shared files and page
// would link under ui.Pages, read as ui.Pages reads it: from the
// layout through every template it calls, counting markup, string
// arguments, the framework functions' hooks and needs. rastrillo
// doctor asks it of an app's templates, whose own functions it does
// not have, so each stands in as one that writes nothing: only what
// the templates say is read.
func ComponentStylesheetsShown(
	layout string, shared []string, page string,
) ([]string, error) {
	sources := slices.Concat([]string{layout}, shared, []string{page})
	funcs := Funcs()
	maps.Copy(funcs, standIns(sources, funcs))
	set, err := tmpl.New("layout").Funcs(funcs).
		ParseFS(Templates(), "*.html")
	for _, source := range sources {
		if err == nil {
			set, err = set.Parse(source)
		}
	}
	if err != nil {
		return nil, err
	}
	reading, err := readPage("doctor", set, "layout")
	shown := slices.Collect(maps.Keys(reading.hooks))
	return vendored.NeededStylesheets(shown), err
}

// builtins are the functions every template has, never stood in for.
var builtins = []string{"and", "call", "eq", "ge", "gt", "html", "index",
	"js", "le", "len", "lt", "ne", "not", "or", "print", "printf",
	"println", "slice", "urlquery"}

// standIns is a function that writes nothing for each name sources
// call that funcs and the builtins lack.
func standIns(sources []string, funcs template.FuncMap) template.FuncMap {
	stands := template.FuncMap{}
	for _, source := range sources {
		tree := parse.New("source")
		tree.Mode = parse.SkipFuncCheck
		trees := map[string]*parse.Tree{}
		if _, err := tree.Parse(source, "{{", "}}", trees); err != nil {
			continue
		}
		for _, parsed := range trees {
			for _, name := range calledNames(parsed.Root) {
				if funcs[name] == nil && !slices.Contains(builtins, name) {
					stands[name] = func(...any) any { return nil }
				}
			}
		}
	}
	return stands
}

// calledNames is every function name node calls, in every branch. An
// if with no else holds a nil list, and a template call with no data a
// nil pipe, each a typed nil in the interface.
func calledNames(node parse.Node) []string {
	var names []string
	if list, ok := node.(*parse.ListNode); ok && list == nil {
		return nil
	}
	if pipe, ok := node.(*parse.PipeNode); ok && pipe == nil {
		return nil
	}
	switch concrete := node.(type) {
	case *parse.IdentifierNode:
		names = append(names, concrete.Ident)
	case *parse.ListNode:
		for _, child := range concrete.Nodes {
			names = append(names, calledNames(child)...)
		}
	case *parse.ActionNode:
		names = calledNames(concrete.Pipe)
	case *parse.TemplateNode:
		names = calledNames(concrete.Pipe)
	case *parse.PipeNode:
		for _, command := range concrete.Cmds {
			names = append(names, calledNames(command)...)
		}
	case *parse.CommandNode:
		for _, argument := range concrete.Args {
			names = append(names, calledNames(argument)...)
		}
	case *parse.ChainNode:
		names = calledNames(concrete.Node)
	case *parse.IfNode:
		names = branchNames(&concrete.BranchNode)
	case *parse.RangeNode:
		names = branchNames(&concrete.BranchNode)
	case *parse.WithNode:
		names = branchNames(&concrete.BranchNode)
	}
	return names
}

func branchNames(branch *parse.BranchNode) []string {
	return slices.Concat(calledNames(branch.Pipe), calledNames(branch.List),
		calledNames(branch.ElseList))
}
