//go:build browser

package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// noShadow: none, or a shadow list whose every colour is fully
// transparent, which is what a token set flat computes to; an element
// with such a list paints nothing.
func noShadow(v string) bool {
	if v == "none" {
		return true
	}
	for _, c := range regexp.MustCompile(`rgba?\([^)]*\)|color\([^)]*\)`).FindAllString(v, -1) {
		if !strings.HasSuffix(c, ", 0)") && !strings.HasSuffix(c, "/ 0)") {
			return false
		}
	}
	return true
}

// noMove: none, or a translate whose every length is 0px (a 0px drop
// computes to "0px", not to none).
func noMove(v string) bool {
	if v == "none" {
		return true
	}
	for _, f := range strings.Fields(v) {
		if f != "0px" {
			return false
		}
	}
	return true
}

// knobStyle reads one computed property of the first switch's knob,
// the ::after getComputedStyle takes as its second argument.
func knobStyle(t *testing.T, ctx context.Context, prop string) string {
	t.Helper()
	return knobStyleOf(t, ctx, "#sw [rst-switch-track]", prop)
}

// knobStyleOf is knobStyle for the track the selector names.
func knobStyleOf(t *testing.T, ctx context.Context, track, prop string) string {
	t.Helper()
	var out string
	mustRun(t, ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector(`+jsStr(track)+`), "::after").getPropertyValue(`+jsStr(prop)+`)`, &out))
	return strings.TrimSpace(out)
}

// systemColour reads what the page resolves one CSS system colour to,
// so a drive can hold an element to Highlight or HighlightText by
// value rather than by a guess at the emulated palette.
func systemColour(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var out string
	mustRun(t, ctx, chromedp.Evaluate(`(() => { const e = document.createElement("span"); e.style.backgroundColor = `+jsStr(name)+`; document.body.append(e); const v = getComputedStyle(e).backgroundColor; e.remove(); return v; })()`, &out))
	return strings.TrimSpace(out)
}

// emulatedRig opens the depth page under an emulated medium, at a width
// set before the navigation. A viewport change after the page has
// loaded turns (hover: hover) off in this Chromium until the next
// navigation, so a hover cell read after a resize sees no rule inside
// a hover query and tests nothing; the query is checked here so that
// cannot happen quietly.
func emulatedRig(t *testing.T, media *emulation.SetEmulatedMediaParams, width, height int64) (*harness.Rig, context.Context) {
	t.Helper()
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	t.Cleanup(cancel)
	mustRun(t, ctx, media, chromedp.EmulateViewport(width, height), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#field", chromedp.ByQuery))
	still(t, ctx)
	var hov bool
	mustRun(t, ctx, chromedp.Evaluate(`matchMedia("(hover: hover)").matches`, &hov))
	if !hov {
		t.Fatalf("(hover: hover) is false at %dx%d; the hover cells would read nothing", width, height)
	}
	mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("dd").open = true, true`, nil))
	return rig, ctx
}

var forcedMedia = emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "forced-colors", Value: "active"}, {Name: "prefers-color-scheme", Value: "light"}})

