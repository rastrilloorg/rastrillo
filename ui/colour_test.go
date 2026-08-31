package ui

import (
	"math"
	"math/rand"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The gates over the colour engine (colour.go, design doc §6-v2.2b).
//
// Every check in this file follows §7-v2: run the check against a case
// whose answer you already know, and see whether it says so. Mutation
// shows a gate CAN fail; a control shows it is measuring. So each gate
// here comes in two halves — the real assertion, and a planted case with
// a known answer — and the halves are named so a reader can see when one
// goes missing.

// ─── The floors themselves ───────────────────────────────────────────

// TestFloorConstantsAreTheStandard pins the two floors to the numbers
// WCAG 2.2 AA publishes, as literals.
//
// This is not pedantry, it is the gap that made every other contrast
// assertion in this file self-referential. A test that asserts
// "sw.FillRatio >= ContrastFloorBoundary" measures the engine against
// whatever the constant currently says: lower the constant and the
// assertion lowers with it, and the gate reports green on a palette that
// fails WCAG. The bar has to be written down somewhere that a mutation of
// the bar cannot move. Here it is.
//
// minDeliveredChroma and MinSeparation are pinned for a different reason:
// they are this project's own policy rather than anybody's standard, so
// the literal is there to make a change to them a visible edit with a
// number in the diff.
func TestFloorConstantsAreTheStandard(t *testing.T) {
	if ContrastFloorText != 4.5 {
		t.Errorf("ContrastFloorText = %v, want 4.5 (WCAG 2.2 AA, 1.4.3 normal text)", ContrastFloorText)
	}
	if ContrastFloorBoundary != 3.0 {
		t.Errorf("ContrastFloorBoundary = %v, want 3.0 (WCAG 2.2 AA, 1.4.11 non-text contrast)", ContrastFloorBoundary)
	}
	if minDeliveredChroma != 0.035 {
		t.Errorf("minDeliveredChroma = %v, want 0.035 — changing it changes which intents are admissible", minDeliveredChroma)
	}
	if MinSeparation != 0.03 {
		t.Errorf("MinSeparation = %v, want 0.03 — changing it changes what \"distinguishable\" means", MinSeparation)
	}
}

// ─── ContrastRatio ───────────────────────────────────────────────────

// TestContrastRatioControls is the control for the arithmetic the whole
// design system rests on. Three cases whose answers are not opinions:
// a colour against itself is exactly 1, black against white is exactly
// 21, and the ratio does not care which colour is named first.
//
// TestContrastMathMatchesDocumentedDangerFillRatios in contrast_test.go
// is the other half — two hand-verified ratios from tokens.css. Together
// they say the formula is both self-consistent and correctly calibrated
// against a number a person computed by hand.
func TestContrastRatioControls(t *testing.T) {
	for _, hex := range []string{"#000000", "#ffffff", "#b91c1c", "#7f7f7f", "#123456"} {
		got, err := ContrastRatio(hex, hex)
		if err != nil {
			t.Fatalf("ContrastRatio(%s, %s): %v", hex, hex, err)
		}
		if got != 1.0 {
			t.Errorf("ContrastRatio(%s, %s) = %v, want exactly 1 — a colour compared with itself", hex, hex, got)
		}
	}

	got, err := ContrastRatio("#000000", "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-21) > 1e-9 {
		t.Errorf("ContrastRatio(black, white) = %v, want 21", got)
	}

	fwd, _ := ContrastRatio("#b91c1c", "#ffffff")
	rev, _ := ContrastRatio("#ffffff", "#b91c1c")
	if fwd != rev {
		t.Errorf("ContrastRatio is not symmetric: %v vs %v", fwd, rev)
	}

	// Short hex is the same colour as its long form, and a value that is
	// not a hex literal is an error rather than a silent zero.
	short, _ := ContrastRatio("#fff", "#000")
	if short != got {
		t.Errorf("ContrastRatio(#fff, #000) = %v, want the same as #ffffff/#000000 (%v)", short, got)
	}
	for _, bad := range []string{"white", "var(--rst-bg)", "rgba(0,0,0,0.5)", "#ff", "", "#12345"} {
		if _, err := ContrastRatio(bad, "#ffffff"); err == nil {
			t.Errorf("ContrastRatio(%q, …) returned no error; a value this file cannot read must say so", bad)
		}
	}
}

// ─── OKLCH → sRGB ────────────────────────────────────────────────────

// TestOklchHexControls checks the colour conversion against cases whose
// answers are known without running it: the achromatic axis, which must
// come back grey (all three channels equal) at every lightness, and the
// two ends, which must come back black and white.
//
// It then pins the out-of-gamut decision. Asking for a chroma sRGB
// cannot hold must come back with a REDUCED chroma at the SAME hue —
// desaturated, never hue-shifted — which is the whole argument for
// reducing chroma instead of clamping channels.
func TestOklchHexControls(t *testing.T) {
	for _, l := range []float64{0.1, 0.25, 0.5, 0.75, 0.9} {
		hex, c := oklchHex(l, 0, 210)
		if c != 0 {
			t.Errorf("oklchHex(%v, 0, 210) delivered chroma %v, want 0", l, c)
		}
		r, g, b, err := parseHex(hex)
		if err != nil {
			t.Fatalf("oklchHex returned an unparseable colour %q: %v", hex, err)
		}
		if r != g || g != b {
			t.Errorf("oklchHex(%v, 0, 210) = %s, want a grey (chroma 0 is the achromatic axis)", l, hex)
		}
	}
	if hex, _ := oklchHex(0, 0, 0); hex != "#000000" {
		t.Errorf("oklchHex(0, 0, 0) = %s, want #000000", hex)
	}
	if hex, _ := oklchHex(1, 0, 0); hex != "#ffffff" {
		t.Errorf("oklchHex(1, 0, 0) = %s, want #ffffff", hex)
	}

	// 0.4 chroma at mid lightness is outside sRGB at every hue.
	for _, hue := range []float64{25, 145, 265, 325} {
		hex, c := oklchHex(0.6, 0.4, hue)
		if c >= 0.4 {
			t.Errorf("oklchHex(0.6, 0.4, %v) claimed chroma %v — 0.4 is outside sRGB and must be reduced", hue, c)
		}
		if c <= 0 {
			t.Errorf("oklchHex(0.6, 0.4, %v) reduced chroma to %v — it should find the largest chroma that fits, not give up", hue, c)
		}
		// The reduced colour must still be the hue that was asked for.
		// A clamp would move it; a chroma reduction cannot.
		if got := hueOf(t, hex); angleDiff(got, hue) > 2.0 {
			t.Errorf("oklchHex(0.6, 0.4, %v) = %s, whose hue is %.1f — gamut mapping moved the hue, which is what clamping does and reducing chroma must not", hue, hex, got)
		}
	}
}

// hueOf reads the OKLCh hue back out of a rendered hex, so a test can
// ask whether gamut mapping moved it. Deliberately a separate route
// through the maths from oklchHex's own: it goes sRGB → oklab → angle
// rather than reusing anything on the way out.
func hueOf(t *testing.T, hex string) float64 {
	t.Helper()
	r, g, b, err := parseHex(hex)
	if err != nil {
		t.Fatalf("hueOf(%q): %v", hex, err)
	}
	rl := linearFromSRGB(float64(r) / 255)
	gl := linearFromSRGB(float64(g) / 255)
	bl := linearFromSRGB(float64(b) / 255)
	lc := math.Cbrt(0.4122214708*rl + 0.5363325363*gl + 0.0514459929*bl)
	mc := math.Cbrt(0.2119034982*rl + 0.6806995451*gl + 0.1073969566*bl)
	sc := math.Cbrt(0.0883024619*rl + 0.2817188376*gl + 0.6299787005*bl)
	a := 1.9779984951*lc - 2.4285922050*mc + 0.4505937099*sc
	bb := 0.0259040371*lc + 0.7827717662*mc - 0.8086757660*sc
	deg := math.Atan2(bb, a) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}
	return deg
}

