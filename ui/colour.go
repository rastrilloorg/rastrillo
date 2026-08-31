package ui

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"slices"
	"sort"
)

// This file is rastrillo's colour engine (design doc §6-v2.2b): the two
// entry points a caller needs to paint something in a colour it did not
// have to hand-verify, plus the WCAG arithmetic they are built on.
//
//   - Pair resolves one intent against one background into a fill and
//     the colour to draw on it, contrast-correct by construction.
//   - Allocate hands a set of opaque keys mutually distinguishable hues.
//
// They are two functions and not one on purpose. Contrast-correctness
// and mutual distinguishability are different guarantees; a single
// function that delivers whichever one its arguments imply is the shape
// of API nobody can gate.
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
type Swatch struct {
	Fill       string
	On         string
	Background string

	Hue    float64
	Chroma float64

	// FillRatio is Fill against Background, held at
	// ContrastFloorBoundary. OnRatio is On against Fill, held at
	// ContrastFloorText.
	FillRatio float64
	OnRatio   float64
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
// body text — a fill three per cent away from the page that leaves the
// author's own ink alone. That is a different guarantee (the text keeps
// its own contrast; the fill has none) and asking one function for both
// is what the two-entry-point split exists to prevent. If you want a
// wash, mix your own and gate the text against it.
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

	bgL := oklabLightness(bg)
	var best Swatch
	var found bool
	var bestDist float64

	for step := 0; step <= lightnessSteps; step++ {
		l := lightnessLow + (lightnessHigh-lightnessLow)*float64(step)/float64(lightnessSteps)
		fill, c := oklchHex(l, chroma, h)
		fillRatio, err := ContrastRatio(fill, bg)
		if err != nil {
			return Swatch{}, err
		}
		if fillRatio < ContrastFloorBoundary+contrastMargin {
			continue
		}
		on, onRatio, ok := ink(fill, h, chroma)
		if !ok {
			continue
		}
		dist := math.Abs(l - bgL)
		if found && dist >= bestDist {
			continue
		}
		found, bestDist = true, dist
		best = Swatch{
			Fill:       fill,
			On:         on,
			Background: bg,
			Hue:        h,
			Chroma:     c,
			FillRatio:  fillRatio,
			OnRatio:    onRatio,
		}
	}
	if !found {
		return Swatch{}, fmt.Errorf("ui.Pair: no lightness resolves hue %g chroma %g against background %s at %.1f:1 fill and %.1f:1 on-fill",
			h, chroma, bg, ContrastFloorBoundary, ContrastFloorText)
	}
	return best, nil
}

// The lightness search. A fixed grid rather than a solve, because the
// feasible band is not always contiguous once gamut reduction and
// eight-bit rounding are in it, and because a grid is a thing a reader
// can check by hand. The step is fine enough that the chosen fill sits
// within about a quarter of a per cent of the quietest one available.
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
func ink(fill string, hue, chroma float64) (string, float64, bool) {
	type candidate struct{ l, maxC float64 }
	best, bestRatio, ok := "", 0.0, false
	for _, c := range []candidate{{0.99, 0.03}, {0.20, 0.05}} {
		hex, _ := oklchHex(c.l, math.Min(chroma, c.maxC), hue)
		ratio, err := ContrastRatio(hex, fill)
		if err != nil {
			continue
		}
		if ratio < ContrastFloorText+contrastMargin {
			continue
		}
		if !ok || ratio > bestRatio {
			best, bestRatio, ok = hex, ratio, true
		}
	}
	return best, bestRatio, ok
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
	sort.Strings(canonical)
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
	for _, in := range intents {
		for _, bg := range backgrounds {
			sw, err := Pair(in.Hue, in.Chroma, bg)
			if err != nil {
				errs = append(errs, fmt.Errorf("hue %g chroma %g on %s: %w", in.Hue, in.Chroma, bg, err))
				continue
			}
			errs = append(errs, checkSwatch(in, sw)...)
		}
	}
	return errors.Join(errs...)
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
	a, b := c*math.Cos(rad), c*math.Sin(rad)

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
	if !inGamut(oklchToLinear(l, c, hue)) {
		lo, hi := 0.0, c
		for i := 0; i < 40; i++ {
			mid := (lo + hi) / 2
			if inGamut(oklchToLinear(l, mid, hue)) {
				lo = mid
			} else {
				hi = mid
			}
		}
		c = lo
	}
	lin := oklchToLinear(l, c, hue)
	var v [3]uint8
	for i, ch := range lin {
		// Rounding to eight bits is the last step and the only clamp:
		// by here the colour is in gamut, so this guards the rounding
		// rather than mapping anything.
		n := math.Round(linearToSRGB(ch) * 255)
		v[i] = uint8(math.Min(255, math.Max(0, n)))
	}
	return fmt.Sprintf("#%02x%02x%02x", v[0], v[1], v[2]), c
}

// oklabLightness is the oklab L of a hex colour — used only to ask which
// candidate fill is nearest the background, which wants a perceptual
// scale rather than a luminance one.
func oklabLightness(hex string) float64 {
	r, g, b, err := parseHex(hex)
	if err != nil {
		return 0
	}
	rl := linearFromSRGB(float64(r) / 255)
	gl := linearFromSRGB(float64(g) / 255)
	bl := linearFromSRGB(float64(b) / 255)
	lc := math.Cbrt(0.4122214708*rl + 0.5363325363*gl + 0.0514459929*bl)
	mc := math.Cbrt(0.2119034982*rl + 0.6806995451*gl + 0.1073969566*bl)
	sc := math.Cbrt(0.0883024619*rl + 0.2817188376*gl + 0.6299787005*bl)
	return 0.2104542553*lc + 0.7936177850*mc - 0.0040720468*sc
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

// hexPattern matches a bare #rgb or #rrggbb value, the only colour
// syntax this file reads.
var hexPattern = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// parseHex reads a #rgb or #rrggbb literal. Anything else (var(),
// color-mix(), rgba(), a bare name) is reported so a caller can route it
// through colorMixSkip instead of guessing.
func parseHex(s string) (r, g, b uint8, err error) {
	if !hexPattern.MatchString(s) {
		return 0, 0, 0, fmt.Errorf("not a #rgb/#rrggbb literal: %q", s)
	}
	h := s[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		n, err := parseHexByte(h[i*2 : i*2+2])
		if err != nil {
			return 0, 0, 0, err
		}
		v[i] = n
	}
	return v[0], v[1], v[2], nil
}

func parseHexByte(s string) (uint8, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%02x", &n); err != nil {
		return 0, err
	}
	return uint8(n), nil
}

// normalHex parses and re-renders a hex colour as lower-case #rrggbb, so
// a Swatch reports one spelling of a background however it was written.
func normalHex(s string) (string, error) {
	r, g, b, err := parseHex(s)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b), nil
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

// relLuminance is the WCAG relative luminance of an sRGB colour.
func relLuminance(r, g, b uint8) float64 {
	rl := srgbToLinear(float64(r) / 255)
	gl := srgbToLinear(float64(g) / 255)
	bl := srgbToLinear(float64(b) / 255)
	return 0.2126*rl + 0.7152*gl + 0.0722*bl
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
	la := relLuminance(ar, ag, ab)
	lb := relLuminance(br, bg, bb)
	lighter, darker := la, lb
	if lb > la {
		lighter, darker = lb, la
	}
	return (lighter + 0.05) / (darker + 0.05), nil
}
