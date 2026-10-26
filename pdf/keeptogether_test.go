package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// The exact bug this exists for: a heading with only a little room left
// on the page, immediately followed by a table, used to draw the
// heading as the last line on the page and let the table's own
// pagination start it on the next — orphaning the heading from
// everything that makes it mean something.
func TestKeepTogetherBreaksWhenTheGroupWouldNotFit(t *testing.T) {
	b := New(opts())
	b.Paragraph(strings.Repeat("Front matter filling most of the page. ", 40))
	// However much room addSample-style filler left, it is now within
	// a heading's own height of the bottom — not zero, which proves
	// this is the GROUP that does not fit, not merely the heading.
	b.f.y = b.f.pageH - b.f.marginB - 30

	b.KeepTogether(func(b *Builder) {
		b.Heading(1, "Balance Sheet")
		b.Paragraph("As at 5 April 2026.")
		b.Table("", []Column{{Width: 3.4}, {Align: "right", Width: 1}}, [][]Cell{
			{{Text: "Cash at bank"}, {Text: "100,000.00"}},
			{{Text: "Total", Bold: true}, {Text: "61,250.00", Bold: true}},
		}, nil)
	})

	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 2 {
		t.Fatalf("expected the group to be pushed to a second page, got %d pages", pageCount(bs))
	}
	// Everything in the group reaches the page, and only once.
	for _, want := range []string{"Balance Sheet", "Cash at bank", "100,000.00", "61,250.00"} {
		if n := bytes.Count(bs, []byte("("+want+") Tj")); n != 1 {
			t.Errorf("%q drawn %d times, want 1", want, n)
		}
	}
}

// Plenty of room: KeepTogether must not force a page nobody needed.
func TestKeepTogetherDoesNothingWithRoomToSpare(t *testing.T) {
	b := New(opts())
	b.KeepTogether(func(b *Builder) {
		b.Heading(1, "Balance Sheet")
		b.Paragraph("As at 5 April 2026.")
	})
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 1 {
		t.Fatalf("a short group at the top of a fresh document forced %d pages", pageCount(bs))
	}
}

// fn is handed to KeepTogether once and runs twice internally — once to
// measure, once to draw — so the drawing pass must be the only one that
// reaches the real page. A probe whose ops leaked into the real flow,
// or a real pass run twice, would double every op in the group.
func TestKeepTogetherDrawsItsGroupExactlyOnce(t *testing.T) {
	b := New(opts())
	b.KeepTogether(func(b *Builder) {
		b.Heading(2, "Note: Investments")
		b.Paragraph("A single, distinctive sentence naming itself only once.")
	})
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(bs, []byte("(Note: Investments) Tj")); n != 1 {
		t.Errorf("heading drawn %d times, want 1", n)
	}
	if n := bytes.Count(bs, []byte("distinctive")); n != 1 {
		t.Errorf("paragraph drawn %d times, want 1", n)
	}
	if pageCount(bs) != 1 {
		t.Fatalf("want 1 page, got %d", pageCount(bs))
	}
}

// A group longer than a single page has nowhere it entirely fits, so
// KeepTogether can only ask for a full page's worth of room — and once
// inside, the group's own pagination (a long table's repeating header)
// must carry on exactly as it would with no KeepTogether at all.
func TestKeepTogetherOfAGroupLongerThanOnePageStillPaginatesNormally(t *testing.T) {
	words := make([]string, 850)
	for i := range words {
		words[i] = fmt.Sprintf("word%04d", i)
	}
	b := New(opts())
	b.KeepTogether(func(b *Builder) {
		b.Heading(1, "Cash Flow Statement")
		b.Table("", []Column{{Title: "Narrative", Width: 2}, {Title: "Amount", Width: 1}},
			[][]Cell{{{Text: strings.Join(words, " ")}, {Text: "123.45"}}}, nil)
	})
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	pages := pageCount(bs)
	if pages < 3 {
		t.Fatalf("expected the oversized table to span several pages, got %d", pages)
	}
	if n := bytes.Count(bs, []byte("(Cash Flow Statement) Tj")); n != 1 {
		t.Errorf("heading drawn %d times, want 1", n)
	}
	if n := bytes.Count(bs, []byte("(Narrative) Tj")); n < pages-1 {
		t.Errorf("column heading repeated %d times across %d pages; expected roughly one per page", n, pages)
	}
	if n := bytes.Count(bs, []byte("word0849")); n != 1 {
		t.Errorf("last source word drawn %d times, want 1", n)
	}
}
