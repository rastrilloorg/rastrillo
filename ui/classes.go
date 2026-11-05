package ui

import (
	"bytes"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// classAttribute finds a class attribute's name, and data-rst-class's,
// whose classes select.js copies onto the rows it draws. Text that only
// looks like one costs rules, never a style.
var classAttribute = regexp.MustCompile(
	`(?i)(?:^|[\s"'/])(?:data-rst-)?class\b`)

// readClasses reads the hooks a run of markup writes as classes. A
// class attribute whose value the text does not hold whole is written
// from data or a template, so the page may wear any class. A browser
// splits a class list at HTML's spaces; select.js splits
// data-rst-class at JavaScript's \s, which is wider.
func (reading *pageReading) readClasses(text []byte) {
	for _, span := range classAttribute.FindAllIndex(text, -1) {
		value, state := attributeValue(text[span[1]:])
		if state != valueKnown {
			reading.anyClass = true
			continue
		}
		split := isHTMLSpace
		name := bytes.ToLower(text[span[0]:span[1]])
		if bytes.HasSuffix(name, []byte("data-rst-class")) {
			split = isScriptSpace
		}
		for _, class := range strings.FieldsFunc(value, split) {
			if vendored.IsHook(class) {
				reading.classes = setOf(reading.classes)
				reading.classes[class] = true
			}
		}
	}
}

func isHTMLSpace(character rune) bool {
	return strings.ContainsRune(htmlSpaces, character)
}

// isScriptSpace is true of every character JavaScript's \s matches, and
// of U+0085, which it does not: splitting a list at one more place
// only names more classes.
func isScriptSpace(character rune) bool {
	return unicode.IsSpace(character) || character == '\uFEFF'
}

// shownClasses is a vendored.ShownClass entry for each hook the page
// can wear as a class, those created among them, and
// vendored.ListedClasses, saying there are no others. A page that may
// wear any class gets none: one with a class from data, a hook in a
// string argument or a needs, which may be markup built in Go, or code
// of its own, which may add any class.
func (reading *pageReading) shownClasses(created map[string]bool) []string {
	if reading.anyClass || reading.scripts.own {
		return nil
	}
	classes := maps.Clone(setOf(reading.classes))
	maps.Copy(classes, created)
	entries := []string{vendored.ListedClasses}
	for _, hook := range slices.Sorted(maps.Keys(classes)) {
		entries = append(entries, vendored.ShownClass(hook))
	}
	return entries
}

// anyClassIn lets the page wear any class when text, a string argument
// or a needs, names a hook: it may be a class the walk cannot see.
func (reading *pageReading) anyClassIn(text []byte) {
	reading.anyClass = reading.anyClass || len(vendored.HooksIn(text)) > 0
}
