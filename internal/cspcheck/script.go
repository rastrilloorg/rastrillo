package cspcheck

import (
	"fmt"
	"regexp"
	"strings"
)

// inspectScript reads the markup in a script's string and template
// literals, and its setAttribute calls. Code outside a literal is not
// markup, so i<n is not taken for a tag, and a comment that explains
// the rule is not taken for code that breaks it.
func inspectScript(source []byte) []problem {
	var problems []problem
	for _, found := range scriptLiterals(string(source)) {
		markup := inspectMarkup(found.text, found.line)
		if found.quoted {
			markup = onLine(markup, found.line)
		}
		problems = append(problems, markup...)
		attribute := strings.ToLower(found.text)
		if found.setsAttribute &&
			(attribute == "style" || isHandler(attribute)) {
			problems = append(problems, problem{
				line: found.line, text: setAttributeProblem(attribute),
			})
		}
	}
	return problems
}

func setAttributeProblem(attribute string) string {
	if attribute == "style" {
		return `setAttribute("style"), which the CSP drops: set a ` +
			"class, or a property with element.style.setProperty"
	}
	return fmt.Sprintf(`setAttribute("%s"), which the CSP blocks: `+
		"use addEventListener", attribute)
}

// literal is the text of one string, or one stretch of a template
// literal between its ${} holes, with escapes' backslashes removed.
type literal struct {
	text string
	line int
	// quoted is a '' or "" string: one line, whatever it escapes.
	quoted bool
	// setsAttribute is a literal that is setAttribute's first argument.
	setsAttribute bool
}

// setAttributeCall matches the code just before setAttribute's first
// argument.
var setAttributeCall = regexp.MustCompile(`setAttribute\s*\(\s*$`)

// scriptLexer walks a script far enough to know a literal from code
// and comments. It does not know a regular expression literal from a
// division, so a quote inside one may start a string; a quoted string
// ends at its line's end, which limits the damage to that line.
type scriptLexer struct {
	source   string
	index    int
	line     int
	holes    []int // braces open inside each ${} still open
	literals []literal
}

func scriptLiterals(source string) []literal {
	lexer := &scriptLexer{source: source, line: 1}
	for lexer.index < len(source) {
		lexer.step()
	}
	return lexer.literals
}

func (lexer *scriptLexer) step() {
	rest := lexer.source[lexer.index:]
	switch {
	case strings.HasPrefix(rest, "//"):
		lexer.skipTo("\n", 0)
	case strings.HasPrefix(rest, "/*"):
		lexer.skipTo("*/", len("*/"))
	case rest[0] == '"' || rest[0] == '\'':
		lexer.quoted(rest[0])
	case rest[0] == '`':
		lexer.index++
		lexer.template(lexer.callsSetAttribute())
	case rest[0] == '{' && len(lexer.holes) > 0:
		lexer.holes[len(lexer.holes)-1]++
		lexer.index++
	case rest[0] == '}' && len(lexer.holes) > 0:
		lexer.closeBrace()
	default:
		lexer.advance()
	}
}

// advance moves one byte on, counting lines.
func (lexer *scriptLexer) advance() {
	if lexer.source[lexer.index] == '\n' {
		lexer.line++
	}
	lexer.index++
}

// skipTo moves past a comment: up to end, and past it by past bytes.
func (lexer *scriptLexer) skipTo(end string, past int) {
	length := strings.Index(lexer.source[lexer.index:], end)
	if length < 0 {
		length, past = len(lexer.source)-lexer.index, 0
	}
	stop := lexer.index + length + past
	lexer.line += strings.Count(lexer.source[lexer.index:stop], "\n")
	lexer.index = stop
}

// callsSetAttribute reports a literal that starts setAttribute's
// arguments. Only the code just before it is read: a call is short.
func (lexer *scriptLexer) callsSetAttribute() bool {
	before := lexer.source[max(0, lexer.index-64):lexer.index]
	return setAttributeCall.MatchString(before)
}

// quoted reads a single- or double-quoted string, which ends at its
// quote or its line.
func (lexer *scriptLexer) quoted(quote byte) {
	found := literal{line: lexer.line, quoted: true,
		setsAttribute: lexer.callsSetAttribute()}
	var text strings.Builder
	for lexer.index++; lexer.index < len(lexer.source); lexer.index++ {
		character := lexer.source[lexer.index]
		if character == quote || character == '\n' {
			break
		}
		if character == '\\' && lexer.index+1 < len(lexer.source) {
			lexer.index++
			character = lexer.source[lexer.index]
		}
		text.WriteByte(character)
	}
	if lexer.index < len(lexer.source) && lexer.source[lexer.index] == quote {
		lexer.index++
	}
	found.text = text.String()
	lexer.literals = append(lexer.literals, found)
}

// template reads a template literal from just after its backtick or a
// hole's closing brace, up to its closing backtick or its next hole.
func (lexer *scriptLexer) template(setsAttribute bool) {
	found := literal{line: lexer.line, setsAttribute: setsAttribute}
	var text strings.Builder
	defer func() {
		found.text = text.String()
		lexer.literals = append(lexer.literals, found)
	}()
	for lexer.index < len(lexer.source) {
		rest := lexer.source[lexer.index:]
		switch {
		case rest[0] == '`':
			lexer.index++
			return
		case strings.HasPrefix(rest, "${"):
			lexer.index += len("${")
			lexer.holes = append(lexer.holes, 0)
			return
		case rest[0] == '\\' && len(rest) > 1:
			lexer.index++
		}
		text.WriteByte(lexer.source[lexer.index])
		lexer.advance()
	}
}

// closeBrace closes a brace inside a hole, or the hole itself, which
// goes back to reading its template literal.
func (lexer *scriptLexer) closeBrace() {
	last := len(lexer.holes) - 1
	lexer.index++
	if lexer.holes[last] > 0 {
		lexer.holes[last]--
		return
	}
	lexer.holes = lexer.holes[:last]
	lexer.template(false)
}
