// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Forked from html/template/transition.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Names are exported for tmplrt and
// internal/tmplescape (doc.go); behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/transition.go

package htmlctx

import (
	"bytes"
	"strings"
)

// transitionFunc is the array of context transition functions for text nodes.
// A transition function takes a context and template text input, and returns
// the updated context and the number of bytes consumed from the front of the
// input.
var TransitionFunc = [...]func(Context, []byte) (Context, int){
	StateText:           tText,
	StateTag:            tTag,
	StateAttrName:       tAttrName,
	StateAfterName:      tAfterName,
	StateBeforeValue:    tBeforeValue,
	StateHTMLCmt:        tHTMLCmt,
	StateRCDATA:         TSpecialTagEnd,
	StateAttr:           tAttr,
	StateURL:            tURL,
	StateMetaContent:    tMetaContent,
	StateMetaContentURL: tMetaContentURL,
	StateSrcset:         tURL,
	StateJS:             tJS,
	StateJSDqStr:        tJSDelimited,
	StateJSSqStr:        tJSDelimited,
	StateJSRegexp:       tJSDelimited,
	StateJSTmplLit:      tJSTmpl,
	StateJSBlockCmt:     tBlockCmt,
	StateJSLineCmt:      tLineCmt,
	StateJSHTMLOpenCmt:  tLineCmt,
	StateJSHTMLCloseCmt: tLineCmt,
	StateCSS:            tCSS,
	StateCSSDqStr:       tCSSStr,
	StateCSSSqStr:       tCSSStr,
	StateCSSDqURL:       tCSSStr,
	StateCSSSqURL:       tCSSStr,
	StateCSSURL:         tCSSStr,
	StateCSSBlockCmt:    tBlockCmt,
	StateCSSLineCmt:     tLineCmt,
	StateError:          tError,
}

var commentStart = []byte("<!--")
var commentEnd = []byte("-->")

// tText is the context transition function for the text state.
func tText(c Context, s []byte) (Context, int) {
	k := 0
	for {
		i := k + bytes.IndexByte(s[k:], '<')
		if i < k || i+1 == len(s) {
			return c, len(s)
		} else if i+4 <= len(s) && bytes.Equal(commentStart, s[i:i+4]) {
			return Context{State: StateHTMLCmt}, i + 4
		}
		i++
		end := false
		if s[i] == '/' {
			if i+1 == len(s) {
				return c, len(s)
			}
			end, i = true, i+1
		}
		j, e := eatTagName(s, i)
		if j != i {
			if end {
				e = ElementNone
			}
			// We've found an HTML tag.
			return Context{State: StateTag, Element: e}, j
		}
		k = j
	}
}

var ElementContentType = [...]State{
	ElementNone:     StateText,
	ElementScript:   StateJS,
	ElementStyle:    StateCSS,
	ElementTextarea: StateRCDATA,
	ElementTitle:    StateRCDATA,
	ElementMeta:     StateText,
}

// tTag is the context transition function for the tag state.
func tTag(c Context, s []byte) (Context, int) {
	// Find the attribute name.
	i := eatWhiteSpace(s, 0)
	if i == len(s) {
		return c, len(s)
	}
	if s[i] == '>' {
		// Treat <meta> specially, because it doesn't have an end tag, and we
		// want to transition into the correct state/element for it.
		if c.Element == ElementMeta {
			return Context{State: StateText, Element: ElementNone}, i + 1
		}
		return Context{
			State:   ElementContentType[c.Element],
			Element: c.Element,
		}, i + 1
	}
	j, err := eatAttrName(s, i)
	if err != nil {
		return Context{State: StateError, Err: err}, len(s)
	}
	state, attr := StateTag, AttrNone
	if i == j {
		return Context{
			State: StateError,
			Err:   Errorf(ErrBadHTML, nil, 0, "expected space, attr name, or end of tag, but got %q", s[i:]),
		}, len(s)
	}

	attrName := strings.ToLower(string(s[i:j]))
	if c.Element == ElementScript && attrName == "type" {
		attr = AttrScriptType
	} else if c.Element == ElementMeta && attrName == "content" {
		attr = AttrMetaContent
	} else {
		switch AttrType(attrName) {
		case ContentTypeURL:
			attr = AttrURL
		case ContentTypeCSS:
			attr = AttrStyle
		case ContentTypeJS:
			attr = AttrScript
		case ContentTypeSrcset:
			attr = AttrSrcset
		}
	}

	if j == len(s) {
		state = StateAttrName
	} else {
		state = StateAfterName
	}
	return Context{State: state, Element: c.Element, Attr: attr}, j
}

