//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// realSize reads every Mobile frame's transform, its laid-out and
// rendered widths, a text field's computed and rendered font size, and
// the box's inline edges against the document's.
//
// The field is the first plain one. The primary variant is the form's
// headline field and is sized on purpose at --rst-fs-lg, 19px on a
// phone; tokens.css orders its floor so that size survives. Held to
// 16px it failed a field that was right, and a floor written to drag
// it down would be the bug tokens.css already fixed once.
const realSize = `JSON.stringify([...document.querySelectorAll(".ds-view")].filter(v => getComputedStyle(v.querySelector(".ds-view__stage")).display !== "none").map(v => {
  const box = v.querySelector(".ds-view__box"), f = v.querySelector(".ds-view__frame"), b = box.getBoundingClientRect(), fr = f.getBoundingClientRect();
  const lit = [...v.querySelectorAll(".ds-view__tab")].findIndex(l => getComputedStyle(l).fontWeight === "600");
  let field = null;
  try { const i = f.contentDocument.querySelector(':is(input:not([type]), input[type=text], input[type=email], input[type=url]):not(.rst-input--primary, [rst-input~="primary"])'); if (i) field = parseFloat(f.contentDocument.defaultView.getComputedStyle(i).fontSize); } catch (e) {}
  return {ID: (v.closest("article, section") || {}).id || "", Lit: lit, Transform: getComputedStyle(f).transform,
    Layout: f.offsetWidth, Rendered: fr.width, Field: field, BoxL: b.left, BoxR: b.right, DocW: document.documentElement.clientWidth};
}))`

type realReading struct {
	ID                                 string
	Lit                                int
	Transform                          string
	Layout, Rendered, BoxL, BoxR, DocW float64
	Field                              *float64
}

func readReal(t *testing.T, ctx context.Context, where string) []realReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(realSize, &raw)); err != nil {
		t.Fatalf("%s: reading the frames: %v", where, err)
	}
	var rs []realReading
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		t.Fatalf("%s: decoding: %v", where, err)
	}
	if len(rs) == 0 {
		t.Fatalf("%s: no frame showing", where)
	}
	return rs
}

