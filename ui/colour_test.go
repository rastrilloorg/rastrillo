package ui

import (
	"fmt"
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

// TestAllocateHonoursAvoid checks the OBSERVABLE half of the avoid set:
// that keys land outside it while there is room, that the separation
// flag goes false when there is not, and that a value naming no offered
// hue is ignored rather than shifting the allocation.
//
// It does NOT check the rule the spec spends its longest paragraph on —
// that avoid is applied inside the probe rather than filtering the set
// first — and this comment says so because an earlier version of it
// claimed otherwise, which is how a gated rule comes to look gated when
// it is not. Every assertion below passes equally under the forbidden
// compact-then-probe implementation, because both implementations keep
// keys out of avoid; they disagree only about WHERE the displaced key
// lands, and nothing here looks at that.
//
// That rule is gated by the golden table in colour_golden_test.go, which
// pins the destination as data — see its "the same keys, 25° withheld"
// case, where the two implementations differ on seven of eight keys.
// Deliberately one gate and not two: a second test trying to own the
// same rule is how the two drift and how one of them ends up asserting
// nothing while reading as though it does.
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
// ContrastRatio from 2,558 to 17, and parseHex from 845 to 7. Wash costs
// about 17,000 — it walks rather than bisects, because its second floor
// is a distance and turns back on itself either side of the background,
// but the early exit keeps it in the same order as Pair.

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

func BenchmarkColourWash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Wash(115, 0.14, testWeight, "#000000", "#ffffff")
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

// TestPublishedCapacityCeiling pins the two numbers reference/ui.md
// publishes about how big an evenly-spaced offered set can be.
//
// The page tells two apps that seventeen hues is the measured ceiling and
// sixteen is the one to plan against. Those are numbers somebody will
// size a feature on, and until now nothing recomputed them — the first
// version of that sentence said sixteen, because the densities were
// sampled two at a time and seventeen was never tried. A measurement
// published as prose and computed once is a snapshot, and this is the
// gate that makes it a measurement.
//
// The margins are asserted too, not just the pass/fail: "seventeen
// clears by 1.9% and sixteen by 7.3%" is the whole argument for
// recommending the smaller number, and an argument with a stale number in
// it is worse than none.
func TestPublishedCapacityCeiling(t *testing.T) {
	backgrounds := shippedBackgrounds(t)
	evenlySpaced := func(n int) []Intent {
		out := make([]Intent, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, Intent{Hue: 25 + float64(i)*(360.0/float64(n)), Chroma: 0.14})
		}
		return out
	}
	margin := func(n int) float64 {
		d, _, _, _ := WorstSeparation(evenlySpaced(n), backgrounds)
		return (d/MinSeparation - 1) * 100
	}

	// The ceiling, from both sides. Sampling only one side of a boundary
	// is how the published number came out one short the first time.
	if err := CheckIntents(evenlySpaced(17), backgrounds); err != nil {
		t.Errorf("seventeen hues no longer clears the floor, but reference/ui.md says it is the ceiling:\n%v", err)
	}
	if err := CheckIntents(evenlySpaced(18), backgrounds); err == nil {
		t.Error("eighteen hues now clears the floor, so seventeen is no longer the ceiling reference/ui.md publishes")
	}

	// And the margins the page quotes as the reason to recommend sixteen.
	for _, tt := range []struct {
		n    int
		want float64
	}{{16, 7.3}, {17, 1.9}} {
		if got := margin(tt.n); math.Abs(got-tt.want) > 0.15 {
			t.Errorf("%d hues clear the floor by %.1f%%, but reference/ui.md publishes %.1f%%", tt.n, got, tt.want)
		}
	}
}

// ─── Wash ────────────────────────────────────────────────────────────

// testWeight is the weight most of these gates ask for: Excel's
// conditional-formatting preset band, measured in
// TestWashScaleReferencePoints, which is what a spreadsheet fill weighs
// in the shipping software people compare us against. Gates that are
// about the FLOOR rather than about a request ask for MinSeparation
// explicitly, and say so where they do.
const testWeight = 0.12

// TestWashAgainstCasesWithAnObviousAnswer is the control for the wash
// resolver, on two cases whose answers need no computation.
//
// Black ink on paper white must give a PALE fill. Black text needs a
// light background, so the only fills it reads on are light ones, and
// "palest that is still visible" puts the answer just off white. Light
// ink on dark paper must give the mirror: a deep fill just off the page.
//
// If the resolver ever stopped reading its arguments — hard-coded a
// lightness, ignored the ink, confused ink with background — this is what
// notices, because the two answers are opposites.
func TestWashAgainstCasesWithAnObviousAnswer(t *testing.T) {
	pale, err := Wash(115, 0.14, testWeight, "#000000", "#ffffff")
	if err != nil {
		t.Fatalf("a yellow wash under black ink on white paper failed: %v", err)
	}
	if l := lum(t, pale.Fill); l < 0.8 {
		t.Errorf("black ink on paper white gave fill %s at luminance %.3f — a wash must be pale, not saturated", pale.Fill, l)
	}
	if pale.On != "#000000" {
		t.Errorf("On = %s, want the ink that was passed in (#000000)", pale.On)
	}
	// On is NORMALISED, not echoed: a Swatch spells all three of its
	// colours one way. A caller doing string equality against the value
	// they stored needs to know that, so it is pinned here rather than
	// left to be discovered by an XLSX round-trip that compares strings.
	shouty, err := Wash(115, 0.14, testWeight, "#FFF", "#111418")
	if err != nil {
		t.Fatal(err)
	}
	if shouty.On != "#ffffff" {
		t.Errorf("Wash(ink=\"#FFF\") returned On = %s, want the normalised #ffffff", shouty.On)
	}

	deep, err := Wash(115, 0.14, testWeight, "#ffffff", "#111418")
	if err != nil {
		t.Fatalf("a yellow wash under white ink on dark paper failed: %v", err)
	}
	if l := lum(t, deep.Fill); l > 0.1 {
		t.Errorf("white ink on dark paper gave fill %s at luminance %.3f — the mirror of the case above", deep.Fill, l)
	}
	if lum(t, pale.Fill) <= lum(t, deep.Fill) {
		t.Errorf("the same intent gave %s on white and %s on dark; those are the wrong way round", pale.Fill, deep.Fill)
	}

	// Both hold both floors, recomputed from the hexes they returned
	// rather than read off the struct.
	for _, sw := range []Swatch{pale, deep} {
		onRatio, err := ContrastRatio(sw.On, sw.Fill)
		if err != nil {
			t.Fatal(err)
		}
		if onRatio < 4.5 { // a literal, per TestFloorConstantsAreTheStandard
			t.Errorf("ink %s on fill %s = %.2f:1, want >= 4.5:1", sw.On, sw.Fill, onRatio)
		}
		sep, err := DeltaEOK(sw.Fill, sw.Background)
		if err != nil {
			t.Fatal(err)
		}
		if sep < MinSeparation {
			t.Errorf("fill %s against %s is ΔE_OK %.4f, under the %.3f floor — you could not see it had been applied", sw.Fill, sw.Background, sep, MinSeparation)
		}
		if math.Abs(sep-sw.Separation) > 1e-9 {
			t.Errorf("%+v reports separation %.6f but its own colours measure %.6f", sw, sw.Separation, sep)
		}
	}

	// And the wash is a wash: it does NOT stand 3:1 off the page. This is
	// the line between Wash and Pair, and it is worth an assertion so
	// nobody "fixes" Wash into Pair.
	if pale.FillRatio >= ContrastFloorBoundary {
		t.Errorf("the wash %s stands %.2f:1 off the page — that is a fill, not a wash", pale.Fill, pale.FillRatio)
	}
}

