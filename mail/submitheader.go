package mail

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The line lengths of RFC 5322 §2.1.1: lines should fit in 78
// characters and must fit in 998, without the CRLF. SES refuses a raw
// message with a longer line.
const (
	foldWidth     = 78
	maxHeaderLine = 998
	// maxPlainText is the longest header text written as it is. Plain
	// text is never folded, so longer text is encoded, and encoded words
	// can always be.
	maxPlainText = 900
)

// foldPoint marks, in a header this package builds, the one kind of
// place it may fold: after an encoded word, where the folding white
// space is not part of the value. A space anywhere else may be
// meaningful (two spaces in a quoted local part, or in a Subject), and
// unfolding would not give it back. Inputs never hold a control byte,
// so the mark cannot come from a caller.
const foldPoint = "\x01"

// plainText reports whether text can go in a header as it is: ASCII,
// short enough never to need folding, and with no "=?", which a reader
// would try to decode as an encoded word.
func plainText(text string) bool {
	return ascii(text) && len(text) <= maxPlainText &&
		!strings.Contains(text, "=?")
}

// encodedWords is text as RFC 2047 Q-encoded words of at most 75
// characters, separated by fold points, never splitting a character. Only
// letters, digits and !*+-/ stay literal, the set §5(3) allows in a
// phrase, so the words are safe in a Subject and in a display name.
func encodedWords(text string) string {
	const prefix, suffix = "=?utf-8?q?", "?="
	const room = 75 - len(prefix) - len(suffix)
	var words []string
	var word strings.Builder
	for _, char := range text {
		piece := qChar(char)
		if word.Len()+len(piece) > room {
			words = append(words, prefix+word.String()+suffix)
			word.Reset()
		}
		word.WriteString(piece)
	}
	words = append(words, prefix+word.String()+suffix)
	return strings.Join(words, foldPoint)
}

// qChar is one character in Q encoding: a space is "_", the safe ASCII
// set is itself, and everything else is its UTF-8 bytes as =XX.
func qChar(char rune) string {
	switch {
	case char == ' ':
		return "_"
	case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z',
		char >= '0' && char <= '9', strings.ContainsRune("!*+-/", char):
		return string(char)
	}
	var out strings.Builder
	var buffer [utf8.UTFMax]byte
	for _, octet := range buffer[:utf8.EncodeRune(buffer[:], char)] {
		fmt.Fprintf(&out, "=%02X", octet)
	}
	return out.String()
}

// foldHeader turns each fold point into a space, or into a fold where
// the line would otherwise pass foldWidth, and refuses a line still
// longer than maxHeaderLine: plain text, a URL or a message id is never
// folded.
func foldHeader(header []byte) ([]byte, error) {
	lines := strings.Split(string(header), "\r\n")
	for index, line := range lines {
		lines[index] = foldLine(line)
		for _, physical := range strings.Split(lines[index], "\r\n") {
			if len(physical) > maxHeaderLine {
				return nil, fmt.Errorf("rastrillo/mail: header %.40q... "+
					"is %d characters with nowhere to fold it, over the "+
					"%d a mail line may have; shorten it", physical,
					len(physical), maxHeaderLine)
			}
		}
	}
	return []byte(strings.Join(lines, "\r\n")), nil
}

// foldLine is line with its fold points made spaces or folds. The first
// piece holds the field name, so it never folds straight after it.
func foldLine(line string) string {
	pieces := strings.Split(line, foldPoint)
	var out strings.Builder
	out.WriteString(pieces[0])
	width := len(pieces[0])
	for _, piece := range pieces[1:] {
		if width+1+len(piece) > foldWidth {
			out.WriteString("\r\n")
			width = 0
		}
		out.WriteString(" " + piece)
		width += 1 + len(piece)
	}
	return out.String()
}
