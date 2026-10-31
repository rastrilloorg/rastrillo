package main

import (
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"
)

// packageName derives a valid Go identifier from the app name for the
// scaffolded app package: name also serves as the module path, where
// hyphens (and other punctuation) are legal, but a package clause
// needs an identifier. Non-identifier runes are dropped rather than
// rejected, so every name rastrillo new already accepts keeps working.
// A leading digit (decoded properly, not just the first byte, so a
// multi-byte leading digit is still caught) or a bare Go keyword — a
// package clause can't start with either — earns the same "app" prefix.
func packageName(name string) string {
	var identifier strings.Builder
	for _, letter := range name {
		if letter == '_' || unicode.IsLetter(letter) ||
			unicode.IsDigit(letter) {
			identifier.WriteRune(letter)
		}
	}
	out := identifier.String()
	first, _ := utf8.DecodeRuneInString(out)
	if out == "" || unicode.IsDigit(first) || token.IsKeyword(out) {
		out = "app" + out
	}
	return out
}
