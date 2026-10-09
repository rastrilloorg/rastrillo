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
// deliberate desktop changes, pinned at their new values: the whole-row
// target (§1.5), which TestTheWholeRowIsTheTarget holds, and the 32px
// desktop floor with its 16px icons and 12px carets (2026-10-07), which
// lifted the small button, the kebab and the row checkbox's label to 32.
// Desktop targets had ranged from 24 to 36px, sized one control at a
// time; the floor is one number for all of them.
func TestDesktopDensityIsPinned(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("sizing", sizingFixture(t)), "/modal": sizingDoc("modal", Styleguide()["modal"])})
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
	    Box: box('[data-sample="selbox"] [rst-selbox] input'),
	    Chip: box([...document.querySelectorAll("[rst-pagination] a")].filter(a => /^\d$/.test(a.textContent.trim())).map(a => "#" + (a.id || (a.id = "sizing-chip")))[0])});
	})()`, &raw)); err != nil {
		t.Fatal(err)
	}
	var d struct {
		Sm, Md, Lg        float64
		Kebab, Label, Box [2]float64
		Chip              [2]float64
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	// Measured before the touch rules existed: 27.69, 33.88 and 43.97
	// (the "about 28/34/44" of tokens.css's header), a 26×26 kebab, a
	// 16px checkbox in a 24×24 label. The floor lifts the small button
	// to 32 and leaves the two that were already over it alone.
	if math.Round(d.Sm) != 32 || math.Round(d.Md) != 34 || math.Round(d.Lg) != 44 {
		t.Errorf("desktop button heights %v/%v/%v, want 32/34/44", d.Sm, d.Md, d.Lg)
	}
	if d.Kebab != [2]float64{32, 32} {
		t.Errorf("desktop kebab %v, want 32×32", d.Kebab)
	}
	if d.Box != [2]float64{16, 16} || d.Label != [2]float64{32, 32} {
		t.Errorf("desktop row checkbox %v in a label %v, want the 16px box in a 32×32 target", d.Box, d.Label)
	}
	// A one-digit page chip was 53.19px wide before the floor reached the
	// desktop (its 2rem minimum on the content box, with its padding and
	// border on top). The floor's border-box made it 32; the floor
	// raises the height to 32 and must not narrow it.
	if d.Chip != [2]float64{53.19, 32} {
		t.Errorf("desktop page chip %v, want 53.2×32: the old width, at the floor's height", d.Chip)
	}
	assertIcons(t, ctx, "1280 mouse")
	// The floor is a minimum on the whole box, padding and border
	// included. Measured on the content box it added the padding on top:
	// a dropdown summary already 33.8px tall grew to 44.8, the topbar
	// with it to 61.8, and the bulk bar's Actions to 41. These are exact,
	// because a floor that only checks "at least 32" cannot see growth.
	var rawFloor string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
	  const h = sel => Math.round(document.querySelector(sel).getBoundingClientRect().height * 10) / 10;
	  const group = document.querySelector('[data-sample="dropdown"] [rst-menu-group] > summary');
	  for (let d = group.closest("details"); d; d = d.parentElement && d.parentElement.closest("details")) d.open = true;
	  const out = {Summary: h('[data-sample="dropdown"] [rst-dropdown] > summary'), Bulk: h('[data-partial="bulk-bar"] [rst-dropdown] > summary'),
	    Group: Math.round(group.getBoundingClientRect().height * 10) / 10, Help: h('[data-sample="help"] [rst-help]'),
	    Bar: h('[data-sample="shell-topbar"] [rst-shell-bar]'),
	    Search: h('[rst-search]:has([rst-search-clear])'), SearchInput: h('[rst-search]:has([rst-search-clear]) input[type="search"]'),
	    BulkSearch: h('#sizing-bulk [rst-search]')};
	  document.querySelectorAll("details[open]").forEach(d => { d.open = false; });
	  return JSON.stringify(out);
	})()`, &rawFloor)); err != nil {
		t.Fatal(err)
	}
	var f struct{ Summary, Bulk, Group, Help, Bar, ModalNav, Search, SearchInput, BulkSearch float64 }
	if err := json.Unmarshal([]byte(rawFloor), &f); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+"/modal"), chromedp.WaitReady("body"),
		chromedp.Evaluate(`Math.round(document.querySelector("[rst-modal-panel] > nav a").getBoundingClientRect().height * 10) / 10`, &f.ModalNav)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"a dropdown summary (its own padding, over the floor)", f.Summary, 33.8},
		{"the bulk bar's Actions summary", f.Bulk, 32},
		{"a menu-group summary", f.Group, 32},
		{"the help button, border included", f.Help, 32},
		{"the topbar's bar", f.Bar, 50.8},
		{"a modal nav link (its own padding, over the floor)", f.ModalNav, 33.8},
		// Before the floor reached the desktop the search box was 35.6
		// (a 33 without its clear link) around a 21.4px input, under the
		// floor. Its input and clear link are the targets, and at 32 with
		// the box's own 0.3rem block padding on top the box was 43.6 and
		// every list bar grew with it. The padding goes and the box is
		// the floor plus its border; the input fills it.
		{"a search box with its clear link", f.Search, 34},
		{"a search box's input", f.SearchInput, 32},
		{"the bulk bar's search box", f.BulkSearch, 34},
	} {
		if c.got != c.want {
			t.Errorf("desktop %s is %vpx tall, want %v", c.name, c.got, c.want)
		}
	}
	c := openCalendar(t, ctx, rig.Origin+"/", 1280, 900)
	if c.Position != "absolute" || c.InlineSize != "288px" {
		t.Errorf("desktop calendar is %s, inline-size %s; want today's anchored 18rem (288px) panel", c.Position, c.InlineSize)
	}
	// The calendar is outside the target drive (it opens by script), so
	// its controls are held to the desktop floor here.
	if c.MinDayW < 31.5 || c.MinDayH < 31.5 || c.MinNavW < 31.5 || c.MinNavH < 31.5 {
		t.Errorf("desktop calendar: smallest day %.1f×%.1f, month button %.1f×%.1f; want both at least 32×32", c.MinDayW, c.MinDayH, c.MinNavW, c.MinNavH)
	}
}

