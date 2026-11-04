package vendored

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2/css"
)

// ShownValue is the entry, among the hooks a page shows, saying that it
// writes hook with exactly value. A hook with such entries shows only
// their values, so a rule for another variant of it is cut; a hook with
// none may carry any value, as when a template writes it from data.
func ShownValue(hook, value string) string {
	return hook + "=" + strconv.Quote(value)
}

// IsShownValue reports whether entry is one ShownValue wrote.
func IsShownValue(entry string) bool {
	hook, quoted, found := strings.Cut(entry, "=")
	_, err := strconv.Unquote(quoted)
	return found && IsHook(hook) && err == nil
}

// shown is a page's hooks, with the values it writes for those whose
// every value is known.
type shown struct {
	hooks  map[string]bool
	values map[string][]string
}

func readShown(hooks map[string]bool) shown {
	page := shown{hooks: hooks, values: map[string][]string{}}
	for entry := range hooks {
		hook, quoted, found := strings.Cut(entry, "=")
		value, err := strconv.Unquote(quoted)
		if found && IsHook(hook) && err == nil {
			page.values[hook] = append(page.values[hook], value)
		}
	}
	return page
}

// hookMet reports whether the page shows the hook named at tokens[at],
// with a value its attribute test, if it has one, accepts.
func (page shown) hookMet(tokens []css.Token, at int) bool {
	attribute := tokens[at-1].TokenType == css.LeftBracketToken
	return page.hooks[string(tokens[at].Data)] &&
		(!attribute || page.attributeMet(tokens, at))
}

// attributeMet reports whether the page can show the attribute selector
// that names a hook at tokens[at]: always, unless the selector asks for
// a value and the page writes the hook only with values that fail it.
// A value written with an escape is not read, so its rule is kept.
func (page shown) attributeMet(tokens []css.Token, at int) bool {
	values, known := page.values[string(tokens[at].Data)]
	operator, wanted, sensitive, ok := attributeTest(tokens[at+1:])
	if !known || !ok {
		return true
	}
	for _, value := range values {
		if !sensitive {
			value, wanted = strings.ToLower(value), strings.ToLower(wanted)
		}
		if valueMatches(operator, value, wanted) {
			return true
		}
	}
	return false
}

// attributeTest reads the rest of an attribute selector after its name:
// the operator, the value it asks for and whether case counts. ok is
// false for a selector it cannot read with certainty, and for one with
// no value, which any value meets.
func attributeTest(tokens []css.Token) (
	operator, wanted string, sensitive, ok bool,
) {
	var words []css.Token
	for _, token := range tokens {
		if token.TokenType == css.RightBracketToken {
			break
		}
		if token.TokenType != css.WhitespaceToken {
			words = append(words, token)
		}
	}
	if len(words) < 2 || len(words) > 3 {
		return "", "", false, false
	}
	operator = string(words[0].Data)
	value := words[1]
	if bytes.ContainsRune(value.Data, '\\') {
		return "", "", false, false
	}
	switch value.TokenType {
	case css.IdentToken:
		wanted = string(value.Data)
	case css.StringToken:
		wanted = string(value.Data[1 : len(value.Data)-1])
	default:
		return "", "", false, false
	}
	sensitive = true
	if len(words) == 3 {
		flag := strings.ToLower(string(words[2].Data))
		if flag != "i" && flag != "s" {
			return "", "", false, false
		}
		sensitive = flag == "s"
	}
	return operator, wanted, sensitive, true
}

// valueMatches applies an attribute selector's operator, as CSS
// Selectors 4 defines it, to a value an element carries.
func valueMatches(operator, value, wanted string) bool {
	switch operator {
	case "=":
		return value == wanted
	case "~=":
		words := strings.FieldsFunc(value, htmlSpace)
		return wanted != "" && !strings.ContainsFunc(wanted, htmlSpace) &&
			containsWord(words, wanted)
	case "|=":
		return value == wanted || strings.HasPrefix(value, wanted+"-")
	case "^=":
		return wanted != "" && strings.HasPrefix(value, wanted)
	case "$=":
		return wanted != "" && strings.HasSuffix(value, wanted)
	case "*=":
		return wanted != "" && strings.Contains(value, wanted)
	}
	return true
}

func containsWord(words []string, wanted string) bool {
	for _, word := range words {
		if word == wanted {
			return true
		}
	}
	return false
}

// htmlSpace is HTML's ASCII whitespace, which separates the words ~=
// compares.
func htmlSpace(character rune) bool {
	return strings.ContainsRune(" \t\n\f\r", character)
}
