package pdf

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Fields drew its value in bold (faceBold) but measured and wrapped it
// in regular — bold is wider, so a value that just fit the narrower
// measurement ran past the margin once actually drawn.
func TestFieldsValueIsMeasuredInTheFaceItIsDrawnIn(t *testing.T) {
	f := newFlow(newGeom(false))
	layoutFields(f, "", []Field{{Label: "Name", Value: strings.Repeat("i", 200)}})
	for _, o := range f.cur {
		if o.kind != 't' {
			continue
		}
		if right := o.x + measure(o.text, o.face, o.size); right > f.pageW-f.marginR+0.5 {
			t.Errorf("op %q ends at x=%.2f, past the right margin %.2f", o.text, right, f.pageW-f.marginR)
		}
	}
}

// A field whose value wraps to more lines than fit on one page used to
// reserve the WHOLE field's height once, then draw every line with no
// further room check — so lines past the bottom margin drew straight
// off the page rather than continuing onto a further one.
func TestFieldsPaginatesAnOversizedValue(t *testing.T) {
	b := New(opts())
	b.Fields("", []Field{{Label: "Note", Value: strings.Repeat("W ", 4000)}})
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) < 2 {
		t.Errorf("want an oversized value to continue onto further pages, got %d", pageCount(bs))
	}
}

// A heading (or a Fields/List/Table/Callout title) used to be drawn as
// one text operation regardless of width, so a long one ran off the
// right edge of the page instead of wrapping onto a line of its own.
func TestHeadingWraps(t *testing.T) {
	f := newFlow(newGeom(false))
	layoutHeading(f, 1, strings.Repeat("W", 100))
	for _, o := range f.cur {
		if o.kind != 't' {
			continue
		}
		if right := o.x + measure(o.text, o.face, o.size); right > f.pageW-f.marginR+0.5 {
			t.Errorf("heading op %q ends at x=%.2f, past the right margin %.2f", o.text, right, f.pageW-f.marginR)
		}
	}
}

// A column title wrapping to many lines used to reserve exactly that
// much room for the header band — unbounded, so a sufficiently long
// title pushed the header itself, and every row after it, past the
// bottom margin with no page able to hold it. maxHeaderLines bounds
// this; the title is truncated with an ellipsis rather than growing
// the band without limit.
func TestTallColumnHeaderDoesNotPushDataOffThePage(t *testing.T) {
	f := newFlow(newGeom(false))
	layoutTable(f, "", []Column{
		{Title: strings.Repeat("H", 100), Width: 1},
		{Title: "B", Width: 99},
	}, [][]Cell{{{Text: "x"}, {Text: "y"}}}, nil)
	f.pages = append(f.pages, f.cur)
	for n, page := range f.pages {
		for _, o := range page {
			if o.kind == 't' && (o.y < f.marginT-0.5 || o.y > f.pageH-f.marginB+0.5) {
				t.Errorf("page %d: op %q at y=%.2f is outside the printable body [%.2f, %.2f]",
					n, o.text, o.y, f.marginT, f.pageH-f.marginB)
			}
		}
	}
}

// columnWidths let a non-finite or negative Width reach the PDF file
// unchanged: a column weight near the top of float64's range overflowed
// contentW*w to +Inf, and a negative one produced a negative column —
// neither is a legal PDF number. Both are now treated the same as an
// unstated (zero) Width: fall back to sharing the mean.
func TestColumnWidthsRejectsOverflowAndNegativeWeights(t *testing.T) {
	cases := []struct {
		name string
		cols []Column
	}{
		{"huge weight", []Column{{Width: 1e308}}},
		{"negative weight", []Column{{Width: 1}, {Width: -1}, {Width: 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			widths := columnWidths(c.cols, 483.28)
			for i, w := range widths {
				if w != w { // NaN
					t.Errorf("column %d is NaN: %v", i, widths)
				}
				if w < 0 {
					t.Errorf("column %d is negative: %v", i, widths)
				}
				if w > 483.28 {
					t.Errorf("column %d (%v) exceeds the whole content width: %v", i, w, widths)
				}
			}
		})
	}
}

// The header+first-row pre-check used to assume the first row costs
// exactly one line, so a first row that itself wraps to several lines
// could still pass the check, fail its own room test immediately
// afterwards, and move the header a second time regardless.
func TestHeaderAndFirstRowReservationAccountsForAWrappedFirstRow(t *testing.T) {
	b := New(opts())
	for i := 0; i < 37; i++ {
		b.Paragraph("x")
	}
	b.Table("", []Column{{Title: "H"}}, [][]Cell{{{Text: strings.Repeat("word ", 100)}}}, nil)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(bs, []byte("(H) Tj")); n != 1 {
		t.Errorf("header drawn %d times, want 1 (orphaned-and-repeated when its first row wraps)", n)
	}
}

// wrap's hard-break used to re-measure the whole remaining prefix on
// every step of deciding where to cut, and repeated that search for
// every line a long token produced — quadratic per token, cubic
// overall. A single forward pass should handle a long pasted token
// (a base64 blob, a reference) in well under a second regardless.
func TestWrapOfALongTokenStaysFast(t *testing.T) {
	s := strings.Repeat("a", 20000)
	done := make(chan struct{})
	go func() {
		wrap(s, faceRegular, sizeBody, 483.28)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wrapping a 20,000-character token did not finish within 2s")
	}
}
