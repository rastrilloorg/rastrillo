//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// switchJS reads one switch: its checkbox, the events it fired since the
// page counted, where the thumb is drawn (the ::after's translateX, in
// px, signed as the screen has it), the thumb's transition and the
// track's touch-action.
const switchJS = `((id) => { const l = document.getElementById(id), t = l.querySelector("[rst-switch-track]"), a = getComputedStyle(t, "::after");
  const m = new DOMMatrix(a.transform === "none" ? "" : a.transform), r = t.getBoundingClientRect();
  return JSON.stringify({Checked: l.querySelector("input").checked, Changes: window.changes[id] || 0, Inputs: window.inputs[id] || 0,
    X: m.m41, Duration: a.transitionDuration, TouchAction: getComputedStyle(t).touchAction, Dragging: l.hasAttribute("data-rst-drag"),
    L: r.left, R: r.right, Y: r.top + r.height / 2}); })`

type switchReading struct {
	Checked, Dragging     bool
	Changes, Inputs       int
	X, L, R, Y            float64
	Duration, TouchAction string
}

// read runs switchJS on the switch with this id.
func read(t *testing.T, ctx context.Context, id string) switchReading {
	t.Helper()
	var raw string
	mustRun(t, ctx, chromedp.Evaluate(switchJS+`("`+id+`")`, &raw))
	var s switchReading
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestASwitchSlidesAndSnaps: the thumb can be dragged. It follows the
// pointer along the track, and on release snaps to the nearer end; the
// checkbox toggles, once, with one input and one change event, only when
// that end is the other state. A drag that comes back before release
// changes nothing, a tap toggles once as it always has (the label's own
// behaviour, so never twice), a disabled switch does not move (nor one
// in a disabled fieldset), and in
// RTL the track runs the other way. The thumb snaps with the CSS
// transition, which reduced motion turns off; the track lets a vertical
// swipe scroll the page and keeps a horizontal one for the drag. With a
// mouse, and with touch.
func TestASwitchSlidesAndSnaps(t *testing.T) {
	sw := func(id, extra string) string {
		return `<p><label rst-switch id="` + id + `"><input type="checkbox" name="` + id + `"` + extra + `><span rst-switch-track aria-hidden="true"></span> Email me about replies</label></p>`
	}
	body := `<div rst-page>` + sw("drag", "") + sw("back", "") + sw("tap", "") + sw("off", " disabled") + `<fieldset disabled>` + sw("fieldset", "") + `</fieldset>` + sw("touch", "") + `</div>` +
		`<script>window.changes = {}; window.inputs = {}; for (const [k, m] of [["change", window.changes], ["input", window.inputs]]) document.addEventListener(k, e => { const l = e.target.closest("[rst-switch]"); if (l) m[l.id] = (m[l.id] || 0) + 1; });</script>`
	pages := map[string]string{"/": sizingDoc("switch", body), "/rtl": strings.Replace(sizingDoc("switch", body), `dir="ltr"`, `dir="rtl"`, 1)}
	for _, leg := range []struct {
		name, path string
		reduce     bool
	}{{"ltr", "/", false}, {"rtl", "/rtl", false}, {"ltr reduced motion", "/", true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, false, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			if leg.reduce {
				mustRun(t, ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}))
			}
			mustRun(t, ctx, chromedp.EmulateViewport(800, 700), chromedp.Navigate(rig.Origin+leg.path), chromedp.WaitReady("#touch", chromedp.ByQuery),
				chromedp.Evaluate(`new Promise(r => addEventListener("load", r, {once: true}) || document.readyState === "complete" && r())`, nil, awaitPromise))
			// sign turns "toward on" into screen pixels; the thumb starts
			// at the track's inline start, 2px in, 15px across.
			sign := 1.0
			if leg.path == "/rtl" {
				sign = -1
			}
			thumb := func(s switchReading) float64 {
				if sign > 0 {
					return s.L + 2 + 7.5
				}
				return s.R - 2 - 7.5
			}
			mouse := func(typ input.MouseType, x, y float64) chromedp.Action {
				return input.DispatchMouseEvent(typ, x, y).WithButton(input.Left).WithClickCount(1)
			}
			// drag presses on the thumb, moves through each offset (toward
			// on, in px) and releases at the last.
			drag := func(id string, offsets ...float64) (mid switchReading) {
				s := read(t, ctx, id)
				x := thumb(s)
				mustRun(t, ctx, mouse(input.MousePressed, x, s.Y))
				for _, o := range offsets {
					mustRun(t, ctx, mouse(input.MouseMoved, x+sign*o, s.Y))
				}
				mid = read(t, ctx, id)
				mustRun(t, ctx, mouse(input.MouseReleased, x+sign*offsets[len(offsets)-1], s.Y), chromedp.Sleep(350*time.Millisecond))
				return mid
			}

			if s := read(t, ctx, "drag"); s.TouchAction != "pan-y" {
				t.Errorf("the track's touch-action is %q; want pan-y, so a vertical swipe scrolls and a horizontal one drags", s.TouchAction)
			}
			if s := read(t, ctx, "drag"); leg.reduce && s.Duration != "0s" || !leg.reduce && s.Duration == "0s" {
				t.Errorf("the thumb's transition is %s with reduced motion %v; it should snap with a transition, and without one under reduced motion", s.Duration, leg.reduce)
			}

			mid := drag("drag", 3, 6, 9, 12)
			if !mid.Dragging || math.Abs(mid.X-sign*12) > 1 {
				t.Errorf("12px into a drag the thumb is at %.1f (dragging %v); it should follow the pointer to %.0f", mid.X, mid.Dragging, sign*12)
			}
			if s := read(t, ctx, "drag"); !s.Checked || s.Changes != 1 || s.Inputs != 1 || math.Abs(s.X-sign*15) > 0.5 || s.Dragging {
				t.Errorf("released past the middle: checked %v, %d change and %d input events, thumb at %.1f; want on, one of each, at %.0f", s.Checked, s.Changes, s.Inputs, s.X, sign*15)
			}

			drag("back", 4, 8, 12, 4, 2)
			if s := read(t, ctx, "back"); s.Checked || s.Changes != 0 || math.Abs(s.X) > 0.5 {
				t.Errorf("dragged out and back before release: checked %v, %d change events, thumb at %.1f; want nothing changed", s.Checked, s.Changes, s.X)
			}

			s := read(t, ctx, "tap")
			mustRun(t, ctx, mouse(input.MousePressed, thumb(s), s.Y), mouse(input.MouseReleased, thumb(s), s.Y), chromedp.Sleep(350*time.Millisecond))
			if s := read(t, ctx, "tap"); !s.Checked || s.Changes != 1 {
				t.Errorf("a tap: checked %v, %d change events; want on, once", s.Checked, s.Changes)
			}

			// Disabled itself, and disabled by its fieldset, where the
			// checkbox's own disabled property is false but it matches
			// :disabled and a click does nothing.
			for _, id := range []string{"off", "fieldset"} {
				mid = drag(id, 4, 8, 12)
				if s := read(t, ctx, id); s.Checked || s.Changes != 0 || mid.Dragging || math.Abs(mid.X) > 0.5 || s.Dragging {
					t.Errorf("#%s, disabled, dragged: checked %v, %d change events, knob at %.1f mid-drag (dragging %v); want it not to move", id, s.Checked, s.Changes, mid.X, mid.Dragging)
				}
			}

			// Touch: the same drag with a finger. Read after touch is on:
			// a coarse pointer grows the rows, and the switch moves.
			mustRun(t, ctx, emulation.SetTouchEmulationEnabled(true))
			s = read(t, ctx, "touch")
			x := thumb(s)
			mustRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: x, Y: s.Y}}).Do(ctx); err != nil {
					return err
				}
				for _, o := range []float64{3, 6, 9, 12, 14} {
					if err := input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: x + sign*o, Y: s.Y}}).Do(ctx); err != nil {
						return err
					}
				}
				return input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}).Do(ctx)
			}), chromedp.Sleep(350*time.Millisecond))
			if s := read(t, ctx, "touch"); !s.Checked || s.Changes != 1 {
				t.Errorf("a finger dragged past the middle: checked %v, %d change events; want on, once", s.Checked, s.Changes)
			}
		})
	}
}

