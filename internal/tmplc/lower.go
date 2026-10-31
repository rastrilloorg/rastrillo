package tmplc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// lowerer lowers the templates one entry reaches in its escaped trees.
type lowerer struct {
	file    *file
	set     tmpl.CapturedSet
	trees   map[string]*parse.Tree
	missing string // the tmplrt MissingKey constant
	// checked holds, for each template looked at, why it is unsupported,
	// or "" if it is not or is still being looked at, which a call cycle
	// would otherwise recurse on.
	checked map[string]string
	bodies  map[string]*body

	// The state of the template being lowered. breaks has the innermost
	// range last: "" ends a range body, a label a range statement.
	cur    *body
	scope  []binding
	breaks []string
	temps  int
}

// binding is a template variable in scope, and its Go variable.
type binding struct{ name, goName string }

// template lowers the template called name and every template it calls.
func (lowerer *lowerer) template(name string) (*body, error) {
	lowerer.checked[name] = ""
	tree := lowerer.trees[name]
	if tree == nil || tree.Root == nil {
		err := unsupported(fmt.Sprintf(
			"calls %q, which is not defined", baseName(name)))
		lowerer.checked[name] = err.Error()
		return nil, err
	}
	lowered := &body{
		name: name, fn: lowerer.fnName(name), funcs: map[string]*fn{},
	}
	lowerer.bodies[name] = lowered
	saved, savedScope, savedBreaks :=
		lowerer.cur, lowerer.scope, lowerer.breaks
	lowerer.cur, lowerer.scope, lowerer.breaks =
		lowered, []binding{{"$", "root"}}, nil
	code, err := lowerer.list(tree.Root)
	lowerer.cur, lowerer.scope, lowerer.breaks =
		saved, savedScope, savedBreaks
	if err != nil {
		lowerer.checked[name] = err.Error()
		return nil, err
	}
	lowered.code = code
	return lowered, nil
}

// fnName is the Go function for the template called name: a hash of
// everything that decides its code, which is its escaped tree, the trees
// of every template it reaches, the set's missingkey option and how the
// functions those trees use are called. Equal templates in two entries
// or two sets share one function, and a cycle of calls is no problem
// for the hash, as it would be for a hash of the code.
func (lowerer *lowerer) fnName(name string) string {
	hash := sha256.New()
	field := func(text string) {
		fmt.Fprintf(hash, "%d:%s;", len(text), text)
	}
	field("tmplc body 1")
	field(name)
	field(lowerer.missing)
	reached, idents := lowerer.reach(name)
	for _, callee := range slices.Sorted(maps.Keys(reached)) {
		field(callee)
		if tree := lowerer.trees[callee]; tree != nil && tree.Root != nil {
			field(tree.Root.String())
		} else {
			field("undefined")
		}
	}
	for _, ident := range slices.Sorted(maps.Keys(idents)) {
		field(ident)
		if sig, ok := lowerer.set.Funcs[ident]; ok {
			field(lowerer.file.fn(ident, sig).field)
		}
	}
	return "exec_" + hex.EncodeToString(hash.Sum(nil))[:prefixLen]
}

// reach is the templates the one called name reaches, itself included,
// and the identifiers their trees use.
func (lowerer *lowerer) reach(
	name string,
) (reached, idents map[string]bool) {
	reached = map[string]bool{name: true}
	idents = map[string]bool{}
	queue := []string{name}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		tree := lowerer.trees[next]
		if tree == nil || tree.Root == nil {
			continue
		}
		walk(tree.Root, func(node parse.Node) {
			switch node := node.(type) {
			case *parse.TemplateNode:
				if !reached[node.Name] {
					reached[node.Name] = true
					queue = append(queue, node.Name)
				}
			case *parse.IdentifierNode:
				idents[node.Ident] = true
			}
		})
	}
	return reached, idents
}

// walk calls visit for node and every node under it.
func walk(node parse.Node, visit func(parse.Node)) {
	if node == nil {
		return
	}
	visit(node)
	switch node := node.(type) {
	case *parse.ListNode:
		if node == nil {
			return
		}
		for _, child := range node.Nodes {
			walk(child, visit)
		}
	case *parse.ActionNode:
		walk(node.Pipe, visit)
	case *parse.IfNode:
		walkBranch(&node.BranchNode, visit)
	case *parse.WithNode:
		walkBranch(&node.BranchNode, visit)
	case *parse.RangeNode:
		walkBranch(&node.BranchNode, visit)
	case *parse.TemplateNode:
		if node.Pipe != nil {
			walk(node.Pipe, visit)
		}
	case *parse.PipeNode:
		if node == nil {
			return
		}
		for _, command := range node.Cmds {
			walk(command, visit)
		}
	case *parse.CommandNode:
		for _, argument := range node.Args {
			walk(argument, visit)
		}
	case *parse.ChainNode:
		walk(node.Node, visit)
	}
}

func walkBranch(branch *parse.BranchNode, visit func(parse.Node)) {
	walk(branch.Pipe, visit)
	if branch.List != nil {
		walk(branch.List, visit)
	}
	if branch.ElseList != nil {
		walk(branch.ElseList, visit)
	}
}

func (lowerer *lowerer) temp(prefix string) string {
	lowerer.temps++
	return prefix + strconv.Itoa(lowerer.temps)
}

// declare binds a new template variable in the current scope.
func (lowerer *lowerer) declare(name string) string {
	goName := lowerer.temp("x")
	lowerer.scope = append(lowerer.scope, binding{name, goName})
	return goName
}

// lookup is the Go variable for the template variable name.
func (lowerer *lowerer) lookup(name string) (string, error) {
	for index := len(lowerer.scope) - 1; index >= 0; index-- {
		if lowerer.scope[index].name == name {
			return lowerer.scope[index].goName, nil
		}
	}
	// The parser checked it is declared; it is declared in a branch that
	// does not run before this one, and text/template fails here at run
	// time if the branch that declares it has not run.
	return "", unsupported(
		"uses " + name + " outside the branch that declares it")
}

// callee is why the template called name is unsupported, or "".
func (lowerer *lowerer) callee(name string) string {
	if why, seen := lowerer.checked[baseName(name)]; seen && why != "" {
		return why
	}
	if why, seen := lowerer.checked[name]; seen {
		return why
	}
	saved := lowerer.temps
	lowerer.temps = 0
	lowerer.template(name)
	lowerer.temps = saved
	return lowerer.checked[name]
}

// baseName is the template a derived one was made from: escaping names
// the copy of p it makes for a <script> p$htmltemplate_stateJS..., and
// the author knows it as p.
func baseName(name string) string {
	base, _, _ := strings.Cut(name, "$htmltemplate_")
	return base
}
