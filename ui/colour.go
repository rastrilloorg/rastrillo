package ui

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
)

// This file is rastrillo's colour engine (design doc §6-v2.2b and
// §6-v2.2c): the three entry points a caller needs to paint something in
// a colour it did not have to hand-verify, plus the WCAG arithmetic they
// are built on.
//
//   - Pair resolves one intent against one background into a fill and
//     the colour to draw on it, contrast-correct by construction. Use it
//     when a RULE picks the colour and you write both halves — a presence
//     cursor, an author dot, a conditional format.
//   - Wash resolves one into a background wash under an ink the caller
//     already has, and never returns an ink of its own. Use it when a
//     PERSON picked the colour and kept their own text — someone selects
//     a cell and clicks yellow.
//   - Allocate hands a set of opaque keys mutually distinguishable hues.
//
// They are three functions and not one on purpose. Contrast-correctness,
// legibility under someone else's ink, and mutual distinguishability are
// different guarantees; a single function that delivers whichever one its
// arguments imply is the shape of API nobody can gate.
//
// # What "contrast-correct" does and does not mean
//
// This is the same class of trap the contrast gate in contrast_test.go
// documents at length, and it is worth restating here because two teams
// will read this comment before they read that file.
//
// A Pair is correct against THE BACKGROUND YOU PASSED, and against
// nothing else. If you pass the colour you assume the fill sits on
// rather than the colour it actually sits on, Pair returns a pair that
// clears the floor against your assumption, your own contrast test
// asserts it against the same assumption, and the two agree while the
// rendered pixel is wrong. That is why background is a literal colour
// and never a scheme: a conditional format paints under a user fill, a
// selected cell sits on the selection tint, and a document canvas can be
// paper white in a dark theme. Resolve what the pixel is really on, then
// pass that.
//
// And contrast is not accessibility. Clearing 4.5:1 makes a label
// legible; it does not make colour a carrier of meaning. The framework's
// standing rule holds here without exception — colour never says
// anything on its own, so every author, cursor, thread and cell that
// carries a colour carries a name, initials or a label as well. Allocate
// leans on that rule: past the offered set's capacity it reports that
// separation is gone, and the design survives because the label was
// always doing the work.

// ContrastFloorText is WCAG 2.2 AA's 4.5:1 floor for normal-size body
// text. Exported because a caller writing its own contrast gate over its
// own backgrounds should hold the same floor with the same arithmetic as
// the framework, not a re-typed approximation of it.
const ContrastFloorText = 4.5

// ContrastFloorBoundary is WCAG 2.2 AA's 3:1 floor for a non-text user
// interface component boundary (1.4.11) — a control border, and here a
// fill that has to read as a shape sitting on a background.
const ContrastFloorBoundary = 3.0

// contrastMargin is how far above a floor the search insists on landing.
// Ratios here are computed from the eight-bit hex actually emitted, so a
// result that clears the floor clears it exactly; the margin exists so a
// caller re-measuring in another language, with another rounding, cannot
// come out on the other side of a boundary this engine sat exactly on.
const contrastMargin = 0.05

// minDeliveredChroma is the least chroma an offered intent may resolve
// to and still count as a colour rather than a grey. It is a separation
// requirement, not an aesthetic one: two hues that both collapse towards
// neutral at some background are no longer distinguishable from each
// other, which would leave Allocate's guarantee true in name and false
// on the screen.
//
// CheckIntents holds an intent to it, whether the chroma was thrown away
// by gamut reduction or never asked for: a grey is not an offered colour.
// Pair does not, because a caller who asks for chroma 0 wants a grey and
// should get one.
const minDeliveredChroma = 0.035

// Intent is an unresolved colour: a hue and a chroma in OKLCH, with no
// lightness and therefore no rendered colour of its own. Lightness is
// Pair's to choose, because it is the only one of the three that has to
// change with the background — the same stored intent is a dark fill on
// paper white and a light one on dark paper, and that is the whole reason
// intent and colour are separate things here.
//
// Hue is in degrees. Chroma is OKLCH chroma, roughly 0 (grey) to 0.37
// (the most saturated colour sRGB can hold, and only at a few hues); a
// chroma no lightness can deliver is reduced rather than clipped, see
// Pair.
type Intent struct {
	Hue    float64
	Chroma float64
}

// Swatch is one intent resolved against one background: the concrete
// colours, and the measurements that say why they are the ones you got.
//
// Fill, On and Background are #rrggbb literals. The ratios are computed
// from those eight-bit values and not from the floating-point colours
// behind them, so they are the ratios of the pixels a browser will
// actually paint — recompute them yourself and you will get the same
// numbers.
//
// Hue and Chroma are the intent AS DELIVERED. Chroma is at most the
// chroma you asked for, and is lower whenever the requested one lay
// outside sRGB at the lightness the background forced. Read it if you
// care whether you got the colour you asked for.
//
// The three measurements are always computed and always mean the same
// thing; which of them carries a FLOOR depends on which function made the
// swatch, because Pair and Wash guarantee different things:
//
//	              FillRatio          OnRatio            Separation
//	Pair          >= 3:1 (floored)   >= 4.5:1 (floored) measured
//	Wash          measured, and LOW  >= 4.5:1 (floored) >= MinSeparation,
//	                                                    and as near the
//	                                                    request as it can be
//
// A Wash's FillRatio is low by design — a wash that stood 3:1 off the
// page would not be a wash — so do not gate on it and do not carry a
// Wash swatch into a check written for a Pair one.
type Swatch struct {
	Fill       string
	On         string
	Background string

	Hue    float64
	Chroma float64

	// FillRatio is Fill against Background as a WCAG contrast ratio.
	// OnRatio is On against Fill.
	FillRatio float64
	OnRatio   float64

	// Separation is Fill against Background as a perceptual distance
	// (DeltaEOK) rather than a contrast ratio. It is the measurement
	// behind "can you see that this cell is filled", which contrast
	// ratio answers badly: a pale yellow wash is about 1.2:1 off white,
	// a number that says nothing useful, while being plainly visible.
	Separation float64

	// SeparationRequested is the weight the caller asked Wash for, kept
	// beside the one they got. Zero for a swatch from Pair, which is not
	// asked for a weight. See Wash for the scale.
	SeparationRequested float64

	// SeparationMet reports whether the wash carries AT LEAST the weight
	// that was asked for. It is false in one case only: the ink and the
	// background between them would not allow that much weight, so the
	// wash came back lighter than requested.
	//
	// One case, deliberately, because the flag exists to be acted on. The
	// harm a caller warns about is a highlight quieter than the one their
	// user asked for — a flag that also fired when the wash came back
	// HEAVIER would fire on an outcome nobody needs to be warned about,
	// and a warning that fires when nothing is wrong stops being read.
	// So a heavier wash is met: you asked for at least that much weight
	// and you got at least that much. It happens when the constraints
	// exclude the requested weight from below, and Separation is there to
	// be read if you care which way it went.
	//
	// A request under MinSeparation is likewise met, since it is raised
	// to the floor and the floor is heavier than what was asked. Compare
	// SeparationRequested against MinSeparation if you want to know.
	//
	// True for a swatch from Pair, which requests no weight and therefore
	// cannot fail to carry it — so `if !sw.SeparationMet { warn }` is
	// safe to write against any swatch, from either function.
	//
	// Read this rather than comparing the two floats yourself. The
	// comparison needs a tolerance for the grid's own resolution, and the
	// achievable weights are not evenly spaced — the ink floor can cut a
	// wide band out of the middle — so it is exactly the arithmetic every
	// caller would get slightly differently.
	SeparationMet bool
}

