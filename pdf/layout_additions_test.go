package pdf

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A short label such as "Per member" left the figure stranded out past
// a gap sized for a label several times longer, because the value
// column sat at a fixed offset rather than one measured from the
// label actually on the page.
func TestLayoutFieldsPlacesValueCloseToItsLabel(t *testing.T) {
	f := newFlow(newGeom(false))
	layoutFields(f, "", []Field{{Label: "Per member", Value: "£5,161.87"}})

	var labelX, valueX float64
	var foundLabel, foundValue bool
	for _, o := range f.cur {
		if o.kind != 't' {
			continue
		}
		switch o.text {
		case "Per member":
			labelX, foundLabel = o.x, true
		case "£5,161.87":
			valueX, foundValue = o.x, true
		}
	}
	if !foundLabel || !foundValue {
		t.Fatalf("want both the label and the value drawn, got ops %+v", f.cur)
	}
	gap := valueX - (labelX + measure("Per member", faceRegular, sizeBody))
	if gap < 0 || gap > 20 {
		t.Errorf("want the value within ~16pt of the label's end, got a gap of %.1fpt (label at %.1f, value at %.1f)", gap, labelX, valueX)
	}
}

// The "Produced by" line belongs in the last page's footer, not as a
// block of its own after the body: laid out as an ordinary block it
// could tip a document that just filled its last page into a trailing
// page holding nothing else.
func TestProducedGoesInTheLastPageFooterNotATrailingPage(t *testing.T) {
	b := New(opts())
	b.Paragraph("One short paragraph.")
	b.Produced("a member", "02 Oct 2026, 11:14", "")

	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 1 {
		t.Fatalf("want the body and the Produced line on one page, got %d", pageCount(bs))
	}
	if !bytes.Contains(bs, []byte("(Produced by a member on 02 Oct 2026, 11:14.) Tj")) {
		t.Error("want the Produced line drawn on the page")
	}
}

// A table that pads unused width with a blank trailing column — Amount
// pulled in from the margin, the way a ledger with a spacer column
// does — must not have its rules run past the real figures and into
// that blank space. A table with no such column, where every column
// has a title, still rules edge to edge, the same as before.
func TestTableRulesStopAtTheLastTitledColumn(t *testing.T) {
	rows := [][]Cell{{{Text: "A line"}, {Text: "£1.00"}}}
	total := []Cell{{Text: "Total"}, {Text: "£1.00"}}
	rules := func(f *flow) (x2s []float64) {
		for _, o := range f.cur {
			if o.kind == 'l' {
				x2s = append(x2s, o.x2)
			}
		}
		return x2s
	}

	f := newFlow(newGeom(false))
	cols := []Column{{Title: "Line", Width: 200}, {Title: "Amount", Align: "right", Width: 90}, {Width: 200}}
	widths := columnWidths(cols, f.contentW)
	wantRight := f.marginL + widths[0] + widths[1]
	layoutTable(f, "", cols, rows, total)
	if got := rules(f); len(got) != 3 {
		t.Fatalf("want 3 rules (header, total, double-total), got %d: %+v", len(got), got)
	}
	for _, x2 := range rules(f) {
		if x2 != wantRight {
			t.Errorf("rule to x2=%.1f, want the Amount column's own right edge at %.1f (not the margin)", x2, wantRight)
		}
	}

	f2 := newFlow(newGeom(false))
	layoutTable(f2, "", []Column{{Title: "Line", Width: 200}, {Title: "Amount", Align: "right", Width: 90}}, rows, total)
	wantFullWidth := f2.pageW - f2.marginR
	for _, x2 := range rules(f2) {
		if x2 != wantFullWidth {
			t.Errorf("rule to x2=%.1f, want the full margin at %.1f when every column has a title", x2, wantFullWidth)
		}
	}
}

// Landscape swaps width and height and nothing else: the same document
// laid out landscape has a wider page (and so fewer pages, for a table
// of many columns) but the same margins and type.
func TestLandscapeSwapsPageDimensions(t *testing.T) {
	portrait := newGeom(false)
	landscape := newGeom(true)
	if landscape.pageW != portrait.pageH || landscape.pageH != portrait.pageW {
		t.Fatalf("want width and height swapped, got portrait %gx%g landscape %gx%g",
			portrait.pageW, portrait.pageH, landscape.pageW, landscape.pageH)
	}
	if landscape.marginL != portrait.marginL || landscape.marginT != portrait.marginT {
		t.Errorf("want margins unchanged by orientation")
	}

	b := New(Options{Landscape: true})
	b.Table("", []Column{{Title: "A"}, {Title: "B", Align: "right"}}, [][]Cell{{{Text: "x"}, {Text: "1,234.00"}}}, nil)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`/MediaBox \[0 0 841\.89 595\.28\]`)
	if !re.Match(bs) {
		t.Errorf("want a landscape MediaBox (%s) in the emitted PDF", re)
	}
	// The test this layout path could actually fail: a right-aligned
	// cell positioned from a portrait-sized page's right edge, rather
	// than the landscape one, would still produce a landscape MediaBox
	// above and be wrong regardless.
	cellRe := regexp.MustCompile(`BT /F\d [\d.]+ Tf ([\d.]+) [\d.]+ Td \(1,234\.00\) Tj`)
	m := cellRe.FindSubmatch(bs)
	if m == nil {
		t.Fatalf("money cell not found in %s", bs)
	}
	x, _ := strconv.ParseFloat(string(m[1]), 64)
	right := x + measure("1,234.00", faceRegular, sizeBody)
	if want := landscape.pageW - landscape.marginR - 4; right < want-0.5 || right > want+0.5 {
		t.Errorf("cell ends at %.2f, want the LANDSCAPE right edge %.2f (portrait's own would be %.2f)",
			right, want, portrait.pageW-portrait.marginR-4)
	}
}

