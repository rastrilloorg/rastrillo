//go:build browser

package sweep

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// barFit reads the bar's box, the union of what is inside it (the
// brand and each control), whether that content shares one line, and
// the height the content alone needs. The bar's own box is fixed at
// --ds-bar-h, so it cannot measure its own overflow; its children can.
// Need comes from a brief block-size: auto rather than from the
// children's own bounds, because align-items: center plus flex-wrap
// otherwise stretches a one-row bar's children toward whatever height
// the fixed box already has, so a 50px line was read back as 65px.
const barFit = `(() => {
  const bar = document.querySelector(".ds-top"), b = bar.getBoundingClientRect();
  let top = Infinity, bottom = -Infinity, left = Infinity, right = -Infinity, maxTop = -Infinity, minBottom = Infinity;
  for (const el of bar.querySelectorAll(".ds-top__brand, .ds-top__controls > *")) {
    const r = el.getBoundingClientRect();
    if (!r.width && !r.height) continue;
    top = Math.min(top, r.top); bottom = Math.max(bottom, r.bottom);
    left = Math.min(left, r.left); right = Math.max(right, r.right);
    maxTop = Math.max(maxTop, r.top); minBottom = Math.min(minBottom, r.bottom);
  }
  const prevStyle = bar.getAttribute("style");
  bar.style.blockSize = "auto";
  const need = bar.getBoundingClientRect().height;
  if (prevStyle === null) bar.removeAttribute("style"); else bar.setAttribute("style", prevStyle);
  return JSON.stringify({Top: b.top, Bottom: b.bottom, Left: b.left, Right: b.right,
    UTop: top, UBottom: bottom, ULeft: left, URight: right, Over: bar.scrollWidth - bar.clientWidth,
    Need: need, MaxTop: maxTop, MinBottom: minBottom});
})()`

type barReading struct {
	Top, Bottom, Left, Right, UTop, UBottom, ULeft, URight, Over, Need, MaxTop, MinBottom float64
}

// barHSlack is how far above the measured maximum --ds-bar-h may sit:
// values are rounded up to the next quarter rem (4px), so up to 4px of
// slack is the rounding itself rather than a reservation nobody needs.
const barHSlack = 4.5

// Every theme × locale, scripts on and off, at both edges of both
// bands: what is in the bar lies inside it, nothing in it overflows
// sideways, it is still at the top after scrolling to the end, and
// from 1024px it never wraps to a second line. A failure names the
// height the band needs, which is the number to write into
// --ds-bar-h; the final check does the same comparison the other way,
// catching a reservation nobody's content has needed since.
//
// Routine CI drives sweepLocales() rather than every locale: the full
// 36 theme × locale pages × scripts on/off × 4 widths took 284 of this
// package's 385 seconds under load, most of it re-proving what the
// narrower set already covers. make browser-sweep (RASTRILLO_SWEEP=full)
// still runs every locale, for a release that touches locale strings
// or the bar's controls.
func TestThePinnedBarFitsItsReservation(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	need := map[string]float64{}
	reservation := map[string]float64{}
	for _, scripts := range []bool{true, false} {
		tab := ctx
		if !scripts {
			tab = noScripts(t, ctx)
		}
		for _, theme := range ui.ThemeNames() {
			for _, locale := range sweepLocales() {
				url := rig.Origin + pageHref(mountPath, theme, locale, fileOf("form"))
				if err := chromedp.Run(tab, chromedp.EmulateViewport(1440, 900), chromedp.Navigate(url), chromedp.WaitReady(`.ds-top`, chromedp.ByQuery)); err != nil {
					t.Fatalf("%s/%s scripts=%v: loading: %v", theme, locale, scripts, err)
				}
				if !scripts {
					requireScriptsOff(t, tab, fmt.Sprintf("%s/%s scripts off", theme, locale))
				}
				for _, w := range []int64{1440, 1024, 1023, 800} {
					where := fmt.Sprintf("%s/%s at %dpx, scripts %v", theme, locale, w, scripts)
					band := "1024 and up"
					if w < 1024 {
						band = "800 to 1023"
					}
					var raw string
					var after float64
					if err := chromedp.Run(tab,
						chromedp.EmulateViewport(w, 900),
						chromedp.Evaluate(`scrollTo(0, 0)`, nil),
						chromedp.Evaluate(barFit, &raw),
						chromedp.Evaluate(`scrollTo(0, document.documentElement.scrollHeight); document.querySelector(".ds-top").getBoundingClientRect().top`, &after),
					); err != nil {
						t.Fatalf("%s: reading the bar: %v", where, err)
					}
					var r barReading
					if err := json.Unmarshal([]byte(raw), &r); err != nil {
						t.Fatalf("%s: decoding %q: %v", where, raw, err)
					}
					need[band] = max(need[band], r.Need)
					reservation[band] = r.Bottom - r.Top
					if r.UTop < r.Top-0.5 || r.UBottom > r.Bottom+0.5 || r.ULeft < r.Left-0.5 || r.URight > r.Right+0.5 {
						t.Errorf("%s: the bar's contents [%v…%v × %v…%v] spill out of its box [%v…%v × %v…%v]; this band needs --ds-bar-h of at least %.0fpx", where, r.ULeft, r.URight, r.UTop, r.UBottom, r.Left, r.Right, r.Top, r.Bottom, r.Need)
					}
					if r.Over > 0.5 {
						t.Errorf("%s: the bar overflows sideways by %.1fpx", where, r.Over)
					}
					if after < -0.5 || after > 0.5 {
						t.Errorf("%s: scrolled to the end, the bar's top is at %.1fpx, not pinned at 0", where, after)
					}
					if w >= 1024 && r.MaxTop >= r.MinBottom-0.5 {
						t.Errorf("%s: the brand and the controls do not share one line (the latest top %.1fpx is not above the earliest bottom %.1fpx); the bar has wrapped", where, r.MaxTop, r.MinBottom)
					}
				}
			}
		}
	}
	for _, band := range []string{"1024 and up", "800 to 1023"} {
		if over := reservation[band] - need[band]; over > barHSlack {
			t.Errorf("--ds-bar-h for %s reserves %.1fpx but the measured maximum content needs only %.1fpx, %.1fpx more than this drive's rounding slack", band, reservation[band], need[band], over)
		}
	}
	t.Logf("tallest bar contents per band (the --ds-bar-h each needs): %v", need)
}

