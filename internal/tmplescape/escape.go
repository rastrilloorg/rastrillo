// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Forked from html/template/escape.go in Go 1.26.10 for Rastrillo's
// compiled templates; see LICENSE-go and doc.go for what changed.

//rastrillo:vendored go1.26.10 src/html/template/escape.go

package tmplescape

import (
	"bytes"
	"fmt"
	"html"
	"maps"
	"regexp"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/htmlctx"
)

// escapeTemplate rewrites the named template, which must be
// associated with t, to guarantee that the output of any of the named
// templates is properly escaped. If no error is returned, then the named templates have
// been modified. Otherwise the named templates have been rendered
// unusable.
func escapeTemplate(e *escaper, node parse.Node, name string) error {
	c, _ := e.escapeTree(htmlctx.Context{}, node, name, 0)
	var err error
	if c.Err != nil {
		err, c.Err.Name = c.Err, name
	} else if c.State != htmlctx.StateText {
		// Keyed, unlike the stdlib's: vet asks for keys on another package's struct.
		err = &Error{ErrorCode: ErrEndContext, Name: name, Description: fmt.Sprintf("ends in a non-text context: %v", c)}
	}
	if err != nil {
		return err
	}
	e.commit()
	return nil
}

// escaper collects type inferences about templates and changes needed to make
// templates injection safe.
type escaper struct {
	// ns is the nameSpace that this escaper is associated with.
	ns *nameSpace
	// output[templateName] is the output context for a templateName that
	// has been mangled to include its input context.
	output map[string]htmlctx.Context
	// derived[c.mangle(name)] maps to a template derived from the template
	// named name templateName for the start context c.
	derived map[string]*template
	// called[templateName] is a set of called mangled template names.
	called map[string]bool
	// xxxNodeEdits are the accumulated edits to apply during commit.
	// Such edits are not applied immediately in case a template set
	// executes a given template in different escaping contexts.
	actionNodeEdits   map[*parse.ActionNode][]string
	templateNodeEdits map[*parse.TemplateNode]string
	textNodeEdits     map[*parse.TextNode][]byte
	// rangeContext holds context about the current range loop.
	rangeContext *rangeContext
}

// rangeContext holds information about the current range loop.
type rangeContext struct {
	outer     *rangeContext     // outer loop
	breaks    []htmlctx.Context // context at each break action
	continues []htmlctx.Context // context at each continue action
}

// makeEscaper creates a blank escaper for the given set.
func makeEscaper(n *nameSpace) escaper {
	return escaper{
		n,
		map[string]htmlctx.Context{},
		map[string]*template{},
		map[string]bool{},
		map[*parse.ActionNode][]string{},
		map[*parse.TemplateNode]string{},
		map[*parse.TextNode][]byte{},
		nil,
	}
}

// escape escapes a template node.
func (e *escaper) escape(c htmlctx.Context, n parse.Node) htmlctx.Context {
	switch n := n.(type) {
	case *parse.ActionNode:
		return e.escapeAction(c, n)
	case *parse.BreakNode:
		c.N = n
		e.rangeContext.breaks = append(e.rangeContext.breaks, c)
		return htmlctx.Context{State: htmlctx.StateDead}
	case *parse.CommentNode:
		return c
	case *parse.ContinueNode:
		c.N = n
		e.rangeContext.continues = append(e.rangeContext.continues, c)
		return htmlctx.Context{State: htmlctx.StateDead}
	case *parse.IfNode:
		return e.escapeBranch(c, &n.BranchNode, "if")
	case *parse.ListNode:
		return e.escapeList(c, n)
	case *parse.RangeNode:
		return e.escapeBranch(c, &n.BranchNode, "range")
	case *parse.TemplateNode:
		return e.escapeTemplate(c, n)
	case *parse.TextNode:
		return e.escapeText(c, n)
	case *parse.WithNode:
		return e.escapeBranch(c, &n.BranchNode, "with")
	}
	panic("escaping " + n.String() + " is unimplemented")
}

