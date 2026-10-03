//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// kebabA is a row menu written by hand in the native details/summary
// idiom: five items, so its open panel reaches down over the row below
// it.
const kebabA = `<details rst-row-menu name="rst-menus" id="menu-a"><summary id="kebab-a" aria-label="Actions for Grace Hopper">⋮</summary>` +
	`<div rst-row-menu-panel id="panel-a"><a href="/go/a-view">View</a><a href="/go/a-edit">Edit</a><a href="/go/a-copy">Duplicate</a><a href="/go/a-move">Move</a><hr><a class="rst-danger" href="/go/a-delete">Delete…</a></div></details>`

// rowsFixture is every row idiom of spec §2.2 that has a primary link
// (the list grid's status-pill row among them, §10.2), a row with none
// in each of the two idioms, and a row holding the three controls that
// position themselves (a switch, a date field, an enhanced select) with
// the same three outside any row to compare against. The three sit in
// one stacked cell of the same width in and out of the row: below 800px
// the list grid becomes three columns whatever --rst-cols says
// (tokens.css's narrow list-grid rule), and a fourth child would land a
// date field in the 44px kebab column under its own picker. The
// lifting rule reaches controls at any depth in a row, so a wrapper cell
// is the honest test of it.
func rowsFixture(t *testing.T) string {
	t.Helper()
	var opts []any
	for i := 1; i <= 12; i++ {
		opts = append(opts, map[string]any{"Value": fmt.Sprint(i), "Label": fmt.Sprintf("Option %d", i)})
	}
	sw := func(id string) string {
		return `<label rst-switch id="` + id + `"><input type="checkbox" name="` + id + `"><span rst-switch-track aria-hidden="true"></span> On</label>`
	}
	return `<div rst-page>
<a id="before" href="#before">Before the lists</a>
<div rst-list id="lra">` + render(t, "list-row-action", map[string]any{
		"Href": "/go/lra", "Main": "Release notes, August", "Sub": "Published 2 August",
		"StatusTone": "positive", "StatusLabel": "Published",
		"ActionHref": "/go/lra-edit", "ActionLabel": "Edit", "ActionAria": "Edit Release notes, August",
	}) + `</div>
<div rst-list id="lra-inert"><div rst-row id="lra-n"><span rst-row-main><span>Archived notes</span><small rst-row-sub>No page of its own</small></span></div></div>
<div rst-card id="statuses" style="--rst-cols: minmax(0, 1fr) auto"><div rst-lrow id="row-s"><a class="rst-nm" id="link-s" href="/go/row-s">Invoice 4471 never arrived<small>Fiona Reid</small></a>` +
		render(t, "status-pill", map[string]any{"Tone": "warning", "Label": "Waiting"}) + `</div></div>
<div rst-card id="grid" style="--rst-cols: auto minmax(0, 1fr) 110px var(--rst-col-menu)">
<div rst-lrow id="row-a"><label rst-selbox id="label-a"><input type="checkbox" id="check-a" aria-label="Select Grace Hopper"></label><a class="rst-nm" id="link-a" href="/go/row-a">Grace Hopper<small>AB3PX</small></a><span class="rst-m-hide rst-cell-mut" id="cell-a">Paid</span>` + kebabA + `</div>
<div rst-lrow id="row-b"><label rst-selbox id="label-b"><input type="checkbox" id="check-b" aria-label="Select Alan Turing"></label><a class="rst-nm" id="link-b" href="/go/row-b">Alan Turing<small>CD4QY</small></a><span class="rst-m-hide rst-cell-mut">Due</span><details rst-row-menu name="rst-menus" id="menu-b"><summary id="kebab-b" aria-label="Actions for Alan Turing">⋮</summary><div rst-row-menu-panel><a href="/go/b-view">View</a></div></details></div>
</div>
<div rst-card id="people" style="--rst-cols: minmax(0, 1fr) 110px"><div rst-lrow id="row-p"><a rst-person href="/go/person" id="link-p"><span rst-person-av aria-hidden="true">A</span><span rst-person-meta><span rst-person-name>Ada Lovelace</span><span rst-person-email>ada@example.com</span></span></a><span class="rst-cell-mut">Owner</span></div></div>
<div rst-card id="inert" style="--rst-cols: minmax(0, 1fr) 110px"><div rst-lrow id="row-n"><span rst-person><span rst-person-av aria-hidden="true">B</span><span rst-person-meta><span rst-person-name>Barbara Liskov</span></span></span><span class="rst-cell-mut">Viewer</span></div></div>
<div rst-card id="controls" style="--rst-cols: minmax(0, 1fr) minmax(0, 2fr)"><div rst-lrow id="row-c"><a class="rst-nm" href="/go/row-c" id="link-c">Settings</a>` +
		`<div class="ctl-stack" id="ctl-in">` + sw("switch-in") + `<div id="date-in">` + render(t, "field-date", map[string]any{"Name": "due_in", "Label": "Due", "Value": "2026-08-28"}) + `</div>` +
		`<div id="combo-in">` + render(t, "field-select", map[string]any{"ID": "combo_in", "Name": "combo_in", "Label": "Country", "Options": opts}) + `</div></div></div></div>
<section rst-box id="outside"><div class="ctl-stack" id="ctl-out">` +
		sw("switch-out") + `<div id="date-out">` + render(t, "field-date", map[string]any{"Name": "due_out", "Label": "Due", "Value": "2026-08-28"}) + `</div>` +
		`<div id="combo-out">` + render(t, "field-select", map[string]any{"ID": "combo_out", "Name": "combo_out", "Label": "Country", "Options": opts}) + `</div></div></section>
<style>.ctl-stack { display: flex; flex-direction: column; gap: 0.5rem; min-inline-size: 0; inline-size: 100%; max-inline-size: 22rem; }</style>
</div>`
}

