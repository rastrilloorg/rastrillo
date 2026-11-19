// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Forked from html/template/context.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Names are exported for tmplrt and
// internal/tmplescape (doc.go); behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/context.go

package htmlctx

import (
	"fmt"
	"slices"
	"text/template/parse"
)

// context describes the state an HTML parser must be in when it reaches the
// portion of HTML produced by evaluating a particular template node.
//
// The zero value of type context is the start context for a template that
// produces an HTML fragment as defined at
// https://www.w3.org/TR/html5/syntax.html#the-end
// where the context element is null.
type Context struct {
	State   State
	Delim   Delim
	URLPart URLPart
	JSCtx   JSCtx
	// jsBraceDepth contains the current depth, for each JS template literal
	// string interpolation expression, of braces we've seen. This is used to
	// determine if the next } will close a JS template literal string
	// interpolation expression or not.
	JSBraceDepth []int
	Attr         Attr
	Element      Element
	N            parse.Node // for range break/continue
	Err          *Error
}

func (c Context) String() string {
	var err error
	if c.Err != nil {
		err = c.Err
	}
	return fmt.Sprintf("{%v %v %v %v %v %v %v %v}", c.State, c.Delim, c.URLPart, c.JSCtx, c.JSBraceDepth, c.Attr, c.Element, err)
}

// eq reports whether two contexts are equal.
func (c Context) Eq(d Context) bool {
	return c.State == d.State &&
		c.Delim == d.Delim &&
		c.URLPart == d.URLPart &&
		c.JSCtx == d.JSCtx &&
		slices.Equal(c.JSBraceDepth, d.JSBraceDepth) &&
		c.Attr == d.Attr &&
		c.Element == d.Element &&
		c.Err == d.Err
}

// mangle produces an identifier that includes a suffix that distinguishes it
// from template names mangled with different contexts.
func (c Context) Mangle(templateName string) string {
	// The mangled name for the default context is the input templateName.
	if c.State == StateText {
		return templateName
	}
	s := templateName + "$htmltemplate_" + c.State.String()
	if c.Delim != DelimNone {
		s += "_" + c.Delim.String()
	}
	if c.URLPart != URLPartNone {
		s += "_" + c.URLPart.String()
	}
	if c.JSCtx != JSCtxRegexp {
		s += "_" + c.JSCtx.String()
	}
	if c.JSBraceDepth != nil {
		s += fmt.Sprintf("_jsBraceDepth(%v)", c.JSBraceDepth)
	}
	if c.Attr != AttrNone {
		s += "_" + c.Attr.String()
	}
	if c.Element != ElementNone {
		s += "_" + c.Element.String()
	}
	return s
}

// clone returns a copy of c with the same field values.
func (c Context) Clone() Context {
	clone := c
	clone.JSBraceDepth = slices.Clone(c.JSBraceDepth)
	return clone
}

// state describes a high-level HTML parser state.
//
// It bounds the top of the element stack, and by extension the HTML insertion
// mode, but also contains state that does not correspond to anything in the
// HTML5 parsing algorithm because a single token production in the HTML
// grammar may contain embedded actions in a template. For instance, the quoted
// HTML attribute produced by
//
//	<div title="Hello {{.World}}">
//
// is a single token in HTML's grammar but in a template spans several nodes.
type State uint8

const (
	// stateText is parsed character data. An HTML parser is in
	// this state when its parse position is outside an HTML tag,
	// directive, comment, and special element body.
	StateText State = iota
	// stateTag occurs before an HTML attribute or the end of a tag.
	StateTag
	// stateAttrName occurs inside an attribute name.
	// It occurs between the ^'s in ` ^name^ = value`.
	StateAttrName
	// stateAfterName occurs after an attr name has ended but before any
	// equals sign. It occurs between the ^'s in ` name^ ^= value`.
	StateAfterName
	// stateBeforeValue occurs after the equals sign but before the value.
	// It occurs between the ^'s in ` name =^ ^value`.
	StateBeforeValue
	// stateHTMLCmt occurs inside an <!-- HTML comment -->.
	StateHTMLCmt
	// stateRCDATA occurs inside an RCDATA element (<textarea> or <title>)
	// as described at https://www.w3.org/TR/html5/syntax.html#elements-0
	StateRCDATA
	// stateAttr occurs inside an HTML attribute whose content is text.
	StateAttr
	// stateURL occurs inside an HTML attribute whose content is a URL.
	StateURL
	// stateSrcset occurs inside an HTML srcset attribute.
	StateSrcset
	// stateJS occurs inside an event handler or script element.
	StateJS
	// stateJSDqStr occurs inside a JavaScript double quoted string.
	StateJSDqStr
	// stateJSSqStr occurs inside a JavaScript single quoted string.
	StateJSSqStr
	// stateJSTmplLit occurs inside a JavaScript back quoted string.
	StateJSTmplLit
	// stateJSRegexp occurs inside a JavaScript regexp literal.
	StateJSRegexp
	// stateJSBlockCmt occurs inside a JavaScript /* block comment */.
	StateJSBlockCmt
	// stateJSLineCmt occurs inside a JavaScript // line comment.
	StateJSLineCmt
	// stateJSHTMLOpenCmt occurs inside a JavaScript <!-- HTML-like comment.
	StateJSHTMLOpenCmt
	// stateJSHTMLCloseCmt occurs inside a JavaScript --> HTML-like comment.
	StateJSHTMLCloseCmt
	// stateCSS occurs inside a <style> element or style attribute.
	StateCSS
	// stateCSSDqStr occurs inside a CSS double quoted string.
	StateCSSDqStr
	// stateCSSSqStr occurs inside a CSS single quoted string.
	StateCSSSqStr
	// stateCSSDqURL occurs inside a CSS double quoted url("...").
	StateCSSDqURL
	// stateCSSSqURL occurs inside a CSS single quoted url('...').
	StateCSSSqURL
	// stateCSSURL occurs inside a CSS unquoted url(...).
	StateCSSURL
	// stateCSSBlockCmt occurs inside a CSS /* block comment */.
	StateCSSBlockCmt
	// stateCSSLineCmt occurs inside a CSS // line comment.
	StateCSSLineCmt
	// stateError is an infectious error state outside any valid
	// HTML/CSS/JS construct.
	StateError
	// stateMetaContent occurs inside a HTML meta element content attribute.
	StateMetaContent
	// stateMetaContentURL occurs inside a "url=" tag in a HTML meta element content attribute.
	StateMetaContentURL
	// stateDead marks unreachable code after a {{break}} or {{continue}}.
	StateDead
)

