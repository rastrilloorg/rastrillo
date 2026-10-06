//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// focusTopJS is the top of whatever has focus, or of the element a
// fragment targets, in the page's coordinates. Inside a preview frame
// the element's offset is in the frame's own pixels, and the frame is
// drawn at scale(--ds-k), so the offset is multiplied by the frame's
// rendered width over its layout width before the frame's top is added.
const focusTopJS = `((el) => {
  let top = 0, k = 1, doc = document;
  el = el || document.activeElement;
  while (el && el.tagName === "IFRAME") {
    const r = el.getBoundingClientRect();
    top += r.top; k *= r.width / el.offsetWidth;
    const d = el.contentDocument;
    if (!d || !d.activeElement || d.activeElement === d.body) break;
    el = d.activeElement; doc = d;
  }
  const r = el.getBoundingClientRect();
  return JSON.stringify({Top: el.tagName === "IFRAME" ? top : top + r.top * k, What: el.tagName.toLowerCase() + (el.id ? "#" + el.id : ""),
    Bar: document.querySelector(".ds-top").getBoundingClientRect().bottom,
    End: Math.abs(scrollY + innerHeight - document.documentElement.scrollHeight) < 2});
})`

type focusReading struct {
	Top, Bar float64
	What     string
	End      bool
}

func readFocus(t *testing.T, ctx context.Context, where, target string) focusReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(focusTopJS+"("+target+")", &raw)); err != nil {
		t.Fatalf("%s: reading focus: %v", where, err)
	}
	var r focusReading
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return r
}

// Focus and fragment targets stop below the pinned bar, never under
// it (WCAG 2.4.11): forty Tabs on from the first control in main and
// forty back, the skip link's target, and every rail fragment of the
// page. A target near the end of the page may stop lower, because the
// browser clamps the scroll at the end, but never higher.
func TestFocusIsNeverUnderThePinnedBar(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, locale := range []string{"en", "ar"} {
		for _, w := range []int64{1440, 800} {
			where := fmt.Sprintf("day/%s form at %dpx", locale, w)
			url := rig.Origin + pageHref(mountPath, "day", locale, fileOf("form"))
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(w, 900), chromedp.Navigate(url), chromedp.WaitReady(`main`, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			eagerly(t, ctx, where)
			under := func(how string, r focusReading) {
				if r.Top < r.Bar-1 {
					t.Errorf("%s: %s puts %s at %.1fpx, under the bar ending at %.1fpx", where, how, r.What, r.Top, r.Bar)
				}
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("main [rst-page]").querySelector("a[href], button, summary, input:not([type=hidden])").focus(); true`, nil)); err != nil {
				t.Fatalf("%s: focusing main's first control: %v", where, err)
			}
			for i := 0; i < 40; i++ {
				if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
					t.Fatalf("%s: tab %d: %v", where, i+1, err)
				}
				under(fmt.Sprintf("tab %d", i+1), readFocus(t, ctx, where, ""))
			}
			for i := 0; i < 40; i++ {
				if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(input.ModifierShift))); err != nil {
					t.Fatalf("%s: shift+tab %d: %v", where, i+1, err)
				}
				under(fmt.Sprintf("shift+tab %d", i+1), readFocus(t, ctx, where, ""))
			}
			// The skip link's target.
			if err := chromedp.Run(ctx, chromedp.Evaluate(`scrollTo(0, 2000); document.querySelector("[rst-skip]").click(); true`, nil)); err != nil {
				t.Fatalf("%s: following the skip link: %v", where, err)
			}
			under("the skip link", readFocus(t, ctx, where, `document.getElementById("main")`))
			// Every rail fragment of this page.
			var hrefs []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll("#ds-nav a[href*='#']")].filter(a => a.pathname === location.pathname).map(a => a.hash.slice(1))`, &hrefs)); err != nil {
				t.Fatalf("%s: listing the rail's fragments: %v", where, err)
			}
			if len(hrefs) == 0 {
				t.Fatalf("%s: the rail links no fragment of this page", where)
			}
			for _, id := range hrefs {
				if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`document.querySelector('#ds-nav a[href$="#%s"]').click(); true`, id), nil)); err != nil {
					t.Fatalf("%s: following #%s: %v", where, id, err)
				}
				r := readFocus(t, ctx, where, fmt.Sprintf(`document.getElementById(%q)`, id))
				under("#"+id, r)
				if !r.End && r.Top > r.Bar+40 {
					t.Errorf("%s: #%s lands at %.1fpx, far below the bar's %.1fpx with the page not at its end; the scroll padding is not what the fragment stopped at", where, id, r.Top, r.Bar)
				}
			}
		}
	}
}

