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
)

// TestTheDemoRequestsRowsAreClickableEdgeToEdge is the whole-row
// target's demo leg. The Requests list is a list grid with a status
// pill in every row, written with no markup of its own for the
// whole-row rule, so it is the check that the rule reaches a real
// screen from tokens.css alone. At 1280 with a mouse and 390 with a
// coarse pointer, a click at the row's far edge opens the request; at
// 1280, where the status and updated columns are shown, so does a click
// on the pill and on the time. The destination is read off the request
// page's own section being in the document, which only the click can
// produce, never off the address alone. The list and the request are
// pages of their own, so each click is followed by a load of the list
// again.
func TestTheDemoRequestsRowsAreClickableEdgeToEdge(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280 mouse", 1280, 900, false}, {"390 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			var opts []harness.Option
			if leg.coarse {
				opts = append(opts, harness.WithCoarsePointer())
			}
			rig := harness.New(t, func(string) http.Handler { return treeHandler(t) }, opts...)
			ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
			defer cancel()
			url := rig.Origin + demoPageHref(mountPath, RootTheme(), "en", "requests")
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(url)); err != nil {
				t.Fatal(err)
			}
			var coarse bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`matchMedia("(pointer: coarse)").matches`, &coarse)); err != nil {
				t.Fatal(err)
			}
			if coarse != leg.coarse {
				t.Fatalf("(pointer: coarse) is %v, this leg needs %v; the rig is not the device this leg claims to measure", coarse, leg.coarse)
			}

			const row = `#view-requests [rst-lrow]:not([rst-lrow~="head"])`
			for _, c := range []struct {
				where, sel string
				fx, dx     float64
				wideOnly   bool
			}{
				{"the first row's far edge", row, 1, -12, false},
				// Both cells are rst-m-hide: below 800px they have no box.
				{"the first row's status pill", row + ` [rst-status]`, 0.5, 0, true},
				{"the first row's updated time", row + ` .rst-cell-mut`, 0.5, 0, true},
			} {
				if c.wideOnly && leg.coarse {
					continue
				}
				// Back to the list, a page of its own, settled: a click sent
				// during the narrow slide between pages lands on the
				// transition's snapshot and does nothing.
				if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
					t.Fatal(err)
				}
				demoUntil(t, ctx, `!!document.getElementById("view-requests") && document.readyState === "complete" && !document.activeViewTransition`)
				var p struct {
					X, Y float64
					Hit  string
				}
				var raw string
				if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => { const el = document.querySelector(%q); el.scrollIntoView({block: "center"}); const r = el.getBoundingClientRect();
				  const x = r.left + r.width * %v + %v, y = r.top + r.height / 2, h = document.elementFromPoint(x, y), link = document.querySelector(%q);
				  return JSON.stringify({X: x, Y: y, Hit: h === link ? "the row's link" : h ? h.tagName + " " + h.className : "nothing"}); })()`, c.sel, c.fx, c.dx, row+` > a.rst-nm`), &raw)); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(raw), &p); err != nil {
					t.Fatal(err)
				}
				if p.Hit != "the row's link" {
					t.Errorf("%s: at %s the element under the pointer is %s, want the row's link", leg.name, c.where, p.Hit)
				}
				if err := chromedp.Run(ctx, chromedp.MouseClickXY(p.X, p.Y)); err != nil {
					t.Fatal(err)
				}
				if !demoPoll(ctx, `location.pathname.endsWith("/demo-request.html") && !!document.getElementById("view-request")`) {
					t.Errorf("%s: a click at %s (%.0f, %.0f) did not open the request", leg.name, c.where, p.X, p.Y)
				}
			}
		})
	}
}

// demoUntil fails the test unless expr becomes true within 10s.
func demoUntil(t *testing.T, ctx context.Context, expr string) {
	t.Helper()
	if !demoPoll(ctx, expr) {
		t.Fatalf("never became true within 10s: %s", expr)
	}
}

// demoPoll reports whether a page expression became true within 10s.
func demoPoll(ctx context.Context, expr string) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var yes bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &yes)); err == nil && yes {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
