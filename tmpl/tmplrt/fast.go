package tmplrt

import (
	"bytes"
	"encoding/json"
	"strings"
)

// The functions below are for a value whose static Go type is exactly
// string. The generic escapers have to look at what they were given
// (pointers, Stringers, content types) before they can escape it; for a
// plain string none of that applies, so these skip it. Each returns what
// the generic port returns for the same string, and returns the string
// itself, without allocating, when it needs no escaping. A fuzz test
// holds them to that against html/template.

// HTMLEscaperString is HTMLEscaper for a string.
func HTMLEscaperString(text string) string {
	special := firstHTMLSpecial(text)
	if special < 0 {
		return text
	}
	var escaped strings.Builder
	escaped.Grow(len(text) + 16)
	escaped.WriteString(text[:special])
	writeHTMLSpecials(&escaped, text[special:])
	return escaped.String()
}

// AttrEscaperString is AttrEscaper for a string.
func AttrEscaperString(text string) string {
	return HTMLEscaperString(text)
}

// RCDATAEscaperString is RCDATAEscaper for a string.
func RCDATAEscaperString(text string) string {
	return HTMLEscaperString(text)
}

// WriteHTMLEscaped writes HTMLEscaperString(text) to buffer.
func WriteHTMLEscaped(buffer *bytes.Buffer, text string) {
	for {
		special := firstHTMLSpecial(text)
		if special < 0 {
			buffer.WriteString(text)
			return
		}
		buffer.WriteString(text[:special])
		buffer.WriteString(htmlReplacementTable[text[special]])
		text = text[special+1:]
	}
}

// WriteAttrEscaped writes AttrEscaperString(text) to buffer.
func WriteAttrEscaped(buffer *bytes.Buffer, text string) {
	WriteHTMLEscaped(buffer, text)
}

// firstHTMLSpecial returns the index of the first byte of text that
// htmlReplacementTable replaces, or -1. The table only has ASCII
// entries, and the badRunes mode that HTMLEscaper uses leaves every other
// rune alone, so a byte scan is exact.
func firstHTMLSpecial(text string) int {
	for index := 0; index < len(text); index++ {
		if isHTMLSpecial(text[index]) {
			return index
		}
	}
	return -1
}

// isHTMLSpecial reports whether htmlReplacementTable replaces the byte.
func isHTMLSpecial(char byte) bool {
	return int(char) < len(htmlReplacementTable) &&
		htmlReplacementTable[char] != ""
}

// writeHTMLSpecials writes text, which starts with a special byte, with
// every special byte replaced.
func writeHTMLSpecials(builder *strings.Builder, text string) {
	written := 0
	for index := 0; index < len(text); index++ {
		if isHTMLSpecial(text[index]) {
			builder.WriteString(text[written:index])
			builder.WriteString(htmlReplacementTable[text[index]])
			written = index + 1
		}
	}
	builder.WriteString(text[written:])
}

// URLFilterString is URLFilter for a string.
func URLFilterString(text string) string {
	if !isSafeURL(text) {
		return "#" + filterFailsafe
	}
	return text
}

// URLNormalizerString is URLNormalizer for a string.
func URLNormalizerString(text string) string {
	return urlProcessString(text, true)
}

// URLEscaperString is URLEscaper for a string.
func URLEscaperString(text string) string {
	return urlProcessString(text, false)
}

// urlProcessString is urlProcessor for a string. It looks for the first
// byte processURLOnto would encode, because processURLOnto allocates
// before it looks.
func urlProcessString(text string, norm bool) string {
	if !urlNeedsEncoding(text, norm) {
		return text
	}
	var builder strings.Builder
	processURLOnto(text, norm, &builder)
	return builder.String()
}

// urlNeedsEncoding reports whether processURLOnto would encode any byte
// of text, asking urlByteEncoded, the same classification it uses.
func urlNeedsEncoding(text string, norm bool) bool {
	for index := 0; index < len(text); index++ {
		if urlByteEncoded(text, index, norm) {
			return true
		}
	}
	return false
}

// JSValEscaperString is JSValEscaper for a string. A string is always
// marshalled as a JSON string, which starts and ends with a quote, so
// the padding and U+2028 handling JSValEscaper does afterwards never
// applies. The result is a new string, so this allocates.
func JSValEscaperString(text string) string {
	marshalled, err := json.Marshal(text)
	if err != nil {
		// Marshalling a string cannot fail; it replaces invalid UTF-8.
		return JSValEscaper(text)
	}
	return string(marshalled)
}

// JSStrEscaperString is JSStrEscaper for a string.
func JSStrEscaperString(text string) string {
	return replace(text, jsStrReplacementTable)
}
