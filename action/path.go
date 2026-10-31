package action

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Path is a route's path, written once and in full. The same value
// registers the route and fills the URL an action redirects to, so a
// renamed route is one edit and a redirect cannot drift away from it.
type Path string

// For fills the path's parameters with values, in order. Each value is
// escaped as one segment, so "a/b" stays one parameter and PathValue
// gives it back whole. It panics on a wrong count, a catch-all or a
// value that leads off the route: each is a mistake in the code, and
// no request could put it right.
func (path Path) For(values ...any) string {
	pieces := path.pieces()
	path.refuseCatchAll(pieces)
	names := parameterNames(pieces)
	if len(names) != len(values) {
		panic(fmt.Sprintf("action: Path(%q).For has %s and was given %s. "+
			"Pass one value for each parameter, in order.",
			string(path), listed("parameters", braced(names)),
			listed("values", quoted(values))))
	}
	var filled strings.Builder
	next := 0
	for position, piece := range pieces {
		if !piece.parameter {
			filled.WriteString(piece.text)
			continue
		}
		value := fmt.Sprint(values[next])
		filled.WriteString(path.escape(piece, ending(pieces, position), value))
		next++
	}
	return filled.String()
}

// piece is a run of literal text, or one parameter with its braces.
type piece struct {
	text      string
	parameter bool
}

// inside is a parameter without its braces: "id:[0-9]+".
func (parameter piece) inside() string {
	return parameter.text[1 : len(parameter.text)-1]
}

// pieces splits the path the way chi reads it. A parameter's braces
// are counted, so a pattern's own braces, as in {id:[0-9]{2}}, stay
// inside it, and a parameter can sit inside a segment (/x-{id}.json).
func (path Path) pieces() []piece {
	var pieces []piece
	rest := string(path)
	for rest != "" {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			pieces = append(pieces, piece{text: rest})
			break
		}
		end := path.closingBrace(rest, open)
		if open > 0 {
			pieces = append(pieces, piece{text: rest[:open]})
		}
		pieces = append(pieces,
			piece{text: rest[open : end+1], parameter: true})
		rest = rest[end+1:]
	}
	return pieces
}

// shape is the pattern as chi matches it: each parameter's name left
// out and its regexp kept, because chi tells {id} and {id:[0-9]+} apart
// but not {id} and {name}. The regexp is anchored as chi anchors it, so
// [0-9]+ and ^[0-9]+$, which chi compiles alike, are one shape.
func (path Path) shape() string {
	var shape strings.Builder
	for _, piece := range path.pieces() {
		if !piece.parameter {
			shape.WriteString(piece.text)
			continue
		}
		_, pattern, found := strings.Cut(piece.inside(), ":")
		if found && pattern != "" {
			pattern = anchored(pattern)
		}
		shape.WriteString("{:" + pattern + "}")
	}
	return shape.String()
}

// anchored is a parameter's regexp as chi compiles it: chi adds a ^ and
// a $ where the pattern lacks them, and leaves those it has.
func anchored(pattern string) string {
	if !strings.HasPrefix(pattern, "^") {
		pattern = "^" + pattern
	}
	if !strings.HasSuffix(pattern, "$") {
		pattern += "$"
	}
	return pattern
}

// closingBrace is the index of the brace that closes the one at open.
func (path Path) closingBrace(text string, open int) int {
	depth := 0
	for position := open; position < len(text); position++ {
		switch text[position] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return position
			}
		}
	}
	panic(fmt.Sprintf("action: Path(%q) has a { with no } to close it. "+
		"Close each parameter, as in \"/things/{id}\".", string(path)))
}

// refuseCatchAll panics on chi's catch-all, a * that ends the pattern
// outside any parameter: chi matches it to any rest of the path, so
// there is no one segment For could fill it with.
func (path Path) refuseCatchAll(pieces []piece) {
	if len(pieces) == 0 {
		return
	}
	last := pieces[len(pieces)-1]
	if last.parameter || !strings.HasSuffix(last.text, "*") {
		return
	}
	named := strings.TrimSuffix(string(path), "*") + "{name}"
	panic(fmt.Sprintf("action: Path(%q).For cannot fill the * at its end, "+
		"which matches any rest of the path. Name the part you fill "+
		"with a parameter instead, as in %q.", string(path), named))
}

// ending is the character chi stops a parameter's value at: the one
// after it in the pattern, or "" when a slash, another parameter or
// nothing follows. chi ends every value at a slash, and For escapes
// one, so a slash needs no check.
func ending(pieces []piece, position int) string {
	if position+1 == len(pieces) || pieces[position+1].parameter {
		return ""
	}
	following := pieces[position+1].text[:1]
	if following == "/" {
		return ""
	}
	return following
}

