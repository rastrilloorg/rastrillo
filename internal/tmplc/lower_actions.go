package tmplc

import (
	"fmt"
	"text/template/parse"
)

func (lowerer *lowerer) list(list *parse.ListNode) (block, error) {
	if list == nil {
		return nil, nil
	}
	var out block
	for _, node := range list.Nodes {
		lowered, err := lowerer.node(node)
		if err != nil {
			return nil, err
		}
		out = append(out, lowered...)
		// What follows a {{break}} or {{continue}}, or an {{if}} whose
		// branches both end in one, never runs, and Go vet calls the code
		// after the return or goto it becomes unreachable.
		if terminates(out) {
			return out, nil
		}
	}
	return out, nil
}

func (lowerer *lowerer) node(node parse.Node) (block, error) {
	switch node := node.(type) {
	case *parse.TextNode:
		return block{writeText{text: string(node.Text)}}, nil
	case *parse.CommentNode:
		return nil, nil
	case *parse.ActionNode:
		return lowerer.action(node)
	case *parse.IfNode:
		return lowerer.branch(&node.BranchNode, false)
	case *parse.WithNode:
		return lowerer.branch(&node.BranchNode, true)
	case *parse.RangeNode:
		return lowerer.rangeNode(node)
	case *parse.BreakNode:
		if len(lowerer.breaks) == 0 {
			return nil, unsupported("uses {{break}} outside {{range}}")
		}
		innermost := lowerer.breaks[len(lowerer.breaks)-1]
		return block{breakStmt{to: innermost}}, nil
	case *parse.ContinueNode:
		return block{continueStmt{}}, nil
	case *parse.TemplateNode:
		return lowerer.templateNode(node)
	}
	return nil, unsupported("uses " + node.String())
}

func (lowerer *lowerer) action(node *parse.ActionNode) (block, error) {
	if len(node.Pipe.Decl) > 0 {
		// A declaration prints nothing, and escaping leaves it alone.
		return lowerer.declaration(node.Pipe)
	}
	value, err := lowerer.pipeline(node.Pipe)
	if err != nil {
		return nil, err
	}
	// Escaping ends an action in an escaper, or keeps an html or
	// urlquery that ends it, which text/template prints by its own
	// rules.
	if _, ok := value.(escaperCall); !ok {
		return block{printValue{value: value, at: node.String()}}, nil
	}
	return block{writeValue{value: value}}, nil
}

func (lowerer *lowerer) templateNode(
	node *parse.TemplateNode,
) (block, error) {
	// A caller is reported for its callee first: that is the template
	// to change.
	if why := lowerer.callee(node.Name); why != "" {
		return nil, unsupported(fmt.Sprintf(
			"calls %q, which %s", baseName(node.Name), why))
	}
	var pre block
	var argument expr
	if node.Pipe != nil {
		var err error
		// Variables declared by the pipeline persist.
		pre, argument, err = lowerer.declaredValue(node.Pipe)
		if err != nil {
			return nil, err
		}
	}
	call := templateCall{
		callee: node.Name, arg: argument, at: node.String(),
	}
	return append(pre, call), nil
}

// declaration is an action that declares or assigns variables.
func (lowerer *lowerer) declaration(pipe *parse.PipeNode) (block, error) {
	pre, _, err := lowerer.declaredValue(pipe)
	return pre, err
}

// declaredValue lowers a pipeline that may declare or assign variables:
// the statements that set them, and the pipeline's value.
func (lowerer *lowerer) declaredValue(
	pipe *parse.PipeNode,
) (block, expr, error) {
	value, err := lowerer.pipeline(pipe)
	if err != nil || len(pipe.Decl) == 0 {
		return nil, value, err
	}
	vars, err := lowerer.pipeVars(pipe)
	if err != nil {
		return nil, nil, err
	}
	last := variable{vars[len(vars)-1]}
	if pipe.IsAssign {
		return block{assign{vars: vars, value: value}}, last, nil
	}
	return block{declare{vars: vars, value: value}}, last, nil
}

// pipeVars are the Go variables for those pipe sets: existing ones it
// assigns, or new ones it declares.
func (lowerer *lowerer) pipeVars(pipe *parse.PipeNode) ([]string, error) {
	var vars []string
	for _, declared := range pipe.Decl {
		name := declared.Ident[0]
		if !pipe.IsAssign {
			vars = append(vars, lowerer.declare(name))
			continue
		}
		goName, err := lowerer.lookup(name)
		if err != nil {
			return nil, err
		}
		vars = append(vars, goName)
	}
	return vars, nil
}

// branch lowers {{if}} and {{with}}. Variables the pipeline declares
// are in scope in both branches.
func (lowerer *lowerer) branch(
	node *parse.BranchNode, with bool,
) (block, error) {
	mark := len(lowerer.scope)
	defer func() { lowerer.scope = lowerer.scope[:mark] }()
	pre, value, err := lowerer.declaredValue(node.Pipe)
	if err != nil {
		return nil, err
	}
	then, err := lowerer.scoped(node.List)
	if err != nil {
		return nil, err
	}
	els, err := lowerer.scoped(node.ElseList)
	if err != nil {
		return nil, err
	}
	if with {
		return append(pre, withStmt{value: value, then: then, els: els}), nil
	}
	return append(pre, ifStmt{cond: value, then: then, els: els}), nil
}

// scoped lowers a list whose variables end with it.
func (lowerer *lowerer) scoped(list *parse.ListNode) (block, error) {
	mark := len(lowerer.scope)
	defer func() { lowerer.scope = lowerer.scope[:mark] }()
	return lowerer.list(list)
}

func (lowerer *lowerer) rangeNode(node *parse.RangeNode) (block, error) {
	mark := len(lowerer.scope)
	defer func() { lowerer.scope = lowerer.scope[:mark] }()
	pipe := node.Pipe
	over, err := lowerer.pipeline(pipe)
	if err != nil {
		return nil, err
	}
	vars, err := lowerer.pipeVars(pipe)
	if err != nil {
		return nil, err
	}
	body, err := lowerer.inRange("", node.List)
	if err != nil {
		return nil, err
	}
	end := lowerer.temp("end")
	els, err := lowerer.inRange(end, node.ElseList)
	if err != nil {
		return nil, err
	}
	if !breaksTo(els, end) {
		end = ""
	}
	return block{rangeStmt{
		over: over, at: pipe.String(),
		vars: vars, assign: pipe.IsAssign, two: len(pipe.Decl) > 1,
		body: body, els: els, end: end,
	}}, nil
}

// inRange lowers a list of a range, in which a {{break}} ends what label
// names: "" the range body, and the statement's end label the range.
func (lowerer *lowerer) inRange(
	label string, list *parse.ListNode,
) (block, error) {
	lowerer.breaks = append(lowerer.breaks, label)
	defer func() { lowerer.breaks = lowerer.breaks[:len(lowerer.breaks)-1] }()
	return lowerer.scoped(list)
}