func angleDiff(a, b float64) float64 {
	d := math.Abs(math.Mod(a-b+360, 360))
	if d > 180 {
		d = 360 - d
	}
	return d
}

// ─── Pair ────────────────────────────────────────────────────────────

// TestPairAgainstBackgroundsWithAnObviousAnswer is the control for the
// resolver. Two backgrounds whose right answer needs no computation:
//
//   - On paper white the fill has to move away from white to be seen at
//     all, and a fill that is 3:1 from white sits at a luminance where
//     white ink cannot reach 4.5:1 — so the on-fill must come back DARK.
//   - On near-black the fill moves the other way, and the on-fill must
//     come back LIGHT.
//
// If the resolver ever stops reading the background — hard-codes a
// lightness, ignores the parameter, returns the same swatch twice — this
// is the check that notices, because the two answers are opposites.
func TestPairAgainstBackgroundsWithAnObviousAnswer(t *testing.T) {
	white, err := Pair(265, 0.14, "#ffffff")
	if err != nil {
		t.Fatalf("Pair on white: %v", err)
	}
	if lum(t, white.On) >= lum(t, white.Fill) {
		t.Errorf("on paper white: fill %s, on-fill %s — the on-fill must be the darker of the two", white.Fill, white.On)
	}
	if l := lum(t, white.On); l > 0.1 {
		t.Errorf("on paper white: on-fill %s has luminance %.3f, want a dark ink", white.On, l)
	}

	black, err := Pair(265, 0.14, "#0b0d10")
	if err != nil {
		t.Fatalf("Pair on near-black: %v", err)
	}
	if lum(t, black.On) <= lum(t, black.Fill) {
		t.Errorf("on near-black: fill %s, on-fill %s — the on-fill must be the lighter of the two", black.Fill, black.On)
	}
	if l := lum(t, black.On); l < 0.7 {
		t.Errorf("on near-black: on-fill %s has luminance %.3f, want a light ink", black.On, l)
	}

	if white.Fill == black.Fill {
		t.Errorf("the same intent resolved to %s on both paper white and near-black — background is not being read", white.Fill)
	}

	// Both swatches say what they measured, and they are telling the
	// truth: recompute the ratios from the hexes they returned.
	for _, sw := range []Swatch{white, black} {
		fr, err := ContrastRatio(sw.Fill, sw.Background)
		if err != nil {
			t.Fatal(err)
		}
		or, err := ContrastRatio(sw.On, sw.Fill)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(fr-sw.FillRatio) > 1e-9 || math.Abs(or-sw.OnRatio) > 1e-9 {
			t.Errorf("%+v reports ratios (%.4f, %.4f) but its own colours measure (%.4f, %.4f)", sw, sw.FillRatio, sw.OnRatio, fr, or)
		}
		if fr < 3.0 { // a literal, for the reason given in TestFloorConstantsAreTheStandard
			t.Errorf("fill %s on %s = %.2f:1, want >= 3.0:1", sw.Fill, sw.Background, fr)
		}
		if or < 4.5 {
			t.Errorf("on-fill %s on fill %s = %.2f:1, want >= 4.5:1", sw.On, sw.Fill, or)
		}
	}
}