// rowsPages serves the fixture at / and a landing page for every /go/
// href, so a click that navigates is a URL change the drive can read.
func rowsPages(t *testing.T) map[string]string {
	return map[string]string{
		"/":    sizingDoc("rows", rowsFixture(t)),
		"/go/": sizingDoc("landed", `<p id="landed">landed</p>`),
	}
}

// at evaluates a JS expression returning JSON into v.
func at(t *testing.T, ctx context.Context, js string, v any) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("evaluating: %v\n%s", err, js)
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
}

// point is a place on screen and what elementFromPoint finds there.
type point struct {
	X, Y float64
	Hit  string
}

// probe returns the point at (x, y) in the box of sel, where fx/fy are
// fractions of the width and height and dx/dy pixel offsets from that.
func probe(t *testing.T, ctx context.Context, sel string, fx, fy, dx, dy float64) point {
	t.Helper()
	var p point
	at(t, ctx, fmt.Sprintf(`(() => { const el = document.querySelector(%q); el.scrollIntoView({block: "center", inline: "center"}); const r = el.getBoundingClientRect();
	  const x = r.left + r.width * %v + %v, y = r.top + r.height * %v + %v; const h = document.elementFromPoint(x, y);
	  return JSON.stringify({X: x, Y: y, Hit: h ? (h.id || h.tagName) : "nothing"}); })()`, sel, fx, dx, fy, dy), &p)
	return p
}