// TestASwitchFollowsItsOwnDirection: the knob's travel comes from the
// switch's own direction, not from an ancestor's dir attribute. An LTR
// section inside an RTL page and an RTL one inside an LTR page each
// draw their checked knob at their own inline end, inside the track,
// and a drag toward that end turns them on.
func TestASwitchFollowsItsOwnDirection(t *testing.T) {
	sw := func(id, checked string) string {
		return `<label rst-switch id="` + id + `"><input type="checkbox" name="` + id + `"` + checked + `><span rst-switch-track aria-hidden="true"></span> Email me</label>`
	}
	body := func(inner string) string {
		return `<div rst-page><section dir="` + inner + `"><p>` + sw("on", " checked") + `</p><p>` + sw("drag", "") + `</p></section></div>` +
			`<script>window.changes = {}; window.inputs = {}; document.addEventListener("change", e => { const l = e.target.closest("[rst-switch]"); if (l) window.changes[l.id] = (window.changes[l.id] || 0) + 1; });</script>`
	}
	pages := map[string]string{
		"/ltr-in-rtl": strings.Replace(sizingDoc("switch", body("ltr")), `dir="ltr"`, `dir="rtl"`, 1),
		"/rtl-in-ltr": sizingDoc("switch", body("rtl")),
	}
	rig := sizingRig(t, false, pages)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	for path, sign := range map[string]float64{"/ltr-in-rtl": 1, "/rtl-in-ltr": -1} {
		mustRun(t, ctx, chromedp.EmulateViewport(800, 700), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("#drag", chromedp.ByQuery),
			chromedp.Evaluate(`new Promise(r => document.readyState === "complete" ? r() : addEventListener("load", r, {once: true}))`, nil, awaitPromise))
		if s := read(t, ctx, "on"); math.Abs(s.X-sign*15) > 0.5 {
			t.Errorf("%s: the checked knob is moved %.1fpx; want %.0f, toward the switch's own inline end", path, s.X, sign*15)
		}
		s := read(t, ctx, "drag")
		x := s.L + 9.5
		if sign < 0 {
			x = s.R - 9.5
		}
		mouse := func(typ input.MouseType, x float64) chromedp.Action {
			return input.DispatchMouseEvent(typ, x, s.Y).WithButton(input.Left).WithClickCount(1)
		}
		mustRun(t, ctx, mouse(input.MousePressed, x))
		for _, o := range []float64{3, 6, 9, 12} {
			mustRun(t, ctx, mouse(input.MouseMoved, x+sign*o))
		}
		mustRun(t, ctx, mouse(input.MouseReleased, x+sign*12), chromedp.Sleep(350*time.Millisecond))
		if s := read(t, ctx, "drag"); !s.Checked || s.Changes != 1 || math.Abs(s.X-sign*15) > 0.5 {
			t.Errorf("%s: dragged toward its inline end: checked %v, %d change events, knob at %.1f; want on, once, at %.0f", path, s.Checked, s.Changes, s.X, sign*15)
		}
	}
}

