//go:build browser

package ui

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The day switch (the switch rework, 2026-10-24): its size comes from
// theme tokens with a fine-pointer set and a touch set; the track sits
// in and the knob stands on a drop shadow that deepens on hover; the
// on colour follows the knob, dragged or clicked; a released knob
// settles at the nearer end with a firm curve. plain and signal keep
// today's switch (A15).

// swapTheme replaces the page's theme stylesheet with the named
// theme's CSS, inline, so one page can be read under every theme.
func swapTheme(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	css, ok := ThemeCSS(name)
	if !ok {
		t.Fatalf("no theme %q", name)
	}
	mustRun(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => {
		const l = document.querySelector('link[href="/theme.css"]');
		if (l) l.remove();
		for (const s of document.querySelectorAll("style[data-swap]"))
			s.remove();
		const s = document.createElement("style");
		s.dataset.swap = "1";
		s.textContent = `+jsStr(string(css))+`;
		document.head.append(s);
		return true;
	})()`))
}

// daySwitchBody is a page with an on switch, an off switch and a
// toggle block with an on head, and the counters switchJS reads.
const daySwitchBody = `<div rst-page><p><label rst-switch id="on"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span> On</label></p>` +
	`<p><label rst-switch id="off"><input type="checkbox"><span rst-switch-track aria-hidden="true"></span> Off</label></p>` +
	`<p><label rst-switch id="dis"><input type="checkbox" checked disabled><span rst-switch-track aria-hidden="true"></span> Locked</label></p>` +
	`<div rst-tblock id="tb"><label rst-tblock-head id="head"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span><span rst-tblock-title>Block</span></label><div rst-tblock-body>body</div></div></div>` +
	`<script>window.changes = {}; window.inputs = {}; for (const [k, m] of [["change", window.changes], ["input", window.inputs]]) document.addEventListener(k, e => { const l = e.target.closest("[rst-switch],[rst-tblock-head]"); if (l) m[l.id] = (m[l.id] || 0) + 1; });</script>`

// TestTheSwitchTakesItsSizeFromTheTheme: the track's width and height
// and the knob's size are theme tokens in two sets, one for a fine
// pointer and one for a coarse one, and the knob's travel follows from
// them. day is 40x23 with a 19px knob under a mouse and 51x31 with a
// 27px knob on a touch screen, the toggle block's head the same; plain
// and signal are 34x19 with a 15px knob under both (A15). The label's
// minimum block size, the target floor, is not the switch's to change.
func TestTheSwitchTakesItsSizeFromTheTheme(t *testing.T) {
	type size struct{ W, H, K, Run float64 }
	want := map[bool]map[string]size{
		false: {"day": {40, 23, 19, 17}, "plain": {34, 19, 15, 15}, "signal": {34, 19, 15, 15}},
		true:  {"day": {51, 31, 27, 20}, "plain": {34, 19, 15, 15}, "signal": {34, 19, 15, 15}},
	}
	for _, coarse := range []bool{false, true} {
		name := "fine"
		if coarse {
			name = "coarse"
		}
		t.Run(name, func(t *testing.T) {
			rig := sizingRig(t, coarse, map[string]string{"/": sizingDoc("switch size", daySwitchBody)})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(chromedp.CSS("#tb")))
			requirePointer(t, ctx, coarse)
			still(t, ctx)
			floor := computed(t, ctx, "#on", "min-block-size")
			for _, theme := range []string{"day", "plain", "signal"} {
				swapTheme(t, ctx, theme)
				w := want[coarse][theme]
				for _, id := range []string{"on", "off", "head"} {
					s := read(t, ctx, id)
					if s.W != w.W || s.H != w.H || s.K != w.K {
						t.Errorf("%s, %s pointer: #%s is %gx%g with a %gpx knob; want %gx%g with %gpx", theme, name, id, s.W, s.H, s.K, w.W, w.H, w.K)
					}
					if on := id != "off"; on && math.Abs(s.X-w.Run) > 0.5 || !on && math.Abs(s.X) > 0.5 {
						t.Errorf("%s, %s pointer: #%s's knob is moved %.1fpx; want %g on and 0 off, the track's width less its height", theme, name, id, s.X, w.Run)
					}
				}
				if got := computed(t, ctx, "#on", "min-block-size"); got != floor {
					t.Errorf("%s, %s pointer: the label's min-block-size is %s, was %s under day; the target floor is not the switch's to change", theme, name, got, floor)
				}
			}
		})
	}
}

// shadowBlur reads one box-shadow layer's blur radius, the third
// length after its colour ("rgba(…) 0px 1px 2px 0px inset" is 2).
func shadowBlur(t *testing.T, layer string) float64 {
	t.Helper()
	i := strings.LastIndex(layer, ")")
	f := strings.Fields(layer[i+1:])
	if len(f) < 4 {
		t.Fatalf("box-shadow layer %q: want a colour and four lengths", layer)
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(f[2], "px"), 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// painted is the layers of a box-shadow that paint: the ones whose
// colour is not transparent, in order. A transparent layer's geometry
// is nothing on screen, so two shadows with the same painted layers
// are the same pixels.
func painted(v string) []string {
	var out []string
	for _, l := range layers(v) {
		if !strings.HasPrefix(l, "rgba(0, 0, 0, 0)") {
			out = append(out, l)
		}
	}
	return out
}

// paintsNothing is true when no layer of a box-shadow paints.
func paintsNothing(v string) bool { return painted(v) == nil }

// TestTheTrackSitsInAndTheKnobLiftsOnHover (the switch rework, depth
// like the buttons'): in day the track is sunk by two inset layers,
// the contact shadow and an ambient hairline rim; the knob stands on
// the theme's knob shadow, two layers in day (a drop and a hairline
// ring), over its own contact shadow and under its highlight; and
// hovered it lifts: a longer contact shadow and the theme's hover knob
// shadow, deeper than the rest one. The toggle block's head lifts the
// same. A disabled switch hovered keeps its rest shadow. plain and
// signal declare no depth: their track paints no shadow, and their
// knob paints its own theme shadow, the same layers hovered as at
// rest (A15). Frozen first, for forceState.
func TestTheTrackSitsInAndTheKnobLiftsOnHover(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("switch depth", daySwitchBody)}, harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(chromedp.CSS("#tb")))
	still(t, ctx)
	knob := func(sel string) string { return knobStyleOf(t, ctx, sel+" [rst-switch-track]", "box-shadow") }
	track := func(sel string) string { return computed(t, ctx, sel+" [rst-switch-track]", "box-shadow") }
	hovered := func(sel string, read func(string) string) string {
		forceState(t, ctx, sel, "hover")
		v := read(sel)
		forceState(t, ctx, sel)
		return v
	}
	for _, theme := range []string{"day", "plain", "signal"} {
		swapTheme(t, ctx, theme)
		if theme != "day" {
			for _, sel := range []string{"#off", "#on", "#head"} {
				if tr, k, hk := track(sel), painted(knob(sel)), painted(hovered(sel, knob)); !paintsNothing(tr) || len(k) == 0 || strings.Join(k, ", ") != strings.Join(hk, ", ") {
					t.Errorf("%s: %s paints track %q, knob %q, hovered knob %q; a flat theme's track paints nothing and its knob paints its own shadow, hovered or not", theme, sel, tr, k, hk)
				}
			}
			continue
		}
		if tr := layers(track("#off")); len(tr) != 2 || !strings.HasSuffix(tr[0], "0px 1px 2px 0px inset") || !strings.HasSuffix(tr[1], "0px 0px 0px 0.5px inset") || paintsNothing(track("#off")) {
			t.Errorf("day: the track's box-shadow is %q; want two painted inset layers, a 1px 2px contact shadow and a 0.5px rim", track("#off"))
		}
		for _, sel := range []string{"#off", "#on", "#head"} {
			rest, lift := layers(knob(sel)), layers(hovered(sel, knob))
			if len(rest) < 4 || !strings.HasSuffix(rest[0], "inset") || paintsNothing(knob(sel)) {
				t.Errorf("day: %s's knob at rest has box-shadow %q; want the highlight inset first, its contact shadow, and the theme's two-layer knob shadow", sel, knob(sel))
				continue
			}
			if len(lift) != len(rest) || shadowBlur(t, lift[1]) <= shadowBlur(t, rest[1]) || shadowBlur(t, lift[2]) <= shadowBlur(t, rest[2]) {
				t.Errorf("day: %s hovered has knob box-shadow %q, at rest %q; the knob should lift, with a longer contact shadow and a deeper knob shadow", sel, strings.Join(lift, ", "), strings.Join(rest, ", "))
			}
		}
		if rest, lift := knob("#dis"), hovered("#dis", knob); lift != rest {
			t.Errorf("day: a disabled switch hovered has knob box-shadow %q, at rest %q; want no lift", lift, rest)
		}
	}
}

// rgbOf parses an rgb() triple (canonColours' output) into channels.
func rgbOf(t *testing.T, v string) (r, g, b float64) {
	t.Helper()
	if _, err := fmt.Sscanf(canonColours(v), "rgb(%f, %f, %f)", &r, &g, &b); err != nil {
		t.Fatalf("%q is not an rgb() colour", v)
	}
	return
}

// mixed is color-mix(in srgb, on share, off): each channel the share
// of the way from off to on, which is what the track must paint when
// its knob is that share of the way across.
func mixed(t *testing.T, off, on string, share float64) string {
	t.Helper()
	r1, g1, b1 := rgbOf(t, off)
	r2, g2, b2 := rgbOf(t, on)
	return fmt.Sprintf("rgb(%.0f, %.0f, %.0f)", r1+(r2-r1)*share, g1+(g2-g1)*share, b1+(b2-b1)*share)
}

// near is true when two rgb() colours are within tol per channel.
func near(t *testing.T, a, b string, tol float64) bool {
	t.Helper()
	r1, g1, b1 := rgbOf(t, a)
	r2, g2, b2 := rgbOf(t, b)
	return math.Abs(r1-r2) <= tol && math.Abs(g1-g2) <= tol && math.Abs(b1-b2) <= tol
}

// TestTheTrackColourFollowsTheKnob (the switch rework): the on colour
// fades in exactly as far as the knob has travelled. Two registered
// numbers on the track, --rst-switch-p (the knob's share of its
// travel, which the knob's translate reads) and --rst-switch-on (the
// on colour's share of the track, which a color-mix() reads), set
// side by side by every rule and by the one script write, and eased
// together by one transition. Mid-drag, with a finger: the track is
// the share of the way from the off colour to the on colour that the
// knob is across; released past the middle, it is the on colour. Mid-
// animation after a click, the transitions paused part way: the same,
// for a switch going on and for a toggle block's head going off. At
// rest the numbers are 0 and 0% off, 1 and 100% on. In LTR and RTL.
func TestTheTrackColourFollowsTheKnob(t *testing.T) {
	doc := sizingDoc("switch colour", daySwitchBody)
	pages := map[string]string{"/": doc, "/rtl": strings.Replace(doc, `dir="ltr"`, `dir="rtl"`, 1)}
	for _, path := range []string{"/", "/rtl"} {
		t.Run(path, func(t *testing.T) {
			rig := sizingRig(t, false, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible(chromedp.CSS("#tb")),
				chromedp.Evaluate[chromedp.Void](`new Promise(r =>
					document.readyState === "complete" ? r()
						: addEventListener("load", r, {once: true}))`,
					awaitPromise),
				send(emulation.SetTouchEmulationEnabled,
					emulation.SetTouchEmulationEnabledParams{Enabled: true}))
			sign := 1.0
			if path == "/rtl" {
				sign = -1
			}
			off, on := read(t, ctx, "off"), read(t, ctx, "on")
			offColour, onColour := canonColours(off.Bg), canonColours(on.Bg)
			if offColour == onColour {
				t.Fatalf("the off and on tracks are both %s; the drive would prove nothing", offColour)
			}
			if off.P != "0" || off.On != "0%" || on.P != "1" || on.On != "100%" {
				t.Errorf("at rest the off track's numbers are %q and %q, the on track's %q and %q; want 0 and 0%% off, 1 and 100%% on", off.P, off.On, on.P, on.On)
			}
			if d := computed(t, ctx, "#off [rst-switch-track]", "transition"); !strings.Contains(d, "--rst-switch-p 0.24s cubic-bezier(0.22, 1, 0.36, 1)") || !strings.Contains(d, "--rst-switch-on 0.24s cubic-bezier(0.22, 1, 0.36, 1)") {
				t.Errorf("the track's transition is %q; want both numbers eased by the same 0.24s cubic-bezier(0.22, 1, 0.36, 1), so the colour cannot drift from the knob", d)
			}

			// A finger drags the off knob 60% of the way across in
			// steps (the shim needs 4px before it takes the drag) and
			// holds it there.
			x := thumbAt(off, sign)
			mustRun(t, ctx, touchEvent(
				input.DispatchTouchEventTypeTouchStart,
				[]*input.TouchPoint{{X: x, Y: off.Y}}))
			for _, o := range []float64{0.2, 0.4, 0.6} {
				mustRun(t, ctx, touchEvent(
					input.DispatchTouchEventTypeTouchMove,
					[]*input.TouchPoint{{X: x + sign*o*off.Run, Y: off.Y}}))
			}
			mustRun(t, ctx, chromedp.Sleep(100*time.Millisecond))
			mid := read(t, ctx, "off")
			if !mid.Dragging {
				t.Fatal("the drag did not take")
			}
			share := math.Abs(mid.X) / mid.Run
			if share < 0.5 || share > 0.7 {
				t.Errorf("mid-drag the knob is %.2f of the way across; want about 0.6", share)
			}
			if want := mixed(t, offColour, onColour, share); !near(t, mid.Bg, want, 2) {
				t.Errorf("mid-drag, knob %.2f across, the track is %s; want %s, that share of the way from %s to %s", share, canonColours(mid.Bg), want, offColour, onColour)
			}
			mustRun(t, ctx,
				touchEvent(input.DispatchTouchEventTypeTouchEnd, nil),
				chromedp.Sleep(400*time.Millisecond))
			if s := read(t, ctx, "off"); !s.Checked || canonColours(s.Bg) != onColour || math.Abs(s.X-sign*s.Run) > 0.5 {
				t.Errorf("released at 60%%: checked %v, track %s, knob at %.1f; want on, %s, %.1f", s.Checked, canonColours(s.Bg), s.X, onColour, sign*s.Run)
			}

			// A click, and the transitions paused 100ms in: the knob and
			// the colour have come the same share of the way. #on goes
			// off; #head, the toggle block's, goes off too.
			var pausedProps string
			for _, id := range []string{"on", "head"} {
				before := read(t, ctx, id)
				mustRun(t, ctx, harness.Into(&pausedProps,
					chromedp.Evaluate[string](`(() => {
						const l = document.getElementById(`+jsStr(id)+`),
							tr = l.querySelector("[rst-switch-track]");
						l.querySelector("input").click();
				  const as = document.getAnimations().filter(a => a.effect && a.effect.target === tr); as.forEach(a => { a.pause(); a.currentTime = 100; }); return as.map(a => a.transitionProperty).sort().join(" "); })()`)))
				if pausedProps != "--rst-switch-on --rst-switch-p" {
					t.Errorf("#%s clicked: the track's running transitions are %q; want --rst-switch-on and --rst-switch-p", id, pausedProps)
				}
				s := read(t, ctx, id)
				share := math.Abs(s.X) / s.Run
				if s.Checked == before.Checked || share < 0.05 || share > 0.95 {
					t.Errorf("#%s clicked and paused 100ms in: checked %v (was %v), knob %.2f across; want toggled and mid-way", id, s.Checked, before.Checked, share)
				}
				if want := mixed(t, offColour, onColour, share); !near(t, s.Bg, want, 2) {
					t.Errorf("#%s paused 100ms into its toggle, knob %.2f across, the track is %s; want %s", id, share, canonColours(s.Bg), want)
				}
				mustRun(t, ctx, chromedp.Evaluate[chromedp.Void](
					`document.getAnimations().forEach(a => a.finish()), true`))
			}
		})
	}
}

// TestAReleasedKnobSettlesWithAFirmCurve (the switch rework): a drag
// follows the pointer 1:1 across the whole track, with no transition
// on the track while it lasts; released, the knob goes to the nearer
// end, on from half way or further (0.5 is on), with the firm 0.18s
// cubic-bezier(0.3, 0.9, 0.4, 1) rather than a toggle's 0.24s: the
// shim marks the label data-rst-settle for the release and clears it
// after, and both numbers ride the one transition, so the colour
// follows the knob home. The checkbox is clicked only when the end is
// the other state. Under reduced motion the settle has no transition.
// In LTR and RTL.
func TestAReleasedKnobSettlesWithAFirmCurve(t *testing.T) {
	doc := sizingDoc("switch release", daySwitchBody)
	pages := map[string]string{"/": doc, "/rtl": strings.Replace(doc, `dir="ltr"`, `dir="rtl"`, 1)}
	const firm = "0.18s cubic-bezier(0.3, 0.9, 0.4, 1)"
	for _, leg := range []struct {
		name, path string
		reduce     bool
	}{{"ltr", "/", false}, {"rtl", "/rtl", false}, {"ltr reduced motion", "/", true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, false, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			if leg.reduce {
				features := []*emulation.MediaFeature{
					{Name: "prefers-reduced-motion", Value: "reduce"},
				}
				mustRun(t, ctx, send(emulation.SetEmulatedMedia,
					emulation.SetEmulatedMediaParams{Features: features}))
			}
			mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+leg.path), chromedp.WaitVisible(chromedp.CSS("#tb")),
				chromedp.Evaluate[chromedp.Void](`new Promise(r =>
					document.readyState === "complete" ? r()
						: addEventListener("load", r, {once: true}))`,
					awaitPromise))
			sign := 1.0
			if leg.path == "/rtl" {
				sign = -1
			}
			mouse := func(
				typ input.DispatchMouseEventType, x, y float64,
			) chromedp.Action[chromedp.Void] {
				return chromedp.MouseEvent(typ, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1))
			}
			// drag presses the knob where it rests, moves it through the
			// shares of the travel given (toward on), reads the switch at
			// the last, releases, and reads it again at once.
			drag := func(id string, shares ...float64) (mid, dropped switchReading) {
				s := read(t, ctx, id)
				x := thumbAt(s, sign)
				if s.Checked {
					x += sign * s.Run
				}
				mustRun(t, ctx, mouse(chromedp.MousePressed, x, s.Y))
				for _, o := range shares {
					mustRun(t, ctx, mouse(chromedp.MouseMoved,
						thumbAt(s, sign)+sign*o*s.Run, s.Y))
				}
				mid = read(t, ctx, id)
				last := shares[len(shares)-1]
				mustRun(t, ctx, mouse(chromedp.MouseReleased,
					thumbAt(s, sign)+sign*last*s.Run, s.Y))
				dropped = read(t, ctx, id)
				return
			}
			off, on := read(t, ctx, "off"), read(t, ctx, "on")
			offColour, onColour := canonColours(off.Bg), canonColours(on.Bg)

			// Short of half way: back to off, nothing fired.
			mid, dropped := drag("off", 0.2, 0.4)
			if !mid.Dragging || mid.Duration != "0s" || math.Abs(math.Abs(mid.X)/mid.Run-0.4) > 0.03 {
				t.Errorf("mid-drag: dragging %v, transition %s, knob %.2f across; want a drag with no transition, the knob 0.4 across under the pointer", mid.Dragging, mid.Duration, math.Abs(mid.X)/mid.Run)
			}
			if !dropped.Settling || dropped.Dragging {
				t.Errorf("just released: settling %v, dragging %v; want the settle mark on and the drag mark off", dropped.Settling, dropped.Dragging)
			}
			if d := computed(t, ctx, "#off [rst-switch-track]", "transition"); leg.reduce && d != "none" || !leg.reduce && d != "--rst-switch-p "+firm+", --rst-switch-on "+firm {
				t.Errorf("just released the track's transition is %q; want both numbers on the firm %s (reduced motion %v: none)", d, firm, leg.reduce)
			}
			mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))
			if s := read(t, ctx, "off"); s.Checked || s.Changes != 0 || s.Settling || math.Abs(s.X) > 0.5 || canonColours(s.Bg) != offColour {
				t.Errorf("released at 0.4, settled: checked %v, %d change events, settling %v, knob at %.1f, track %s; want off, none, the mark gone, the knob home and the off colour", s.Checked, s.Changes, s.Settling, s.X, canonColours(s.Bg))
			}
			if s := read(t, ctx, "off"); !leg.reduce && !strings.Contains(s.Duration, "0.24s") || leg.reduce && s.Duration != "0s" {
				t.Errorf("settled, the track's transition is %s; want a toggle's 0.24s back (reduced motion %v: none)", s.Duration, leg.reduce)
			}

			// Half way exactly: on. Then from on, half way: still on; and
			// short of it: off.
			drag("off", 0.2, 0.5)
			mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))
			if s := read(t, ctx, "off"); !s.Checked || s.Changes != 1 || s.Inputs != 1 || math.Abs(s.X-sign*s.Run) > 0.5 || canonColours(s.Bg) != onColour {
				t.Errorf("released at 0.5: checked %v, %d change and %d input events, knob at %.1f, track %s; want on, one of each, the knob at %.1f and the on colour", s.Checked, s.Changes, s.Inputs, s.X, canonColours(s.Bg), sign*s.Run)
			}
			drag("on", 0.8, 0.5)
			mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))
			if s := read(t, ctx, "on"); !s.Checked || s.Changes != 0 {
				t.Errorf("from on, released at 0.5: checked %v, %d change events; want still on, none", s.Checked, s.Changes)
			}
			drag("on", 0.8, 0.45)
			mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))
			if s := read(t, ctx, "on"); s.Checked || s.Changes != 1 || math.Abs(s.X) > 0.5 || canonColours(s.Bg) != offColour {
				t.Errorf("from on, released at 0.45: checked %v, %d change events, knob at %.1f, track %s; want off, once, the knob home and the off colour", s.Checked, s.Changes, s.X, canonColours(s.Bg))
			}

			// Mid-settle the colour is still the knob's share: release
			// #off at 0.3 (it is on now, so this is from on) and pause
			// the settle 60ms in.
			if leg.reduce {
				return
			}
			var props string
			s := read(t, ctx, "off")
			x := thumbAt(s, sign) + sign*s.Run
			trackStart := thumbAt(s, sign)
			mustRun(t, ctx,
				mouse(chromedp.MousePressed, x, s.Y),
				mouse(chromedp.MouseMoved, trackStart+sign*0.6*s.Run, s.Y),
				mouse(chromedp.MouseMoved, trackStart+sign*0.3*s.Run, s.Y))
			mustRun(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => {
				const tr = document.querySelector("#off [rst-switch-track]");
				return new Promise(done => {
					document.addEventListener("pointerup", () => {
						requestAnimationFrame(() => {
							const as = document.getAnimations().filter(a =>
								a.effect && a.effect.target === tr);
							as.forEach(a => { a.pause(); a.currentTime = 60; });
							done(as.map(a => a.transitionProperty)
								.sort().join(" "));
						});
					}, {once: true});
				});
			})()`))
			mustRun(t, ctx, mouse(chromedp.MouseReleased,
				trackStart+sign*0.3*s.Run, s.Y))
			mustRun(t, ctx, harness.Into(&props, chromedp.Evaluate[string](
				`(() => {
				const tr = document.querySelector("#off [rst-switch-track]");
				const as = document.getAnimations()
					.filter(a => a.effect && a.effect.target === tr);
				as.forEach(a => { a.pause(); a.currentTime = 60; });
				return as.map(a => a.transitionProperty).sort().join(" ");
			})()`)))
			if props != "--rst-switch-on --rst-switch-p" {
				t.Errorf("released at 0.3 from on, the track's running transitions are %q; want --rst-switch-on and --rst-switch-p", props)
			}
			if s := read(t, ctx, "off"); s.Checked {
				t.Errorf("released at 0.3 from on: still checked")
			} else if share := math.Abs(s.X) / s.Run; share <= 0.02 || share >= 0.3 {
				t.Errorf("paused 60ms into the settle the knob is %.2f across; want it on its way from 0.3 to 0", share)
			} else if want := mixed(t, offColour, onColour, share); !near(t, s.Bg, want, 2) {
				t.Errorf("paused 60ms into the settle, knob %.2f across, the track is %s; want %s", share, canonColours(s.Bg), want)
			}
			mustRun(t, ctx, chromedp.Evaluate[chromedp.Void](
				`document.getAnimations().forEach(a => a.finish()), true`))
		})
	}
}

