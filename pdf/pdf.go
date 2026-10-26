package pdf

import (
	"fmt"
	"strings"
)

// Builder flows blocks onto A4 pages, one call per block, in the order
// they are called. There is no tree to build first and nothing to
// unmarshal: call what the document needs, in order, and call [Render]
// once at the end.
type Builder struct {
	f   *flow
	opt Options
}

// New starts a document. opt's clock and title apply to the whole
// document; nothing drawn by a later call can change them.
func New(opt Options) *Builder {
	return &Builder{f: newFlow(), opt: opt}
}

// Heading starts a section. level is 1 or 2; nothing here needs a third
// and a document that thinks it does is really two documents.
func (b *Builder) Heading(level int, text string) { layoutHeading(b.f, level, text) }

// Paragraph flows running prose.
func (b *Builder) Paragraph(text string) { layoutParagraph(b.f, text) }

// Fields draws a titled list of label/value pairs — the shape most of a
// document's front matter takes. title is omitted when empty.
func (b *Builder) Fields(title string, fields []Field) { layoutFields(b.f, title, fields) }

// Table draws a table with a header row and an optional totals row.
// total is rendered apart from the body, because a total that is just
// another row is a total a reader can misread as a line item. A row (or
// the total) wider than fits the page continues onto further pages,
// with the column headings repeated at the top of each.
func (b *Builder) Table(title string, columns []Column, rows [][]Cell, total []Cell) {
	layoutTable(b.f, title, columns, rows, total)
}

// List draws a bulleted list. title is omitted when empty.
func (b *Builder) List(title string, items []string) { layoutList(b.f, title, items) }

// Callout boxes a statement the reader must not miss. strong draws it
// with a heavier bar and an italic body, for whichever of a caller's
// own two levels of emphasis this is.
func (b *Builder) Callout(title, text string, strong bool) {
	layoutCallout(b.f, title, text, strong)
}

// PageBreak starts a new page. Two in a row, or one that lands at the
// top of a fresh sheet anyway, draws no blank page: see newPage.
func (b *Builder) PageBreak() { b.f.newPage() }

// Render finishes the document and returns its bytes, together with
// every character none of the three faces could draw. A caller that
// ships the bytes without checking the second return is shipping a
// silent substitution.
//
// Call it once, after every block has been added.
func (b *Builder) Render() ([]byte, []rune, error) {
	b.f.pages = append(b.f.pages, b.f.cur)
	b.f.cur = nil

	footers(b.f, b.opt)
	stamps(b.f, b.opt.Stamp)

	var dropped []rune
	for r := range b.f.dropped {
		dropped = append(dropped, r)
	}
	sortRunes(dropped)

	bs, err := emit(b.f.pages, b.opt)
	return bs, dropped, err
}

func sortRunes(rs []rune) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j] < rs[j-1]; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// footers draws the running footer on every page, once the total is
// known. "Page 3 of 11" is the whole reason the flow and the emit are
// separate steps: a reader holding a loose sheet has to be able to tell
// that something is missing.
func footers(f *flow, opt Options) {
	total := len(f.pages)
	left := opt.Title
	if opt.Reference != "" {
		left = opt.Title + " — " + opt.Reference
	}
	if opt.NoRunningTitle {
		left = ""
	}
	for i := range f.pages {
		right := fmt.Sprintf("Page %d of %d", i+1, total)
		room := contentW - measure(right, faceRegular, sizeSmall) - 18
		shown := trim(left, faceRegular, sizeSmall, room)
		// The footer's ops are appended directly rather than through
		// f.text, so what it cannot draw is noted here: a character the
		// typeface lacks is a question mark on every page, and that has
		// to reach the dropped set like any other.
		_, dropped := encode(shown)
		f.note(dropped)
		f.pages[i] = append(f.pages[i],
			op{kind: 'l', x: marginL, x2: pageW - marginR, y: footerY - 12, gray: 0.75},
			op{kind: 't', x: marginL, y: footerY, face: faceRegular, size: sizeSmall,
				text: shown, gray: 0.4},
			op{kind: 't', x: pageW - marginR - measure(right, faceRegular, sizeSmall), y: footerY,
				face: faceRegular, size: sizeSmall, text: right, gray: 0.4},
		)
	}
}

// stamps lays an approval stamp over every page, right-aligned in the
// top margin above the body, where it covers nothing the document says.
func stamps(f *flow, s *Stamp) {
	if s == nil {
		return
	}
	const headSize, lineSize = 16.0, 8.0
	for i := range f.pages {
		if s.Head != "" {
			_, dropped := encode(s.Head)
			f.note(dropped)
			f.pages[i] = append(f.pages[i], op{kind: 'c', x: pageW - marginR - measure(s.Head, faceBold, headSize),
				y: 34, face: faceBold, size: headSize, text: s.Head})
		}
		if s.Line != "" {
			_, dropped := encode(s.Line)
			f.note(dropped)
			line := trim(s.Line, faceRegular, lineSize, contentW)
			f.pages[i] = append(f.pages[i], op{kind: 'c', x: pageW - marginR - measure(line, faceRegular, lineSize),
				y: 46, face: faceRegular, size: lineSize, text: line})
		}
	}
}

// trim keeps a footer's left-hand side from colliding with the page
// number on a document with a long title.
func trim(s string, face int, size, max float64) string {
	if measure(s, face, size) <= max {
		return s
	}
	// By rune, not by byte: the footer's left text is the title, an em
	// dash and the reference, so a byte cut can land in the middle of
	// the dash and draw a question mark on every page.
	rs := []rune(s)
	for len(rs) > 1 {
		rs = rs[:len(rs)-1]
		if measure(string(rs)+"…", face, size) <= max {
			return strings.TrimSpace(string(rs)) + "…"
		}
	}
	return string(rs)
}
