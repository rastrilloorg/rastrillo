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
