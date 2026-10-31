// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Forked from html/template/html.go and escape.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Names are exported for tmplrt and
// internal/tmplescape (doc.go); behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/html.go and escape.go, adapted

package htmlctx

import (
	"bytes"
	"strings"
)

// delimEnds is from escape.go, stripTags from html.go: stripTags walks
// the state machine, and tmplrt's attribute escaper calls it.

// delimEnds maps each delim to a string of characters that terminate it.
var DelimEnds = [...]string{
	DelimDoubleQuote: `"`,
	DelimSingleQuote: "'",
	// Determined empirically by running the below in various browsers.
	// var div = document.createElement("DIV");
	// for (var i = 0; i < 0x10000; ++i) {
	//   div.innerHTML = "<span title=x" + String.fromCharCode(i) + "-bar>";
	//   if (div.getElementsByTagName("SPAN")[0].title.indexOf("bar") < 0)
	//     document.write("<p>U+" + i.toString(16));
	// }
	DelimSpaceOrTagEnd: " \t\n\f\r>",
}

// stripTags takes a snippet of HTML and returns only the text content.
// For example, `<b>&iexcl;Hi!</b> <script>...</script>` -> `&iexcl;Hi! `.
func StripTags(html string) string {
	var b strings.Builder
	s, c, i, allText := []byte(html), Context{}, 0, true
	// Using the transition funcs helps us avoid mangling
	// `<div title="1>2">` or `I <3 Ponies!`.
	for i != len(s) {
		if c.Delim == DelimNone {
			st := c.State
			// Use RCDATA instead of parsing into JS or CSS styles.
			if c.Element != ElementNone && !IsInTag(st) {
				st = StateRCDATA
			}
			d, nread := TransitionFunc[st](c, s[i:])
			i1 := i + nread
			if c.State == StateText || c.State == StateRCDATA {
				// Emit text up to the start of the tag or comment.
				j := i1
				if d.State != c.State {
					for j1 := j - 1; j1 >= i; j1-- {
						if s[j1] == '<' {
							j = j1
							break
						}
					}
				}
				b.Write(s[i:j])
			} else {
				allText = false
			}
			c, i = d, i1
			continue
		}
		i1 := i + bytes.IndexAny(s[i:], DelimEnds[c.Delim])
		if i1 < i {
			break
		}
		if c.Delim != DelimSpaceOrTagEnd {
			// Consume any quote.
			i1++
		}
		c, i = Context{State: StateTag, Element: c.Element}, i1
	}
	if allText {
		return html
	} else if c.State == StateText || c.State == StateRCDATA {
		b.Write(s[i:])
	}
	return b.String()
}
