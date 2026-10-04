//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// readType loads path at w×h and returns the token and field readings.
func readType(t *testing.T, ctx context.Context, url string, w, h int64) (typeReading, []fontReading, float64) {
	t.Helper()
	var rawType, rawFonts string
	var primary float64
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(w, h),
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(tokensJS, &rawType),
		chromedp.Evaluate(fontsJS, &rawFonts),
		chromedp.Evaluate(`parseFloat(getComputedStyle(document.getElementById("sizing-primary")).fontSize)`, &primary),
	); err != nil {
		t.Fatalf("reading type at %dx%d: %v", w, h, err)
	}
	var tr typeReading
	var fonts []fontReading
	if err := json.Unmarshal([]byte(rawType), &tr); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(rawFonts), &fonts); err != nil {
		t.Fatal(err)
	}
	// The small-parent fields and the bare and primary inputs are the
	// cases this drive exists for; each is required by name, so losing
	// one from the fixture fails here rather than shrinking the sweep.
	named := map[string]bool{}
	for _, f := range fonts {
		named[f.Name] = true
	}
	for _, id := range []string{"sizing-bulk-search", "sizing-bulk-input", "sizing-bulk-note", "sizing-menu-search", "sizing-menu-input", "sizing-menu-note", "sizing-bare", "sizing-primary"} {
		if !named[id] {
			t.Fatalf("the sizing page has no %s; the fixture is not the one this drive measures", id)
		}
	}
	return tr, fonts, primary
}

// TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens is the type
// half of §10.1 of the mobile ergonomics design spec
// (docs/superpowers/specs/2026-09-30-mobile-ergonomics-design.md): at
// 390 with touch, at 1024 with touch (the pointer half of the query
// alone) and at 600 with a mouse (the width half alone), the four
// tokens are one step up, every text-entry control is at least 16px —
// including the ones in a bulk bar and a menu panel — and the primary
// field is exactly --rst-fs-lg, 19px (the 1em bug gave 16).
func TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens(t *testing.T) {
	pages := map[string]string{"/": sizingDoc("sizing", sizingFixture(t))}
	for _, coarse := range []bool{true, false} {
		rig := sizingRig(t, coarse, pages)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		legs := []struct {
			name string
			w, h int64
		}{{"600x800 mouse, the width half alone", 600, 800}}
		if coarse {
			legs = []struct {
				name string
				w, h int64
			}{{"390x844 touch", 390, 844}, {"1024x768 touch, the pointer half alone", 1024, 768}}
		}
		for _, leg := range legs {
			tr, fonts, primary := readType(t, ctx, rig.Origin+"/", leg.w, leg.h)
			requirePointer(t, ctx, coarse)
			// Each leg exists to exercise one half of the query: the 1024
			// touch leg proves the pointer half only if the page really is
			// wider than 40rem, and the 600 mouse leg the width half only if
			// the pointer really is fine.
			if tr.Width != int(leg.w) || tr.Coarse != coarse {
				t.Fatalf("%s: the page read width %d, coarse %v; this leg needs width %d, coarse %v", leg.name, tr.Width, tr.Coarse, leg.w, coarse)
			}
			if tr.Lg != 19 || tr.Base != 16 || tr.Sm != 14 || tr.Xs != 13 || tr.Body != 16 {
				t.Errorf("%s: tokens %v/%v/%v/%v body %v, want 19/16/14/13 body 16", leg.name, tr.Lg, tr.Base, tr.Sm, tr.Xs, tr.Body)
			}
			for _, f := range fonts {
				if f.Px < 16 {
					t.Errorf("%s: %s is %vpx; a phone zooms the page when it is focused", leg.name, f.Name, f.Px)
				}
			}
			if primary != 19 {
				t.Errorf("%s: the primary field is %vpx, want --rst-fs-lg (19px); 16 is the 1em trap", leg.name, primary)
			}
		}
		cancel()
	}
}