func lum(t *testing.T, hex string) float64 {
	t.Helper()
	r, g, b, err := parseHex(hex)
	if err != nil {
		t.Fatalf("lum(%q): %v", hex, err)
	}
	return relLuminance(r, g, b)
}

// TestPairHoldsItsFloorsEverywhere sweeps hue, chroma and background
// together. The claim under test is "by construction", which is a claim
// about every input and not about a chosen one.
//
// It walks the whole grey ramp as backgrounds, including the middle of
// it, where the feasible band is narrowest.
func TestPairHoldsItsFloorsEverywhere(t *testing.T) {
	var backgrounds []string
	for v := 0; v <= 255; v += 15 {
		backgrounds = append(backgrounds, greyHex(v))
	}
	backgrounds = append(backgrounds, "#ffffff", "#0b0d10", "#b91c1c", "#eef3fe")

	for _, bg := range backgrounds {
		for hue := 0.0; hue < 360; hue += 15 {
			for _, chroma := range []float64{0, 0.05, 0.14, 0.25} {
				sw, err := Pair(hue, chroma, bg)
				if err != nil {
					t.Errorf("Pair(%v, %v, %s): %v", hue, chroma, bg, err)
					continue
				}
				// The floors here are LITERALS, deliberately, not
				// ContrastFloorBoundary and ContrastFloorText. Against
				// the constants this sweep would only prove the engine
				// agrees with itself, and would pass unchanged if the
				// constants were lowered to 1.05.
				if sw.FillRatio < 3.0 {
					t.Errorf("Pair(%v, %v, %s): fill %s = %.3f:1, want >= 3.0:1 (WCAG 1.4.11)", hue, chroma, bg, sw.Fill, sw.FillRatio)
				}
				if sw.OnRatio < 4.5 {
					t.Errorf("Pair(%v, %v, %s): on-fill %s on fill %s = %.3f:1, want >= 4.5:1 (WCAG 1.4.3)", hue, chroma, bg, sw.On, sw.Fill, sw.OnRatio)
				}
				if sw.Chroma > chroma {
					t.Errorf("Pair(%v, %v, %s): delivered chroma %v, more than was asked for", hue, chroma, bg, sw.Chroma)
				}
			}
		}
	}
}

func greyHex(v int) string {
	const digits = "0123456789abcdef"
	c := string([]byte{digits[v>>4], digits[v&15]})
	return "#" + c + c + c
}