// escapeAction escapes an action template node.
func (e *escaper) escapeAction(c htmlctx.Context, n *parse.ActionNode) htmlctx.Context {
	if len(n.Pipe.Decl) != 0 {
		// A local variable assignment, not an interpolation.
		return c
	}
	c = nudge(c)
	// Check for disallowed use of predefined escapers in the pipeline.
	for pos, idNode := range n.Pipe.Cmds {
		node, ok := idNode.Args[0].(*parse.IdentifierNode)
		if !ok {
			// A predefined escaper "esc" will never be found as an identifier in a
			// Chain or Field node, since:
			// - "esc.x ..." is invalid, since predefined escapers return strings, and
			//   strings do not have methods, keys or fields.
			// - "... .esc" is invalid, since predefined escapers are global functions,
			//   not methods or fields of any types.
			// Therefore, it is safe to ignore these two node types.
			continue
		}
		ident := node.Ident
		if _, ok := predefinedEscapers[ident]; ok {
			if pos < len(n.Pipe.Cmds)-1 ||
				c.State == htmlctx.StateAttr && c.Delim == htmlctx.DelimSpaceOrTagEnd && ident == "html" {
				return htmlctx.Context{
					State: htmlctx.StateError,
					Err:   htmlctx.Errorf(ErrPredefinedEscaper, n, n.Line, "predefined escaper %q disallowed in template", ident),
				}
			}
		}
	}
	s := make([]string, 0, 3)
	switch c.State {
	case htmlctx.StateError:
		return c
	case htmlctx.StateURL, htmlctx.StateCSSDqStr, htmlctx.StateCSSSqStr, htmlctx.StateCSSDqURL, htmlctx.StateCSSSqURL, htmlctx.StateCSSURL:
		switch c.URLPart {
		case htmlctx.URLPartNone:
			s = append(s, "_html_template_urlfilter")
			fallthrough
		case htmlctx.URLPartPreQuery:
			switch c.State {
			case htmlctx.StateCSSDqStr, htmlctx.StateCSSSqStr:
				s = append(s, "_html_template_cssescaper")
			default:
				s = append(s, "_html_template_urlnormalizer")
			}
		case htmlctx.URLPartQueryOrFrag:
			s = append(s, "_html_template_urlescaper")
		case htmlctx.URLPartUnknown:
			return htmlctx.Context{
				State: htmlctx.StateError,
				Err:   htmlctx.Errorf(ErrAmbigContext, n, n.Line, "%s appears in an ambiguous context within a URL", n),
			}
		default:
			panic(c.URLPart.String())
		}
	case htmlctx.StateMetaContent:
		// Handled below in delim check.
	case htmlctx.StateMetaContentURL:
		// html/template reads GODEBUG=htmlmetacontenturlescape here, through
		// internal/godebug, which the fork cannot import; the caller passes
		// the setting in Options. The stdlib's else branch only counts the
		// non-default use.
		if e.ns.opts.MetaContentURLEscape {
			s = append(s, "_html_template_urlfilter")
		}
	case htmlctx.StateJS:
		s = append(s, "_html_template_jsvalescaper")
		// A slash after a value starts a div operator.
		c.JSCtx = htmlctx.JSCtxDivOp
	case htmlctx.StateJSDqStr, htmlctx.StateJSSqStr:
		s = append(s, "_html_template_jsstrescaper")
	case htmlctx.StateJSTmplLit:
		s = append(s, "_html_template_jstmpllitescaper")
	case htmlctx.StateJSRegexp:
		s = append(s, "_html_template_jsregexpescaper")
	case htmlctx.StateCSS:
		s = append(s, "_html_template_cssvaluefilter")
	case htmlctx.StateText:
		s = append(s, "_html_template_htmlescaper")
	case htmlctx.StateRCDATA:
		s = append(s, "_html_template_rcdataescaper")
	case htmlctx.StateAttr:
		// Handled below in delim check.
	case htmlctx.StateAttrName, htmlctx.StateTag:
		c.State = htmlctx.StateAttrName
		s = append(s, "_html_template_htmlnamefilter")
	case htmlctx.StateSrcset:
		s = append(s, "_html_template_srcsetescaper")
	default:
		if htmlctx.IsComment(c.State) {
			s = append(s, "_html_template_commentescaper")
		} else {
			panic("unexpected state " + c.State.String())
		}
	}
	switch c.Delim {
	case htmlctx.DelimNone:
		// No extra-escaping needed for raw text content.
	case htmlctx.DelimSpaceOrTagEnd:
		s = append(s, "_html_template_nospaceescaper")
	default:
		s = append(s, "_html_template_attrescaper")
	}
	e.editActionNode(n, s)
	return c
}