// TestWashSecondFloorBinds is the gate the brief asked for by name: if
// removing the perceptibility floor changed no output, that floor would
// be decorative and we should be told.
//
// It removes it, here, by finding what Wash WOULD return under the ink
// floor alone — the fill nearest the background that the ink still reads
// on — and requires two things: that it differs from what Wash actually
// returns, and that the reason it was rejected is the one claimed, namely
// that it is under MinSeparation from the background.
//
// That is the mutation kept as a permanent gate rather than run once by
// hand. The floor is binding on every offered hue or this fails.
func TestWashSecondFloorBinds(t *testing.T) {
	const ink, bg = "#000000", "#ffffff"
	bgR, bgG, bgB, err := parseHex(bg)
	if err != nil {
		t.Fatal(err)
	}
	bgLab := oklabOf(bgR, bgG, bgB)
	inkR, inkG, inkB, _ := parseHex(ink)
	inkLum := relLuminance(inkR, inkG, inkB)

	for _, in := range Offered() {
		got, err := Wash(in.Hue, in.Chroma, MinSeparation, ink, bg)
		if err != nil {
			t.Errorf("hue %g: %v", in.Hue, err)
			continue
		}

		// The ink floor alone, nearest the background.
		var paler string
		var palerSep float64
		best := math.Inf(1)
		for step := 0; step <= lightnessSteps; step++ {
			rgb, _ := oklchRGB(stepLightness(step), in.Chroma, in.Hue)
			if ratioOf(inkLum, relLuminance(rgb[0], rgb[1], rgb[2])) < ContrastFloorText+contrastMargin {
				continue
			}
			sep := labDistance(oklabOf(rgb[0], rgb[1], rgb[2]), bgLab)
			if sep < best {
				best, paler, palerSep = sep, hexOf(rgb), sep
			}
		}
		if paler == got.Fill {
			t.Errorf("hue %g: dropping the perceptibility floor changes nothing — it returns %s either way, so the floor is not doing any work here", in.Hue, paler)
			continue
		}
		if palerSep >= MinSeparation {
			t.Errorf("hue %g: the fill the ink floor alone would pick (%s) is ΔE_OK %.4f from the background, which clears %.3f — so it was rejected for some other reason than the one this test claims",
				in.Hue, paler, palerSep, MinSeparation)
		}
	}
}

// TestWashFailsWhenNoFillCanCarryTheInk shows the error path, which
// otherwise would never have been observed. An error return nobody has
// seen returned is not evidence that it can be.
//
// The case is a mid-grey font colour, and it is not contrived — it is
// forced arithmetic. #737373 sits at relative luminance 0.1714, and the
// engine holds 4.55:1 (the floor plus its margin), so a fill must be at
// luminance ≤ −0.0013 or ≥ 0.9576.
//
// The dark side is unreachable by ANY colour: the bound is negative, so
// not even black clears it — ContrastRatio("#737373", "#000000") is
// 4.43:1. Chroma is not the obstacle there; nothing is dark enough,
// because nothing is darker than black.
//
// The light side IS reachable, but only just, and only by the hues that
// are palest at their own chroma while still clearing the perceptibility
// floor against white. Two of the twelve manage it — hues 115 and 145,
// which at the floor resolve to #fbffe6 and #eeffed — which is why the
// count below is 10 and not 12. That number is a consequence of this
// paragraph, so the paragraph is asserted too: if the survivors were
// ever a different pair, the arithmetic above would no longer explain
// the count.
//
// The accessibility argument for the error existing, stated as a test:
// the incumbent behaviour is to hand back the fill and let the text
// vanish. Excel ships a conditional-formatting preset that does exactly
// that — #9C6500 on #FFEB9C, 4.12:1 — so this is the product default
// being improved on, not a careless user being second-guessed.
func TestWashFailsWhenNoFillCanCarryTheInk(t *testing.T) {
	const badInk = "#737373"

	// The arithmetic the comment above rests on, checked rather than
	// asserted in prose. Both bounds are computed the way Wash computes
	// them, but the CONCLUSIONS are literals.
	r, g, b, err := parseHex(badInk)
	if err != nil {
		t.Fatal(err)
	}
	inkLum := relLuminance(r, g, b)
	const engineFloor = ContrastFloorText + contrastMargin
	darkBound := (inkLum+0.05)/engineFloor - 0.05
	lightBound := engineFloor*(inkLum+0.05) - 0.05
	if darkBound >= 0 {
		t.Errorf("the dark bound is %.6f, not negative — the claim that no colour whatever is dark enough no longer holds", darkBound)
	}
	if black, err := ContrastRatio(badInk, "#000000"); err != nil || black >= ContrastFloorText {
		t.Errorf("this ink is %.4f:1 on black, which clears %.1f — the dark side is reachable after all", black, ContrastFloorText)
	}
	if math.Abs(lightBound-0.9576) > 0.0005 {
		t.Errorf("the light bound is %.6f, but the comment above says 0.9576", lightBound)
	}

	var survivors []float64
	failed := 0
	for _, in := range Offered() {
		if _, err := Wash(in.Hue, in.Chroma, testWeight, badInk, "#ffffff"); err != nil {
			failed++
			if !strings.Contains(err.Error(), "no wash") {
				t.Errorf("hue %g failed for an unexpected reason: %v", in.Hue, err)
			}
			continue
		}
		survivors = append(survivors, in.Hue)
	}
	if failed == 0 {
		t.Fatalf("every hue found a wash under ink %s; that ink cannot be read on anything and the error path has never been observed", badInk)
	}
	if !slices.Equal(survivors, []float64{115, 145}) {
		t.Errorf("the hues that survive ink %s are %v, but the explanation above names 115 and 145 — the count is no longer explained by the arithmetic", badInk, survivors)
	}
	t.Logf("ink %s: %d of %d offered hues have no wash; the survivors are %v", badInk, failed, len(Offered()), survivors)

	// The control. The same hues under an ink that CAN be carried must
	// all succeed, or this test is only proving that Wash sometimes
	// errors — which a function that always errored would also satisfy.
	for _, in := range Offered() {
		if _, err := Wash(in.Hue, in.Chroma, testWeight, "#000000", "#ffffff"); err != nil {
			t.Errorf("hue %g has no wash under plain black ink on white: %v", in.Hue, err)
		}
	}
}

