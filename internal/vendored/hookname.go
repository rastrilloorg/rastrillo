package vendored

import (
	"bytes"
	"regexp"
	"slices"

	"github.com/tdewolff/parse/v2/css"
)

// hookName matches the attribute and class names components carry,
// the class forms' "__" included (.rst-switch__track beside
// [rst-switch-track]): read without it, every rule naming a class part
// would be kept for every page.
var hookName = regexp.MustCompile(`^rst-[a-z0-9_-]+$`)

// IsHook reports whether the whole of name is a hook. Every reader of
// hook names, in a stylesheet, a template or a script, goes through it,
// so a page's hooks and the stylesheet's rules agree on what a name is.
func IsHook(name string) bool {
	return hookName.MatchString(name)
}

// HooksIn returns the hooks named in a run of markup or script text,
// each once, sorted. A name is a whole run of letters, digits, "_" and
// "-", so rst-switch__track never reads as rst-switch, and neither
// data-rst-select nor --rst-press is a hook.
func HooksIn(text []byte) []string {
	var hooks []string
	for _, name := range bytes.FieldsFunc(text, outsideName) {
		if IsHook(string(name)) {
			hooks = append(hooks, string(name))
		}
	}
	slices.Sort(hooks)
	return slices.Compact(hooks)
}

func outsideName(character rune) bool {
	switch {
	case 'a' <= character && character <= 'z',
		'A' <= character && character <= 'Z',
		'0' <= character && character <= '9',
		character == '_', character == '-':
		return false
	}
	return true
}

// hookAt returns the hook named at tokens[index]: an identifier that is an
// attribute's name, after "[", or a class, after ".". An identifier
// anywhere else, such as an attribute's value, is not a hook.
func hookAt(tokens []css.Token, index int) (string, bool) {
	token := tokens[index]
	if index == 0 || token.TokenType != css.IdentToken ||
		!IsHook(string(token.Data)) {
		return "", false
	}
	before := tokens[index-1]
	isAttribute := before.TokenType == css.LeftBracketToken
	isClass := before.TokenType == css.DelimToken &&
		string(before.Data) == "."
	return string(token.Data), isAttribute || isClass
}

func hooksNamed(tokens []css.Token) []string {
	var named []string
	for index := range tokens {
		if name, ok := hookAt(tokens, index); ok {
			named = append(named, name)
		}
	}
	return named
}
