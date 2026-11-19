package mcp

import (
	"strconv"
	"unicode/utf8"
)

// The UTF-16 surrogate ranges a \u escape can name. A pair names one
// character; either half alone names none, and a decoder would quietly
// replace it with U+FFFD.
const (
	highSurrogateFirst = 0xD800
	lowSurrogateFirst  = 0xDC00
	lowSurrogateLast   = 0xDFFF
)

// argumentBytesValid reports whether raw is valid UTF-8 whose string
// escapes pair every surrogate. It reads the bytes as sent, before any
// decoder can repair them (spec §6, #116), so an operation never acts
// on text the agent did not send.
func argumentBytesValid(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	inString := false
	for index := 0; index < len(raw); index++ {
		switch {
		case raw[index] == '"':
			inString = !inString
		case inString && raw[index] == '\\':
			end, paired := escapeEnd(raw, index)
			if !paired {
				return false
			}
			// The loop steps past the escape's last byte.
			index = end - 1
		}
	}
	return true
}

// escapeEnd is the index just past the escape at index, and false for a
// surrogate escape without its other half. Any escape but \u is two
// bytes, so an escaped backslash leaves the letters after it as letters.
func escapeEnd(raw []byte, index int) (int, bool) {
	unit, isUnicode := unicodeEscape(raw, index)
	switch {
	case !isUnicode:
		return index + 2, true
	case unit >= lowSurrogateFirst && unit <= lowSurrogateLast:
		return 0, false
	case unit < highSurrogateFirst || unit > lowSurrogateLast:
		return index + 6, true
	}
	low, isUnicode := unicodeEscape(raw, index+6)
	if !isUnicode || low < lowSurrogateFirst || low > lowSurrogateLast {
		return 0, false
	}
	return index + 12, true
}

// unicodeEscape reads the \uXXXX at index, or reports there is none.
func unicodeEscape(raw []byte, index int) (uint64, bool) {
	if index+6 > len(raw) || raw[index] != '\\' || raw[index+1] != 'u' {
		return 0, false
	}
	unit, err := strconv.ParseUint(string(raw[index+2:index+6]), 16, 16)
	return unit, err == nil
}