// ensurePipelineContains ensures that the pipeline ends with the commands with
// the identifiers in s in order. If the pipeline ends with a predefined escaper
// (i.e. "html" or "urlquery"), merge it with the identifiers in s.
func ensurePipelineContains(p *parse.PipeNode, s []string) {
	if len(s) == 0 {
		// Do not rewrite pipeline if we have no escapers to insert.
		return
	}
	// Precondition: p.Cmds contains at most one predefined escaper and the
	// escaper will be present at p.Cmds[len(p.Cmds)-1]. This precondition is
	// always true because of the checks in escapeAction.
	pipelineLen := len(p.Cmds)
	if pipelineLen > 0 {
		lastCmd := p.Cmds[pipelineLen-1]
		if idNode, ok := lastCmd.Args[0].(*parse.IdentifierNode); ok {
			if esc := idNode.Ident; predefinedEscapers[esc] {
				// Pipeline ends with a predefined escaper.
				if len(p.Cmds) == 1 && len(lastCmd.Args) > 1 {
					// Special case: pipeline is of the form {{ esc arg1 arg2 ... argN }},
					// where esc is the predefined escaper, and arg1...argN are its arguments.
					// Convert this into the equivalent form
					// {{ _eval_args_ arg1 arg2 ... argN | esc }}, so that esc can be easily
					// merged with the escapers in s.
					lastCmd.Args[0] = parse.NewIdentifier("_eval_args_").SetTree(nil).SetPos(lastCmd.Args[0].Position())
					p.Cmds = appendCmd(p.Cmds, newIdentCmd(esc, p.Position()))
					pipelineLen++
				}
				// If any of the commands in s that we are about to insert is equivalent
				// to the predefined escaper, use the predefined escaper instead.
				dup := false
				for i, escaper := range s {
					if escFnsEq(esc, escaper) {
						s[i] = idNode.Ident
						dup = true
					}
				}
				if dup {
					// The predefined escaper will already be inserted along with the
					// escapers in s, so do not copy it to the rewritten pipeline.
					pipelineLen--
				}
			}
		}
	}
	// Rewrite the pipeline, creating the escapers in s at the end of the pipeline.
	newCmds := make([]*parse.CommandNode, pipelineLen, pipelineLen+len(s))
	insertedIdents := make(map[string]bool)
	for i := 0; i < pipelineLen; i++ {
		cmd := p.Cmds[i]
		newCmds[i] = cmd
		if idNode, ok := cmd.Args[0].(*parse.IdentifierNode); ok {
			insertedIdents[normalizeEscFn(idNode.Ident)] = true
		}
	}
	for _, name := range s {
		if !insertedIdents[normalizeEscFn(name)] {
			// When two templates share an underlying parse tree via the use of
			// AddParseTree and one template is executed after the other, this check
			// ensures that escapers that were already inserted into the pipeline on
			// the first escaping pass do not get inserted again.
			newCmds = appendCmd(newCmds, newIdentCmd(name, p.Position()))
		}
	}
	p.Cmds = newCmds
}

// predefinedEscapers contains template predefined escapers that are equivalent
// to some contextual escapers. Keep in sync with equivEscapers.
var predefinedEscapers = map[string]bool{
	"html":     true,
	"urlquery": true,
}

// equivEscapers matches contextual escapers to equivalent predefined
// template escapers.
var equivEscapers = map[string]string{
	// The following pairs of HTML escapers provide equivalent security
	// guarantees, since they all escape '\000', '\'', '"', '&', '<', and '>'.
	"_html_template_attrescaper":   "html",
	"_html_template_htmlescaper":   "html",
	"_html_template_rcdataescaper": "html",
	// These two URL escapers produce URLs safe for embedding in a URL query by
	// percent-encoding all the reserved characters specified in RFC 3986 Section
	// 2.2
	"_html_template_urlescaper": "urlquery",
	// These two functions are not actually equivalent; urlquery is stricter as it
	// escapes reserved characters (e.g. '#'), while _html_template_urlnormalizer
	// does not. It is therefore only safe to replace _html_template_urlnormalizer
	// with urlquery (this happens in ensurePipelineContains), but not the otherI've
	// way around. We keep this entry around to preserve the behavior of templates
	// written before Go 1.9, which might depend on this substitution taking place.
	"_html_template_urlnormalizer": "urlquery",
}