// On a phone, Mobile is the page at its real size: the frame is laid
// out at the stage's width up to 390px and never scaled, so a 16px
// field is 16px, and the box reaches the screen's edges. Auto opens on
// Mobile there, as it always has; the Mobile tab lit is the first
// thing asserted, or the rest measures some other rendering.
func TestMobileIsThePageAtItsRealSize(t *testing.T) {
	// Both rigs draw their scrollbars, because the box is held to the
	// document's width: see requireDrawnScrollbar.
	coarse := harness.New(t, func(string) http.Handler { return treeHandler(t) }, harness.WithCoarsePointer(), harness.WithScrollbars())
	cctx, ccancel := context.WithTimeout(coarse.Context(), 240*time.Second)
	defer ccancel()
	fine := harness.New(t, func(string) http.Handler { return treeHandler(t) }, harness.WithScrollbars())
	fctx, fcancel := context.WithTimeout(fine.Context(), 240*time.Second)
	defer fcancel()
	for _, rig := range []struct {
		name   string
		ctx    context.Context
		origin string
		widths []int64
	}{{"coarse", cctx, coarse.Origin, []int64{390, 320}}, {"fine", fctx, fine.Origin, []int64{390}}} {
		for _, kind := range []string{"form", "shells"} {
			for _, locale := range []string{"en", "ar"} {
				for _, w := range rig.widths {
					where := fmt.Sprintf("day/%s %s at %dpx, %s pointer", locale, kind, w, rig.name)
					if err := chromedp.Run(rig.ctx, chromedp.EmulateViewport(w, 844),
						chromedp.Navigate(rig.origin+pageHref(mountPath, "day", locale, fileOf(kind))),
						chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery)); err != nil {
						t.Fatalf("%s: loading: %v", where, err)
					}
					if rig.name == "coarse" {
						requireCoarse(t, rig.ctx)
					}
					eagerly(t, rig.ctx, where)
					requireDrawnScrollbar(t, rig.ctx, where)
					var sideways float64
					if err := chromedp.Run(rig.ctx, chromedp.Evaluate(`document.documentElement.scrollWidth - document.documentElement.clientWidth`, &sideways)); err != nil {
						t.Fatalf("%s: %v", where, err)
					}
					if sideways > 0.5 {
						t.Errorf("%s: the page scrolls %.1fpx sideways", where, sideways)
					}
					fields := 0
					for _, r := range readReal(t, rig.ctx, where) {
						if r.Lit != 1 {
							t.Fatalf("%s: %s opens with tab %d lit; Auto on a phone opens on Mobile, and the readings below assume it", where, r.ID, r.Lit)
						}
						if r.Transform != "matrix(1, 0, 0, 1, 0, 0)" && r.Transform != "none" {
							t.Errorf("%s: %s is transformed %s; Mobile is never scaled", where, r.ID, r.Transform)
						}
						if math.Abs(r.Layout-r.Rendered) > 0.5 || r.Layout > 390 {
							t.Errorf("%s: %s lays out at %.1fpx and renders at %.1fpx; want one width, at most 390px", where, r.ID, r.Layout, r.Rendered)
						}
						if math.Abs(r.BoxL) > 1 || math.Abs(r.BoxR-r.DocW) > 1 {
							t.Errorf("%s: %s's box runs %.1f…%.1f in a %.0fpx document; on a phone the stage is the screen's width", where, r.ID, r.BoxL, r.BoxR, r.DocW)
						}
						if r.Field != nil {
							fields++
							if rendered := *r.Field * r.Rendered / r.Layout; math.Abs(*r.Field-16) > 0.1 || math.Abs(rendered-16) > 0.1 {
								t.Errorf("%s: %s's text field computes %.2fpx and renders %.2fpx; a phone's field is 16px", where, r.ID, *r.Field, rendered)
							}
						}
					}
					if kind == "form" && fields == 0 {
						t.Errorf("%s: no text field measured on Form; the 16px claim was checked against nothing", where)
					}
				}
			}
		}
	}
	// The control: on a desktop the Mobile rendering is the 390px phone
	// it has always been, centred in its stage.
	where := "day/en form at 1280px, Mobile chosen"
	if err := chromedp.Run(fctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(fine.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	clickAll(t, fctx, where, clickedMobile, "Mobile")
	// A short page would panic on the slice, and a panic ends the whole
	// package run rather than this test.
	const want = 3
	shown := readReal(t, fctx, where)
	if len(shown) < want {
		t.Fatalf("%s: %d frames showing, want at least %d to check", where, len(shown), want)
	}
	for _, r := range shown[:want] {
		if r.Layout != 390 {
			t.Errorf("%s: %s lays out at %.1fpx, want 390", where, r.ID, r.Layout)
		}
	}
}

// Desktop on a phone pans, and it pans from the page's start in either
// direction: in Arabic the frame is anchored at the box's right edge
// and the box scrolls left. Anchored at the physical left, a
// right-to-left box could not scroll at all, and the start of an
// Arabic page, its right-hand 339px, was cropped.
func TestDesktopPansFromTheInlineStart(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()
	for _, c := range []struct {
		kind  string
		width float64
	}{{"form", 900}, {"shells", 1200}} {
		for _, locale := range []string{"ar", "en"} {
			where := fmt.Sprintf("day/%s %s at 390px, Desktop chosen", locale, c.kind)
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
				chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", locale, fileOf(c.kind))),
				chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			eagerly(t, ctx, where)
			// Each widget's own Desktop radio: it is the only control
			// that chooses a widget's view.
			clickAll(t, ctx, where, clickedDesktop, "Desktop")
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			  const rtl = document.documentElement.dir === "rtl", out = [];
			  for (const v of document.querySelectorAll(".ds-view")) {
			    const box = v.querySelector(".ds-view__box"), f = v.querySelector(".ds-view__frame");
			    const checked = v.querySelector(".ds-view__tab--d input").checked;
			    const startEdge = () => { const b = box.getBoundingClientRect(), r = f.getBoundingClientRect(); return rtl ? r.right - b.right : r.left - b.left; };
			    const endEdge = () => { const b = box.getBoundingClientRect(), r = f.getBoundingClientRect(); return rtl ? b.left - r.left : r.right - b.right; };
			    const atStart = startEdge();
			    box.scrollLeft = rtl ? -(box.scrollWidth - box.clientWidth) : box.scrollWidth - box.clientWidth;
			    const atEnd = endEdge();
			    box.scrollLeft = 0;
			    out.push({ID: (v.closest("article, section") || {}).id || "", Checked: checked, Layout: f.offsetWidth,
			      Pans: box.scrollWidth > box.clientWidth, Start: atStart, End: atEnd});
			  }
			  return JSON.stringify(out);
			})()`, &raw)); err != nil {
				t.Fatalf("%s: measuring: %v", where, err)
			}
			var rs []struct {
				ID         string
				Checked    bool
				Layout     float64
				Pans       bool
				Start, End float64
			}
			if err := json.Unmarshal([]byte(raw), &rs); err != nil {
				t.Fatalf("%s: decoding: %v", where, err)
			}
			// The control: every widget really is on Desktop at its
			// class's width. A widget left on Auto or Mobile would never
			// overflow, and everything after this would pass on nothing.
			for _, r := range rs {
				if !r.Checked || r.Layout != c.width {
					t.Fatalf("%s: %s has Desktop checked %v and lays out at %.0fpx, want %.0fpx", where, r.ID, r.Checked, r.Layout, c.width)
				}
			}
			for _, r := range rs {
				if !r.Pans {
					t.Errorf("%s: %s's box does not scroll", where, r.ID)
				}
				if math.Abs(r.Start) > 1 {
					t.Errorf("%s: at rest %s's frame starts %.1fpx from its box's inline start", where, r.ID, r.Start)
				}
				if math.Abs(r.End) > 1 {
					t.Errorf("%s: scrolled to the end, %s's frame ends %.1fpx from its box's inline end", where, r.ID, r.End)
				}
			}
		}
	}
}