// clickAndLand clicks the page at p and waits for the landing page at
// want: the URL is want AND the landing page's own marker, #landed, is
// in the document. No page under test carries that marker, so a reading
// taken in the departing document can never pass for the destination,
// which reading location.pathname alone could.
func clickAndLand(t *testing.T, ctx context.Context, p point, want string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(p.X, p.Y)); err != nil {
		t.Fatal(err)
	}
	var last string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname + (document.getElementById("landed") ? " (landed)" : "")`, &last)); err == nil && last == want+" (landed)" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("a click at (%.0f, %.0f), over %s, did not land on %s; the page is at %q", p.X, p.Y, p.Hit, want, last)
}

// clickAndStay clicks the page at p, waits for done (a page expression
// naming what the click should have done: a menu open, a box checked)
// and fails if the click asked for any navigation. Navigation requests
// are read off CDP's Page.frameRequestedNavigation, which the browser
// sends for every navigation the page asks for, scripts on or off, and
// in order before the reply to the evaluation that finds done true. So
// "it did not navigate" is an observed absence, not a timed wait. arm is
// an optional expression run before the click (to give a click with no
// visible effect something to report).
func clickAndStay(t *testing.T, ctx context.Context, p point, arm, done string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	active := true
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*cdppage.EventFrameRequestedNavigation); ok {
			mu.Lock()
			if active {
				asked = append(asked, e.URL)
			}
			mu.Unlock()
		}
	})
	defer func() { mu.Lock(); active = false; mu.Unlock() }()
	if arm != "" {
		if err := chromedp.Run(ctx, chromedp.Evaluate(arm, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(p.X, p.Y)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, done)
	mu.Lock()
	defer mu.Unlock()
	if len(asked) > 0 {
		t.Errorf("a click at (%.0f, %.0f), over %s, asked to navigate to %v; it must not navigate", p.X, p.Y, p.Hit, asked)
	}
}

func home(t *testing.T, ctx context.Context, origin string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+"/"), chromedp.WaitVisible("#grid", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, `!!document.querySelector("#combo-in [role=combobox]") && !!document.querySelector("#date-in [rst-dtp-pick]")`)
}

// TestTheWholeRowIsTheTarget is §10.2 at 1280 with a mouse and 390 with
// a coarse pointer. For every row idiom with a primary link, a click at
// the row's far empty edge and inside an empty cell goes to the primary
// href; the status pill is part of the target now; the row's own
// controls each do their own thing and never navigate.
func TestTheWholeRowIsTheTarget(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280 mouse", 1280, 900, false}, {"390 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, rowsPages(t))
			ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h)); err != nil {
				t.Fatal(err)
			}
			home(t, ctx, rig.Origin)
			requirePointer(t, ctx, leg.coarse)

			for _, c := range []struct {
				where, sel   string
				fx, fy, dx   float64
				link, target string
				wideOnly     bool
			}{
				{"the list grid row's far edge", "#row-a", 1, 0.5, -12, "link-a", "/go/row-a", false},
				// The cell is rst-m-hide: below 800px the list grid is three
				// columns and the column that is hidden has no box to probe.
				{"an empty cell of the list grid row", "#cell-a", 0.5, 0.5, 0, "link-a", "/go/row-a", true},
				{"the person row's far edge", "#row-p", 1, 0.5, -12, "link-p", "/go/person", false},
				{"list-row-action's empty main area", "#lra [rst-row-main]", 1, 0.5, -8, "A", "/go/lra", false},
				{"list-row-action's status pill", "#lra [rst-status]", 0.5, 0.5, 0, "A", "/go/lra", false},
				// List-grid pills were never lifted; this pins that they
				// stay under the overlay now that there is one.
				{"the list grid row's status pill", "#row-s [rst-status]", 0.5, 0.5, 0, "link-s", "/go/row-s", false},
			} {
				if c.wideOnly && leg.coarse {
					continue
				}
				home(t, ctx, rig.Origin)
				p := probe(t, ctx, c.sel, c.fx, c.fy, c.dx, 0)
				if p.Hit != c.link {
					t.Errorf("%s: at %s the element under the pointer is %s, want the primary link %s", leg.name, c.where, p.Hit, c.link)
				}
				clickAndLand(t, ctx, p, c.target)
			}

			// The row's own controls.
			home(t, ctx, rig.Origin)
			clickAndLand(t, ctx, probe(t, ctx, "#lra [rst-row-action]", 0.5, 0.5, 0, 0), "/go/lra-edit")
			home(t, ctx, rig.Origin)
			clickAndStay(t, ctx, probe(t, ctx, "#kebab-a", 0.5, 0.5, 0, 0), "", `document.getElementById("menu-a").open`)
			for _, c := range []struct {
				where      string
				fx, fy, dx float64
			}{{"the checkbox itself", 0.5, 0.5, 0}, {"its label's padding", 0, 0, 2}} {
				home(t, ctx, rig.Origin)
				p := probe(t, ctx, "#label-a", c.fx, c.fy, c.dx, c.dx)
				clickAndStay(t, ctx, p, "", `document.getElementById("check-a").checked`)
			}

			// Sizes: 44×44 for everything in the rows on a phone; on the
			// desktop the checkbox's label is the approved 24×24.
			home(t, ctx, rig.Origin)
			if leg.coarse {
				assertTargets(t, leg.name+", rows", readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify([...measure(document.getElementById("lra")), ...measure(document.getElementById("statuses")), ...measure(document.getElementById("grid")), ...measure(document.getElementById("people"))]); })()`))
			} else {
				var box [2]float64
				at(t, ctx, `(() => { const r = document.getElementById("label-a").getBoundingClientRect(); return JSON.stringify([r.width, r.height]); })()`, &box)
				if box != [2]float64{24, 24} {
					t.Errorf("%s: the row checkbox's label is %v, want 24×24", leg.name, box)
				}
			}
		})
	}
}

