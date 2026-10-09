//go:build browser

package ui

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// segTabsPage is seg-tabs over a list card, as a list screen stacks
// them, and over a box, whose own top margin must not double the gap.
func segTabsPage(t *testing.T) map[string]string {
	t.Helper()
	tabs := func(id string) string {
		return `<div id="` + id + `-tabs">` + render(t, "seg-tabs", map[string]any{"Label": "Requests", "Items": []any{
			map[string]any{"Label": "All", "Href": "/", "Current": true},
			map[string]any{"Label": "Open", "Href": "/"},
			map[string]any{"Label": "Resolved", "Href": "/"},
		}}) + `</div>`
	}
	return map[string]string{"/": sizingDoc("seg-tabs", `<div rst-page>`+
		tabs("card")+`<div rst-card id="card"><div rst-lrow><span>A row</span></div></div>`+
		tabs("box")+`<section rst-box id="box">A box</section>`+
		`</div>`)}
}

// TestSegTabsKeepAGapBelowThem: every block component leaves
// var(--rst-sp-4) under itself, and seg-tabs left nothing, so on a
// list screen the tabs sat flush on the card below them, the two
// reading as one control.
func TestSegTabsKeepAGapBelowThem(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w      int64
		coarse bool
	}{{"1280 mouse", 1280, false}, {"390 touch", 390, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, segTabsPage(t))
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#box", chromedp.ByQuery))
			requirePointer(t, ctx, leg.coarse)
			var g struct{ Want, Card, Box float64 }
			at(t, ctx, `(() => { const gap = id => document.getElementById(id).getBoundingClientRect().top - document.querySelector("#" + id + "-tabs [rst-seg-tabs]").getBoundingClientRect().bottom;
			  return JSON.stringify({Want: parseFloat(getComputedStyle(document.getElementById("box")).marginTop), Card: gap("card"), Box: gap("box")}); })()`, &g)
			if g.Want <= 0 {
				t.Fatalf("the box's top margin reads %.1fpx; there is no block spacing to compare with and this leg proves nothing", g.Want)
			}
			if d := g.Card - g.Want; d < -0.5 || d > 0.5 {
				t.Errorf("the gap between the tabs and the card under them is %.1fpx, want the block spacing, %.1fpx", g.Card, g.Want)
			}
			if d := g.Box - g.Want; d < -0.5 || d > 0.5 {
				t.Errorf("the gap between the tabs and the box under them is %.1fpx, want the block spacing, %.1fpx, once", g.Box, g.Want)
			}
		})
	}
}

// TestADetailListsLabelSitsOnItsValuesFirstLine: a label is smaller
// than its value, and the grid stretched both to the row and set each
// at its own top, so every label rode a pixel or two above the line it
// names. A label must share its value's first baseline, and stay on the
// first line when the value wraps rather than drift to the middle.
func TestADetailListsLabelSitsOnItsValuesFirstLine(t *testing.T) {
	dl := render(t, "detail-list", map[string]any{"Items": []any{
		map[string]any{"Label": "Audience", "Value": "Members"},
		map[string]any{"Label": "Published", "Value": "2 August 2026", "DateTime": "2026-08-02"},
		map[string]any{"Label": "Reference", "Value": "post_01H9ZQ", "Mono": true},
		map[string]any{"Label": "Summary", "Value": "Release notes for August: the new export, faster search, a fix for invoices that never arrived, and the rest of the month's small changes."},
	}})
	pages := map[string]string{"/": sizingDoc("detail-list", `<div rst-page><section rst-box id="box">`+dl+`</section></div>`)}
	for _, leg := range []struct {
		name   string
		w      int64
		coarse bool
	}{{"1280 mouse", 1280, false}, {"390 touch", 390, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#box", chromedp.ByQuery))
			requirePointer(t, ctx, leg.coarse)
			// A zero-size inline-block's bottom edge is the baseline of the
			// line it sits on; put first in the element, it marks the first.
			var rows []struct {
				Label                    string
				LabelLine, ValueLine     float64
				LabelSize, ValueSize, VH float64
			}
			at(t, ctx, `(() => { const line = el => { const m = document.createElement("span"); m.style.cssText = "display: inline-block; inline-size: 0; block-size: 0"; el.prepend(m); const y = m.getBoundingClientRect().bottom; m.remove(); return y; };
			  return JSON.stringify([...document.querySelectorAll("[rst-detail] dt")].map(dt => { const dd = dt.nextElementSibling;
			    return {Label: dt.textContent, LabelLine: line(dt), ValueLine: line(dd), LabelSize: parseFloat(getComputedStyle(dt).fontSize), ValueSize: parseFloat(getComputedStyle(dd).fontSize), VH: dd.getBoundingClientRect().height / parseFloat(getComputedStyle(dd).lineHeight)}; })); })()`, &rows)
			if len(rows) != 4 {
				t.Fatalf("measured %d rows, want 4", len(rows))
			}
			if rows[0].LabelSize >= rows[0].ValueSize {
				t.Fatalf("the label is %.1fpx and its value %.1fpx; with no difference in size the tops line up anyway and this leg proves nothing", rows[0].LabelSize, rows[0].ValueSize)
			}
			if rows[3].VH < 1.9 {
				t.Fatalf("the summary is %.1f lines tall; it must wrap for the multi-line leg to prove anything", rows[3].VH)
			}
			for _, r := range rows {
				if d := r.LabelLine - r.ValueLine; d < -0.5 || d > 0.5 {
					t.Errorf("%s: the label's baseline is at %.1f and its value's first is at %.1f, %.1fpx apart", r.Label, r.LabelLine, r.ValueLine, d)
				}
			}
		})
	}
}