// escFnsEq reports whether the two escaping functions are equivalent.
func escFnsEq(a, b string) bool {
	return normalizeEscFn(a) == normalizeEscFn(b)
}

// normalizeEscFn(a) is equal to normalizeEscFn(b) for any pair of names of
// escaper functions a and b that are equivalent.
func normalizeEscFn(e string) string {
	if norm := equivEscapers[e]; norm != "" {
		return norm
	}
	return e
}

// redundantFuncs[a][b] implies that funcMap[b](funcMap[a](x)) == funcMap[a](x)
// for all x.
var redundantFuncs = map[string]map[string]bool{
	"_html_template_commentescaper": {
		"_html_template_attrescaper": true,
		"_html_template_htmlescaper": true,
	},
	"_html_template_cssescaper": {
		"_html_template_attrescaper": true,
	},
	"_html_template_jsregexpescaper": {
		"_html_template_attrescaper": true,
	},
	"_html_template_jsstrescaper": {
		"_html_template_attrescaper": true,
	},
	"_html_template_jstmpllitescaper": {
		"_html_template_attrescaper": true,
	},
	"_html_template_urlescaper": {
		"_html_template_urlnormalizer": true,
	},
}

// appendCmd appends the given command to the end of the command pipeline
// unless it is redundant with the last command.
func appendCmd(cmds []*parse.CommandNode, cmd *parse.CommandNode) []*parse.CommandNode {
	if n := len(cmds); n != 0 {
		last, okLast := cmds[n-1].Args[0].(*parse.IdentifierNode)
		next, okNext := cmd.Args[0].(*parse.IdentifierNode)
		if okLast && okNext && redundantFuncs[last.Ident][next.Ident] {
			return cmds
		}
	}
	return append(cmds, cmd)
}

// newIdentCmd produces a command containing a single identifier node.
func newIdentCmd(identifier string, pos parse.Pos) *parse.CommandNode {
	return &parse.CommandNode{
		NodeType: parse.NodeCommand,
		Args:     []parse.Node{parse.NewIdentifier(identifier).SetTree(nil).SetPos(pos)}, // TODO: SetTree.
	}
}

// nudge returns the context that would result from following empty string
// transitions from the input context.
// For example, parsing:
//
//	`<a href=`
//
// will end in context{stateBeforeValue, attrURL}, but parsing one extra rune:
//
//	`<a href=x`
//
// will end in context{stateURL, delimSpaceOrTagEnd, ...}.
// There are two transitions that happen when the 'x' is seen:
// (1) Transition from a before-value state to a start-of-value state without
//
//	consuming any character.
//
// (2) Consume 'x' and transition past the first value character.
// In this case, nudging produces the context after (1) happens.
func nudge(c htmlctx.Context) htmlctx.Context {
	switch c.State {
	case htmlctx.StateTag:
		// In `<foo {{.}}`, the action should emit an attribute.
		c.State = htmlctx.StateAttrName
	case htmlctx.StateBeforeValue:
		// In `<foo bar={{.}}`, the action is an undelimited value.
		c.State, c.Delim, c.Attr = htmlctx.AttrStartStates[c.Attr], htmlctx.DelimSpaceOrTagEnd, htmlctx.AttrNone
	case htmlctx.StateAfterName:
		// In `<foo bar {{.}}`, the action is an attribute name.
		c.State, c.Attr = htmlctx.StateAttrName, htmlctx.AttrNone
	}
	return c
}

