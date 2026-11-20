//go:build browser

package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Fiesta 1.5 draws one focus ring on a field: its edge turns to the
// accent, the 2px accent outline sits flush against it, and the 3px halo
// flush against that. The v0.28 pair cannot be the reference for those
// pixels, so the drives that hold the matrix to v0.28 let a focused
// field's ring band differ, and nothing else: the field's box must not
// move, every pixel outside the band is compared as usual, and the band
// must really change. ringFollowsAccent checks the ring's colours, and
// TestTheRingBandLetsNothingElseThrough proves the band blind to nothing
// else. This is Fiesta 1.5.0's check/ approach, ported.

// focusRingCrops are the matrix crops of a focused field, a key less its
// scheme.
var focusRingCrops = map[string]bool{
	"focus+focus-visible #field": true,
	"focus+focus-visible #area":  true,
}

// isFocusRingCrop reports whether a matrix key ("light focus+focus-visible
// #field") is one of focusRingCrops.
func isFocusRingCrop(key string) bool {
	_, crop, _ := strings.Cut(key, " ")
	return focusRingCrops[crop]
}

// matrixBox is a target's border box on the page, as the matrix read it
// with its state pinned: x, y, width, height.
type matrixBox [4]float64

// The ring band: from 1px inside the field's border box out to 5px
// outside it (the flush 2px outline, then 3px of halo), rounded outward
// on the outside and inward on the inside, plus a square inside each
// corner where the edge follows the field's radius (the largest a theme
// gives a field, 4px, its antialiasing and a pixel of rounding).
const (
	ringBandOut    = 5
	ringBandIn     = 1
	ringBandCorner = 7
)

// ringBand is the band in a crop's own coordinates, for a target whose
// border box is box: cropPNG puts the crop's origin at the floored box
// less matrixMargin.
func ringBand(box matrixBox) func(image.Point) bool {
	originX := math.Floor(box[0]) - matrixMargin
	originY := math.Floor(box[1]) - matrixMargin
	left, top := box[0]-originX, box[1]-originY
	right, bottom := left+box[2], top+box[3]
	floor := func(at float64) int { return int(math.Floor(at)) }
	ceil := func(at float64) int { return int(math.Ceil(at)) }
	outer := image.Rect(floor(left)-ringBandOut, floor(top)-ringBandOut,
		ceil(right)+ringBandOut, ceil(bottom)+ringBandOut)
	inner := image.Rect(ceil(left)+ringBandIn, ceil(top)+ringBandIn,
		floor(right)-ringBandIn, floor(bottom)-ringBandIn)
	edge := image.Rect(floor(left), floor(top), ceil(right), ceil(bottom))
	side := ringBandCorner
	corners := []image.Rectangle{
		image.Rect(edge.Min.X, edge.Min.Y, edge.Min.X+side, edge.Min.Y+side),
		image.Rect(edge.Max.X-side, edge.Min.Y, edge.Max.X, edge.Min.Y+side),
		image.Rect(edge.Min.X, edge.Max.Y-side, edge.Min.X+side, edge.Max.Y),
		image.Rect(edge.Max.X-side, edge.Max.Y-side, edge.Max.X, edge.Max.Y),
	}
	return func(point image.Point) bool {
		if !point.In(outer) {
			return false
		}
		if !point.In(inner) {
			return true
		}
		for _, corner := range corners {
			if point.In(corner) {
				return true
			}
		}
		return false
	}
}

