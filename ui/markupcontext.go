package ui

import (
	"bytes"
	"strings"
	"text/template/parse"
)

// A markupState is where a template's text has got to in the HTML it
// writes, as html/template tracks it to escape an action.
type markupState int

const (
	inText      markupState = iota // between tags
	inTag                          // in a tag, between its attributes
	inName                         // in an attribute's name
	afterName                      // after a name, before any "="
	beforeValue                    // after an attribute's "="
	inValue                        // in an attribute's value
	inComment                      // in <!-- … -->
	inRawText                      // in a script, style, title or textarea
)

// A markupContext follows one template's text through the HTML it
// writes, so an action's place in it is known: a class an action
// writes can be in an attribute's name position or in what it prints.
type markupContext struct {
	state     markupState
	attribute string // the attribute named or valued, lower case
	quote     byte   // the value's quote, or 0 for an unquoted value
	rawTag    string // the element whose raw text the tag opens
	rawEnd    string // the end tag that closes inRawText's text
}

// rawTextElements hold text that no tag inside opens.
var rawTextElements = map[string]bool{
	"script": true, "style": true, "title": true, "textarea": true,
	"xmp": true, "iframe": true, "noembed": true, "noframes": true,
	"noscript": true, "plaintext": true,
}

// read moves the context past a run of the template's text.
func (context *markupContext) read(text []byte) {
	for index := 0; index < len(text); index++ {
		index = context.step(text, index)
	}
}

// step reads text[index] and returns the index of the last byte read.
func (context *markupContext) step(text []byte, index int) int {
	character := text[index]
	switch context.state {
	case inText:
		return context.readText(text, index)
	case inComment:
		end := bytes.Index(text[index:], []byte("-->"))
		if end < 0 {
			return len(text) - 1
		}
		context.state = inText
		return index + end + len("-->") - 1
	case inRawText:
		rest := bytes.ToLower(text[index:])
		end := bytes.Index(rest, []byte(context.rawEnd))
		if end < 0 {
			return len(text) - 1
		}
		context.state, context.rawTag = inTag, ""
		return index + end + len(context.rawEnd) - 1
	case inValue:
		if context.quote == 0 && isSpace(character) ||
			character == context.quote && context.quote != 0 {
			context.state = inTag
		} else if context.quote == 0 && character == '>' {
			context.closeTag()
		}
	case beforeValue:
		switch {
		case isSpace(character):
		case character == '"' || character == '\'':
			context.state, context.quote = inValue, character
		case character == '>':
			context.closeTag()
		default:
			context.state, context.quote = inValue, 0
		}
	default:
		context.readTag(character)
	}
	return index
}

// readText reads text[index] between tags, where "<" may open a tag
// or a comment.
func (context *markupContext) readText(text []byte, index int) int {
	if text[index] != '<' || index+1 >= len(text) {
		return index
	}
	rest := text[index+1:]
	switch {
	case bytes.HasPrefix(rest, []byte("!--")):
		context.state = inComment
		return index + len("<!--") - 1
	case rest[0] == '/' || rest[0] == '!' || rest[0] == '?':
		context.state, context.rawTag = inTag, ""
		return index + 1
	case isLetter(rest[0]):
		end := 0
		for end < len(rest) && !isSpace(rest[end]) &&
			rest[end] != '>' && rest[end] != '/' {
			end++
		}
		name := strings.ToLower(string(rest[:end]))
		context.state, context.rawTag = inTag, ""
		if rawTextElements[name] {
			context.rawTag = name
		}
		return index + end
	}
	return index
}

// readTag reads a byte of a tag outside any value.
func (context *markupContext) readTag(character byte) {
	switch {
	case character == '>':
		context.closeTag()
	case character == '=' &&
		(context.state == inName || context.state == afterName):
		context.state = beforeValue
	case isSpace(character) && context.state == inName:
		context.state = afterName
	case isSpace(character) || character == '/':
		if character == '/' {
			context.state = inTag
		}
	case context.state == inName:
		context.attribute += string(lower(character))
	default:
		context.state, context.attribute = inName, string(lower(character))
	}
}

func (context *markupContext) closeTag() {
	context.state, context.attribute = inText, ""
	if context.rawTag != "" {
		context.state, context.rawEnd = inRawText, "</"+context.rawTag
	}
}

func isSpace(character byte) bool {
	return strings.IndexByte(htmlSpaces, character) >= 0
}

func isLetter(character byte) bool {
	return 'a' <= lower(character) && lower(character) <= 'z'
}

func lower(character byte) byte {
	if 'A' <= character && character <= 'Z' {
		return character + 'a' - 'A'
	}
	return character
}

// mayWriteClass reports whether what an action prints, in this
// context, may put a class on an element: printed in a tag outside a
// value, html/template lets a template.HTMLAttr write whole
// attributes, class among them; printed in a class or data-rst-class
// value, it is a class. Printed as text it is read as text, whatever
// its type: markup built in Go, from a function or in data typed
// template.HTML, declares the hooks and classes it shows with needs,
// as #158 has it for hooks, and a needs keeps every class.
func (context *markupContext) mayWriteClass() bool {
	switch context.state {
	case inTag, inName, afterName:
		return true
	case beforeValue, inValue:
		return context.attribute == "class" ||
			context.attribute == "data-rst-class"
	}
	return false
}

// followMarkup moves the markup context of the template named template
// past node, and lets the page wear any class when an action or a
// template call there may write one. An else branch starts where its
// if did, not where the if's own branch ended.
func (reading *pageReading) followMarkup(
	template string, node parse.Node,
) {
	if reading.contexts == nil {
		reading.contexts = map[string]*markupContext{}
		reading.elseStarts = map[*parse.ListNode]markupContext{}
	}
	context := reading.contexts[template]
	if context == nil {
		context = &markupContext{}
		reading.contexts[template] = context
	}
	if list, ok := node.(*parse.ListNode); ok && list != nil {
		if start, ok := reading.elseStarts[list]; ok {
			*context = start
		}
	}
	switch concrete := node.(type) {
	case *parse.TextNode:
		context.read(concrete.Text)
	case *parse.ActionNode:
		if len(concrete.Pipe.Decl) == 0 && context.mayWriteClass() {
			reading.anyClass = true
		}
	case *parse.TemplateNode:
		reading.anyClass = reading.anyClass || context.mayWriteClass()
	case *parse.IfNode:
		reading.startElse(concrete.ElseList, *context)
	case *parse.RangeNode:
		reading.startElse(concrete.ElseList, *context)
	case *parse.WithNode:
		reading.startElse(concrete.ElseList, *context)
	}
}

func (reading *pageReading) startElse(
	list *parse.ListNode, start markupContext,
) {
	if list != nil {
		reading.elseStarts[list] = start
	}
}
