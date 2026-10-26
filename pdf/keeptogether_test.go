package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// addBalanceSheet draws the heading, period line and table KeepTogether
// is meant to keep on one page together.
func addBalanceSheet(b *Builder) {
	b.Heading(1, "Balance Sheet")
	b.Paragraph("As at 5 April 2026.")
	b.Table("", []Column{{Width: 3.4}, {Align: "right", Width: 1}}, [][]Cell{
		{{Text: "Cash at bank"}, {Text: "100,000.00"}},
		{{Text: "Total", Bold: true}, {Text: "61,250.00", Bold: true}},
	}, nil)
}

// The exact bug this exists for: a heading with only a little room left
// on the page, immediately followed by a table, used to draw the
// heading as the last line on the page and let the table's own
// pagination start it on the next — orphaning the heading from
// everything that makes it mean something.
//
// 60pt is chosen, not guessed: a heading alone needs 41pt (asserted by
// the baseline below), so 60pt is enough for the heading by itself but
// not for the heading plus what it is grouped with — which is exactly
// the band this bug lived in. A room value too small would have the
// heading break on its OWN account regardless of KeepTogether, proving
// nothing about the GROUP (the mistake the room value of 30 made here
// before this test was reviewed).
func TestKeepTogetherBreaksWhenTheGroupWouldNotFit(t *testing.T) {
	const room = 60

	// Front matter first, and real enough to leave ops on the page: a
	// break with nothing already on the current page to leave behind
	// is not a break anybody could observe, and would pass either way.
	const front = "Front matter."

	baseline := New(opts())
	baseline.Paragraph(front)
	baseline.f.y = baseline.f.pageH - baseline.f.marginB - room
	addBalanceSheet(baseline)
	base, _, err := baseline.Render()
	if err != nil {
		t.Fatal(err)
	}
	basePages := pageContents(t, base)
	if len(basePages) == 0 || !bytes.Contains(basePages[0], []byte("(Balance Sheet) Tj")) {
		t.Fatalf("baseline: want the heading itself to fit on page 1 in %dpt of room, it did not", room)
	}

	b := New(opts())
	b.Paragraph(front)
	b.f.y = b.f.pageH - b.f.marginB - room
	b.KeepTogether(addBalanceSheet)
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	// Not merely "2 pages exist somewhere" -- the heading itself must
	// be on the second page, i.e. moved by KeepTogether, not left where
	// the baseline put it with the rest of the group split away from it.
	pages := pageContents(t, bs)
	if len(pages) != 2 {
		t.Fatalf("want 2 content streams, got %d", len(pages))
	}
	if bytes.Contains(pages[0], []byte("(Balance Sheet) Tj")) {
		t.Errorf("want the heading on page 2, found it on page 1")
	}
	if !bytes.Contains(pages[1], []byte("(Balance Sheet) Tj")) {
		t.Errorf("want the heading on page 2, it is on neither")
	}
	// Everything in the group reaches the page, and only once.
	for _, want := range []string{"Balance Sheet", "Cash at bank", "100,000.00", "61,250.00"} {
		if n := bytes.Count(bs, []byte("("+want+") Tj")); n != 1 {
			t.Errorf("%q drawn %d times, want 1", want, n)
		}
	}
}

// The over-ask vs. consumption gap the original two-pass design had:
// a heading reserves more room (asking need()) than it goes on to use,
// for visual breathing room, and that gap used to be invisible to
// KeepTogether's measurement, which only looked at final consumption.
// A group ending in a heading -- rather than the usual table, whose
// trailing gap happens to cover it -- is exactly where that showed: at
// precisely the measured room, the heading used to split onto its own
// second page anyway, which is the identical orphaning KeepTogether
// exists to prevent, reached from the other end of the group.
func TestKeepTogetherAccountsForABlocksOwnOverAsk(t *testing.T) {
	probe := New(opts())
	probe.Paragraph("x")
	probe.Heading(2, "Notes")
	need := probe.f.y - probe.f.marginT

	b := New(opts())
	b.f.y = b.f.pageH - b.f.marginB - need
	b.KeepTogether(func(b *Builder) {
		b.Paragraph("x")
		b.Heading(2, "Notes")
	})
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 1 {
		t.Errorf("the group fits in exactly the room it was measured to need; want 1 page, got %d", pageCount(bs))
	}
	if n := bytes.Count(bs, []byte("(Notes) Tj")); n != 1 {
		t.Errorf("heading drawn %d times, want 1", n)
	}
}

// fn must draw only on the Builder it is handed. A closure that
// captures the outer Builder instead -- the obvious mistake the
// parameter's own shadowing invites -- is refused rather than let the
// probe silently measure nothing while the real page is drawn on
// without any room check at all.
func TestKeepTogetherPanicsIfFnDrawsOnTheOuterBuilder(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want a panic when fn draws on the outer Builder instead of its own parameter")
		}
	}()
	b := New(opts())
	b.KeepTogether(func(*Builder) {
		b.Paragraph("drawn on the wrong builder")
	})
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