// escape refuses an empty value, "." and "..", because each sends the
// URL off the route: an empty segment can make //admin, which a
// browser reads as another host, and a dot segment is cleaned away. It
// refuses a value holding the character that ends its parameter, which
// For cannot escape (url.PathEscape leaves - and . alone), because chi
// would end the value there and read another resource: "a-b" in
// /{owner}-{name} is read as owner "a".
func (path Path) escape(parameter piece, end, text string) string {
	name, pattern, _ := strings.Cut(parameter.inside(), ":")
	if text == "" || text == "." || text == ".." {
		panic(fmt.Sprintf("action: Path(%q).For was given %q for {%s}. "+
			"A value cannot be empty, \".\" or \"..\", because the URL "+
			"would leave the route. Check the value you pass for {%s}.",
			string(path), text, name, name))
	}
	if end != "" {
		path.refuseEnding(name, end, text)
	}
	if pattern != "" {
		path.refuseUnmatched(name, pattern, text)
	}
	return url.PathEscape(text)
}

// refuseEnding panics on a value holding end, the character after its
// parameter in the pattern, as it is given or once escaped: chi reads
// the escaped text under a tenant, or whenever escaping is not Go's
// default, so the F in "%2F" ends {id} in /{id}F{rest} as a literal F
// would.
func (path Path) refuseEnding(name, end, text string) {
	if strings.Contains(text, end) {
		panic(fmt.Sprintf("action: Path(%q).For was given %q for {%s}. "+
			"The value contains %q, which ends {%s} in the path, so chi "+
			"would read a different value from the URL. Pass a value "+
			"without %q, or put a / after {%s} in the path.",
			string(path), text, name, end, name, end, name))
	}
	if escaped := url.PathEscape(text); strings.Contains(escaped, end) {
		panic(fmt.Sprintf("action: Path(%q).For was given %q for {%s}. "+
			"Escaped for a URL it becomes %q, which contains %q, the "+
			"character that ends {%s} in the path, so chi would read a "+
			"different value from the URL. Pass a value whose escaped "+
			"form has no %q, or put a / after {%s} in the path.",
			string(path), text, name, escaped, end, name, end, name))
	}
}

// refuseUnmatched panics on a value chi would not match to the
// parameter's pattern, anchored as chi anchors it, since no route would
// serve the URL. It also refuses a value that escaping changes: chi
// checks the pattern against the escaped text under a tenant's base
// path, which sets RawPath, and the unescaped text at the root, so only
// a value that is the same both ways matches both. A pattern that does
// not compile is left to chi, which refuses it when it is registered.
func (path Path) refuseUnmatched(name, pattern, text string) {
	matcher, err := regexp.Compile(anchored(pattern))
	if err == nil && !matcher.MatchString(text) {
		panic(fmt.Sprintf("action: Path(%q).For was given %q for {%s}, "+
			"which its pattern %q does not match, so no route would "+
			"serve the URL. Pass a value the pattern matches.",
			string(path), text, name, pattern))
	}
	if url.PathEscape(text) != text {
		panic(fmt.Sprintf("action: Path(%q).For was given %q for {%s}, "+
			"which has a pattern, and the value changes when it is "+
			"escaped for a URL. chi checks a pattern against the escaped "+
			"text under a tenant's base path and the unescaped text at "+
			"the root, so the URL would not match both. Pass a value of "+
			"letters, digits, - and _, or drop the pattern from {%s}.",
			string(path), text, name, name))
	}
}

// parameterNames is each parameter's name, without a pattern after a
// colon, so a message says {id} and not {id:[0-9]+}.
func parameterNames(pieces []piece) []string {
	var names []string
	for _, piece := range pieces {
		if !piece.parameter {
			continue
		}
		name, _, _ := strings.Cut(piece.inside(), ":")
		names = append(names, name)
	}
	return names
}

func quoted(values []any) []string {
	texts := make([]string, len(values))
	for position, value := range values {
		texts[position] = fmt.Sprintf("%q", fmt.Sprint(value))
	}
	return texts
}

func braced(names []string) []string {
	texts := make([]string, len(names))
	for position, name := range names {
		texts[position] = "{" + name + "}"
	}
	return texts
}

// listed names a list in a sentence: "no values" when there is none.
func listed(kind string, items []string) string {
	if len(items) == 0 {
		return "no " + kind
	}
	return "the " + kind + " " + strings.Join(items, ", ")
}
