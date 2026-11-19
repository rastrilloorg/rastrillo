package cspcheck

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The messages say what the CSP does to each thing and what to write
// instead, because the reader is usually an agent that will act on the
// line and read nothing else.
const (
	styleAttribute = "a style attribute, which the CSP drops: give " +
		"the element a class and style it in your own stylesheet"
	styleBlock = "a <style> block, which the CSP drops: move its " +
		"rules to your own stylesheet in static/"
	handlerAttribute = "an %s attribute, which the CSP blocks: add " +
		"the listener with addEventListener in your own script in static/"
	inlineScript = "an inline <script>, which the CSP blocks: move " +
		"it to a file in static/ and load it with <script defer src>"
	javascriptURL = "a javascript: URL, which the CSP blocks: use a " +
		"button with a listener, or a link to a route"
)

// tagPattern matches an opening tag: its name, then its attributes,
// quoted values whole, up to the closing > or the end of the text. A
// tag cut off by the end is a Go string fragment, "<span style=\"" +
// colour, which still writes the attribute, so a quote left open at
// the end counts as a value. Only a closed tag is taken for a <style>
// or <script> element: the pattern `(?i)<script\b` names one and
// writes none.
var tagPattern = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9:-]*)` +
	`((?:[^>"']|"[^"]*"|'[^']*')*(?:"[^"]*|'[^']*)?)(>|$)`)

// attributePattern matches one attribute and its value, if it has one.
var attributePattern = regexp.MustCompile(
	"([^\\s\"'=<>/`]+)(?:\\s*=\\s*(\"[^\"]*\"|'[^']*'|[^\\s\"'=<>`]+))?")

// blankedPattern matches what a template says that is not markup: its
// actions and comments, and HTML comments.
var blankedPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}|<!--.*?-->`)

// urlAttributes are the attributes a browser follows as a URL, where a
// javascript: URL runs. In any other attribute it is only text.
var urlAttributes = []string{
	"href", "src", "action", "formaction", "xlink:href",
}

// scriptTypes are the type attributes a browser runs a <script> for:
// none, a module, speculation rules, or a JavaScript MIME type's
// essence (mimesniff.spec.whatwg.org). Any other type is a data block,
// which the CSP ignores.
var scriptTypes = []string{
	"", "module", "speculationrules",
	"application/ecmascript", "application/javascript",
	"application/x-ecmascript", "application/x-javascript",
	"text/ecmascript", "text/javascript", "text/javascript1.0",
	"text/javascript1.1", "text/javascript1.2", "text/javascript1.3",
	"text/javascript1.4", "text/javascript1.5", "text/jscript",
	"text/livescript", "text/x-ecmascript", "text/x-javascript",
}

// inspectTemplate reads a template, blanking its actions and comments
// first.
func inspectTemplate(source []byte) []problem {
	return inspectMarkup(blankTemplate(string(source)), 1)
}

// blankTemplate keeps every newline, so offsets still give lines.
func blankTemplate(source string) string {
	return blankedPattern.ReplaceAllStringFunc(source, blank)
}

func blank(text string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' {
			return '\n'
		}
		return ' '
	}, text)
}

// inspectMarkup reads markup whose first line is firstLine, reporting
// each attribute on its own line.
func inspectMarkup(markup string, firstLine int) []problem {
	var problems []problem
	for _, match := range tagPattern.FindAllStringSubmatchIndex(markup, -1) {
		name := strings.ToLower(markup[match[2]:match[3]])
		if match[6] == match[7] { // cut off: an element it is not
			name = ""
		}
		for _, found := range inspectTag(name, markup[match[4]:match[5]]) {
			offset := match[4] + found.offset
			problems = append(problems, problem{
				line: firstLine + strings.Count(markup[:offset], "\n"),
				text: found.text,
			})
		}
	}
	return problems
}

// placed is a problem at an offset into a tag's attributes.
type placed struct {
	offset int
	text   string
}

// inspectTag returns what the CSP drops in one opening tag.
func inspectTag(name, attributes string) []placed {
	var found []placed
	if name == "style" {
		found = append(found, placed{text: styleBlock})
	}
	values := map[string]string{}
	for _, match := range attributePattern.FindAllStringSubmatchIndex(
		attributes, -1) {
		attribute := strings.ToLower(attributes[match[2]:match[3]])
		value := ""
		if match[4] >= 0 {
			value = strings.Trim(attributes[match[4]:match[5]], `"'`)
		}
		values[attribute] = value
		for _, text := range inspectAttribute(attribute, value) {
			found = append(found, placed{offset: match[0], text: text})
		}
	}
	if name == "script" && runsInline(values) {
		found = append(found, placed{text: inlineScript})
	}
	return found
}

func inspectAttribute(attribute, value string) []string {
	switch {
	case attribute == "style":
		return []string{styleAttribute}
	case isHandler(attribute):
		return []string{fmt.Sprintf(handlerAttribute, attribute)}
	case slices.Contains(urlAttributes, attribute) && strings.HasPrefix(
		strings.ToLower(strings.TrimSpace(value)), "javascript:"):
		return []string{javascriptURL}
	}
	return nil
}

// isHandler reports an on* event handler attribute. No attribute HTML
// defines starts with "on" but those, and a hyphenated name such as
// data-onclick is a different attribute.
func isHandler(attribute string) bool {
	rest, found := strings.CutPrefix(attribute, "on")
	if !found || rest == "" {
		return false
	}
	return strings.Trim(rest, "abcdefghijklmnopqrstuvwxyz") == ""
}

// runsInline reports a <script> the browser runs that has no src. An
// import map is held to the CSP too, but passes: skill/scripts.md has
// an app admit one by its hash in Options.CSP, which this cannot see.
func runsInline(values map[string]string) bool {
	if _, hasSource := values["src"]; hasSource {
		return false
	}
	kind, _, _ := strings.Cut(values["type"], ";")
	kind = strings.ToLower(strings.TrimSpace(kind))
	return slices.Contains(scriptTypes, kind)
}