// TestASwitchTakesOneDragAtATime: a second finger on another switch
// while the first is dragging is ignored, and when both lift, neither
// switch is left mid-drag. The second press overwrote the first drag,
// which kept its drag mark and its knob position for good, where a
// keyboard toggle or a form reset could no longer move it.
func TestASwitchTakesOneDragAtATime(t *testing.T) {
	sw := func(id string) string {
		return `<p><label rst-switch id="` + id + `"><input type="checkbox" name="` + id + `"><span rst-switch-track aria-hidden="true"></span> Email me</label></p>`
	}
	body := `<div rst-page>` + sw("a") + sw("b") + `</div><script>window.changes = {}; window.inputs = {}; document.addEventListener("change", e => { const l = e.target.closest("[rst-switch]"); if (l) window.changes[l.id] = (window.changes[l.id] || 0) + 1; });</script>`
	rig := sizingRig(t, true, map[string]string{"/": sizingDoc("switch", body)})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 700), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("#b", chromedp.ByQuery),
		chromedp.Evaluate(`new Promise(r => document.readyState === "complete" ? r() : addEventListener("load", r, {once: true}))`, nil, awaitPromise))
	a, b := read(t, ctx, "a"), read(t, ctx, "b")
	ax, bx := a.L+9.5, b.L+9.5
	touch := func(typ input.TouchType, pts ...*input.TouchPoint) chromedp.Action {
		return input.DispatchTouchEvent(typ, pts)
	}
	pa := func(o float64) *input.TouchPoint { return &input.TouchPoint{X: ax + o, Y: a.Y, ID: 1} }
	pb := func(o float64) *input.TouchPoint { return &input.TouchPoint{X: bx + o, Y: b.Y, ID: 2} }
	mustRun(t, ctx,
		touch(input.TouchStart, pa(0)), touch(input.TouchMove, pa(6)), touch(input.TouchMove, pa(12)),
		touch(input.TouchStart, pa(12), pb(0)), touch(input.TouchMove, pa(13), pb(6)), touch(input.TouchMove, pa(14), pb(12)),
		touch(input.TouchEnd), chromedp.Sleep(350*time.Millisecond))
	if s := read(t, ctx, "a"); !s.Checked || s.Changes != 1 || s.Dragging || math.Abs(s.X-15) > 0.5 {
		t.Errorf("the first switch: checked %v, %d change events, still dragging %v, knob at %.1f; want on, once, settled at 15", s.Checked, s.Changes, s.Dragging, s.X)
	}
	if s := read(t, ctx, "b"); s.Checked || s.Changes != 0 || s.Dragging || math.Abs(s.X) > 0.5 {
		t.Errorf("the second switch, pressed mid-drag: checked %v, %d change events, dragging %v, knob at %.1f; want it untouched", s.Checked, s.Changes, s.Dragging, s.X)
	}
}