// stripBadJS scans down the page in steps of at most half the strip's
// own height — a gap any wider could let a strip-sized overlap fall
// entirely between two samples — sampling roughly every 32px across the
// strip's width at its top, middle and bottom edges. It reports every
// sampled point where elementFromPoint lands outside the strip.
const stripBadJS = `((sel) => {
  const strip = document.querySelector(sel);
  const h = strip.getBoundingClientRect().height, step = Math.max(1, h / 2);
  const bad = [];
  for (let y = 0; y < document.documentElement.scrollHeight; y += step) {
    scrollTo(0, y);
    const r = strip.getBoundingClientRect();
    for (const yy of [r.top + 2, (r.top + r.bottom) / 2, r.bottom - 2]) {
      for (let x = r.left; x <= r.right; x += 32) {
        const hit = document.elementFromPoint(x, yy);
        if (!hit || !strip.contains(hit)) bad.push({X: Math.round(x), Y: Math.round(yy), Hit: hit ? hit.tagName + "." + hit.className : "nothing"});
      }
    }
  }
  return JSON.stringify(bad);
})`

// stripH2PointsJS finds, at the same scroll steps and sample points
// stripBadJS uses, every point that a <main> heading's own box covers
// — by geometry, not elementFromPoint, since a heading under the strip
// is exactly what elementFromPoint cannot see. TestNothingPaintsOverTheBarOrTheBackStrip
// uses this to check the scan's own sensitivity: every point here
// should turn up in stripBadJS once the heading is raised above the
// strip, or the scan's sampling is too coarse to trust.
const stripH2PointsJS = `((sel) => {
  const strip = document.querySelector(sel);
  const h = strip.getBoundingClientRect().height, step = Math.max(1, h / 2);
  const pts = [];
  for (let y = 0; y < document.documentElement.scrollHeight; y += step) {
    scrollTo(0, y);
    const r = strip.getBoundingClientRect();
    const h2s = [...document.querySelectorAll("main h2")].map(h2 => h2.getBoundingClientRect());
    for (const yy of [r.top + 2, (r.top + r.bottom) / 2, r.bottom - 2]) {
      for (let x = r.left; x <= r.right; x += 32) {
        if (h2s.some(hr => x >= hr.left && x <= hr.right && yy >= hr.top && yy <= hr.bottom)) {
          pts.push({X: Math.round(x), Y: Math.round(yy)});
        }
      }
    }
  }
  return JSON.stringify(pts);
})`

type stripPoint struct {
	X, Y float64
	Hit  string
}

func evalStripPoints(t *testing.T, ctx context.Context, where, js, selector string) []stripPoint {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js+"("+fmt.Sprintf("%q", selector)+")", &raw)); err != nil {
		t.Fatalf("%s: scanning: %v", where, err)
	}
	var pts []stripPoint
	if err := json.Unmarshal([]byte(raw), &pts); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return pts
}

