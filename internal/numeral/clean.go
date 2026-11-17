// Package numeral is the reading and writing that money and plain
// numbers share: cleaning what a person typed, splitting it into digits
// and marks, reading the marks under a convention, and grouping digits
// for display. One copy keeps a table's money and numbers from reading
// "1.500" two ways (the multi-currency money spec, §5.1; the Fichas
// level-up spec rev 4, R2).
package numeral

import (
	"strings"
	"unicode"

	"amadan.net/rastrillo/rastrillo/locales"
)

// isBidi reports the marks a pasted, displayed value carries.
func isBidi(char rune) bool {
	return char == '\u200e' || char == '\u200f' || char == '\u061c' ||
		('\u202a' <= char && char <= '\u202e') ||
		('\u2066' <= char && char <= '\u2069')
}

// Clean removes bidi marks and folds every space separator to U+0020,
// then trims, so a value of only spaces or U+200F is blank.
func Clean(text string) string {
	var out strings.Builder
	for _, char := range text {
		switch {
		case isBidi(char):
		case unicode.IsSpace(char) || unicode.In(char, unicode.Zs):
			out.WriteByte(' ')
		default:
			out.WriteRune(char)
		}
	}
	return strings.TrimSpace(out.String())
}

// FoldSpace is a mark as Clean leaves it: any space mark becomes " ".
func FoldSpace(mark string) string {
	if strings.TrimFunc(mark, func(char rune) bool {
		return unicode.IsSpace(char) || unicode.In(char, unicode.Zs)
	}) == "" {
		return " "
	}
	return mark
}

// Digit is a digit's value in any shipped numbering system, or ASCII.
// Unicode has other decimal-digit runs (mathematical bold, for one)
// that nobody types into a number, so they are refused.
func Digit(char rune) (int, bool) {
	value := digitValue[char]
	return value - 1, value > 0
}

// digitValue maps every digit to its value plus one, so zero means
// "not a digit". It holds every numbering system any shipped locale
// uses, linked or not: what a person can type does not depend on which
// packs an app links.
var digitValue = func() map[rune]int {
	values := map[rune]int{}
	for value := 0; value < 10; value++ {
		values[rune('0'+value)] = value + 1
	}
	for _, set := range locales.DigitSets() {
		for value, char := range []rune(set) {
			values[char] = value + 1
		}
	}
	return values
}()

// LocalDigits writes ASCII digits in digits, a numbering system's ten.
func LocalDigits(text string, digits []rune) string {
	return strings.Map(func(char rune) rune {
		if char >= '0' && char <= '9' {
			return digits[char-'0']
		}
		return char
	}, text)
}
