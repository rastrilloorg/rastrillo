//go:build browser

package ui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// rowMenuPage is three list-grid rows, each with a row menu rendered by
// the partial: a GET item, a POST item carrying two hidden fields, and
// a destructive link. The third row's POST label is forty-odd
// characters, the case the spinner slot exists for. /bottom is the same
// list pushed to the foot of the viewport.
func rowMenuPage(t *testing.T) (map[string]string, chan string) {
	t.Helper()
	row := func(id, name, post string) string {
		return `<div rst-lrow id="row-` + id + `"><a class="rst-nm" href="/go/row-` + id + `">` + name + `</a><span class="rst-m-hide rst-cell-mut">Paid</span>` +
			render(t, "row-menu", map[string]any{"Name": name, "Items": []any{
				map[string]any{"Label": "View", "Href": "/go/view-" + id},
				map[string]any{"Label": post, "Action": "/act/" + id, "Hidden": [][2]string{{"state", "archived"}, {"from", "list"}}},
				map[string]any{"Label": "Delete order…", "Href": "/go/delete-" + id, "Danger": true},
			}}) + `</div>`
	}
	list := `<div rst-card id="list" style="--rst-cols: minmax(0, 1fr) 110px var(--rst-col-menu)">` +
		row("a", "Grace Hopper", "Archive") + row("b", "Alan Turing", "Archive") +
		row("c", "Ada Lovelace", "Archive this order and notify the customer") + `</div>`
	return map[string]string{
		"/":       sizingDoc("row menus", `<div rst-page><p id="outside">Outside every menu.</p>`+list+`</div>`),
		"/bottom": sizingDoc("row menus at the bottom", `<div rst-page><div style="block-size: 150vh"></div>`+list+`</div>`),
		"/go/":    sizingDoc("landed", `<p id="landed">landed</p>`),
	}, make(chan string, 8)
}

func rowMenuRig(t *testing.T, coarse bool, opts ...harness.Option) (*harness.Rig, chan string) {
	t.Helper()
	pages, posted := rowMenuPage(t)
	if coarse {
		opts = append(opts, harness.WithCoarsePointer())
	}
	rig := harness.New(t, func(string) http.Handler {
		mux := sizingMux(t, pages)
		mux.HandleFunc("POST /act/", func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			posted <- r.URL.Path + "?" + string(body)
			http.Redirect(w, r, "/go/done", http.StatusSeeOther)
		})
		return mux
	}, opts...)
	return rig, posted
}

// mustRun is chromedp.Run that fails the test on a browser error: a
// swallowed error would leave the leg reading a page that never got
// where the leg thinks it is, and passing on it.
func mustRun(t *testing.T, ctx context.Context, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatal(err)
	}
}

func openMenus(t *testing.T, ctx context.Context) string {
	t.Helper()
	var s string
	mustRun(t, ctx, chromedp.Evaluate(`[...document.querySelectorAll("[rst-row-menu]")].filter(d => d.open).map(d => d.closest("[rst-lrow]").id).join(",")`, &s))
	return s
}

// TestRowMenusExcludeEachOtherWithNoScript: the native group, with
// script execution disabled in the engine. B opens first, then A: an
// open panel drops over the rows below it, so a click aimed at B's
// kebab with A open would land on A's items and submit or follow one.
func TestRowMenusExcludeEachOtherWithNoScript(t *testing.T) {
	rig, _ := rowMenuRig(t, false)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, emulation.SetScriptExecutionDisabled(true), chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery),
		chromedp.Click("#row-b summary", chromedp.ByQuery))
	if got := openMenus(t, ctx); got != "row-b" {
		t.Fatalf("with no script, open menus are %q after opening B, want row-b; the second click proves nothing", got)
	}
	mustRun(t, ctx, chromedp.Click("#row-a summary", chromedp.ByQuery))
	if got := openMenus(t, ctx); got != "row-a" {
		t.Errorf("with no script, open menus are %q after opening B then A, want only row-a", got)
	}
}

