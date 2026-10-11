//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// buttonScaleJS reads the gallery's button sample and an sm link: each
// size's drawn height, and sm's reach, the run of points down its
// middle that a click lands on it from. Each point is hit-tested, so
// the reach is what a tap really gets, not what the ::before claims.
const buttonScaleJS = `(() => {
  const h = el => Math.round(el.getBoundingClientRect().height * 100) / 100;
  const sizes = {};
  for (const el of document.querySelectorAll('[rst-btn~="primary"]')) sizes[el.textContent.trim()] = h(el);
  const reach = el => {
    el.scrollIntoView({block: "center"});
    const r = el.getBoundingClientRect(), x = r.left + Math.min(r.width / 2, 8);
    let top = r.top, bottom = r.bottom;
    while (document.elementFromPoint(x, top - 0.25) === el) top -= 0.25;
    while (document.elementFromPoint(x, bottom + 0.25) === el) bottom += 0.25;
    return Math.round((bottom - top) * 100) / 100;
  };
  const sm = document.querySelector('[rst-btn="primary sm"]'), link = document.querySelector("#sm-link");
  return JSON.stringify({Sm: sizes.Small, Md: sizes.Default, Lg: sizes.Large, Reach: reach(sm), LinkH: h(link), LinkReach: reach(link),
    MdReach: reach(document.querySelector('[rst-btn="primary"]'))});
})()`

// TestTheButtonSizesAreAnEvenScale pins the three drawn sizes at 1280
// with a mouse and at 390 with touch, and sm's hit area at the floor on
// both. The floor lifted sm to the default's 44px on a phone (32 beside
// 34 on a desktop), so the three read as two sizes. sm is now drawn
// under the floor and reaches it with an invisible ::before, measured
// here by hit-testing: on a button and on a link, since a link is not
// positioned by the browser's own stylesheet and a button is not either,
// so a missing position: relative would show on both.
func TestTheButtonSizesAreAnEvenScale(t *testing.T) {
	body := `<div rst-page>` + Styleguide()["button"] + `<p><a rst-btn="sm" href="#" id="sm-link">Edit</a></p></div>`
	for _, leg := range []struct {
		name              string
		w, h              int64
		coarse            bool
		sm, md, lg, floor float64
	}{
		{"1280x900 mouse", 1280, 900, false, 26, 34, 42, 32},
		{"390x844 touch", 390, 844, true, 32, 44, 52, 44},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, map[string]string{"/": sizingDoc("buttons", body)})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			var raw string
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("#sm-link", chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			requirePointer(t, ctx, leg.coarse)
			if err := chromedp.Run(ctx, chromedp.Evaluate(buttonScaleJS, &raw)); err != nil {
				t.Fatal(err)
			}
			var got struct{ Sm, Md, Lg, Reach, LinkH, LinkReach, MdReach float64 }
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatal(err)
			}
			if math.Round(got.Sm) != leg.sm || math.Round(got.Md) != leg.md || math.Round(got.Lg) != leg.lg {
				t.Errorf("drawn heights %v/%v/%v, want %v/%v/%v", got.Sm, got.Md, got.Lg, leg.sm, leg.md, leg.lg)
			}
			if math.Round(got.LinkH) != leg.sm {
				t.Errorf("an sm link is drawn %vpx tall, want %v like the sm button", got.LinkH, leg.sm)
			}
			// Within the 0.25px step of the walk, plus rounding: the
			// reach is the floor, not the drawn box and not more.
			for _, r := range []struct {
				name string
				got  float64
			}{{"the sm button", got.Reach}, {"the sm link", got.LinkReach}} {
				if math.Abs(r.got-leg.floor) > 0.75 {
					t.Errorf("%s takes clicks over %vpx of its height, want the %vpx floor", r.name, r.got, leg.floor)
				}
			}
			// The control: the default button has no hit area of its own,
			// so the walk must stop at its border, or the walk measures
			// something other than the ::before.
			if math.Abs(got.MdReach-got.Md) > 0.75 {
				t.Errorf("CONTROL: the default button takes clicks over %vpx but is drawn %vpx; the walk is not reading the hit area", got.MdReach, got.Md)
			}
		})
	}
}

