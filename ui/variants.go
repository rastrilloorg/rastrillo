package ui

import (
	"bytes"
	"maps"
	"regexp"
	"slices"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// hookRun finds a run that may be a hook's name; readValues checks its
// edges as HooksIn does.
var hookRun = regexp.MustCompile(`rst-[a-z0-9_-]+`)

// readValues reads the value each hook attribute in a run of markup is
// written with. A value the text holds whole is known; one that runs
// into a template action, or that the text cannot spell plainly, could
// be anything, so its hook keeps every variant's rules. A name that
// ends the text is open: html/template writes no "=value" from data
// after a name (only a template.HTMLAttr, markup built in Go, which a
// page asks for with needs), so its value, if any, is literal text that
// follows, starting with "=", which counts for every open hook.
func (reading *pageReading) readValues(text []byte) {
	if trimmed := bytes.TrimLeft(text, htmlSpaces); len(trimmed) > 0 &&
		trimmed[0] == '=' {
		value, state := attributeValue(trimmed)
		reading.follows(value, state == valueKnown)
	}
	for _, span := range hookRun.FindAllIndex(text, -1) {
		start, end := span[0], span[1]
		if start > 0 && !outsideHook(text[start-1]) ||
			end < len(text) && !outsideHook(text[end]) {
			continue
		}
		hook := string(text[start:end])
		value, state := attributeValue(text[end:])
		switch state {
		case valueUnknown:
			reading.anyValue(hook)
		case valueOpen:
			reading.open = setOf(reading.open)
			reading.open[hook] = true
			fallthrough
		default:
			reading.addValue(hook, value)
		}
	}
}

// addValue records value as one hook is written with.
func (reading *pageReading) addValue(hook, value string) {
	if reading.values == nil {
		reading.values = map[string]map[string]bool{}
	}
	reading.values[hook] = setOf(reading.values[hook])
	reading.values[hook][value] = true
}

// follows records a value written as literal text after a template
// action, which may belong to any open hook; known is false when the
// text does not hold the whole value.
func (reading *pageReading) follows(value string, known bool) {
	if !known {
		reading.anyFollows = true
		return
	}
	reading.following = setOf(reading.following)
	reading.following[value] = true
}

func setOf(set map[string]bool) map[string]bool {
	if set == nil {
		return map[string]bool{}
	}
	return set
}

// anyValue marks hook as written with a value no template text holds.
func (reading *pageReading) anyValue(hook string) {
	reading.unknown = setOf(reading.unknown)
	reading.unknown[hook] = true
}

// anyValueIn marks every hook text names as written with any value: a
// name in a string argument or a needs is markup the walk cannot see.
func (reading *pageReading) anyValueIn(text []byte) {
	for _, hook := range vendored.HooksIn(text) {
		reading.anyValue(hook)
	}
}

// shownValues is a vendored.ShownValue entry for each value of each
// hook whose every value the page's text holds. A hook a script
// creates is left out, and every hook on a page that runs code of its
// own: a script may write any value.
func (reading *pageReading) shownValues(created map[string]bool) []string {
	if reading.scripts.own {
		return nil
	}
	var entries []string
	for _, hook := range slices.Sorted(maps.Keys(reading.values)) {
		open := reading.open[hook]
		if reading.unknown[hook] || created[hook] || !reading.hooks[hook] ||
			open && reading.anyFollows {
			continue
		}
		values := maps.Clone(reading.values[hook])
		if open {
			maps.Copy(values, reading.following)
		}
		for _, value := range slices.Sorted(maps.Keys(values)) {
			entries = append(entries, vendored.ShownValue(hook, value))
		}
	}
	return entries
}

// A valueState says how much of an attribute's value the text holds.
type valueState int

const (
	valueKnown   valueState = iota // all of it, or the attribute is bare
	valueOpen                      // the text ends at the name
	valueUnknown                   // the text ends inside the value
)

// attributeValue reads what follows a hook's name: no value for a bare
// attribute (or a name that is not one, which only adds a value), the
// value of name="value", name='value' or name=value, and unknown when
// the text ends before the value does, or the value holds a character
// reference this does not decode.
func attributeValue(rest []byte) (string, valueState) {
	rest = bytes.TrimLeft(rest, htmlSpaces)
	if len(rest) == 0 {
		return "", valueOpen
	}
	if rest[0] != '=' {
		return "", valueKnown
	}
	rest = bytes.TrimLeft(rest[1:], htmlSpaces)
	if len(rest) == 0 {
		return "", valueUnknown
	}
	var raw []byte
	if quote := rest[0]; quote == '"' || quote == '\'' {
		closing := bytes.IndexByte(rest[1:], quote)
		if closing < 0 {
			return "", valueUnknown
		}
		raw = rest[1 : 1+closing]
	} else {
		end := bytes.IndexAny(rest, htmlSpaces+">")
		if end < 0 {
			return "", valueUnknown
		}
		raw = rest[:end]
	}
	if bytes.ContainsAny(raw, "&\"'<=`") {
		return "", valueUnknown
	}
	return string(raw), valueKnown
}

// htmlSpaces is HTML's ASCII whitespace.
const htmlSpaces = " \t\n\f\r"

// outsideHook reports whether character cannot be part of a hook's
// name, as HooksIn reads names.
func outsideHook(character byte) bool {
	return !('a' <= character && character <= 'z' ||
		'A' <= character && character <= 'Z' ||
		'0' <= character && character <= '9' ||
		character == '_' || character == '-')
}
