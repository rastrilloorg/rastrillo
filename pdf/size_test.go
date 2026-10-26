package pdf

import (
	"math"
	"regexp"
	"testing"
)

// The zero value of Options.Size is A4: a caller who never sets it
// gets exactly what every test written before Size existed already
// assumed.
func TestZeroSizeIsA4(t *testing.T) {
	got := newGeom(Size{}, false)
	want := newGeom(A4, false)
	if got != want {
		t.Errorf("zero Size gave %+v, want A4's %+v", got, want)
	}
}

// Letter is a real, named alternative to A4, not just a landscape
// flip of it: the MediaBox has to carry Letter's own dimensions.
func TestLetterSizeInTheEmittedPDF(t *testing.T) {
	b := New(Options{Size: Letter})
	b.Paragraph("x")
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`/MediaBox \[0 0 612\.00 792\.00\]`)
	if !re.Match(bs) {
		t.Errorf("want a Letter MediaBox (%s) in the emitted PDF", re)
	}
}

// Landscape swaps whichever Size was named, not only A4's.
func TestLandscapeSwapsWhicheverSizeWasNamed(t *testing.T) {
	g := newGeom(Legal, true)
	if g.pageW != Legal.H || g.pageH != Legal.W {
		t.Errorf("want Legal's dimensions swapped, got %.2fx%.2f from %+v", g.pageW, g.pageH, Legal)
	}
}

// A caller is not limited to the named sizes: nothing about this
// package requires a standard one, and a Size is just a rectangle a
// caller can supply their own dimensions for — a pre-printed form, say.
func TestACustomSizeIsHonoured(t *testing.T) {
	custom := Size{W: 300, H: 500}
	g := newGeom(custom, false)
	if g.pageW != custom.W || g.pageH != custom.H {
		t.Errorf("want the custom size honoured, got %.2fx%.2f", g.pageW, g.pageH)
	}
}

// A Size too small to leave any content area once the margins are
// subtracted falls back to A4, rather than reaching layout with a
// negative content width -- the same reasoning Column.Width's own
// validation already applies to a value with no sane interpretation.
func TestATooSmallSizeFallsBackToA4(t *testing.T) {
	cases := []Size{
		{}, // the zero value: already covered above, repeated for clarity
		{W: -10, H: 500},
		{W: 10, H: 10}, // smaller than the margins alone
		{W: math.Inf(1), H: 800},
	}
	for _, c := range cases {
		got := newGeom(c, false)
		want := newGeom(A4, false)
		if got != want {
			t.Errorf("Size %+v gave %+v, want the A4 fallback %+v", c, got, want)
		}
	}
}

// A Size is checked after Landscape turns it, not before: the top and
// bottom margins are larger than the left and right ones, so a size
// that leaves room in portrait can leave a negative body height once
// it is on its side. Such a size falls back to A4, still landscape.
func TestASizeIsCheckedAfterLandscapeTurnsIt(t *testing.T) {
	got := newGeom(Size{W: 120, H: 300}, true)
	want := newGeom(A4, true)
	if got != want {
		t.Errorf("Size{120,300} landscape gave %+v, want the A4 landscape fallback %+v", got, want)
	}
}

// A page has to hold the one thing that cannot continue onto the next
// one: a table header at its four-line limit with one row under it. A
// body shorter than that drew the header into the footer, and falls
// back to A4 instead.
func TestASizeTooShortForATableHeaderFallsBackToA4(t *testing.T) {
	for _, c := range []Size{{W: 300, H: 141}, {W: 300, H: 175}} {
		if got, want := newGeom(c, false), newGeom(A4, false); got != want {
			t.Errorf("Size %+v gave %.2fx%.2f, want the A4 fallback", c, got.pageW, got.pageH)
		}
	}
	g := newGeom(Size{W: 300, H: 175}, false)
	if room := g.pageH - g.marginB - g.marginT; room < headerBandHeight(maxHeaderLines)+leadBody {
		t.Errorf("body height %.2f cannot hold a full header and a row", room)
	}
}

// A finite but enormous Size overflowed the column arithmetic to +Inf,
// which is not a PDF number. PDF readers also stop at 14,400pt (200in)
// a side (ISO 32000-1, Annex C), so anything larger falls back to A4.
func TestAHugeSizeFallsBackRatherThanWritingInf(t *testing.T) {
	for _, c := range []Size{{W: 1e308, H: 800}, {W: 20000, H: 20000}} {
		if got, want := newGeom(c, false), newGeom(A4, false); got != want {
			t.Errorf("Size %+v gave %.2fx%.2f, want the A4 fallback", c, got.pageW, got.pageH)
		}
		b := New(Options{Size: c})
		b.Table("", []Column{{Title: "a", Width: 2}, {Title: "b", Width: 1}}, [][]Cell{{{Text: "x"}, {Text: "y"}}}, nil)
		bs, _, err := b.Render()
		if err != nil {
			t.Fatal(err)
		}
		if regexp.MustCompile(`Inf|NaN`).Match(regexp.MustCompile(`/Info`).ReplaceAll(bs, nil)) {
			t.Errorf("Size %+v wrote a non-finite number into the PDF", c)
		}
	}
	if got := newGeom(Size{W: 14400, H: 14400}, false); got.pageW != 14400 {
		t.Errorf("want 14400pt, the largest page a reader accepts, honoured; got %.2f", got.pageW)
	}
}
