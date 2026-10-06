//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// boxOf says whether the first element matching sel takes up room.
const boxOf = `(sel => { const e = document.querySelector(sel); if (!e) return false; const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0; })`

// focusID names whatever has focus the way the expected sequences below
// are written: an input or a row by id, a link by its href, a scheme
// button by its value, a summary by its tag.
const focusID = `(() => { const a = document.activeElement;
  if (!a || a === document.body) return "body";
  if (a.id) return "#" + a.id;
  if (a.dataset && a.dataset.dsScheme) return "scheme:" + a.dataset.dsScheme;
  if (a.tagName === "A") return a.getAttribute("href");
  return a.tagName.toLowerCase(); })()`

func TestThePhoneIndexAndTheWayBack(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, locale := range []string{"en", "ar"} {
		where := "day/" + locale + " at 390px"
		index := rig.Origin + indexHref(mountPath, "day", locale)
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(index), chromedp.WaitVisible(`.ds-index`, chromedp.ByQuery)); err != nil {
			t.Fatalf("%s: loading the index: %v", where, err)
		}
		requireCoarse(t, ctx)

		// The Overview is the index: the rail is the page, main and the
		// bar have no box, the one visible h1 is the index title, and the
		// rows are the page kinds in table order, then Demos.
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		  const box = `+boxOf+`;
		  const h1s = [...document.querySelectorAll("h1")].filter(h => h.getBoundingClientRect().height > 0).map(h => h.textContent.trim());
		  const rows = [...document.querySelectorAll(".ds-index > a")].map(a => ({T: a.textContent.trim(), H: a.getBoundingClientRect().height, ID: a.id}));
		  return JSON.stringify({Rail: box("[rst-shell-rail]"), Main: box("main"), Bar: box(".ds-top"), Lead: box(".ds-index-lead"), H1: h1s,
		    Group: (document.querySelector(".ds-index > [rst-shell-group]") || {}).textContent || "", Rows: rows});
		})()`, &raw)); err != nil {
			t.Fatalf("%s: reading the index: %v", where, err)
		}
		var ix struct {
			Rail, Main, Bar, Lead bool
			H1                    []string
			Group                 string
			Rows                  []struct {
				T, ID string
				H     float64
			}
		}
		if err := json.Unmarshal([]byte(raw), &ix); err != nil {
			t.Fatalf("%s: decoding %q: %v", where, raw, err)
		}
		if !ix.Rail || ix.Main || ix.Bar || !ix.Lead {
			t.Errorf("%s: rail %v, main %v, bar %v, lead %v; the index is the rail and its lead alone", where, ix.Rail, ix.Main, ix.Bar, ix.Lead)
		}
		if len(ix.H1) != 1 || ix.H1[0] != proseIn(locale, "rastrillo design system") {
			t.Errorf("%s: visible h1s %q, want exactly the index title", where, ix.H1)
		}
		var want []string
		for _, pk := range pageKinds() {
			if pk.Kind != "overview" {
				want = append(want, proseIn(locale, pk.Title))
			}
		}
		if ix.Group != proseIn(locale, "Demos") {
			t.Errorf("%s: the rows' group label is %q, want Demos", where, ix.Group)
		}
		if len(ix.Rows) < len(want) {
			t.Fatalf("%s: %d rows, want at least %d", where, len(ix.Rows), len(want))
		}
		for i, w := range want {
			if ix.Rows[i].T != w {
				t.Errorf("%s: row %d is %q, want %q", where, i, ix.Rows[i].T, w)
			}
		}
		for _, r := range ix.Rows {
			if r.H < 44 {
				t.Errorf("%s: the row %q is %.1fpx tall, under a 44px target", where, r.T, r.H)
			}
		}

		// Tab runs the display settings button in the header row, then
		// the filter and the rows, in screen order. The controls are in
		// the menu, closed, so nothing follows the rows.
		var seq []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement && document.activeElement.blur(); scrollTo(0, 0); true`, nil)); err != nil {
			t.Fatalf("%s: resetting focus: %v", where, err)
		}
		var expect []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`["summary", "#ds-filter", ...[...document.querySelectorAll(".ds-index > a")].map(a => a.id ? "#" + a.id : a.getAttribute("href"))]`, &expect)); err != nil {
			t.Fatalf("%s: listing the expected order: %v", where, err)
		}
		for range expect {
			var at string
			if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(focusID, &at)); err != nil {
				t.Fatalf("%s: tabbing: %v", where, err)
			}
			seq = append(seq, at)
		}
		if strings.Join(seq, " ") != strings.Join(expect, " ") {
			t.Errorf("%s: Tab order\n got %v\nwant %v", where, seq, expect)
		}
		// The filter: the search icon inside the field at its inline
		// start, and a gap of at least 8px between the field and the
		// first row.
		var filter struct {
			IconIn bool
			Gap    float64
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const i = document.getElementById("ds-filter").getBoundingClientRect(), g = document.querySelector(".ds-search > .icon").getBoundingClientRect();
		  const rtl = document.documentElement.dir === "rtl", row = document.querySelector(".ds-index > a").getBoundingClientRect();
		  const start = rtl ? i.right - g.right : g.left - i.left;
		  return JSON.stringify({IconIn: g.width > 0 && g.top >= i.top && g.bottom <= i.bottom && start >= 0 && start < 16, Gap: row.top - i.bottom}); })()`, &raw)); err != nil {
			t.Fatalf("%s: measuring the filter: %v", where, err)
		}
		if err := json.Unmarshal([]byte(raw), &filter); err != nil {
			t.Fatalf("%s: decoding %q: %v", where, raw, err)
		}
		if !filter.IconIn || filter.Gap < 8 {
			t.Errorf("%s: the search icon is inside the field at its inline start %v, and the field is %.1fpx above the first row; want the icon there and a gap of at least 8px", where, filter.IconIn, filter.Gap)
		}

		// A row opens its page as a content page: Back first after the
		// skip link, and no rail, bar or switcher with a box.
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("nav-form").click(); true`, nil)); err != nil {
			t.Fatalf("%s: tapping the Form row: %v", where, err)
		}
		until(t, ctx, where+", Form loaded", `location.pathname.endsWith("/form.html") && document.readyState === "complete"`)
		var page string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const box = `+boxOf+`;
		  const back = document.querySelector("[rst-skip] + [rst-shell-back] > a");
		  return JSON.stringify({Back: !!back && box("[rst-shell-back]"), Href: back ? back.getAttribute("href") : "",
		    Rail: box("[rst-shell-rail]"), Bar: box(".ds-top"),
		    Switch: [...document.querySelectorAll("[data-ds-scheme], [rst-seg-tabs] a, [rst-locale]")].some(e => e.checkVisibility() && e.getBoundingClientRect().width > 0)});
		})()`, &page)); err != nil {
			t.Fatalf("%s: reading Form: %v", where, err)
		}
		var cp struct {
			Back, Rail, Bar, Switch bool
			Href                    string
		}
		if err := json.Unmarshal([]byte(page), &cp); err != nil {
			t.Fatalf("%s: decoding %q: %v", where, page, err)
		}
		if !cp.Back || cp.Rail || cp.Bar || cp.Switch {
			t.Errorf("%s: Form shows back %v, rail %v, bar %v, a switcher %v; a content page is the way back and the content", where, cp.Back, cp.Rail, cp.Bar, cp.Switch)
		}
		if want := indexHref(mountPath, "day", locale) + "#nav-form"; cp.Href != want {
			t.Errorf("%s: Back goes to %q, want %q", where, cp.Href, want)
		}

		// Back returns to the index with the Form row focused. This is a
		// history traversal, so the leg polls and clicks through the page.
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("[rst-shell-back] a").click(); true`, nil)); err != nil {
			t.Fatalf("%s: going back: %v", where, err)
		}
		until(t, ctx, where+", back on the index", `location.pathname.endsWith("/index.html") && document.activeElement && document.activeElement.id === "nav-form"`)

		// A theme link in the display settings menu lands on the other
		// theme's index, with the menu's button on screen at its top.
		// A real tap, so first the end of Back's slide: a tap during a
		// view transition lands on its snapshot and does nothing.
		until(t, ctx, where+", the slide over", `document.readyState === "complete"`)
		settleMotion(t, ctx, where+", the slide over")
		// Taps at the elements' centres, read off the page: after a
		// history traversal chromedp's own node lookups wait on a document
		// it never re-read, so selector actions would wait forever.
		tapAt(t, ctx, where, ".ds-prefs > summary")
		until(t, ctx, where+", the menu open", `document.querySelector(".ds-prefs").open`)
		tapAt(t, ctx, where, `.ds-prefs [rst-seg-tabs] a[href*="/signal/"]`)
		until(t, ctx, where+", switched to signal", `location.pathname.includes("/signal/") && (() => { const r = document.querySelector(".ds-prefs > summary").getBoundingClientRect(); return r.height > 0 && r.top >= 0 && r.top < innerHeight; })()`)
	}

	// Scripts off: Back is the link, the row is the :target, and the
	// next Tab lands on the row after it.
	off := noScripts(t, ctx)
	where := "day/en at 390px, scripts off"
	if err := chromedp.Run(off, chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitVisible(`[rst-shell-back]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	requireScriptsOff(t, off, where)
	if err := chromedp.Run(off, chromedp.Evaluate(`document.querySelector("[rst-shell-back] a").click(); true`, nil)); err != nil {
		t.Fatalf("%s: going back: %v", where, err)
	}
	until(t, off, where, `location.hash === "#nav-form" && document.querySelector(":target") && document.querySelector(":target").id === "nav-form"`)
	requireScriptsOff(t, off, where+", back on the index")
	var next, after string
	if err := chromedp.Run(off,
		chromedp.Evaluate(`document.getElementById("nav-form").nextElementSibling.id || document.getElementById("nav-form").nextElementSibling.getAttribute("href")`, &after),
		chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(focusID, &next)); err != nil {
		t.Fatalf("%s: tabbing from the target: %v", where, err)
	}
	if strings.TrimPrefix(next, "#") != after {
		t.Errorf("%s: the Tab after Back lands on %s, want the row after Form, %s", where, next, after)
	}
}

// axe on both phone views, every theme and both schemes, motion
// settled first.
func TestA11yScansThePhoneViews(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	axeJS := axeSource(t)
	total := 0
	for _, theme := range ui.ThemeNames() {
		for _, view := range []struct{ name, file, ready string }{{"index", "index.html", ".ds-index"}, {"content page", "form.html", "[rst-shell-back]"}} {
			for _, scheme := range a11ySchemes {
				where := fmt.Sprintf("%s/en %s at 390px (%s)", theme, view.name, scheme)
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
					chromedp.Navigate(rig.Origin+pageHref(mountPath, theme, "en", view.file)),
					chromedp.WaitVisible(view.ready, chromedp.ByQuery)); err != nil {
					t.Fatalf("%s: loading: %v", where, err)
				}
				requireCoarse(t, ctx)
				settleMotion(t, ctx, where)
				if err := chromedp.Run(ctx, chromedp.Evaluate(axeJS, nil)); err != nil {
					t.Fatalf("%s: loading axe: %v", where, err)
				}
				paint(t, ctx, scheme)
				total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))
				// And with the display settings open, the language inside
				// it too. target-size is off for the same reason as the
				// shells' cards: the page outside the card is under the
				// summary's light-dismiss layer, which axe cannot see.
				where += ", display settings open"
				if err := chromedp.Run(ctx, chromedp.Click(".ds-prefs > summary", chromedp.ByQuery), chromedp.WaitVisible(".ds-prefs [rst-locale] > summary", chromedp.ByQuery),
					chromedp.Click(".ds-prefs [rst-locale] > summary", chromedp.ByQuery), chromedp.WaitVisible(".ds-prefs [rst-locale] [rst-dropdown-menu] a", chromedp.ByQuery)); err != nil {
					t.Fatalf("%s: opening: %v", where, err)
				}
				settleMotion(t, ctx, where)
				total += report(t, where, scan(t, ctx, where, `{run: (target, opts) => window.axe.run(target, Object.assign(opts, {rules: {"target-size": {enabled: false}}}))}`, "document", "false"))
			}
		}
	}
	if total == 0 {
		t.Logf("clean: both phone views, menu closed and open, in %d themes × %d schemes", len(ui.ThemeNames()), len(a11ySchemes))
	}
}

// The filter finds a component by what a reader calls it, on desktop
// and on the phone index: "checkbox" finds field-check, "dialog" the
// modal, "button" the Buttons entry, and junk says No matches with no
// page link left showing beside it. On the index, a query swaps the
// rows for the tree it filters, and a result opens its section with
// the partial in view.
func TestSearchFindsWhatPeopleCallThings(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	visible := func(sel string) string {
		return `(() => { const e = document.querySelector('` + sel + `'); return !!e && e.getBoundingClientRect().height > 0; })()`
	}
	typeInto := func(where, q string) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const i = document.getElementById("ds-filter"); i.value = ""; i.dispatchEvent(new Event("input")); return true; })()`, nil),
			chromedp.SendKeys(`#ds-filter`, q, chromedp.ByQuery)); err != nil {
			t.Fatalf("%s: typing %q: %v", where, q, err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitVisible(`#ds-filter`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Form: %v", err)
	}
	for q, id := range map[string]string{"checkbox": "partial-field-check", "dialog": "idiom-modal", "button": "idiom-button", "CHECKBOX": "partial-field-check"} {
		where := "desktop, " + q
		typeInto(where, q)
		until(t, ctx, where, visible(`#ds-nav a[href$="#`+id+`"]`))
	}
	typeInto("desktop, junk", "zzqxv")
	until(t, ctx, "desktop, junk", visible(`[data-ds-filter-empty]`)+` && ![...document.querySelectorAll("#ds-nav a")].some(a => a.getBoundingClientRect().height > 0)`)

	phone := phoneRig(t)
	pctx, pcancel := context.WithTimeout(phone.Context(), 120*time.Second)
	defer pcancel()
	where := "the phone index"
	if err := chromedp.Run(pctx, chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(phone.Origin+indexHref(mountPath, "day", "en")),
		chromedp.WaitVisible(`#ds-filter`, chromedp.ByQuery),
		chromedp.SendKeys(`#ds-filter`, "checkbox", chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: typing: %v", where, err)
	}
	requireCoarse(t, pctx)
	until(t, pctx, where+", rows swapped for the tree", `document.querySelector(".ds-index").getBoundingClientRect().height === 0 && `+visible(`#ds-nav a[href$="#partial-field-check"]`))
	if err := chromedp.Run(pctx, chromedp.Evaluate(`(() => { const i = document.getElementById("ds-filter"); i.value = ""; i.dispatchEvent(new Event("input")); return true; })()`, nil)); err != nil {
		t.Fatalf("%s: clearing: %v", where, err)
	}
	until(t, pctx, where+", rows back", visible(`.ds-index`))
	if err := chromedp.Run(pctx, chromedp.SendKeys(`#ds-filter`, "checkbox", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#ds-nav a[href$="#partial-field-check"]').click(); true`, nil)); err != nil {
		t.Fatalf("%s: following the result: %v", where, err)
	}
	until(t, pctx, where+", Form at the partial", `location.pathname.endsWith("/form.html") && location.hash === "#partial-field-check" && (() => { const r = document.getElementById("partial-field-check").getBoundingClientRect(); return r.top >= 0 && r.top < innerHeight; })()`)
}
