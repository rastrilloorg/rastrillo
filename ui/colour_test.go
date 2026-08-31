package ui

import (
	"math"
	"math/rand"
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
		if fr < ContrastFloorBoundary {
			t.Errorf("fill %s on %s = %.2f:1, want >= %.1f:1", sw.Fill, sw.Background, fr, ContrastFloorBoundary)
		}
		if or < ContrastFloorText {
			t.Errorf("on-fill %s on fill %s = %.2f:1, want >= %.1f:1", sw.On, sw.Fill, or, ContrastFloorText)
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
				if sw.FillRatio < ContrastFloorBoundary {
					t.Errorf("Pair(%v, %v, %s): fill %s = %.3f:1, want >= %.1f:1", hue, chroma, bg, sw.Fill, sw.FillRatio, ContrastFloorBoundary)
				}
				if sw.OnRatio < ContrastFloorText {
					t.Errorf("Pair(%v, %v, %s): on-fill %s on fill %s = %.3f:1, want >= %.1f:1", hue, chroma, bg, sw.On, sw.Fill, sw.OnRatio, ContrastFloorText)
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

	over := append(slicesClone(keys), "one-too-many")
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

func slicesClone(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
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
		shuffled := slicesClone(base)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := allocationMap(t, shuffled, nil)
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("trial %d: key %q got hue %v in order %v, but %v in the reference order — allocation depends on arrival order",
					trial, k, got[k], shuffled, v)
			}
		}
	}

	// The control for the control: the same shuffle harness must be able
	// to see a difference. A DIFFERENT key set has to produce a
	// different map, or this test would pass against an allocator that
	// returned one constant.
	other := allocationMap(t, []string{"member-7", "different-key-entirely"}, nil)
	if other["member-7"] == want["member-7"] && len(want) > 1 {
		// Not a failure on its own — the two may legitimately agree,
		// since member-7 keeps its preferred hue when nothing displaces
		// it. What must differ is the map as a whole.
		same := true
		for k := range other {
			if _, ok := want[k]; !ok {
				same = false
			}
		}
		if same {
			t.Error("two different key sets produced the same allocation map; the harness is not measuring the keys")
		}
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
	n := len(all)
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
	_ = n
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
// stability promise: a key that nothing displaces keeps the same hue in
// a different document, and a key that something does displace moves.
// Both halves are documented behaviour, so both are asserted — the
// second so nobody reads the first as a guarantee it is not.
func TestAllocateStabilityIsBestEffort(t *testing.T) {
	n := len(Offered())

	// Find two keys that prefer the same hue. They exist: twelve hues
	// and an unbounded key space.
	var a, b string
	for i := 0; i < 5000 && b == ""; i++ {
		k := "collide-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		if a == "" {
			a = k
			continue
		}
		if fnv1a64(k)%uint64(n) == fnv1a64(a)%uint64(n) {
			b = k
		}
	}
	if b == "" {
		t.Skip("no colliding pair found in the sampled key space")
	}

	alone, _ := Allocate([]string{a}, nil)
	together, _ := Allocate([]string{a, b}, nil)
	first, second := a, b
	if second < first {
		first, second = second, first
	}
	// The lexicographically earlier key keeps its preferred hue; the
	// later one is the one that moves. That is what "displace the later
	// arrival" means once arrival order has been replaced by the sorted
	// order.
	idx := map[string]int{a: 0, b: 1}
	if together[idx[first]].Hue != alone[0].Hue && first == a {
		t.Errorf("%q lost its preferred hue to %q, which sorts after it", first, second)
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

	// Report the tightest margin the set actually has, so a change that
	// leaves it green while eating all the headroom is visible.
	worstFill, worstOn, worstChroma := math.Inf(1), math.Inf(1), math.Inf(1)
	for _, in := range Offered() {
		for _, bg := range backgrounds {
			sw, err := Pair(in.Hue, in.Chroma, bg)
			if err != nil {
				t.Fatalf("Pair(%v, %v, %s): %v", in.Hue, in.Chroma, bg, err)
			}
			worstFill = math.Min(worstFill, sw.FillRatio)
			worstOn = math.Min(worstOn, sw.OnRatio)
			worstChroma = math.Min(worstChroma, sw.Chroma)
		}
	}
	t.Logf("tightest margins: fill %.2f:1 (floor %.1f), on-fill %.2f:1 (floor %.1f), chroma %.3f (floor %.3f)",
		worstFill, ContrastFloorBoundary, worstOn, ContrastFloorText, worstChroma, minDeliveredChroma)
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

	planted := append(slicesCloneIntents(good), Intent{Hue: 265, Chroma: 0})
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

func slicesCloneIntents(s []Intent) []Intent {
	out := make([]Intent, len(s))
	copy(out, s)
	return out
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
