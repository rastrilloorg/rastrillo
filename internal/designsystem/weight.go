package designsystem

import (
	"bytes"
	"compress/gzip"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// ── What a file weighs ───────────────────────────────────────────────
//
// The Getting started page gives each shipped file three figures: the
// bytes as written, gzipped, and gzipped with its comments taken out.
// All three are computed here, at render time, from the embedded bytes.
// None is typed into prose or pinned in a test: the spec's ruling on
// the weights table (§6-v2.9) is that compressor output can move
// between Go versions, so a golden "28,998" would churn on a toolchain
// bump while proving nothing. The tests assert the relationships
// instead: gzipped is smaller than raw, and stripped is no bigger than
// gzipped.
//
// Raw alone overstated what a visitor pays by about three times, and
// gzipped alone hid the other finding: comments are most of tokens.css
// even compressed. The third figure is what an app would save by
// stripping them, which is the decision a reader of this page is making.

// gzipLevel is the level every figure is compressed at: 6, gzip's own
// default and the level a server or edge that compresses on the fly
// typically uses. A fixed level, so the figure does not depend on
// whatever DefaultCompression happens to mean in a given Go release.
const gzipLevel = 6

// weightLine is the key for one file's three figures. The stripped
// figure is the last part after " · " in every language, so a file with
// no comments to strip loses that part rather than repeating the gzipped
// figure under a different name. TestTheWeightLineDropsItsLastPartCleanly
// holds every translation to that shape.
const weightLine = "{raw} as written · {gzip} gzipped · {stripped} gzipped without comments"

// weightSep is the separator weightLine's parts are cut at.
const weightSep = " · "

// weight is one file's three figures, in bytes. Stripped is zero when
// the file has no comments, which is how the line knows to drop it.
type weight struct {
	Raw, Gzip, Stripped int
}

// weigh measures one shipped file. Its name's extension picks the
// comment syntax: .css and .js are the two the shipped files use, and
// anything else gets no stripped figure.
func weigh(name string, body []byte) weight {
	w := weight{Raw: len(body), Gzip: gzipped(body)}
	var bare []byte
	switch {
	case strings.HasSuffix(name, ".css"):
		bare = stripCSSComments(body)
	case strings.HasSuffix(name, ".js"):
		bare = stripJSComments(body)
	}
	if bare != nil && !bytes.Equal(bare, body) {
		w.Stripped = gzipped(bare)
	}
	return w
}

// gzipped is the compressed length of b at gzipLevel.
func gzipped(b []byte) int {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzipLevel)
	if err != nil {
		// Only an out-of-range level errors, and gzipLevel is a constant
		// inside the range.
		panic("designsystem: " + err.Error())
	}
	zw.Write(b)
	zw.Close()
	return buf.Len()
}

// kib formats a byte count in KiB (1024, as the spec rules: lowercase
// "kb" is kilobits) in the page's own number format: one decimal below
// 10 KiB, where rounding to a whole number would turn a 634-byte theme
// into "1" or "0", and whole numbers above, where a decimal is noise.
func kib(locale string, n int) string {
	tag, err := language.Parse(locale)
	if err != nil {
		tag = language.English
	}
	p := message.NewPrinter(tag)
	v := float64(n) / 1024
	if n < 10*1024 {
		return p.Sprint(number.Decimal(v, number.MinFractionDigits(1), number.MaxFractionDigits(1)))
	}
	return p.Sprint(number.Decimal(v, number.MaxFractionDigits(0)))
}

// appTotalLine is the key for what a new app is handed in all. Unlike a
// file's line, which sits under a lead that says the unit, this is a
// sentence of its own, so its figures carry it: kibUnit.
const appTotalLine = "A new app gets {raw} of CSS and JavaScript as written, {gzip} gzipped: tokens.css, one theme, and the scripts."

// kibUnit is kib with the unit after it. KiB is written the same in
// every language, as the unit's own symbol.
func kibUnit(locale string, n int) string { return kib(locale, n) + " KiB" }