// TestPairRejectsWhatItCannotResolve is the other half of the resolver's
// gate: the inputs that must NOT come back as a swatch. A background it
// cannot read is the important one — it is the difference between a
// caller being told they passed a theme token name and a caller getting
// a confident pair resolved against nothing.
func TestPairRejectsWhatItCannotResolve(t *testing.T) {
	for _, bad := range []string{"var(--rst-bg)", "white", "rgba(255,255,255,1)", "", "#ggg", "dark"} {
		if _, err := Pair(200, 0.14, bad); err == nil {
			t.Errorf("Pair(…, %q) returned a swatch; a background this engine cannot read must be an error", bad)
		}
	}
	for _, chroma := range []float64{-0.1, math.NaN(), math.Inf(1)} {
		if _, err := Pair(200, chroma, "#ffffff"); err == nil {
			t.Errorf("Pair(200, %v, white) returned a swatch; chroma must be finite and >= 0", chroma)
		}
	}
	if _, err := Pair(math.NaN(), 0.14, "#ffffff"); err == nil {
		t.Error("Pair(NaN, …) returned a swatch; hue must be finite")
	}

	// A hue outside 0..360 is normalised rather than rejected: -95 and
	// 265 are the same angle and must give the same colours.
	a, err := Pair(-95, 0.14, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Pair(265, 0.14, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if a.Fill != b.Fill || a.On != b.On {
		t.Errorf("Pair(-95, …) = %s/%s but Pair(265, …) = %s/%s; the two are the same angle", a.Fill, a.On, b.Fill, b.On)
	}
	if a.Hue != 265 {
		t.Errorf("Pair(-95, …) reports hue %v, want it normalised to 265", a.Hue)
	}
}

// ─── Allocate ────────────────────────────────────────────────────────

// TestAllocateSeparatesUpToCapacity walks the two sides of the boundary:
// exactly capacity many keys must all get different hues, and one more
// than capacity must not — and must SAY so.
//
// The false half is the point. A flag that has never been observed false
// is not evidence that it can be.
func TestAllocateSeparatesUpToCapacity(t *testing.T) {
	n := len(Offered())

	keys := make([]string, n)
	for i := range keys {
		keys[i] = "key-" + string(rune('a'+i))
	}
	intents, separated := Allocate(keys, nil)
	if !separated {
		t.Errorf("Allocate reported separated=false for %d keys, which is exactly the offered set's capacity", n)
	}
	if got := distinctHues(intents); got != n {
		t.Errorf("Allocate gave %d distinct hues to %d keys; every one should be its own", got, n)
	}

	over := append(slices.Clone(keys), "one-too-many")
	intents, separated = Allocate(over, nil)
	if separated {
		t.Errorf("Allocate reported separated=true for %d keys with a capacity of %d — past capacity it must say separation is gone", len(over), n)
	}
	if len(intents) != len(over) {
		t.Fatalf("Allocate returned %d intents for %d keys", len(intents), len(over))
	}
	if got := distinctHues(intents); got != n {
		t.Errorf("past capacity Allocate used %d of %d hues; it should still use all of them before repeating", got, n)
	}

	// And the flag is not just a length comparison: exhausting the set
	// through avoid rather than through keys must report it too.
	all := make([]float64, 0, n)
	for _, in := range Offered() {
		all = append(all, in.Hue)
	}
	_, separated = Allocate([]string{"a", "b"}, all)
	if separated {
		t.Error("Allocate reported separated=true with every offered hue in avoid; there was nothing left to be separate from")
	}
}

func distinctHues(intents []Intent) int {
	seen := map[float64]bool{}
	for _, in := range intents {
		seen[in.Hue] = true
	}
	return len(seen)
}

// TestAllocateIsDeterministicUnderShuffle is the determinism gate, and
// it is deliberately not "call it twice with the same slice" — that is
// one measurement taken twice, which is exactly the mistake §7-v2 was
// written about. The same key SET is presented in many different orders
// and every one must produce the same key→hue map.
//
// This is the property two clients rendering one document depend on. If
// it fails they disagree about who is which colour, which is worse than
// no colour at all.
func TestAllocateIsDeterministicUnderShuffle(t *testing.T) {
	base := []string{
		"member-7", "member-3", "member-11", "guest-row-902", "member-1",
		"aaa", "zzz", "", "member-3 ", "Member-7", "member-70",
	}
	want := allocationMap(t, base, nil)

	rng := rand.New(rand.NewSource(20260831))
	for trial := 0; trial < 200; trial++ {
		shuffled := slices.Clone(base)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := allocationMap(t, shuffled, nil)
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("trial %d: key %q got hue %v in order %v, but %v in the reference order — allocation depends on arrival order",
					trial, k, got[k], shuffled, v)
			}
		}
	}

	// The control, and it has to be one that must fire. Order-invariance
	// on its own is a weak property: a constant allocator, handing every
	// key the same hue, is perfectly order-invariant and would sail
	// through the 200 shuffles above. So the harness is shown detecting a
	// difference it is supposed to detect.
	//
	// The case is chosen because its answer is known without running
	// anything. "Member-7" and "member-3" both prefer slot 11, and
	// "Member-7" sorts first ('M' is 0x4D, 'm' is 0x6D). Alone,
	// "member-3" keeps slot 11, at 355°. With "Member-7" beside it,
	// "Member-7" keeps 355° and "member-3" is displaced — it probes past
	// the end of the set and wraps to slot 0, at 25°. A harness that
	// cannot see that is not reading the keys.
	alone := allocationMap(t, []string{"member-3"}, nil)
	if alone["member-3"] != 355 {
		t.Errorf("\"member-3\" alone got hue %v, want 355 (its preferred slot)", alone["member-3"])
	}
	displaced := allocationMap(t, []string{"member-3", "Member-7"}, nil)
	if displaced["member-3"] != 25 {
		t.Errorf("\"member-3\" beside \"Member-7\" got hue %v, want 25 — it must be displaced, and it must wrap", displaced["member-3"])
	}
	if displaced["Member-7"] != 355 {
		t.Errorf("\"Member-7\" got hue %v, want 355 — the key that sorts EARLIER keeps its preferred hue", displaced["Member-7"])
	}
	if alone["member-3"] == displaced["member-3"] {
		t.Error("adding a colliding key changed nothing; this harness cannot see a difference and its 200 shuffles prove nothing")
	}
}

func allocationMap(t *testing.T, keys []string, avoid []float64) map[string]float64 {
	t.Helper()
	intents, _ := Allocate(keys, avoid)
	if len(intents) != len(keys) {
		t.Fatalf("Allocate returned %d intents for %d keys", len(intents), len(keys))
	}
	out := make(map[string]float64, len(keys))
	for i, k := range keys {
		if prev, ok := out[k]; ok && prev != intents[i].Hue {
			t.Fatalf("duplicate key %q got two different hues (%v, %v) in one allocation", k, prev, intents[i].Hue)
		}
		out[k] = intents[i].Hue
	}
	return out
}