// TestAHoveredKnobPrintsNoShadow (switch review fix 1): the knob's
// hover lift is a box-shadow in a rule that outranks print's reset on
// the knob, so print has to flatten the two knob-shadow tokens as it
// flattens the depth tokens; otherwise a page printed with the pointer
// over a switch printed the lift. Hovered and at rest, the ordinary
// switch and the toggle block's head, in day, plain and signal, on a
// hover rig under print media: no painted shadow on the knob or the
// track.
func TestAHoveredKnobPrintsNoShadow(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("switch print", daySwitchBody)}, harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx,
		send(emulation.SetEmulatedMedia,
			emulation.SetEmulatedMediaParams{Media: "print"}),
		chromedp.EmulateViewport(800, 600),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(chromedp.CSS("#tb")))
	still(t, ctx)
	var hov, print bool
	mustRun(t, ctx,
		harness.Into(&hov,
			chromedp.Evaluate[bool](`matchMedia("(hover: hover)").matches`)),
		harness.Into(&print,
			chromedp.Evaluate[bool](`matchMedia("print").matches`)))
	if !hov || !print {
		t.Fatalf("(hover: hover) %v, print %v; the hover rule would not apply and the drive would prove nothing", hov, print)
	}
	for _, theme := range []string{"day", "plain", "signal"} {
		swapTheme(t, ctx, theme)
		for _, sel := range []string{"#on", "#off", "#head"} {
			for _, states := range [][]string{{}, {"hover"}} {
				forceState(t, ctx, sel, states...)
				if k, tr := knobStyleOf(t, ctx, sel+" [rst-switch-track]", "box-shadow"), computed(t, ctx, sel+" [rst-switch-track]", "box-shadow"); !noShadow(k) || !noShadow(tr) {
					t.Errorf("%s: %s %v in print paints knob box-shadow %q, track %q; want nothing painted", theme, sel, states, k, tr)
				}
				forceState(t, ctx, sel)
			}
		}
	}
}

