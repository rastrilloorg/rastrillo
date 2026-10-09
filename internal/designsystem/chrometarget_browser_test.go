//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The gallery's own controls carry ds- classes the framework's floors
// cannot match, so each reads --rst-target itself. Held exactly, not
// "at least": each control must be the larger of the floor and its own
// height with the floor lifted, so the floor never adds to a control
// already over it. The rail's section headings once read the floor on
// the content box and grew from 28 to 43px with their padding on top,
// which a minimum alone passes. A heading that wraps is taller than
// the floor by its second line, and that is its own height. The example
// tabs (Desktop, Mobile, HTML, Template) were 27px with a mouse and 29
// on a phone, and the Copy buttons on the code 27px with a mouse, their
// 44px reaching only a coarse pointer and not a narrow mouse window.
// Copy is held on both axes. Read on a section page with a mouse at
// 1280px, and at 600px and with a coarse pointer at 390px, where the
// floor is 44 and the rail is not drawn.
func TestTheGallerysOwnControlsAreExactlyTheFloor(t *testing.T) {
	const read = `(() => {
	  const floor = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--rst-target")) * parseFloat(getComputedStyle(document.documentElement).fontSize);
	  const h = e => Math.round(e.getBoundingClientRect().height * 10) / 10;
	  const each = sel => [...document.querySelectorAll(sel)].filter(e => e.checkVisibility()).map(e => {
	    const got = h(e); e.style.minBlockSize = "0px"; const own = h(e); e.style.minBlockSize = "";
	    return {Got: got, Want: Math.max(floor, own), W: e.getBoundingClientRect().width}; });
	  document.querySelector(".ds-view__tab--h input").click();
	  return JSON.stringify({Floor: floor, Heads: each(".ds-nav > details > summary"), Tabs: each(".ds-view__tab"), Copies: each(".ds-copy")});
	})()`
	for _, c := range []struct {
		name  string
		rig   func(*testing.T) *harness.Rig
		w, h  int64
		floor float64
		rail  bool
	}{
		{"a mouse at 1280px", func(t *testing.T) *harness.Rig {
			return harness.New(t, func(string) http.Handler { return treeHandler(t) })
		}, 1280, 800, 32, true},
		{"a mouse at 600px", func(t *testing.T) *harness.Rig {
			return harness.New(t, func(string) http.Handler { return treeHandler(t) })
		}, 600, 800, 44, false},
		{"a coarse pointer at 390px", phoneRig, 390, 844, 44, false},
	} {
		rig := c.rig(t)
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		var raw string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(c.w, c.h),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", "primitives.html")),
			chromedp.WaitReady("body"), chromedp.Evaluate(read, &raw)); err != nil {
			cancel()
			t.Fatalf("%s: %v", c.name, err)
		}
		cancel()
		type reading struct{ Got, Want, W float64 }
		var got struct {
			Floor               float64
			Heads, Tabs, Copies []reading
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: decoding %s: %v", c.name, raw, err)
		}
		if got.Floor != c.floor {
			t.Errorf("%s: --rst-target is %vpx, want %v", c.name, got.Floor, c.floor)
		}
		if len(got.Tabs) == 0 || len(got.Copies) == 0 || (c.rail && len(got.Heads) == 0) {
			t.Fatalf("%s: read %d example tabs, %d Copy buttons and %d rail headings; the selectors no longer match the page", c.name, len(got.Tabs), len(got.Copies), len(got.Heads))
		}
		for _, h := range got.Copies {
			if math.Abs(h.Got-h.Want) > 0.05 || h.W < got.Floor-0.05 {
				t.Errorf("%s: a Copy button is %v by %vpx, want at least %v wide and %v tall, the floor or its own height", c.name, h.W, h.Got, got.Floor, h.Want)
				break
			}
		}
		for _, h := range got.Tabs {
			if math.Abs(h.Got-h.Want) > 0.05 {
				t.Errorf("%s: an example tab is %vpx tall, want %v, the floor or its own height", c.name, h.Got, h.Want)
				break
			}
		}
		for _, h := range got.Heads {
			if math.Abs(h.Got-h.Want) > 0.05 {
				t.Errorf("%s: a rail section heading is %vpx tall, want %v, the floor or its own height", c.name, h.Got, h.Want)
				break
			}
		}
	}
}