// TestAnOpenRowMenuStaysAboveTheRowsBelowIt: only the summary is lifted
// (§2.3), so the open panel — z-index 40, or position: fixed under
// anchor positioning — paints above the next row's lifted controls, and
// a click on an item lying over row B's kebab hits the item.
func TestAnOpenRowMenuStaysAboveTheRowsBelowIt(t *testing.T) {
	for _, coarse := range []bool{false, true} {
		t.Run(fmt.Sprintf("coarse %v", coarse), func(t *testing.T) {
			rig := sizingRig(t, coarse, rowsPages(t))
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			w := int64(1280)
			if coarse {
				w = 390
			}
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(w, 844)); err != nil {
				t.Fatal(err)
			}
			home(t, ctx, rig.Origin)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("kebab-a").click(), true`, nil)); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Overlapping int
				Wrong       []string
			}
			at(t, ctx, `(() => {
			  const k = document.getElementById("kebab-b").getBoundingClientRect(), wrong = []; let n = 0;
			  for (const item of document.querySelectorAll("#panel-a a")) {
			    const r = item.getBoundingClientRect();
			    const x0 = Math.max(r.left, k.left), x1 = Math.min(r.right, k.right), y0 = Math.max(r.top, k.top), y1 = Math.min(r.bottom, k.bottom);
			    if (x1 - x0 < 2 || y1 - y0 < 2) continue;
			    n++;
			    const h = document.elementFromPoint((x0 + x1) / 2, (y0 + y1) / 2);
			    if (!(h === item || item.contains(h))) wrong.push(item.textContent + " -> " + (h ? (h.id || h.tagName) : "nothing"));
			  }
			  return JSON.stringify({Overlapping: n, Wrong: wrong});
			})()`, &got)
			if got.Overlapping == 0 {
				t.Fatalf("coarse=%v: no item of row A's open menu lies over row B's kebab; the case this leg is for has not arisen", coarse)
			}
			if len(got.Wrong) > 0 {
				t.Errorf("coarse=%v: over row B's kebab, a click on row A's menu lands elsewhere: %v", coarse, got.Wrong)
			}
		})
	}
}

// TestControlsInARowKeepTheirOwnBoxes: the lifting rule is wholly inside
// :where(), so it gives a static control a position and never replaces
// one a component chose (review round 2, finding 4). A switch's input
// stays over its track, the date field's pick button inside its field,
// select.js's native select out of flow, each the same size in a row as
// outside one. And each is OPERATED, at 1280 with a mouse and at 390
// with a coarse pointer: the switch toggles, the calendar opens and a
// day is picked into the native input, the combobox opens and an option
// is picked into the native select, and none of it opens the row.
func TestControlsInARowKeepTheirOwnBoxes(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280 mouse", 1280, 900, false}, {"390 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, rowsPages(t))
			ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h)); err != nil {
				t.Fatal(err)
			}
			home(t, ctx, rig.Origin)
			requirePointer(t, ctx, leg.coarse)
			var g map[string]string
			at(t, ctx, `(() => {
			  const pos = el => getComputedStyle(el).position;
			  const size = el => { const r = el.getBoundingClientRect(); return Math.round(r.width) + "x" + Math.round(r.height); };
			  // fx is the fraction across the box to test: the date input is
			  // tested a quarter of the way in, clear of the picker button
			  // that sits over its inline end on purpose.
			  const own = (sel, fx) => { const el = document.querySelector(sel); el.scrollIntoView({block: "center"}); const r = el.getBoundingClientRect(), h = document.elementFromPoint(r.left + r.width * (fx || 0.5), r.top + r.height / 2); return h && (h === el || el.contains(h)) ? "ok" : "hit " + (h ? h.tagName : "nothing"); };
			  const pick = side => { const p = document.querySelector("#date-" + side + " [rst-dtp-pick]"), d = p.closest("[rst-dtp]"); return Math.round(d.getBoundingClientRect().right - p.getBoundingClientRect().right) + "px from the end"; };
			  return JSON.stringify({
			    switchIn: pos(document.querySelector("#switch-in input")) + " " + size(document.querySelector("#switch-in [rst-switch-track]")),
			    switchOut: pos(document.querySelector("#switch-out input")) + " " + size(document.querySelector("#switch-out [rst-switch-track]")),
			    pickIn: pos(document.querySelector("#date-in [rst-dtp-pick]")) + " " + size(document.querySelector("#date-in [rst-dtp-pick]")) + " " + pick("in"),
			    pickOut: pos(document.querySelector("#date-out [rst-dtp-pick]")) + " " + size(document.querySelector("#date-out [rst-dtp-pick]")) + " " + pick("out"),
			    selectIn: pos(document.querySelector("#combo_in")), selectOut: pos(document.querySelector("#combo_out")),
			    switchHit: own("#switch-in"), dateHit: own("#date-in [role=combobox]", 0.25), pickHit: own("#date-in [rst-dtp-pick]"), comboHit: own("#combo-in [role=combobox]")});
			})()`, &g)
			for _, pair := range [][2]string{{"switchIn", "switchOut"}, {"pickIn", "pickOut"}, {"selectIn", "selectOut"}} {
				if g[pair[0]] != g[pair[1]] {
					t.Errorf("in a row %s is %q; outside one it is %q", pair[0], g[pair[0]], g[pair[1]])
				}
			}
			if !strings.HasPrefix(g["switchIn"], "absolute") || g["selectIn"] != "absolute" {
				t.Errorf("the lifting rule replaced a component's own position: switch input %q, native select %q", g["switchIn"], g["selectIn"])
			}
			for _, k := range []string{"switchHit", "dateHit", "pickHit", "comboHit"} {
				if g[k] != "ok" {
					t.Errorf("in a row, %s: %s", k, g[k])
				}
			}

			// Operated. Each click must do its job and ask for no
			// navigation (the row's link is under all of them).
			clickAndStay(t, ctx, probe(t, ctx, "#switch-in [rst-switch-track]", 0.5, 0.5, 0, 0), "", `document.querySelector("#switch-in input").checked`)
			clickAndStay(t, ctx, probe(t, ctx, "#date-in [rst-dtp-pick]", 0.5, 0.5, 0, 0), "", `!!document.querySelector('[rst-cal] [data-rst-day="2026-08-12"]')`)
			clickAndStay(t, ctx, probe(t, ctx, `[rst-cal] [data-rst-day="2026-08-12"]`, 0.5, 0.5, 0, 0), "", `document.querySelector('#date-in input[name="due_in"]').value === "2026-08-12"`)
			clickAndStay(t, ctx, probe(t, ctx, "#combo-in [role=combobox]", 0.5, 0.5, 0, 0), "", `document.querySelectorAll("#combo-in [role=option]").length === 12 && document.querySelector("#combo-in [role=option]").checkVisibility()`)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const o = [...document.querySelectorAll("#combo-in [role=option]")].find(o => o.textContent.trim() === "Option 7"); o.scrollIntoView({block: "center"}); return true; })()`, nil)); err != nil {
				t.Fatal(err)
			}
			at(t, ctx, `JSON.stringify((() => { const o = [...document.querySelectorAll("#combo-in [role=option]")].find(o => o.textContent.trim() === "Option 7"); o.setAttribute("data-pick", ""); return true; })())`, new(bool))
			clickAndStay(t, ctx, probe(t, ctx, "#combo-in [data-pick]", 0.5, 0.5, 0, 0), "", `document.getElementById("combo_in").value === "7"`)
		})
	}
}