// tAttrName is the context transition function for stateAttrName.
func tAttrName(c Context, s []byte) (Context, int) {
	i, err := eatAttrName(s, 0)
	if err != nil {
		return Context{State: StateError, Err: err}, len(s)
	} else if i != len(s) {
		c.State = StateAfterName
	}
	return c, i
}

// tAfterName is the context transition function for stateAfterName.
func tAfterName(c Context, s []byte) (Context, int) {
	// Look for the start of the value.
	i := eatWhiteSpace(s, 0)
	if i == len(s) {
		return c, len(s)
	} else if s[i] != '=' {
		// Occurs due to tag ending '>', and valueless attribute.
		c.State = StateTag
		return c, i
	}
	c.State = StateBeforeValue
	// Consume the "=".
	return c, i + 1
}

var AttrStartStates = [...]State{
	AttrNone:        StateAttr,
	AttrScript:      StateJS,
	AttrScriptType:  StateAttr,
	AttrStyle:       StateCSS,
	AttrURL:         StateURL,
	AttrSrcset:      StateSrcset,
	AttrMetaContent: StateMetaContent,
}

// tBeforeValue is the context transition function for stateBeforeValue.
func tBeforeValue(c Context, s []byte) (Context, int) {
	i := eatWhiteSpace(s, 0)
	if i == len(s) {
		return c, len(s)
	}
	// Find the attribute delimiter.
	delim := DelimSpaceOrTagEnd
	switch s[i] {
	case '\'':
		delim, i = DelimSingleQuote, i+1
	case '"':
		delim, i = DelimDoubleQuote, i+1
	}
	c.State, c.Delim = AttrStartStates[c.Attr], delim
	return c, i
}

// tHTMLCmt is the context transition function for stateHTMLCmt.
func tHTMLCmt(c Context, s []byte) (Context, int) {
	if i := bytes.Index(s, commentEnd); i != -1 {
		return Context{}, i + 3
	}
	return c, len(s)
}

// specialTagEndMarkers maps element types to the character sequence that
// case-insensitively signals the end of the special tag body.
var specialTagEndMarkers = [...][]byte{
	ElementScript:   []byte("script"),
	ElementStyle:    []byte("style"),
	ElementTextarea: []byte("textarea"),
	ElementTitle:    []byte("title"),
	ElementMeta:     []byte(""),
}

var (
	specialTagEndPrefix = []byte("</")
	tagEndSeparators    = []byte("> \t\n\f/")
)

// tSpecialTagEnd is the context transition function for raw text and RCDATA
// element states.
func TSpecialTagEnd(c Context, s []byte) (Context, int) {
	if c.Element != ElementNone {
		// script end tags ("</script") within script literals are ignored, so that
		// we can properly escape them.
		if c.Element == ElementScript && (IsInScriptLiteral(c.State) || IsComment(c.State)) {
			return c, len(s)
		}
		if i := indexTagEnd(s, specialTagEndMarkers[c.Element]); i != -1 {
			return Context{}, i
		}
	}
	return c, len(s)
}

// indexTagEnd finds the index of a special tag end in a case insensitive way, or returns -1
func indexTagEnd(s []byte, tag []byte) int {
	res := 0
	plen := len(specialTagEndPrefix)
	for len(s) > 0 {
		// Try to find the tag end prefix first
		i := bytes.Index(s, specialTagEndPrefix)
		if i == -1 {
			return i
		}
		s = s[i+plen:]
		// Try to match the actual tag if there is still space for it
		if len(tag) <= len(s) && bytes.EqualFold(tag, s[:len(tag)]) {
			s = s[len(tag):]
			// Check the tag is followed by a proper separator
			if len(s) > 0 && bytes.IndexByte(tagEndSeparators, s[0]) != -1 {
				return res + i
			}
			res += len(tag)
		}
		res += i + plen
	}
	return -1
}