// TestWashRejectsWhatItCannotRead: the inputs that must not come back as
// a swatch. The ink is the new one — passing a theme token name where a
// colour belongs must be an error, not a wash resolved against nothing.
func TestWashRejectsWhatItCannotRead(t *testing.T) {
	for _, bad := range []string{"var(--rst-text)", "black", "rgba(0,0,0,1)", "", "#gg0000"} {
		if _, err := Wash(115, 0.14, testWeight, bad, "#ffffff"); err == nil {
			t.Errorf("Wash(ink=%q) returned a swatch; an ink this engine cannot read must be an error", bad)
		}
		if _, err := Wash(115, 0.14, testWeight, "#000000", bad); err == nil {
			t.Errorf("Wash(background=%q) returned a swatch", bad)
		}
	}
	for _, chroma := range []float64{-0.1, math.NaN(), math.Inf(1)} {
		if _, err := Wash(115, chroma, testWeight, "#000000", "#ffffff"); err == nil {
			t.Errorf("Wash(chroma=%v) returned a swatch", chroma)
		}
	}
	if _, err := Wash(math.NaN(), 0.14, testWeight, "#000000", "#ffffff"); err == nil {
		t.Error("Wash(hue=NaN) returned a swatch")
	}
}

// TestWashWalkFindsTheSameFillAsAScan is the control for Wash's early
// exit. The walk stops as soon as the remaining lightnesses are further
// from the background than the best wash found so far, on the argument
// that a perceptual distance is at least the lightness difference alone.
// That is an argument, so here is the measurement: the plain scan of all
// 391 steps, written out independently, agreeing on every colour.
func TestWashWalkFindsTheSameFillAsAScan(t *testing.T) {
	inks := []string{"#000000", "#ffffff", "#1a1d21", "#e8eaed", "#404040"}
	// #000000 and #ffffff are in here because of the quantisation
	// plateaus at the two ends of the ramp, which is where the early
	// exit's premise is weakest — see
	// TestSeparationIsAtLeastTheLightnessDifference.
	backgrounds := []string{"#ffffff", "#f4f5f8", "#111418", "#ffff00", "#808080", "#000000"}
	for _, bg := range backgrounds {
		bgR, bgG, bgB, err := parseHex(bg)
		if err != nil {
			t.Fatal(err)
		}
		bgLab := oklabOf(bgR, bgG, bgB)
		for _, ink := range inks {
			inkR, inkG, inkB, _ := parseHex(ink)
			inkLum := relLuminance(inkR, inkG, inkB)
			for hue := 0.0; hue < 360; hue += 13 {
				for _, chroma := range []float64{0.05, 0.14} {
					for _, want := range []float64{0.0, MinSeparation, 0.08, testWeight, 0.30, 0.63, 0.9} {
						target := math.Max(want, MinSeparation)
						var wantFill string
						best := math.Inf(1)
						for step := 0; step <= lightnessSteps; step++ {
							rgb, _ := oklchRGB(stepLightness(step), chroma, hue)
							sep := labDistance(oklabOf(rgb[0], rgb[1], rgb[2]), bgLab)
							if sep < MinSeparation {
								continue
							}
							if ratioOf(inkLum, relLuminance(rgb[0], rgb[1], rgb[2])) < ContrastFloorText+contrastMargin {
								continue
							}
							if e := math.Abs(sep - target); e < best {
								best, wantFill = e, hexOf(rgb)
							}
						}
						sw, err := Wash(hue, chroma, want, ink, bg)
						if wantFill == "" {
							if err == nil {
								t.Errorf("hue %g chroma %g want %g ink %s on %s: the scan found nothing, Wash returned %s", hue, chroma, want, ink, bg, sw.Fill)
							}
							continue
						}
						if err != nil {
							t.Errorf("hue %g chroma %g want %g ink %s on %s: the scan found %s, Wash failed: %v", hue, chroma, want, ink, bg, wantFill, err)
							continue
						}
						if sw.Fill != wantFill {
							t.Errorf("hue %g chroma %g want %g ink %s on %s: Wash gave %s, the scan gives %s", hue, chroma, want, ink, bg, sw.Fill, wantFill)
						}
					}
				}
			}
		}
	}
}

// washCanvases is every surface the suite can render a wash on, paired
// with every ink that can appear on it: the theme's own --rst-text for
// that scheme, plus the two an author can PIN.
//
// The pinned pair is the half that matters and the half a conjunction
// would have missed. An author who sets their font colour to black keeps
// it when a reader switches to the dark theme, so "pinned black on dark
// paper" is a canvas that really occurs, and it is the hardest one: black
// ink needs a light fill no matter what surrounds the cell.
//
// Paper white and dark paper are literals rather than tokens on purpose.
// A document canvas is not a theme surface — it is paper, in every theme
// — which is the case that made background a colour rather than a scheme
// in the first place.
func washCanvases(t *testing.T) []Canvas {
	t.Helper()
	out := []Canvas{
		{Background: "#ffffff", Inks: []string{"#000000", "#ffffff"}},
		{Background: "#1a1a1a", Inks: []string{"#ffffff", "#000000"}},
	}
	for _, theme := range ThemeNames() {
		tokens := themeTokens(t, theme)
		for _, scheme := range []string{"light", "dark"} {
			ink, ok := tokens[scheme]["--rst-text"]
			if !ok {
				t.Fatalf("theme %s (%s) declares no --rst-text", theme, scheme)
			}
			for _, name := range []string{"--rst-bg", "--rst-surface", "--rst-surface-2", "--rst-accent-soft"} {
				bg, ok := tokens[scheme][name]
				if !ok {
					t.Fatalf("theme %s (%s) does not declare %s", theme, scheme, name)
				}
				out = append(out, Canvas{Background: bg, Inks: []string{ink, "#000000", "#ffffff"}})
			}
		}
	}
	return out
}