// TestForcedColoursKeepBordersAndSelection emulates forced colours
// (C6): cards, buttons, fields and summaries have a visible border and
// no painted shadow in every state; the knob is painted and its track
// bordered; the current item in each of the three shells, the current
// menu item, tab and chip use the highlight pair and paint no shadow
// (the tab's lift is a theme shadow the axis never flattened); a
// pressed button does not move; the focus outline is still there. At
// the phone width, on a rig born there, the topbar card's current item
// takes the pair, and the index's current link keeps it hovered and
// pressed: the index ladder painted its surface over the highlight
// and left the text HighlightText, white on white.
func TestForcedColoursKeepBordersAndSelection(t *testing.T) {
	_, ctx := emulatedRig(t, forcedMedia, 1280, 900)
	var active bool
	mustRun(t, ctx, chromedp.Evaluate(`matchMedia("(forced-colors: active)").matches`, &active))
	if !active {
		t.Fatal("forced colours are not emulated; the leg would prove nothing")
	}
	for _, sel := range []string{"#box", "#card", "#list", "#stats", "#b-plain", "#b-primary", "#b-ghost", "#field", "#dd-sum"} {
		for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}} {
			forceState(t, ctx, sel, states...)
			if w, s := computed(t, ctx, sel, "border-top-width"), computed(t, ctx, sel, "border-top-style"); w == "0px" || s == "none" {
				t.Errorf("%s %v under forced colours has border %s %s; want a real border", sel, states, w, s)
			}
			if sh := computed(t, ctx, sel, "box-shadow"); !noShadow(sh) {
				t.Errorf("%s %v under forced colours paints box-shadow %q", sel, states, sh)
			}
			forceState(t, ctx, sel)
		}
	}
	// The mode paints Canvas under everything, so the knob is held
	// against its track, not against transparent: ButtonText on Canvas.
	if knob, track := knobStyle(t, ctx, "background-color"), computed(t, ctx, "#sw [rst-switch-track]", "background-color"); knob == "rgba(0, 0, 0, 0)" || knob == track {
		t.Errorf("the switch knob under forced colours is %s on a %s track; want ButtonText on Canvas", knob, track)
	}
	// Checked, the track is Highlight, and the knob on it must be
	// HighlightText: the pair a palette guarantees contrasts. ButtonText
	// on Highlight need not (a white ButtonText on a cyan Highlight is
	// about 1.25:1), for the ordinary switch and the toggle block's head
	// alike (Astra A4). Highlight and HighlightText are held apart from
	// ButtonText first, or the knob could pass by coincidence.
	hl, hlText, btnText := systemColour(t, ctx, "Highlight"), systemColour(t, ctx, "HighlightText"), systemColour(t, ctx, "ButtonText")
	if hlText == btnText || hl == hlText {
		t.Fatalf("the emulated palette has HighlightText %s, ButtonText %s and Highlight %s; the checked knob could not be told from an unchecked one", hlText, btnText, hl)
	}
	for _, track := range []string{"#sw-on [rst-switch-track]", "#tb-head [rst-switch-track]"} {
		if knob, bg := knobStyleOf(t, ctx, track, "background-color"), computed(t, ctx, track, "background-color"); bg != hl || knob != hlText {
			t.Errorf("the checked switch %s under forced colours paints its knob %s on a %s track; want HighlightText %s on Highlight %s", track, knob, bg, hlText, hl)
		}
	}
	// The hovered knob's dome is a gradient of the theme's knob colours,
	// not of depth tokens; the mode drops every gradient layer (one none
	// per layer), and that is the measurement the block relies on when
	// it sets only the knob's colour.
	forceState(t, ctx, "#sw", "hover")
	if img := knobStyle(t, ctx, "background-image"); strings.Trim(strings.ReplaceAll(img, "none", ""), ", ") != "" {
		t.Errorf("a hovered knob under forced colours still paints background-image %q", img)
	}
	forceState(t, ctx, "#sw")
	// Canvas on CanvasText is what the mode gives every item, so the
	// current item is held against its sibling: the pair must differ.
	highlighted := func(ctx context.Context, cur, other string) {
		t.Helper()
		bg, fg := computed(t, ctx, cur, "background-color"), computed(t, ctx, cur, "color")
		if bg == "rgba(0, 0, 0, 0)" || bg == computed(t, ctx, other, "background-color") || fg == computed(t, ctx, other, "color") {
			t.Errorf("%s under forced colours is %s on %s, the same as %s; want Highlight on HighlightText", cur, fg, bg, other)
		}
		if sh := computed(t, ctx, cur, "box-shadow"); !noShadow(sh) {
			t.Errorf("%s under forced colours paints box-shadow %q", cur, sh)
		}
	}
	for _, pair := range [][2]string{{"#nav-y", "#nav-x"}, {"#cnav-y", "#cnav-x"}, {"#menu-cur", "#menu-a"}, {"#tab-2", "#tab-1"}, {"#page-2", "#page-1"}} {
		highlighted(ctx, pair[0], pair[1])
	}
	forceState(t, ctx, "#b-primary", "hover", "active")
	if got := computed(t, ctx, "#b-primary", "translate"); !noMove(got) {
		t.Errorf("a pressed button moved (%s) under forced colours", got)
	}
	forceState(t, ctx, "#b-primary", "focus", "focus-visible")
	if st := computed(t, ctx, "#b-primary", "outline-style"); st == "none" {
		t.Error("the focus outline is gone under forced colours")
	}
	forceState(t, ctx, "#b-primary")

	_, nctx := emulatedRig(t, forcedMedia, 390, 844)
	highlighted(nctx, "#tnav-y", "#tnav-x")
	rest := computed(t, nctx, "#inav-y", "background-color")
	for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}} {
		forceState(t, nctx, "#inav-y", states...)
		if bg, fg := computed(t, nctx, "#inav-y", "background-color"), computed(t, nctx, "#inav-y", "color"); bg != rest || bg == fg {
			t.Errorf("the index's current link %v under forced colours is %s on %s; want the rest highlight %s under HighlightText", states, fg, bg, rest)
		}
		forceState(t, nctx, "#inav-y")
	}
}

