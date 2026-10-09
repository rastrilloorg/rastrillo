//go:build browser

package ui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
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
	// The panel keeps a box while its menu is closed (Chromium reports a
	// non-zero height for it), so the height proves nothing and the Open
	// check is what matters: without it the leg would pass on a click
	// that opened nothing.
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

// TestARowMenuOpensBesideItsOwnKebab: the panel hangs from the kebab,
// wherever the <details> around it is put. The panel used to hang from
// the <details>, which only hugged the kebab where something shrank it:
// a list grid cell, or an engine that applies justify-self to a block.
// Anywhere else (a flex column here, the gallery's bare sample in
// Safari) it was as wide as its container and the panel opened at the
// container's far edge, nowhere near the kebab just pressed.
//
// Left alone, the menu sits at its container's trailing edge in every
// engine, and the panel's trailing edge meets the kebab's. An app that
// pins it to the leading edge (the /pinned legs) gets a panel that flips
// to open from the kebab's leading edge instead. An engine without
// anchor positioning cannot flip, which is why the menu must reach the
// trailing edge on its own: the no-anchor leg is a kebab that would sit
// at the leading edge if margins left it there, and its panel must still
// open on screen. Every leg runs in RTL too, where the edges mirror.
func TestARowMenuOpensBesideItsOwnKebab(t *testing.T) {
	menu := render(t, "row-menu", map[string]any{"Name": "Grace Hopper", "Items": []any{
		map[string]any{"Label": "Edit", "Href": "/go/edit"},
		map[string]any{"Label": "Archive", "Action": "/act/a"},
		map[string]any{"Label": "Delete order…", "Href": "/go/delete", "Danger": true},
	}})
	stack := func(style string) string {
		return `<style>#box { display: flex; flex-direction: column; padding: 1rem; } ` + style + `</style><div id="box">` + menu + `</div>`
	}
	pages := map[string]string{
		"/start":  sizingDoc("a menu in a stack", stack("")),
		"/pinned": sizingDoc("a menu pinned to the leading edge", stack("#box > [rst-row-menu] { margin-inline-start: 0; }")),
	}
	rtl := map[string]string{}
	for path, html := range pages {
		rtl["/rtl"+path] = strings.Replace(html, `dir="ltr"`, `dir="rtl"`, 1)
	}
	for path, html := range rtl {
		pages[path] = html
	}
	type leg struct {
		name, path string
		w          int64
		coarse     bool
		anchor     bool
		edge       string // the kebab edge, logical, the panel must share
	}
	var legs []leg
	for _, dir := range []string{"", "/rtl"} {
		for _, l := range []leg{
			{"1280, left where it falls", "/start", 1280, false, true, "end"},
			{"1280, pinned to the leading edge", "/pinned", 1280, false, true, "start"},
			{"390 touch, left where it falls", "/start", 390, true, true, "end"},
			{"390 touch, pinned to the leading edge", "/pinned", 390, true, true, "start"},
			{"1280 without anchor positioning", "/start", 1280, false, false, "end"},
			{"390 touch without anchor positioning", "/start", 390, true, false, "end"},
		} {
			l.path = dir + l.path
			if dir != "" {
				l.name = "RTL " + l.name
			}
			legs = append(legs, l)
		}
	}
	for _, leg := range legs {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 800), chromedp.Navigate(rig.Origin+leg.path), chromedp.WaitVisible("#box", chromedp.ByQuery))
			requirePointer(t, ctx, leg.coarse)
			if !leg.anchor {
				mustRun(t, ctx, chromedp.Evaluate(galleryrig.WithoutAnchorPositioning, nil, awaitPromise))
			}
			galleryrig.RequireAnchorPositioning(t, ctx, leg.name, "[rst-row-menu-panel]", leg.anchor)
			// Edges are read in the inline direction: Start and End are
			// distances from the root box's leading edge, so one set of
			// comparisons serves both directions. The root box, not the
			// viewport: tokens.css reserves a stable scrollbar gutter, and
			// RTL puts it on the left.
			var g struct {
				Open, RTL                                      bool
				SummaryStart, SummaryEnd, PanelStart, PanelEnd float64
				PanelTop, SummaryBottom, VW                    float64
			}
			at(t, ctx, `(() => { const d = document.querySelector("[rst-row-menu]"); d.querySelector("summary").click();
			  const s = d.querySelector("summary").getBoundingClientRect(), p = d.querySelector("[rst-row-menu-panel]").getBoundingClientRect();
			  const rtl = getComputedStyle(d).direction === "rtl", h = document.documentElement.getBoundingClientRect();
			  const start = r => rtl ? h.right - r.right : r.left - h.left, end = r => rtl ? h.right - r.left : r.right - h.left;
			  return JSON.stringify({Open: d.open, RTL: rtl, SummaryStart: start(s), SummaryEnd: end(s), PanelStart: start(p), PanelEnd: end(p), PanelTop: p.top, SummaryBottom: s.bottom, VW: h.width}); })()`, &g)
			if !g.Open {
				t.Fatalf("the menu did not open; this leg proves nothing")
			}
			if g.RTL != strings.HasPrefix(leg.path, "/rtl") {
				t.Fatalf("the menu's direction is RTL %v; this leg needs %v", g.RTL, !g.RTL)
			}
			if leg.edge == "start" && g.SummaryStart > 0.25*g.VW {
				t.Fatalf("the kebab starts %.0fpx from the leading edge of a %.0fpx viewport; it is not pinned there and this leg proves nothing", g.SummaryStart, g.VW)
			}
			switch leg.edge {
			case "end":
				if g.SummaryEnd < 0.75*g.VW {
					t.Errorf("the kebab ends %.0fpx from the leading edge of a %.0fpx viewport; left where it falls it must sit at the trailing edge", g.SummaryEnd, g.VW)
				}
				if d := g.PanelEnd - g.SummaryEnd; d < -0.5 || d > 0.5 {
					t.Errorf("the panel's trailing edge is at %.0f, the kebab's at %.0f; it must open from the kebab", g.PanelEnd, g.SummaryEnd)
				}
			case "start":
				if d := g.PanelStart - g.SummaryStart; d < -0.5 || d > 0.5 {
					t.Errorf("the panel's leading edge is at %.0f, the kebab's at %.0f; with no room before the kebab it must flip and open from it", g.PanelStart, g.SummaryStart)
				}
			}
			if g.PanelStart < 0 || g.PanelEnd > g.VW+0.5 {
				t.Errorf("the panel spans %.0f to %.0f from the leading edge of a %.0fpx viewport; it must stay on screen", g.PanelStart, g.PanelEnd, g.VW)
			}
			if g.PanelTop < g.SummaryBottom {
				t.Errorf("the panel starts at %.0f, above the kebab's foot at %.0f; it must drop below the kebab", g.PanelTop, g.SummaryBottom)
			}
		})
	}
}

