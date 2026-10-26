package pdf

import "strings"

// Column describes one column of a [Builder.Table].
type Column struct {
	Title string
	// Align is "left" or "right". Money and counts are right; prose is
	// left. There is no centre, because nothing this package draws
	// wants one.
	Align string
	// Width is the column's share of the table width, as a proportion
	// of the sum of every column's Width. A zero means "equal share".
	Width float64
}

// Cell is one table cell. It is a struct rather than a string so a cell
// can be emphasised without a markup language leaking into the page.
type Cell struct {
	Text string
	Bold bool
}

// Field is one label and its value, for [Builder.Fields].
type Field struct {
	Label string
	Value string
	// Source names where a value that is not the caller's own came
	// from. Left empty, nothing is printed.
	Source string
}

func layoutHeading(f *flow, level int, text string) {
	size, lead := sizeH1, 17.0
	if level >= 2 {
		size, lead = sizeH2, 14.0
	}
	f.need(lead + 24)
	f.y += 12
	// Wrapped, not drawn as one line regardless of width: a heading is
	// a caller's own words, with no length this package can assume,
	// and a long one drawn as a single text operation ran straight off
	// the right edge of the page rather than onto a line of its own.
	f.para(f.marginL, f.contentW, faceBold, size, lead, text, 0)
}

func layoutParagraph(f *flow, text string) {
	f.para(f.marginL, f.contentW, faceRegular, sizeBody, leadBody, text, 0)
	f.y += 5
}

func layoutList(f *flow, title string, items []string) {
	if title != "" {
		f.need(14 + 16)
		f.y += 10
		f.para(f.marginL, f.contentW, faceBold, sizeH2, 14.0, title, 0)
		f.y += 2
	}
	for _, item := range items {
		f.need(leadBody)
		f.y += sizeBody
		f.text(f.marginL+4, faceRegular, sizeBody, "•", 0)
		f.y -= sizeBody
		f.para(f.marginL+16, f.contentW-16, faceRegular, sizeBody, leadBody, item, 0)
	}
	f.y += 5
}

// layoutCallout draws a boxed statement a reader must not miss. strong
// gets a heavier left bar and an italic body, for whichever of a
// caller's own two levels of emphasis this is.
func layoutCallout(f *flow, title, text string, strong bool) {
	face := faceRegular
	if strong {
		face = faceItalic
	}
	// The whole block is measured before anything is drawn, so this
	// reserves the real height a wrapped title and body need rather
	// than a flat guess: a guess too small let the body run past where
	// it was reserved to stop, and a guess that assumed a title was
	// always one line undercounted a long one.
	need := 8.0 + float64(len(wrap(text, face, sizeBody, f.contentW-20)))*leadBody + 8.0 + 10.0
	if title != "" {
		need += 14.0 * float64(len(wrap(title, faceBold, sizeH2, f.contentW-20)))
	}
	f.need(need)
	startPage := len(f.pages)
	start := f.y
	f.y += 8
	if title != "" {
		f.para(f.marginL+10, f.contentW-20, faceBold, sizeH2, 14.0, title, 0)
	}
	f.para(f.marginL+10, f.contentW-20, face, sizeBody, leadBody, text, 0)
	f.y += 8
	// A left bar rather than a filled box: emphasis told by a colour
	// alone is emphasis a monochrome printer loses, and these documents
	// are printed.
	weight := 1.5
	if strong {
		weight = 3.0
	}
	drawCalloutBar(f, startPage, start, weight)
	f.y += 10
}

// drawCalloutBar draws the callout's left bar. Reserving the real
// height up front (above) keeps a callout on one page for as long as
// one page can hold it at all, but a body still longer than that has
// nowhere else to go, so one rectangle spanning "where it started" to
// "where it ended" is not a shape any single page can draw once those
// two points are on different pages — the bar's own height, measured
// across the break, used to come out negative and run into whatever
// followed, on the wrong page, with no bar at all on the one the
// callout actually started on. One segment per occupied page: the
// first from its own start down to the bottom of the body area, any
// page fully inside the callout top to bottom, and the last from the
// top margin down to wherever the body actually ended.
func drawCalloutBar(f *flow, startPage int, start, weight float64) {
	endPage := len(f.pages)
	if startPage == endPage {
		f.rect(f.marginL, start, weight, f.y-start, 0.2)
		return
	}
	f.pages[startPage] = append(f.pages[startPage],
		op{kind: 'r', x: f.marginL, y: start, x2: f.marginL + weight, y2: f.pageH - f.marginB, gray: 0.2})
	for i := startPage + 1; i < endPage; i++ {
		f.pages[i] = append(f.pages[i],
			op{kind: 'r', x: f.marginL, y: f.marginT, x2: f.marginL + weight, y2: f.pageH - f.marginB, gray: 0.2})
	}
	f.rect(f.marginL, f.marginT, weight, f.y-f.marginT, 0.2)
}