// tAttr is the context transition function for the attribute state.
func tAttr(c Context, s []byte) (Context, int) {
	return c, len(s)
}

// tURL is the context transition function for the URL state.
func tURL(c Context, s []byte) (Context, int) {
	if bytes.ContainsAny(s, "#?") {
		c.URLPart = URLPartQueryOrFrag
	} else if len(s) != eatWhiteSpace(s, 0) && c.URLPart == URLPartNone {
		// HTML5 uses "Valid URL potentially surrounded by spaces" for
		// attrs: https://www.w3.org/TR/html5/index.html#attributes-1
		c.URLPart = URLPartPreQuery
	}
	return c, len(s)
}

// tJS is the context transition function for the JS state.
func tJS(c Context, s []byte) (Context, int) {
	i := bytes.IndexAny(s, "\"`'/{}<-#")
	if i == -1 {
		// Entire input is non string, comment, regexp tokens.
		c.JSCtx = nextJSCtx(s, c.JSCtx)
		return c, len(s)
	}
	c.JSCtx = nextJSCtx(s[:i], c.JSCtx)
	switch s[i] {
	case '"':
		c.State, c.JSCtx = StateJSDqStr, JSCtxRegexp
	case '\'':
		c.State, c.JSCtx = StateJSSqStr, JSCtxRegexp
	case '`':
		c.State, c.JSCtx = StateJSTmplLit, JSCtxRegexp
	case '/':
		switch {
		case i+1 < len(s) && s[i+1] == '/':
			c.State, i = StateJSLineCmt, i+1
		case i+1 < len(s) && s[i+1] == '*':
			c.State, i = StateJSBlockCmt, i+1
		case c.JSCtx == JSCtxRegexp:
			c.State = StateJSRegexp
		case c.JSCtx == JSCtxDivOp:
			c.JSCtx = JSCtxRegexp
		default:
			return Context{
				State: StateError,
				Err:   Errorf(ErrSlashAmbig, nil, 0, "'/' could start a division or regexp: %.32q", s[i:]),
			}, len(s)
		}
	// ECMAScript supports HTML style comments for legacy reasons, see Appendix
	// B.1.1 "HTML-like Comments". The handling of these comments is somewhat
	// confusing. Multi-line comments are not supported, i.e. anything on lines
	// between the opening and closing tokens is not considered a comment, but
	// anything following the opening or closing token, on the same line, is
	// ignored. As such we simply treat any line prefixed with "<!--" or "-->"
	// as if it were actually prefixed with "//" and move on.
	case '<':
		if i+3 < len(s) && bytes.Equal(commentStart, s[i:i+4]) {
			c.State, i = StateJSHTMLOpenCmt, i+3
		}
	case '-':
		if i+2 < len(s) && bytes.Equal(commentEnd, s[i:i+3]) {
			c.State, i = StateJSHTMLCloseCmt, i+2
		}
	// ECMAScript also supports "hashbang" comment lines, see Section 12.5.
	case '#':
		if i+1 < len(s) && s[i+1] == '!' {
			c.State, i = StateJSLineCmt, i+1
			break
		}
		// A private identifier such as #yield is never a keyword, and
		// precedes a div op.
		for i+1 < len(s) && IsJSIdentPart(rune(s[i+1])) {
			i++
		}
		c.JSCtx = JSCtxDivOp
	case '{':
		// We only care about tracking brace depth if we are inside of a
		// template literal.
		if len(c.JSBraceDepth) == 0 {
			c.JSCtx = nextJSCtx(s[i:i+1], c.JSCtx)
			return c, i + 1
		}
		c.JSBraceDepth[len(c.JSBraceDepth)-1]++
		c.JSCtx = nextJSCtx(s[i:i+1], c.JSCtx)
	case '}':
		if len(c.JSBraceDepth) == 0 {
			c.JSCtx = nextJSCtx(s[i:i+1], c.JSCtx)
			return c, i + 1
		}
		// There are no cases where a brace can be escaped in the JS context
		// that are not syntax errors, it seems. Because of this we can just
		// count "\}" as "}" and move on, the script is already broken as
		// fully fledged parsers will just fail anyway.
		c.JSBraceDepth[len(c.JSBraceDepth)-1]--
		if c.JSBraceDepth[len(c.JSBraceDepth)-1] >= 0 {
			c.JSCtx = nextJSCtx(s[i:i+1], c.JSCtx)
			return c, i + 1
		}
		c.JSBraceDepth = c.JSBraceDepth[:len(c.JSBraceDepth)-1]
		c.State = StateJSTmplLit
	default:
		panic("unreachable")
	}
	return c, i + 1
}

