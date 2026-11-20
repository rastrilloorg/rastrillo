package ui

import (
	"bytes"
	"regexp"
)

// Attribute is one attribute of an HTML start tag.
type Attribute struct{ Name, Value string }

// markupCommentRE matches an HTML comment or a Go template comment,
// both prose about markup rather than markup.
var markupCommentRE = regexp.MustCompile(
	`(?s)<!--.*?-->|\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

// Attributes reads the attributes of every start tag in src, in order,
// with comments left out. A name inside a quoted value or a comment is
// not an attribute, so a caller asking whether a layout has an
// attribute is not fooled by a title or a note that says its name. A
// tag cut short by a template action ends its own reading.
func Attributes(src string) []Attribute {
	text := markupCommentRE.ReplaceAll([]byte(src), nil)
	var found []Attribute
	for {
		at := bytes.IndexByte(text, '<')
		if at < 0 || at+1 >= len(text) {
			return found
		}
		text = text[at+1:]
		if !isLetter(text[0]) {
			continue
		}
		end := bytes.IndexAny(text, htmlSpace+"/>")
		if end < 0 {
			return found
		}
		text = text[end:]
		for {
			text = bytes.TrimLeft(text, htmlSpace+"/")
			if len(text) == 0 {
				return found
			}
			if text[0] == '>' {
				break
			}
			name, value, rest, complete := readAttribute(text)
			if !complete {
				break
			}
			found = append(found, Attribute{name, value})
			text = rest
		}
	}
}