// Pair resolves an intent against one background into a fill and an
// on-fill that clear their floors by construction.
//
//	sw, err := ui.Pair(264, 0.14, "#ffffff")
//	// sw.Fill is what you paint; sw.On is what you draw on it.
//
// background is a literal #rgb or #rrggbb colour: the colour the fill
// will actually sit on. Never a theme name, never a scheme, never a
// var() — see the trap described at the top of this file.
//
// Two floors hold, and they are the framework's own two:
//
//   - Fill against background clears ContrastFloorBoundary (3:1). The
//     fill is a shape a reader has to be able to see on the page — a
//     cell fill, an avatar disc, a caret, a highlight block, a thread
//     dot. This is the floor that makes background a real parameter: it
//     is the reason the same intent resolves dark on paper white and
//     light on dark paper, and the reason an intent has to be proven
//     against every background it can be rendered on rather than one.
//   - On against Fill clears ContrastFloorText (4.5:1), so a label,
//     initials or a run of text drawn on the fill is legible.
//
// What this deliberately does not give you is a pale wash under existing
// body text — a fill that leaves the author's own ink alone. That is a
// different guarantee (the text keeps its own contrast; the fill stands
// off the page hardly at all), and asking one function for both is what
// the split into separate entry points exists to prevent.
//
// USE Wash FOR THAT, and do not mix your own. Hand-rolling it is the
// pattern this package exists to replace: a fill chosen without the ink
// in hand either restyles the author's text or leaves it unreadable on
// the result, and the first of those writes a font colour into an
// exported file that nobody set. Wash takes the ink you already have and
// returns only a fill.
//
// Lightness is chosen as the least loud one that clears both floors: of
// every lightness that works, the one nearest the background's own. So a
// fill is as quiet as its floor allows rather than as loud as the space
// allows.
//
// Out of gamut is handled by reducing chroma at the chosen lightness and
// hue until the colour fits sRGB, never by clamping channels. Clamping
// is what silently changes a colour's hue — clip one channel of a
// too-saturated blue and it comes back a different blue — and hue is the
// one part of an intent that carries the caller's meaning. Desaturating
// keeps the hue exact and loses only vividness, and Swatch.Chroma
// reports how much was lost, so nothing is silent either way.
//
// Pair returns an error for a background it cannot parse, for a negative
// or non-finite chroma, and for the rare intent that no lightness can
// resolve against this background. It never returns a Swatch that misses
// a floor.
func Pair(hue, chroma float64, background string) (Swatch, error) {
	if math.IsNaN(hue) || math.IsInf(hue, 0) {
		return Swatch{}, fmt.Errorf("ui.Pair: hue must be a finite number, got %v", hue)
	}
	if math.IsNaN(chroma) || math.IsInf(chroma, 0) || chroma < 0 {
		return Swatch{}, fmt.Errorf("ui.Pair: chroma must be finite and >= 0, got %v", chroma)
	}
	bg, err := normalHex(background)
	if err != nil {
		return Swatch{}, fmt.Errorf("ui.Pair: background: %w", err)
	}
	h := math.Mod(hue, 360)
	if h < 0 {
		h += 360
	}

	bgR, bgG, bgB, err := parseHex(bg)
	if err != nil {
		return Swatch{}, err
	}
	bgLab := oklabOf(bgR, bgG, bgB)
	bgL := bgLab[0]
	bgLum := relLuminance(bgR, bgG, bgB)

	// Where the fill is allowed to be. A fill clears the boundary floor
	// by being far enough from the background in luminance, and it can
	// be on either side, so the two thresholds are the two sides:
	// anything at or below darkMax is dark enough, anything at or above
	// lightMin is light enough, and the band between them is barred.
	const target = ContrastFloorBoundary + contrastMargin
	darkMax := (bgLum+0.05)/target - 0.05
	lightMin := target*(bgLum+0.05) - 0.05

	// A fill's luminance rises with its lightness, monotonically, over
	// the whole grid at every hue and chroma — gamut reduction narrows
	// the colour but never turns the ramp back on itself, which
	// TestLuminanceRisesWithLightness holds it to. So the two boundaries
	// are found by bisection rather than by walking into them: the
	// highest step still dark enough, and the lowest step already light
	// enough. Everything below the first and above the second is
	// feasible; the band between them is not.
	lum := func(step int) float64 {
		rgb, _ := oklchRGB(stepLightness(step), chroma, h)
		return relLuminance(rgb[0], rgb[1], rgb[2])
	}
	down := lastStepAtMost(lum, darkMax)  // -1 when even black is too light
	up := firstStepAtLeast(lum, lightMin) // lightnessSteps+1 when even white is too dark

	// Then take the nearest of the two, and on failure keep walking
	// outward from whichever we took. Visiting in order of distance from
	// the background means the first swatch that also satisfies the ink
	// is the nearest one that does — the quietest fill that works, which
	// is what "least loud" means here. Ties go to the lower lightness.
	for down >= 0 || up <= lightnessSteps {
		var step int
		switch {
		case down < 0:
			step, up = up, up+1
		case up > lightnessSteps:
			step, down = down, down-1
		case math.Abs(stepLightness(down)-bgL) <= math.Abs(stepLightness(up)-bgL):
			step, down = down, down-1
		default:
			step, up = up, up+1
		}

		rgb, c := oklchRGB(stepLightness(step), chroma, h)
		fillRatio := ratioOf(relLuminance(rgb[0], rgb[1], rgb[2]), bgLum)
		if fillRatio < target {
			continue // only reachable if the bisection's premise ever broke
		}
		on, onRatio, ok := ink(rgb, h, c)
		if !ok {
			continue
		}
		return Swatch{
			Fill:       hexOf(rgb),
			On:         on,
			Background: bg,
			Hue:        h,
			Chroma:     c,
			FillRatio:  fillRatio,
			OnRatio:    onRatio,
			Separation: labDistance(oklabOf(rgb[0], rgb[1], rgb[2]), bgLab),
			// Pair asks for no weight, so it cannot have failed to carry
			// one. Set explicitly rather than left as the zero value, so
			// that `if !sw.SeparationMet` does not warn on every Pair
			// result a caller happens to run it against.
			SeparationMet: true,
		}, nil
	}
	return Swatch{}, fmt.Errorf("ui.Pair: no lightness resolves hue %g chroma %g against background %s at %.1f:1 fill and %.1f:1 on-fill",
		h, chroma, bg, ContrastFloorBoundary, ContrastFloorText)
}

// stepLightness is the lightness one step of the grid stands for.
func stepLightness(step int) float64 {
	return lightnessLow + (lightnessHigh-lightnessLow)*float64(step)/float64(lightnessSteps)
}

