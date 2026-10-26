package pdf

import (
	"bytes"
	"regexp"
	"testing"
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
	b.Table("", []Column{{Title: "A"}, {Title: "B"}}, nil, nil)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`/MediaBox \[0 0 841\.89 595\.28\]`)
	if !re.Match(bs) {
		t.Errorf("want a landscape MediaBox (%s) in the emitted PDF", re)
	}
}
