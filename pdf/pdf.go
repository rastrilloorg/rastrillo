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
	f        *flow
	opt      Options
	produced *producedLine
}

// New starts a document. opt's clock, title and page orientation apply
// to the whole document; nothing drawn by a later call can change them.
func New(opt Options) *Builder {
	return &Builder{f: newFlow(newGeom(opt.Landscape)), opt: opt}
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
//
// A column whose Title is "" is blank padding rather than a real one —
// a way to keep a narrow right-aligned figure from being stretched out
// to the margin — and the table's own rules (under its header, and
// under and double-under its total) stop at the last column that does
// have a title, not at the page margin, so they do not run on into
// that blank space. A table with no such column is unaffected: its
// rules reach the margin exactly as before.
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

// KeepTogether lays out fn's calls so they start on the same page: if
// they would not fit in the room left on the current page, a page
// break happens first — before any of fn's own output is drawn — so a
// heading never lands as the last line on a page with its table
// orphaned onto the next.
//
// How much room "would not fit" means is measured, not guessed: fn
// runs once, first, against a throwaway Builder that starts at the top
// of a blank page, and the height that costs is the room this call
// then asks the real page for. A fn whose content is longer than one
// page costs exactly one page's worth — there is no page anywhere it
// would entirely fit, so the only useful question left is whether the
// current page has a full page's worth of room, same as if it did not
// fit. Either way, fn's own pagination is untouched: a long table
// inside it still breaks across further pages with its header
// repeated, exactly as it would outside KeepTogether.
//
// fn is called twice — once to measure, once for real — so it must be
// a pure function of the Builder it is handed: no state outside that
// parameter, and no side effect that should not happen twice.
func (b *Builder) KeepTogether(fn func(b *Builder)) {
	probe := &Builder{f: newFlow(b.f.geom), opt: b.opt}
	fn(probe)
	need := probe.f.y - probe.f.marginT
	if len(probe.f.pages) > 0 {
		// fn overflowed onto a further page even starting fresh: there
		// is no page it fits on, so the only thing left to ask for is a
		// full page's worth of room, which is what "does not fit here
		// either" comes down to.
		need = probe.f.pageH - probe.f.marginT - probe.f.marginB
	}
	b.f.need(need)
	fn(b)
}

// produced holds what [Builder.Produced] was given.
type producedLine struct{ by, at, note string }

// Produced records who produced this document and when — "Produced by
// A Practitioner on 24 August 2026.", with note (left empty for none)
// drawn beneath it in bold, such as "Draft — not issued". It is drawn
// into the LAST page's existing footer area at [Render] time, rather
// than flowed as an ordinary block, so it can never end up alone at
// the top of a trailing page that holds nothing else — the same
// failure KeepTogether exists to stop a heading from having, reached
// from the tail of the document instead of partway through it.
//
// Call it at most once; a later call replaces the line rather than
// adding a second one.
func (b *Builder) Produced(by, at, note string) {
	b.produced = &producedLine{by: by, at: at, note: note}
}

// Render finishes the document and returns its bytes, together with
// every character none of the three faces could draw. A caller that
// ships the bytes without checking the second return is shipping a
// silent substitution.
//
// Call it once, after every block has been added.
func (b *Builder) Render() ([]byte, []rune, error) {
	b.f.pages = append(b.f.pages, b.f.cur)
	b.f.cur = nil

	footers(b.f, b.opt, b.produced)
	stamps(b.f, b.opt.Stamp)

	var dropped []rune
	for r := range b.f.dropped {
		dropped = append(dropped, r)
	}
	sortRunes(dropped)

	bs, err := emit(b.f.pages, b.opt, b.f.geom)
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
func footers(f *flow, opt Options, p *producedLine) {
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
		room := f.contentW - measure(right, faceRegular, sizeSmall) - 18
		shown := trim(left, faceRegular, sizeSmall, room)
		// The footer's ops are appended directly rather than through
		// f.text, so what it cannot draw is noted here: a character the
		// typeface lacks is a question mark on every page, and that has
		// to reach the dropped set like any other.
		_, dropped := encode(shown)
		f.note(dropped)
		f.pages[i] = append(f.pages[i],
			op{kind: 'l', x: f.marginL, x2: f.pageW - f.marginR, y: f.footerY - 12, gray: 0.75},
			op{kind: 't', x: f.marginL, y: f.footerY, face: faceRegular, size: sizeSmall,
				text: shown, gray: 0.4},
			op{kind: 't', x: f.pageW - f.marginR - measure(right, faceRegular, sizeSmall), y: f.footerY,
				face: faceRegular, size: sizeSmall, text: right, gray: 0.4},
		)
		if i == total-1 && p != nil {
			f.pages[i] = append(f.pages[i], producedOps(f, *p)...)
		}
	}
}

// producedOps is the "Produced by" line, and its optional note, drawn
// below the running footer line itself: there is room for both within
// the page's existing bottom margin without widening it.
func producedOps(f *flow, p producedLine) []op {
	y := f.footerY + leadSmall
	line := fmt.Sprintf("Produced by %s on %s.", p.by, p.at)
	_, dropped := encode(line)
	f.note(dropped)
	ops := []op{{kind: 't', x: f.marginL, y: y, face: faceRegular, size: sizeSmall, text: line, gray: 0.4}}
	if p.note != "" {
		_, dropped := encode(p.note)
		f.note(dropped)
		ops = append(ops, op{kind: 't', x: f.marginL, y: y + leadSmall, face: faceBold, size: sizeSmall,
			text: p.note, gray: 0})
	}
	return ops
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
			f.pages[i] = append(f.pages[i], op{kind: 'c', x: f.pageW - f.marginR - measure(s.Head, faceBold, headSize),
				y: 34, face: faceBold, size: headSize, text: s.Head})
		}
		if s.Line != "" {
			_, dropped := encode(s.Line)
			f.note(dropped)
			line := trim(s.Line, faceRegular, lineSize, f.contentW)
			f.pages[i] = append(f.pages[i], op{kind: 'c', x: f.pageW - f.marginR - measure(line, faceRegular, lineSize),
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