// Nothing in the content paints over the pinned chrome: at 1440px the
// bar, and at 390px the shell's back strip, are what elementFromPoint
// finds at their top, middle and bottom edges, everywhere down a page
// of positioned boxes and transformed frames. A second pass raises
// every <main> heading to z-index: 11, above the strip's 10, and
// checks that every sampled point a heading's own box covers is now
// among the scan's hits — the control for the scan's own density, since
// a coarser "any heading somewhere near this step" check missed a
// heading that only grazed the strip's edge at a few scroll positions.
func TestNothingPaintsOverTheBarOrTheBackStrip(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
	defer cancel()
	for _, c := range []struct {
		w     int64
		strip string
	}{{1440, ".ds-top"}, {390, "[rst-shell-back]"}} {
		where := fmt.Sprintf("form at %dpx", c.w)
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(c.w, 844),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
			chromedp.WaitVisible(c.strip, chromedp.ByQuery)); err != nil {
			t.Fatalf("%s: loading: %v", where, err)
		}
		eagerly(t, ctx, where)
		clickAll(t, ctx, where, clickedDesktop, "Desktop")
		before := evalStripPoints(t, ctx, where, stripBadJS, c.strip)
		if len(before) > 0 {
			if len(before) > 6 {
				before = before[:6]
			}
			t.Errorf("%s: something paints over %s: %v", where, c.strip, before)
		}
		h2Points := evalStripPoints(t, ctx, where, stripH2PointsJS, c.strip)
		// No heading ever under the strip would make the control below
		// vacuous: it checks the scan catches every point a raised
		// heading covers, and zero points are caught by any scan.
		if len(h2Points) == 0 {
			t.Fatalf("%s: no heading passes under %s at any scroll step, so nothing checks the scan can see one", where, c.strip)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.head.insertAdjacentHTML("beforeend", "<style>main h2{position:relative;z-index:11}</style>"); true`, nil)); err != nil {
			t.Fatalf("%s: raising every heading over the strip: %v", where, err)
		}
		after := evalStripPoints(t, ctx, where, stripBadJS, c.strip)
		caught := make(map[[2]int]bool, len(after))
		for _, p := range after {
			caught[[2]int{int(p.X), int(p.Y)}] = true
		}
		var missed []stripPoint
		for _, p := range h2Points {
			if !caught[[2]int{int(p.X), int(p.Y)}] {
				missed = append(missed, p)
			}
		}
		if len(missed) > 0 {
			if len(missed) > 6 {
				missed = missed[:6]
			}
			t.Errorf("%s: a heading raised above %s went uncaught at %d of %d points its own box covers: %v", where, c.strip, len(missed), len(h2Points), missed)
		}
	}
}

// Right to left, the shell's grid puts the rail on the right and the
// bar over main on the left, with no rule of the gallery's: the grid
// lines are numbered, not sided.
func TestTheFrameMirrorsRightToLeft(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var raw string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "ar", fileOf("form"))),
		chromedp.WaitVisible(`.ds-top`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
		  const b = s => document.querySelector(s).getBoundingClientRect();
		  const rail = b("[rst-shell-rail]"), bar = b(".ds-top"), main = b("main");
		  const brand = b(".ds-top__brand"), controls = b(".ds-top__controls");
		  return JSON.stringify({RailLeft: rail.left, MainRight: main.right, BarBottom: bar.bottom, MainTop: main.top, BarLeft: bar.left, BarRight: bar.right, MainLeft: main.left, BrandLeft: brand.left, ControlsRight: controls.right});
		})()`, &raw)); err != nil {
		t.Fatalf("day/ar form at 1280px: %v", err)
	}
	var r struct {
		RailLeft, MainRight, BarBottom, MainTop, BarLeft, BarRight, MainLeft, BrandLeft, ControlsRight float64
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if r.RailLeft < r.MainRight-0.5 {
		t.Errorf("in Arabic the rail starts at %.1fpx, left of main's right edge %.1fpx; it belongs on the right", r.RailLeft, r.MainRight)
	}
	if r.BarBottom > r.MainTop+0.5 || r.BarLeft < r.MainLeft-0.5 || r.BarRight > r.MainRight+0.5 {
		t.Errorf("in Arabic the bar [%v…%v, bottom %v] is not above main [%v…%v, top %v]", r.BarLeft, r.BarRight, r.BarBottom, r.MainLeft, r.MainRight, r.MainTop)
	}
	// The brand is first in source order and justify-content:
	// space-between puts the first child at the inline start, which in
	// Arabic is the bar's right side, not its left — a rule keyed to
	// "left" rather than to the writing direction would swap these two.
	if r.BrandLeft < r.ControlsRight-0.5 {
		t.Errorf("in Arabic the brand's left edge is at %.1fpx, not right of the controls' right edge %.1fpx; the brand belongs at the inline start and the controls at the end", r.BrandLeft, r.ControlsRight)
	}
}