func layoutFields(f *flow, title string, fields []Field) {
	if title != "" {
		f.need(14 + 24)
		f.y += 12
		f.para(f.marginL, f.contentW, faceBold, sizeH2, 14.0, title, 0)
		f.y += 2
	}
	// The value sits just past the widest label actually in this
	// block, not a fixed guessed-at column: a short label such as
	// "Per member" on its own otherwise leaves the figure stranded out
	// past a gap sized for a label several times longer.
	const gap = 16.0
	labelW := 0.0
	for _, fl := range fields {
		if w := measure(fl.Label, faceRegular, sizeBody); w > labelW {
			labelW = w
		}
	}
	labelW += gap
	// Capped at half the content width: a single pathological label
	// (far longer than anyone would actually write one) would otherwise
	// claim the whole row for itself and leave every value with no room
	// at all. A label this long already wraps onto its own further
	// lines below; it does not need the column widened to avoid that.
	if half := f.contentW / 2; labelW > half {
		labelW = half
	}
	for _, fl := range fields {
		labelLines := wrap(fl.Label, faceRegular, sizeBody, labelW-gap)
		// Measured and wrapped in the SAME face it is drawn in: wrapping
		// in regular and drawing in bold let a value that just fit in
		// the narrower regular measurement run past the margin once
		// actually drawn, since bold is wider.
		valueLines := wrap(fl.Value, faceBold, sizeBody, f.contentW-labelW)
		// Drawn band by band, with a room check before each one — a
		// label and a value can wrap to far more lines than fit on any
		// one page (a long note, say), and a single f.need() for the
		// whole field either let the rest draw straight past the
		// bottom margin or, on a short page, moved the entire field
		// rather than letting it continue, the way a table's own rows
		// already do.
		n := max(len(labelLines), len(valueLines))
		for i := 0; i < n; i++ {
			f.need(leadBody)
			f.y += sizeBody
			if i < len(labelLines) {
				f.text(f.marginL, faceRegular, sizeBody, labelLines[i], 0.35)
			}
			if i < len(valueLines) {
				f.text(f.marginL+labelW, faceBold, sizeBody, valueLines[i], 0)
			}
			f.y += leadBody - sizeBody
		}
		if fl.Source != "" {
			f.para(f.marginL+labelW, f.contentW-labelW, faceItalic, sizeSmall, leadSmall,
				"Recorded by "+fl.Source, 0.3)
		}
		f.y += 2
	}
	f.y += 6
}

// columnWidths turns the table's relative widths into points. A column
// with no width stated takes an equal share of what the stated ones
// leave, which is what makes a table of three prose columns render
// sensibly with nothing declared at all.
// maxColumnWidth bounds a single column's own stated Width. It is not
// a realistic weight — no caller states anything near this — it is a
// ceiling against a pathological one: contentW times a Width near the
// top of float64's range overflowed to +Inf, which this package wrote
// into the content stream unchanged, and +Inf is not a legal PDF
// number (ISO 32000-2, clause 7.3.3) — a reader has to decide what an
// illegal file means. Anything above the ceiling is treated as
// unstated instead, the same answer a zero or negative Width already
// gets.
const maxColumnWidth = 1e6

// validColumnWidth reports whether a Width can safely take part in the
// arithmetic below: positive, finite, and not large enough for that
// arithmetic to overflow on its own. NaN fails every ordinary
// comparison, including this one — deliberately, since there is no
// comparison that would make a NaN Width valid.
func validColumnWidth(w float64) bool {
	return w > 0 && w <= maxColumnWidth
}