// isComment is true for any state that contains content meant for template
// authors & maintainers, not for end-users or machines.
func IsComment(s State) bool {
	switch s {
	case StateHTMLCmt, StateJSBlockCmt, StateJSLineCmt, StateJSHTMLOpenCmt, StateJSHTMLCloseCmt, StateCSSBlockCmt, StateCSSLineCmt:
		return true
	}
	return false
}

// isInTag return whether s occurs solely inside an HTML tag.
func IsInTag(s State) bool {
	switch s {
	case StateTag, StateAttrName, StateAfterName, StateBeforeValue, StateAttr:
		return true
	}
	return false
}

// isInScriptLiteral returns true if s is one of the literal states within a
// <script> tag, and as such occurrences of "<!--", "<script", and "</script"
// need to be treated specially.
func IsInScriptLiteral(s State) bool {
	// Ignore the comment states (stateJSBlockCmt, stateJSLineCmt,
	// stateJSHTMLOpenCmt, stateJSHTMLCloseCmt) because their content is already
	// omitted from the output.
	switch s {
	case StateJSDqStr, StateJSSqStr, StateJSTmplLit, StateJSRegexp:
		return true
	}
	return false
}

// delim is the delimiter that will end the current HTML attribute.
type Delim uint8

const (
	// delimNone occurs outside any attribute.
	DelimNone Delim = iota
	// delimDoubleQuote occurs when a double quote (") closes the attribute.
	DelimDoubleQuote
	// delimSingleQuote occurs when a single quote (') closes the attribute.
	DelimSingleQuote
	// delimSpaceOrTagEnd occurs when a space or right angle bracket (>)
	// closes the attribute.
	DelimSpaceOrTagEnd
)

// urlPart identifies a part in an RFC 3986 hierarchical URL to allow different
// encoding strategies.
type URLPart uint8

const (
	// urlPartNone occurs when not in a URL, or possibly at the start:
	// ^ in "^http://auth/path?k=v#frag".
	URLPartNone URLPart = iota
	// urlPartPreQuery occurs in the scheme, authority, or path; between the
	// ^s in "h^ttp://auth/path^?k=v#frag".
	URLPartPreQuery
	// urlPartQueryOrFrag occurs in the query portion between the ^s in
	// "http://auth/path?^k=v#frag^".
	URLPartQueryOrFrag
	// urlPartUnknown occurs due to joining of contexts both before and
	// after the query separator.
	URLPartUnknown
)

// jsCtx determines whether a '/' starts a regular expression literal or a
// division operator.
type JSCtx uint8

const (
	// jsCtxRegexp occurs where a '/' would start a regexp literal.
	JSCtxRegexp JSCtx = iota
	// jsCtxDivOp occurs where a '/' would start a division operator.
	JSCtxDivOp
	// jsCtxUnknown occurs where a '/' is ambiguous due to context joining.
	JSCtxUnknown
)

// element identifies the HTML element when inside a start tag or special body.
// Certain HTML element (for example <script> and <style>) have bodies that are
// treated differently from stateText so the element type is necessary to
// transition into the correct context at the end of a tag and to identify the
// end delimiter for the body.
type Element uint8

const (
	// elementNone occurs outside a special tag or special element body.
	ElementNone Element = iota
	// elementScript corresponds to the raw text <script> element
	// with JS MIME type or no type attribute.
	ElementScript
	// elementStyle corresponds to the raw text <style> element.
	ElementStyle
	// elementTextarea corresponds to the RCDATA <textarea> element.
	ElementTextarea
	// elementTitle corresponds to the RCDATA <title> element.
	ElementTitle
	// elementMeta corresponds to the HTML <meta> element.
	ElementMeta
)

// attr identifies the current HTML attribute when inside the attribute,
// that is, starting from stateAttrName until stateTag/stateText (exclusive).
type Attr uint8

const (
	// attrNone corresponds to a normal attribute or no attribute.
	AttrNone Attr = iota
	// attrScript corresponds to an event handler attribute.
	AttrScript
	// attrScriptType corresponds to the type attribute in script HTML element
	AttrScriptType
	// attrStyle corresponds to the style attribute whose value is CSS.
	AttrStyle
	// attrURL corresponds to an attribute whose value is a URL.
	AttrURL
	// attrSrcset corresponds to a srcset attribute.
	AttrSrcset
	// attrMetaContent corresponds to the content attribute in meta HTML element.
	AttrMetaContent
)
