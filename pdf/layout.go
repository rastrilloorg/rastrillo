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
	f.y += size
	f.text(f.marginL, faceBold, size, text, 0)
	f.y += lead - size
}

func layoutParagraph(f *flow, text string) {
	f.para(f.marginL, f.contentW, faceRegular, sizeBody, leadBody, text, 0)
	f.y += 5
}

func layoutList(f *flow, title string, items []string) {
	if title != "" {
		f.need(30)
		f.y += 10 + sizeH2
		f.text(f.marginL, faceBold, sizeH2, title, 0)
		f.y += 6
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
	f.need(46)
	start := f.y
	f.y += 8
	if title != "" {
		f.y += sizeH2
		f.text(f.marginL+10, faceBold, sizeH2, title, 0)
		f.y += 5
	}
	face := faceRegular
	if strong {
		face = faceItalic
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
	f.rect(f.marginL, start, weight, f.y-start, 0.2)
	f.y += 10
}

func layoutFields(f *flow, title string, fields []Field) {
	if title != "" {
		f.need(34)
		f.y += 12 + sizeH2
		f.text(f.marginL, faceBold, sizeH2, title, 0)
		f.y += 8
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
	for _, fl := range fields {
		labelLines := wrap(fl.Label, faceRegular, sizeBody, labelW-gap)
		valueLines := wrap(fl.Value, faceRegular, sizeBody, f.contentW-labelW)
		h := float64(max(len(labelLines), len(valueLines))) * leadBody
		f.need(h)
		top := f.y
		for i, l := range labelLines {
			f.y = top + float64(i)*leadBody + sizeBody
			f.text(f.marginL, faceRegular, sizeBody, l, 0.35)
		}
		for i, l := range valueLines {
			f.y = top + float64(i)*leadBody + sizeBody
			f.text(f.marginL+labelW, faceBold, sizeBody, l, 0)
		}
		f.y = top + h
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
func columnWidths(cols []Column, contentW float64) []float64 {
	if len(cols) == 0 {
		return nil
	}
	var stated float64
	var unstated int
	for _, c := range cols {
		if c.Width > 0 {
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
		if w == 0 {
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

func layoutTable(f *flow, title string, columns []Column, rows [][]Cell, total []Cell) {
	if len(columns) == 0 {
		return
	}
	widths := columnWidths(columns, f.contentW)
	ruleRight := tableRuleRight(f, columns, widths)
	if title != "" {
		f.need(40)
		f.y += 12 + sizeH2
		f.text(f.marginL, faceBold, sizeH2, title, 0)
		f.y += 8
	}
	var headerHeight float64
	header := func() {
		f.need(26)
		start := f.y
		f.y += 4
		top := f.y
		var maxLines int
		for i, c := range columns {
			maxLines = max(maxLines, len(wrap(c.Title, faceBold, sizeSmall, widths[i]-8)))
		}
		for i, c := range columns {
			for j, line := range wrap(c.Title, faceBold, sizeSmall, widths[i]-8) {
				f.y = top + float64(j)*leadSmall + sizeSmall
				drawCell(f, widths, i, line, faceBold, sizeSmall, c.Align, 0)
			}
		}
		f.y = top + float64(maxLines)*leadSmall + 4
		f.rule(f.marginL, ruleRight, f.y, 0.4)
		f.y += 6
		headerHeight = f.y - start
	}
	header()
	for _, row := range rows {
		height := leadBody * float64(rowLines(row, columns, widths))
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
		height := leadBody*float64(rowLines(total, columns, widths)) + 10
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

func rowLines(row []Cell, cols []Column, widths []float64) int {
	n := 1
	for i, c := range row {
		if i >= len(cols) {
			break
		}
		n = max(n, len(wrap(c.Text, faceRegular, sizeBody, widths[i]-8)))
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
