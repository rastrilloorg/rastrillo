package pdf

import (
	"sort"
	"strings"
	"unicode"
)

// Faces. The indices are the /F1, /F2, /F3 names in the emitted file.
const (
	faceRegular = 0
	faceBold    = 1
	faceItalic  = 2
)

// widths returns the metric table for a face. An unknown face is drawn
// in the regular one rather than refused: a report that failed to
// render because of a font index would be a worse answer than one set
// in the wrong weight, and the wrong weight is visible.
func widths(face int) *[256]int16 {
	switch face {
	case faceBold:
		return &helveticaBoldWidths
	case faceItalic:
		return &helveticaObliqueWidths
	default:
		return &helveticaWidths
	}
}

// cp1252High is the part of Windows-1252 that is not Latin-1: the
// 0x80-0x9F range, where the encoding carries the typographic
// characters real prose is full of — curly quotes, en and em dashes,
// the ellipsis, the euro sign.
//
// It is written out rather than derived because there is nothing to
// derive it from in the standard library: Go's unicode tables know
// nothing about this legacy encoding, and golang.org/x/text/encoding
// would be a dependency for one table of twenty-seven entries.
var cp1252High = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84,
	'…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88,
	'‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C,
	'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93,
	'”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B,
	'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// fallbacks are characters drawn as a different, but visually close,
// character rather than reported as undrawable: a non-breaking space
// or a soft hyphen has no base-14 glyph of its own, but a plain space
// or an empty string respectively is a closer rendering of it than a
// question mark would be. This is a deliberate, silent substitution —
// unlike an unsupported character, which is drawn as "?" and returned
// by [encode]'s second value, nothing here is ever reported, because
// the whole point is that the reader cannot tell the difference. Treat
// this list as fixed policy, not a place to add a new substitution
// without the same care: a caller measuring or comparing drawn text
// against its own source has to know these runes do not round-trip.
var fallbacks = map[rune]string{
	' ': " ", ' ': " ", ' ': " ", ' ': " ",
	'­': "", '‑': "-", '−': "-",
	'′': "'", '″': "\"",
}

// encode turns a Go string into the Windows-1252 bytes a PDF text
// object can show, and reports every rune it could not represent.
//
// The dropped runes are returned rather than swallowed because a
// document that quietly replaced a character with a question mark is
// the plausible-wrong-answer failure this package exists to avoid: a
// reader cannot see what is not there. The caller states it; this
// function only knows the facts. [fallbacks] is the deliberate
// exception: a substitution close enough in appearance that the
// reader is not expected to be told, and is documented there rather
// than reported here.
func encode(s string) ([]byte, []rune) {
	var out []byte
	var dropped []rune
	seen := map[rune]bool{}
	for _, r := range s {
		if sub, ok := fallbacks[r]; ok {
			out = append(out, sub...)
			continue
		}
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			out = append(out, ' ')
		case r < 0x20:
			// A control character has no glyph in any face. Dropping it
			// silently is right: it was never visible.
		case r < 0x7F:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := cp1252High[r]; ok {
				out = append(out, b)
				continue
			}
			out = append(out, '?')
			if !seen[r] {
				seen[r] = true
				dropped = append(dropped, r)
			}
		}
	}
	sort.Slice(dropped, func(i, j int) bool { return dropped[i] < dropped[j] })
	return out, dropped
}

// escape makes encoded bytes safe inside a PDF literal string. Only
// three characters need it, and a byte above 0x7F is written as an
// octal escape so the content stream stays seven-bit — which keeps the
// file readable in a text editor, a property worth more than the bytes
// it costs for a record somebody may have to inspect by hand in twenty
// years.
func escape(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/8)
	for _, c := range b {
		switch {
		case c == '\\' || c == '(' || c == ')':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 0x20 || c > 0x7E:
			sb.WriteByte('\\')
			sb.WriteByte('0' + (c>>6)&7)
			sb.WriteByte('0' + (c>>3)&7)
			sb.WriteByte('0' + c&7)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// width measures encoded bytes in points at a given size.
func width(b []byte, face int, size float64) float64 {
	w := widths(face)
	var total int
	for _, c := range b {
		total += int(w[c])
	}
	return float64(total) * size / 1000
}

// measure is width for a Go string, which is what every caller has.
func measure(s string, face int, size float64) float64 {
	b, _ := encode(s)
	return width(b, face, size)
}

// wrap breaks a string into lines that fit max points wide.
//
// It breaks at spaces, and hard-breaks a single token too long to fit
// on a line of its own — a case that arises in practice from a long
// reference or a URL, and whose alternative is a line running off the
// page, which is the one outcome a reader cannot recover from. A
// single rune wider than max (an oversized glyph at a very small max)
// still gets a line of its own even though that line does not fit
// either: there is no narrower way to draw it, and dropping it would
// be the plausible-wrong-answer failure this package exists to avoid.
//
// A non-breaking space (U+00A0), figure space (U+2007) or narrow
// no-break space (U+202F) is NOT a break point, even though
// unicode.IsSpace says yes: the only reason anyone types one is to
// keep two words together, and splitting there defeats it. encode
// still draws each as an ordinary space.
func wrap(s string, face int, size, max float64) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', ' ', ' ':
			return false
		}
		return unicode.IsSpace(r)
	})
	if len(fields) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, f := range fields {
		trial := f
		if cur != "" {
			trial = cur + " " + f
		}
		if measure(trial, face, size) <= max {
			cur = trial
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
		// The token alone on a fresh line. If it still does not fit,
		// cut it where it stops fitting — by RUNE, never by byte. A
		// byte cut lands inside a multi-byte character often enough to
		// matter (an em dash in a reference, an accent in a name), and
		// the fragment then ranges as U+FFFD, which encode draws as a
		// question mark on the page.
		//
		// This is one forward pass, measuring each rune's own width
		// once and summing: a token t runes long produces as many
		// lines as it needs, but costs O(t) regardless of how many. An
		// earlier version re-measured the WHOLE remaining prefix on
		// every step of deciding whether to keep cutting, and within
		// that search re-measured every shorter candidate prefix from
		// scratch — quadratic work for one token, and because that
		// search ran again for every line the token produced, cubic
		// overall: a 10,000-character token (a pasted base64 blob or a
		// long reference, reachable from an ordinary Paragraph call)
		// took upwards of twenty seconds. Summing precomputed widths
		// once cannot loop at all, whatever max is — a column too
		// narrow to hold anything degrades to one rune per line rather
		// than hanging the render, with no separate case needed for a
		// non-positive max.
		rs := []rune(f)
		rw := make([]float64, len(rs))
		for i, r := range rs {
			rw[i] = measure(string(r), face, size)
		}
		lineStart := 0
		var w float64
		for i := range rs {
			if i > lineStart && w+rw[i] > max {
				lines = append(lines, string(rs[lineStart:i]))
				lineStart = i
				w = 0
			}
			w += rw[i]
		}
		cur = string(rs[lineStart:])
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}