func tJSTmpl(c Context, s []byte) (Context, int) {
	var k int
	for {
		i := k + bytes.IndexAny(s[k:], "`\\$")
		if i < k {
			break
		}
		switch s[i] {
		case '\\':
			i++
			if i == len(s) {
				return Context{
					State: StateError,
					Err:   Errorf(ErrPartialEscape, nil, 0, "unfinished escape sequence in JS string: %q", s),
				}, len(s)
			}
		case '$':
			if len(s) >= i+2 && s[i+1] == '{' {
				c.JSBraceDepth = append(c.JSBraceDepth, 0)
				c.State, c.JSCtx = StateJS, JSCtxRegexp
				return c, i + 2
			}
		case '`':
			// end
			c.State = StateJS
			return c, i + 1
		}
		k = i + 1
	}

	return c, len(s)
}

// tJSDelimited is the context transition function for the JS string and regexp
// states.
func tJSDelimited(c Context, s []byte) (Context, int) {
	specials := `\"`
	switch c.State {
	case StateJSSqStr:
		specials = `\'`
	case StateJSRegexp:
		specials = `\/[]`
	}

	k, inCharset := 0, false
	for {
		i := k + bytes.IndexAny(s[k:], specials)
		if i < k {
			break
		}
		switch s[i] {
		case '\\':
			i++
			if i == len(s) {
				return Context{
					State: StateError,
					Err:   Errorf(ErrPartialEscape, nil, 0, "unfinished escape sequence in JS string: %q", s),
				}, len(s)
			}
		case '[':
			inCharset = true
		case ']':
			inCharset = false
		case '/':
			// If "</script" appears in a regex literal, the '/' should not
			// close the regex literal, and it will later be escaped to
			// "\x3C/script" in escapeText.
			if i > 0 && i+7 <= len(s) && bytes.Equal(bytes.ToLower(s[i-1:i+7]), []byte("</script")) {
				i++
			} else if !inCharset {
				c.State, c.JSCtx = StateJS, JSCtxDivOp
				return c, i + 1
			}
		default:
			// end delimiter
			if !inCharset {
				c.State, c.JSCtx = StateJS, JSCtxDivOp
				return c, i + 1
			}
		}
		k = i + 1
	}

	if inCharset {
		// This can be fixed by making context richer if interpolation
		// into charsets is desired.
		return Context{
			State: StateError,
			Err:   Errorf(ErrPartialCharset, nil, 0, "unfinished JS regexp charset: %q", s),
		}, len(s)
	}

	return c, len(s)
}

var blockCommentEnd = []byte("*/")

// tBlockCmt is the context transition function for /*comment*/ states.
func tBlockCmt(c Context, s []byte) (Context, int) {
	i := bytes.Index(s, blockCommentEnd)
	if i == -1 {
		return c, len(s)
	}
	switch c.State {
	case StateJSBlockCmt:
		c.State = StateJS
	case StateCSSBlockCmt:
		c.State = StateCSS
	default:
		panic(c.State.String())
	}
	return c, i + 2
}

