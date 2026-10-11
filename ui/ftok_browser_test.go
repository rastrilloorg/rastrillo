//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// ftokJS reads the filter chip's × against the chip's inner edges (its
// border box less its border) and the two corners of the ×'s hover
// fill on its trailing side.
const ftokJS = `(() => {
  const chip = document.querySelector("[rst-ftok]"), x = chip.querySelector("a");
  const c = chip.getBoundingClientRect(), r = x.getBoundingClientRect(), s = getComputedStyle(chip), xs = getComputedStyle(x);
  const rtl = getComputedStyle(chip).direction === "rtl";
  const inner = {top: c.top + parseFloat(s.borderTopWidth), bottom: c.bottom - parseFloat(s.borderBottomWidth),
    left: c.left + parseFloat(s.borderLeftWidth), right: c.right - parseFloat(s.borderRightWidth)};
  return JSON.stringify({
    Trailing: rtl ? r.left - inner.left : inner.right - r.right,
    Top: r.top - inner.top, Bottom: inner.bottom - r.bottom, W: r.width, H: r.height,
    ChipRadius: parseFloat(s.borderTopLeftRadius) - parseFloat(s.borderTopWidth),
    EndRadii: rtl ? [xs.borderTopLeftRadius, xs.borderBottomLeftRadius] : [xs.borderTopRightRadius, xs.borderBottomRightRadius]});
})()`

// TestTheFilterChipsRemoveFillsItsEnd: the ×'s hover fill sat inset
// from the chip's trailing border by the chip's own padding, so the
// fill read as a button floating inside the chip with a gap after it.
// It now fills the chip's trailing end: flush with the inner edge of
// the border on the trailing side, the top and the bottom, its fill's
// trailing corners the chip's own radius less the border, and still
// --rst-target square. In both directions, at 1280 with a mouse and
// 390 with touch.
func TestTheFilterChipsRemoveFillsItsEnd(t *testing.T) {
	chip := `<div rst-page><p><span rst-ftok><span rst-ftok-k>Paid</span><a href="/orders" aria-label="Remove filter Paid">✕</a></span></p></div>`
	pages := map[string]string{
		"/":    sizingDoc("ftok", chip),
		"/rtl": strings.Replace(sizingDoc("ftok", chip), `dir="ltr"`, `dir="rtl"`, 1),
	}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
		target float64
	}{{"1280x900 mouse", 1280, 900, false, 32}, {"390x844 touch", 390, 844, true, 44}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h)); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/", "/rtl"} {
				var raw string
				if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("[rst-ftok] a", chromedp.ByQuery), chromedp.Evaluate(ftokJS, &raw)); err != nil {
					t.Fatal(err)
				}
				var got struct {
					Trailing, Top, Bottom, W, H, ChipRadius float64
					EndRadii                                [2]string
				}
				if err := json.Unmarshal([]byte(raw), &got); err != nil {
					t.Fatal(err)
				}
				where := leg.name + " " + map[string]string{"/": "ltr", "/rtl": "rtl"}[path]
				if math.Abs(got.Trailing) > 0.5 || math.Abs(got.Top) > 0.5 || math.Abs(got.Bottom) > 0.5 {
					t.Errorf("%s: the × sits %.1fpx from the chip's trailing border, %.1f from its top and %.1f from its bottom; want it flush on all three", where, got.Trailing, got.Top, got.Bottom)
				}
				if math.Abs(got.W-leg.target) > 0.5 || math.Abs(got.H-leg.target) > 0.5 {
					t.Errorf("%s: the × is %.1f×%.1f, want the %v target", where, got.W, got.H, leg.target)
				}
				want := fmtPx(got.ChipRadius)
				if got.EndRadii[0] != want || got.EndRadii[1] != want {
					t.Errorf("%s: the ×'s trailing corners are %v, want the chip's inner radius %s on both", where, got.EndRadii, want)
				}
			}
		})
	}
}

// fmtPx spells a length as getComputedStyle does: "6px", "6.5px".
func fmtPx(v float64) string {
	b, _ := json.Marshal(v)
	return string(b) + "px"
}