// join joins the two contexts of a branch template node. The result is an
// error context if either of the input contexts are error contexts, or if the
// input contexts differ.
func join(a, b htmlctx.Context, node parse.Node, nodeName string) htmlctx.Context {
	if a.State == htmlctx.StateError {
		return a
	}
	if b.State == htmlctx.StateError {
		return b
	}
	if a.State == htmlctx.StateDead {
		return b
	}
	if b.State == htmlctx.StateDead {
		return a
	}
	if a.Eq(b) {
		return a
	}

	c := a
	c.URLPart = b.URLPart
	if c.Eq(b) {
		// The contexts differ only by urlPart.
		c.URLPart = htmlctx.URLPartUnknown
		return c
	}

	c = a
	c.JSCtx = b.JSCtx
	if c.Eq(b) {
		// The contexts differ only by jsCtx.
		c.JSCtx = htmlctx.JSCtxUnknown
		return c
	}

	// Allow a nudged context to join with an unnudged one.
	// This means that
	//   <p title={{if .C}}{{.}}{{end}}
	// ends in an unquoted value state even though the else branch
	// ends in stateBeforeValue.
	if c, d := nudge(a), nudge(b); !(c.Eq(a) && d.Eq(b)) {
		if e := join(c, d, node, nodeName); e.State != htmlctx.StateError {
			return e
		}
	}

	return htmlctx.Context{
		State: htmlctx.StateError,
		Err:   htmlctx.Errorf(ErrBranchEnd, node, 0, "{{%s}} branches end in different contexts: %v, %v", nodeName, a, b),
	}
}

// escapeBranch escapes a branch template node: "if", "range" and "with".
func (e *escaper) escapeBranch(c htmlctx.Context, n *parse.BranchNode, nodeName string) htmlctx.Context {
	if nodeName == "range" {
		e.rangeContext = &rangeContext{outer: e.rangeContext}
	}
	c0 := e.escapeList(c.Clone(), n.List)
	if nodeName == "range" {
		if c0.State != htmlctx.StateError {
			c0 = joinRange(c0, e.rangeContext)
		}
		e.rangeContext = e.rangeContext.outer
		if c0.State == htmlctx.StateError {
			return c0
		}

		// The "true" branch of a "range" node can execute multiple times.
		// We check that executing n.List once results in the same context
		// as executing n.List twice.
		e.rangeContext = &rangeContext{outer: e.rangeContext}
		c1, _ := e.escapeListConditionally(c0, n.List, nil)
		c0 = join(c0, c1, n, nodeName)
		if c0.State == htmlctx.StateError {
			e.rangeContext = e.rangeContext.outer
			// Make clear that this is a problem on loop re-entry
			// since developers tend to overlook that branch when
			// debugging templates.
			c0.Err.Line = n.Line
			c0.Err.Description = "on range loop re-entry: " + c0.Err.Description
			return c0
		}
		c0 = joinRange(c0, e.rangeContext)
		e.rangeContext = e.rangeContext.outer
		if c0.State == htmlctx.StateError {
			return c0
		}
	}
	c1 := e.escapeList(c.Clone(), n.ElseList)
	return join(c0, c1, n, nodeName)
}

func joinRange(c0 htmlctx.Context, rc *rangeContext) htmlctx.Context {
	// Merge contexts at break and continue statements into overall body context.
	// In theory we could treat breaks differently from continues, but for now it is
	// enough to treat them both as going back to the start of the loop (which may then stop).
	for _, c := range rc.breaks {
		c0 = join(c0, c, c.N, "range")
		if c0.State == htmlctx.StateError {
			c0.Err.Line = c.N.(*parse.BreakNode).Line
			c0.Err.Description = "at range loop break: " + c0.Err.Description
			return c0
		}
	}
	for _, c := range rc.continues {
		c0 = join(c0, c, c.N, "range")
		if c0.State == htmlctx.StateError {
			c0.Err.Line = c.N.(*parse.ContinueNode).Line
			c0.Err.Description = "at range loop continue: " + c0.Err.Description
			return c0
		}
	}
	return c0
}

// escapeList escapes a list template node.
func (e *escaper) escapeList(c htmlctx.Context, n *parse.ListNode) htmlctx.Context {
	if n == nil {
		return c
	}
	for _, m := range n.Nodes {
		c = e.escape(c, m)
		if c.State == htmlctx.StateDead {
			break
		}
	}
	return c
}