// tLineCmt is the context transition function for //comment states, and the JS HTML-like comment state.
func tLineCmt(c Context, s []byte) (Context, int) {
	var lineTerminators string
	var endState State
	switch c.State {
	case StateJSLineCmt, StateJSHTMLOpenCmt, StateJSHTMLCloseCmt:
		lineTerminators, endState = "\n\r\u2028\u2029", StateJS
	case StateCSSLineCmt:
		lineTerminators, endState = "\n\f\r", StateCSS
		// Line comments are not part of any published CSS standard but
		// are supported by the 4 major browsers.
		// This defines line comments as
		//     LINECOMMENT ::= "//" [^\n\f\d]*
		// since https://www.w3.org/TR/css3-syntax/#SUBTOK-nl defines
		// newlines:
		//     nl ::= #xA | #xD #xA | #xD | #xC
	default:
		panic(c.State.String())
	}

	i := bytes.IndexAny(s, lineTerminators)
	if i == -1 {
		return c, len(s)
	}
	c.State = endState
	// Per section 7.4 of EcmaScript 5 : https://es5.github.io/#x7.4
	// "However, the LineTerminator at the end of the line is not
	// considered to be part of the single-line comment; it is
	// recognized separately by the lexical grammar and becomes part
	// of the stream of input elements for the syntactic grammar."
	return c, i
}

// tCSS is the context transition function for the CSS state.
func tCSS(c Context, s []byte) (Context, int) {
	// CSS quoted strings are almost never used except for:
	// (1) URLs as in background: "/foo.png"
	// (2) Multiword font-names as in font-family: "Times New Roman"
	// (3) List separators in content values as in inline-lists:
	//    <style>
	//    ul.inlineList { list-style: none; padding:0 }
	//    ul.inlineList > li { display: inline }
	//    ul.inlineList > li:before { content: ", " }
	//    ul.inlineList > li:first-child:before { content: "" }
	//    </style>
	//    <ul class=inlineList><li>One<li>Two<li>Three</ul>
	// (4) Attribute value selectors as in a[href="http://example.com/"]
	//
	// We conservatively treat all strings as URLs, but make some
	// allowances to avoid confusion.
	//
	// In (1), our conservative assumption is justified.
	// In (2), valid font names do not contain ':', '?', or '#', so our
	// conservative assumption is fine since we will never transition past
	// urlPartPreQuery.
	// In (3), our protocol heuristic should not be tripped, and there
	// should not be non-space content after a '?' or '#', so as long as
	// we only %-encode RFC 3986 reserved characters we are ok.
	// In (4), we should URL escape for URL attributes, and for others we
	// have the attribute name available if our conservative assumption
	// proves problematic for real code.

	k := 0
	for {
		i := k + bytes.IndexAny(s[k:], `("'/`)
		if i < k {
			return c, len(s)
		}
		switch s[i] {
		case '(':
			// Look for url to the left.
			p := bytes.TrimRight(s[:i], "\t\n\f\r ")
			if endsWithCSSKeyword(p, "url") {
				j := len(s) - len(bytes.TrimLeft(s[i+1:], "\t\n\f\r "))
				switch {
				case j != len(s) && s[j] == '"':
					c.State, j = StateCSSDqURL, j+1
				case j != len(s) && s[j] == '\'':
					c.State, j = StateCSSSqURL, j+1
				default:
					c.State = StateCSSURL
				}
				return c, j
			}
		case '/':
			if i+1 < len(s) {
				switch s[i+1] {
				case '/':
					c.State = StateCSSLineCmt
					return c, i + 2
				case '*':
					c.State = StateCSSBlockCmt
					return c, i + 2
				}
			}
		case '"':
			c.State = StateCSSDqStr
			return c, i + 1
		case '\'':
			c.State = StateCSSSqStr
			return c, i + 1
		}
		k = i + 1
	}
}