// TestARowWithNoLinkLooksAndActsInert: no overlay, no hover fill, in
// both idioms. The linked row of the same idiom is each one's control:
// its hover DOES fill.
func TestARowWithNoLinkLooksAndActsInert(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t))
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900)); err != nil {
		t.Fatal(err)
	}
	home(t, ctx, rig.Origin)
	bg := func(sel string) string {
		var s string
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`getComputedStyle(document.querySelector(%q)).backgroundColor`, sel), &s)); err != nil {
			t.Fatal(err)
		}
		return s
	}
	hover := func(sel string) string {
		p := probe(t, ctx, sel, 0.5, 0.5, 0, 0)
		if err := chromedp.Run(ctx, chromedp.MouseEvent("mouseMoved", p.X, p.Y)); err != nil {
			t.Fatal(err)
		}
		return bg(sel)
	}
	for _, c := range []struct{ idiom, inert, linked string }{
		{"list grid", "#row-n", "#row-a"},
		{"list-row-action", "#lra-n", "#lra [rst-row]"},
	} {
		if err := chromedp.Run(ctx, chromedp.MouseEvent("mouseMoved", 1, 1)); err != nil {
			t.Fatal(err)
		}
		restN, restA := bg(c.inert), bg(c.linked)
		if hovered := hover(c.linked); hovered == restA {
			t.Fatalf("%s CONTROL: hovering a linked row does not change its background, so the inert row's unchanged background proves nothing", c.idiom)
		}
		if err := chromedp.Run(ctx, chromedp.MouseEvent("mouseMoved", 1, 1)); err != nil {
			t.Fatal(err)
		}
		if hovered := hover(c.inert); hovered != restN {
			t.Errorf("%s: hovering a row with no link fills it (%s -> %s): it looks clickable and is not", c.idiom, restN, hovered)
		}
		p := probe(t, ctx, c.inert, 0.5, 0.5, 0, 0)
		// Any link at all, by tag, not by the id-less "A" probe reports:
		// a link with an id would otherwise read as its id and slip by.
		var link string
		at(t, ctx, fmt.Sprintf(`(() => { const h = document.elementFromPoint(%v, %v), a = h && h.closest("a"); return JSON.stringify(a ? a.outerHTML.slice(0, 60) : ""); })()`, p.X, p.Y), &link)
		if link != "" {
			t.Errorf("%s: the centre of a row with no link is under a link: %s", c.idiom, link)
		}
		clickAndStay(t, ctx, p,
			fmt.Sprintf(`window.rowClicked = false, document.querySelector(%q).addEventListener("click", () => { window.rowClicked = true; }), true`, c.inert), `window.rowClicked === true`)
	}
}

