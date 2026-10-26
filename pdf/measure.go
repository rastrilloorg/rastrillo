package pdf

// The measuring half of this package, exported on its own.
//
// An app that draws onto something this package does not lay out itself
// — a blank form somebody else published, a label, a fixed-position
// stamp — still needs to know how wide a word will be before deciding
// whether it fits, and there is exactly one right answer: Adobe's
// published widths for the base-14 faces, which metrics.go already
// carries. A second copy of that table living in the app would be a
// second place for the truth to live, and the first disagreement
// between them would be text running out of a box on a real document.
//
// Nothing here draws anything. The drawing stays private, because the
// argument for this package is that it writes a whole file and parses
// nothing, and an exported drawing surface would be an invitation to
// assemble a page from outside it.

// Face is one of the three base-14 faces this package draws with.
type Face int

const (
	// Regular is Helvetica.
	Regular Face = Face(faceRegular)
	// Bold is Helvetica-Bold.
	Bold Face = Face(faceBold)
	// Italic is Helvetica-Oblique.
	Italic Face = Face(faceItalic)
)

// Encode turns a string into the Windows-1252 bytes a base-14 face can
// draw, and returns the runes it could not. A caller that ships the
// bytes without reading the second return is shipping a silent
// substitution, which is why the two come back together — the same
// contract [Builder.Render] already has.
func Encode(s string) ([]byte, []rune) { return encode(s) }

// Measure is the width of s in points, drawn in f at size.
func Measure(s string, f Face, size float64) float64 {
	return measure(s, int(f), size)
}

// Wrap breaks s into lines no wider than max points, drawn in f at size.
// A single token too long for a line of its own is cut where it stops
// fitting, by rune — so every line it returns really does fit, and a
// caller deciding whether a value fits a printed box counts the lines
// rather than measuring them again.
func Wrap(s string, f Face, size, max float64) []string {
	return wrap(s, int(f), size, max)
}
