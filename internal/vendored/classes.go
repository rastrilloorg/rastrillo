package vendored

import (
	"bytes"
	"slices"
	"strings"

	"github.com/tdewolff/parse/v2/css"
)

// ListedClasses is the entry, among the hooks a page shows, saying that
// its ShownClass entries are every hook it writes as a class, so a
// class selector for any other hook cannot match it. A page without it
// may wear any hook it names as a class.
const ListedClasses = "."

// ShownClass is the entry, among the hooks a page shows, saying that it
// writes hook as a class.
func ShownClass(hook string) string { return "." + hook }

// IsShownClass reports whether entry is ListedClasses or one ShownClass
// wrote.
func IsShownClass(entry string) bool {
	hook, found := strings.CutPrefix(entry, ".")
	return found && (hook == "" || IsHook(hook))
}

// readClasses returns the hooks a page writes as classes, or nil when
// they are not listed and any hook it shows may be a class.
func readClasses(hooks map[string]bool) map[string]bool {
	if !hooks[ListedClasses] {
		return nil
	}
	classes := map[string]bool{}
	for entry := range hooks {
		hook, found := strings.CutPrefix(entry, ".")
		if found && IsHook(hook) {
			classes[hook] = true
		}
	}
	return classes
}

// keptText is what a style rule writes into a slice: nothing when the
// page can match none of its comma-separated selectors, the rule as it
// was read when it can match them all, its classes are not listed or
// it nests rules, and otherwise the rule with only those it can match.
// A page whose classes are not listed may wear any class, whether or
// not it names the hook, so it keeps every selector of a rule it
// keeps; and a nested rule's & takes the specificity of the most
// specific selector in the list, matched or not (css-nesting-1).
func keptText(rule rule, page shown) []byte {
	selectors := splitAtCommas(rule.selector)
	met := make([]bool, len(selectors))
	for index, selector := range selectors {
		met[index] = selectorMet(selector, page)
	}
	if !slices.Contains(met, true) {
		return nil
	}
	if !slices.Contains(met, false) || page.classes == nil || rule.nested {
		return rule.text
	}
	lead, entries, block, ok := cutSelectors(rule.text)
	if !ok || len(entries) != len(selectors) {
		return rule.text
	}
	text := slices.Clone(lead)
	for index, entry := range entries {
		if !met[index] {
			continue
		}
		if len(text) > len(lead) {
			text = append(text, ',')
		}
		text = append(text, entry...)
	}
	return append(text, block...)
}

// cutSelectors cuts a style rule's text into the space before it, each
// of its comma-separated selectors without the space around it, and
// its block, from the opening brace on. ok is false for text with no
// block.
func cutSelectors(text []byte) (
	lead []byte, entries [][]byte, block []byte, ok bool,
) {
	body := bytes.TrimLeft(text, cssSpaces)
	lead = text[:len(text)-len(body)]
	lexer := css.NewLexer(input(body))
	depth, start := 0, 0
	for offset, length := 0, 0; ; offset += length {
		token, data := lexer.Next()
		length = len(data)
		switch token {
		case css.ErrorToken:
			return nil, nil, nil, false
		case css.FunctionToken, css.LeftParenthesisToken,
			css.LeftBracketToken:
			depth++
		case css.RightParenthesisToken, css.RightBracketToken:
			depth--
		case css.CommaToken, css.LeftBraceToken:
			if depth > 0 {
				continue
			}
			entries = append(entries,
				bytes.Trim(body[start:offset], cssSpaces))
			start = offset + 1
			if token == css.LeftBraceToken {
				return lead, entries, body[offset:], true
			}
		}
	}
}

// cssSpaces is the space CSS allows around a selector.
const cssSpaces = " \t\n\r\f"
