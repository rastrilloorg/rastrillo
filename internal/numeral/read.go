package numeral

import (
	"errors"
	"math/big"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrInvalid is text with no reading at all: junk, a stray sign, an
// empty pair of parentheses. Callers map it to their own error.
var ErrInvalid = errors.New("numeral: invalid")

// Token is a run of ASCII digits, or one separator mark.
type Token struct {
	Digits string // "" for a separator
	Mark   rune
}

// Convention says which marks are decimal marks and which group.
type Convention struct{ Decimal, Group map[rune]bool }

// Conventions are the readings of money spec §5.1's decision table: the
// locale's own (exactly the strict reading), point-decimal and
// comma-decimal. In all of them a space, an apostrophe and the Arabic
// thousands mark group, and the Arabic decimal mark is a decimal. group
// and decimal are the locale's marks for what is read; ordinary is its
// plain-number decimal mark, read as a decimal too unless it groups.
func Conventions(group, decimal, ordinary string) []Convention {
	base := func(decimals ...rune) Convention {
		made := Convention{
			Decimal: map[rune]bool{'\u066b': true},
			Group: map[rune]bool{' ': true, '\'': true, '\u2019': true,
				'\u066c': true},
		}
		for _, mark := range decimals {
			made.Decimal[mark] = true
		}
		return made
	}
	groupMark, _ := utf8.DecodeRuneInString(FoldSpace(group))
	decimalMark, _ := utf8.DecodeRuneInString(decimal)
	ordinaryMark, _ := utf8.DecodeRuneInString(ordinary)
	local := base(decimalMark, ordinaryMark)
	if groupMark != '.' {
		local.Decimal['.'] = true
	}
	delete(local.Decimal, groupMark)
	local.Group[groupMark] = true
	point := base('.')
	point.Group[','] = true
	comma := base(',')
	comma.Group['.'] = true
	return []Convention{local, point, comma}
}

// Tokenize splits cleaned text into digit runs and single marks and
// refuses anything else. The locale's own marks are marks too, so
// pt-CV's "$" decimal (1284$50) is not junk.
func Tokenize(text string, local Convention) ([]Token, error) {
	var out []Token
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			out = append(out, Token{Digits: run.String()})
			run.Reset()
		}
	}
	for _, char := range text {
		value, digit := Digit(char)
		switch {
		case digit:
			run.WriteByte(byte('0' + value))
		case strings.ContainsRune(".,' \u2019\u066b\u066c", char) ||
			local.Decimal[char] || local.Group[char]:
			flush()
			out = append(out, Token{Mark: char})
		default:
			return nil, ErrInvalid
		}
	}
	flush()
	return out, nil
}

// AllGroups reports whether the tokens have marks and the locale's own
// convention takes every one of them for a group mark.
func AllGroups(tokens []Token, local Convention) bool {
	marks := 0
	for _, token := range tokens {
		if token.Mark != 0 {
			if !local.Group[token.Mark] {
				return false
			}
			marks++
		}
	}
	return marks > 0
}

// Groupings are the locale's own group sizes and the two people type
// anywhere: thousands (3 and 3) and Indian (3, then 2). Equal values
// from different groupings are one reading.
func Groupings(primary, secondary int) [][2]int {
	out := [][2]int{{primary, secondary}}
	for _, sizes := range [][2]int{{3, 3}, {3, 2}} {
		if !slices.Contains(out, sizes) {
			out = append(out, sizes)
		}
	}
	return out
}

// Less orders two canonical decimals by value.
func Less(left, right string) bool {
	first, _ := new(big.Rat).SetString(left)
	second, _ := new(big.Rat).SetString(right)
	return first.Cmp(second) < 0
}