// tCSSStr is the context transition function for the CSS string and URL states.
func tCSSStr(c Context, s []byte) (Context, int) {
	var endAndEsc string
	switch c.State {
	case StateCSSDqStr, StateCSSDqURL:
		endAndEsc = `\"`
	case StateCSSSqStr, StateCSSSqURL:
		endAndEsc = `\'`
	case StateCSSURL:
		// Unquoted URLs end with a newline or close parenthesis.
		// The below includes the wc (whitespace character) and nl.
		endAndEsc = "\\\t\n\f\r )"
	default:
		panic(c.State.String())
	}

	k := 0
	for {
		i := k + bytes.IndexAny(s[k:], endAndEsc)
		if i < k {
			c, nread := tURL(c, DecodeCSS(s[k:]))
			return c, k + nread
		}
		if s[i] == '\\' {
			i++
			if i == len(s) {
				return Context{
					State: StateError,
					Err:   Errorf(ErrPartialEscape, nil, 0, "unfinished escape sequence in CSS string: %q", s),
				}, len(s)
			}
		} else {
			c.State = StateCSS
			return c, i + 1
		}
		c, _ = tURL(c, DecodeCSS(s[:i+1]))
		k = i + 1
	}
}

// tError is the context transition function for the error state.
func tError(c Context, s []byte) (Context, int) {
	return c, len(s)
}

// tMetaContent is the context transition function for the meta content attribute state.
func tMetaContent(c Context, s []byte) (Context, int) {
	for i := range len(s) {
		if i+3 <= len(s)-1 && bytes.EqualFold(s[i:i+3], []byte("url")) {
			if j := eatWhiteSpace(s, i+3); j < len(s) && s[j] == '=' {
				c.State = StateMetaContentURL
				return c, j + 1
			}
		}
	}
	return c, len(s)
}

// tMetaContentURL is the context transition function for the "url=" part of a meta content attribute state.
func tMetaContentURL(c Context, s []byte) (Context, int) {
	for i := range len(s) {
		if s[i] == ';' {
			c.State = StateMetaContent
			return c, i + 1
		}
	}
	return c, len(s)
}

// eatAttrName returns the largest j such that s[i:j] is an attribute name.
// It returns an error if s[i:] does not look like it begins with an
// attribute name, such as encountering a quote mark without a preceding
// equals sign.
func eatAttrName(s []byte, i int) (int, *Error) {
	for j := i; j < len(s); j++ {
		switch s[j] {
		case ' ', '\t', '\n', '\f', '\r', '=', '>':
			return j, nil
		case '\'', '"', '<':
			// These result in a parse warning in HTML5 and are
			// indicative of serious problems if seen in an attr
			// name in a template.
			return -1, Errorf(ErrBadHTML, nil, 0, "%q in attribute name: %.32q", s[j:j+1], s)
		default:
			// No-op.
		}
	}
	return len(s), nil
}

var ElementNameMap = map[string]Element{
	"script":   ElementScript,
	"style":    ElementStyle,
	"textarea": ElementTextarea,
	"title":    ElementTitle,
	"meta":     ElementMeta,
}

// asciiAlpha reports whether c is an ASCII letter.
func asciiAlpha(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z'
}

// asciiAlphaNum reports whether c is an ASCII letter or digit.
func asciiAlphaNum(c byte) bool {
	return asciiAlpha(c) || '0' <= c && c <= '9'
}

// eatTagName returns the largest j such that s[i:j] is a tag name and the tag type.
func eatTagName(s []byte, i int) (int, Element) {
	if i == len(s) || !asciiAlpha(s[i]) {
		return i, ElementNone
	}
	j := i + 1
	for j < len(s) {
		x := s[j]
		if asciiAlphaNum(x) {
			j++
			continue
		}
		// Allow "x-y" or "x:y" but not "x-", "-y", or "x--y".
		if (x == ':' || x == '-') && j+1 < len(s) && asciiAlphaNum(s[j+1]) {
			j += 2
			continue
		}
		break
	}
	return j, ElementNameMap[strings.ToLower(string(s[i:j]))]
}

// eatWhiteSpace returns the largest j such that s[i:j] is white space.
func eatWhiteSpace(s []byte, i int) int {
	for j := i; j < len(s); j++ {
		switch s[j] {
		case ' ', '\t', '\n', '\f', '\r':
			// No-op.
		default:
			return j
		}
	}
	return len(s)
}
