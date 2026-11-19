// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Ported from html/template/html.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/html.go

package tmplrt

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/internal/htmlctx"
)

// HTMLNospaceEscaper escapes for inclusion in unquoted attribute values.
func HTMLNospaceEscaper(args ...any) string {
	s, t := stringify(args...)
	if s == "" {
		return filterFailsafe
	}
	if t == htmlctx.ContentTypeHTML {
		return htmlReplacer(htmlctx.StripTags(s), htmlNospaceNormReplacementTable, false)
	}
	return htmlReplacer(s, htmlNospaceReplacementTable, false)
}

// AttrEscaper escapes for inclusion in quoted attribute values.
func AttrEscaper(args ...any) string {
	s, t := stringify(args...)
	if t == htmlctx.ContentTypeHTML {
		return htmlReplacer(htmlctx.StripTags(s), htmlNormReplacementTable, true)
	}
	return htmlReplacer(s, htmlReplacementTable, true)
}

// RCDATAEscaper escapes for inclusion in an RCDATA element body.
func RCDATAEscaper(args ...any) string {
	s, t := stringify(args...)
	if t == htmlctx.ContentTypeHTML {
		return htmlReplacer(s, htmlNormReplacementTable, true)
	}
	return htmlReplacer(s, htmlReplacementTable, true)
}

// HTMLEscaper escapes for inclusion in HTML text.
func HTMLEscaper(args ...any) string {
	s, t := stringify(args...)
	if t == htmlctx.ContentTypeHTML {
		return s
	}
	return htmlReplacer(s, htmlReplacementTable, true)
}

// htmlReplacementTable contains the runes that need to be escaped
// inside a quoted attribute value or in a text node.
var htmlReplacementTable = []string{
	// https://www.w3.org/TR/html5/syntax.html#attribute-value-(unquoted)-state
	// U+0000 NULL Parse error. Append a U+FFFD REPLACEMENT
	// CHARACTER character to the current attribute's value.
	// "
	// and similarly
	// https://www.w3.org/TR/html5/syntax.html#before-attribute-value-state
	0:    "\uFFFD",
	'"':  "&#34;",
	'&':  "&amp;",
	'\'': "&#39;",
	'+':  "&#43;",
	'<':  "&lt;",
	'>':  "&gt;",
}

// htmlNormReplacementTable is like htmlReplacementTable but without '&' to
// avoid over-encoding existing entities.
var htmlNormReplacementTable = []string{
	0:    "\uFFFD",
	'"':  "&#34;",
	'\'': "&#39;",
	'+':  "&#43;",
	'<':  "&lt;",
	'>':  "&gt;",
}

// htmlNospaceReplacementTable contains the runes that need to be escaped
// inside an unquoted attribute value.
// The set of runes escaped is the union of the HTML specials and
// those determined by running the JS below in browsers:
// <div id=d></div>
// <script>(function () {
// var a = [], d = document.getElementById("d"), i, c, s;
// for (i = 0; i < 0x10000; ++i) {
//
//	c = String.fromCharCode(i);
//	d.innerHTML = "<span title=" + c + "lt" + c + "></span>"
//	s = d.getElementsByTagName("SPAN")[0];
//	if (!s || s.title !== c + "lt" + c) { a.push(i.toString(16)); }
//
// }
// document.write(a.join(", "));
// })()</script>
var htmlNospaceReplacementTable = []string{
	0:    "&#xfffd;",
	'\t': "&#9;",
	'\n': "&#10;",
	'\v': "&#11;",
	'\f': "&#12;",
	'\r': "&#13;",
	' ':  "&#32;",
	'"':  "&#34;",
	'&':  "&amp;",
	'\'': "&#39;",
	'+':  "&#43;",
	'<':  "&lt;",
	'=':  "&#61;",
	'>':  "&gt;",
	// A parse error in the attribute value (unquoted) and
	// before attribute value states.
	// Treated as a quoting character by IE.
	'`': "&#96;",
}

// htmlNospaceNormReplacementTable is like htmlNospaceReplacementTable but
// without '&' to avoid over-encoding existing entities.
var htmlNospaceNormReplacementTable = []string{
	0:    "&#xfffd;",
	'\t': "&#9;",
	'\n': "&#10;",
	'\v': "&#11;",
	'\f': "&#12;",
	'\r': "&#13;",
	' ':  "&#32;",
	'"':  "&#34;",
	'\'': "&#39;",
	'+':  "&#43;",
	'<':  "&lt;",
	'=':  "&#61;",
	'>':  "&gt;",
	// A parse error in the attribute value (unquoted) and
	// before attribute value states.
	// Treated as a quoting character by IE.
	'`': "&#96;",
}

// htmlReplacer returns s with runes replaced according to replacementTable
// and when badRunes is true, certain bad runes are allowed through unescaped.
func htmlReplacer(s string, replacementTable []string, badRunes bool) string {
	written, b := 0, new(strings.Builder)
	r, w := rune(0), 0
	for i := 0; i < len(s); i += w {
		// Cannot use 'for range s' because we need to preserve the width
		// of the runes in the input. If we see a decoding error, the input
		// width will not be utf8.Runelen(r) and we will overrun the buffer.
		r, w = utf8.DecodeRuneInString(s[i:])
		if int(r) < len(replacementTable) {
			if repl := replacementTable[r]; len(repl) != 0 {
				if written == 0 {
					b.Grow(len(s))
				}
				b.WriteString(s[written:i])
				b.WriteString(repl)
				written = i + w
			}
		} else if badRunes {
			// No-op.
			// IE does not allow these ranges in unquoted attrs.
		} else if 0xfdd0 <= r && r <= 0xfdef || 0xfff0 <= r && r <= 0xffff {
			if written == 0 {
				b.Grow(len(s))
			}
			fmt.Fprintf(b, "%s&#x%x;", s[written:i], r)
			written = i + w
		}
	}
	if written == 0 {
		return s
	}
	b.WriteString(s[written:])
	return b.String()
}

// HTMLNameFilter accepts valid parts of an HTML attribute or tag name or
// a known-safe HTML attribute.
func HTMLNameFilter(args ...any) string {
	s, t := stringify(args...)
	if t == htmlctx.ContentTypeHTMLAttr {
		return s
	}
	if len(s) == 0 {
		// Avoid violation of structure preservation.
		// <input checked {{.K}}={{.V}}>.
		// Without this, if .K is empty then .V is the value of
		// checked, but otherwise .V is the value of the attribute
		// named .K.
		return filterFailsafe
	}
	s = strings.ToLower(s)
	if t := htmlctx.AttrType(s); t != htmlctx.ContentTypePlain {
		// TODO: Split attr and element name part filters so we can recognize known attributes.
		return filterFailsafe
	}
	for _, r := range s {
		switch {
		case '0' <= r && r <= '9':
		case 'a' <= r && r <= 'z':
		default:
			return filterFailsafe
		}
	}
	return s
}

// CommentEscaper returns the empty string regardless of input.
// Comment content does not correspond to any parsed structure or
// human-readable content, so the simplest and most secure policy is to drop
// content interpolated into comments.
// This approach is equally valid whether or not static comment content is
// removed from the template.
func CommentEscaper(args ...any) string {
	return ""
}