// TestAnIconKeepsItsSpaceBesideItsText: a menu item (a link or a
// button, in both spellings: a bulk action is a button), a nav link, the
// brand, the back link, a row action and the sign-in link are flex boxes
// on a phone, and the ones born inline are on a desktop too, so the
// floor reaches them. A flex box drops the whitespace between an icon
// and the word after it, and the icon touched the word (4px of space
// became 0); each sets a gap, and a control that stays a block keeps its
// space. This reads the space between the icon's edge and the word's,
// with a mouse at 1280 and a coarse pointer at 390, in both directions
// of text.
func TestAnIconKeepsItsSpaceBesideItsText(t *testing.T) {
	ids := []string{"icon-item", "icon-button", "icon-button-class", "icon-nav", "icon-brand", "icon-back", "icon-action", "icon-signin"}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280x900 mouse", 1280, 900, false}, {"390x844 touch", 390, 844, true}} {
		rig := sizingRig(t, leg.coarse, map[string]string{"/": sizingDoc("sizing", sizingFixture(t))})
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body")); err != nil {
			cancel()
			t.Fatal(err)
		}
		requirePointer(t, ctx, leg.coarse)
		for _, dir := range []string{"ltr", "rtl"} {
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
			  document.documentElement.dir = %q;
			  document.querySelectorAll('[data-extra="icon-text"] details').forEach(d => { d.open = true; });
			  const out = {};
			  for (const id of %s) {
			    const a = document.getElementById(id);
			    if (!a.checkVisibility()) continue;
			    const i = a.querySelector("svg").getBoundingClientRect();
			    // From the word's first letter: in a block the space is
			    // the start of the text node, and is the gap itself.
			    const r = document.createRange(), w = a.lastChild; r.setStart(w, w.textContent.search(/\S/)); r.setEnd(w, w.textContent.length);
			    const tx = r.getBoundingClientRect();
			    out[id] = document.documentElement.dir === "rtl" ? i.left - tx.right : tx.left - i.right;
			  }
			  document.querySelectorAll('[data-extra="icon-text"] details').forEach(d => { d.open = false; });
			  return JSON.stringify(out);
			})()`, dir, "[\""+strings.Join(ids, "\",\"")+"\"]"), &raw)); err != nil {
				cancel()
				t.Fatal(err)
			}
			var got map[string]float64
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				cancel()
				t.Fatal(err)
			}
			// The rail is not drawn on a phone; every other control is.
			if want := len(ids) - map[bool]int{true: 1, false: 0}[leg.coarse]; len(got) < want {
				t.Errorf("%s, %s: measured %d of the icon-and-text controls, want %d: %v", leg.name, dir, len(got), want, got)
			}
			for id, gap := range got {
				if gap < 3 {
					t.Errorf("%s, %s: in #%s the icon is %.1fpx from its text; it touches the word", leg.name, dir, id, gap)
				}
			}
		}
		cancel()
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

// assertTargets holds every measured control to 44×44, the floor on a
// small or touch screen.
func assertTargets(t *testing.T, where string, got []targetReading) {
	t.Helper()
	assertTargetsAt(t, where, got, 44)
}

// assertTargetsAt holds every measured control to floor×floor, whole in
// the viewport and hittable at its centre. A link in running text is the
// one exemption (WCAG 2.5.8's inline exception). The floor is 44 on a
// small or touch screen and 32 on a desktop with a mouse.
func assertTargetsAt(t *testing.T, where string, got []targetReading, floor float64) {
	t.Helper()
	for _, g := range got {
		switch {
		case !g.Fits:
			t.Errorf("%s: %s is %.1f×%.1f and cannot be brought wholly into the viewport; too big to hit", where, g.Name, g.W, g.H)
		case !g.Owns:
			t.Errorf("%s: %s is occluded: a tap at its centre lands on %s", where, g.Name, g.Hit)
		case g.Inline:
		case g.W < floor-0.5 || g.H < floor-0.5:
			t.Errorf("%s: %s is %.1f×%.1f, under the %v×%v floor", where, g.Name, g.W, g.H, floor, floor)
		}
	}
}

// measureEverything reads the closed page, then every overlay alone,
// then the combobox's list and the date field's list, each opened by
// the script that owns it, holding each control to floor.
func measureEverything(t *testing.T, ctx context.Context, where string, floor float64) int {
	t.Helper()
	settleUntil(t, ctx, `!!document.querySelector("[rst-combo] [role=combobox]") && !!document.querySelector("[rst-dtp-pick]")`)
	got := readTargets(t, ctx, targetsJS)
	n := len(got)
	assertTargetsAt(t, where+", overlays closed", got, floor)
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
		assertTargetsAt(t, fmt.Sprintf("%s, overlay %d open", where, i), o.Targets, floor)
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
		assertTargetsAt(t, where+", "+open.name, got, floor)
		chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape))
	}
	return n
}

// iconsJS reads every rendered icon inside a control on the page as
// it loads: rastrillo.Icon's svg.icon, sized by the control and not by
// the text around it, and whether it sits in a caret.
const iconsJS = `JSON.stringify([...document.querySelectorAll(":is(a[href], button, summary, label, [role=button]) svg.icon")]
  .filter(i => i.checkVisibility({visibilityProperty: true}))
  .map(i => { const r = i.getBoundingClientRect(), c = i.closest("a[href], button, summary, label, [role=button]");
    return {Name: c.tagName.toLowerCase() + [...c.attributes].filter(a => a.name.startsWith("rst-")).map(a => "[" + a.name + "]").join("") + " “" + (c.getAttribute("aria-label") || c.textContent).trim().slice(0, 30) + "”",
      Caret: !!i.closest("[rst-caret], .rst-caret"), W: Math.round(r.width * 100) / 100, H: Math.round(r.height * 100) / 100}; }))`

// assertIcons holds every icon inside a control to 16×16 and a caret's
// chevron to 12×12. An icon sized from its font came out 9 to 20px
// depending on the text beside it (a caret in an xs summary, a menu
// icon in a 1.25rem button), so two icons in one toolbar disagreed. At
// least one of each kind must be read, or a fixture that lost its
// carets would pass with nothing measured.
func assertIcons(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(iconsJS, &raw)); err != nil {
		t.Fatalf("%s: reading icons: %v", where, err)
	}
	var got []struct {
		Name  string
		Caret bool
		W, H  float64
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	carets, icons := 0, 0
	for _, g := range got {
		want := 16.0
		if g.Caret {
			want, carets = 12, carets+1
		} else {
			icons++
		}
		if g.W != want || g.H != want {
			t.Errorf("%s: the icon in %s is %v×%v, want %v×%v", where, g.Name, g.W, g.H, want, want)
		}
	}
	if carets == 0 || icons == 0 {
		t.Errorf("%s: read %d caret icons and %d other icons in controls; the fixture must show at least one of each", where, carets, icons)
	}
}

// TestAnIconOutsideAControlFollowsItsText pins where the fixed icon
// size stops. Inside a control an icon is 16px whatever the text beside
// it (assertIcons); everywhere else it is 1em and follows its text, as
// it always did: an icon in a 24px heading is 24px, one in a line of
// prose or in a link in that line is the line's size. A 1rem .icon
// made every one of them 16px, shrinking heading icons and growing
// small print's. The button beside them is the control: 16px.
func TestAnIconOutsideAControlFollowsItsText(t *testing.T) {
	page := map[string]string{"/": sizingDoc("icons", `<div rst-page>`+
		`<h2 id="heading" style="font-size: 24px">`+sizingIcon+` Orders</h2>`+
		`<p id="prose" style="font-size: 12px">`+sizingIcon+` Shipped. <a href="#guide" id="prose-link">`+sizingIcon+` Read the guide</a></p>`+
		`<button rst-btn type="button" id="button">`+sizingIcon+` Save</button></div>`)}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280x900 mouse", 1280, 900, false}, {"390x844 touch", 390, 844, true}} {
		rig := sizingRig(t, leg.coarse, page)
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		var got map[string]float64
		var raw string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("#button", chromedp.ByQuery),
			chromedp.Evaluate(`JSON.stringify(Object.fromEntries(["heading", "prose", "prose-link", "button"].map(id => [id, document.querySelector("#" + id + " > svg").getBoundingClientRect().height])))`, &raw)); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]float64{"heading": 24, "prose": 12, "prose-link": 12, "button": 16} {
			if got[id] != want {
				t.Errorf("%s: the icon in #%s is %vpx, want %v", leg.name, id, got[id], want)
			}
		}
	}
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
	// Previous and 1 are spans (disabled, current); 9 and Next are links
	// at every width. 2, beside the current page, is a link only a wide
	// strip shows, so it is measured where it is drawn and not counted.
	{`[data-partial="pagination"] [rst-pagination] a:not([rst-pagination-wide])`, 2},
	{`[data-partial="bulk-bar"] [rst-dropdown-menu] button`, 2},
	{`[data-partial="seg-tabs"] a`, 2},
	{`[data-partial="dropdown"] [rst-dropdown-menu] a`, 1},
	{`[data-extra="short-labels"] [rst-bulkbar-escalate]`, 1},
	{`[data-extra="short-labels"] a[rst-person]`, 1},
	{`[data-extra="signin-providers"] [rst-signin-providers] > a`, 1},
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
// on a page of its own. The 1280 mouse leg is the desktop floor: the
// same drive, every control at least 32×32, so a control that is only
// floored on touch (a 26px kebab, a 24px clear link) fails there. Every
// leg also holds the icons inside controls to 16px and a caret's to 12.
func TestEveryTapTargetIsAtLeast44Pixels(t *testing.T) {
	pages := map[string]string{
		"/":      sizingDoc("sizing", sizingFixture(t)),
		"/modal": sizingDoc("modal", Styleguide()["modal"]),
	}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
		floor  float64
	}{
		{"390x844 touch", 390, 844, true, 44},
		{"1024x768 touch", 1024, 768, true, 44},
		{"600x800 mouse", 600, 800, false, 44},
		{"1280x900 mouse", 1280, 900, false, 32},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/")); err != nil {
				t.Fatal(err)
			}
			requirePointer(t, ctx, leg.coarse)
			assertIcons(t, ctx, leg.name)
			n := measureEverything(t, ctx, leg.name, leg.floor)
			t.Logf("%s: %d controls measured", leg.name, n)
			assertCovered(t, ctx, leg.name, "", leg.w >= 800)
			assertFixtureCounts(t, ctx, leg.name)
			if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+"/modal")); err != nil {
				t.Fatal(err)
			}
			got := readTargets(t, ctx, targetsJS)
			assertTargetsAt(t, leg.name+", the modal", got, leg.floor)
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
  let minW = 1e9, minH = 1e9, navW = 1e9, navH = 1e9; const unhittable = [];
  for (const el of items) {
    el.scrollIntoView({block: "nearest", inline: "nearest"});
    const b = el.getBoundingClientRect();
    if (el.matches("[rst-cal-day]")) { minW = Math.min(minW, b.width); minH = Math.min(minH, b.height); }
    else { navW = Math.min(navW, b.width); navH = Math.min(navH, b.height); }
    const hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    if (!hit || !(hit === el || el.contains(hit))) unhittable.push((el.getAttribute("data-rst-day") || "nav") + " -> " + (hit ? hit.tagName : "nothing"));
  }
  cal.scrollTop = 0;
  const r = cal.getBoundingClientRect(), g = cal.querySelector("[rst-cal-grid]").getBoundingClientRect(), cs = getComputedStyle(cal);
  return JSON.stringify({Open: true, Position: cs.position, InlineSize: cs.inlineSize, Left: r.left, Top: r.top, Right: r.right, Bottom: r.bottom, H: r.height,
    VW: document.documentElement.clientWidth, VH: innerHeight, GridW: g.width, ScrollH: cal.scrollHeight, ClientH: cal.clientHeight,
    Gutter: cal.offsetWidth - cal.clientWidth - parseFloat(cs.borderLeftWidth) - parseFloat(cs.borderRightWidth),
    Days: cal.querySelectorAll("[rst-cal-day]").length, Navs: cal.querySelectorAll("[rst-cal-nav]").length,
    MinDayW: minW, MinDayH: minH, MinNavW: navW, MinNavH: navH, Unhittable: unhittable});
})()`