// TestOfferedSetServesEveryWashCanvas is the wash half of the offered-set
// proof, and it answers the question the two callers asked.
//
// Docs stores an intent and resolves it per reader, because Paul ruled
// their canvas light by default with dark as a PER-PERSON preference. So
// the requirement is not that one hex works on both papers — it is that
// every hue has a wash on white AND has a wash on dark, two separate
// resolutions of one stored intent. That distinction is why this is a
// matrix and not a conjunction: reading it as a conjunction would reject
// a set that serves both readers perfectly well.
//
// The ink flips with the theme too, and an author can pin it, so each
// background carries three inks. 26 canvases × their inks × 12 intents.
func TestOfferedSetServesEveryWashCanvas(t *testing.T) {
	canvases := washCanvases(t)
	if len(canvases) < 20 {
		t.Fatalf("only %d canvases derived; the derivation has broken and this gate would prove almost nothing", len(canvases))
	}

	// Counted rather than asserted from a formula, because the canvases
	// do not all carry the same number of inks: the two literal ones
	// carry two each (a document canvas has no theme ink of its own) and
	// the 24 theme ones carry three. 2×2 + 24×3 = 76 background/ink
	// pairs, × 12 intents = 912. Some backgrounds repeat across themes,
	// so those 26 canvases hold 21 distinct backgrounds and 64 distinct
	// background/ink pairs — nothing is under-covered, but the honest
	// count of independent cells is the smaller one.
	cells, pairs := 0, 0
	distinctBG, distinctPairs := map[string]bool{}, map[string]bool{}
	for _, c := range canvases {
		pairs += len(c.Inks)
		cells += len(c.Inks) * len(Offered())
		distinctBG[c.Background] = true
		for _, ink := range c.Inks {
			distinctPairs[c.Background+"/"+ink] = true
		}
	}
	t.Logf("%d intents over %d canvases = %d background/ink pairs = %d cells (%d distinct backgrounds, %d distinct pairs)",
		len(Offered()), len(canvases), pairs, cells, len(distinctBG), len(distinctPairs))

	// At the weight an app actually asks for, AND at the floor. The two
	// are different questions: at testWeight the perceptibility floor
	// never binds, because the target is four times it, so a run at
	// testWeight alone would leave floor 2 unexercised in the gate that
	// exists to prove it. At MinSeparation the floor is the target, and
	// checkWash is looking straight at it.
	for _, weight := range []float64{testWeight, MinSeparation} {
		if err := CheckWashes(Offered(), weight, canvases); err != nil {
			t.Errorf("at weight %v the offered set cannot wash every canvas it can be rendered on:\n%v", weight, err)
		}
	}

	// The map, logged rather than only asserted: how pale the washes
	// actually come out, and how far the hardest cross case is pushed.
	// A cell with a wash is not the same as a cell with a WASH — pinned
	// black ink on a dark canvas forces a fill so light it is no longer
	// pale, and a caller should be able to see that in a log rather than
	// discover it on a screen.
	worstSep, bestSep, worstInk := math.Inf(1), 0.0, math.Inf(1)
	cellsSeen := 0
	for _, c := range canvases {
		for _, ink := range c.Inks {
			want, err := normalHex(ink)
			if err != nil {
				t.Fatal(err)
			}
			for _, in := range Offered() {
				sw, err := Wash(in.Hue, in.Chroma, testWeight, ink, c.Background)
				if err != nil {
					continue
				}
				cellsSeen++

				// Asserted on every cell, not logged. An earlier version
				// of this loop measured these and printed them, which is
				// a gate that gates nothing: with the perceptibility
				// floor removed it printed "quietest 0.0018" and passed.
				if sw.Separation < MinSeparation {
					t.Errorf("%s under ink %s, hue %g: fill %s is ΔE_OK %.4f from the background, under the %.3f floor",
						c.Background, ink, in.Hue, sw.Fill, sw.Separation, MinSeparation)
				}
				if sw.OnRatio < 4.5 { // a literal, per TestFloorConstantsAreTheStandard
					t.Errorf("%s under ink %s, hue %g: the ink is %.2f:1 on fill %s, want >= 4.5:1",
						c.Background, ink, in.Hue, sw.OnRatio, sw.Fill)
				}
				// The headline guarantee of the whole function, on all
				// 912 cells rather than on one.
				if sw.On != want {
					t.Errorf("%s under ink %s, hue %g: On came back %s — Wash must never invent a font colour",
						c.Background, ink, in.Hue, sw.On)
				}

				worstSep = math.Min(worstSep, sw.Separation)
				bestSep = math.Max(bestSep, sw.Separation)
				worstInk = math.Min(worstInk, sw.OnRatio)
			}
		}
	}
	if cellsSeen != cells {
		t.Errorf("only %d of %d cells resolved; the rest are gaps this gate is not reporting", cellsSeen, cells)
	}
	t.Logf("wash separation across the matrix: quietest ΔE_OK %.4f, loudest %.4f (floor %.3f); tightest ink %.2f:1", worstSep, bestSep, MinSeparation, worstInk)

	// The control for the gate: an ink that cannot be carried must make
	// it fail. Without this, a CheckWashes that returned nil
	// unconditionally would look identical to a green run.
	planted := append(slices.Clone(canvases), Canvas{Background: "#ffffff", Inks: []string{"#737373"}})
	if err := CheckWashes(Offered(), testWeight, planted); err == nil {
		t.Error("a canvas with an uncarriable mid-grey ink passed; the gate is not measuring")
	}
	// And the empty sets fail rather than pass, per §7-v2.
	if err := CheckWashes(nil, testWeight, canvases); err == nil {
		t.Error("an empty intent set passed CheckWashes")
	}
	if err := CheckWashes(Offered(), testWeight, nil); err == nil {
		t.Error("an empty canvas set passed CheckWashes")
	}
	if err := CheckWashes(Offered(), testWeight, []Canvas{{Background: "#ffffff"}}); err == nil {
		t.Error("a canvas listing no inks passed; a background with nothing drawn on it proves nothing")
	}
}

