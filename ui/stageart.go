package ui

import (
	"fmt"
	"html/template"
	"strings"
)

// stageArt draws the stage shell's default backdrop: fine wavy lines
// over a soft glow, in the spirit of keymail's sign-in page, so no app
// ships a blank page behind its sign-in card.
//
// It is deterministic in seed — line count, wavelength, amplitude,
// angle and the glow's position are picked from bounded ranges by a
// generator seeded from FNV-1a of the seed — so two apps differ and one
// app never changes between loads. The shell passes a constant seed,
// because no shell block may read the page's data; an app redefines the
// backdrop block as {{stageArt "its-name"}} for its own pattern, or with
// its own picture.
//
// The output is only numbers and fixed markup: the seed is never
// echoed, so returning template.HTML is safe. It carries no colour, no
// style and no reference to anything outside the page: tokens.css
// paints it through [rst-stage-art-lines] and [rst-stage-art-glow] with
// the theme's tokens, so it follows theme and scheme, and hides it
// under forced colours and in print.
func stageArt(seed string) template.HTML {
	r := splitmix{s: fnv1a64(seed)}
	lines := r.between(8, 12)
	half := r.between(100, 200) // half a wavelength, in viewBox units
	amp := r.between(12, 40)
	angle := r.between(-12, 12)
	cx, cy := r.between(240, 960), r.between(120, 480)

	var b strings.Builder
	b.WriteString(`<svg rst-stage-art aria-hidden="true" focusable="false" viewBox="0 0 1200 800" preserveAspectRatio="xMidYMid slice">`)
	// Concentric circles at low opacity rather than a gradient: a
	// gradient needs an id and a url() reference, and ids in an inline
	// SVG collide with the page's.
	b.WriteString(`<g rst-stage-art-glow>`)
	for i := 6; i >= 1; i-- {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d"/>`, cx, cy, i*70)
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<g rst-stage-art-lines transform="rotate(%d 600 400)">`, angle)
	gap := 800 / (lines + 1)
	for i := 1; i <= lines; i++ {
		// One quadratic curve, then smooth continuations: each "t"
		// reflects the last control point, which is what makes a wave.
		// Started a wavelength off the left edge and run past the
		// right, so the rotation never shows a line's end.
		fmt.Fprintf(&b, `<path d="M%d %dq%d %d %d 0`, -2*half, i*gap, half/2, -amp, half)
		for x := -half; x < 1400; x += half {
			fmt.Fprintf(&b, "t%d 0", half)
		}
		b.WriteString(`"/>`)
	}
	b.WriteString(`</g></svg>`)
	return template.HTML(b.String())
}

// splitmix is SplitMix64: a few lines, no allocation, and a good spread
// from a 64-bit hash — all a picture needs. Not math/rand, whose
// generators are not promised to stay the same across Go releases,
// which would change every app's backdrop on an upgrade.
type splitmix struct{ s uint64 }

func (m *splitmix) next() uint64 {
	m.s += 0x9e3779b97f4a7c15
	z := m.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// between is uniform over [lo, hi].
func (m *splitmix) between(lo, hi int) int {
	return lo + int(m.next()%uint64(hi-lo+1))
}
