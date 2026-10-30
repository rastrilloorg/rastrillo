package pdf

// The fixed margins every page uses, whatever its Size: a bigger sheet
// (A3, Tabloid) gets more content area at the same margin, not a
// bigger margin, and a smaller one (A5) gets less — the same choice
// Landscape already made for width and height, just extended to a
// size a caller picked rather than one this package turned on its
// side. marginB leaves room for the running footer, which is drawn
// below the flow rather than inside it.
const (
	// maxPageSide is the largest page side PDF readers accept, 200in
	// (ISO 32000-1, Annex C). It also keeps the column arithmetic in
	// layout finite: a Size near the top of float64's range overflowed
	// it to +Inf, which is not a PDF number.
	maxPageSide = 14400.0

	marginL = 56.0
	marginR = 56.0
	marginT = 64.0
	marginB = 76.0
)

// geom is the page geometry one render uses. It is carried on flow and
// passed into emit rather than held in package-level variables,
// because a package-level "current page size" would be shared mutable
// state across two renders running at once, which is exactly the kind
// of bug the package doc's determinism section warns against, just
// reached from a different direction.
type geom struct {
	pageW, pageH                       float64
	marginL, marginR, marginT, marginB float64
	contentW                           float64
	footerY                            float64
}

// newGeom computes a geom for size, portrait unless landscape is true.
// A size that cannot hold a page falls back to A4, turned the same
// way, rather than reaching layout with a negative, NaN or overflowing
// content width. The check runs after landscape turns the page,
// because the top and bottom margins are larger than the side ones. A
// page's body must hold a table header at its maxHeaderLines limit
// with one row under it, the one block that cannot continue onto the
// next page; and no side may pass maxPageSide.
// footerY sits 44pt above the bottom edge.
func newGeom(size Size, landscape bool) geom {
	w, h := size.W, size.H
	if landscape {
		w, h = h, w
	}
	minH := marginT + marginB + headerBandHeight(maxHeaderLines) + leadBody
	if !size.finite() || w <= marginL+marginR || h < minH || w > maxPageSide || h > maxPageSide {
		w, h = A4.W, A4.H
		if landscape {
			w, h = h, w
		}
	}
	return geom{
		pageW: w, pageH: h,
		marginL: marginL, marginR: marginR, marginT: marginT, marginB: marginB,
		contentW: w - marginL - marginR,
		footerY:  h - 44.0,
	}
}

// Type sizes and leadings, in one place so a document has one voice.
// These do not vary with geom: a landscape page is wider, not read in a
// different voice.
const (
	sizeH1    = 13.0
	sizeH2    = 10.5
	sizeBody  = 9.5
	sizeSmall = 8.0

	leadBody  = 12.5
	leadSmall = 10.5
)

// op is one drawing instruction. The flow produces these; emit turns
// them into a content stream. Keeping the two apart is what makes
// pagination possible at all: the total page count is not known until
// the flow has finished, and the footer on page one needs it.
type op struct {
	kind byte // 't' text, 'l' line, 'r' filled rectangle, 'c' stamp text
	x, y float64
	x2   float64
	y2   float64
	face int
	size float64
	text string
	gray float64
}

// flow lays blocks onto pages, starting a new one when the vertical
// space runs out. y is measured DOWN from the top of the page, which is
// how a reader thinks about a document; emit converts to PDF's
// bottom-left origin at the last moment.
type flow struct {
	geom
	pages [][]op
	cur   []op
	y     float64

	// dropped collects every character no base-14 face could draw,
	// across the whole document, so the caller can say so rather than
	// shipping a document with question marks in it and no explanation.
	dropped map[rune]bool
}

func newFlow(g geom) *flow {
	f := &flow{geom: g, dropped: map[rune]bool{}}
	f.newPage()
	return f
}

func (f *flow) newPage() {
	// A page with nothing drawn on it is not a page to leave behind. An
	// explicit break arriving at the top of a fresh sheet — two breaks
	// in a row, or a section that begins with one and happens to fall
	// on the fold — would otherwise emit a blank sheet carrying only
	// the running footer, and a blank sheet in the middle of a
	// document reads as a page that failed to print. The same guard
	// covers need() asking for more room than a whole page holds, where
	// turning over cannot help either.
	if f.cur != nil && len(f.cur) == 0 {
		f.y = f.marginT
		return
	}
	if f.cur != nil {
		f.pages = append(f.pages, f.cur)
	}
	f.cur = []op{}
	f.y = f.marginT
}

// room reports whether h points remain on this page.
func (f *flow) room(h float64) bool { return f.y+h <= f.pageH-f.marginB }

// need starts a new page unless h points remain.
func (f *flow) need(h float64) {
	if !f.room(h) {
		f.newPage()
	}
}

func (f *flow) note(dropped []rune) {
	for _, r := range dropped {
		f.dropped[r] = true
	}
}

// text draws one line at the current y and does NOT advance it. Callers
// that flow prose advance themselves, because a line inside a wrapped
// paragraph and a line inside a table row advance differently.
func (f *flow) text(x float64, face int, size float64, s string, gray float64) {
	_, dropped := encode(s)
	f.note(dropped)
	f.cur = append(f.cur, op{kind: 't', x: x, y: f.y, face: face, size: size, text: s, gray: gray})
}

func (f *flow) rule(x1, x2, y float64, gray float64) {
	f.cur = append(f.cur, op{kind: 'l', x: x1, x2: x2, y: y, gray: gray})
}

func (f *flow) rect(x, y, w, h float64, gray float64) {
	f.cur = append(f.cur, op{kind: 'r', x: x, y: y, x2: x + w, y2: y + h, gray: gray})
}

// para flows wrapped prose from x to x+w and advances past it.
func (f *flow) para(x, w float64, face int, size, lead float64, s string, gray float64) {
	for _, line := range wrap(s, face, size, w) {
		f.need(lead)
		f.y += size
		f.text(x, face, size, line, gray)
		f.y += lead - size
	}
}