// TestATblockHeadDragsLikeASwitch (switch review fix 3): the toggle
// block's head has the same track and must drag the same. The shim's
// drag found only [rst-switch] labels, so a head never took a drag;
// it finds the head too, reading the head's own input (the body is
// the head's sibling, not inside it), and the drag, sheen and settle
// rules name the head with #104 item 1's direct-child scoping. From
// on, the knob dragged 30% across follows the pointer with no
// transition while the track's colour follows it; released, the head
// settles with the firm curve, goes off once (the label's own click
// after a drag is cancelled, or it would toggle straight back), and
// the body hides; dragged back past the middle it goes on and the body
// shows; a tap still toggles it. In LTR and RTL.
func TestATblockHeadDragsLikeASwitch(t *testing.T) {
	doc := sizingDoc("head drag", daySwitchBody)
	pages := map[string]string{"/": doc, "/rtl": strings.Replace(doc, `dir="ltr"`, `dir="rtl"`, 1)}
	for _, path := range []string{"/", "/rtl"} {
		t.Run(path, func(t *testing.T) {
			rig := sizingRig(t, false, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible(chromedp.CSS("#tb")),
				chromedp.Evaluate[chromedp.Void](`new Promise(r =>
					document.readyState === "complete" ? r()
						: addEventListener("load", r, {once: true}))`,
					awaitPromise))
			sign := 1.0
			if path == "/rtl" {
				sign = -1
			}
			mouse := func(
				typ input.DispatchMouseEventType, x, y float64,
			) chromedp.Action[chromedp.Void] {
				return chromedp.MouseEvent(typ, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1))
			}
			offColour, onColour := canonColours(read(t, ctx, "off").Bg), canonColours(read(t, ctx, "on").Bg)
			body := func() string { return computed(t, ctx, "#tb > [rst-tblock-body]", "display") }
			// drag presses the head's knob where it rests and moves it to
			// each share of the travel, reads before the release and at
			// once after it (with the track's transition then), and
			// waits for the settle.
			drag := func(shares ...float64) (mid, dropped switchReading, trans string) {
				s := read(t, ctx, "head")
				x := thumbAt(s, sign)
				if s.Checked {
					x += sign * s.Run
				}
				mustRun(t, ctx, mouse(chromedp.MousePressed, x, s.Y))
				for _, o := range shares {
					mustRun(t, ctx, mouse(chromedp.MouseMoved,
						thumbAt(s, sign)+sign*o*s.Run, s.Y))
				}
				mid = read(t, ctx, "head")
				last := shares[len(shares)-1]
				mustRun(t, ctx, mouse(chromedp.MouseReleased,
					thumbAt(s, sign)+sign*last*s.Run, s.Y))
				dropped = read(t, ctx, "head")
				trans = computed(t, ctx, "#head [rst-switch-track]", "transition")
				mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))
				return
			}
			if s := read(t, ctx, "head"); !s.Checked || body() != "block" {
				t.Fatalf("the head starts checked %v with its body %s; want on and shown", s.Checked, body())
			}
			mid, dropped, trans := drag(0.7, 0.3)
			if !mid.Dragging || mid.Duration != "0s" || math.Abs(math.Abs(mid.X)/mid.Run-0.3) > 0.03 {
				t.Errorf("%s: mid-drag the head is dragging %v, transition %s, knob %.2f across; want a drag with no transition and the knob 0.3 across under the pointer", path, mid.Dragging, mid.Duration, math.Abs(mid.X)/mid.Run)
			}
			if want := mixed(t, offColour, onColour, math.Abs(mid.X)/mid.Run); mid.Dragging && !near(t, mid.Bg, want, 2) {
				t.Errorf("%s: mid-drag the head's track is %s; want %s, the knob's share of the way from off to on", path, canonColours(mid.Bg), want)
			}
			if !dropped.Settling || dropped.Dragging || !strings.Contains(trans, "0.18s cubic-bezier(0.3, 0.9, 0.4, 1)") {
				t.Errorf("%s: just released the head is settling %v, dragging %v, transition %q; want the settle mark and the firm curve", path, dropped.Settling, dropped.Dragging, trans)
			}
			if s := read(t, ctx, "head"); s.Checked || s.Changes != 1 || s.Inputs != 1 || math.Abs(s.X) > 0.5 || canonColours(s.Bg) != offColour || body() != "none" {
				t.Errorf("%s: released at 0.3: checked %v, %d change and %d input events, knob at %.1f, track %s, body %s; want off, one of each, the knob home, the off colour and the body hidden", path, s.Checked, s.Changes, s.Inputs, s.X, canonColours(s.Bg), body())
			}
			drag(0.3, 0.8)
			if s := read(t, ctx, "head"); !s.Checked || s.Changes != 2 || math.Abs(s.X-sign*s.Run) > 0.5 || body() != "block" {
				t.Errorf("%s: dragged back to 0.8: checked %v, %d change events, knob at %.1f, body %s; want on, two, the knob at the end and the body shown", path, s.Checked, s.Changes, s.X, body())
			}
			s := read(t, ctx, "head")
			x := thumbAt(s, sign) + sign*s.Run
			mustRun(t, ctx,
				mouse(chromedp.MousePressed, x, s.Y),
				mouse(chromedp.MouseReleased, x, s.Y),
				chromedp.Sleep(400*time.Millisecond))
			if s := read(t, ctx, "head"); s.Checked || s.Changes != 3 {
				t.Errorf("%s: a tap on the head: checked %v, %d change events; want off, three", path, s.Checked, s.Changes)
			}
		})
	}
}