// The bar's language menu, opened, lies inside the viewport and every
// language is reachable by keyboard: left to right and right to left,
// with anchor positioning and without, with scripts and without, at
// both edges of both bands.
func TestTheBarsLanguageMenuStaysOnScreen(t *testing.T) {
	// Scrollbars drawn and the client box as the screen: innerWidth
	// counts the 15px under the root's scrollbar, and with scrollbars
	// hidden clientWidth counts the gutter the page is not laid out in,
	// so either way a panel up to 15px past the page's edge read as on
	// screen. See RequireDrawnScrollbar.
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) }, harness.WithScrollbars())
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, scripts := range []bool{true, false} {
		tab := ctx
		if !scripts {
			tab = noScripts(t, ctx)
		}
		for _, locale := range []string{"en", "ar"} {
			for _, anchored := range []bool{true, false} {
				for _, w := range []int64{1440, 1024, 1023, 800} {
					where := fmt.Sprintf("day/%s at %dpx, scripts %v, anchor positioning %v", locale, w, scripts, anchored)
					if err := chromedp.Run(tab, chromedp.EmulateViewport(w, 900),
						chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", locale, fileOf("form"))),
						chromedp.WaitReady(`.ds-top__controls [rst-locale]`, chromedp.ByQuery)); err != nil {
						t.Fatalf("%s: loading: %v", where, err)
					}
					if !scripts {
						requireScriptsOff(t, tab, where)
					}
					if !anchored {
						if err := chromedp.Run(tab, chromedp.Evaluate(withoutAnchorPositioning, nil,
							func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
							t.Fatalf("%s: switching anchor positioning off: %v", where, err)
						}
					}
					requireAnchorPositioning(t, tab, where, ".ds-top__controls [rst-dropdown-menu]", anchored)
					requireDrawnScrollbar(t, tab, where)
					var raw string
					if err := chromedp.Run(tab, chromedp.Evaluate(`(() => {
					  const d = document.querySelector(".ds-top__controls [rst-locale]");
					  d.open = true;
					  const p = d.querySelector("[rst-dropdown-menu]").getBoundingClientRect();
					  d.querySelector("summary").focus();
					  return JSON.stringify({L: p.left, R: p.right, T: p.top, B: p.bottom, W: document.documentElement.clientWidth, H: document.documentElement.clientHeight});
					})()`, &raw)); err != nil {
						t.Fatalf("%s: opening the menu: %v", where, err)
					}
					var p struct{ L, R, T, B, W, H float64 }
					if err := json.Unmarshal([]byte(raw), &p); err != nil {
						t.Fatalf("%s: decoding %q: %v", where, raw, err)
					}
					if p.L < -0.5 || p.T < -0.5 || p.R > p.W+0.5 || p.B > p.H+0.5 {
						t.Errorf("%s: the open panel [%v…%v × %v…%v] is not inside the %vx%v client box", where, p.L, p.R, p.T, p.B, p.W, p.H)
					}
					for i := 0; i < len(rastrillo.BaseLocales()); i++ {
						var at string
						if err := chromedp.Run(tab, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`(() => {
						  const a = document.activeElement, r = a.getBoundingClientRect();
						  const de = document.documentElement, inside = r.top >= 0 && r.bottom <= de.clientHeight && r.left >= 0 && r.right <= de.clientWidth;
						  return a.closest(".ds-top__controls [rst-dropdown-menu]") && inside ? a.lang : "not a visible language: " + a.outerHTML.slice(0, 60);
						})()`, &at)); err != nil {
							t.Fatalf("%s: tab %d: %v", where, i+1, err)
						}
						if want := rastrillo.BaseLocales()[i]; at != want {
							t.Errorf("%s: tab %d lands on %s, want the %s link on screen", where, i+1, at, want)
							break
						}
					}
				}
			}
		}
	}
}