// TestPrintDrawsBordersAndNoDepth emulates print media (C17): no
// painted shadow, no gradient, no tint, no movement in any state;
// cards, fields, buttons and summaries have a --rst-line-strong
// border; a filled button is outlined text in every state (the fills
// are tokens, and print sets them transparent); the knob has a
// printable fill and a ring, and the checked track a fill the off
// track lacks; current items are flat. The check is for borders
// because "backgrounds disabled" is a print setting that drops
// backgrounds and leaves borders and outlines, so a boundary drawn as
// one of those is the one that survives it. The phone width is walked
// on a rig born there, because the topbar's card and its hover are
// phone-only rules inside a hover query; and a dark-scheme press on a
// page opened again, because day's dark press numbers sit in
// :root[data-theme] blocks that outrank html:root.
func TestPrintDrawsBordersAndNoDepth(t *testing.T) {
	printMedia := emulation.SetEmulatedMedia().WithMedia("print")
	rig, ctx := emulatedRig(t, printMedia, 1280, 900)
	flat := func(ctx context.Context, sel string, states ...string) {
		t.Helper()
		forceState(t, ctx, sel, states...)
		if sh, img, tr := computed(t, ctx, sel, "box-shadow"), computed(t, ctx, sel, "background-image"), computed(t, ctx, sel, "translate"); !noShadow(sh) || img != "none" || !noMove(tr) {
			t.Errorf("%s %v in print: box-shadow %q background-image %q translate %s; want nothing painted and nothing moved", sel, states, sh, img, tr)
		}
		forceState(t, ctx, sel)
	}
	for _, sel := range []string{"#box", "#card", "#list", "#stats", "#b-plain", "#b-primary", "#b-danger", "#b-ghost", "#field", "#dd-sum", "#row-a", "#lra", "#tile", "#nav-x", "#nav-y", "#menu-a", "#opt-active", "#tab-1", "#tab-2", "#page-1", "#page-2"} {
		for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}} {
			flat(ctx, sel, states...)
		}
	}
	// day's light --rst-line-strong is #868691 (its comment says why it
	// is darker than the mockup's).
	const line = "rgb(134, 134, 145)"
	for _, sel := range []string{"#box", "#card", "#list", "#stats", "#b-plain", "#b-primary", "#b-ghost", "#field", "#dd-sum", "#page-2"} {
		if c, w := computed(t, ctx, sel, "border-top-color"), computed(t, ctx, sel, "border-top-width"); c != line || w != "1px" {
			t.Errorf("%s in print has border %s %s, want 1px --rst-line-strong %s", sel, c, w, line)
		}
	}
	for _, sel := range []string{"#b-primary", "#b-danger", "#b-plain"} {
		for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}} {
			forceState(t, ctx, sel, states...)
			if bg, fg := computed(t, ctx, sel, "background-color"), computed(t, ctx, sel, "color"); bg != "rgba(0, 0, 0, 0)" || fg != "rgb(24, 24, 27)" {
				t.Errorf("%s %v in print is %s on %s; want outlined text", sel, states, fg, bg)
			}
			forceState(t, ctx, sel)
		}
	}
	if knob, ring := knobStyle(t, ctx, "background-color"), knobStyle(t, ctx, "outline-style"); knob != "rgb(24, 24, 27)" || ring != "solid" {
		t.Errorf("the switch knob prints as %s with outline %s; want the text colour inside a solid ring", knob, ring)
	}
	// On and off must differ by more than the knob's end: the checked
	// track (a switch's and a toggle block's) keeps the strong line.
	if off, on, head := computed(t, ctx, "#sw [rst-switch-track]", "background-color"), computed(t, ctx, "#sw-on [rst-switch-track]", "background-color"), computed(t, ctx, "#tb [rst-switch-track]", "background-color"); off != "rgba(0, 0, 0, 0)" || on != line || head != line {
		t.Errorf("tracks print off %s, on %s, toggle-block on %s; want an empty ring and two filled with %s", off, on, head, line)
	}
	// Hover tints: the row's, the tile's and the menu item's are tokens
	// of their own, and print sets each to the surface or to nothing.
	for _, sel := range []string{"#row-a", "#tile", "#menu-a"} {
		forceState(t, ctx, sel, "hover")
		if got := computed(t, ctx, sel, "background-color"); got != "rgba(0, 0, 0, 0)" && got != "rgb(255, 255, 255)" {
			t.Errorf("hovered %s in print is tinted %s", sel, got)
		}
		forceState(t, ctx, sel)
	}

	_, nctx := emulatedRig(t, printMedia, 390, 844)
	for _, sel := range []string{"#tnav-x", "#tnav-y", "#nav-y", "#inav-y", "#b-primary", "#card"} {
		for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}} {
			flat(nctx, sel, states...)
		}
	}
	// The card's hover is its own token, read only inside a phone-and-
	// hover query; print sets it to nothing.
	forceState(t, nctx, "#tnav-x", "hover")
	if got := computed(t, nctx, "#tnav-x", "background-color"); got != "rgba(0, 0, 0, 0)" {
		t.Errorf("a hovered topbar phone-card link in print is tinted %s", got)
	}
	forceState(t, nctx, "#tnav-x")
	if got := computed(t, nctx, "#tnav-y", "background-color"); got != "rgba(0, 0, 0, 0)" {
		t.Errorf("topbar phone-card current item in print is filled %s; want no fill", got)
	}

	// A fresh navigation on the first rig: open() sets the viewport
	// before it navigates, so the hover query is on again.
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	var hov bool
	mustRun(t, ctx, chromedp.Evaluate(`matchMedia("(hover: hover)").matches`, &hov))
	if !hov {
		t.Fatal("(hover: hover) is false after the second navigation; the dark press would be the tap rule alone")
	}
	mustRun(t, ctx, chromedp.Evaluate(`document.documentElement.dataset.theme = "dark", true`, nil))
	flat(ctx, "#b-primary", "hover", "active")
	flat(ctx, "#b-plain", "active")
}