// TestDesktopDensityIsPinned is §10.1's 1280×900 mouse leg: every value
// below is today's, measured, and any change fails, except the
// deliberate desktop changes (§1.5), pinned at their new values: the
// row checkbox's 24×24 label here, and the whole-row target, which
// TestTheWholeRowIsTheTarget holds.
func TestDesktopDensityIsPinned(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("sizing", sizingFixture(t))})
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()
	tr, fonts, primary := readType(t, ctx, rig.Origin+"/", 1280, 900)
	requirePointer(t, ctx, false)
	if tr.Lg != 17 || tr.Base != 14 || tr.Sm != 12.5 || tr.Xs != 11.5 || tr.Body != 14 {
		t.Errorf("desktop tokens %v/%v/%v/%v body %v, want 17/14/12.5/11.5 body 14", tr.Lg, tr.Base, tr.Sm, tr.Xs, tr.Body)
	}
	if primary != 17 {
		t.Errorf("desktop primary field %vpx, want 17", primary)
	}
	// The control for the touch drive's "every field ≥ 16": on a desktop
	// some field is under 16, so the instrument can see a small one.
	small := false
	for _, f := range fonts {
		small = small || f.Px < 16
	}
	if !small {
		t.Error("CONTROL: every text control is ≥16px on a 1280px desktop, so the touch drive's ≥16 reading proves nothing")
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
	  const box = sel => { const r = document.querySelector(sel).getBoundingClientRect(); return [Math.round(r.width * 100) / 100, Math.round(r.height * 100) / 100]; };
	  return JSON.stringify({Sm: box("#sizing-btn-sm")[1], Md: box("#sizing-btn")[1], Lg: box("#sizing-btn-lg")[1],
	    Kebab: box('[data-sample="list-grid"] [rst-row-menu] > summary'), Label: box('[data-sample="selbox"] [rst-selbox]'),
	    Box: box('[data-sample="selbox"] [rst-selbox] input')});
	})()`, &raw)); err != nil {
		t.Fatal(err)
	}
	var d struct {
		Sm, Md, Lg        float64
		Kebab, Label, Box [2]float64
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	// Measured before the touch rules existed: 27.69, 33.88 and 43.97
	// (the "about 28/34/44" of tokens.css's header), a 26×26 kebab, a
	// 16px checkbox.
	if math.Round(d.Sm) != 28 || math.Round(d.Md) != 34 || math.Round(d.Lg) != 44 {
		t.Errorf("desktop button heights %v/%v/%v, want 28/34/44", d.Sm, d.Md, d.Lg)
	}
	if d.Kebab != [2]float64{26, 26} {
		t.Errorf("desktop kebab %v, want 26×26", d.Kebab)
	}
	if d.Box != [2]float64{16, 16} || d.Label != [2]float64{24, 24} {
		t.Errorf("desktop row checkbox %v in a label %v, want the 16px box in a 24×24 target", d.Box, d.Label)
	}
	c := openCalendar(t, ctx, rig.Origin+"/", 1280, 900)
	if c.Position != "absolute" || c.InlineSize != "288px" {
		t.Errorf("desktop calendar is %s, inline-size %s; want today's anchored 18rem (288px) panel", c.Position, c.InlineSize)
	}
}

// targetReading is one control, measured as its activation area.
type targetReading struct {
	Name, Hit          string
	W, H               float64
	Fits, Owns, Inline bool
}

// measureFn is the measuring half every target reading shares, written
// once so the closed page, each opened overlay and the modal are the
// same instrument. The activation area is the element a tap activates:
// a checkbox or radio is measured as its label, a stretched link (an
// absolutely positioned ::after with content) as its positioned
// ancestor, the row. Each area is scrolled into view (scrollIntoView
// scrolls every scrolling ancestor, a rail or a menu panel included)
// and measured again after the scroll, and the point tested is the
// centre of the part inside the viewport. elementFromPoint there must
// return the control or a descendant of it (a switch's track, a
// summary's icon), or for a labelled input its label: anything else is
// an occlusion failure, never a skip. Only a control that is not
// rendered at all is skipped. An element marked
// data-sizing-not-an-idiom is an app's own control on the fixture for
// the font test and is not an idiom this floor covers.
const measureFn = `function measure(root, skip) {
  const CONTROLS = 'a[href], button, summary, input:not([type=hidden]), select, textarea, [rst-cal-day], [role="option"]';
  const describe = el => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") +
    [...el.attributes].filter(a => a.name.startsWith("rst-")).map(a => "[" + a.name + (a.value ? "=" + a.value : "") + "]").join("") +
    " “" + (el.getAttribute("aria-label") || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 30) + "”";
  const rendered = el => {
    if (el.checkVisibility && !el.checkVisibility({visibilityProperty: true})) return false;
    const r = el.getBoundingClientRect();
    return r.width >= 2 && r.height >= 2 && getComputedStyle(el).clipPath !== "inset(50%)";
  };
  const area = el => {
    if (el.matches("input[type=checkbox], input[type=radio]")) {
      const l = el.closest("label") || (el.id && document.querySelector('label[for="' + el.id + '"]'));
      if (l) return l;
    }
    if (el.tagName === "A") {
      const after = getComputedStyle(el, "::after");
      // pointer-events: none is a tooltip ([rst-tip]::after), which a tap
      // passes through; only an overlay that takes the tap stretches the
      // link, and mistaking the tip for one measured the whole page.
      if (after.content !== "none" && after.position === "absolute" && after.pointerEvents !== "none" && el.offsetParent) return el.offsetParent;
    }
    return el;
  };
  const out = [], seen = new Set();
  root.querySelectorAll(CONTROLS).forEach(el => {
    if (skip && skip(el)) return;
    if (el.closest("[data-sizing-not-an-idiom]")) return;
    // Inert content (a modal's backdrop) is unreachable on purpose.
    if (el.closest("[inert]")) return;
    const a = area(el);
    if (seen.has(a) || !rendered(a)) return;
    seen.add(a);
    el.setAttribute("data-measured", "");
    a.setAttribute("data-measured-area", "");
    a.scrollIntoView({block: "center", inline: "center"});
    const r = a.getBoundingClientRect();
    const x0 = Math.max(r.left, 0), y0 = Math.max(r.top, 0), x1 = Math.min(r.right, innerWidth), y1 = Math.min(r.bottom, innerHeight);
    const hit = document.elementFromPoint((x0 + x1) / 2, (y0 + y1) / 2);
    out.push({Name: describe(el), W: r.width, H: r.height,
      Fits: r.left >= -0.5 && r.top >= -0.5 && r.right <= innerWidth + 0.5 && r.bottom <= innerHeight + 0.5,
      // Ownership is the control, a descendant of it (a summary's icon,
      // a switch's track), or, for a labelled input only, its label and
      // what the label holds. Never "anything in the measured box": for a
      // stretched link the box is the whole row, and a row holds other
      // controls, so a hit on the kebab must not count for the name link.
      Owns: !!hit && (hit === el || el.contains(hit) || (a !== el && a.tagName === "LABEL" && a.contains(hit))),
      Hit: hit ? describe(hit) : "nothing",
      Inline: el.tagName === "A" && !el.hasAttribute("rst-btn") && !!el.closest("p, [rst-field-help]") && !el.closest("nav")});
  });
  return out;
}`

// targetsJS measures the page as it loads: every overlay closed.
const targetsJS = `(() => { ` + measureFn + `; return JSON.stringify(measure(document)); })()`

// overlayJS opens the i-th <details> on the page (and every <details>
// around it), measures what it revealed — its own summary was measured
// closed — and closes everything again, so one open overlay never
// counts as occluding the next. It is a function the caller applies to
// i, not a Sprintf format: measureFn holds "inset(50%)", which a format
// would mangle into a script error.
const overlayJS = `(async (i) => { ` + measureFn + `;
  const all = document.querySelectorAll("details");
  if (i >= all.length) return JSON.stringify({Done: true});
  const d = all[i];
  for (let p = d; p; p = p.parentElement && p.parentElement.closest("details")) p.open = true;
  // A menu inside the topbar's or console's tail is behind a SIBLING
  // disclosure (the tail is the shell menu's next sibling, not its
  // content), which the ancestor walk above cannot see.
  const tail = d.closest("[rst-shell-tail], .rst-shell__tail");
  if (tail && tail.previousElementSibling && tail.previousElementSibling.matches("details")) tail.previousElementSibling.open = true;
  // A menu panel is position: fixed and anchored to its summary where
  // anchor positioning is supported, and scrollIntoView on a fixed
  // panel's items cannot scroll the page, so the anchor would stay
  // wherever the last control left the page and the panel with it.
  // Bring the outermost summary into view first, as a tap on it would,
  // and wait two frames: an anchored panel takes up its anchor's new
  // scroll position at the next frame, so a rect read at once is stale.
  let outer = d;
  while (outer.parentElement && outer.parentElement.closest("details")) outer = outer.parentElement.closest("details");
  outer.querySelector(":scope > summary").scrollIntoView({block: "center", inline: "center"});
  await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
  const summary = d.querySelector(":scope > summary");
  const got = measure(d, el => el === summary);
  // A shell disclosure reveals its next SIBLING (the topbar's tail, the
  // old sidebar's rail), not its own content.
  if (d.matches("[rst-shell-menu], [rst-shell-chrome]") && d.nextElementSibling) got.push(...measure(d.nextElementSibling));
  document.querySelectorAll("details[open]").forEach(x => { x.open = false; });
  return JSON.stringify({Done: false, Targets: got});
})`

func readTargets(t *testing.T, ctx context.Context, js string) []targetReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("measuring targets: %v", err)
	}
	var got []targetReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading targets (%q): %v", raw, err)
	}
	return got
}

// assertTargets holds every measured control to 44×44, whole in the
// viewport and hittable at its centre. A link in running text is the
// one exemption (WCAG 2.5.8's inline exception).
func assertTargets(t *testing.T, where string, got []targetReading) {
	t.Helper()
	for _, g := range got {
		switch {
		case !g.Fits:
			t.Errorf("%s: %s is %.1f×%.1f and cannot be brought wholly into the viewport; too big to hit", where, g.Name, g.W, g.H)
		case !g.Owns:
			t.Errorf("%s: %s is occluded: a tap at its centre lands on %s", where, g.Name, g.Hit)
		case g.Inline:
		case g.W < 43.5 || g.H < 43.5:
			t.Errorf("%s: %s is %.1f×%.1f, under the 44×44 floor", where, g.Name, g.W, g.H)
		}
	}
}

// measureEverything reads the closed page, then every overlay alone,
// then the combobox's list and the date field's list, each opened by
// the script that owns it.
func measureEverything(t *testing.T, ctx context.Context, where string) int {
	t.Helper()
	settleUntil(t, ctx, `!!document.querySelector("[rst-combo] [role=combobox]") && !!document.querySelector("[rst-dtp-pick]")`)
	got := readTargets(t, ctx, targetsJS)
	n := len(got)
	assertTargets(t, where+", overlays closed", got)
	for i := 0; ; i++ {
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf("%s(%d)", overlayJS, i), &raw, awaitPromise)); err != nil {
			t.Fatalf("%s: opening overlay %d: %v", where, i, err)
		}
		var o struct {
			Done    bool
			Targets []targetReading
		}
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			t.Fatal(err)
		}
		if o.Done {
			break
		}
		n += len(o.Targets)
		assertTargets(t, fmt.Sprintf("%s, overlay %d open", where, i), o.Targets)
	}
	for _, open := range []struct{ name, js, list string }{
		{"the combobox's list", `(() => { const i = document.querySelector("#sizing-combo").closest("[rst-field]").querySelector("[role=combobox]"); i.focus(); i.click(); return true; })()`, "[rst-combo-list]"},
		{"the date field's list", `(() => { const i = document.querySelector("[rst-dtp] [role=combobox]"); i.focus(); i.value = "t"; i.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`, "[rst-dtp-list]"},
	} {
		if err := chromedp.Run(ctx, chromedp.Evaluate(open.js, nil)); err != nil {
			t.Fatalf("%s: opening %s: %v", where, open.name, err)
		}
		settleUntil(t, ctx, `(() => { const l = document.querySelector("`+open.list+`"); return !!l && l.checkVisibility() && l.querySelectorAll("[role=option]").length > 0; })()`)
		got := readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify(measure(document.querySelector("`+open.list+`"))); })()`)
		if len(got) == 0 {
			t.Fatalf("%s: %s opened with no options to measure", where, open.name)
		}
		n += len(got)
		assertTargets(t, where+", "+open.name, got)
		chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape))
	}
	return n
}