// A table's own first header() call used to reserve only its own
// height, so a header could land alone at the foot of a page with its
// first row pushed onto the next — where the header is drawn again,
// so the reader sees the same column titles twice in a row.
func TestTableHeaderDoesNotLandAloneWithItsFirstRowOrphaned(t *testing.T) {
	b := New(opts())
	b.Paragraph("Front matter.")
	// Leave exactly 30pt: enough for a one-line header (24.5pt) but not
	// enough for the header plus a first row (24.5+12.5=37).
	b.f.y = b.f.pageH - b.f.marginB - 30
	b.Table("", []Column{{Title: "Narrative"}, {Title: "Amount", Align: "right"}},
		[][]Cell{{{Text: "Row one"}, {Text: "1.00"}}, {{Text: "Row two"}, {Text: "2.00"}}}, nil)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(bs, []byte("(Narrative) Tj")); n != 1 {
		t.Errorf("column heading drawn %d times, want 1 (no orphan-and-repeat)", n)
	}
	pages := pageContents(t, bs)
	if len(pages) != 2 {
		t.Fatalf("want 2 pages (the whole header+row pushed to the second), got %d", len(pages))
	}
	if bytes.Contains(pages[0], []byte("(Narrative) Tj")) {
		t.Error("want the header on page 2 with its first row, not alone on page 1")
	}
}

// A table's own header band, for a wrapped (multi-line) column title,
// used to be reserved at a flat 26pt regardless of how many lines the
// title actually wrapped to — wrong for anything but one line, and the
// kind of mismatch that invites exactly the orphan above for a
// two-line title even where the one-line budget looked generous.
func TestTableHeaderBandAccountsForWrappedTitles(t *testing.T) {
	b := New(opts())
	b.Table("", []Column{{Title: "A column heading long enough to wrap onto two lines", Width: 1}},
		[][]Cell{{{Text: "x"}}}, nil)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(bs, []byte(" Tj")); n < 3 {
		t.Fatalf("want the wrapped title (2+ lines) and the body cell drawn, got %d text ops", n)
	}
}

// A callout's left bar spans from where it started to where its body
// ended. If the body broke onto a second page, "where it started" was
// a y on the OLD page and "where it ended" a y on the new one — the
// bar's own height (f.y-start) came out negative, and the rectangle
// ran off the callout entirely rather than marking it on either page.
// Reserving the real height up front (rather than a flat guess) is
// what keeps the callout on one page for as long as one page can hold
// it at all.
func TestCalloutBarIsNotNegativeHeightWhenBodyWouldHaveBrokenAPage(t *testing.T) {
	b := New(opts())
	b.Paragraph(strings.Repeat("Filler to approach the bottom of the page. ", 34))
	b.Callout("Note", strings.Repeat("A callout body long enough that a flat height guess would have let it break across a page. ", 6), false)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`([\d.]+) [\d.]+ ([\d.]+) [\d.]+ re f`)
	for _, m := range re.FindAllSubmatch(bs, -1) {
		w, _ := strconv.ParseFloat(string(m[1]), 64)
		h, _ := strconv.ParseFloat(string(m[2]), 64)
		if w < 10 && (h < 0 || h > b.f.pageH) {
			t.Errorf("callout bar rectangle has an implausible height %.2f (width %.2f)", h, w)
		}
	}
}

// A table drawn with no columns at all has nothing to show, but its
// title is still real text the caller asked for: silently dropping it
// along with the (legitimately empty) body would look like a caller
// mistake rather than the documented "nothing to show" case.
func TestLayoutTableStillDrawsItsTitleWithNoColumns(t *testing.T) {
	f := newFlow(newGeom(false))
	layoutTable(f, "Receipts", nil, nil, nil)
	var found bool
	for _, o := range f.cur {
		if o.kind == 't' && o.text == "Receipts" {
			found = true
		}
	}
	if !found {
		t.Error("want the title drawn even though there are no columns")
	}
}

// Both reachable without calling Wrap directly: a label wider than the
// room left for its value, and a column requested narrower than its
// own padding, used to hang Render outright (see
// TestWrapTerminatesForNonPositiveWidth for the mechanism). Both must
// now simply render something, promptly.
func TestLayoutFieldsOfAPathologicallyLongLabelTerminates(t *testing.T) {
	done := make(chan struct{})
	go func() {
		b := New(opts())
		b.Fields("", []Field{{Label: strings.Repeat("a", 120), Value: "v"}})
		_, _, _ = b.Render()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not return within 2s for a pathologically long label")
	}
}

func TestLayoutTableOfPathologicallyNarrowColumnsTerminates(t *testing.T) {
	done := make(chan struct{})
	go func() {
		b := New(opts())
		cols := make([]Column, 80)
		for i := range cols {
			cols[i] = Column{Width: 1}
		}
		b.Table("", cols, [][]Cell{{{Text: "x"}}}, nil)
		_, _, _ = b.Render()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not return within 2s for 80 equal (so sub-8pt) columns")
	}
}
