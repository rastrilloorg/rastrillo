package vendored

import (
	"bytes"

	"github.com/tdewolff/parse/v2/css"
)

// anySelectorMet reports whether the page can show one of list's
// comma-separated selectors.
func anySelectorMet(list []css.Token, page shown) bool {
	for _, selector := range splitAtCommas(list) {
		if selectorMet(selector, page) {
			return true
		}
	}
	return false
}

// selectorMet reports whether the page shows every hook selector
// needs, with a value each attribute test accepts. A hook inside
// :not() is never needed, since the rule applies where it is absent.
// Any other function's arguments, :is(), :where(), :has() and the "of"
// list of :nth-child() among them, need one branch.
func selectorMet(selector []css.Token, page shown) bool {
	for index := 0; index < len(selector); index++ {
		if selector[index].TokenType == css.FunctionToken {
			end := closingParenthesis(selector, index)
			negated := bytes.EqualFold(selector[index].Data, []byte("not("))
			if !negated && !anySelectorMet(selector[index+1:end], page) {
				return false
			}
			index = end
			continue
		}
		if _, ok := hookAt(selector, index); ok &&
			!page.hookMet(selector, index) {
			return false
		}
	}
	return true
}

func splitAtCommas(list []css.Token) [][]css.Token {
	var selectors [][]css.Token
	depth, start := 0, 0
	for index, token := range list {
		switch token.TokenType {
		case css.FunctionToken, css.LeftParenthesisToken,
			css.LeftBracketToken:
			depth++
		case css.RightParenthesisToken, css.RightBracketToken:
			depth--
		case css.CommaToken:
			if depth == 0 {
				selectors = append(selectors, list[start:index])
				start = index + 1
			}
		}
	}
	return append(selectors, list[start:])
}

// closingParenthesis returns the index of the parenthesis that closes
// the function opened at tokens[open], or len(tokens) for a function
// left open.
func closingParenthesis(tokens []css.Token, open int) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		switch tokens[index].TokenType {
		case css.FunctionToken, css.LeftParenthesisToken:
			depth++
		case css.RightParenthesisToken:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return len(tokens)
}
