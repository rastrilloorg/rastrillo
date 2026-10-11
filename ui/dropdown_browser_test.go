//go:build browser

package ui

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// withoutAnchorSupport is what anchor.js's feature test sees in a
// browser without anchor positioning: CSS.supports says no to it, run
// before any script on the page.
const withoutAnchorSupport = `(() => { const s = CSS.supports.bind(CSS); CSS.supports = (...a) => !/position-area|position-try/.test(a.join(" ")) && s(...a); })()`

// TestADropdownOpensUnderItsOwnSummary: the panel hangs from the
// summary, wherever the <details> is put. It hung from the <details>,
// which in a block or a stretching flex column is as wide as its
// container, so at 1280 the gallery's Filter opened its panel at the
// far trailing side of the frame, nowhere near the word just pressed.
// The same bug as the row menu's, without the row menu's way out: a
// dropdown stays where the page puts it, at the leading edge of a block
// as often as at the trailing end of a bar.
//
// Both partial spellings are read (the dropdown partial's summary is a
// button, the gallery sample's a bare summary), each in a block, in a
// stretching flex column, and pushed to the trailing edge. At the
// leading edge there is no room for the panel to open toward the
// leading side, so it opens from the summary's leading edge; at the
// trailing edge it opens from the summary's trailing edge. With and
// without anchor positioning, at 1280 with a mouse and 390 with touch,
// in both directions. An engine without it is flipped by anchor.js: the
// no-anchor legs take the feature from the script's own test as well as
// from the stylesheet. The last leg takes it from the stylesheet alone,
// so the panel is placed the no-anchor way while the browser still
// reports the feature, and anchor.js must do nothing: the dropdown keeps
// its container's width and the panel its far edge, the geometry of a
// browser with neither (TestADropdownWithoutAnchorsOrScriptsStaysOnScreen).
func TestADropdownOpensUnderItsOwnSummary(t *testing.T) {
	partial := render(t, "dropdown", map[string]any{"Label": "Newest", "Aria": "Sort: Newest", "Items": []any{
		map[string]any{"Href": "/o?sort=new", "Label": "Newest", "Current": true},
		map[string]any{"Href": "/o?sort=old", "Label": "Oldest"},
	}})
	sample := Styleguide()["dropdown"]
	box := func(style, menu string) string {
		return `<style>#box { padding: 1rem; } ` + style + `</style><div id="box">` + menu + `</div>`
	}
	pages := map[string]string{}
	for name, menu := range map[string]string{"partial": partial, "sample": sample} {
		pages["/"+name+"/block"] = sizingDoc("a dropdown in a block", box("", menu))
		pages["/"+name+"/stretch"] = sizingDoc("a dropdown in a stretching column", box("#box { display: flex; flex-direction: column; }", menu))
		pages["/"+name+"/end"] = sizingDoc("a dropdown at the trailing edge", box("#box > [rst-dropdown] { margin-inline-start: auto; }", menu))
	}
	for path, html := range pages {
		pages["/rtl"+path] = strings.Replace(html, `dir="ltr"`, `dir="rtl"`, 1)
	}
	type leg struct {
		name   string
		w      int64
		coarse bool
		anchor bool
	}
	for _, l := range []leg{
		{"1280 mouse", 1280, false, true},
		{"390 touch", 390, true, true},
		{"1280 mouse without anchor positioning", 1280, false, false},
		{"390 touch without anchor positioning", 390, true, false},
		{"1280 mouse, anchor positioning reported but not applied", 1280, false, false},
	} {
		t.Run(l.name, func(t *testing.T) {
			rig := sizingRig(t, l.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
			defer cancel()
			reported := strings.Contains(l.name, "reported")
			if !l.anchor && !reported {
				mustRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := cdppage.AddScriptToEvaluateOnNewDocument(withoutAnchorSupport).Do(ctx)
					return err
				}))
			}
			for _, dir := range []string{"", "/rtl"} {
				for _, spelling := range []string{"partial", "sample"} {
					for _, where := range []string{"block", "stretch", "end"} {
						path := dir + "/" + spelling + "/" + where
						mustRun(t, ctx, chromedp.EmulateViewport(l.w, 800), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible("#box", chromedp.ByQuery))
						requirePointer(t, ctx, l.coarse)
						if !l.anchor {
							mustRun(t, ctx, chromedp.Evaluate(galleryrig.WithoutAnchorPositioning, nil, awaitPromise))
						}
						galleryrig.RequireAnchorPositioning(t, ctx, path, "#box > [rst-dropdown] > [rst-dropdown-menu]", l.anchor)
						// Edges are distances from the root box's leading edge,
						// as in the row menu's test: the scrollbar gutter is on
						// the left in RTL.
						var g struct {
							Open                                           bool
							SummaryStart, SummaryEnd, PanelStart, PanelEnd float64
							PanelTop, SummaryBottom, VW                    float64
						}
						at(t, ctx, `(async () => { const d = document.querySelector("#box > [rst-dropdown]"); d.querySelector("summary").click();
						  await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
						  const s = d.querySelector("summary").getBoundingClientRect(), p = d.querySelector(":scope > [rst-dropdown-menu]").getBoundingClientRect();
						  const rtl = getComputedStyle(d).direction === "rtl", h = document.documentElement.getBoundingClientRect();
						  const start = r => rtl ? h.right - r.right : r.left - h.left, end = r => rtl ? h.right - r.left : r.right - h.left;
						  return JSON.stringify({Open: d.open, SummaryStart: start(s), SummaryEnd: end(s), PanelStart: start(p), PanelEnd: end(p), PanelTop: p.top, SummaryBottom: s.bottom, VW: h.width}); })()`, &g, awaitPromise)
						if !g.Open {
							t.Fatalf("%s: the menu did not open; this leg proves nothing", path)
						}
						// The control: the summary is where the page put it,
						// or the edge the panel shares proves nothing.
						if where == "end" && g.SummaryEnd < g.VW-40 || where != "end" && g.SummaryStart > 40 {
							t.Fatalf("%s: the summary spans %.0f to %.0f of %.0f; it is not at the edge this page puts it", path, g.SummaryStart, g.SummaryEnd, g.VW)
						}
						if reported {
							if where != "end" && (g.SummaryEnd-g.SummaryStart < g.VW/2 || g.PanelStart < g.VW/2) {
								t.Errorf("%s: the summary spans %.0f to %.0f and the panel %.0f to %.0f; with anchor positioning reported, anchor.js must do nothing, leaving the dropdown its container's width and the panel at its far edge", path, g.SummaryStart, g.SummaryEnd, g.PanelStart, g.PanelEnd)
							}
							continue
						}
						if where == "end" {
							if d := g.PanelEnd - g.SummaryEnd; d < -0.5 || d > 0.5 {
								t.Errorf("%s: the panel's trailing edge is at %.0f, the summary's at %.0f; it must open from the summary", path, g.PanelEnd, g.SummaryEnd)
							}
						} else if d := g.PanelStart - g.SummaryStart; d < -0.5 || d > 0.5 {
							t.Errorf("%s: the panel spans %.0f to %.0f, the summary %.0f to %.0f; with no room before the summary it must open from its leading edge", path, g.PanelStart, g.PanelEnd, g.SummaryStart, g.SummaryEnd)
						}
						if g.PanelStart < -0.5 || g.PanelEnd > g.VW+0.5 {
							t.Errorf("%s: the panel spans %.0f to %.0f of a %.0fpx viewport; it must stay on screen", path, g.PanelStart, g.PanelEnd, g.VW)
						}
						if g.PanelTop < g.SummaryBottom || g.PanelTop > g.SummaryBottom+8 {
							t.Errorf("%s: the panel starts at %.0f, the summary's foot is at %.0f; it must drop just below the summary", path, g.PanelTop, g.SummaryBottom)
						}
					}
				}
			}
		})
	}
}