// sameExceptRing compares a focused field's crops with only the ring band
// let through: the field must be where it was and its size, the crops
// the same size, and every pixel outside the band within maxDelta under
// samePixelsWithin's rules. changed says whether any pixel in the band
// differs, so a drive can insist the ring really moved.
func sameExceptRing(
	t *testing.T, was, is []byte, wasBox, isBox matrixBox, maxDelta int,
) (ok bool, why string, changed bool) {
	t.Helper()
	for side := range wasBox {
		if math.Abs(wasBox[side]-isBox[side]) > 0.01 {
			return false, fmt.Sprintf("the field's box moved or changed "+
				"size: %v before, %v after", wasBox, isBox), false
		}
	}
	before, after := decodePNG(t, was), decodePNG(t, is)
	if before.Bounds() != after.Bounds() {
		return false, fmt.Sprintf("the crops are %v and %v",
			before.Bounds(), after.Bounds()), false
	}
	inBand := ringBand(wasBox)
	magenta := color.RGBA{255, 0, 255, 255}
	blank := func(img image.Image) []byte {
		out := image.NewRGBA(img.Bounds())
		draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
		bounds := out.Bounds()
		for row := bounds.Min.Y; row < bounds.Max.Y; row++ {
			for col := bounds.Min.X; col < bounds.Max.X; col++ {
				if !inBand(image.Pt(col, row).Sub(bounds.Min)) {
					continue
				}
				changed = changed ||
					differs(before.At(col, row), after.At(col, row))
				out.Set(col, row, magenta)
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, out); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	ok, why = samePixelsWithin(t, blank(before), blank(after), maxDelta)
	return ok, why, changed
}

// differs compares two pixels by colour, whatever type each image keeps.
func differs(was, is color.Color) bool {
	r1, g1, b1, _ := was.RGBA()
	r2, g2, b2, _ := is.RGBA()
	return r1 != r2 || g1 != g2 || b1 != b2
}

// holdRing fails a focused field's crop that differs outside the ring
// band, or whose band did not change.
func holdRing(
	t *testing.T, key string, was, is []byte, wasBox, isBox matrixBox,
	maxDelta int,
) {
	t.Helper()
	ok, why, changed := sameExceptRing(t, was, is, wasBox, isBox, maxDelta)
	switch {
	case !ok:
		t.Errorf("%s renders differently under the new tokens.css outside "+
			"the focus ring: %s", key, why)
	case !changed:
		t.Errorf("%s renders as it did under the old tokens.css; the one "+
			"focus ring should have changed it", key)
	}
}

// accentProbe is the page's --rst-accent, as a computed colour.
const accentProbe = `(() => {
  const probe = document.createElement("span");
  probe.style.color = "var(--rst-accent)";
  document.body.append(probe);
  const accent = getComputedStyle(probe).color;
  probe.remove();
  return accent;
})()`

// ringFollowsAccent checks, on the page the drive last loaded, that a
// focused field's edge and outline are the accent: want, an app's
// override, or the page's own --rst-accent when want is "".
func ringFollowsAccent(t *testing.T, ctx context.Context, scheme, want string) {
	t.Helper()
	if want == "" {
		want = mustRead(t, ctx, chromedp.Evaluate[string](accentProbe))
	}
	for _, field := range []string{"#field", "#area"} {
		forceState(t, ctx, field, "focus", "focus-visible")
		edge := computed(t, ctx, field, "border-top-color")
		outline := computed(t, ctx, field, "outline-color")
		forceState(t, ctx, field)
		if edge != want || outline != want {
			t.Errorf("%s: focused %s has the edge %s and the outline %s, "+
				"want the accent %s in both", scheme, field, edge, outline,
				want)
		}
	}
}

// TestTheRingBandLetsNothingElseThrough is sameExceptRing's negative
// control, on the matrix's focused field and textarea under the old Day
// theme. The tree's tokens.css must pass with the band changed. A
// tokens.css that also changes the focused field's face, its text colour
// or its size must fail: the band lets the ring through and nothing else.
func TestTheRingBandLetsNothingElseThrough(t *testing.T) {
	oldTokens := legacyFile(t, "tokens-v028.css")
	oldTheme := legacyFile(t, "day-v028.css")
	const focused = "[rst-input]:focus-visible, [rst-textarea]:focus-visible"
	for _, served := range []struct {
		name, extra string
		passes      bool
	}{
		{"the tree's tokens.css", "", true},
		{"a focused face",
			focused + " { background-color: #fc0 !important; }", false},
		{"focused text in another colour",
			focused + " { color: #c00 !important; }", false},
		{"a focused field 6px taller",
			focused + " { padding-block: calc(0.45rem + 3px) !important; }",
			false},
	} {
		t.Run(served.name, func(t *testing.T) {
			tokens := append(append([]byte{}, TokensCSS()...),
				"\n"+served.extra+"\n"...)
			rig := legacyRig(t, oldTokens, oldTheme, tokens, oldTheme, false)
			ctx, cancel := context.WithTimeout(rig.Context(),
				120*time.Second)
			defer cancel()
			was, wasBoxes := matrixShotsBoxes(t, ctx, rig.Origin, "/old/",
				"light")
			is, isBoxes := matrixShotsBoxes(t, ctx, rig.Origin, "/new/",
				"light")
			read := 0
			for key := range was {
				if !isFocusRingCrop(key) {
					continue
				}
				read++
				ok, why, changed := sameExceptRing(t, was[key], is[key],
					wasBoxes[key], isBoxes[key], 24)
				switch {
				case served.passes && !ok:
					t.Errorf("%s: %s", key, why)
				case served.passes && !changed:
					t.Errorf("%s: the ring band did not change", key)
				case !served.passes && ok:
					t.Errorf("%s: passed; the band lets more than the "+
						"ring through", key)
				}
			}
			if read != len(focusRingCrops) {
				t.Fatalf("read %d focused-field crops, want %d", read,
					len(focusRingCrops))
			}
		})
	}
}