// TestAllocateHonoursAvoid checks the avoid set, and checks the one
// thing about it that is easy to get wrong: avoid is applied INSIDE the
// probe, so an avoided hue displaces a key exactly the way a taken one
// does. Filtering the set first and probing the remainder gives a
// different — and therefore incompatible — answer for the same inputs.
func TestAllocateHonoursAvoid(t *testing.T) {
	all := Offered()
	keys := []string{"a", "b", "c", "d"}

	// Avoid every hue but three. Four keys into three hues cannot
	// separate, and the flag has to say so, but the three that fit must
	// all land outside the avoided set.
	var avoid []float64
	keep := map[float64]bool{all[0].Hue: true, all[5].Hue: true, all[9].Hue: true}
	for _, in := range all {
		if !keep[in.Hue] {
			avoid = append(avoid, in.Hue)
		}
	}
	intents, separated := Allocate(keys, avoid)
	if separated {
		t.Errorf("4 keys into 3 permitted hues reported separated=true")
	}
	inKept := 0
	for _, in := range intents {
		if keep[in.Hue] {
			inKept++
		}
	}
	if inKept < 3 {
		t.Errorf("only %d of 4 keys landed on one of the 3 permitted hues: %v", inKept, intents)
	}

	// Three keys into the same three permitted hues: all inside avoid's
	// complement, all distinct, and the flag true.
	intents, separated = Allocate([]string{"a", "b", "c"}, avoid)
	if !separated {
		t.Errorf("3 keys into 3 permitted hues reported separated=false")
	}
	for _, in := range intents {
		if !keep[in.Hue] {
			t.Errorf("key landed on hue %v, which was in avoid", in.Hue)
		}
	}

	// An avoid value that names no offered hue changes nothing, and one
	// that names a hue the long way round the circle still matches.
	ref := allocationMap(t, keys, nil)
	if got := allocationMap(t, keys, []float64{7.5, 123.456, -999}); !sameMap(got, ref) {
		t.Errorf("avoiding hues that are not in the offered set changed the allocation: %v vs %v", got, ref)
	}
	if _, ok := offeredIndexOf(all[0].Hue - 360); !ok {
		t.Errorf("offeredIndexOf did not match %v as %v", all[0].Hue-360, all[0].Hue)
	}
}