func columnWidths(cols []Column, contentW float64) []float64 {
	if len(cols) == 0 {
		return nil
	}
	var stated float64
	var unstated int
	for _, c := range cols {
		if validColumnWidth(c.Width) {
			stated += c.Width
		} else {
			unstated++
		}
	}
	out := make([]float64, len(cols))
	if unstated == len(cols) {
		for i := range out {
			out[i] = contentW / float64(len(cols))
		}
		return out
	}
	// Everything is expressed as a share of the total, so an unstated
	// column is given the mean of the stated ones before scaling.
	mean := stated / float64(len(cols)-unstated)
	total := stated + mean*float64(unstated)
	for i, c := range cols {
		w := c.Width
		if !validColumnWidth(w) {
			w = mean
		}
		out[i] = contentW * w / total
	}
	return out
}

// tableRuleRight is the x a table's own rules — under the header, and
// under and double-under its total — should reach: the right edge of
// its last column with a real title, not the page margin. A table that
// pads unused width with a blank trailing column, so a narrow right-
// aligned column is not stretched out to the margin, should not have
// its rules run past the figures and into that blank space; a table
// with no such column, where every column has a title, reaches the
// margin exactly as before.
func tableRuleRight(f *flow, cols []Column, widths []float64) float64 {
	x, right, titled := f.marginL, f.marginL, false
	for i, c := range cols {
		x += widths[i]
		if c.Title != "" {
			right, titled = x, true
		}
	}
	if !titled {
		return f.pageW - f.marginR
	}
	return right
}

// maxHeaderLines bounds how many lines one column's title may wrap to.
// A table's header row — unlike a heading or a paragraph — cannot
// simply run onto a further page: it has to appear whole, above its
// data, on every page it is repeated on, so there is no "paginate it
// normally" answer once a title wraps to more lines than a page can
// hold above even one row. A column title needing more than a
// handful of lines is not a real table header; bounding it here keeps
// a pathological one from pushing both itself and the data below it
// off every page it is drawn on.
const maxHeaderLines = 4

// headerTitleLines wraps one column's title, bounded to maxHeaderLines:
// the lines that fit are kept as they wrapped, and the last allowed
// line carries everything past that point, trimmed with an ellipsis
// the same way a long footer already is.
func headerTitleLines(title string, width float64) []string {
	lines := wrap(title, faceBold, sizeSmall, width)
	if len(lines) <= maxHeaderLines {
		return lines
	}
	out := append([]string{}, lines[:maxHeaderLines-1]...)
	rest := strings.Join(lines[maxHeaderLines-1:], " ")
	return append(out, trim(rest, faceBold, sizeSmall, width))
}

// headerMaxLines is the tallest a table's column titles wrap to, which
// is also what the header band's own height depends on: shared by the
// band-height reservation before the first header() and header()'s own
// drawing, so the two cannot drift apart.
func headerMaxLines(columns []Column, widths []float64) int {
	var maxLines int
	for i, c := range columns {
		maxLines = max(maxLines, len(headerTitleLines(c.Title, widths[i]-8)))
	}
	return maxLines
}

// headerBandHeight is the header band's own height for a given number
// of wrapped title lines — the arithmetic header() below actually does,
// pulled out so a caller can ask for it before header() runs.
func headerBandHeight(maxLines int) float64 {
	return 14 + float64(maxLines)*leadSmall
}

