//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The way back from a shell demo is a link in the shell's foot, and it
// is a target like any other: at least --rst-target tall, 32px with a
// mouse and 44px on a phone. As a line of 12px foot text it was 15 to
// 19px, the smallest target on any page of the tree. Read on every
// shell's demo that has a foot, with a mouse at 1280px and a coarse pointer at 390px;
// a demo whose foot link is not on screen is counted, so a selector
// that stopped matching cannot pass by measuring nothing.
func TestTheWayBackFromAShellDemoIsATarget(t *testing.T) {
	const read = `(() => [...document.querySelectorAll("[rst-shell-foot] a, [rst-stage-foot] a")]
	  .filter(a => a.getAttribute("href").endsWith("/index.html") && a.checkVisibility())
	  .map(a => ({H: a.getBoundingClientRect().height,
	    Floor: parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--rst-target")) * parseFloat(getComputedStyle(document.documentElement).fontSize)})))()`
	for _, c := range []struct {
		name string
		rig  func(*testing.T) *harness.Rig
		w, h int64
		min  float64
	}{
		{"a mouse at 1280px", func(t *testing.T) *harness.Rig {
			return harness.New(t, func(string) http.Handler { return treeHandler(t) })
		}, 1280, 800, 32},
		{"a coarse pointer at 390px", phoneRig, 390, 844, 44},
	} {
		rig := c.rig(t)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		measured := 0
		for _, shell := range ui.LayoutNames() {
			where := fmt.Sprintf("the %s demo with %s", shell, c.name)
			var raw string
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(c.w, c.h),
				chromedp.Navigate(rig.Origin+shellHref(mountPath, RootTheme(), "en", shell)),
				chromedp.WaitReady("body"),
				chromedp.Evaluate(`JSON.stringify(`+read+`)`, &raw)); err != nil {
				cancel()
				t.Fatalf("%s: %v", where, err)
			}
			var links []struct{ H, Floor float64 }
			if err := json.Unmarshal([]byte(raw), &links); err != nil {
				cancel()
				t.Fatalf("%s: decoding %s: %v", where, raw, err)
			}
			for _, l := range links {
				measured++
				if l.Floor < c.min {
					t.Errorf("%s: --rst-target is %.0fpx, want at least %.0f", where, l.Floor, c.min)
				}
				if l.H < l.Floor-0.5 {
					t.Errorf("%s: the way back is %.1fpx tall, under the %.0fpx target", where, l.H, l.Floor)
				}
			}
		}
		cancel()
		if measured == 0 {
			t.Errorf("with %s no shell demo showed its way back, so this measured nothing", c.name)
		}
	}
}

// A shell demo is the shell as an app gets it, so nothing of the
// gallery's own styling may move it. The demos once linked gallery.css
// for the way back's floor, and its layout for the gallery's pinned bar
// (a body > [rst-shell-sidebar] grid with a bar row) took hold of the
// sidebar demo too: main dropped below an empty 52px strip, 96px
// between 800 and 1023px. Every element's box is read as served and
// again with any gallery stylesheet switched off; the two must agree.
// At 1280 and 900 with a mouse, either side of the gallery bar's
// two-row band, and at 390 with a coarse pointer.
func TestAShellDemoLaysOutAsAnAppWould(t *testing.T) {
	const read = `(() => {
	  const boxes = () => [...document.querySelectorAll("body *")].map(e => {
	    const b = e.getBoundingClientRect();
	    return (e.getAttribute("id") || e.tagName.toLowerCase()) + " " + [b.left, b.top, b.width, b.height].map(n => Math.round(n)).join(",");
	  });
	  const served = boxes();
	  const gallery = [...document.styleSheets].filter(s => s.href && /gallery[^/]*\.css/.test(s.href));
	  gallery.forEach(s => { s.disabled = true; });
	  const bare = boxes();
	  gallery.forEach(s => { s.disabled = false; });
	  const diff = [];
	  served.forEach((s, i) => { if (s !== bare[i]) diff.push(s + " (without gallery CSS: " + bare[i] + ")"); });
	  return JSON.stringify({Main: !!document.querySelector("main"), Diff: diff.slice(0, 5)});
	})()`
	for _, c := range []struct {
		name string
		rig  func(*testing.T) *harness.Rig
		w, h int64
	}{
		{"a mouse at 1280px", func(t *testing.T) *harness.Rig {
			return harness.New(t, func(string) http.Handler { return treeHandler(t) })
		}, 1280, 800},
		{"a mouse at 900px", func(t *testing.T) *harness.Rig {
			return harness.New(t, func(string) http.Handler { return treeHandler(t) })
		}, 900, 800},
		{"a coarse pointer at 390px", phoneRig, 390, 844},
	} {
		rig := c.rig(t)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		for _, shell := range ui.LayoutNames() {
			where := fmt.Sprintf("the %s demo with %s", shell, c.name)
			var raw string
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(c.w, c.h),
				chromedp.Navigate(rig.Origin+shellHref(mountPath, RootTheme(), "en", shell)),
				chromedp.WaitReady("body"),
				chromedp.Evaluate(read, &raw)); err != nil {
				cancel()
				t.Fatalf("%s: %v", where, err)
			}
			var got struct {
				Main bool
				Diff []string
			}
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				cancel()
				t.Fatalf("%s: decoding %s: %v", where, raw, err)
			}
			if !got.Main {
				t.Errorf("%s: no main element; the demo is not the page this test knows", where)
			}
			for _, d := range got.Diff {
				t.Errorf("%s: the gallery's CSS moves %s", where, d)
			}
		}
		cancel()
	}
}
