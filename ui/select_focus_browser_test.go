//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// selection reads the colour box's value, its selection and how many
// rows its list shows.
const selectionJS = `(() => { const i = document.getElementById("colour-combo"), l = document.getElementById(i.getAttribute("aria-controls"));
  return JSON.stringify({Value: i.value, Start: i.selectionStart, End: i.selectionEnd, Focused: document.activeElement === i,
    Rows: l ? [...l.querySelectorAll("[role=option]")].filter(o => o.checkVisibility()).length : -1}); })()`

type comboSelection struct {
	Value            string
	Start, End, Rows int
	Focused          bool
}

// TestSelectFocusSelectsThePick: focusing a combobox that shows its pick
// selects the whole pick, so typing or Delete replaces it, by a click as
// much as by Tab. A click put the caret where it landed, and clearing
// "Red" took a double-click or a run of backspaces first. Only the
// focusing click does it: a second click in the focused box places the
// caret, so a partial edit still works, and a drag across the pick as it
// is focused keeps what the drag selected. Real pointer and key events,
// because what is fenced is the order the browser runs mousedown, focus,
// mouseup and click in, which a scripted focus() never goes through. In
// both directions of text.
func TestSelectFocusSelectsThePick(t *testing.T) {
	for _, dir := range []string{"ltr", "rtl"} {
		t.Run(dir, func(t *testing.T) {
			mux := convergedPage(t)
			rig := harness.New(t, func(string) http.Handler { return mux })
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, emulation.SetFocusEmulationEnabled(true), chromedp.EmulateViewport(1000, 900), chromedp.Navigate(rig.Origin+"/"),
				chromedp.WaitVisible(`#colour-combo`, chromedp.ByQuery),
				chromedp.Evaluate(`document.documentElement.dir = "`+dir+`"; document.getElementById("colour-combo").scrollIntoView({block: "center"}); true`, nil))
			var box struct{ L, R, Y, PadL, PadR, Gr float64 }
			at(t, ctx, `(() => { const i = document.getElementById("colour-combo"), r = i.getBoundingClientRect(), s = getComputedStyle(i);
			  return JSON.stringify({L: r.left, R: r.right, Y: r.top + r.height / 2, PadL: parseFloat(s.paddingLeft) + parseFloat(s.borderLeftWidth), PadR: parseFloat(s.paddingRight) + parseFloat(s.borderRightWidth),
			    Gr: Object.assign(document.createElement("canvas").getContext("2d"), {font: s.font}).measureText("Gr").width}); })()`, &box)
			// After a frame: the focusing click selects in the next one.
			read := func() comboSelection {
				var raw string
				mustRun(t, ctx, chromedp.Evaluate(afterFrame, nil, awaitPromise), chromedp.Evaluate(selectionJS, &raw))
				var s comboSelection
				if err := json.Unmarshal([]byte(raw), &s); err != nil {
					t.Fatal(err)
				}
				return s
			}
			// Inside the box, clear of the lead and the chevron: a third
			// of the way in from either end.
			x1, x2 := box.L+(box.R-box.L)/3, box.L+2*(box.R-box.L)/3
			blur := func() {
				mustRun(t, ctx, chromedp.Evaluate(`document.activeElement.blur(); document.querySelectorAll("[rst-combo] [role=listbox]").forEach(l => l.hidden = true); true`, nil))
			}

			mustRun(t, ctx, chromedp.MouseClickXY(x1, box.Y))
			if s := read(); !s.Focused || s.Value != "Red" || s.Start != 0 || s.End != len(s.Value) {
				t.Errorf("a click into the box showing %q left the selection at %d–%d (focused %v); want the whole pick, 0–%d", s.Value, s.Start, s.End, s.Focused, len(s.Value))
			}
			mustRun(t, ctx, chromedp.MouseClickXY(x2, box.Y))
			if s := read(); s.Start != s.End {
				t.Errorf("a second click in the focused box left %d–%d selected; it should place the caret", s.Start, s.End)
			}

			blur()
			mustRun(t, ctx, chromedp.Focus(`#dept-combo`, chromedp.ByQuery), chromedp.KeyEvent(kb.Tab))
			if s := read(); !s.Focused || s.Start != 0 || s.End != len(s.Value) {
				t.Errorf("Tab into the box left %d–%d selected (focused %v); want the whole pick", s.Start, s.End, s.Focused)
			}
			mustRun(t, ctx, chromedp.KeyEvent("gr"))
			if s := read(); s.Value != "gr" || s.Rows != 1 {
				t.Errorf("typing over the selected pick left %q with %d rows showing; want \"gr\", filtered to Green", s.Value, s.Rows)
			}

			// A drag across "Green" as it focuses the box: from just past
			// its "Gr" to well past its end, which selects "een".
			// Whatever the drag selected stays.
			blur()
			mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("colour").value = "green"; document.getElementById("colour").dispatchEvent(new Event("change")); true`, nil))
			from, to := box.L+box.PadL+box.Gr+1, x2
			if dir == "rtl" {
				from, to = box.R-box.PadR-box.Gr-1, x1
			}
			mustRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				if err := input.DispatchMouseEvent(input.MousePressed, from, box.Y).WithButton(input.Left).WithClickCount(1).Do(ctx); err != nil {
					return err
				}
				if err := input.DispatchMouseEvent(input.MouseMoved, to, box.Y).WithButton(input.Left).Do(ctx); err != nil {
					return err
				}
				return input.DispatchMouseEvent(input.MouseReleased, to, box.Y).WithButton(input.Left).WithClickCount(1).Do(ctx)
			}))
			if s := read(); !s.Focused || s.Start == 0 && s.End == len(s.Value) {
				t.Errorf("a drag as the box was focused ended with %d–%d of %q selected (focused %v); the drag's own selection should stay", s.Start, s.End, s.Value, s.Focused)
			}

			// A tap selects the pick too: a tap selects on focus and a
			// mouse on its click, so each path is read. Not a second tap:
			// with the protocol's raw touch events Chromium keeps a
			// selection under any later tap, with or without this file's
			// handlers, so it would fence the emulation, not select.js.
			blur()
			tap := func(x float64) {
				mustRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					pt := []*input.TouchPoint{{X: x, Y: box.Y}}
					if err := input.DispatchTouchEvent(input.TouchStart, pt).Do(ctx); err != nil {
						return err
					}
					return input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}).Do(ctx)
				}), chromedp.Evaluate(afterFrame, nil, awaitPromise))
			}
			mustRun(t, ctx, emulation.SetTouchEmulationEnabled(true))
			tap(x1)
			if s := read(); !s.Focused || s.Start != 0 || s.End != len(s.Value) {
				t.Errorf("a tap into the box showing %q left %d–%d selected (focused %v); want the whole pick", s.Value, s.Start, s.End, s.Focused)
			}
		})
	}
}
