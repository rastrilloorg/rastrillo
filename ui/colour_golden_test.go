package ui

import (
	"math"
	"math/rand"
	"testing"
)

// The golden tables: the colour engine's cross-implementation contract,
// written down as data.
//
// Everything else in colour_test.go asks whether the engine is
// self-consistent — whether it clears the floor it declares, whether it
// gives the same answer twice. This file asks the different question, and
// it is the one two clients rendering one document actually depend on:
// does it give THIS answer? A property test cannot see a change that
// preserves every property and moves every colour, and that is exactly
// the change that breaks a downstream app: swapping FNV-1a for FNV-1,
// rotating the offered hues by 25 degrees, compacting the permitted hues
// before probing instead of probing past them. Each of those keeps the
// engine correct by every other measure here and silently repaints every
// document every client has ever stored.
//
// So the tables below are LITERALS, not arithmetic. A table recomputed at
// test time by the same route the code takes is one measurement twice
// (design doc §7-v2), which is the failure this project keeps rediscovering.
//
// Changing any of these numbers is a break for CARLOS Docs and CARLOS
// Sheets, whose stored documents resolve through them. That is the point:
// the table makes it a decision somebody signs rather than a diff nobody
// notices.

// ─── The offered set ─────────────────────────────────────────────────

// TestOfferedSetIsPinned fixes the twelve hues and their chroma. A
// rotation, a resize or a chroma change lands here first, where a reader
// can see what it costs.
func TestOfferedSetIsPinned(t *testing.T) {
	want := []Intent{
		{25, 0.14}, {55, 0.14}, {85, 0.14}, {115, 0.14},
		{145, 0.14}, {175, 0.14}, {205, 0.14}, {235, 0.14},
		{265, 0.14}, {295, 0.14}, {325, 0.14}, {355, 0.14},
	}
	got := Offered()
	if len(got) != len(want) {
		t.Fatalf("Offered() has %d intents, want %d — the capacity Allocate reports is part of the contract", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Offered()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// ─── Allocation ──────────────────────────────────────────────────────

// The expected hues below were computed by a SECOND implementation —
// FNV-1a/64, the sort and the probe written out from their definitions in
// Python, not read out of colour.go — and the two agreed on every row.
// That is what makes this table a control rather than a photograph of
// whatever the code did on the day.
//
// Two rows are worth checking by hand, because they are the ones carrying
// the rules:
//
//   - "Member-7" and "member-3" both prefer slot 11. "Member-7" sorts
//     first ('M' is 0x4D, 'm' is 0x6D), so it keeps 355°, and "member-3"
//     probes past the end of the set and wraps to slot 0, at 25°. That is
//     "displace the later arrival" with arrival replaced by byte order,
//     and it is also the wrap-around case.
//   - In the avoid case the only change is that slot 0 is off limits, and
//     "member-3" therefore walks 11 → 0 (avoided) → 1 (taken) → 2 (taken)
//     → 3, landing at 115° and pushing "member-7" from 115° to 145°. An
//     avoided hue displaced it exactly the way a taken hue does, which is
//     the rule the spec spends a paragraph on.

type allocCase struct {
	name  string
	keys  []string
	avoid []float64
	want  []float64 // aligned with keys
	sep   bool
}

var allocGolden = []allocCase{
	{
		// No avoid. Contains one collision ("Member-7" vs "member-3")
		// which resolves by wrapping around the end of the set.
		name: "eight keys, no avoid",
		keys: []string{"member-7", "member-3", "member-11", "guest-row-902",
			"member-1", "aaa", "zzz", "Member-7"},
		avoid: nil,
		want:  []float64{115, 25, 265, 325, 55, 85, 295, 355},
		sep:   true,
	},
	{
		// The same keys with one hue withheld. THIS is the case that
		// tells the specified probe apart from the forbidden one:
		// compacting the permitted hues and probing over the compacted
		// list gives a different answer for seven of these eight keys,
		// while satisfying every other property the suite checks.
		name: "the same keys, 25° withheld",
		keys: []string{"member-7", "member-3", "member-11", "guest-row-902",
			"member-1", "aaa", "zzz", "Member-7"},
		avoid: []float64{25},
		want:  []float64{145, 115, 265, 325, 55, 85, 295, 355},
		sep:   true,
	},
	{
		// Three keys on one slot. A chain resolves only because the
		// probe is deterministic: the third key's destination cannot
		// depend on the order it was observed in.
		name:  "a three-key collision chain",
		keys:  []string{"chain-0", "chain-15", "chain-24"},
		avoid: nil,
		want:  []float64{115, 145, 175},
		sep:   true,
	},
	{
		// Past capacity. Fifteen keys into twelve hues: every hue used,
		// three of them twice, and the flag false. Pinning the repeats
		// matters as much as pinning the singles — "it repeats somehow"
		// is not a contract two clients can both implement.
		name: "fifteen keys into twelve hues",
		keys: []string{"person-0", "person-1", "person-2", "person-3", "person-4",
			"person-5", "person-6", "person-7", "person-8", "person-9",
			"person-10", "person-11", "person-12", "person-13", "person-14"},
		avoid: nil,
		want: []float64{175, 325, 115, 265, 85, 235, 295, 145, 55, 205,
			205, 55, 145, 355, 25},
		sep: false,
	},
}

// TestAllocateMatchesTheGoldenTable is the gate that pins the hash, the
// set, the probe and the destination at once. Every one of those is a
// thing a second implementation has to agree about, and none of them was
// checkable before this table existed.
func TestAllocateMatchesTheGoldenTable(t *testing.T) {
	for _, tc := range allocGolden {
		t.Run(tc.name, func(t *testing.T) {
			got, sep := Allocate(tc.keys, tc.avoid)
			if len(got) != len(tc.want) {
				t.Fatalf("Allocate returned %d intents for %d keys", len(got), len(tc.keys))
			}
			for i := range tc.want {
				if got[i].Hue != tc.want[i] {
					t.Errorf("key %q got hue %v, want %v", tc.keys[i], got[i].Hue, tc.want[i])
				}
			}
			if sep != tc.sep {
				t.Errorf("separated = %v, want %v", sep, tc.sep)
			}
		})
	}
}

// TestGoldenAllocationSurvivesShuffling joins the golden table to the
// order-independence property, which is the pair of claims a second
// client actually needs: the same answer as this table, from any order.
//
// On its own the shuffle test cannot see a constant allocator — a
// constant is perfectly order-invariant. On its own the table cannot see
// an allocator that is right for the order the table was written in.
// Together they close both.
func TestGoldenAllocationSurvivesShuffling(t *testing.T) {
	tc := allocGolden[1] // the avoid case: the most specific one
	want := map[string]float64{}
	for i, k := range tc.keys {
		want[k] = tc.want[i]
	}

	rng := rand.New(rand.NewSource(20260831))
	order := append([]string(nil), tc.keys...)
	for trial := 0; trial < 500; trial++ {
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		got, sep := Allocate(order, tc.avoid)
		if sep != tc.sep {
			t.Fatalf("trial %d: separated = %v, want %v (order %v)", trial, sep, tc.sep, order)
		}
		for i, k := range order {
			if got[i].Hue != want[k] {
				t.Fatalf("trial %d: key %q got hue %v in order %v, want %v from the golden table",
					trial, k, got[i].Hue, order, want[k])
			}
		}
	}
}

// ─── Resolved colours ────────────────────────────────────────────────

// swatchGolden is every offered intent resolved against four
// backgrounds: paper white, signal's light page, a mid grey (the hardest
// case, where feasible fills exist in both directions) and day's dark
// page.
//
// Unlike the allocation table this one is a photograph, and it is
// labelled as one: it was produced by this implementation, so it is not
// evidence that these hexes are the RIGHT colours. What it is evidence of
// is that they have not MOVED. Its job is to make a performance rewrite
// of the lightness search — which must not change a single returned
// colour — provable rather than asserted, and to put a caller's stored
// XLSX fill under the same protection as their stored intent.
//
// Correctness of these colours is established elsewhere: the floors are
// asserted from literals in TestFloorConstantsAreTheStandard and
// TestPairHoldsItsFloorsEverywhere, and the conversion is checked against
// the achromatic axis and the gamut boundary in TestOklchHexControls.
var swatchGolden = []struct {
	background string
	hue        float64
	fill, on   string
}{
	{"#ffffff", 25, "#e2726b", "#290b0a"},
	{"#ffffff", 55, "#d77c36", "#260f00"},
	{"#ffffff", 85, "#bb8c00", "#1e1400"},
	{"#ffffff", 115, "#8f9b1d", "#161800"},
	{"#ffffff", 145, "#51a556", "#051c07"},
	{"#ffffff", 175, "#00a68a", "#001b15"},
	{"#ffffff", 205, "#00a3b0", "#001a1d"},
	{"#ffffff", 235, "#009dda", "#001926"},
	{"#ffffff", 265, "#6a91ea", "#0b152c"},
	{"#ffffff", 295, "#9e84e4", "#18102a"},
	{"#ffffff", 325, "#c378c7", "#210d22"},
	{"#ffffff", 355, "#db719d", "#270b17"},
	{"#f4f5f8", 25, "#da6b65", "#290b0a"},
	{"#f4f5f8", 55, "#d0762e", "#260f00"},
	{"#f4f5f8", 85, "#b28600", "#1e1400"},
	{"#f4f5f8", 115, "#899411", "#161800"},
	{"#f4f5f8", 145, "#4a9e50", "#051c07"},
	{"#f4f5f8", 175, "#009f84", "#001b15"},
	{"#f4f5f8", 205, "#009ba8", "#001a1d"},
	{"#f4f5f8", 235, "#0096d0", "#001926"},
	{"#f4f5f8", 265, "#638ae2", "#0b152c"},
	{"#f4f5f8", 295, "#987ddc", "#18102a"},
	{"#f4f5f8", 325, "#bc71c0", "#210d22"},
	{"#f4f5f8", 355, "#d36a96", "#270b17"},
	{"#808080", 25, "#72020e", "#fffbfa"},
	{"#808080", 55, "#572900", "#fffbf8"},
	{"#808080", 85, "#473300", "#fffbf4"},
	{"#808080", 115, "#343900", "#fbffe8"},
	{"#808080", 145, "#00400b", "#f5fff5"},
	{"#808080", 175, "#003e32", "#f3fffb"},
	{"#808080", 205, "#003c42", "#f4feff"},
	{"#808080", 235, "#003a54", "#f8fcff"},
	{"#808080", 265, "#132e7f", "#fafcff"},
	{"#808080", 295, "#42227a", "#fcfbff"},
	{"#808080", 325, "#5c1461", "#fffaff"},
	{"#808080", 355, "#6c043e", "#fffafc"},
	{"#111418", 25, "#aa403d", "#fffbfa"},
	{"#111418", 55, "#9b4e00", "#fffbf8"},
	{"#111418", 85, "#7e5e00", "#fffbf4"},
	{"#111418", 115, "#606800", "#fbffe8"},
	{"#111418", 145, "#177225", "#f5fff5"},
	{"#111418", 175, "#00705d", "#f3fffb"},
	{"#111418", 205, "#006e77", "#f4feff"},
	{"#111418", 235, "#006a95", "#f8fcff"},
	{"#111418", 265, "#3c5fb4", "#fafcff"},
	{"#111418", 295, "#6e52ad", "#fcfbff"},
	{"#111418", 325, "#8f4794", "#fffaff"},
	{"#111418", 355, "#a33f6c", "#fffafc"},
}

// TestPairMatchesTheGoldenSwatches is the refactor pin. If it fails
// alongside a change that was meant to be behaviour-preserving, the
// change was not behaviour-preserving.
func TestPairMatchesTheGoldenSwatches(t *testing.T) {
	if len(swatchGolden) != 4*len(Offered()) {
		t.Fatalf("the golden swatch table has %d rows for %d intents on 4 backgrounds — a row went missing, and a table with a hole in it protects nothing", len(swatchGolden), len(Offered()))
	}
	for _, tc := range swatchGolden {
		sw, err := Pair(tc.hue, 0.14, tc.background)
		if err != nil {
			t.Errorf("Pair(%v, 0.14, %s): %v", tc.hue, tc.background, err)
			continue
		}
		if sw.Fill != tc.fill || sw.On != tc.on {
			t.Errorf("Pair(%v, 0.14, %s) = fill %s / on %s, want fill %s / on %s",
				tc.hue, tc.background, sw.Fill, sw.On, tc.fill, tc.on)
		}
	}
}

// ─── Resolved washes ─────────────────────────────────────────────────

// washGolden is the same instrument as swatchGolden, pointed at the
// function that needs it more.
//
// swatchGolden's own comment justifies itself as protecting "a caller's
// stored XLSX fill", and Wash is the XLSX function: it is what a
// spreadsheet calls when somebody clicks yellow, and its output is what
// gets written into a workbook and read back. It went a whole round
// without one, and the cost showed. washSlack — a constant the search's
// soundness rests on, which turned out to have been set below the bound
// its own premise required — moved from 0.02 to 0.08 and not one test
// noticed, because no property changed: every wash still cleared both
// floors, still sat nearest its target, still carried the caller's ink.
// A photograph is what catches a constant moving when no property does.
//
// Like swatchGolden this is a photograph and not a proof: it was produced
// by this implementation, so it is evidence that these colours have not
// MOVED, never that they are right. Correctness lives in the floors, the
// scan control, and the premise gates.
//
// The rows are chosen to span the contract rather than to sample it:
// both SeparationMet states, a request under the floor, a request past
// the ceiling, the cross case where a pinned ink forces a wash heavier
// than asked, and three rows that are known to be sensitive to washSlack
// — those return a different colour at slack 0, which is the mutation
// that survived the last round.
var washGolden = []struct {
	hue, chroma, separation float64
	ink, background         string
	fill, on                string
	sep                     float64
	met                     bool
}{
	// The default case: near-black ink on a white canvas, at a weight in
	// the rule-driven band. This is what a spreadsheet cell is before
	// anybody touches it, so these three rows are the ones a regression
	// would be felt through first.
	{115, 0.14, 0.12, "#000000", "#ffffff", "#f2ffa2", "#000000", 0.120600, true},
	{25, 0.14, 0.12, "#000000", "#ffffff", "#ffcfca", "#000000", 0.118805, true},
	{205, 0.14, 0.12, "#000000", "#ffffff", "#9cf5ff", "#000000", 0.119447, true},

	// The hand-picked band, still comfortably reachable under black ink.
	{115, 0.14, 0.21, "#000000", "#ffffff", "#c9d663", "#000000", 0.210616, true},
	{145, 0.14, 0.38, "#000000", "#ffffff", "#50a456", "#000000", 0.379577, true},

	// A request under the floor: raised to MinSeparation, and MET, since
	// it came back heavier than what was asked for.
	{115, 0.14, 0.001, "#000000", "#ffffff", "#fbffe6", "#000000", 0.034244, true},

	// Constrained, both from the doc comment's worked example and from
	// just past a hue's own ceiling. These are the SeparationMet == false
	// rows, and without them the flag would be pinned in one state.
	{265, 0.14, 0.63, "#000000", "#ffffff", "#4d72c8", "#000000", 0.454940, false},
	{355, 0.14, 0.45, "#000000", "#ffffff", "#b9537f", "#000000", 0.439618, false},

	// A dark canvas with the theme's own ink.
	{115, 0.14, 0.12, "#e8eaed", "#111418", "#2a2e00", "#e8eaed", 0.121540, true},
	{265, 0.14, 0.30, "#e8eaed", "#111418", "#3152a5", "#e8eaed", 0.300475, true},

	// The cross case: an author pinned black and the reader chose the
	// dark theme. Black ink needs a light fill whatever surrounds it, so
	// asking for 0.12 returns 0.39 — heavier than requested, and met,
	// because being given more weight than you asked for is not
	// something to warn a user about.
	{115, 0.14, 0.12, "#000000", "#111418", "#727c00", "#000000", 0.392508, true},
	{25, 0.14, 0.12, "#000000", "#111418", "#bf534e", "#000000", 0.415987, true},
	{115, 0.14, 0.12, "#ffffff", "#ffffff", "#737c00", "#ffffff", 0.458287, true},

	// Known to be sensitive to washSlack: at slack 0 the walk stops
	// before the true answer and the first of these comes back #090200
	// instead. They are in the table for exactly that reason.
	{50, 0.05, 0.90, "#ffffff", "#ffffff", "#080200", "#ffffff", 0.900097, true},
	{50, 0.14, 0.90, "#e8eaed", "#f4f5f8", "#030100", "#e8eaed", 0.895126, true},
	{262, 0.05, 0.12, "#ffffff", "#111418", "#000106", "#ffffff", 0.119736, true},
}

// TestWashMatchesTheGoldenTable is the refactor pin for Wash. If it fails
// alongside a change that was meant to be behaviour-preserving, the
// change was not behaviour-preserving.
func TestWashMatchesTheGoldenTable(t *testing.T) {
	var met, unmet int
	for _, tc := range washGolden {
		sw, err := Wash(tc.hue, tc.chroma, tc.separation, tc.ink, tc.background)
		if err != nil {
			t.Errorf("Wash(%v, %v, %v, %s, %s): %v", tc.hue, tc.chroma, tc.separation, tc.ink, tc.background, err)
			continue
		}
		if sw.Fill != tc.fill || sw.On != tc.on {
			t.Errorf("Wash(%v, %v, %v, %s, %s) = fill %s / on %s, want fill %s / on %s",
				tc.hue, tc.chroma, tc.separation, tc.ink, tc.background, sw.Fill, sw.On, tc.fill, tc.on)
		}
		if math.Abs(sw.Separation-tc.sep) > 1e-6 {
			t.Errorf("Wash(%v, %v, %v, %s, %s) separation = %.6f, want %.6f",
				tc.hue, tc.chroma, tc.separation, tc.ink, tc.background, sw.Separation, tc.sep)
		}
		if sw.SeparationMet != tc.met {
			t.Errorf("Wash(%v, %v, %v, %s, %s) SeparationMet = %v, want %v",
				tc.hue, tc.chroma, tc.separation, tc.ink, tc.background, sw.SeparationMet, tc.met)
		}
		if sw.SeparationRequested != tc.separation {
			t.Errorf("Wash(%v, %v, %v, %s, %s) SeparationRequested = %v, want %v",
				tc.hue, tc.chroma, tc.separation, tc.ink, tc.background, sw.SeparationRequested, tc.separation)
		}
		if tc.met {
			met++
		} else {
			unmet++
		}
	}

	// A table that only ever recorded one state of the flag would pin the
	// colours and leave the flag free to be anything.
	if met == 0 || unmet == 0 {
		t.Errorf("the table holds %d met and %d unmet rows; it has to pin both states or it is not pinning the flag at all", met, unmet)
	}
}
