//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
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
			read := func(id string) switchReading {
				var raw string
				mustRun(t, ctx, chromedp.Evaluate(switchJS+`("`+id+`")`, &raw))
				var s switchReading
				if err := json.Unmarshal([]byte(raw), &s); err != nil {
					t.Fatal(err)
				}
				return s
			}
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
				s := read(id)
				x := thumb(s)
				mustRun(t, ctx, mouse(input.MousePressed, x, s.Y))
				for _, o := range offsets {
					mustRun(t, ctx, mouse(input.MouseMoved, x+sign*o, s.Y))
				}
				mid = read(id)
				mustRun(t, ctx, mouse(input.MouseReleased, x+sign*offsets[len(offsets)-1], s.Y), chromedp.Sleep(350*time.Millisecond))
				return mid
			}

			if s := read("drag"); s.TouchAction != "pan-y" {
				t.Errorf("the track's touch-action is %q; want pan-y, so a vertical swipe scrolls and a horizontal one drags", s.TouchAction)
			}
			if s := read("drag"); leg.reduce && s.Duration != "0s" || !leg.reduce && s.Duration == "0s" {
				t.Errorf("the thumb's transition is %s with reduced motion %v; it should snap with a transition, and without one under reduced motion", s.Duration, leg.reduce)
			}

			mid := drag("drag", 3, 6, 9, 12)
			if !mid.Dragging || math.Abs(mid.X-sign*12) > 1 {
				t.Errorf("12px into a drag the thumb is at %.1f (dragging %v); it should follow the pointer to %.0f", mid.X, mid.Dragging, sign*12)
			}
			if s := read("drag"); !s.Checked || s.Changes != 1 || s.Inputs != 1 || math.Abs(s.X-sign*15) > 0.5 || s.Dragging {
				t.Errorf("released past the middle: checked %v, %d change and %d input events, thumb at %.1f; want on, one of each, at %.0f", s.Checked, s.Changes, s.Inputs, s.X, sign*15)
			}

			drag("back", 4, 8, 12, 4, 2)
			if s := read("back"); s.Checked || s.Changes != 0 || math.Abs(s.X) > 0.5 {
				t.Errorf("dragged out and back before release: checked %v, %d change events, thumb at %.1f; want nothing changed", s.Checked, s.Changes, s.X)
			}

			s := read("tap")
			mustRun(t, ctx, mouse(input.MousePressed, thumb(s), s.Y), mouse(input.MouseReleased, thumb(s), s.Y), chromedp.Sleep(350*time.Millisecond))
			if s := read("tap"); !s.Checked || s.Changes != 1 {
				t.Errorf("a tap: checked %v, %d change events; want on, once", s.Checked, s.Changes)
			}

			// Disabled itself, and disabled by its fieldset, where the
			// checkbox's own disabled property is false but it matches
			// :disabled and a click does nothing.
			for _, id := range []string{"off", "fieldset"} {
				mid = drag(id, 4, 8, 12)
				if s := read(id); s.Checked || s.Changes != 0 || mid.Dragging || math.Abs(mid.X) > 0.5 || s.Dragging {
					t.Errorf("#%s, disabled, dragged: checked %v, %d change events, knob at %.1f mid-drag (dragging %v); want it not to move", id, s.Checked, s.Changes, mid.X, mid.Dragging)
				}
			}

			// Touch: the same drag with a finger. Read after touch is on:
			// a coarse pointer grows the rows, and the switch moves.
			mustRun(t, ctx, emulation.SetTouchEmulationEnabled(true))
			s = read("touch")
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
			if s := read("touch"); !s.Checked || s.Changes != 1 {
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
		read := func(id string) switchReading {
			var raw string
			mustRun(t, ctx, chromedp.Evaluate(switchJS+`("`+id+`")`, &raw))
			var s switchReading
			if err := json.Unmarshal([]byte(raw), &s); err != nil {
				t.Fatal(err)
			}
			return s
		}
		if s := read("on"); math.Abs(s.X-sign*15) > 0.5 {
			t.Errorf("%s: the checked knob is moved %.1fpx; want %.0f, toward the switch's own inline end", path, s.X, sign*15)
		}
		s := read("drag")
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
		if s := read("drag"); !s.Checked || s.Changes != 1 || math.Abs(s.X-sign*15) > 0.5 {
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
	read := func(id string) switchReading {
		var raw string
		mustRun(t, ctx, chromedp.Evaluate(switchJS+`("`+id+`")`, &raw))
		var s switchReading
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := read("a"), read("b")
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
	if s := read("a"); !s.Checked || s.Changes != 1 || s.Dragging || math.Abs(s.X-15) > 0.5 {
		t.Errorf("the first switch: checked %v, %d change events, still dragging %v, knob at %.1f; want on, once, settled at 15", s.Checked, s.Changes, s.Dragging, s.X)
	}
	if s := read("b"); s.Checked || s.Changes != 0 || s.Dragging || math.Abs(s.X) > 0.5 {
		t.Errorf("the second switch, pressed mid-drag: checked %v, %d change events, dragging %v, knob at %.1f; want it untouched", s.Checked, s.Changes, s.Dragging, s.X)
	}
}