// TestADropdownWithoutAnchorsOrScriptsStaysOnScreen: in a browser with
// neither anchor positioning nor scripts, nothing can flip a panel, so
// a dropdown keeps the geometry it had before it learned to hug its
// summary. A <details> as wide as its block puts the panel at the
// block's trailing edge, far from a summary at the leading edge but on
// the screen, where a hugging one would hang it off the leading edge.
// The page links no script at all and a stylesheet whose anchor block
// can never apply. Both spellings, both directions, 1280 and 390.
func TestADropdownWithoutAnchorsOrScriptsStaysOnScreen(t *testing.T) {
	partial := render(t, "dropdown", map[string]any{"Label": "Newest", "Aria": "Sort: Newest", "Items": []any{
		map[string]any{"Href": "/o?sort=new", "Label": "Newest", "Current": true},
		map[string]any{"Href": "/o?sort=old", "Label": "Oldest"},
	}})
	bare := func(dir, menu string) string {
		return `<!doctype html><html lang="en" dir="` + dir + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>no scripts</title>` +
			`<link rel="stylesheet" href="/tokens-noanchor.css"><link rel="stylesheet" href="/theme.css"></head><body><div id="box" style="padding: 1rem">` + menu + `</div></body></html>`
	}
	pages := map[string]string{}
	for name, menu := range map[string]string{"partial": partial, "sample": Styleguide()["dropdown"]} {
		pages["/"+name] = bare("ltr", menu)
		pages["/rtl/"+name] = bare("rtl", menu)
	}
	noAnchor := strings.ReplaceAll(string(TokensCSS()), "@supports (position-area: block-end) and (position-try-fallbacks: flip-block)", "@supports (position-area: rastrillo-no-such-value)")
	for _, leg := range []struct {
		name   string
		w      int64
		coarse bool
	}{{"1280 mouse", 1280, false}, {"390 touch", 390, true}} {
		t.Run(leg.name, func(t *testing.T) {
			var opts []harness.Option
			if leg.coarse {
				opts = append(opts, harness.WithCoarsePointer())
			}
			rig := harness.New(t, func(string) http.Handler {
				mux := sizingMux(t, pages)
				mux.HandleFunc("GET /tokens-noanchor.css", func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/css")
					fmt.Fprint(w, noAnchor)
				})
				return mux
			}, opts...)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			for path := range pages {
				mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 800), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible("#box", chromedp.ByQuery),
					chromedp.Click("#box > [rst-dropdown] > summary", chromedp.ByQuery))
				galleryrig.RequireAnchorPositioning(t, ctx, path, "#box > [rst-dropdown] > [rst-dropdown-menu]", false)
				var g struct {
					Open                       bool
					Left, Right, VW, Top, Foot float64
				}
				at(t, ctx, `(() => { const d = document.querySelector("#box > [rst-dropdown]"), p = d.querySelector(":scope > [rst-dropdown-menu]").getBoundingClientRect(), s = d.querySelector("summary").getBoundingClientRect(), h = document.documentElement.getBoundingClientRect();
				  return JSON.stringify({Open: d.open, Left: p.left - h.left, Right: p.right - h.left, VW: h.width, Top: p.top, Foot: s.bottom}); })()`, &g)
				if !g.Open {
					t.Fatalf("%s: the menu did not open; this leg proves nothing", path)
				}
				if g.Left < -0.5 || g.Right > g.VW+0.5 {
					t.Errorf("%s: with neither anchor positioning nor scripts the panel spans %.0f to %.0f of a %.0fpx page; it must stay on the screen", path, g.Left, g.Right, g.VW)
				}
				if g.Top < g.Foot || g.Top > g.Foot+8 {
					t.Errorf("%s: the panel starts at %.0f, the summary's foot is at %.0f; it must drop just below", path, g.Top, g.Foot)
				}
			}
		})
	}
}

// TestAnOpenDropdownIsPlacedAgainWhenTheWindowChanges: without anchor
// positioning, anchor.js places a menu as it opens, and a window that
// is resized or rotated with the menu open can move the summary under
// it. Here a narrow window puts the dropdown at the trailing edge: the
// menu flipped to open from the summary's leading edge at 1280 would
// run off the trailing edge at 500, so it is placed again on resize.
// It stays on screen and under its summary, in both directions.
func TestAnOpenDropdownIsPlacedAgainWhenTheWindowChanges(t *testing.T) {
	menu := render(t, "dropdown", map[string]any{"Label": "Newest", "Aria": "Sort: Newest", "Items": []any{
		map[string]any{"Href": "/o?sort=new", "Label": "Newest", "Current": true},
		map[string]any{"Href": "/o?sort=old", "Label": "Oldest"},
	}})
	doc := sizingDoc("resize", `<style>#box { display: flex; padding: 1rem; } @media (max-width: 600px) { #box > [rst-dropdown] { margin-inline-start: auto; } }</style><div id="box">`+menu+`</div>`)
	pages := map[string]string{"/": doc, "/rtl": strings.Replace(doc, `dir="ltr"`, `dir="rtl"`, 1)}
	rig := sizingRig(t, false, pages)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(withoutAnchorSupport).Do(ctx)
		return err
	}))
	for _, path := range []string{"/", "/rtl"} {
		mustRun(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible("#box", chromedp.ByQuery),
			chromedp.Evaluate(galleryrig.WithoutAnchorPositioning, nil, awaitPromise))
		galleryrig.RequireAnchorPositioning(t, ctx, path, "#box > [rst-dropdown] > [rst-dropdown-menu]", false)
		frames := chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))`, nil, awaitPromise)
		// The control: opened at 1280 the menu is flipped, from the
		// summary's leading edge, or the resize has nothing to undo.
		var flipped bool
		mustRun(t, ctx, chromedp.Click("#box > [rst-dropdown] > summary", chromedp.ByQuery), frames,
			chromedp.Evaluate(`document.querySelector("#box > [rst-dropdown]").classList.contains("is-flip-inline")`, &flipped))
		if !flipped {
			t.Fatalf("%s: opened at the leading edge at 1280, the menu was not flipped; this leg proves nothing", path)
		}
		mustRun(t, ctx, chromedp.EmulateViewport(500, 800), frames)
		var g struct {
			Open                                   bool
			PanelL, PanelR, SummaryL, SummaryR, VW float64
		}
		at(t, ctx, `(() => { const d = document.querySelector("#box > [rst-dropdown]"), p = d.querySelector(":scope > [rst-dropdown-menu]").getBoundingClientRect(), s = d.querySelector("summary").getBoundingClientRect(), h = document.documentElement.getBoundingClientRect();
		  return JSON.stringify({Open: d.open, PanelL: p.left - h.left, PanelR: p.right - h.left, SummaryL: s.left - h.left, SummaryR: s.right - h.left, VW: h.width}); })()`, &g)
		if !g.Open {
			t.Fatalf("%s: the menu closed; this leg proves nothing", path)
		}
		if g.PanelL < -0.5 || g.PanelR > g.VW+0.5 {
			t.Errorf("%s: resized to 500 with the menu open, the panel spans %.0f to %.0f of %.0f; it must be placed again, on the screen", path, g.PanelL, g.PanelR, g.VW)
		}
		if math.Abs(g.PanelL-g.SummaryL) > 0.5 && math.Abs(g.PanelR-g.SummaryR) > 0.5 {
			t.Errorf("%s: the panel spans %.0f to %.0f, the summary %.0f to %.0f; it must open from one of the summary's edges", path, g.PanelL, g.PanelR, g.SummaryL, g.SummaryR)
		}
	}
}