// switchSheen reads the on track's sheen position as the browser
// resolved it: the x offset of the 40px radial gradient relative to the
// track's centre, in px, signed as the screen has it. The gradient is
// written as calc(50% + dir * (x - 7.5px)); Chromium serialises the
// resolved calc as "calc(50% + Npx)" or "calc(50% - Npx)".
func switchSheen(t *testing.T, ctx context.Context, sel string) (present bool, offset float64) {
	t.Helper()
	var img string
	mustRun(t, ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector(`+jsStr(sel)+` + " [rst-switch-track]")).backgroundImage`, &img))
	m := regexp.MustCompile(`radial-gradient\(40px at calc\(50% ([+-]) ([0-9.]+)px\)`).FindStringSubmatch(img)
	if m == nil {
		return false, 0
	}
	v, _ := strconv.ParseFloat(m[2], 64)
	if m[1] == "-" {
		v = -v
	}
	return true, v
}

// TestTheKnobIsLitAndTheTrackSheenFollowsIt (spec §3, C13, A10): the
// knob is a white-topped gradient in both schemes; the off track is
// --rst-track-off and the on track the accent fill; hovered, the on
// track's sheen sits over the knob, at the inline end in LTR and the
// inline start in RTL; the tblock variant does the same; and the
// sheen's position is pure CSS: no script writes on a switch. The knob
// carries its highlight, its contact shadow and the knob shadow, and
// the track sits in with an inset contact shadow; a disabled switch
// takes no hover, as a disabled button takes none.
func TestTheKnobIsLitAndTheTrackSheenFollowsIt(t *testing.T) {
	sw := `<p><label rst-switch id="s"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span> On</label></p>` +
		`<p><label rst-switch id="dis"><input type="checkbox" checked disabled><span rst-switch-track aria-hidden="true"></span> Locked</label></p>` +
		`<p><label rst-switch id="off"><input type="checkbox"><span rst-switch-track aria-hidden="true"></span> Off</label></p>` +
		`<div rst-tblock id="tb"><label rst-tblock-head id="head"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span><span rst-tblock-title>Block</span></label><div rst-tblock-body>body</div></div>`
	doc := sizingDoc("knob", `<div rst-page>`+sw+`</div>`)
	pages := map[string]string{"/": doc, "/rtl": strings.Replace(doc, `dir="ltr"`, `dir="rtl"`, 1)}
	for _, leg := range []struct{ name, path, scheme string }{{"ltr light", "/", "light"}, {"rtl light", "/rtl", "light"}, {"ltr dark", "/", "dark"}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, false, pages, harness.WithHover())
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(800, 600), chromedp.Navigate(rig.Origin+leg.path), chromedp.WaitVisible("#tb", chromedp.ByQuery),
				chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+leg.scheme+`"), true`, nil))
			still(t, ctx)
			wantOff, wantOn := "rgb(138, 138, 149)", "rgb(32, 89, 221)"
			if leg.scheme == "dark" {
				wantOff, wantOn = "rgb(113, 115, 124)", "rgb(49, 96, 234)"
			}
			var knob, offTrack, onTrack string
			mustRun(t, ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector("#off [rst-switch-track]"), "::after").backgroundImage`, &knob),
				chromedp.Evaluate(`getComputedStyle(document.querySelector("#off [rst-switch-track]")).backgroundColor`, &offTrack),
				chromedp.Evaluate(`getComputedStyle(document.querySelector("#s [rst-switch-track]")).backgroundColor`, &onTrack))
			// The on track is day's derived fill, which serialises as
			// oklch(); the literal above is its eight-bit triple.
			offTrack, onTrack = canonColours(offTrack), canonColours(onTrack)
			if offTrack != wantOff || !strings.Contains(knob, "linear-gradient(rgb(255, 255, 255)") {
				t.Errorf("off switch: track %s (want %s), knob %q (want a white-topped gradient)", offTrack, wantOff, knob)
			}
			if onTrack != wantOn {
				t.Errorf("on switch: track %s, want %s", onTrack, wantOn)
			}
			if present, _ := switchSheen(t, ctx, "#s"); present {
				t.Error("the on track has a sheen at rest; the sheen is a hover")
			}
			var knobShadow string
			mustRun(t, ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector("#off [rst-switch-track]"), "::after").boxShadow`, &knobShadow))
			if n := len(layers(knobShadow)); n != 3 || !strings.HasSuffix(layers(knobShadow)[0], "inset") {
				t.Errorf("knob box-shadow %q: want three layers, the inset highlight first", knobShadow)
			}
			if sh := computed(t, ctx, "#off [rst-switch-track]", "box-shadow"); !strings.HasSuffix(sh, "0px 1px 2px 0px inset") {
				t.Errorf("track box-shadow %q, want an inset contact shadow", sh)
			}
			var restKnob, hoverKnob string
			knobOf := `getComputedStyle(document.querySelector("#dis [rst-switch-track]"), "::after").backgroundImage`
			mustRun(t, ctx, chromedp.Evaluate(knobOf, &restKnob))
			forceState(t, ctx, "#dis", "hover")
			present, _ := switchSheen(t, ctx, "#dis")
			mustRun(t, ctx, chromedp.Evaluate(knobOf, &hoverKnob))
			forceState(t, ctx, "#dis")
			if present || hoverKnob != restKnob {
				t.Errorf("a disabled switch hovered: sheen %v, knob changed %v; want neither", present, hoverKnob != restKnob)
			}
			for _, sel := range []string{"#s", "#head"} {
				forceState(t, ctx, sel, "hover")
				present, off := switchSheen(t, ctx, sel)
				forceState(t, ctx, sel)
				if !present {
					t.Errorf("%s hovered has no 40px sheen", sel)
					continue
				}
				// On: the knob's centre is 7.5px past the middle toward
				// the inline end, which is +x in LTR and -x in RTL.
				if (leg.path == "/rtl") != (off < 0) || math.Abs(math.Abs(off)-7.5) > 0.5 {
					t.Errorf("%s hovered: sheen offset %v on %s; want 7.5px toward the inline end (C13)", sel, off, leg.path)
				}
			}
			if got := computed(t, ctx, "#head", "-webkit-tap-highlight-color"); got != "rgba(0, 0, 0, 0)" {
				t.Errorf("tblock head tap highlight %s, want transparent", got)
			}
		})
	}
}