// escapeListConditionally escapes a list node but only preserves edits and
// inferences in e if the inferences and output context satisfy filter.
// It returns the best guess at an output context, and the result of the filter
// which is the same as whether e was updated.
func (e *escaper) escapeListConditionally(c htmlctx.Context, n *parse.ListNode, filter func(*escaper, htmlctx.Context) bool) (htmlctx.Context, bool) {
	e1 := makeEscaper(e.ns)
	e1.rangeContext = e.rangeContext
	// Make type inferences available to f.
	maps.Copy(e1.output, e.output)
	c = e1.escapeList(c, n)
	ok := filter != nil && filter(&e1, c)
	if ok {
		// Copy inferences and edits from e1 back into e.
		maps.Copy(e.output, e1.output)
		maps.Copy(e.derived, e1.derived)
		maps.Copy(e.called, e1.called)
		for k, v := range e1.actionNodeEdits {
			e.editActionNode(k, v)
		}
		for k, v := range e1.templateNodeEdits {
			e.editTemplateNode(k, v)
		}
		for k, v := range e1.textNodeEdits {
			e.editTextNode(k, v)
		}
	}
	return c, ok
}

// escapeTemplate escapes a {{template}} call node.
func (e *escaper) escapeTemplate(c htmlctx.Context, n *parse.TemplateNode) htmlctx.Context {
	c, name := e.escapeTree(c, n, n.Name, n.Line)
	if name != n.Name {
		e.editTemplateNode(n, name)
	}
	return c
}

// escapeTree escapes the named template starting in the given context as
// necessary and returns its output context.
func (e *escaper) escapeTree(c htmlctx.Context, node parse.Node, name string, line int) (htmlctx.Context, string) {
	// Mangle the template name with the input context to produce a reliable
	// identifier.
	dname := c.Mangle(name)
	e.called[dname] = true
	if out, ok := e.output[dname]; ok {
		// Already escaped.
		return out, dname
	}
	t := e.template(name)
	if t == nil {
		// Two cases: The template exists but is empty, or has never been mentioned at
		// all. Distinguish the cases in the error messages.
		if _, ok := e.ns.set[name]; ok {
			return htmlctx.Context{
				State: htmlctx.StateError,
				Err:   htmlctx.Errorf(ErrNoSuchTemplate, node, line, "%q is an incomplete or empty template", name),
			}, dname
		}
		return htmlctx.Context{
			State: htmlctx.StateError,
			Err:   htmlctx.Errorf(ErrNoSuchTemplate, node, line, "no such template %q", name),
		}, dname
	}
	if dname != name {
		// Use any template derived during an earlier call to escapeTemplate
		// with different top level templates, or clone if necessary.
		dt := e.template(dname)
		if dt == nil {
			dt = &template{name: dname, Tree: &parse.Tree{Name: dname, Root: t.Root.CopyList()}}
			e.derived[dname] = dt
		}
		t = dt
	}
	return e.computeOutCtx(c, t), dname
}

// computeOutCtx takes a template and its start context and computes the output
// context while storing any inferences in e.
func (e *escaper) computeOutCtx(c htmlctx.Context, t *template) htmlctx.Context {
	// Propagate context over the body.
	c1, ok := e.escapeTemplateBody(c, t)
	if !ok {
		// Look for a fixed point by assuming c1 as the output context.
		if c2, ok2 := e.escapeTemplateBody(c1, t); ok2 {
			c1, ok = c2, true
		}
		// Use c1 as the error context if neither assumption worked.
	}
	if !ok && c1.State != htmlctx.StateError {
		return htmlctx.Context{
			State: htmlctx.StateError,
			Err:   htmlctx.Errorf(ErrOutputContext, t.Tree.Root, 0, "cannot compute output context for template %s", t.Name()),
		}
	}
	return c1
}

// escapeTemplateBody escapes the given template assuming the given output
// context, and returns the best guess at the output context and whether the
// assumption was correct.
func (e *escaper) escapeTemplateBody(c htmlctx.Context, t *template) (htmlctx.Context, bool) {
	filter := func(e1 *escaper, c1 htmlctx.Context) bool {
		if c1.State == htmlctx.StateError {
			// Do not update the input escaper, e.
			return false
		}
		if !e1.called[t.Name()] {
			// If t is not recursively called, then c1 is an
			// accurate output context.
			return true
		}
		// c1 is accurate if it matches our assumed output context.
		return c.Eq(c1)
	}
	// We need to assume an output context so that recursive template calls
	// take the fast path out of escapeTree instead of infinitely recurring.
	// Naively assuming that the input context is the same as the output
	// works >90% of the time.
	e.output[t.Name()] = c
	return e.escapeListConditionally(c, t.Tree.Root, filter)
}

// delimEnds is in htmlctx (html.go), beside stripTags, which also reads it.