func sameMap(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestAllocateStabilityIsBestEffort pins the documented shape of the
// stability promise, in both directions, with no branch that can skip an
// assertion. A key that nothing displaces keeps its hue in a different
// document; a key that something does displace moves. Both halves are
// documented behaviour, so both are asserted — the second so nobody reads
// the first as a guarantee it is not.
//
// The keys are chosen rather than mined, so the expected answer is known
// in advance rather than read off the run: "Member-7" and "member-3" both
// prefer slot 11 and "Member-7" sorts first. See the golden table in
// colour_golden_test.go, which pins the same two rows as data.
func TestAllocateStabilityIsBestEffort(t *testing.T) {
	const early, late = "Member-7", "member-3"
	n := uint64(len(Offered()))
	if fnv1a64(early)%n != fnv1a64(late)%n {
		t.Fatalf("%q and %q no longer prefer the same slot (%d vs %d) — this test needs a colliding pair and its premise has gone",
			early, late, fnv1a64(early)%n, fnv1a64(late)%n)
	}
	if !(early < late) {
		t.Fatalf("%q no longer sorts before %q; the test has the roles the wrong way round", early, late)
	}

	// Stability: the earlier key keeps its preferred hue whether it is
	// alone or in company. This is the "recognisable across documents"
	// half, and it is best-effort exactly because the OTHER key cannot
	// have it.
	soloEarly, _ := Allocate([]string{early}, nil)
	soloLate, _ := Allocate([]string{late}, nil)
	together, sep := Allocate([]string{early, late}, nil)
	if !sep {
		t.Error("two keys into twelve hues reported separated=false")
	}
	if together[0].Hue != soloEarly[0].Hue {
		t.Errorf("%q got %v alone and %v in company; the key that sorts earlier must keep its preferred hue",
			early, soloEarly[0].Hue, together[0].Hue)
	}

	// And the other half, asserted unconditionally: the later key moves.
	if together[1].Hue == soloLate[0].Hue {
		t.Errorf("%q got %v both alone and beside %q, but they prefer the same slot — one of them has to move, and it is this one",
			late, soloLate[0].Hue, early)
	}
	if together[0].Hue == together[1].Hue {
		t.Errorf("two colliding keys got the same hue %v; separation is the guarantee", together[0].Hue)
	}
}

// ─── The offered set ─────────────────────────────────────────────────

// TestOfferedSetClearsEveryBackgroundWeShip is the framework's own half
// of the proof R2 exports: every offered intent, against paper white and
// against every surface every shipped theme declares, in both schemes.
//
// The background list is DERIVED from the themes rather than typed out,
// so a theme that adds a surface or changes one is covered without
// anybody remembering to come back here. Paper white is the one literal,
// and it is a literal on purpose: it is not a theme token, it is the
// colour a document canvas is in every theme including dark, which is
// the case that made background a colour rather than a scheme.
func TestOfferedSetClearsEveryBackgroundWeShip(t *testing.T) {
	backgrounds := shippedBackgrounds(t)
	if len(backgrounds) < 12 {
		t.Fatalf("only %d backgrounds derived from the themes; the derivation has broken and this gate would be proving almost nothing", len(backgrounds))
	}
	t.Logf("proving %d offered intents against %d backgrounds", len(Offered()), len(backgrounds))

	if err := CheckIntents(Offered(), backgrounds); err != nil {
		t.Errorf("the offered set does not clear every background it can be rendered on:\n%v", err)
	}

	// Report the margins the set actually has, so a change that leaves
	// the gate green while eating all the headroom is visible.
	//
	// There is deliberately NO "worst fill ratio" here, and its absence
	// is the point. The search takes the first lightness clearing
	// floor + contrastMargin and then the one nearest the background,
	// which is always that same first step — so a worst fill ratio reads
	// 3.05 for a palette one step from failing and 3.05 for a palette
	// with the whole grid to spare. It is a measurement of
	// contrastMargin, not of the set, and a number that cannot move is
	// worse than no number: it looks like headroom and reports nothing.
	// The floor itself is still enforced, above, by CheckIntents.
	worstOn, worstChroma := math.Inf(1), math.Inf(1)
	for _, in := range Offered() {
		for _, bg := range backgrounds {
			sw, err := Pair(in.Hue, in.Chroma, bg)
			if err != nil {
				t.Fatalf("Pair(%v, %v, %s): %v", in.Hue, in.Chroma, bg, err)
			}
			worstOn = math.Min(worstOn, sw.OnRatio)
			worstChroma = math.Min(worstChroma, sw.Chroma)
		}
	}
	sep, sepBG, sepA, sepB := WorstSeparation(Offered(), backgrounds)
	t.Logf("margins: on-fill %.2f:1 (floor 4.5), chroma %.3f (floor %.3f), separation ΔE_OK %.4f (floor %.3f) between %s and %s on %s",
		worstOn, worstChroma, minDeliveredChroma, sep, MinSeparation, sepA, sepB, sepBG)

	// The separation number is the one that decides whether twelve hues
	// is the right capacity, so it gets an assertion of its own rather
	// than only a log line.
	if sep < MinSeparation {
		t.Errorf("the two closest offered fills are ΔE_OK %.4f apart on %s (%s vs %s), under the %.3f floor", sep, sepBG, sepA, sepB, MinSeparation)
	}
}

// shippedBackgrounds is every surface colour the suite can render an
// intent on: the four background tokens each theme declares, in both
// schemes, plus paper white.
func shippedBackgrounds(t *testing.T) []string {
	t.Helper()
	surfaces := []string{"--rst-bg", "--rst-surface", "--rst-surface-2", "--rst-accent-soft"}
	seen := map[string]bool{"#ffffff": true}
	out := []string{"#ffffff"}
	for _, theme := range ThemeNames() {
		tokens := themeTokens(t, theme)
		for _, scheme := range []string{"light", "dark"} {
			for _, name := range surfaces {
				v, ok := tokens[scheme][name]
				if !ok {
					t.Fatalf("theme %s (%s) does not declare %s", theme, scheme, name)
				}
				if _, _, _, err := parseHex(v); err != nil {
					t.Fatalf("theme %s (%s) declares %s as %q, which this gate cannot read: %v", theme, scheme, name, v, err)
				}
				if !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestCheckIntentsRejectsWhatItShould is the control for the proof
// itself. §7-v2's rule, applied to a gate whose real run is green:
// plant cases whose answer is known and confirm it says so.
//
// Four plants:
//   - a grey intent — chroma zero, which is what "out of gamut" looks
//     like once it has been through chroma reduction, and which is not
//     separable from any other grey;
//   - a background the engine cannot parse;
//   - an empty intent set, which must FAIL rather than pass, because a
//     gate over an empty set proves nothing;
//   - an empty background set, for the same reason.
func TestCheckIntentsRejectsWhatItShould(t *testing.T) {
	good := []Intent{{Hue: 265, Chroma: 0.14}}
	bgs := []string{"#ffffff", "#0b0d10"}

	if err := CheckIntents(good, bgs); err != nil {
		t.Fatalf("a known-good intent was rejected: %v", err)
	}

	planted := append(slices.Clone(good), Intent{Hue: 265, Chroma: 0})
	err := CheckIntents(planted, bgs)
	if err == nil {
		t.Error("a grey intent (chroma 0) passed; it resolves to a colour no other hue can be told apart from")
	} else if !strings.Contains(err.Error(), "grey") {
		t.Errorf("the grey intent was rejected for the wrong reason: %v", err)
	}

	if err := CheckIntents(good, []string{"#ffffff", "var(--rst-bg)"}); err == nil {
		t.Error("a background the engine cannot parse passed; it must be reported, not skipped")
	}

	if err := CheckIntents(nil, bgs); err == nil {
		t.Error("an empty intent set passed; a gate over an empty set must fail rather than pass")
	}
	if err := CheckIntents(good, nil); err == nil {
		t.Error("an empty background set passed; a gate over an empty set must fail rather than pass")
	}

	// Every failure is reported, not only the first: two bad intents
	// across two backgrounds is four complaints.
	bad := []Intent{{Hue: 10, Chroma: 0}, {Hue: 200, Chroma: 0}}
	err = CheckIntents(bad, bgs)
	if err == nil {
		t.Fatal("two grey intents passed")
	}
	if got := strings.Count(err.Error(), "grey"); got != 4 {
		t.Errorf("CheckIntents reported %d failures for 2 bad intents on 2 backgrounds, want 4 — it should not stop at the first", got)
	}
}

// TestCheckSwatchMeasuresTheFloors is the other control the proof needs.
// Pair is not able to return a swatch under a floor, so CheckIntents'
// floor assertions never fire in a real run — which means nothing has
// ever shown them measuring anything. They exist to catch Pair
// regressing, so here they are handed exactly the swatch a regressed
// Pair would produce, and they have to reject it.
func TestCheckSwatchMeasuresTheFloors(t *testing.T) {
	in := Intent{Hue: 265, Chroma: 0.14}

	sound := Swatch{Fill: "#6a91ea", On: "#0b152c", Background: "#ffffff", Hue: 265, Chroma: 0.14, FillRatio: 3.07, OnRatio: 5.92}
	if errs := checkSwatch(in, sound); len(errs) != 0 {
		t.Fatalf("a sound swatch was rejected: %v", errs)
	}

	underBoundary := sound
	underBoundary.FillRatio = 2.99
	if errs := checkSwatch(in, underBoundary); len(errs) != 1 {
		t.Errorf("a fill at 2.99:1 produced %d complaints, want 1", len(errs))
	}

	underText := sound
	underText.OnRatio = 4.49
	if errs := checkSwatch(in, underText); len(errs) != 1 {
		t.Errorf("an on-fill at 4.49:1 produced %d complaints, want 1", len(errs))
	}

	bothAndGrey := sound
	bothAndGrey.FillRatio, bothAndGrey.OnRatio, bothAndGrey.Chroma = 1.0, 1.0, 0.001
	if errs := checkSwatch(in, bothAndGrey); len(errs) != 3 {
		t.Errorf("a swatch failing all three criteria produced %d complaints, want 3", len(errs))
	}

	// Exactly on a floor passes: the floors are >=, not >.
	onTheLine := sound
	onTheLine.FillRatio, onTheLine.OnRatio = ContrastFloorBoundary, ContrastFloorText
	if errs := checkSwatch(in, onTheLine); len(errs) != 0 {
		t.Errorf("a swatch exactly on both floors was rejected: %v", errs)
	}
}

// TestOfferedIsACopy: a caller that edits the slice Offered hands back
// must not be editing the allocator's own set. Offered is the only route
// to it, so this is the only place that could go wrong.
func TestOfferedIsACopy(t *testing.T) {
	first := Offered()
	want := first[0].Hue
	first[0] = Intent{Hue: 999, Chroma: 999}
	if got := Offered()[0].Hue; got != want {
		t.Errorf("editing the slice from Offered changed the offered set: hue %v became %v", want, got)
	}
	intents, _ := Allocate([]string{"a"}, nil)
	if intents[0].Hue == 999 {
		t.Error("editing the slice from Offered changed what Allocate hands out")
	}
}

// ─── The bisection's premise ─────────────────────────────────────────

// TestLuminanceRisesWithLightness is the assumption Pair's search rests
// on, checked rather than assumed.
//
// Pair does not scan the lightness grid any more; it bisects for the two
// steps where the fill stops being dark enough and starts being light
// enough. Bisection is only sound if luminance never turns back on itself
// as lightness rises — and it is not obvious that it does not, because
// gamut reduction changes the colour at every step and eight-bit rounding
// quantises the result. So: 120 hues × 8 chromas × 391 steps, and not one
// step may be darker than the step below it.
//
// If this ever fails, Pair's search is unsound before it is slow, and the
// fix is to go back to a scan rather than to widen a tolerance here.
func TestLuminanceRisesWithLightness(t *testing.T) {
	for hue := 0.0; hue < 360; hue += 3 {
		for _, c := range []float64{0, 0.02, 0.05, 0.08, 0.14, 0.2, 0.3, 0.4} {
			prev := -1.0
			for step := 0; step <= lightnessSteps; step++ {
				rgb, _ := oklchRGB(stepLightness(step), c, hue)
				lum := relLuminance(rgb[0], rgb[1], rgb[2])
				if lum < prev {
					t.Fatalf("hue %g chroma %g: luminance falls from %.9f to %.9f between steps %d and %d — the search bisects and may not",
						hue, c, prev, lum, step-1, step)
				}
				prev = lum
			}
		}
	}
}

// TestBisectionFindsTheSameStepsAsAScan is the control for the search
// rewrite: the slow, obviously-correct thing, run beside the fast one.
//
// A scan of all 391 steps keeping the nearest feasible is what Pair used
// to do, and it is a paragraph of code anyone can check by eye. It is
// written out again here, independently of the shipped implementation,
// and the two have to agree on every colour. That is what makes the
// bisection a speed-up rather than a change.
func TestBisectionFindsTheSameStepsAsAScan(t *testing.T) {
	backgrounds := []string{"#ffffff", "#f4f5f8", "#eef3fe", "#808080", "#4b5563", "#111418", "#0b0d10", "#b91c1c"}
	for _, bg := range backgrounds {
		bgR, bgG, bgB, err := parseHex(bg)
		if err != nil {
			t.Fatal(err)
		}
		bgL, bgLum := oklabOf(bgR, bgG, bgB)[0], relLuminance(bgR, bgG, bgB)
		for hue := 0.0; hue < 360; hue += 11 {
			for _, chroma := range []float64{0.02, 0.14, 0.3} {
				// The scan, written out in full.
				var wantFill, wantOn string
				var found bool
				var bestDist float64
				for step := 0; step <= lightnessSteps; step++ {
					rgb, c := oklchRGB(stepLightness(step), chroma, hue)
					if ratioOf(relLuminance(rgb[0], rgb[1], rgb[2]), bgLum) < ContrastFloorBoundary+contrastMargin {
						continue
					}
					on, _, ok := ink(rgb, hue, c)
					if !ok {
						continue
					}
					dist := math.Abs(stepLightness(step) - bgL)
					if found && dist >= bestDist {
						continue
					}
					found, bestDist = true, dist
					wantFill, wantOn = hexOf(rgb), on
				}

				sw, err := Pair(hue, chroma, bg)
				if !found {
					if err == nil {
						t.Errorf("hue %g chroma %g on %s: the scan found nothing but Pair returned %s", hue, chroma, bg, sw.Fill)
					}
					continue
				}
				if err != nil {
					t.Errorf("hue %g chroma %g on %s: the scan found %s but Pair failed: %v", hue, chroma, bg, wantFill, err)
					continue
				}
				if sw.Fill != wantFill || sw.On != wantOn {
					t.Errorf("hue %g chroma %g on %s: Pair gave %s/%s, the scan gives %s/%s",
						hue, chroma, bg, sw.Fill, sw.On, wantFill, wantOn)
				}
			}
		}
	}
}

// ─── Separation ──────────────────────────────────────────────────────

// TestDeltaEOKControls checks the perceptual metric against distances
// whose answers are known before it runs: a colour against itself is
// exactly 0, the metric is symmetric, and black against white is exactly
// the OKLab lightness range, which is 1.
func TestDeltaEOKControls(t *testing.T) {
	for _, hex := range []string{"#000000", "#ffffff", "#00705d", "#b91c1c"} {
		got, err := DeltaEOK(hex, hex)
		if err != nil {
			t.Fatal(err)
		}
		if got != 0 {
			t.Errorf("DeltaEOK(%s, %s) = %v, want exactly 0 — a colour compared with itself", hex, hex, got)
		}
	}
	black, err := DeltaEOK("#000000", "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(black-1) > 1e-6 {
		t.Errorf("DeltaEOK(black, white) = %v, want 1 (OKLab lightness runs 0 to 1 and both are neutral)", black)
	}
	fwd, _ := DeltaEOK("#00705d", "#006d76")
	rev, _ := DeltaEOK("#006d76", "#00705d")
	if fwd != rev {
		t.Errorf("DeltaEOK is not symmetric: %v vs %v", fwd, rev)
	}
	// The pair the shipped set is tightest on, pinned so the number in
	// the design record and the number the code computes stay the same
	// thing. Two teals on day's dark page.
	if math.Abs(fwd-0.0449) > 0.0005 {
		t.Errorf("DeltaEOK(#00705d, #006d76) = %.4f, want 0.0449 — the shipped set's closest pair", fwd)
	}
	for _, bad := range []string{"teal", "var(--x)", "#12"} {
		if _, err := DeltaEOK(bad, "#ffffff"); err == nil {
			t.Errorf("DeltaEOK(%q, …) returned no error", bad)
		}
	}
}

// TestCheckIntentsMeasuresSeparation is the control for the pairwise
// half of the proof, which is new and therefore has never been seen
// failing. Two intents 3° apart resolve to two colours nobody can tell
// apart; the gate has to say so, and it has to name both.
//
// The known-good half runs beside it: the same two intents 30° apart —
// the shipped spacing — pass.
func TestCheckIntentsMeasuresSeparation(t *testing.T) {
	bgs := []string{"#ffffff", "#101318"}

	tooClose := []Intent{{Hue: 175, Chroma: 0.14}, {Hue: 178, Chroma: 0.14}}
	err := CheckIntents(tooClose, bgs)
	if err == nil {
		t.Fatal("two intents 3° apart passed; they resolve to the same colour to a reader")
	}
	if !strings.Contains(err.Error(), "OKLab") {
		t.Errorf("rejected for the wrong reason: %v", err)
	}

	farEnough := []Intent{{Hue: 175, Chroma: 0.14}, {Hue: 205, Chroma: 0.14}}
	if err := CheckIntents(farEnough, bgs); err != nil {
		t.Errorf("two intents at the shipped 30° spacing were rejected: %v", err)
	}

	// A single intent has no pair, and must not be reported as failing
	// one — an easy off-by-one in a pairwise loop.
	if err := CheckIntents([]Intent{{Hue: 175, Chroma: 0.14}}, bgs); err != nil {
		t.Errorf("one intent on its own was rejected: %v", err)
	}

	// WorstSeparation agrees with the gate about which pair is tightest.
	d, bg, _, _ := WorstSeparation(tooClose, bgs)
	if d >= MinSeparation {
		t.Errorf("WorstSeparation says %.4f on %s, but CheckIntents rejected the same set", d, bg)
	}
	// And it says +Inf when there is nothing to measure, rather than 0,
	// which would read as "these are identical".
	if d, _, _, _ := WorstSeparation(nil, bgs); !math.IsInf(d, 1) {
		t.Errorf("WorstSeparation over no intents = %v, want +Inf", d)
	}
}

// ─── Cost ────────────────────────────────────────────────────────────
//
// Both declared consumers call Pair per cell or per highlight, so its
// cost is part of its contract. Run with:
//
//	go test ./ui/ -run XXX -bench Colour
//
// For the record, on the machine this was written on: Pair went from
// 2,762,408 ns/op to about 12,000 ns/op when the scan became a bisection,
// ContrastRatio from 2,558 to 17, and parseHex from 845 to 7.

func BenchmarkColourPair(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Pair(265, 0.14, "#ffffff")
	}
}

func BenchmarkColourPairOnDark(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Pair(175, 0.14, "#101318")
	}
}

func BenchmarkColourContrastRatio(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ContrastRatio("#b91c1c", "#ffffff")
	}
}

func BenchmarkColourAllocate(b *testing.B) {
	keys := []string{"member-7", "member-3", "member-11", "guest-row-902", "member-1", "aaa", "zzz", "Member-7"}
	for i := 0; i < b.N; i++ {
		Allocate(keys, nil)
	}
}