// TestARowsKebabSitsAtItsTrailingEdge: below 800px the list grid is
// three columns, name, one auto cell and the kebab's, whatever
// --rst-cols says. A row whose middle cells are all rst-m-hide has two
// children left, and auto-placement put the kebab in the auto cell:
// mid-row, beside the name, with the empty kebab column to its right.
// The kebab belongs in the last column at every width.
//
// With the kebab there, the empty auto column still cost the name a
// column gap (13.6px at 390) it could not use, so the name spans it.
// The control is row V, the same row with its middle cell shown: its
// name must stop at the cell, not run under it or push it down a line.
func TestARowsKebabSitsAtItsTrailingEdge(t *testing.T) {
	for _, leg := range []struct {
		name           string
		w              int64
		coarse, narrow bool
	}{{"1280 mouse", 1280, false, false}, {"600 mouse", 600, false, true}, {"390 touch", 390, true, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig, _ := rowMenuRig(t, leg.coarse)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery),
				chromedp.Evaluate(`(() => { const v = document.getElementById("row-a").cloneNode(true); v.id = "row-v"; v.querySelector(".rst-m-hide").classList.remove("rst-m-hide"); document.getElementById("list").append(v); return true; })()`, nil))
			requirePointer(t, ctx, leg.coarse)
			var rows []struct {
				ID                                     string
				KebabRight, RowEnd, NameEnd, Gap, Menu float64
				CellStart, NameBottom, CellTop         float64
			}
			at(t, ctx, `JSON.stringify([...document.querySelectorAll("#list [rst-lrow]")].map(r => {
			  const s = getComputedStyle(r), b = r.getBoundingClientRect(), n = r.querySelector(".rst-nm").getBoundingClientRect(), c = r.querySelector(":scope > span").getBoundingClientRect();
			  return {ID: r.id, KebabRight: r.querySelector("[rst-row-menu] > summary").getBoundingClientRect().right,
			    RowEnd: b.right - parseFloat(s.paddingRight) - parseFloat(s.borderRightWidth), NameEnd: n.right, Gap: parseFloat(s.columnGap),
			    Menu: parseFloat(s.gridTemplateColumns.split(" ").pop()), CellStart: c.left, NameBottom: n.bottom, CellTop: c.top};
			}))`, &rows)
			if len(rows) != 4 {
				t.Fatalf("measured %d rows, want 4", len(rows))
			}
			for _, r := range rows {
				if d := r.RowEnd - r.KebabRight; d < -0.5 || d > 0.5 {
					t.Errorf("%s: the kebab ends at %.0f, %.0fpx short of the row's trailing edge at %.0f", r.ID, r.KebabRight, d, r.RowEnd)
				}
				if !leg.narrow {
					continue
				}
				if r.ID == "row-v" {
					if d := r.CellStart - r.Gap - r.NameEnd; d < -0.5 || d > 0.5 || r.CellTop >= r.NameBottom {
						t.Errorf("%s: the name ends at %.0f and the shown cell starts at %.0f, at %.0fpx down against the name's foot at %.0fpx; want the name to stop one gap before the cell, on the same line", r.ID, r.NameEnd, r.CellStart, r.CellTop, r.NameBottom)
					}
					continue
				}
				if want := r.RowEnd - r.Menu - r.Gap; r.NameEnd < want-0.5 || r.NameEnd > want+0.5 {
					t.Errorf("%s: the name ends at %.0f, %.0fpx short of the kebab column's gap at %.0f; with every middle cell hidden it must take their column", r.ID, r.NameEnd, want-r.NameEnd, want)
				}
			}
		})
	}
}