// TestAToggleDuringASettleTakesItsOwnCurve (switch review fix 4): the
// settle mark outlives the release (300ms for a 0.18s transition), so a
// click or Space inside that window took the firm release curve
// instead of a toggle's 0.24s. The mark now goes on the next change
// event inside the switch, bar the release's own checkbox click, which
// is the settle itself. A release at 0.4 then a click: the mark is
// gone at once and the toggle rides 0.24s; a release that commits
// keeps the mark and the firm curve; a release at 0.8 from on (no
// commit) then Space on the focused checkbox: the mark is gone.
func TestAToggleDuringASettleTakesItsOwnCurve(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("settle leak", daySwitchBody)})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(chromedp.CSS("#tb")),
		chromedp.Evaluate[chromedp.Void](`new Promise(r =>
			document.readyState === "complete" ? r()
				: addEventListener("load", r, {once: true}))`,
			awaitPromise))
	const firm, toggle = "0.18s cubic-bezier(0.3, 0.9, 0.4, 1)", "0.24s cubic-bezier(0.22, 1, 0.36, 1)"
	mouse := func(
		typ input.DispatchMouseEventType, x, y float64,
	) chromedp.Action[chromedp.Void] {
		return chromedp.MouseEvent(typ, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1))
	}
	trans := func(id string) string { return computed(t, ctx, "#"+id+" [rst-switch-track]", "transition") }
	// release drags the knob to share and lets go, reading at once.
	release := func(id string, share float64) switchReading {
		s := read(t, ctx, id)
		x := thumbAt(s, 1)
		if s.Checked {
			x += s.Run
		}
		mustRun(t, ctx,
			mouse(chromedp.MousePressed, x, s.Y),
			mouse(chromedp.MouseMoved, thumbAt(s, 1)+0.5*s.Run, s.Y),
			mouse(chromedp.MouseMoved, thumbAt(s, 1)+share*s.Run, s.Y),
			mouse(chromedp.MouseReleased, thumbAt(s, 1)+share*s.Run, s.Y))
		return read(t, ctx, id)
	}

	if s := release("off", 0.4); !s.Settling || s.Checked || !strings.Contains(trans("off"), firm) {
		t.Fatalf("released at 0.4: settling %v, checked %v, transition %q; want the mark, off, and the firm curve: the drive's premise", s.Settling, s.Checked, trans("off"))
	}
	mustRun(t, ctx, chromedp.Evaluate[chromedp.Void](
		`document.querySelector("#off input").click(), true`))
	if s := read(t, ctx, "off"); s.Settling || !s.Checked || !strings.Contains(trans("off"), toggle) {
		t.Errorf("clicked while settling: settling %v, checked %v, transition %q; want the mark gone, on, and a toggle's %s", s.Settling, s.Checked, trans("off"), toggle)
	}
	mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))
	if s := read(t, ctx, "off"); !s.Checked || math.Abs(s.X-s.Run) > 0.5 || s.Changes != 1 {
		t.Errorf("settled after the click: checked %v, knob at %.1f, %d change events; want on at %.1f, one", s.Checked, s.X, s.Changes, s.Run)
	}

	// The release's own click is the settle, and keeps the mark.
	if s := release("on", 0.2); !s.Settling || s.Checked || !strings.Contains(trans("on"), firm) {
		t.Errorf("released at 0.2 from on, which commits: settling %v, checked %v, transition %q; want the mark kept, off, and the firm curve", s.Settling, s.Checked, trans("on"))
	}
	mustRun(t, ctx, chromedp.Sleep(400*time.Millisecond))

	// Space on the focused checkbox during a settle that did not commit.
	if s := release("off", 0.8); !s.Settling || !s.Checked {
		t.Fatalf("released at 0.8 from on: settling %v, checked %v; want the mark and still on", s.Settling, s.Checked)
	}
	mustRun(t, ctx,
		chromedp.Evaluate[chromedp.Void](
			`document.querySelector("#off input").focus(), true`),
		chromedp.KeyEvent(" "))
	if s := read(t, ctx, "off"); s.Settling || s.Checked || !strings.Contains(trans("off"), toggle) {
		t.Errorf("Space while settling: settling %v, checked %v, transition %q; want the mark gone, off, and a toggle's %s", s.Settling, s.Checked, trans("off"), toggle)
	}
}