// TestRowMenuDismissesAndHandsFocusBack: an outside click and Escape
// close it (rastrillo.js), and Escape puts focus back on its summary.
// Clicking an item goes to the item, never the row.
func TestRowMenuDismissesAndHandsFocusBack(t *testing.T) {
	rig, _ := rowMenuRig(t, false)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery))
	mustRun(t, ctx, chromedp.Click("#row-a summary", chromedp.ByQuery))
	if got := openMenus(t, ctx); got != "row-a" {
		t.Fatalf("clicking row A's kebab left %q open, want row-a; the dismissal legs prove nothing", got)
	}
	mustRun(t, ctx, chromedp.Click("#outside", chromedp.ByQuery))
	if got := openMenus(t, ctx); got != "" {
		t.Errorf("after an outside click, %q is still open", got)
	}
	var focus string
	mustRun(t, ctx,
		chromedp.Click("#row-a summary", chromedp.ByQuery), chromedp.Focus("#row-a [rst-row-menu-panel] a", chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Evaluate(`document.activeElement.closest("[rst-lrow]").id + " " + document.activeElement.tagName`, &focus))
	if got := openMenus(t, ctx); got != "" || focus != "row-a SUMMARY" {
		t.Errorf("after Escape: open %q, focus on %q; want none open and focus on row-a's summary", got, focus)
	}
	mustRun(t, ctx, chromedp.Click("#row-a summary", chromedp.ByQuery))
	clickAndLand(t, ctx, probe(t, ctx, "#row-a [rst-row-menu-panel] a", 0.5, 0.5, 0, 0), "/go/view-a")
}

// TestARowMenuNearTheBottomOpensUpward: anchor positioning's flip-block
// (Chromium implements it), so the last row's menu is not cut off.
func TestARowMenuNearTheBottomOpensUpward(t *testing.T) {
	rig, _ := rowMenuRig(t, false)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var g struct {
		Open                                          bool
		PanelTop, PanelBottom, PanelH, SummaryTop, VH float64
	}
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 700), chromedp.Navigate(rig.Origin+"/bottom"), chromedp.WaitReady("#list", chromedp.ByQuery),
		chromedp.Evaluate(`window.scrollTo(0, document.documentElement.scrollHeight), true`, nil),
		chromedp.Evaluate(`document.querySelector("#row-c summary").click(), true`, nil))
	at(t, ctx, `(() => { const d = document.querySelector("#row-c [rst-row-menu]"), p = d.querySelector("[rst-row-menu-panel]").getBoundingClientRect(), s = d.querySelector("summary").getBoundingClientRect(); return JSON.stringify({Open: d.open, PanelTop: p.top, PanelBottom: p.bottom, PanelH: p.height, SummaryTop: s.top, VH: innerHeight}); })()`, &g)
	// A closed menu's panel is display: none, an all-zero box whose
	// bottom (0) is above any summary: without this the leg would pass on
	// a click that opened nothing.
	if !g.Open || g.PanelH <= 0 {
		t.Fatalf("the last row's menu did not open (open %v, panel %.0fpx tall); this leg proves nothing", g.Open, g.PanelH)
	}
	if g.SummaryTop < g.VH-200 {
		t.Fatalf("the last row's kebab is at %.0f in a %.0fpx viewport; it is not near the bottom and this leg proves nothing", g.SummaryTop, g.VH)
	}
	if g.PanelBottom > g.SummaryTop+0.5 {
		t.Errorf("the panel ends at %.0f, below its summary's top (%.0f): it opened downward off the screen", g.PanelBottom, g.SummaryTop)
	}
	if g.PanelTop < 0 {
		t.Errorf("the panel starts at %.0f, above the viewport: it flipped upward and was cut off at the top", g.PanelTop)
	}
}

// postBusyJS clicks a row's POST item and reads, in the same task, the item
// and the panel before and after busy.js has put its spinner in.
const postBusyJS = `((row) => {
  const d = document.querySelector("#row-" + row + " [rst-row-menu]"); d.open = true;
  const btn = d.querySelector("[rst-row-menu-panel] button"), panel = d.querySelector("[rst-row-menu-panel]");
  const box = el => { const r = el.getBoundingClientRect(); return [r.width, r.height]; };
  const before = [box(btn), box(panel)];
  btn.click();
  const spin = btn.querySelector("[rst-spin]");
  const sr = spin ? spin.getBoundingClientRect() : null, br = btn.getBoundingClientRect();
  // The label as it is drawn: every line box of its text. The spinner is
  // out of flow, so the boxes above cannot see it sit on the words; this
  // is the reading that can.
  const range = document.createRange(), lines = [];
  for (const n of btn.childNodes) {
    if (n.nodeType !== 3 || !n.textContent.trim()) continue;
    range.selectNodeContents(n);
    lines.push(...range.getClientRects());
  }
  const hits = sr ? lines.filter(l => l.right > sr.left + 0.5 && l.left < sr.right - 0.5 && l.bottom > sr.top + 0.5 && l.top < sr.bottom - 0.5).length : 0;
  return JSON.stringify({Before: before, After: [box(btn), box(panel)], Spin: !!spin, Lines: lines.length, Overlap: hits,
    InSlot: !!sr && sr.left >= br.right - 40 && sr.right <= br.right + 0.5,
    Animation: spin ? getComputedStyle(spin).animationName : ""});
})(%q)`