// settleUntil polls a page expression until it is true or 10s pass.
func settleUntil(t *testing.T, ctx context.Context, expr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var yes bool
		// Errors are tolerated: an evaluation that lands while a document
		// is being swapped in fails, and the next one reads the new page.
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &yes)); err == nil && yes {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("never became true within 10s: %s", expr)
}

// assertCovered is what makes "every control passed" mean something,
// in two halves. Every rendered control on the closed page was measured
// (none skipped by a bug in measure itself), and for every inventory row
// on this page at least one element matching its Probe was measured
// (measureFn marks each with data-measured), so an idiom cannot drop out
// of the drive because its only fixture went away. Each named fixture
// control is also required by id in TestTextControlsAreSixteenPixels.
func assertCovered(t *testing.T, ctx context.Context, where, page string, wide bool) {
	t.Helper()
	var missed []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('a[href], button, summary, input:not([type=hidden]), select, textarea')]
	  .filter(e => !e.hasAttribute("data-measured") && !e.closest("[data-sizing-not-an-idiom], [inert]") && e.checkVisibility({visibilityProperty: true}) && getComputedStyle(e).clipPath !== "inset(50%)" && e.getBoundingClientRect().width >= 2)
	  .filter(e => !(e.matches("input[type=checkbox], input[type=radio]") && e.closest("label") && e.closest("label").hasAttribute("data-measured-area")))
	  .map(e => e.outerHTML.slice(0, 80))`, &missed)); err != nil {
		t.Fatalf("%s: listing unmeasured controls: %v", where, err)
	}
	if len(missed) > 0 {
		t.Errorf("%s: %d rendered controls were never measured: %v", where, len(missed), missed)
	}
	for _, e := range tapInventory {
		sel, entryPage := e.Probe, ""
		switch {
		case strings.HasPrefix(sel, "elsewhere:"):
			continue
		case strings.HasPrefix(sel, "narrow:"):
			if wide {
				continue
			}
			sel = strings.TrimPrefix(sel, "narrow:")
		case strings.HasPrefix(sel, "modal:"):
			sel, entryPage = strings.TrimPrefix(sel, "modal:"), "modal"
		}
		if entryPage != page {
			continue
		}
		var n int
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`[...document.querySelectorAll(%q)].filter(e => e.hasAttribute("data-measured")).length`, sel), &n)); err != nil {
			t.Fatalf("%s: counting %s: %v", where, sel, err)
		}
		if n == 0 {
			t.Errorf("%s: no %s (%s) was measured; the idiom dropped out of the drive", where, sel, e.Idiom)
		}
	}
}

// fixtureCounts are controls the sizing fixture itself owns, with how
// many of each must have been measured. Declared here, not read off the
// page: a control that disappears from the fixture is absent from the
// DOM too, so only a number written down independently can notice it,
// even while another control of the same idiom survives.
var fixtureCounts = []struct {
	Sel string
	N   int
}{
	{`[data-extra="buttons"] [rst-btn]`, 3},
	{`[data-extra="locale"] [rst-locale] button`, 3},
	{`[data-extra="combobox"] [role=option]`, 12},
	{`[data-extra="legacy-drawer"] [rst-shell-nav] a`, 2},
	{`[data-extra="small-parents"] :is(input, textarea)`, 7}, // the bare input is the app's own, not measured
	// Previous and 1 are spans (disabled, current); 2 and 9 are links.
	{`[data-partial="pagination"] [rst-pagination] a`, 2},
	{`[data-partial="bulk-bar"] [rst-dropdown-menu] button`, 2},
	{`[data-partial="seg-tabs"] a`, 2},
	{`[data-partial="dropdown"] [rst-dropdown-menu] a`, 1},
	{`[data-extra="short-labels"] [rst-bulkbar-escalate]`, 1},
	{`[data-extra="short-labels"] a[rst-person]`, 1},
}

func assertFixtureCounts(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	for _, c := range fixtureCounts {
		var n int
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`[...document.querySelectorAll(%q)].filter(e => e.hasAttribute("data-measured")).length`, c.Sel), &n)); err != nil {
			t.Fatalf("%s: counting %s: %v", where, c.Sel, err)
		}
		if n != c.N {
			t.Errorf("%s: %d of %s were measured, want exactly %d", where, n, c.Sel, c.N)
		}
	}
}

// TestEveryTapTargetIsAtLeast44Pixels is §10.1's target half, at 390
// and 1024 with a coarse pointer and at 600 with a mouse, then the modal
// on a page of its own.
func TestEveryTapTargetIsAtLeast44Pixels(t *testing.T) {
	pages := map[string]string{
		"/":      sizingDoc("sizing", sizingFixture(t)),
		"/modal": sizingDoc("modal", Styleguide()["modal"]),
	}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{
		{"390x844 touch", 390, 844, true},
		{"1024x768 touch", 1024, 768, true},
		{"600x800 mouse", 600, 800, false},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/")); err != nil {
				t.Fatal(err)
			}
			requirePointer(t, ctx, leg.coarse)
			n := measureEverything(t, ctx, leg.name)
			t.Logf("%s: %d controls measured", leg.name, n)
			assertCovered(t, ctx, leg.name, "", leg.w >= 800)
			assertFixtureCounts(t, ctx, leg.name)
			if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+"/modal")); err != nil {
				t.Fatal(err)
			}
			got := readTargets(t, ctx, targetsJS)
			assertTargets(t, leg.name+", the modal", got)
			assertCovered(t, ctx, leg.name+", the modal", "modal", leg.w >= 800)
		})
	}
}

// TestTheKebabOverflowsIntoTheGapOnALiteralColumn is §2.3's claim for
// an app that keeps --rst-cols' literal 32px: on a wide touch screen the
// 44px kebab is justify-self: end, so it spills 12px toward the inline
// start, into the 0.85rem gap, and never over the cell beside it.
func TestTheKebabOverflowsIntoTheGapOnALiteralColumn(t *testing.T) {
	page := sizingDoc("literal", `<div rst-page><div rst-card style="--rst-cols: 1fr 110px 32px"><div rst-lrow>`+
		`<a class="rst-nm" href="#">Grace Hopper</a><span id="cell" class="rst-cell-mut">Paid</span>`+
		`<details rst-row-menu name="rst-menus"><summary id="kebab" aria-label="Actions">⋮</summary><div rst-row-menu-panel><a href="#">View</a></div></details>`+
		`</div></div></div>`)
	rig := sizingRig(t, true, map[string]string{"/": page})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var raw string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1024, 768), chromedp.Navigate(rig.Origin+"/"),
		chromedp.Evaluate(`(() => { const c = document.getElementById("cell").getBoundingClientRect(), k = document.getElementById("kebab").getBoundingClientRect();
		  return JSON.stringify({CellEnd: c.right, KebabStart: k.left, W: k.width}); })()`, &raw)); err != nil {
		t.Fatal(err)
	}
	requirePointer(t, ctx, true)
	var g struct{ CellEnd, KebabStart, W float64 }
	json.Unmarshal([]byte(raw), &g)
	if g.W < 43.5 {
		t.Fatalf("the kebab is %.1fpx wide on a touch screen; the premise (a 44px kebab in a 32px track) is not met", g.W)
	}
	if g.KebabStart < g.CellEnd-0.5 {
		t.Errorf("the 44px kebab starts at %.1f and the cell beside it ends at %.1f: it overlaps the neighbouring cell", g.KebabStart, g.CellEnd)
	}
}

// calJS reads the open calendar: where the panel is, whether it
// scrolls, how big each day is, and whether every day and both month
// buttons can be scrolled to and hit.
const calJS = `(() => {
  const cal = document.querySelector("[rst-cal]");
  if (!cal || !cal.checkVisibility()) return JSON.stringify({Open: false});
  const items = [...cal.querySelectorAll("[rst-cal-nav], [rst-cal-day]")];
  let minW = 1e9, minH = 1e9; const unhittable = [];
  for (const el of items) {
    el.scrollIntoView({block: "nearest", inline: "nearest"});
    const b = el.getBoundingClientRect();
    if (el.matches("[rst-cal-day]")) { minW = Math.min(minW, b.width); minH = Math.min(minH, b.height); }
    const hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    if (!hit || !(hit === el || el.contains(hit))) unhittable.push((el.getAttribute("data-rst-day") || "nav") + " -> " + (hit ? hit.tagName : "nothing"));
  }
  cal.scrollTop = 0;
  const r = cal.getBoundingClientRect(), g = cal.querySelector("[rst-cal-grid]").getBoundingClientRect(), cs = getComputedStyle(cal);
  return JSON.stringify({Open: true, Position: cs.position, InlineSize: cs.inlineSize, Left: r.left, Top: r.top, Right: r.right, Bottom: r.bottom, H: r.height,
    VW: document.documentElement.clientWidth, VH: innerHeight, GridW: g.width, ScrollH: cal.scrollHeight, ClientH: cal.clientHeight,
    Gutter: cal.offsetWidth - cal.clientWidth - parseFloat(cs.borderLeftWidth) - parseFloat(cs.borderRightWidth),
    Days: cal.querySelectorAll("[rst-cal-day]").length, Navs: cal.querySelectorAll("[rst-cal-nav]").length,
    MinDayW: minW, MinDayH: minH, Unhittable: unhittable});
})()`

type calReading struct {
	Open                                      bool
	Position, InlineSize                      string
	Left, Top, Right, Bottom, H, VW, VH       float64
	GridW, ScrollH, ClientH, MinDayW, MinDayH float64
	Gutter                                    float64
	Days, Navs                                int
	Unhittable                                []string
}

// openCalendar loads url at w×h, presses the first date field's
// calendar button (through the page, which is what a tap does) and
// reads the panel.
func openCalendar(t *testing.T, ctx context.Context, url string, w, h int64) calReading {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(w, h), chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, `!!document.querySelector("[rst-dtp-pick]")`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("[rst-dtp-pick]").click(), true`, nil)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, `document.querySelectorAll("[rst-cal] [rst-cal-day]").length === 42`)
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(calJS, &raw)); err != nil {
		t.Fatal(err)
	}
	var c calReading
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("reading the calendar (%q): %v", raw, err)
	}
	if !c.Open || c.Days != 42 || c.Navs != 2 {
		t.Fatalf("the calendar is open=%v with %d days and %d month buttons, want six weeks and two buttons", c.Open, c.Days, c.Navs)
	}
	return c
}