// TestClassicFillsAreNearAnOfferedHue measures what XLSX import fidelity
// depends on.
//
// Sheets maps an arbitrary incoming fill onto the nearest offered intent,
// so a gap in the circle is not a cosmetic matter: every imported fill in
// that region snaps visibly sideways, on a file the user never edited.
// Even spacing bounds the worst case at half the step — 15° for twelve
// hues — but the bound says nothing about whether the colours people
// ACTUALLY use fall near an offered hue or in the gaps, and that is the
// question worth measuring.
//
// The five are Excel's own standard palette entries, since XLSX
// round-trip is what motivates this.
func TestClassicFillsAreNearAnOfferedHue(t *testing.T) {
	for _, f := range []struct{ name, hex string }{
		{"Yellow", "#ffff00"},
		{"Green", "#00b050"},
		{"Red", "#ff0000"},
		{"Orange", "#ffc000"},
		{"Light Blue", "#00b0f0"},
	} {
		r, g, b, err := parseHex(f.hex)
		if err != nil {
			t.Fatal(err)
		}
		lab := oklabOf(r, g, b)
		hue := math.Atan2(lab[2], lab[1]) * 180 / math.Pi
		if hue < 0 {
			hue += 360
		}
		nearest, gap := 0.0, math.Inf(1)
		for _, in := range Offered() {
			d := math.Abs(hue - in.Hue)
			if d > 180 {
				d = 360 - d
			}
			if d < gap {
				nearest, gap = in.Hue, d
			}
		}
		t.Logf("%-11s %s  OKLCh hue %6.2f°  nearest offered %5.1f°  gap %5.2f°", f.name, f.hex, hue, nearest, gap)

		// The bound even spacing guarantees. Asserted so that a change to
		// the offered set which happened to straddle one of these five
		// shows up here, where the reason is written down, rather than as
		// a support ticket about imported spreadsheets changing colour.
		if gap > 15.0 {
			t.Errorf("%s (%s) is %.2f° from the nearest offered hue, past the %.1f° half-step even spacing guarantees — the offered set is no longer evenly spaced",
				f.name, f.hex, gap, 15.0)
		}
	}
}

// TestSeparationIsAtLeastTheLightnessDifference is the premise Wash's
// early exit rests on, measured rather than assumed — the same treatment
// TestLuminanceRisesWithLightness gives Pair's bisection.
//
// The walk breaks on the argument that a perceptual distance is at least
// the lightness difference alone, since the other two OKLab axes only add
// to it. That is true of the requested lightness and the delivered
// colour's lightness, but Wash compares the REQUESTED lightness against a
// distance measured on the DELIVERED colour, and those two part company
// where eight-bit rounding flattens a stretch of the ramp onto one hex.
//
// The plateau at the bottom is the worst of it: every lightness from 0.02
// up to about 0.0525 renders #000000, so against a black background the
// separation stays 0 while the lightness difference climbs to 0.0525.
// washSlack is what covers that, so washSlack has to be larger than the
// worst shortfall anyone can construct, and this measures it.
//
// It is pinned as a literal for the same reason the contrast floors are:
// asserting washSlack against a shortfall computed with washSlack in it
// would move the bar with the mutation.
func TestSeparationIsAtLeastTheLightnessDifference(t *testing.T) {
	var backgrounds []string
	for v := 0; v <= 20; v++ { // the dark plateau, one eight-bit step at a time
		backgrounds = append(backgrounds, greyHex(v))
	}
	for v := 235; v <= 255; v++ { // and the light one
		backgrounds = append(backgrounds, greyHex(v))
	}
	backgrounds = append(backgrounds, "#808080", "#111418", "#f4f5f8", "#ffff00", "#0000ff", "#00b050")

	worst, at := 0.0, ""
	for _, bg := range backgrounds {
		r, g, b, err := parseHex(bg)
		if err != nil {
			t.Fatal(err)
		}
		bgLab := oklabOf(r, g, b)
		for hue := 0.0; hue < 360; hue += 9 {
			for _, c := range []float64{0, 0.01, 0.05, 0.14, 0.3} {
				for step := 0; step <= lightnessSteps; step++ {
					l := stepLightness(step)
					rgb, _ := oklchRGB(l, c, hue)
					short := math.Abs(l-bgLab[0]) - labDistance(oklabOf(rgb[0], rgb[1], rgb[2]), bgLab)
					if short > worst {
						worst, at = short, fmt.Sprintf("background %s, hue %g chroma %g at lightness %.4f", bg, hue, c, l)
					}
				}
			}
		}
	}
	t.Logf("worst shortfall of separation below the lightness difference: %.6f (%s)", worst, at)

	if washSlack != 0.08 {
		t.Errorf("washSlack = %v, want 0.08 — it is the bound the early exit is sound under, not a comfort margin", washSlack)
	}
	if worst >= 0.08 {
		t.Errorf("the worst shortfall is %.6f, at or above washSlack — the early exit can now stop before the true answer, and the constant has to rise above the measurement rather than the measurement being explained away (%s)", worst, at)
	}
	if worst < 0.03 {
		t.Errorf("the worst shortfall is only %.6f; washSlack is set at 0.08 on the strength of a plateau that has apparently gone, so the number no longer means what its comment says", worst)
	}
}