// buttonLabelsJS reads every button in the sample: its label's colour
// against the fill behind it (the button's own, or the first opaque one
// above it for a ghost), and the box the label's text takes. A label
// can go missing in six ways: painted in its fill's colour, collapsed to
// nothing, laid out outside the button where it is cut off or off
// screen, hidden, made transparent by the button or anything around
// it, or covered by another box (the point at the label's middle must
// reach the button). The text fill is read rather than color, because
// the busy state hides a label with a transparent fill and leaves color
// alone.
const buttonLabelsJS = `(() => {
  const rgba = s => { const m = s.match(/[\d.]+/g).map(Number); return m.length === 3 ? [...m, 1] : m; };
  const lum = c => { const f = v => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); }; return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]); };
  const fill = el => { for (let e = el; e; e = e.parentElement) { const c = rgba(getComputedStyle(e).backgroundColor); if (c[3] > 0.5) return c; } return rgba(getComputedStyle(document.documentElement).backgroundColor); };
  return JSON.stringify([...document.querySelectorAll("[rst-btn]")].map(el => {
    const cs = getComputedStyle(el), fg = rgba(cs.webkitTextFillColor || cs.color), bg = fill(el);
    const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x);
    el.scrollIntoView({block: "center"});
    const range = document.createRange(); range.selectNodeContents(el);
    const t = range.getBoundingClientRect(), r = el.getBoundingClientRect();
    let opacity = 1;
    for (let e = el; e; e = e.parentElement) opacity *= parseFloat(getComputedStyle(e).opacity);
    const at = document.elementFromPoint(t.left + t.width / 2, t.top + t.height / 2);
    return {Name: el.textContent.trim(), Disabled: el.disabled === true, Ratio: fg[3] < 0.5 ? 0 : Math.round((a + 0.05) / (b + 0.05) * 100) / 100,
      W: t.width, H: t.height, Inside: t.left >= r.left - 0.5 && t.right <= r.right + 0.5 && t.top >= r.top - 0.5 && t.bottom <= r.bottom + 0.5,
      OnScreen: t.left >= 0 && t.right <= innerWidth, Visibility: cs.visibility, Opacity: opacity,
      Reached: !!at && (at === el || el.contains(at))};
  }));
})()`

// TestEveryButtonLabelIsVisible holds every button in the gallery's
// sample to a label a reader can see, in every theme and both schemes,
// at 1280 with a mouse and 390 with touch: the text fill against the
// button's own fill at 4.5:1 (3:1 for the disabled one, which WCAG
// exempts but which must still read), and a text box with size, inside
// the button and on the screen. It was written for a report of the
// full-width primary button showing as a bar with no label in light
// mode, which no engine here reproduced (see the branch's report). Three
// negative controls, a label hidden, transparent and covered, must each
// fail the same checker, or the checker cannot see those ways at all.
func TestEveryButtonLabelIsVisible(t *testing.T) {
	pages := map[string]string{}
	for _, theme := range ThemeNames() {
		for _, scheme := range []string{"light", "dark"} {
			doc := strings.Replace(sizingDoc("labels", `<div rst-page>`+Styleguide()["button"]+`</div>`), `/theme.css`, `/theme-`+theme+`.css`, 1)
			pages["/"+theme+"-"+scheme] = strings.Replace(doc, `<html lang="en"`, `<html data-theme="`+scheme+`" lang="en"`, 1)
		}
	}
	// The negative controls: a label hidden, one under opacity 0, and one
	// covered by another box, each still the right colour and size.
	pages["/controls"] = sizingDoc("controls", `<div rst-page>`+
		`<p><button rst-btn="primary" type="button" style="visibility: hidden">Hidden</button></p>`+
		`<p style="opacity: 0"><button rst-btn="primary" type="button">Transparent</button></p>`+
		`<p style="position: relative; inline-size: fit-content"><button rst-btn="primary block" type="button">Covered</button><span style="background: #fff; inset: 0; position: absolute"></span></p></div>`)
	build := func(string) http.Handler {
		mux := sizingMux(t, pages)
		for _, theme := range ThemeNames() {
			css, _ := ThemeCSS(theme)
			mux.HandleFunc("GET /theme-"+theme+".css", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/css")
				w.Write(css)
			})
		}
		return mux
	}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280x900 mouse", 1280, 900, false}, {"390x844 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			var opts []harness.Option
			if leg.coarse {
				opts = append(opts, harness.WithCoarsePointer())
			}
			rig := harness.New(t, build, opts...)
			ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h)); err != nil {
				t.Fatal(err)
			}
			for path := range pages {
				var raw string
				if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("[rst-btn~=block]", chromedp.ByQuery), chromedp.Evaluate(buttonLabelsJS, &raw)); err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				var got []labelReading
				if err := json.Unmarshal([]byte(raw), &got); err != nil {
					t.Fatal(err)
				}
				if path == "/controls" {
					// Each control hides its label one way; a checker that
					// passes any of them could pass a hidden label anywhere.
					for _, b := range got {
						if b.problem() == "" {
							t.Errorf("CONTROL: %q's label is hidden, and the checker passed it", b.Name)
						}
					}
					continue
				}
				if len(got) != 10 {
					t.Fatalf("%s: read %d buttons, want the sample's 10", path, len(got))
				}
				for _, b := range got {
					if p := b.problem(); p != "" {
						t.Errorf("%s: %q's label %s", path, b.Name, p)
					}
				}
			}
		})
	}
}