// TestTheSheenFollowsADraggedKnob (C13): with a finger (touch
// emulation on the mouse rig, as TestASwitchSlidesAndSnaps drags; no
// hover rig, so nothing hovers), the knob is dragged part way and the
// sheen sits over it, written by rastrillo.js on the track and read by
// the gradient; the drag is cancelled and the knob snaps back with no
// sheen; a second drag loses its capture (a lostpointercapture with
// the drag's own pointer id, which the shim checks) and the knob snaps
// back the same way. In LTR and RTL, and a third time on the LTR page
// with light.js linked, where an observer on the switch records every
// style write during the drags: each must be the shim's own
// --rst-switch-x (or its removal) and nothing else, because light.js
// writes nothing on a switch while rastrillo.js owns the drag (C13).
// The fixture initialises the counters switchJS reads, as the other
// switch fixtures do.
func TestTheSheenFollowsADraggedKnob(t *testing.T) {
	sw := `<p><label rst-switch id="s"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span> On</label></p>` +
		`<script>window.changes = {}; window.inputs = {}; window.__pid = null; addEventListener("pointerdown", e => { window.__pid = e.pointerId; });</script>`
	doc := sizingDoc("drag", `<div rst-page>`+sw+`</div>`)
	pages := map[string]string{"/": doc, "/rtl": strings.Replace(doc, `dir="ltr"`, `dir="rtl"`, 1),
		"/light": strings.Replace(doc, `<script defer src="/busy.js"></script>`, `<script defer src="/busy.js"></script><script defer src="/light.js"></script>`, 1)}
	for _, path := range []string{"/", "/rtl", "/light"} {
		t.Run(path, func(t *testing.T) {
			rig := sizingRig(t, false, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(390, 700), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible("#s", chromedp.ByQuery),
				chromedp.Evaluate(`new Promise(r => addEventListener("load", r, {once: true}) || document.readyState === "complete" && r())`, nil, awaitPromise),
				emulation.SetTouchEmulationEnabled(true))
			if path == "/light" {
				mustRun(t, ctx, chromedp.Evaluate(`window.__sw = []; new MutationObserver(m => m.forEach(r => window.__sw.push(r.target.getAttribute("style") || ""))).observe(document.getElementById("s"), {attributes: true, subtree: true, attributeFilter: ["style"], attributeOldValue: true}); true`, nil))
			}
			s := read(t, ctx, "s")
			sign := 1.0
			if path == "/rtl" {
				sign = -1
			}
			// Start at the on end, drag 9px toward off in three steps
			// (the shim needs 4px before it takes the drag): the knob's
			// x becomes 6px, so the sheen sits at 6 - 7.5 = -1.5px
			// toward the inline end.
			startX := s.L + 2 + 7.5 + 15
			if sign < 0 {
				startX = s.R - 2 - 7.5 - 15
			}
			drag := func() {
				mustRun(t, ctx, input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: startX, Y: s.Y}}),
					input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: startX - 3*sign, Y: s.Y}}),
					input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: startX - 6*sign, Y: s.Y}}),
					input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: startX - 9*sign, Y: s.Y}}),
					chromedp.Sleep(100*time.Millisecond))
			}
			drag()
			if mid := read(t, ctx, "s"); !mid.Dragging || math.Abs(mid.X-6*sign) > 0.5 {
				t.Fatalf("mid-drag: dragging %v, knob at %v; want a drag with the knob at %v", mid.Dragging, mid.X, 6*sign)
			}
			if present, off := switchSheen(t, ctx, "#s"); !present || math.Abs(off-(-1.5*sign)) > 0.6 {
				t.Errorf("mid-drag sheen offset %v (present %v), want %v: the sheen follows --rst-switch-x", off, present, -1.5*sign)
			}
			mustRun(t, ctx, input.DispatchTouchEvent(input.TouchCancel, nil), chromedp.Sleep(350*time.Millisecond))
			if after := read(t, ctx, "s"); after.Dragging || math.Abs(after.X-15*sign) > 0.5 || !after.Checked {
				t.Errorf("after a cancelled drag: dragging %v, knob at %v, checked %v; want the knob back at %v, still on", after.Dragging, after.X, after.Checked, 15*sign)
			}
			if present, _ := switchSheen(t, ctx, "#s"); present {
				t.Error("after a cancelled drag the sheen is still there; nothing hovers a finger's switch")
			}
			drag()
			if !read(t, ctx, "s").Dragging {
				t.Fatal("the second drag did not take")
			}
			mustRun(t, ctx, chromedp.Evaluate(`document.querySelector("#s [rst-switch-track]").dispatchEvent(new PointerEvent("lostpointercapture", {pointerId: window.__pid, bubbles: true})), true`, nil), chromedp.Sleep(350*time.Millisecond))
			if after := read(t, ctx, "s"); after.Dragging || math.Abs(after.X-15*sign) > 0.5 {
				t.Errorf("after lostpointercapture: dragging %v, knob at %v; want the drag dropped and the knob back at %v", after.Dragging, after.X, 15*sign)
			}
			if path == "/light" {
				var seen []string
				mustRun(t, ctx, chromedp.Evaluate(`window.__sw`, &seen))
				if len(seen) == 0 {
					t.Error("the observer saw no style write on the switch during two drags; the shim writes --rst-switch-x there, so the leg proves nothing")
				}
				own := regexp.MustCompile(`^--rst-switch-x:\s*[^;]*;?\s*$`)
				for _, v := range seen {
					if v != "" && !own.MatchString(v) {
						t.Errorf("a style write on the switch during a drag left %q; only the shim's --rst-switch-x may be written there (C13)", v)
					}
				}
			}
			mustRun(t, ctx, input.DispatchTouchEvent(input.TouchEnd, nil))
		})
	}
}