var (
	// Per WHATWG HTML specification, section 4.12.1.3, there are extremely
	// complicated rules for how to handle the set of opening tags <!--,
	// <script, and </script when they appear in JS literals (i.e. strings,
	// regexs, and comments). The specification suggests a simple solution,
	// rather than implementing the arcane ABNF, which involves simply escaping
	// the opening bracket with \x3C. We use the below regex for this, since it
	// makes doing the case-insensitive find-replace much simpler.
	specialScriptTagRE          = regexp.MustCompile("(?i)<(script|/script|!--)")
	specialScriptTagReplacement = []byte("\\x3C$1")
)

func containsSpecialScriptTag(s []byte) bool {
	return specialScriptTagRE.Match(s)
}

func escapeSpecialScriptTags(s []byte) []byte {
	return specialScriptTagRE.ReplaceAll(s, specialScriptTagReplacement)
}

var doctypeBytes = []byte("<!DOCTYPE")

// escapeText escapes a text template node.
func (e *escaper) escapeText(c htmlctx.Context, n *parse.TextNode) htmlctx.Context {
	s, written, i, b := n.Text, 0, 0, new(bytes.Buffer)
	for i != len(s) {
		c1, nread := contextAfterText(c, s[i:])
		i1 := i + nread
		if c.State == htmlctx.StateText || c.State == htmlctx.StateRCDATA {
			end := i1
			if c1.State != c.State {
				for j := end - 1; j >= i; j-- {
					if s[j] == '<' {
						end = j
						break
					}
				}
			}
			for j := i; j < end; j++ {
				if s[j] == '<' && !bytes.HasPrefix(bytes.ToUpper(s[j:]), doctypeBytes) {
					b.Write(s[written:j])
					b.WriteString("&lt;")
					written = j + 1
				}
			}
		} else if htmlctx.IsComment(c.State) && c.Delim == htmlctx.DelimNone {
			switch c.State {
			case htmlctx.StateJSBlockCmt:
				// https://es5.github.io/#x7.4:
				// "Comments behave like white space and are
				// discarded except that, if a MultiLineComment
				// contains a line terminator character, then
				// the entire comment is considered to be a
				// LineTerminator for purposes of parsing by
				// the syntactic grammar."
				if bytes.ContainsAny(s[written:i1], "\n\r\u2028\u2029") {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			case htmlctx.StateCSSBlockCmt:
				b.WriteByte(' ')
			}
			written = i1
		}
		if c.State != c1.State && htmlctx.IsComment(c1.State) && c1.Delim == htmlctx.DelimNone {
			// Preserve the portion between written and the comment start.
			cs := i1 - 2
			if c1.State == htmlctx.StateHTMLCmt || c1.State == htmlctx.StateJSHTMLOpenCmt {
				// "<!--" instead of "/*" or "//"
				cs -= 2
			} else if c1.State == htmlctx.StateJSHTMLCloseCmt {
				// "-->" instead of "/*" or "//"
				cs -= 1
			}
			b.Write(s[written:cs])
			written = i1
		}
		if htmlctx.IsInScriptLiteral(c.State) && containsSpecialScriptTag(s[i:i1]) {
			b.Write(s[written:i])
			b.Write(escapeSpecialScriptTags(s[i:i1]))
			written = i1
		}
		if i == i1 && c.State == c1.State {
			panic(fmt.Sprintf("infinite loop from %v to %v on %q..%q", c, c1, s[:i], s[i:]))
		}
		c, i = c1, i1
	}

	if written != 0 && c.State != htmlctx.StateError {
		if !htmlctx.IsComment(c.State) || c.Delim != htmlctx.DelimNone {
			b.Write(n.Text[written:])
		}
		e.editTextNode(n, b.Bytes())
	}
	return c
}

// contextAfterText starts in context c, consumes some tokens from the front of
// s, then returns the context after those tokens and the unprocessed suffix.
func contextAfterText(c htmlctx.Context, s []byte) (htmlctx.Context, int) {
	if c.Delim == htmlctx.DelimNone {
		c1, i := htmlctx.TSpecialTagEnd(c, s)
		if i == 0 {
			// A special end tag (`</script>`) has been seen and
			// all content preceding it has been consumed.
			return c1, 0
		}
		// Consider all content up to any end tag.
		return htmlctx.TransitionFunc[c.State](c, s[:i])
	}

	// We are at the beginning of an attribute value.

	i := bytes.IndexAny(s, htmlctx.DelimEnds[c.Delim])
	if i == -1 {
		i = len(s)
	}
	if c.Delim == htmlctx.DelimSpaceOrTagEnd {
		// https://www.w3.org/TR/html5/syntax.html#attribute-value-(unquoted)-state
		// lists the runes below as error characters.
		// Error out because HTML parsers may differ on whether
		// "<a id= onclick=f("     ends inside id's or onclick's value,
		// "<a class=`foo "        ends inside a value,
		// "<a style=font:'Arial'" needs open-quote fixup.
		// IE treats '`' as a quotation character.
		if j := bytes.IndexAny(s[:i], "\"'<=`"); j >= 0 {
			return htmlctx.Context{
				State: htmlctx.StateError,
				Err:   htmlctx.Errorf(ErrBadHTML, nil, 0, "%q in unquoted attr: %q", s[j:j+1], s[:i]),
			}, len(s)
		}
	}
	if i == len(s) {
		// Remain inside the attribute.
		// Decode the value so non-HTML rules can easily handle
		//     <button onclick="alert(&quot;Hi!&quot;)">
		// without having to entity decode token boundaries.
		for u := []byte(html.UnescapeString(string(s))); len(u) != 0; {
			c1, i1 := htmlctx.TransitionFunc[c.State](c, u)
			c, u = c1, u[i1:]
		}
		return c, len(s)
	}

	element := c.Element

	// If this is a non-JS "type" attribute inside "script" tag, do not treat the contents as JS.
	if c.State == htmlctx.StateAttr && c.Element == htmlctx.ElementScript && c.Attr == htmlctx.AttrScriptType && !htmlctx.IsJSType(string(s[:i])) {
		element = htmlctx.ElementNone
	}

	if c.Delim != htmlctx.DelimSpaceOrTagEnd {
		// Consume any quote.
		i++
	}
	// On exiting an attribute, we discard all state information
	// except the state and element.
	return htmlctx.Context{State: htmlctx.StateTag, Element: element}, i
}

// editActionNode records a change to an action pipeline for later commit.
func (e *escaper) editActionNode(n *parse.ActionNode, cmds []string) {
	if _, ok := e.actionNodeEdits[n]; ok {
		panic(fmt.Sprintf("node %s shared between templates", n))
	}
	e.actionNodeEdits[n] = cmds
}

// editTemplateNode records a change to a {{template}} callee for later commit.
func (e *escaper) editTemplateNode(n *parse.TemplateNode, callee string) {
	if _, ok := e.templateNodeEdits[n]; ok {
		panic(fmt.Sprintf("node %s shared between templates", n))
	}
	e.templateNodeEdits[n] = callee
}

// editTextNode records a change to a text node for later commit.
func (e *escaper) editTextNode(n *parse.TextNode, text []byte) {
	if _, ok := e.textNodeEdits[n]; ok {
		panic(fmt.Sprintf("node %s shared between templates", n))
	}
	e.textNodeEdits[n] = text
}

// commit applies changes to actions and template calls needed to contextually
// autoescape content and adds any derived templates to the set.
func (e *escaper) commit() {
	// html/template adds its escaper funcs to every template escaped here.
	// The fork's trees go to generated code, which calls tmplrt's escapers
	// by name, so there is nothing to add.
	for _, t := range e.derived {
		e.ns.addParseTree(t)
	}
	for n, s := range e.actionNodeEdits {
		ensurePipelineContains(n.Pipe, s)
	}
	for n, name := range e.templateNodeEdits {
		n.Name = name
	}
	for n, s := range e.textNodeEdits {
		n.Text = s
	}
	// Reset state that is specific to this commit so that the same changes are
	// not re-applied to the template on subsequent calls to commit.
	e.called = make(map[string]bool)
	e.actionNodeEdits = make(map[*parse.ActionNode][]string)
	e.templateNodeEdits = make(map[*parse.TemplateNode]string)
	e.textNodeEdits = make(map[*parse.TextNode][]byte)
}

// template returns the named template given a mangled template name.
func (e *escaper) template(name string) *template {
	t := e.ns.lookup(name)
	if t == nil {
		t = e.derived[name]
	}
	return t
}