func inViewport(c calReading) bool {
	return c.Left >= -0.5 && c.Top >= -0.5 && c.Right <= c.VW+0.5 && c.Bottom <= c.VH+0.5
}

// TestTheCalendarDocksAndItsDaysAreTaps is §10.1's calendar: docked
// inside the viewport with 44×44 days at 390; no taller than a
// landscape phone and scrolling itself at 640×320, with every day and
// both month buttons reachable; with classic scrollbars at 600×320 and
// 390×320, mouse and touch, still scrolling, days still 44 wide, the
// grid exactly seven taps (the gutter widens the panel, never narrows a
// day); at 320 the deliberate exception (days ≥24 wide, 44 tall, no
// overflow); and with the field at the inline end of a field row, still
// inside the viewport.
func TestTheCalendarDocksAndItsDaysAreTaps(t *testing.T) {
	field := render(t, "field-date", map[string]any{"Name": "due", "Label": "Due", "Value": "2026-08-28"})
	pages := map[string]string{
		"/":    sizingDoc("calendar", `<div rst-page>`+field+`</div>`),
		"/end": sizingDoc("calendar at the end of a row", `<div rst-page><div rst-field-row><div rst-field class="rst-grow"><label rst-field-label for="n">Name</label><input rst-input id="n" name="n"></div><div>`+field+`</div></div></div>`),
	}
	touch := sizingRig(t, true, pages)
	ctx, cancel := context.WithTimeout(touch.Context(), 180*time.Second)
	defer cancel()

	c := openCalendar(t, ctx, touch.Origin+"/", 390, 844)
	requirePointer(t, ctx, true)
	if c.Position != "fixed" || !inViewport(c) {
		t.Errorf("390 touch: the calendar is %s at %.0f..%.0f × %.0f..%.0f in a %.0f×%.0f viewport; it is not docked inside it", c.Position, c.Left, c.Right, c.Top, c.Bottom, c.VW, c.VH)
	}
	if c.MinDayW < 43.5 || c.MinDayH < 43.5 {
		t.Errorf("390 touch: the smallest day is %.1f×%.1f, want 44×44", c.MinDayW, c.MinDayH)
	}
	if math.Abs(c.GridW-308) > 0.5 {
		t.Errorf("390 touch: the grid is %.1fpx, want seven taps (308)", c.GridW)
	}

	// The pointer half of the query alone: a tablet wider than 40rem
	// docks too (spec §1.4), so a docking rule scoped to width only
	// would leave it the anchored 18rem panel with 41px days.
	c = openCalendar(t, ctx, touch.Origin+"/", 1024, 768)
	if c.Position != "fixed" || !inViewport(c) {
		t.Errorf("1024 touch: the calendar is %s at %.0f..%.0f × %.0f..%.0f in a %.0f×%.0f viewport; it is not docked inside it", c.Position, c.Left, c.Right, c.Top, c.Bottom, c.VW, c.VH)
	}
	if c.MinDayW < 43.5 || c.MinDayH < 43.5 {
		t.Errorf("1024 touch: the smallest day is %.1f×%.1f, want 44×44", c.MinDayW, c.MinDayH)
	}
	if math.Abs(c.GridW-308) > 0.5 {
		t.Errorf("1024 touch: the grid is %.1fpx, want seven taps (308)", c.GridW)
	}

	c = openCalendar(t, ctx, touch.Origin+"/", 640, 320)
	if c.H > c.VH+0.5 || c.ScrollH <= c.ClientH || len(c.Unhittable) > 0 {
		t.Errorf("640x320 touch: panel %.0fpx in a %.0fpx viewport, scrolls=%v, unreachable %v", c.H, c.VH, c.ScrollH > c.ClientH, c.Unhittable)
	}

	c = openCalendar(t, ctx, touch.Origin+"/", 320, 640)
	// 40, not 24: 24 is WCAG 2.5.8's floor, but the deliberate
	// exception is the geometry of a clamped panel, about 41px
	// days, and a regression to anything narrower is a change to it.
	// That is a phone's geometry, whose overlay scrollbar takes no
	// width. Headless Chromium hides scrollbars without making them
	// overlay ones, so scrollbar-gutter: stable still reserves a classic
	// 15px here and takes a seventh of it from each day (38.7px); the
	// gutter is measured and given back, so the floor is the phone's.
	if phone := c.MinDayW + c.Gutter/7; phone < 40 || c.MinDayH < 43.5 || c.Right > c.VW+0.5 || c.Left < -0.5 {
		t.Errorf("320 touch: days %.1f×%.1f (%.1f wide without the %.0fpx gutter), panel %.0f..%.0f in %.0f; the approved exception is about 41 wide, 44 tall, no overflow", c.MinDayW, c.MinDayH, phone, c.Gutter, c.Left, c.Right, c.VW)
	}

	c = openCalendar(t, ctx, touch.Origin+"/end", 390, 844)
	if !inViewport(c) {
		t.Errorf("390 touch, field at the row's end: the calendar is at %.0f..%.0f in %.0f", c.Left, c.Right, c.VW)
	}

	// Classic scrollbars, which the harness hides by default: the case
	// where the gutter once came out of the days.
	for _, coarse := range []bool{false, true} {
		rig := sizingRig(t, coarse, pages, harness.WithScrollbars())
		sctx, scancel := context.WithTimeout(rig.Context(), 90*time.Second)
		// The days alone cannot tell whether the gutter is reserved:
		// Chromium already counts a scrollbar it draws in a fit-content
		// box's width, so they stay 44 either way. What the reservation
		// adds is a panel that keeps one width whether or not it scrolls,
		// so a month that fits a tall viewport is measured as the control
		// and every short viewport must match it.
		tall := openCalendar(t, sctx, rig.Origin+"/", 390, 844)
		if tall.ScrollH > tall.ClientH {
			t.Errorf("390x844 classic scrollbars, coarse=%v: the panel scrolls (%.0f > %.0f), so it cannot be the width of a panel that does not", coarse, tall.ScrollH, tall.ClientH)
		}
		for _, vp := range [][2]int64{{600, 320}, {390, 320}} {
			c := openCalendar(t, sctx, rig.Origin+"/", vp[0], vp[1])
			requirePointer(t, sctx, coarse)
			where := fmt.Sprintf("%dx%d classic scrollbars, coarse=%v", vp[0], vp[1], coarse)
			if w, tw := c.Right-c.Left, tall.Right-tall.Left; math.Abs(w-tw) > 0.5 {
				t.Errorf("%s: the scrolling panel is %.1fpx and the same month at 390x844 is %.1fpx; the gutter is not reserved, so the panel jumps when the viewport gets short", where, w, tw)
			}
			if c.ScrollH <= c.ClientH {
				t.Errorf("%s: the panel is not scrolling (%.0f ≤ %.0f); the gutter case this leg is for has not arisen", where, c.ScrollH, c.ClientH)
			}
			if c.MinDayW < 43.5 {
				t.Errorf("%s: a day is %.1fpx wide; the scrollbar's gutter came out of the days", where, c.MinDayW)
			}
			if c.MinDayH < 43.5 {
				t.Errorf("%s: a day is %.1fpx tall, want 44", where, c.MinDayH)
			}
			if math.Abs(c.GridW-308) > 0.5 {
				t.Errorf("%s: the grid is %.1fpx, want exactly seven taps (308)", where, c.GridW)
			}
			if !inViewport(c) {
				t.Errorf("%s: the panel is at %.0f..%.0f × %.0f..%.0f, outside the viewport", where, c.Left, c.Right, c.Top, c.Bottom)
			}
		}
		scancel()
	}
}