// weightIn is one file's line in one locale.
func weightIn(locale string, w weight) string {
	line := proseIn(locale, weightLine)
	if w.Stripped == 0 {
		if i := strings.LastIndex(line, weightSep); i >= 0 {
			line = line[:i]
		}
	}
	return interpolate(line, []any{
		"raw", kib(locale, w.Raw), "gzip", kib(locale, w.Gzip), "stripped", kib(locale, w.Stripped),
	})
}

// stripCSSComments removes every /* */ comment outside a string. A
// comment on a line of its own takes the line with it, so the figure is
// what a file with no comments would weigh, not one full of blank lines.
func stripCSSComments(src []byte) []byte {
	var out bytes.Buffer
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			j := endOfQuoted(src, i)
			out.Write(src[i:j])
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = dropComment(&out, src, i, blockCommentEnd(src, i))
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
}

// stripJSComments removes every // and /* */ comment from a script while
// leaving everything that only looks like one alone: a // inside a
// string, a /* inside a regular expression, a comment-shaped run inside
// a template literal and the code inside that literal's ${}. That is
// three lexical states plus the one ambiguity JavaScript is famous for,
// whether a / starts a regular expression or divides; see regexAllowed.
//
// The result is only ever measured, never served, but it is held to
// being a script a browser would still parse, because a figure for a
// file that could not exist is not a figure worth printing.
func stripJSComments(src []byte) []byte {
	var out bytes.Buffer
	// braces is one entry per open { since the innermost template
	// literal's ${: when it reaches zero on a }, that } closes the ${
	// and the scan is back inside the literal.
	var braces []int
	// parens is one entry per open (, true where it opened the head of
	// an if, while, for or with. A / after the ) closing one of those
	// starts the statement it governs, so it is a regex: if (x) /re/.
	// After any other ) it divides: (a + b) / 2. ctlEnd is the length
	// of out just past the last ) that closed such a head.
	var parens []bool
	ctlEnd := -1
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '(':
			parens = append(parens, controlHead(out.Bytes()))
			out.WriteByte(c)
			i++
		case c == ')':
			head := false
			if n := len(parens); n > 0 {
				head, parens = parens[n-1], parens[:n-1]
			}
			out.WriteByte(c)
			i++
			if head {
				ctlEnd = out.Len()
			}
		case c == '"' || c == '\'':
			j := endOfQuoted(src, i)
			out.Write(src[i:j])
			i = j
		case c == '`':
			j, opened := endOfTemplate(src, i+1)
			out.Write(src[i:j])
			i = j
			if opened {
				braces = append(braces, 0)
			}
		case c == '{' && len(braces) > 0:
			braces[len(braces)-1]++
			out.WriteByte(c)
			i++
		case c == '}' && len(braces) > 0 && braces[len(braces)-1] == 0:
			braces = braces[:len(braces)-1]
			j, opened := endOfTemplate(src, i+1)
			out.Write(src[i:j])
			i = j
			if opened {
				braces = append(braces, 0)
			}
		case c == '}' && len(braces) > 0:
			braces[len(braces)-1]--
			out.WriteByte(c)
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			end := i + 2
			for end < len(src) && src[end] != '\n' {
				end++
			}
			i = dropComment(&out, src, i, end)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = dropComment(&out, src, i, blockCommentEnd(src, i))
		case c == '/' && regexAllowed(out.Bytes(), ctlEnd):
			j := endOfRegex(src, i)
			out.Write(src[i:j])
			i = j
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
}