// TestMeasureRejectsAHitOnAnotherPartOfTheRow pins the target drive's
// ownership rule itself. A stretched link is measured over its whole
// row, and the row holds other things; a hit at the row's centre on an
// unrelated child (here a positioned span covering the row) must count
// as occlusion, never as the link. The same row without the cover is
// the control and must read as owned.
func TestMeasureRejectsAHitOnAnotherPartOfTheRow(t *testing.T) {
	row := func(cover string) string {
		return `<div rst-card style="--rst-cols: minmax(0, 1fr) 110px"><div rst-lrow><a class="rst-nm" id="name" href="/go/x">Grace Hopper</a><span class="rst-cell-mut">Paid</span>` + cover + `</div></div>`
	}
	pages := map[string]string{
		"/covered": sizingDoc("covered", `<div rst-page>`+row(`<span id="cover" style="position: absolute; inset: 0; z-index: 2"></span>`)+`</div>`),
		"/clear":   sizingDoc("clear", `<div rst-page>`+row("")+`</div>`),
	}
	rig := sizingRig(t, false, pages)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	read := func(path string) targetReading {
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("#name", chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		for _, g := range readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify(measure(document)); })()`) {
			if strings.Contains(g.Name, "#name") {
				return g
			}
		}
		t.Fatalf("%s: the stretched link was not measured", path)
		return targetReading{}
	}
	if g := read("/clear"); !g.Owns {
		t.Fatalf("CONTROL: an unobstructed stretched link reads as not owning its row (hit %s); the instrument is broken", g.Hit)
	}
	if g := read("/covered"); g.Owns {
		t.Errorf("a hit on an unrelated child of the row (%s) counted as the stretched link's own; the ownership rule is accepting any descendant of the measured box", g.Hit)
	}
}

// TestFocusDrawsTheRingAroundTheWholeRow: the primary link's ring moves
// to its overlay, 2px inside the row, so the focused row is outlined
// whole; the link itself also underlines, which is what the keyboard
// walk's element-and-ancestors reading sees.
func TestFocusDrawsTheRingAroundTheWholeRow(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t))
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900)); err != nil {
		t.Fatal(err)
	}
	home(t, ctx, rig.Origin)
	if err := chromedp.Run(ctx, chromedp.Focus("#check-a", chromedp.ByQuery), chromedp.KeyEvent(kb.Tab)); err != nil {
		t.Fatal(err)
	}
	var g struct {
		Active, Style, Width, Offset, Own, Deco, Corner1, Corner2 string
		Visible                                                   bool
	}
	at(t, ctx, `(() => {
	  const a = document.activeElement, row = document.getElementById("row-a"), r = row.getBoundingClientRect(), after = getComputedStyle(a, "::after");
	  const hit = (x, y) => { const h = document.elementFromPoint(x, y); return h ? (h.id || h.tagName) : "nothing"; };
	  return JSON.stringify({Active: a.id, Visible: a.matches(":focus-visible"), Style: after.outlineStyle, Width: after.outlineWidth, Offset: after.outlineOffset,
	    Own: getComputedStyle(a).outlineStyle, Deco: getComputedStyle(a).textDecorationLine,
	    Corner1: hit(r.left + 3, r.top + 3), Corner2: hit(r.right - 3, r.bottom - 3)});
	})()`, &g)
	if g.Active != "link-a" || !g.Visible {
		t.Fatalf("Tab from the checkbox focused %q (focus-visible %v), want link-a; the rest of this leg reads nothing", g.Active, g.Visible)
	}
	if g.Style != "solid" || g.Width != "2px" || g.Offset != "-2px" {
		t.Errorf("the overlay's ring is %s %s offset %s, want a solid 2px ring inset 2px", g.Style, g.Width, g.Offset)
	}
	if g.Own != "none" || g.Deco != "underline" {
		t.Errorf("the link itself has outline %s and decoration %s, want none and underline", g.Own, g.Deco)
	}
	if g.Corner1 != "link-a" || g.Corner2 != "link-a" {
		t.Errorf("the overlay does not reach the row's corners (%s, %s): the ring is not around the whole row", g.Corner1, g.Corner2)
	}
}