// lastStepAtMost returns the highest step whose value is <= bound, or -1
// if none is. lum must be non-decreasing.
func lastStepAtMost(lum func(int) float64, bound float64) int {
	lo, hi := -1, lightnessSteps+1 // lo is known good, hi known bad
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if lum(mid) <= bound {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// firstStepAtLeast returns the lowest step whose value is >= bound, or
// lightnessSteps+1 if none is. lum must be non-decreasing.
func firstStepAtLeast(lum func(int) float64, bound float64) int {
	lo, hi := -1, lightnessSteps+1 // lo is known bad, hi known good
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if lum(mid) >= bound {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// The lightness grid Pair chooses from. A grid rather than a closed-form
// solve, because gamut reduction and eight-bit rounding both sit between
// a lightness and its contrast, and a grid is a thing a reader can check
// by hand. The step is fine enough that the chosen fill sits within about
// a quarter of a per cent of the quietest one available.
const (
	lightnessLow   = 0.02
	lightnessHigh  = 0.995
	lightnessSteps = 390
)

// ink picks the colour to draw on fill: a near-white or near-black tinted
// with the fill's own hue, whichever clears ContrastFloorText by more. It
// reports false when neither does, which happens for fills in the middle
// of the lightness range where nothing is 4.5:1 away in either direction.
//
// The ink is tinted rather than pure so a label on a coloured fill looks
// like it belongs to it; the tint is capped hard because chroma in an ink
// costs contrast, and contrast is what the ink is for.
//
// chroma is the chroma the FILL was actually delivered, not the one that
// was requested. A fill that lost most of its chroma to the gamut is a
// muted colour, and an ink tinted for the saturation it was asked for
// rather than the one it got would be tinted for a colour that is not on
// the screen.
func ink(fill [3]uint8, hue, chroma float64) (string, float64, bool) {
	type candidate struct{ l, maxC float64 }
	fillLum := relLuminance(fill[0], fill[1], fill[2])
	best, bestRatio, ok := "", 0.0, false
	for _, c := range []candidate{{0.99, 0.03}, {0.20, 0.05}} {
		rgb, _ := oklchRGB(c.l, math.Min(chroma, c.maxC), hue)
		ratio := ratioOf(relLuminance(rgb[0], rgb[1], rgb[2]), fillLum)
		if ratio < ContrastFloorText+contrastMargin {
			continue
		}
		if !ok || ratio > bestRatio {
			best, bestRatio, ok = hexOf(rgb), ratio, true
		}
	}
	return best, bestRatio, ok
}

// Wash resolves an intent into a background wash that leaves the
// caller's own text colour alone.
//
//	sw, err := ui.Wash(115, 0.14, 0.12, authorInk, canvas)
//	// sw.Fill is what you paint. sw.On is authorInk.
//	if !sw.SeparationMet { /* warn: this is paler than you asked for */ }
//
// It is Pair's sibling, not Pair with the arguments swapped, and the
// difference is about who owns the ink.
//
// # Why this exists: a font colour nobody set
//
// Pair is right when the caller owns both halves — a presence cursor, a
// comment author's dot, a conditional format, anywhere a RULE picks the
// colour and the app writes the fill and the text together. It is wrong
// for the commonest case in a spreadsheet or a document: someone selects
// a cell, or a run of words, and clicks yellow.
//
// That person asked for a background and did not ask for their font
// colour to change. If the engine hands back an on-fill and the app
// applies it, the app has to persist it — and on export that is a font
// colour written into the file that the author never set. Import a
// workbook, highlight one cell, export, and it comes back with font
// colours throughout. That is round-trip corruption, and it is why the
// constraint runs the other way here: not "given this fill, what ink
// survives on it" but "given the ink this author already has, what fill
// of this hue does their ink still read on".
//
// Wash never invents a font colour. On is the ink you passed, normalised
// to lower-case #rrggbb so that all three colours in a Swatch are spelled
// one way — compare it case-insensitively, or against sw.On rather than
// against the string you stored.
//
// # It requires a known ink, and that is a real limit
//
// Wash is the wrong function if you do not control the text colour. It
// guarantees legibility for the ink you hand it and says nothing about
// any other, so passing a guessed ink buys a guarantee about a colour
// that is not on the screen — and if you then apply the guess, you have
// restyled an author's text, which is the same harm as the font colour
// leaking into an export that this function exists to prevent.
//
// A highlight behind text the app did not choose and must not change is
// the case that needs a different guarantee: not "this ink is legible"
// but "any ink that was legible on the background is still legible on
// the wash". That is a second function, not this one with the argument
// omitted, and the arithmetic says why. On paper white the worst ink
// still legible against the page sits at luminance 0.1833, and for it to
// keep the full 4.5:1 on a wash the wash would need luminance 1.0 —
// exactly white, which is no wash at all. Retaining the whole floor is
// not merely hard there, it is unsatisfiable, so an ink-unknown variant
// has to retain a lower floor than it admits, and choosing that number
// is a design decision rather than a defaulted parameter. It is being
// designed separately; do not approximate it by guessing an ink here.
//
// # separation: how heavy the wash should be
//
// A fill exists to be found. Someone scans a thousand rows for the cell
// they flagged, so a wash is a flag and not a texture, and one sitting at
// the threshold of perceptibility inverts its own job. separation is the
// weight you are asking for, in OKLab distance from the background, and
// the contract is: AS CLOSE TO THE REQUESTED WEIGHT AS THE INK AND
// BACKGROUND ALLOW, NEVER BELOW MinSeparation. When the constraints bind
// it degrades toward paler rather than starting there.
//
// # What to pass
//
// PASS 0.12 IF YOU HAVE NO PARTICULAR OPINION. It is the middle of the
// weight Excel's own conditional-formatting presets carry, which is what
// a spreadsheet fill weighs in the software people compare you against.
//
// Two bands are worth naming. 0.10 to 0.14 is the rule-driven register —
// a conditional format, a status tint, anything a rule applied rather
// than a person chose — and 0.12 is its middle. 0.21 and above is the
// hand-picked register, where somebody deliberately reached for a
// colour and wants it seen. Below 0.05 you are asking for a tint a
// scanning eye will miss.
//
// # What can actually be delivered
//
// The band that matters is the one reachable under the DEFAULT case, and
// near-black ink on a white canvas is the default case: it is what a
// cell looks like before anybody touches it, it is what imported files
// overwhelmingly carry, and a user picking a fill has almost always left
// their text alone.
//
//	Under any near-black ink on white, every offered hue reaches
//	0.39. Under pure black specifically, 0.43.
//
// So both named bands are comfortably deliverable, and a picker built on
// them will not offer weights it cannot honour. Above roughly 0.39 the
// answer starts to depend on the hue, and past about 0.47 nothing is
// reachable at all — black text has to stay readable on the result, and
// that caps how dark the fill can go.
//
// # The scale underneath, as provenance
//
// Real colours measured against a white canvas on the same scale
// Swatch.Separation reports, so a number can be chosen by pointing at
// one you know. The rule is where these stop being weights you can ask
// for and become colours you can only paint:
//
//	0.030  MinSeparation — OUR floor, where a fill stops being visible
//	0.036  Google Sheets' palest fill, which is a grey (#F3F3F3)
//	0.064  Google Sheets' palest coloured fill (#FFF2CC)
//	0.11   Excel's "light green" conditional-formatting preset (#C6EFCE)
//	0.12   Excel's "light yellow" preset (#FFEB9C)
//	0.14   Excel's "light red" preset (#FFC7CE)
//	0.21   flat yellow (#FFFF00), the most-clicked fill there is
//	0.38   a solid green (#00B050)
//	----- the ceiling under near-black ink on white is about here -----
//	0.45   flat red (#FF0000)      — reachable at some hues, not others
//	0.63   flat blue (#0000FF)     — not reachable under black ink at all
//
// The first row is ours and is not attributed to anybody: it is where
// this engine stops calling something a wash. The two below it are what
// the palest thing another product ships actually measures, which is a
// different claim and a more useful one — our floor sits just under
// Sheets' palest fill and well under their palest COLOUR, so a caller
// asking near the floor is asking for something fainter than any
// coloured fill in a spreadsheet they have used.
//
// The last two rows are why the line is drawn rather than left implied.
// Flat red and flat blue are perfectly good colours and a caller may
// well store them; they are not weights to ask a wash for under black
// text, because no fill that heavy can carry black text at 4.5:1. Ask
// for one anyway and you get the closest that can be delivered, with
// SeparationMet false — which is correct, but if your picker offers them
// as choices, that flag becomes wallpaper.
//
// A request under MinSeparation is raised to it, and a request the ink
// cannot carry is met as closely as it can be. Neither is an error.
// SeparationRequested keeps what you asked for beside Separation, which
// is what you got, and SeparationMet says whether you got at least that
// much — false only when the wash came back LIGHTER than asked, which is
// the one outcome worth warning a user about. Read the boolean rather
// than comparing the floats: the achievable weights are not evenly
// spaced, so the comparison needs a tolerance the caller would have to
// know, and the library does it once.
//
// # The two floors
//
//  1. ink clears ContrastFloorText (4.5:1) against the returned fill.
//     The author's text stays theirs and stays legible.
//  2. The fill is at least MinSeparation from background in OKLab —
//     perceptibly different, or the user clicks yellow and nothing
//     appears to have happened.
//
// The second floor is measured as a perceptual distance and not as a
// contrast ratio, because contrast ratio is the wrong instrument for
// "can you tell this cell is filled": every colour in the scale above is
// between 1.0:1 and 1.3:1 against white, so a contrast floor would either
// admit all of them or reject all of them.
//
// # When there is no answer, you get an error
//
// Wash fails rather than returning a wash the author's text cannot be
// read on. That failure is the feature, and the case for it is not
// hypothetical or about careless users: Excel ships a
// conditional-formatting preset, "Yellow Fill with Dark Yellow Text",
// that pairs #9C6500 on #FFEB9C at 4.12:1 and fails WCAG AA. The
// product's own default does it.
//
// What is claimed here is precise and worth reading twice: A WASH THIS
// FUNCTION PRODUCES CANNOT BE UNREADABLE. Not "no cell in your app can
// be unreadable" — if you retain an imported file's fill and font colour
// verbatim, which is exactly what makes import and export lossless, then
// a document using that Excel preset will display at 4.12:1 and it
// should. Faithfully showing a document somebody else authored is a
// different act from generating a colour, and only the second is this
// function's to guarantee.
//
// # background is a literal colour, for the same reason as Pair's
//
// and the export case makes it concrete. A stored intent resolves per
// viewer, so a light reader and a dark reader see two different washes
// of one highlight. XLSX carries one hex per cell, so on export that
// per-viewer resolution collapses and a background has to be picked. The
// theme someone happened to be using when they clicked yellow must not
// leak into the file: a fill imported from a workbook exports as its
// retained original hex, untouched, so a file the caller did not author
// leaves exactly as it arrived, and a fill the user picked in-app
// exports resolved against a canonical LIGHT background, because Excel's
// canvas is white and that is where the file will be opened. The export
// surface is not the viewer's surface. A scheme enum cannot say any of
// that; a colour can.
//
// ink and background are literal #rgb or #rrggbb colours. ink is
// commonly theme-derived — near-black on a light canvas, near-white on a
// dark one — but an author who pinned their font colour pins it here
// too, and "the author pinned black and the viewer chose the dark theme"
// is a real case. See CheckWashes.
//
// # A worked example where the constraint binds
//
// Flat blue, #0000FF, is a real fill out of a real workbook. Black ink on
// it is 2.44:1, so it cannot carry black text at all — asking for blue at
// its own weight (0.63) under black ink returns a much lighter blue,
// somewhere near the weight black ink can actually hold, with
// SeparationMet false. Nothing failed; the answer is the honest one, and
// the boolean is how a caller knows to say so in its own interface.
func Wash(hue, chroma, separation float64, ink, background string) (Swatch, error) {
	if math.IsNaN(hue) || math.IsInf(hue, 0) {
		return Swatch{}, fmt.Errorf("ui.Wash: hue must be a finite number, got %v", hue)
	}
	if math.IsNaN(chroma) || math.IsInf(chroma, 0) || chroma < 0 {
		return Swatch{}, fmt.Errorf("ui.Wash: chroma must be finite and >= 0, got %v", chroma)
	}
	if math.IsNaN(separation) || math.IsInf(separation, 0) || separation < 0 {
		return Swatch{}, fmt.Errorf("ui.Wash: separation must be finite and >= 0, got %v", separation)
	}
	bg, err := normalHex(background)
	if err != nil {
		return Swatch{}, fmt.Errorf("ui.Wash: background: %w", err)
	}
	pen, err := normalHex(ink)
	if err != nil {
		return Swatch{}, fmt.Errorf("ui.Wash: ink: %w", err)
	}
	h := math.Mod(hue, 360)
	if h < 0 {
		h += 360
	}
	target := math.Max(separation, MinSeparation)

	bgR, bgG, bgB, _ := parseHex(bg)
	bgLab := oklabOf(bgR, bgG, bgB)
	bgLum := relLuminance(bgR, bgG, bgB)
	inkR, inkG, inkB, _ := parseHex(pen)
	inkLum := relLuminance(inkR, inkG, inkB)

	// Nearest first, and "nearest" is now nearest to the REQUESTED
	// weight rather than to the background — so the floor and the
	// objective are still the same measurement, but the objective moved.
	//
	// The walk can still stop early, on the premise that a perceptual
	// distance is at least the lightness difference alone: the other two
	// axes only add to it. So a candidate at lightness distance d has
	// separation of at least d - washSlack, and once a wash missing the
	// target by e has been found, any d with d - washSlack >= target + e
	// is separated by at least target + e and cannot get closer.
	// washSlack absorbs the gap between the lightness asked for and the
	// lightness that survives rounding to eight bits;
	// TestSeparationIsAtLeastTheLightnessDifference measures it.
	best, bestErr, found := Swatch{}, math.Inf(1), false
	for w, ok := nearestFirst(bgLab[0]); ok; w, ok = w.next() {
		l := w.lightness()
		if found && math.Abs(l-bgLab[0])-washSlack >= target+bestErr {
			break
		}
		rgb, c := oklchRGB(l, chroma, h)
		sep := labDistance(oklabOf(rgb[0], rgb[1], rgb[2]), bgLab)
		if sep < MinSeparation {
			continue // you could not tell the cell had been filled
		}
		fillLum := relLuminance(rgb[0], rgb[1], rgb[2])
		onRatio := ratioOf(inkLum, fillLum)
		if onRatio < ContrastFloorText+contrastMargin {
			continue // the author's own text would not be readable on it
		}
		if e := math.Abs(sep - target); !found || e < bestErr {
			best, bestErr, found = Swatch{
				Fill:                hexOf(rgb),
				On:                  pen,
				Background:          bg,
				Hue:                 h,
				Chroma:              c,
				FillRatio:           ratioOf(fillLum, bgLum),
				OnRatio:             onRatio,
				Separation:          sep,
				SeparationRequested: separation,
				SeparationMet:       sep >= separation-washTolerance,
			}, e, true
		}
	}
	if !found {
		return Swatch{}, fmt.Errorf("ui.Wash: no wash of hue %g chroma %g works on background %s under ink %s — nothing this hue can be is both %.2f away from the background and readable at %.1f:1 under that ink",
			h, chroma, bg, pen, MinSeparation, ContrastFloorText)
	}
	return best, nil
}

// washSlack is how much the early exit allows for the difference between
// the lightness Wash asks for and the lightness that survives rounding to
// eight bits. The break reasons about the requested lightness while the
// separation is measured on the delivered colour, and this is the whole
// of the gap between them.
//
// It is not a comfort margin: at 0 the walk stops early and misses the
// true answer on real inputs. TestSeparationIsAtLeastTheLightnessDifference
// measures the largest shortfall over a wide sample and pins this above
// it, so the number is a measurement with headroom rather than a guess.
const washSlack = 0.08

// washTolerance is float and grid dust, and nothing more. The lightness
// grid is discrete, so a request that is comfortably achievable still
// lands a hair either side of the number asked for, and comparing a
// delivered weight against a requested one on the nose would report a
// miss of four thousandths as a failure to honour.
//
// It is deliberately far too small to paper over a real shortfall. The
// achievable weights are not evenly spaced: the ink floor removes a band
// of lightnesses outright, so the set of weights a caller can actually
// be given has gaps in it more than ten times this tolerance wide, and a
// tolerance able to swallow those would report every constrained answer
// as honoured. Those are exactly the cases SeparationMet exists to flag.
// TestAchievableWeightsHaveWideGaps measures the gaps and asserts that
// relation rather than quoting a figure.
const washTolerance = 0.005

// nearestFirst starts a walk of the lightness grid in order of distance
// from a target, nearest first: a two-cursor merge, one running down from
// the step nearest the target and one up from just above it, taking
// whichever is closer and preferring the lower one on a tie.
//
// Pair does not use it — its floor is monotone in lightness, so it
// bisects for the two boundaries instead. Wash's second floor is a
// distance and turns back on itself either side of the background, so it
// walks.
func nearestFirst(target float64) (walk, bool) {
	k := int(math.Round((target - lightnessLow) / (lightnessHigh - lightnessLow) * float64(lightnessSteps)))
	k = min(max(k, 0), lightnessSteps)
	return walk{target: target, down: k, up: k + 1}.next()
}

type walk struct {
	target   float64
	down, up int
	step     int
}

func (w walk) lightness() float64 { return stepLightness(w.step) }

func (w walk) next() (walk, bool) {
	switch {
	case w.down < 0 && w.up > lightnessSteps:
		return w, false
	case w.down < 0:
		w.step, w.up = w.up, w.up+1
	case w.up > lightnessSteps:
		w.step, w.down = w.down, w.down-1
	case math.Abs(stepLightness(w.down)-w.target) <= math.Abs(stepLightness(w.up)-w.target):
		w.step, w.down = w.down, w.down-1
	default:
		w.step, w.up = w.up, w.up+1
	}
	return w, true
}

// offered is the bounded set of hues this engine allocates from: twelve,
// evenly spaced, at one chroma.
//
// Bounded rather than a free wheel, because a fixed set can be PROVEN —
// CheckIntents runs every one of them against every background the suite
// can render, at build time, exactly the way the 26 documented token
// pairs are proven. A generator that hands out an arbitrary hue can only
// be hoped for. That is the same distinction that ruled out a palette
// randomiser (design doc §6-v2.2).
//
// Evenly spaced rather than curated: thirty degrees of OKLCH hue is a
// step no one confuses at these lightnesses, and a curated list would be
// a set of aesthetic judgements with no gate behind them. Twelve is the
// capacity, and Allocate says so out loud when a caller passes it.
//
// The chroma is a request, not a promise: a hue that cannot hold 0.14 at
// the lightness some background forces comes back lower, and CheckIntents
// is what stops it coming back grey.
var offered = func() []Intent {
	const n = 12
	out := make([]Intent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Intent{Hue: 25 + float64(i)*(360.0/n), Chroma: 0.14})
	}
	return out
}()

// Offered returns the hues Allocate draws from, in allocation order. The
// slice is a copy: callers that want their own set (Docs is bringing
// one) pass it to CheckIntents themselves rather than editing this one.
//
// len(Offered()) is the capacity referred to everywhere below.
func Offered() []Intent {
	return slices.Clone(offered)
}

// hueMatchTolerance is how near an avoid value has to be to an offered
// hue to count as that hue. Half a degree: wide enough to survive a
// round trip through a serialisation that dropped digits, far narrower
// than the thirty degrees between neighbours.
const hueMatchTolerance = 0.5

// Allocate assigns each key a hue from the offered set, chosen so that no
// two keys share one for as long as the set has room.
//
//	intents, separated := ui.Allocate(keys, nil)
//	// intents[i] belongs to keys[i]
//	sw, _ := ui.Pair(intents[i].Hue, intents[i].Chroma, canvas)
//
// The returned slice is aligned with keys: intents[i] is keys[i]'s
// colour. Duplicate keys get the same intent and consume one hue between
// them.
//
// A key is opaque. It is a stable string the caller owns the meaning of —
// a member id, a membership row, a session, a column name. The framework
// has no idea what an identity is and this keeps it that way.
//
// avoid holds hues to keep out of the allocation, so a caller can run a
// second allocation that does not collide with a first (Sheets allocates
// cursors around fills that are already on the sheet). A value is matched
// to the offered hue within hueMatchTolerance of it; a value matching no
// offered hue is ignored, since it cannot be handed out anyway.
//
// # Separation is the guarantee; stability is best-effort
//
// You cannot have both, so this is the choice and not an oversight. A
// hash gives the same key the same colour in every document, which is
// what makes a person recognisable — but two people in one document can
// hash to the same hue, and separation is the property carrying meaning
// on the screen. So: hash for the preferred hue, displace on collision.
// A key's colour is therefore stable across documents until some other
// key in the same document collides with it, and then it moves.
//
// # The algorithm, in full, because two clients have to agree
//
// Determinism is a correctness requirement here rather than a nicety. If
// two clients rendering one document disagree about who is which colour,
// that is worse than no colour at all — so the allocation is a pure
// function of the key SET and never of the order the keys arrived in, or
// of anything else a client holds privately.
//
//  1. Sort the distinct keys ascending by their bytes.
//  2. For each key in that order, probe (hash(key)+i) mod N for i
//     rising from 0, and take the first hue that is neither already
//     taken in this pass nor in avoid.
//
// hash is FNV-1a/64 over the key's bytes; N is len(Offered()). Both are
// written down because an implementation that agrees about who moves and
// disagrees about where they move to reproduces, one level down, exactly
// the bug the sorted order was introduced to prevent.
//
// avoid is applied INSIDE the probe rather than filtered out before it,
// so an avoided hue displaces a key exactly the way a taken one does and
// the two orderings cannot disagree.
//
// # Past capacity
//
// separated reports whether every key came away with a hue that is both
// unshared and outside avoid. It is false once the keys plus the avoided
// hues outrun the offered set — the allocation is still returned, still
// deterministic, and hues repeat. Nothing is silently reused: a caller
// leaning on colour to tell two things apart reads this flag and stops.
// A caller who is not leaning on it can ignore it, because the standing
// rule means every one of those things carries a label as well.
func Allocate(keys []string, avoid []float64) ([]Intent, bool) {
	n := len(offered)
	out := make([]Intent, len(keys))
	if len(keys) == 0 {
		return out, true
	}

	avoided := make([]bool, n)
	for _, a := range avoid {
		if i, ok := offeredIndexOf(a); ok {
			avoided[i] = true
		}
	}

	canonical := slices.Clone(keys)
	slices.Sort(canonical)
	canonical = slices.Compact(canonical)

	taken := make([]bool, n)
	assigned := make(map[string]int, len(canonical))
	separated := true

	for _, key := range canonical {
		start := int(fnv1a64(key) % uint64(n))
		idx := -1
		for i := 0; i < n; i++ { // first choice: free, and not avoided
			c := (start + i) % n
			if !taken[c] && !avoided[c] {
				idx = c
				break
			}
		}
		if idx < 0 {
			// Nothing outside avoid is left. Separation from the
			// avoided hues is gone whatever happens next, so say so
			// and fall back to separation from the other keys, which
			// is the more valuable of the two.
			separated = false
			for i := 0; i < n; i++ {
				c := (start + i) % n
				if !taken[c] {
					idx = c
					break
				}
			}
		}
		if idx < 0 { // every hue is taken: reuse, deterministically
			separated = false
			idx = start
		}
		taken[idx] = true
		assigned[key] = idx
	}

	for i, key := range keys {
		out[i] = offered[assigned[key]]
	}
	return out, separated
}

// offeredIndexOf finds the offered hue an avoid value names, comparing
// around the circle so 359.7 matches an offered 0.
func offeredIndexOf(hue float64) (int, bool) {
	if math.IsNaN(hue) || math.IsInf(hue, 0) {
		return 0, false
	}
	h := math.Mod(hue, 360)
	if h < 0 {
		h += 360
	}
	for i, o := range offered {
		d := math.Abs(h - o.Hue)
		if d > 180 {
			d = 360 - d
		}
		if d <= hueMatchTolerance {
			return i, true
		}
	}
	return 0, false
}

// fnv1a64 is FNV-1a over the key's bytes. Named in Allocate's comment
// and pinned here because a second implementation of this API has to
// hash identically to agree with this one.
func fnv1a64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// CheckIntents is the offered-set proof, exported so it can be run
// against backgrounds the framework does not know about.
//
// It reports the first-class failure this engine can have: an intent
// that resolves soundly against one background and not against another.
// Docs' canvas is light by default with dark as a PER-PERSON preference,
// so one stored highlight is read on paper white by one reader and on
// dark paper by another, in the same document, at the same moment. An
// intent that works on white and fails on dark paper is not an offered
// intent; it is a trap with a build gate agreeing with it.
//
// The framework proves its own set against paper white and every shipped
// theme surface in both schemes. It cannot prove yours, because it does
// not know your dark paper, your selection tint or your frozen header —
// and a gate built on a guess at those would agree with the guess. So
// pass your real backgrounds:
//
//	if err := ui.CheckIntents(ui.Offered(), myBackgrounds); err != nil {
//	        t.Fatal(err)
//	}
//
// An intent passes a background when Pair resolves it, both floors hold,
// and the RESOLVED chroma is still at least minDeliveredChroma. That last
// one is where an out-of-gamut request surfaces: chroma reduction never
// fails, it just hands back less colour, and a hue that has collapsed
// towards grey at some background has stopped being distinguishable from
// its neighbours there — which would leave a separation guarantee true on
// paper and false on the screen.
//
// A SET also has to pass as a set: every pair of resolved fills must be
// at least MinSeparation apart in OKLab on every background. Twelve hues
// thirty degrees apart are far apart as angles and can still land on top
// of each other once a dark background has squeezed the lightness out of
// them, and a separation guarantee that is true about slots and false
// about pixels is the failure this whole file is arranged to prevent.
// WorstSeparation returns that measurement rather than judging it.
//
// Every failure is reported, not just the first, because a set is fixed
// as a set. The error is nil when there is nothing to say.
func CheckIntents(intents []Intent, backgrounds []string) error {
	var errs []error
	if len(intents) == 0 {
		errs = append(errs, errors.New("ui.CheckIntents: no intents to check — an empty set proves nothing"))
	}
	if len(backgrounds) == 0 {
		errs = append(errs, errors.New("ui.CheckIntents: no backgrounds to check against — an empty set proves nothing"))
	}
	for _, bg := range backgrounds {
		resolved := make([]Swatch, 0, len(intents))
		asked := make([]Intent, 0, len(intents))
		for _, in := range intents {
			sw, err := Pair(in.Hue, in.Chroma, bg)
			if err != nil {
				errs = append(errs, fmt.Errorf("hue %g chroma %g on %s: %w", in.Hue, in.Chroma, bg, err))
				continue
			}
			errs = append(errs, checkSwatch(in, sw)...)
			resolved = append(resolved, sw)
			asked = append(asked, in)
		}
		// The pairwise half. Everything above judges an intent on its
		// own; separation is a property of the set, and a set of
		// individually sound colours can still be a set nobody can tell
		// apart. This is the only place it is measured.
		for i := 0; i < len(resolved); i++ {
			for j := i + 1; j < len(resolved); j++ {
				d, err := DeltaEOK(resolved[i].Fill, resolved[j].Fill)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if d < MinSeparation {
					errs = append(errs, fmt.Errorf("on %s: hue %g resolves to %s and hue %g to %s, which are %.3f apart in OKLab — closer than %.3f, so they are one colour to a reader",
						bg, asked[i].Hue, resolved[i].Fill, asked[j].Hue, resolved[j].Fill, d, MinSeparation))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// WorstSeparation reports the closest pair of fills the intents resolve
// to on any of the backgrounds, and where. It is the number behind the
// question "is twelve hues the right capacity" — CheckIntents enforces a
// floor, this returns the measurement, so a caller weighing a bigger set
// can see how much room is left rather than only whether the floor held.
//
// It returns +Inf and empty strings for fewer than two resolvable
// intents, since there is no pair to measure.
func WorstSeparation(intents []Intent, backgrounds []string) (deltaE float64, background, a, b string) {
	deltaE = math.Inf(1)
	for _, bg := range backgrounds {
		var fills []string
		for _, in := range intents {
			sw, err := Pair(in.Hue, in.Chroma, bg)
			if err != nil {
				continue
			}
			fills = append(fills, sw.Fill)
		}
		for i := 0; i < len(fills); i++ {
			for j := i + 1; j < len(fills); j++ {
				d, err := DeltaEOK(fills[i], fills[j])
				if err != nil || d >= deltaE {
					continue
				}
				deltaE, background, a, b = d, bg, fills[i], fills[j]
			}
		}
	}
	return deltaE, background, a, b
}

// checkSwatch is the half of CheckIntents that judges a resolved swatch,
// split out so it can be handed a swatch that is known to be wrong and
// seen to say so. Pair is not supposed to be able to produce one — these
// are the assertions that would catch it if Pair ever regressed, and an
// assertion nothing has ever been seen to fail is not evidence of
// anything.
func checkSwatch(in Intent, sw Swatch) []error {
	var errs []error
	if sw.FillRatio < ContrastFloorBoundary {
		errs = append(errs, fmt.Errorf("hue %g chroma %g on %s: fill %s is %.2f:1, want >= %.1f:1",
			in.Hue, in.Chroma, sw.Background, sw.Fill, sw.FillRatio, ContrastFloorBoundary))
	}
	if sw.OnRatio < ContrastFloorText {
		errs = append(errs, fmt.Errorf("hue %g chroma %g on %s: on-fill %s is %.2f:1 against fill %s, want >= %.1f:1",
			in.Hue, in.Chroma, sw.Background, sw.On, sw.OnRatio, sw.Fill, ContrastFloorText))
	}
	if sw.Chroma < minDeliveredChroma {
		errs = append(errs, fmt.Errorf("hue %g chroma %g on %s: resolved to chroma %.3f (%s), below %.3f — a grey here, and no longer separable from any other hue",
			in.Hue, in.Chroma, sw.Background, sw.Chroma, sw.Fill, minDeliveredChroma))
	}
	return errs
}

// A Canvas is a background together with every ink that can be drawn on
// it. It is a pair rather than two lists because the two are not
// independent: a light canvas carries near-black theme ink, a dark one
// carries near-white, and crossing them produces combinations that
// cannot occur.
//
// Inks should hold the theme-derived ink for this background AND any ink
// an author can pin. That second half is the one that catches things: an
// author who sets their font colour to black keeps it when a reader
// switches to the dark theme, so "pinned black on dark paper" is a real
// canvas, and it is the one most likely to have no wash.
type Canvas struct {
	Background string
	Inks       []string
}

// CheckWashes is the wash half of the build-time proof, and the sibling
// of CheckIntents: it reports every (intent, background, ink) for which
// Wash has no answer.
//
// Pass the canvases you REQUIRE to work. That is the whole design of it —
// some combinations legitimately have no wash and failing loudly is the
// intended behaviour, so a checker that judged every conceivable
// combination would be reporting decisions rather than defects. A canvas
// in this list is a claim that its cells must resolve.
//
//	err := ui.CheckWashes(ui.Offered(), 0.12, []ui.Canvas{
//	        {Background: paperWhite, Inks: []string{themeInkLight, "#000000"}},
//	        {Background: darkPaper,  Inks: []string{themeInkDark, "#ffffff"}},
//	})
//
// separation is the weight you would ask Wash for; the answer can depend
// on it, so pass the one your app actually uses. It checks the RESOLVED
// COLOURS and not merely that no error came back: the ink clears its
// floor on the fill, the fill clears the perceptibility floor, and On is
// the ink you passed. A wash the constraints forced paler than the
// request is NOT reported — that resolution is correct and is the one to
// paint, and Swatch.SeparationMet is how a caller learns about it.
//
// Every failure is reported rather than the first, because a set is
// adopted as a set.
func CheckWashes(intents []Intent, separation float64, canvases []Canvas) error {
	var errs []error
	if len(intents) == 0 {
		errs = append(errs, errors.New("ui.CheckWashes: no intents to check — an empty set proves nothing"))
	}
	if len(canvases) == 0 {
		errs = append(errs, errors.New("ui.CheckWashes: no canvases to check against — an empty set proves nothing"))
	}
	for _, canvas := range canvases {
		if len(canvas.Inks) == 0 {
			errs = append(errs, fmt.Errorf("ui.CheckWashes: canvas %s lists no inks — a background with no ink on it proves nothing", canvas.Background))
			continue
		}
		for _, ink := range canvas.Inks {
			for _, in := range intents {
				sw, err := Wash(in.Hue, in.Chroma, separation, ink, canvas.Background)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				errs = append(errs, checkWash(in, ink, sw)...)
			}
		}
	}
	return errors.Join(errs...)
}

// checkWash judges a resolved wash, and exists for the reason checkSwatch
// does: a gate that only asks whether an error came back proves that no
// error came back. Remove Wash's perceptibility floor and it will happily
// return a fill four thousandths from the background, with no error, and
// a checker that never looks at the swatch calls that fine.
//
// So the three things a wash promises are asserted on the returned
// colours rather than inferred from the absence of an error. Wash is not
// supposed to be able to break any of them; these are what would catch it
// if it regressed, and an assertion nothing has been seen to fail is not
// evidence of anything.
//
// SeparationMet is deliberately NOT among them. A wash the constraints
// forced paler than the request is correct and is the one to paint; only
// an unreadable or an invisible one is a defect.
func checkWash(in Intent, ink string, sw Swatch) []error {
	var errs []error
	if sw.OnRatio < ContrastFloorText {
		errs = append(errs, fmt.Errorf("hue %g chroma %g on %s under ink %s: the ink is %.2f:1 on fill %s, want >= %.1f:1",
			in.Hue, in.Chroma, sw.Background, ink, sw.OnRatio, sw.Fill, ContrastFloorText))
	}
	if sw.Separation < MinSeparation {
		errs = append(errs, fmt.Errorf("hue %g chroma %g on %s under ink %s: fill %s is ΔE_OK %.4f from the background, under the %.3f floor — nobody could see it had been applied",
			in.Hue, in.Chroma, sw.Background, ink, sw.Fill, sw.Separation, MinSeparation))
	}
	if want, err := normalHex(ink); err == nil && sw.On != want {
		errs = append(errs, fmt.Errorf("hue %g chroma %g on %s: On came back %s but the ink passed in was %s — Wash must never invent a font colour",
			in.Hue, in.Chroma, sw.Background, sw.On, want))
	}
	return errs
}

// ─── OKLCH → sRGB ────────────────────────────────────────────────────
//
// Björn Ottosson's oklab, the space CSS names in "in oklab" and the one
// the themes' own color-mix() calls interpolate in. Written out rather
// than pulled in: this is thirty lines of arithmetic and the framework
// takes no dependency for it.

// oklchToLinear converts OKLCh (lightness 0..1, chroma, hue in degrees)
// to linear-light sRGB, which may fall outside 0..1 — that is what being
// out of gamut looks like, and the caller decides what to do about it.
func oklchToLinear(l, c, hue float64) [3]float64 {
	rad := hue * math.Pi / 180
	return oklabToLinear(l, c*math.Cos(rad), c*math.Sin(rad))
}

// oklabToLinear is the same conversion with the hue already resolved into
// its two rectangular components. It is the one the gamut bisection calls,
// which is why it is separate: the bisection varies only the chroma, so
// the hue's sine and cosine are computed once per colour instead of once
// per iteration.
func oklabToLinear(l, a, b float64) [3]float64 {
	lc := l + 0.3963377774*a + 0.2158037573*b
	mc := l - 0.1055613458*a - 0.0638541728*b
	sc := l - 0.0894841775*a - 1.2914855480*b
	lc, mc, sc = lc*lc*lc, mc*mc*mc, sc*sc*sc

	return [3]float64{
		+4.0767416621*lc - 3.3077115913*mc + 0.2309699292*sc,
		-1.2684380046*lc + 2.6097574011*mc - 0.3413193965*sc,
		-0.0041960863*lc - 0.7034186147*mc + 1.7076147010*sc,
	}
}

// linearToSRGB is the sRGB gamma transfer function.
func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// inGamut reports whether a linear-light triple lands inside sRGB. The
// tolerance absorbs the last bit of floating-point error at the exact
// boundary; it is far below one eight-bit step.
func inGamut(lin [3]float64) bool {
	for _, v := range lin {
		if v < -1e-9 || v > 1+1e-9 {
			return false
		}
	}
	return true
}

// oklchHex renders OKLCh as #rrggbb, reducing chroma until it fits sRGB,
// and returns the chroma it actually used. Lightness and hue are never
// touched: lightness is what the contrast floor is riding on, and hue is
// what the caller meant.
func oklchHex(l, c, hue float64) (string, float64) {
	rgb, delivered := oklchRGB(l, c, hue)
	return hexOf(rgb), delivered
}

// oklchRGB is oklchHex without the string: the search runs it hundreds of
// times per call and throws most of the results away, so the eight-bit
// triple is the currency and a hex is minted only for the one that wins.
func oklchRGB(l, c, hue float64) ([3]uint8, float64) {
	rad := hue * math.Pi / 180
	ca, sa := math.Cos(rad), math.Sin(rad)
	if !inGamut(oklabToLinear(l, c*ca, c*sa)) {
		lo, hi := 0.0, c
		for i := 0; i < 40; i++ {
			mid := (lo + hi) / 2
			if inGamut(oklabToLinear(l, mid*ca, mid*sa)) {
				lo = mid
			} else {
				hi = mid
			}
		}
		c = lo
	}
	lin := oklabToLinear(l, c*ca, c*sa)
	var v [3]uint8
	for i, ch := range lin {
		// Rounding to eight bits is the last step and the only clamp:
		// by here the colour is in gamut, so this guards the rounding
		// rather than mapping anything.
		n := math.Round(linearToSRGB(ch) * 255)
		v[i] = uint8(math.Min(255, math.Max(0, n)))
	}
	return v, c
}

const hexDigits = "0123456789abcdef"

// hexOf renders an eight-bit triple as lower-case #rrggbb.
func hexOf(v [3]uint8) string {
	b := [7]byte{'#'}
	for i, c := range v {
		b[1+i*2] = hexDigits[c>>4]
		b[2+i*2] = hexDigits[c&15]
	}
	return string(b[:])
}

// oklabOf converts eight-bit sRGB to OKLab — the way in, where
// oklchToLinear is the way out. Two uses: asking which candidate fill is
// nearest the background (which wants a perceptual scale rather than a
// luminance one), and measuring how far apart two resolved fills are.
func oklabOf(r, g, b uint8) [3]float64 {
	rl := linearFromSRGB(float64(r) / 255)
	gl := linearFromSRGB(float64(g) / 255)
	bl := linearFromSRGB(float64(b) / 255)
	lc := math.Cbrt(0.4122214708*rl + 0.5363325363*gl + 0.0514459929*bl)
	mc := math.Cbrt(0.2119034982*rl + 0.6806995451*gl + 0.1073969566*bl)
	sc := math.Cbrt(0.0883024619*rl + 0.2817188376*gl + 0.6299787005*bl)
	return [3]float64{
		0.2104542553*lc + 0.7936177850*mc - 0.0040720468*sc,
		1.9779984951*lc - 2.4285922050*mc + 0.4505937099*sc,
		0.0259040371*lc + 0.7827717662*mc - 0.8086757660*sc,
	}
}

// MinSeparation is the least perceptual distance two fills may sit at and
// still be told apart: 0.03 in OKLab, which is a little over one
// just-noticeable difference (about 0.02 for a large flat patch).
//
// It exists because "twelve hues thirty degrees apart" is a statement
// about intents and not about colours. Two intents that are far apart as
// angles can resolve within a hair of each other once a background has
// forced them both to a low lightness, where there is no room left to be
// saturated in — and at that point Allocate's separation guarantee is
// true about slots and false about the screen. The shipped set's worst
// pair is 0.045, on day's dark page, so the floor is not fitted to pass.
//
// Exported for the same reason the contrast floors are: an app checking
// its own intents against its own canvas should hold the same bar.
const MinSeparation = 0.03

// DeltaEOK is the perceptual distance between two colours: plain
// euclidean distance in OKLab, which is what OKLab is for. Roughly, 0.02
// is the point two large flat patches stop being reliably tellable
// apart, and 0.1 is comfortably different.
//
// Exported because a caller mapping an arbitrary colour onto the offered
// set — an imported XLSX fill, a pasted highlight — needs a perceptual
// nearest-match, and re-deriving one from a hue angle gets it wrong at
// exactly the low lightnesses where it matters.
func DeltaEOK(a, b string) (float64, error) {
	ar, ag, ab, err := parseHex(a)
	if err != nil {
		return 0, fmt.Errorf("first colour: %w", err)
	}
	br, bg, bb, err := parseHex(b)
	if err != nil {
		return 0, fmt.Errorf("second colour: %w", err)
	}
	return labDistance(oklabOf(ar, ag, ab), oklabOf(br, bg, bb)), nil
}

// labDistance is DeltaEOK's kernel, for callers inside this package that
// already hold the OKLab triples. One notion of perceptual distance, one
// place it is written down: the separation check over an offered set, a
// swatch's Separation, and the wash floor are all this function.
func labDistance(a, b [3]float64) float64 {
	dl, da, db := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return math.Sqrt(dl*dl + da*da + db*db)
}

// linearFromSRGB is the inverse gamma transfer, at the sRGB spec's own
// 0.04045 breakpoint. It is not srgbToLinear below, which uses WCAG 2.x's
// 0.03928 — the two differ in the fourth decimal place of one threshold
// and are kept apart deliberately, so the WCAG numbers this file publishes
// are the numbers WCAG's formula gives.
func linearFromSRGB(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// ─── WCAG arithmetic ─────────────────────────────────────────────────
//
// This used to live in contrast_test.go and be reachable only from a
// test. It is shipped code now because the colour engine is built on it
// and because a caller writing its own gate should hold its own colours
// to this arithmetic rather than to a second copy of it. The theme
// contrast gate calls these, so there is exactly one implementation and
// TestContrastMathMatchesDocumentedDangerFillRatios calibrates the
// shipped one.

// parseHex reads a #rgb or #rrggbb literal into eight-bit channels.
// Anything else — var(), color-mix(), rgba(), a bare colour name, a
// value with whitespace around it — is an error rather than a guess,
// because a colour this package cannot read is a colour it must not
// silently substitute something for.
func parseHex(s string) (r, g, b uint8, err error) {
	// Hand-decoded rather than matched against a regexp: ContrastRatio
	// is exported API that two apps will call in loops, and a regexp
	// plus three Sscanf calls cost thirty times the arithmetic they
	// feed. The accepted grammar is unchanged — #rgb or #rrggbb, no
	// whitespace, nothing else.
	var v [3]uint8
	switch len(s) {
	case 4:
		if s[0] != '#' {
			return 0, 0, 0, notHex(s)
		}
		for i := 0; i < 3; i++ {
			n, ok := nibble(s[1+i])
			if !ok {
				return 0, 0, 0, notHex(s)
			}
			v[i] = n<<4 | n
		}
	case 7:
		if s[0] != '#' {
			return 0, 0, 0, notHex(s)
		}
		for i := 0; i < 3; i++ {
			hi, okHi := nibble(s[1+i*2])
			lo, okLo := nibble(s[2+i*2])
			if !okHi || !okLo {
				return 0, 0, 0, notHex(s)
			}
			v[i] = hi<<4 | lo
		}
	default:
		return 0, 0, 0, notHex(s)
	}
	return v[0], v[1], v[2], nil
}

func notHex(s string) error {
	return fmt.Errorf("not a #rgb/#rrggbb literal: %q", s)
}

func nibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// normalHex parses and re-renders a hex colour as lower-case #rrggbb, so
// a Swatch reports one spelling of a background however it was written.
func normalHex(s string) (string, error) {
	r, g, b, err := parseHex(s)
	if err != nil {
		return "", err
	}
	return hexOf([3]uint8{r, g, b}), nil
}

// srgbToLinear converts one sRGB channel (0..1) to its linearized form —
// the standard WCAG 2.x formula (also used by the WCAG 2.2 spec this
// project targets).
func srgbToLinear(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// srgbLinearTable is srgbToLinear for all 256 eight-bit values. Every
// colour this package measures has already been rounded to eight bits, so
// there are only 256 answers and a table is the whole domain rather than
// a cache of part of it. Built from srgbToLinear itself, so the two
// cannot say different things.
var srgbLinearTable = func() (t [256]float64) {
	for i := range t {
		t[i] = srgbToLinear(float64(i) / 255)
	}
	return t
}()

// relLuminance is the WCAG relative luminance of an sRGB colour.
func relLuminance(r, g, b uint8) float64 {
	return 0.2126*srgbLinearTable[r] + 0.7152*srgbLinearTable[g] + 0.0722*srgbLinearTable[b]
}

// ratioOf is the WCAG ratio between two relative luminances. Split out so
// the search can compare luminances it already has instead of re-parsing
// two hex strings, and so there is still exactly one place the formula is
// written down.
func ratioOf(a, b float64) float64 {
	if b > a {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// ContrastRatio is the WCAG contrast ratio between two hex colours:
// (lighter + 0.05) / (darker + 0.05) over relative luminance. It is
// symmetric — there is no foreground and background here, only two
// colours — and a colour against itself is exactly 1.
//
// Exported for the same reason the floors are: an app gating its own
// colours should measure them with the framework's arithmetic, not a
// re-typed one that rounds differently at the boundary.
func ContrastRatio(a, b string) (float64, error) {
	ar, ag, ab, err := parseHex(a)
	if err != nil {
		return 0, fmt.Errorf("first colour: %w", err)
	}
	br, bg, bb, err := parseHex(b)
	if err != nil {
		return 0, fmt.Errorf("second colour: %w", err)
	}
	return ratioOf(relLuminance(ar, ag, ab), relLuminance(br, bg, bb)), nil
}