// dropComment skips src[start:end], a comment, and returns where the
// scan resumes. A comment alone on its line takes the line with it. A
// block comment spanning lines inside code leaves a newline behind
// rather than nothing: JavaScript's automatic semicolons read line
// breaks, and a comment that held two statements apart must still. One
// inside a line leaves a space unless a space is already beside it: a
// comment separates tokens, and with nothing in its place a+/**/++b
// would read a++ +b, a different sum that parses just as well.
func dropComment(out *bytes.Buffer, src []byte, start, end int) int {
	b := out.Bytes()
	lineStart := bytes.LastIndexByte(b, '\n') + 1
	aloneBefore := len(bytes.TrimLeft(b[lineStart:], " \t")) == 0
	rest := end
	for rest < len(src) && (src[rest] == ' ' || src[rest] == '\t' || src[rest] == '\r') {
		rest++
	}
	aloneAfter := rest == len(src) || src[rest] == '\n'
	if aloneBefore && aloneAfter {
		out.Truncate(lineStart)
		if rest < len(src) {
			rest++ // the line's own newline
		}
		return rest
	}
	if bytes.IndexByte(src[start:end], '\n') >= 0 {
		out.WriteByte('\n')
	} else if end < len(src) && !isSpaceByte(src[end]) && len(b) > 0 && !isSpaceByte(b[len(b)-1]) {
		out.WriteByte(' ')
	}
	return end
}

// blockCommentEnd is the index just past the */ closing the comment
// that starts at i, or the end of src for one that never closes.
func blockCommentEnd(src []byte, i int) int {
	if j := bytes.Index(src[i+2:], []byte("*/")); j >= 0 {
		return i + 2 + j + 2
	}
	return len(src)
}

// endOfQuoted is the index just past the string literal opening at i,
// honouring backslash escapes.
func endOfQuoted(src []byte, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(src)
}

// endOfTemplate scans template-literal text from i and returns the
// index just past where it stops: past the closing backtick, or past a
// ${, in which case opened is true and code follows.
func endOfTemplate(src []byte, i int) (int, bool) {
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '`':
			return j + 1, false
		case '$':
			if j+1 < len(src) && src[j+1] == '{' {
				return j + 2, true
			}
		}
	}
	return len(src), false
}

// endOfRegex is the index just past the regular-expression literal
// opening at i, flags included. A / inside a [class] does not close it.
func endOfRegex(src []byte, i int) int {
	inClass := false
	for j := i + 1; j < len(src); j++ {
		switch c := src[j]; {
		case c == '\\':
			j++
		case c == '\n':
			return j // not a regex after all; give the line back
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			j++
			for j < len(src) && isWordByte(src[j]) {
				j++
			}
			return j
		}
	}
	return len(src)
}

// regexAllowed says whether a / after the code written so far starts a
// regular expression rather than dividing. The rule is the usual one: a
// regex can follow an operator, an opening bracket, a comma or a
// semicolon, the ) of a control-flow head (ctlEnd, see stripJSComments),
// or one of the keywords that take an expression; a division follows a
// value: a name, a number, any other closing bracket, and x++ or x--.
//
// A } is taken for the end of an object, so a / after it divides. A
// regex opening a statement straight after a block's } would be read
// wrong; no shipped script writes one, and
// TestEveryShippedScriptStripsToTheSameShape would say if one did.
func regexAllowed(before []byte, ctlEnd int) bool {
	b := bytes.TrimRight(before, " \t\r\n")
	if len(b) == 0 {
		return true
	}
	last := b[len(b)-1]
	if last == ')' {
		return len(b) == ctlEnd
	}
	if (last == '+' || last == '-') && len(b) > 1 && b[len(b)-2] == last {
		return false
	}
	if strings.IndexByte("(,=:[!&|?{};+-*%<>~^", last) >= 0 {
		return true
	}
	if !isWordByte(last) {
		return false
	}
	switch lastWord(b) {
	case "return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw", "instanceof", "yield", "await":
		return true
	}
	return false
}

// controlHead says whether a ( written after before opens the head of
// an if, while, for or with, including for await (…), whose head is
// a for's with one more word in front of the parenthesis.
func controlHead(before []byte) bool {
	b := bytes.TrimRight(before, " \t\r\n")
	w := lastWord(b)
	if w == "await" {
		w = lastWord(bytes.TrimRight(b[:len(b)-len(w)], " \t\r\n"))
		return w == "for"
	}
	switch w {
	case "if", "while", "for", "with":
		return true
	}
	return false
}

// lastWord is the identifier or keyword b ends with, if it ends with one.
func lastWord(b []byte) string {
	j := len(b)
	for j > 0 && isWordByte(b[j-1]) {
		j--
	}
	return string(b[j:])
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