// TestAPostItemsSpinnerNeverMovesTheMenu: busy.js's spinner sits in its
// reserved slot at the inline end, and neither the item's nor the
// panel's box changes size, including for a forty-odd-character label
// at 320px; under reduced motion the ring is still. The request goes to
// the item's own action with only its own fields.
//
// The long label at 1280 is the leg that catches a missing slot: there
// the panel is as wide as that label on one line, so the text runs to
// the item's content edge, where the spinner goes. At 320 the panel is
// held to the room beside the kebab and the label wraps at a word, which
// leaves slack at the end of each line that an unreserved spinner can
// happen to fit in.
func TestAPostItemsSpinnerNeverMovesTheMenu(t *testing.T) {
	for _, leg := range []struct {
		name            string
		w               int64
		row             string
		reduced, coarse bool
	}{
		{"1280, a short label", 1280, "a", false, false},
		{"1280, a long label on one line", 1280, "c", false, false},
		{"320, a long label", 320, "c", false, false},
		{"320, reduced motion", 320, "c", true, false},
		// On a touch screen the item is a 44px flex row, a different
		// layout for the spinner to sit in.
		{"320 touch, a long label", 320, "c", false, true},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig, posted := rowMenuRig(t, leg.coarse)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			acts := []chromedp.Action{chromedp.EmulateViewport(leg.w, 800)}
			if leg.reduced {
				acts = append(acts, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}))
			}
			acts = append(acts, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery))
			mustRun(t, ctx, acts...)
			// busy.js is deferred: a click before it has wired the submit
			// handler would post with no spinner and read as a failure of
			// the slot rather than of the wait.
			settleUntil(t, ctx, `document.readyState === "complete"`)
			requirePointer(t, ctx, leg.coarse)
			var g struct {
				Before, After  [2][2]float64
				Spin, InSlot   bool
				Lines, Overlap int
				Animation      string
			}
			at(t, ctx, fmt.Sprintf(postBusyJS, leg.row), &g)
			if !g.Spin {
				t.Fatalf("no spinner appeared; busy.js did not run and this leg proves nothing")
			}
			if g.Before != g.After {
				t.Errorf("item and panel went from %v to %v when the spinner appeared", g.Before, g.After)
			}
			if !g.InSlot {
				t.Errorf("the spinner is not in the reserved slot at the item's inline end")
			}
			if g.Lines == 0 {
				t.Fatalf("the label has no rendered text to compare with the spinner; the leg reads nothing")
			}
			if g.Overlap > 0 {
				t.Errorf("the spinner sits on %d of the label's %d lines; the reserved slot is not keeping them apart", g.Overlap, g.Lines)
			}
			if leg.reduced && g.Animation != "none" {
				t.Errorf("the spinner animates (%s) under reduced motion", g.Animation)
			}
			select {
			case got := <-posted:
				if want := "/act/" + leg.row + "?state=archived&from=list"; got != want {
					t.Errorf("the POST was %q, want %q (its own action, only its own fields, in order)", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("the POST item never submitted")
			}
		})
	}
}

// TestRowMenuTargetsAreTapsOnAPhone: trigger and items at 44px.
func TestRowMenuTargetsAreTapsOnAPhone(t *testing.T) {
	rig, _ := rowMenuRig(t, true)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector("#row-a [rst-row-menu]").open = true`, nil))
	requirePointer(t, ctx, true)
	got := readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify(measure(document.querySelector("#row-a [rst-row-menu]"))); })()`)
	if len(got) != 4 {
		t.Fatalf("measured %d controls in row A's open menu, want the summary and three items", len(got))
	}
	assertTargets(t, "390 touch, row A's menu", got)
}
