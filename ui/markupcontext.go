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
		return context.readRawText(text, index)
	case inValue:
		if context.quote == 0 && isSpace(character) ||
			character == context.quote && context.quote != 0 {
			context.state, context.attribute, context.quote = inTag, "", 0
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

// readRawText reads raw text from text[index] up to its end tag, whose
// name it leaves the context in.
func (context *markupContext) readRawText(text []byte, index int) int {
	rest := bytes.ToLower(text[index:])
	end := bytes.Index(rest, []byte(context.rawEnd))
	if end < 0 {
		return len(text) - 1
	}
	skipped := index + end + len(context.rawEnd) - 1
	context.state, context.rawTag, context.rawEnd = inTag, "", ""
	return skipped
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
			context.state, context.attribute = inTag, ""
		}
	case context.state == inName:
		context.attribute += string(lower(character))
	default:
		context.state, context.attribute = inName, string(lower(character))
	}
}

func (context *markupContext) closeTag() {
	context.state, context.attribute, context.quote = inText, "", 0
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
		return classAttributes[context.attribute]
	}
	return false
}

// classAttributes are the attributes whose values are classes: class,
// and data-rst-class, which select.js copies onto a row.
var classAttributes = map[string]bool{"class": true, "data-rst-class": true}

// nudged is the context after an action prints, as html/template's
// nudge has it: in a tag or after a name, the action is an attribute's
// name; after "=", it starts an unquoted value, which the next space
// or ">" ends.
func (context markupContext) nudged() markupContext {
	switch context.state {
	case inTag, afterName:
		context.state, context.attribute = inName, ""
	case beforeValue:
		context.state, context.quote = inValue, 0
	}
	return context
}

// A markupWalk follows a page's templates in the order they render, as
// html/template's escaper does, so each action is read in the context
// it prints in: a {{template}} call carries on in the called template
// from the caller's context and leaves the caller where the called
// template ends, and each branch starts where its if or range did.
type markupWalk struct {
	roots      map[string]*parse.ListNode
	ends       map[walkStart]markupContext
	writeClass bool // an action or call may write a class
}

// A walkStart is a template entered in a context, which the walk
// follows once.
type walkStart struct {
	template string
	context  markupContext
}

// call follows the template named template from context and returns
// where it ends. A call met again while it is being followed, as a
// template that calls itself, is taken to end where it began.
func (walk *markupWalk) call(
	template string, context markupContext,
) markupContext {
	start := walkStart{template, context}
	if end, ok := walk.ends[start]; ok {
		return end
	}
	walk.ends[start] = context
	end := walk.list(walk.roots[template], context)
	walk.ends[start] = end
	return end
}

func (walk *markupWalk) list(
	list *parse.ListNode, context markupContext,
) markupContext {
	if list == nil {
		return context
	}
	for _, node := range list.Nodes {
		context = walk.node(node, context)
	}
	return context
}

func (walk *markupWalk) node(
	node parse.Node, context markupContext,
) markupContext {
	switch concrete := node.(type) {
	case *parse.TextNode:
		context.read(concrete.Text)
	case *parse.ActionNode:
		if len(concrete.Pipe.Decl) > 0 {
			return context
		}
		walk.writeClass = walk.writeClass || context.mayWriteClass()
		return context.nudged()
	case *parse.TemplateNode:
		walk.writeClass = walk.writeClass || context.mayWriteClass()
		return walk.call(concrete.Name, context)
	case *parse.IfNode:
		return walk.branch(&concrete.BranchNode, context, false)
	case *parse.RangeNode:
		return walk.branch(&concrete.BranchNode, context, true)
	case *parse.WithNode:
		return walk.branch(&concrete.BranchNode, context, false)
	}
	return context
}

// branch follows both lists of an if, range or with from where it
// starts, a range's body twice, since it may run again from where it
// ended, and joins where they end.
func (walk *markupWalk) branch(
	branch *parse.BranchNode, context markupContext, loops bool,
) markupContext {
	body := walk.list(branch.List, context)
	if loops {
		body = walk.list(branch.List, body)
	}
	return walk.join(body, walk.list(branch.ElseList, context))
}

// join is where two branches that end at one and other carry on, as
// html/template joins them: the same place, or the same once nudged,
// as when one branch ends after a bare attribute and the other in the
// tag. html/template knows an attribute by its kind, not its name, so
// two names join as a name the walk does not know, and a class among
// them counts as writing one. Branches that end apart, which
// html/template refuses to run, count as writing a class too.
func (walk *markupWalk) join(one, other markupContext) markupContext {
	if one == other {
		return one
	}
	one, other = one.nudged(), other.nudged()
	if one.attribute != other.attribute {
		walk.writeClass = walk.writeClass ||
			classAttributes[one.attribute] || classAttributes[other.attribute]
		one.attribute, other.attribute = "", ""
	}
	if one != other {
		walk.writeClass = true
	}
	return one
}

// writesUnseenClass reports whether the page entered at entry may write
// a class its templates' text does not show. roots are the templates'
// bodies, by name.
func writesUnseenClass(
	roots map[string]*parse.ListNode, entry string,
) bool {
	walk := &markupWalk{roots: roots, ends: map[walkStart]markupContext{}}
	walk.call(entry, markupContext{})
	return walk.writeClass
}