func layoutTable(f *flow, title string, columns []Column, rows [][]Cell, total []Cell) {
	if title != "" {
		f.need(14 + 20)
		f.y += 12
		f.para(f.marginL, f.contentW, faceBold, sizeH2, 14.0, title, 0)
		f.y += 4
	}
	if len(columns) == 0 {
		return
	}
	widths := columnWidths(columns, f.contentW)
	ruleRight := tableRuleRight(f, columns, widths)
	var headerHeight float64
	header := func() {
		maxLines := headerMaxLines(columns, widths)
		f.need(headerBandHeight(maxLines))
		start := f.y
		f.y += 4
		top := f.y
		for i, c := range columns {
			for j, line := range headerTitleLines(c.Title, widths[i]-8) {
				f.y = top + float64(j)*leadSmall + sizeSmall
				drawCell(f, widths, i, line, faceBold, sizeSmall, c.Align, 0)
			}
		}
		f.y = top + float64(maxLines)*leadSmall + 4
		f.rule(f.marginL, ruleRight, f.y, 0.4)
		f.y += 6
		headerHeight = f.y - start
	}
	// The header asks only for its OWN height, which is right once the
	// table is already under way: fresh off a break there is a whole
	// page behind it. But asking for only that the FIRST time let the
	// header land alone at the foot of a page, with its first row
	// pushed onto the next — where the header is then drawn a second
	// time, so the reader sees it twice in a row. Reserving room for
	// the header AND the first row before that first call pushes the
	// whole opening onto a fresh page instead, the same shape as
	// Builder.KeepTogether's own reasoning applied to one table's own
	// head.
	//
	// The first row's REAL height, not one line assumed: a row whose
	// first cell wraps to several lines needs several, and asking for
	// only one left exactly enough room for the header plus a single
	// line to pass this check, while the row itself still did not fit
	// and triggered its own break — moving the header a second time
	// regardless.
	firstRowHeight := leadBody
	switch {
	case len(rows) > 0:
		firstRowHeight = leadBody * float64(rowLines(rows[0], columns, widths, faceRegular))
	case len(total) > 0:
		firstRowHeight = leadBody*float64(rowLines(total, columns, widths, faceBold)) + 10
	}
	f.need(headerBandHeight(headerMaxLines(columns, widths)) + firstRowHeight)
	header()
	for _, row := range rows {
		height := leadBody * float64(rowLines(row, columns, widths, faceRegular))
		if !f.room(height) && height <= f.pageH-f.marginB-f.marginT-headerHeight {
			f.newPage()
			header()
		}
		drawRow(f, columns, widths, row, faceRegular, func() {
			f.newPage()
			header()
		})
	}
	if len(total) > 0 {
		height := leadBody*float64(rowLines(total, columns, widths, faceBold)) + 10
		if !f.room(height) && height <= f.pageH-f.marginB-f.marginT-headerHeight {
			f.newPage()
			header()
		}
		f.y += 2
		f.rule(f.marginL, ruleRight, f.y, 0.4)
		f.y += 6
		drawRow(f, columns, widths, total, faceBold, func() {
			f.newPage()
			header()
		})
		f.y += 2
		f.rule(f.marginL, ruleRight, f.y, 0.6)
	}
	f.y += 12
}

// rowLines measures a row the same way drawRow will actually draw it —
// base for an ordinary cell, bold for one row.Bold marks or for the
// whole row when base itself is bold (the total row) — because a
// pre-check measured in the wrong face under-estimates a bold row's
// true height (bold is wider) and can let drawRow's own per-line
// pagination split a row the pre-check thought would move whole.
func rowLines(row []Cell, cols []Column, widths []float64, base int) int {
	n := 1
	for i, c := range row {
		if i >= len(cols) {
			break
		}
		face := base
		if c.Bold {
			face = faceBold
		}
		n = max(n, len(wrap(c.Text, face, sizeBody, widths[i]-8)))
	}
	return n
}

// drawRow keeps columns aligned while flowing an oversized row across
// pages. A caller's narrative has no page-sized limit. Starting the row
// on a fresh sheet is not enough: each subsequent band must check for
// room, and the table must repeat its header whenever the row continues.
func drawRow(f *flow, cols []Column, widths []float64, row []Cell, base int, nextPage func()) {
	wrapped := make([][]string, min(len(row), len(cols)))
	faces := make([]int, len(wrapped))
	lines := 1
	for i := range wrapped {
		faces[i] = base
		if row[i].Bold {
			faces[i] = faceBold
		}
		wrapped[i] = wrap(row[i].Text, faces[i], sizeBody, widths[i]-8)
		lines = max(lines, len(wrapped[i]))
	}
	for j := 0; j < lines; j++ {
		if !f.room(leadBody) {
			nextPage()
		}
		f.y += sizeBody
		for i, cell := range wrapped {
			if j < len(cell) {
				drawCell(f, widths, i, cell[j], faces[i], sizeBody, cols[i].Align, 0)
			}
		}
		f.y += leadBody - sizeBody
	}
}

// drawCell places one line inside column i, honouring its alignment.
//
// Right alignment is measured rather than approximated, which is the
// whole reason this package carries the font metrics: a column of
// figures whose decimal points do not line up is a column a reader
// cannot add up by eye.
func drawCell(f *flow, widths []float64, i int, line string, face int, size float64, align string, gray float64) {
	x := f.marginL
	for j := 0; j < i; j++ {
		x += widths[j]
	}
	if strings.EqualFold(align, "right") {
		x += widths[i] - 4 - measure(line, face, size)
	} else {
		x += 4
	}
	f.text(x, face, size, line, gray)
}