// labelReading is one button's label as buttonLabelsJS reads it.
type labelReading struct {
	Name                                string
	Disabled, Inside, OnScreen, Reached bool
	Ratio, W, H, Opacity                float64
	Visibility                          string
}

// problem says what keeps the label from being seen, or "".
func (b labelReading) problem() string {
	min := 4.5
	if b.Disabled {
		min = 3
	}
	switch {
	case b.Ratio < min:
		return fmt.Sprintf("is %.2f:1 against its fill, want %v:1", b.Ratio, min)
	case b.W < 1 || b.H < 1:
		return fmt.Sprintf("is %.1f×%.1f; it has collapsed", b.W, b.H)
	case !b.Inside || !b.OnScreen:
		return "is outside its button or off the screen"
	case b.Visibility != "visible":
		return "is visibility: " + b.Visibility
	case b.Opacity < 0.5:
		return fmt.Sprintf("is painted at opacity %.2f", b.Opacity)
	case !b.Reached:
		return "is covered: the point at its middle does not reach the button"
	}
	return ""
}

// TestSmallButtonsKeepTheirOwnEdges: sm's hit area reaches the floor
// without taking a tap from anything beside it. Two sm buttons with no
// gap between them, stacked in a column and wrapped in a line, are each
// hit at their own drawn edges; the space the hit area needs is
// reserved in layout instead. And sm composes with a tooltip, which is
// drawn by its own pseudo-element: the hit area still reaches the floor
// and the tip still shows its words. At 1280 with a mouse and 390 with
// touch.
func TestSmallButtonsKeepTheirOwnEdges(t *testing.T) {
	body := `<div rst-page><div id="col" style="display: flex; flex-direction: column; align-items: flex-start; gap: 0">` +
		`<button rst-btn="sm" type="button" id="a">Archive</button><button rst-btn="sm" type="button" id="b">Delete</button></div>` +
		`<p id="row" style="inline-size: 5rem"><button rst-btn="sm" type="button" id="c">Archive</button><button rst-btn="sm" type="button" id="d">Delete</button></p>` +
		`<p><button rst-btn="sm" type="button" id="tip" rst-tip data-tip="Copy the link">Copy</button></p></div>`
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
		floor  float64
	}{{"1280x900 mouse", 1280, 900, false, 32}, {"390x844 touch", 390, 844, true, 44}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, map[string]string{"/": sizingDoc("small", body)})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("#tip", chromedp.ByQuery))
			requirePointer(t, ctx, leg.coarse)
			var g struct {
				Edges      []string
				Stacked    bool
				TipReach   float64
				TipContent string
			}
			at(t, ctx, `(() => {
			  const hit = (el, y) => { const r = el.getBoundingClientRect(), h = document.elementFromPoint(r.left + r.width / 2, y); return h && h.closest("button") ? h.closest("button").id : "nothing"; };
			  const edges = [];
			  for (const [x, y] of [["a", "b"], ["c", "d"]]) {
			    const ex = document.getElementById(x), ey = document.getElementById(y);
			    edges.push(x + ":" + hit(ex, ex.getBoundingClientRect().bottom - 0.5), y + ":" + hit(ey, ey.getBoundingClientRect().top + 0.5));
			  }
			  const tip = document.getElementById("tip"), r = tip.getBoundingClientRect(), x = r.left + 8;
			  let top = r.top, bottom = r.bottom;
			  while (document.elementFromPoint(x, top - 0.25) === tip) top -= 0.25;
			  while (document.elementFromPoint(x, bottom + 0.25) === tip) bottom += 0.25;
			  return JSON.stringify({Edges: edges, Stacked: document.getElementById("d").getBoundingClientRect().top >= document.getElementById("c").getBoundingClientRect().bottom - 0.5,
			    TipReach: bottom - top, TipContent: getComputedStyle(tip, "::after").content});
			})()`, &g)
			if !g.Stacked {
				t.Fatalf("the two sm buttons in the narrow line did not wrap onto two lines; the line case proves nothing")
			}
			for _, e := range g.Edges {
				if id, got, _ := strings.Cut(e, ":"); id != got {
					t.Errorf("a click at the drawn edge of #%s lands on %s", id, got)
				}
			}
			if math.Abs(g.TipReach-leg.floor) > 0.75 {
				t.Errorf("an sm button with a tooltip takes clicks over %.2fpx of its height, want the %vpx floor", g.TipReach, leg.floor)
			}
			if g.TipContent != `"Copy the link"` {
				t.Errorf("the sm button's tooltip reads %s, want \"Copy the link\"", g.TipContent)
			}
		})
	}
}