type calReading struct {
	Open                                      bool
	Position, InlineSize                      string
	Left, Top, Right, Bottom, H, VW, VH       float64
	GridW, ScrollH, ClientH, MinDayW, MinDayH float64
	MinNavW, MinNavH, Gutter                  float64
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

// floorOnly are the layout declarations the floor block may carry
// besides the floor, each because the floor does not work without it,
// keyed by the attribute spelling of the control that carries them.
// An <a> is inline, and min sizes do nothing on an inline box, so the
// controls born inline become inline-flex, centred in the box the floor
// grows (or, blockified by a flex or grid parent, flex). The search
// box's input is its target, so the box gives up its block padding and
// the input stretches to the floor; the bulk bar's close button would
// shrink below the floor in its flex row; the row checkbox centres its
// 16px box in the label the floor widens.
var floorOnly = map[string]string{
	"[rst-row-action]":           "display align-items justify-content",
	"[rst-bulkbar-escalate]":     "display align-items justify-content",
	"[rst-modal-close]":          "display align-items justify-content",
	"[rst-back-nav] a":           "display align-items",
	"[rst-shell-brand]":          "display align-items",
	"[rst-search]":               "padding-top padding-bottom",
	"[rst-search] input":         "align-self",
	"[rst-bulkbar-close]":        "flex-shrink",
	"[rst-selbox]":               "justify-content",
	"[rst-signin-providers] > a": "display align-items align-self",
}

// mayShrink are the controls the floor makes smaller on a desktop, by
// selector as the floor block spells it, each with why.
var mayShrink = map[string]string{
	"[rst-search]":                      "the box gives up its block padding so its input, the target, can be the floor: 35.6px became 34",
	`[rst-search] input[type="search"]`: "it stretches to the box's height, and its width loses what the box's border-box floor takes",
}

// TestTheFloorChangesNothingButSizeOnADesktop: the floor block reaches
// every width, and the layout it carried from its touch-only days came
// with it: a row action 9.6px wider than the floor needed, a date
// suggestion's label moved off its baseline, a full-width button's
// label pulled to its middle, menu items, tabs and nav links made flex
// boxes that drop the spaces in their labels. Every control the block
// names is read at 1280 with a mouse, once as served and once with the
// block cut out; the two may differ in size, box-sizing and
// align-content (which centres a block's content in the floor), and in
// floorOnly's declarations, and in nothing else. And the size may only
// grow: a floor once replaced the textarea's own 5rem minimum (one
// property under two names), and a note field shrank to two lines.
func TestTheFloorChangesNothingButSizeOnADesktop(t *testing.T) {
	without, block := floorBlock(t)
	var sels []string
	for _, r := range leafRules(stripCSSComments(block)) {
		for _, s := range splitSelectorList(r.selector) {
			if !strings.HasPrefix(s, ":where(") {
				sels = append(sels, s)
			}
		}
	}
	body := sizingFixture(t) + `<div rst-page data-extra="floor-layout"><div rst-dtp-row id="dtp-row"><span>Tomorrow</span><span>Thu 9 Oct</span></div>` +
		`<p><a rst-btn href="#wide" style="display: flex">Continue</a></p></div>`
	pages := map[string]string{"/": sizingDoc("floor", body), "/bare": strings.Replace(sizingDoc("floor", body), "/tokens.css", "/tokens-nofloor.css", 1)}
	rig := harness.New(t, func(string) http.Handler {
		mux := sizingMux(t, pages)
		mux.HandleFunc("GET /tokens-nofloor.css", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, without)
		})
		return mux
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	read := `(() => {
	  const sels = ` + mustJSON(t, sels) + `, props = ` + mustJSON(t, strings.Fields("display align-items justify-content padding-top padding-right padding-bottom padding-left align-self flex-shrink flex-grow row-gap column-gap text-align vertical-align")) + `;
	  const seen = new Map();
	  for (const sel of sels) for (const e of document.querySelectorAll(sel)) if (!seen.has(e)) seen.set(e, sel);
	  const all = [...document.querySelectorAll("*")];
	  return JSON.stringify([...seen].map(([e, sel]) => {
	    const cs = getComputedStyle(e), out = {I: all.indexOf(e), Sel: sel, Name: e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") + " “" + (e.getAttribute("aria-label") || e.textContent).trim().slice(0, 24) + "”", P: {}};
	    for (const p of props) out.P[p] = cs.getPropertyValue(p);
	    const box = e.getBoundingClientRect(); out.W = box.width; out.H = box.height;
	    return out;
	  }));
	})()`
	type styled struct {
		I         int
		Sel, Name string
		P         map[string]string
		W, H      float64
	}
	at := func(path string) map[int]styled {
		var raw string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("#dtp-row", chromedp.ByQuery), chromedp.Evaluate(read, &raw)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var got []styled
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		out := map[int]styled{}
		for _, g := range got {
			out[g.I] = g
		}
		return out
	}
	served, bare := at("/"), at("/bare")
	if len(served) < 50 {
		t.Fatalf("read %d controls the floor block names; the selectors no longer match the fixture", len(served))
	}
	for i, s := range served {
		b, ok := bare[i]
		if !ok {
			t.Errorf("%s: not on the page without the floor block", s.Name)
			continue
		}
		if _, ok := mayShrink[s.Sel]; !ok && (s.W < b.W-1 || s.H < b.H-1) {
			t.Errorf("%s (%s): the floor shrinks it on a desktop from %.1f×%.1f to %.1f×%.1f; a floor only raises", s.Name, s.Sel, b.W, b.H, s.W, s.H)
		}
		may := ""
		for key, props := range floorOnly {
			if strings.Contains(s.Sel, key) {
				may += " " + props
			}
		}
		// A gap does nothing outside a flex or grid box; the one that
		// puts back an icon's space applies where the floor made one,
		// which is floorOnly's to allow.
		if d := s.P["display"]; !strings.Contains(d, "flex") && !strings.Contains(d, "grid") || !strings.Contains(b.P["display"], "flex") {
			may += " column-gap row-gap"
		}
		for p, v := range s.P {
			if v != b.P[p] && !strings.Contains(" "+may+" ", " "+p+" ") {
				t.Errorf("%s (%s): the floor changes %s on a desktop from %q to %q", s.Name, s.Sel, p, b.P[p], v)
			}
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