// TestWashMeetsRequestsItCanReach is the control for the target-separation
// contract, in the two directions that matter. A flag that has only ever
// been observed true is not evidence that it can be false.
//
// Both cases have answers known before running them. Black ink on paper
// white can carry a great deal of weight — flat yellow is 19.6:1 under
// black — so a request in Excel's preset band must be met exactly. Flat
// blue cannot: black ink on #0000FF is 2.44:1, so asking for blue at its
// own weight of 0.63 under black ink CANNOT be honoured, and the answer
// must come back lighter, still readable, still visible, and flagged.
func TestWashMeetsRequestsItCanReach(t *testing.T) {
	// Honoured: a weight the constraints allow.
	for _, want := range []float64{0.05, 0.08, testWeight, 0.16, 0.21} {
		sw, err := Wash(115, 0.14, want, "#000000", "#ffffff")
		if err != nil {
			t.Fatalf("yellow at %v under black ink on white: %v", want, err)
		}
		if !sw.SeparationMet {
			t.Errorf("asked for %v, got %.4f, and SeparationMet is false — black ink on white can carry this weight", want, sw.Separation)
		}
		if sw.SeparationRequested != want {
			t.Errorf("SeparationRequested = %v, want %v — it must echo the request, not the effective target", sw.SeparationRequested, want)
		}
		if sw.Separation < want-washTolerance {
			t.Errorf("asked for %v and got %.4f, which is less — SeparationMet must not be true for a wash lighter than the request", want, sw.Separation)
		}
	}

	// Not honoured, case 1: blue at its own weight under black ink. The
	// worked example in Wash's doc comment.
	blue, err := Wash(265, 0.14, 0.63, "#000000", "#ffffff")
	if err != nil {
		t.Fatalf("blue at its own weight under black ink: %v", err)
	}
	if blue.SeparationMet {
		t.Errorf("asked for 0.63 and got %.4f, but SeparationMet is true", blue.Separation)
	}
	if blue.Separation >= 0.63 {
		t.Errorf("got separation %.4f for a request black ink cannot carry; it should have degraded toward paler", blue.Separation)
	}
	if blue.OnRatio < 4.5 {
		t.Errorf("the constrained answer is unreadable at %.2f:1 — degrading must never break floor 1", blue.OnRatio)
	}
	if blue.Separation < MinSeparation {
		t.Errorf("the constrained answer is invisible at %.4f — degrading must never break floor 2", blue.Separation)
	}

	// A request under the floor is raised to it, and is MET — the caller
	// asked for at least 0.001 of weight and got 0.03, which is at least
	// that. It is not the outcome a user needs warning about, and the
	// flag exists to be acted on.
	tiny, err := Wash(115, 0.14, 0.001, "#000000", "#ffffff")
	if err != nil {
		t.Fatalf("a request under the floor: %v", err)
	}
	if !tiny.SeparationMet {
		t.Error("a request of 0.001 answered with the floor was reported as unmet; it got more weight than it asked for, which is not something to warn about")
	}
	if tiny.Separation < MinSeparation {
		t.Errorf("a request under the floor produced %.4f, under the floor", tiny.Separation)
	}
	if tiny.SeparationRequested != 0.001 {
		t.Errorf("SeparationRequested = %v, want the 0.001 that was asked for", tiny.SeparationRequested)
	}

	// A Pair swatch requests no weight and must not read as unmet, or
	// the `if !sw.SeparationMet { warn }` line the docs suggest warns on
	// every Pair result a caller runs it against.
	p, err := Pair(265, 0.14, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if !p.SeparationMet {
		t.Error("a Pair swatch came back with SeparationMet false; it asked for no weight and cannot have failed to carry one")
	}
	if p.SeparationRequested != 0 {
		t.Errorf("a Pair swatch reports SeparationRequested %v, want 0", p.SeparationRequested)
	}

	// The flag against the numbers, over a wide sweep: every swatch that
	// claims the request was met must carry at least the weight asked
	// for, and every swatch that says it was not must fall short or have
	// been asked for less than the floor. The flag is a claim about the
	// two floats beside it and this is that claim checked.
	sweep, met, unmet, heavier := 0, 0, 0, 0
	for _, bg := range []string{"#ffffff", "#f4f5f8", "#111418", "#0b0d10", "#808080"} {
		for _, ink := range []string{"#000000", "#ffffff", "#1a1d21", "#e8eaed"} {
			for _, in := range Offered() {
				for _, want := range []float64{0.001, 0.03, 0.08, 0.12, 0.21, 0.45, 0.63} {
					sw, err := Wash(in.Hue, in.Chroma, want, ink, bg)
					if err != nil {
						continue
					}
					sweep++
					if sw.Separation > want+washTolerance {
						heavier++
					}
					switch {
					case sw.SeparationMet:
						met++
						if sw.Separation < want-washTolerance {
							t.Errorf("bg %s ink %s hue %g: met=true but %.4f is lighter than the %v requested", bg, ink, in.Hue, sw.Separation, want)
						}
					default:
						unmet++
						if sw.Separation >= want-washTolerance {
							t.Errorf("bg %s ink %s hue %g: met=false but %.4f carries the %v requested", bg, ink, in.Hue, sw.Separation, want)
						}
					}
				}
			}
		}
	}
	t.Logf("flag sweep: %d resolutions, %d met, %d not met, %d heavier than asked", sweep, met, unmet, heavier)
	if met == 0 || unmet == 0 {
		t.Errorf("the sweep saw met=%d and unmet=%d; a flag observed in only one state is not evidence it can take the other", met, unmet)
	}

	// The narrowing, checked rather than described. Most of what the
	// earlier definition called "not met" was a wash HEAVIER than asked
	// — an outcome nobody needs warning about — and a flag that fired on
	// it would have been wallpaper by the time it mattered. If heavier
	// results ever stopped outnumbering lighter ones, the narrowing
	// would have stopped being worth its complexity, and this says so.
	if heavier <= unmet {
		t.Errorf("the sweep saw %d heavier and %d lighter; the flag was narrowed to the lighter case because heavier dominates, and it no longer does", heavier, unmet)
	}
}

// TestWashScaleReferencePoints publishes the scale Wash's doc comment
// quotes, computed here rather than transcribed.
//
// Callers have to choose a number, and "0.12" means nothing on its own —
// so the doc comment anchors it to colours people know. Those anchors are
// only worth anything if they are measured on the same scale
// Swatch.Separation reports, so this measures them through the shipped
// path: DeltaEOK, which is the same labDistance kernel Swatch.Separation
// is computed from, and then confirms the chain by asking Wash for each
// weight and reading Swatch.Separation back.
//
// The four saturated fills are from Sheets' own round-trip fixtures. The
// three tints are Excel's conditional-formatting presets.
func TestWashScaleReferencePoints(t *testing.T) {
	const canvas = "#ffffff"
	type ref struct {
		name, hex string
		want      float64 // what the doc comment publishes, to 2dp
	}
	presets := []ref{
		{"Excel light green preset", "#C6EFCE", 0.11},
		{"Excel light yellow preset", "#FFEB9C", 0.12},
		{"Excel light red preset", "#FFC7CE", 0.14},
	}
	saturated := []ref{
		{"flat yellow", "#FFFF00", 0.21},
		{"solid green", "#00B050", 0.38},
		{"flat red", "#FF0000", 0.45},
		{"flat blue", "#0000FF", 0.63},
	}

	measure := func(r ref) float64 {
		t.Helper()
		got, err := DeltaEOK(r.hex, canvas)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		ink, err := ContrastRatio("#000000", r.hex)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		t.Logf("%-26s %s  Separation %.4f   black ink on it %5.2f:1", r.name, r.hex, got, ink)
		if math.Abs(got-r.want) > 0.005 {
			t.Errorf("%s measures %.4f but Wash's doc comment publishes %.2f", r.name, got, r.want)
		}
		return got
	}

	var palest, loudestPreset, quietestSaturated = math.Inf(1), 0.0, math.Inf(1)
	for _, r := range presets {
		d := measure(r)
		loudestPreset = math.Max(loudestPreset, d)
	}
	for _, r := range saturated {
		d := measure(r)
		quietestSaturated = math.Min(quietestSaturated, d)
	}
	for _, in := range Offered() {
		sw, err := Wash(in.Hue, in.Chroma, MinSeparation, "#000000", canvas)
		if err != nil {
			t.Fatalf("hue %g at the floor: %v", in.Hue, err)
		}
		palest = math.Min(palest, sw.Separation)
	}

	// The ordering the scale depends on. If Excel's own rule-driven fills
	// were LIGHTER than our floor, the floor would be the thing that was
	// wrong, and the doc comment would be teaching a scale upside down.
	if !(palest < loudestPreset && loudestPreset < quietestSaturated) {
		t.Errorf("the scale is not ordered as published: our palest %.4f, loudest preset %.4f, quietest saturated fill %.4f", palest, loudestPreset, quietestSaturated)
	}
	t.Logf("scale: our palest %.4f < presets %.4f..%.4f < saturated %.4f..", palest, 0.1056, loudestPreset, quietestSaturated)

	// The round trip that makes these numbers Swatch.Separation's and not
	// a second implementation's: ask Wash for each preset's weight and
	// read back what it reports.
	for _, r := range presets {
		want, _ := DeltaEOK(r.hex, canvas)
		sw, err := Wash(85, 0.14, want, "#000000", canvas)
		if err != nil {
			t.Fatalf("%s weight: %v", r.name, err)
		}
		if !sw.SeparationMet || math.Abs(sw.Separation-want) > washTolerance {
			t.Errorf("asked Wash for %s's weight (%.4f) and Swatch.Separation came back %.4f, met=%v", r.name, want, sw.Separation, sw.SeparationMet)
		}
	}
}

// TestCheckWashesMeasuresTheSwatch is the control for checkWash, whose
// three assertions a working Wash can never trip — so, as with
// checkSwatch, they are handed the swatch a regressed Wash would produce
// and required to reject it.
//
// The middle one is the finding this round exists for: CheckWashes used
// to ask only whether an error came back, so with the perceptibility
// floor removed it returned nil over fills a thousandth from the
// background.
func TestCheckWashesMeasuresTheSwatch(t *testing.T) {
	in := Intent{Hue: 115, Chroma: 0.14}
	const ink = "#000000"
	sound := Swatch{Fill: "#fbffe6", On: ink, Background: "#ffffff", OnRatio: 20.55, Separation: 0.12}
	if errs := checkWash(in, ink, sound); len(errs) != 0 {
		t.Fatalf("a sound wash was rejected: %v", errs)
	}

	unreadable := sound
	unreadable.OnRatio = 4.49
	if errs := checkWash(in, ink, unreadable); len(errs) != 1 {
		t.Errorf("ink at 4.49:1 produced %d complaints, want 1", len(errs))
	}

	invisible := sound
	invisible.Separation = 0.0049 // the exact value the review found slipping through
	if errs := checkWash(in, ink, invisible); len(errs) != 1 {
		t.Errorf("a fill 0.0049 from the background produced %d complaints, want 1", len(errs))
	}

	invented := sound
	invented.On = "#556b2f" // Wash inventing a font colour: the thing it exists not to do
	if errs := checkWash(in, ink, invented); len(errs) != 1 {
		t.Errorf("an invented font colour produced %d complaints, want 1", len(errs))
	}

	allThree := sound
	allThree.OnRatio, allThree.Separation, allThree.On = 1.0, 0.001, "#556b2f"
	if errs := checkWash(in, ink, allThree); len(errs) != 3 {
		t.Errorf("a swatch failing all three produced %d complaints, want 3", len(errs))
	}

	// Exactly on the floors passes: they are >=, not >.
	onTheLine := sound
	onTheLine.OnRatio, onTheLine.Separation = ContrastFloorText, MinSeparation
	if errs := checkWash(in, ink, onTheLine); len(errs) != 0 {
		t.Errorf("a wash exactly on both floors was rejected: %v", errs)
	}
}

// achievableWeights returns every separation Wash could deliver for one
// (hue, chroma, ink, background), sorted.
//
// Sorted, and not in lightness order, because those are different things
// and the difference matters: separation is V-shaped about the
// background, so two lightnesses either side of it deliver the same
// weight. A caller's request lands in the SET of achievable values, so
// the set is what has to be examined for gaps. Reading gaps off the
// lightness walk instead reports boundaries that are not gaps at all.
func achievableWeights(hue, chroma float64, ink, bg string) []float64 {
	r, g, b, err := parseHex(bg)
	if err != nil {
		return nil
	}
	bgLab := oklabOf(r, g, b)
	ir, ig, ib, err := parseHex(ink)
	if err != nil {
		return nil
	}
	inkLum := relLuminance(ir, ig, ib)
	var out []float64
	for step := 0; step <= lightnessSteps; step++ {
		rgb, _ := oklchRGB(stepLightness(step), chroma, hue)
		sep := labDistance(oklabOf(rgb[0], rgb[1], rgb[2]), bgLab)
		if sep < MinSeparation {
			continue
		}
		if ratioOf(inkLum, relLuminance(rgb[0], rgb[1], rgb[2])) < ContrastFloorText+contrastMargin {
			continue
		}
		out = append(out, sep)
	}
	sort.Float64s(out)
	return out
}

// TestAchievableWeightsHaveWideGaps gates the claim washTolerance's
// comment rests on: that the weights a caller can actually be given are
// not evenly spaced, and the gaps are far too wide for any tolerance to
// swallow honestly.
//
// It exists because that claim shipped as a number in a comment with no
// test behind it — the same defect as the error path's arithmetic one
// round earlier, in the same file. So this asserts the CONCLUSION and
// logs the measurement, rather than pinning a figure that would be
// re-quoted into prose the next time somebody needed it.
//
// The conclusion is the one washTolerance depends on: a tolerance wide
// enough to swallow the gaps would report every constrained answer as
// honoured, which is precisely what SeparationMet exists to prevent.
func TestAchievableWeightsHaveWideGaps(t *testing.T) {
	backgrounds := []string{"#ffffff", "#f4f5f8", "#eef3fe", "#111418", "#1a1f26", "#0b0d10", "#171717", "#242424"}
	inks := []string{"#000000", "#ffffff", "#1a1d21", "#e8eaed", "#111111", "#f2f2f2"}

	worst, at := 0.0, ""
	for _, bg := range backgrounds {
		for _, ink := range inks {
			for hue := 0.0; hue < 360; hue += 5 {
				for _, c := range []float64{0.05, 0.14, 0.3} {
					w := achievableWeights(hue, c, ink, bg)
					for i := 1; i < len(w); i++ {
						if d := w[i] - w[i-1]; d > worst {
							worst, at = d, fmt.Sprintf("ink %s on %s, hue %g chroma %g, between %.4f and %.4f", ink, bg, hue, c, w[i-1], w[i])
						}
					}
				}
			}
		}
	}
	t.Logf("largest gap in the achievable weights: %.4f (%s)", worst, at)

	if worst <= 10*washTolerance {
		t.Errorf("the largest gap in the achievable weights is %.4f, no more than ten times washTolerance (%v) — the argument for keeping the tolerance small has gone, and SeparationMet's definition should be revisited rather than this bound relaxed", worst, washTolerance)
	}
}

// TestInkUnknownIsUnsatisfiable gates the arithmetic §6-v2.2d rests on,
// which until now lived only in a doc comment.
//
// The claim is that an ink-unknown wash cannot simply be Wash with the
// argument left out — that retaining the FULL 4.5:1 for every ink still
// legible on the page is not merely hard on paper white but impossible.
// It is a load-bearing number in a spec, so it is checked here rather
// than believed.
//
// The conclusions are literals; only the arithmetic is computed.
func TestInkUnknownIsUnsatisfiable(t *testing.T) {
	const paper = "#ffffff"
	pr, pg, pb, err := parseHex(paper)
	if err != nil {
		t.Fatal(err)
	}
	paperLum := relLuminance(pr, pg, pb)
	if paperLum != 1.0 {
		t.Fatalf("paper white measures luminance %v, want 1.0", paperLum)
	}

	// The worst ink still legible on the page: exactly at the floor.
	worstInk := (paperLum+0.05)/ContrastFloorText - 0.05
	if math.Abs(worstInk-0.183333) > 1e-5 {
		t.Errorf("the worst still-legible ink on paper white is at luminance %.6f, but the doc comment says 0.1833", worstInk)
	}

	// What a wash would have to be for that ink to keep the whole floor.
	needed := ContrastFloorText*(worstInk+0.05) - 0.05
	if math.Abs(needed-1.0) > 1e-9 {
		t.Errorf("a wash retaining the full floor for that ink needs luminance %.9f, but the doc comment says exactly 1.0", needed)
	}
	if needed < paperLum {
		t.Errorf("the required luminance %.9f is below the page's own %.9f, so the case is merely hard rather than impossible and the comment overstates it", needed, paperLum)
	}

	// And the only colour at that luminance is the page itself, which
	// fails the other floor — so there is no wash there at all, which is
	// what makes it unsatisfiable rather than a narrow squeeze.
	sep, err := DeltaEOK(paper, paper)
	if err != nil {
		t.Fatal(err)
	}
	if sep >= MinSeparation {
		t.Errorf("the page against itself measures %.4f, which clears the perceptibility floor — the argument that the only candidate is invisible has gone", sep)
	}
}

// TestReachableCeilingUnderTheDefaultCase measures what a caller building
// a weight picker most needs, and what the published scale is arranged
// around.
//
// Near-black ink on a white canvas is not an edge case in a spreadsheet:
// it is what a cell looks like before anybody touches it. Excel's default
// font is black, imported files overwhelmingly carry black or near-black,
// and a user picking a fill has almost always left their text alone. So
// the weights reachable under THAT case are the ones a picker can offer,
// and any published weight above them is a colour to paint rather than a
// weight to ask for.
//
// The numbers are logged, and the two things the scale's presentation
// depends on are asserted: that the whole rule-driven band is reachable
// for every offered hue, and that the ceiling sits well above the
// hand-picked band's floor. If either stopped holding, the guidance would
// be partly fiction and would have to be rewritten rather than the test
// relaxed.
func TestReachableCeilingUnderTheDefaultCase(t *testing.T) {
	inks := []string{"#000000", "#111111", "#1a1d21", "#0c0e12", "#0f0f0f", "#171717"}

	guaranteed, at := math.Inf(1), ""
	for _, ink := range inks {
		for _, in := range Offered() {
			w := achievableWeights(in.Hue, in.Chroma, ink, "#ffffff")
			if len(w) == 0 {
				t.Errorf("ink %s, hue %g: no weight at all is reachable on paper white", ink, in.Hue)
				continue
			}
			if ceiling := w[len(w)-1]; ceiling < guaranteed {
				guaranteed, at = ceiling, fmt.Sprintf("ink %s, hue %g", ink, in.Hue)
			}
		}
	}
	t.Logf("guaranteed ceiling across near-black inks on paper white: %.4f (%s)", guaranteed, at)

	blackOnly, blackBest := math.Inf(1), 0.0
	for _, in := range Offered() {
		w := achievableWeights(in.Hue, in.Chroma, "#000000", "#ffffff")
		blackOnly = math.Min(blackOnly, w[len(w)-1])
		blackBest = math.Max(blackBest, w[len(w)-1])
	}
	t.Logf("under pure black ink specifically: every hue reaches %.4f, the best hue %.4f", blackOnly, blackBest)

	// The three figures Wash's doc comment and reference/ui.md publish.
	// They are prose in two places, so they are held here — a figure
	// quoted in a comment that no test holds is the defect this file has
	// now paid for twice.
	for _, tt := range []struct {
		name  string
		got   float64
		want  float64
		claim string
	}{
		{"guaranteed ceiling across near-black inks", guaranteed, 0.39, "every offered hue reaches 0.39"},
		{"guaranteed ceiling under pure black", blackOnly, 0.43, "under pure black specifically, 0.43"},
		{"the most any hue reaches under black", blackBest, 0.47, "past about 0.47 nothing is reachable at all"},
	} {
		// Published rounded down, so the measurement must be at or above
		// the figure but not so far above that the figure misleads.
		if tt.got < tt.want || tt.got > tt.want+0.01 {
			t.Errorf("%s measures %.4f, but the docs publish %.2f (%q)", tt.name, tt.got, tt.want, tt.claim)
		}
	}

	// The rule-driven band, which is where a default lives.
	for _, w := range []float64{0.10, 0.11, 0.12, 0.13, 0.14} {
		if w > guaranteed {
			t.Errorf("weight %v is above the guaranteed ceiling %.4f, but it is inside the rule-driven band the docs recommend defaulting into", w, guaranteed)
		}
	}
	// And the hand-picked band's floor, which is what makes two bands
	// honest rather than one band and a disclaimer.
	if guaranteed < 0.21 {
		t.Errorf("the guaranteed ceiling is %.4f, below the 0.21 the docs name as the hand-picked band's floor — the second band is then mostly unreachable under the default case and the guidance is fiction", guaranteed)
	}
	if guaranteed < 0.30 {
		t.Errorf("the guaranteed ceiling is %.4f, which leaves the hand-picked band too narrow to be worth naming as a band", guaranteed)
	}
}
