//go:build browser

// Package galleryrig is the design-system gallery's shared drive
// equipment: the rendered tree served at its mount, frames waited on
// until each shows its own document, and the other conditions that
// internal/designsystem's browser tests and its sweep package both
// need. A package rather than test files because two test packages use
// it; browser-tagged like harness, so chromedp stays out of the
// ordinary build graph. It imports nothing of the gallery's: the
// gallery's own tests import it, and an import back would be a cycle,
// so the rendered files are handed in.
package galleryrig

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// Tree serves a rendered tree at the mount its pages expect, and fails
// the test that uses it if anything asked for a file the tree does not
// have. Every URL in a page is absolute under the mount, so serving the
// tree anywhere else would 404 the stylesheet and a drive would measure
// an unstyled page; and a preview frame naming a missing file loads a
// 404 page that settles, measures and scans like any other document, so
// the miss has to be counted here.
//
// The failure is reported from t.Cleanup rather than from the request:
// lazy frames can still be loading after the test function returns,
// and t.Errorf from a server goroutine after that panics.
func Tree(t *testing.T, files map[string][]byte, mountPrefix string) http.Handler {
	t.Helper()
	var mu sync.Mutex
	var missing []string
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if len(missing) > 0 {
			t.Errorf("the tree was asked for %d file(s) it does not have: %v", len(missing), missing)
		}
	})
	return TreeRecording(t, files, mountPrefix, func(p string) {
		mu.Lock()
		missing = append(missing, p)
		mu.Unlock()
	})
}

// TreeRecording is Tree with each miss handed to the caller, so the
// recording itself can be tested.
func TreeRecording(t *testing.T, files map[string][]byte, mountPrefix string, miss func(path string)) http.Handler {
	t.Helper()
	types := map[string]string{
		".html": "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "text/javascript; charset=utf-8",
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The browser asks for /favicon.ico on its own; nothing outside
		// the mount is the tree's to answer.
		if !strings.HasPrefix(r.URL.Path, mountPrefix) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, mountPrefix)
		body, ok := files[name]
		if !ok {
			miss(r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if ct, ok := types[path.Ext(name)]; ok {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(body)
	})
}

// SettleFrames turns every preview frame eager and reports, for each
// one, the address its src names (a fresh frame is about:blank for a
// moment before its file replaces it) and, once that document is
// complete and populated, its height — or null while it is not there
// yet. It also reports how many frame documents the page requested,
// which is the cost a reader pays for reading the whole page.
//
// One synchronous read rather than an in-page wait loop: the "scripts
// off" leg (browser_test.go) proves the widget needs no script by
// disabling script execution for the whole page, and Blink still runs
// a debugger-injected evaluate under that, but stops servicing the
// timers an async/await loop would need — so a Promise built from
// setTimeout never resolves, and Chromium eventually reports it
// collected. Eagerly polls by calling this repeatedly from Go instead,
// which needs nothing running inside the page between calls.
const SettleFrames = `(() => {
  const frames = [...document.querySelectorAll(".ds-view__frame")];
  frames.forEach(f => { f.loading = "eager"; });
  const heights = frames.map(f => {
    let d = null;
    try { d = f.contentDocument; } catch (e) { return null; }
    if (!d || d.URL !== f.src || d.readyState !== "complete" || !d.body || !d.body.children.length) return null;
    return d.body.scrollHeight;
  });
  const requested = performance.getEntriesByType("resource").filter(e => e.initiatorType === "iframe").length;
  return JSON.stringify({Titles: frames.map(f => f.title), Heights: heights, Requested: requested});
})()`

// Eagerly loads every frame on the page and waits until each settles:
// complete, populated, and the same height across 150ms. Measuring
// what a reader sees means reaching inside each frame, and a frame
// that never loaded reports zero height, which reads exactly like a
// collapsed preview: so a frame that does not settle within 30s fails
// the leg by name rather than being measured.
func Eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	type reading struct {
		Titles    []string
		Heights   []*int
		Requested int
	}
	read := func() reading {
		t.Helper()
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(SettleFrames, &raw)); err != nil {
			t.Fatalf("%s: loading the framed documents: %v", where, err)
		}
		var got reading
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: reading the frame settle (%q): %v", where, raw, err)
		}
		return got
	}

	before := read()
	pending := make([]bool, len(before.Titles))
	for i := range pending {
		pending[i] = true
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		still := false
		for _, p := range pending {
			still = still || p
		}
		if !still {
			t.Logf("%s: %d frames settled; the page requested %d frame documents", where, len(before.Titles), before.Requested)
			return
		}
		if !time.Now().Before(deadline) {
			var names []string
			for i, p := range pending {
				if p {
					names = append(names, before.Titles[i])
				}
			}
			t.Fatalf("%s: %d of %d frames never settled within 30s: %v", where, len(names), len(before.Titles), names)
		}
		time.Sleep(150 * time.Millisecond)
		after := read()
		for i, p := range pending {
			if !p {
				continue
			}
			if before.Heights[i] == nil || after.Heights[i] == nil || *after.Heights[i] != *before.Heights[i] {
				continue
			}
			pending[i] = false
		}
		before = after
	}
}

// SettleMobile waits after every Mobile radio has been clicked: every
// frame laid out no wider than a phone, and every frame's width and
// content height the same across 150ms.
const SettleMobile = `(async () => {
  const frames = [...document.querySelectorAll(".ds-view__frame")];
  const read = () => frames.map(f => {
    let h = -1;
    try { h = f.contentDocument.body.scrollHeight; } catch (e) {}
    return f.offsetWidth + ":" + h;
  }).join(",");
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const a = read();
    await new Promise(r => setTimeout(r, 150));
    if (a === read() && frames.every(f => f.offsetWidth <= 390)) return "ok";
  }
  return "never settled: " + frames.filter(f => f.offsetWidth > 390).map(f => f.title).slice(0, 5).join("; ");
})()`

// MobileSettle runs SettleMobile and fails the leg if it times out.
func MobileSettle(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var state string
	if err := chromedp.Run(ctx, chromedp.Evaluate(SettleMobile, &state,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("%s: waiting for the Mobile layout: %v", where, err)
	}
	if state != "ok" {
		t.Fatalf("%s: the frames %s", where, state)
	}
}

// AddInit runs js in every document the tab loads, before the page's
// own scripts: the only way to stand in for an API a script reads at
// DOMContentLoaded, such as the clipboard or a storage that throws.
func AddInit(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(js).Do(ctx)
		return err
	})
}

// Until polls expr until it is true, for ten seconds, and fails the leg
// naming it otherwise. Polling rather than waiting on an event, because
// what these legs wait for (a promise settling, a timer, a page restored
// from the back/forward cache) raises nothing chromedp can wait on. An
// evaluation that lands while one document is being swapped for the
// next fails, and the next poll reads the new page; so an error is
// retried, and one that lasts to the deadline fails the leg by name, as
// a false would.
func Until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for {
		var ok bool
		err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok))
		if err == nil && ok {
			return
		}
		if err != nil {
			last = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: never true within 10s (last error: %v): %s", where, last, expr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ClickEvery is the script that clicks every radio of one tab ("d",
// "m" or "c") and reports how many clicks moved something. It is the
// instrument's own control: a reading taken after a click that did not
// land is a reading of the state before it.
func ClickEvery(mod string) string {
	return `(() => {
  let clicked = 0, moved = 0;
  document.querySelectorAll(".ds-view__tab--` + mod + ` input").forEach(i => {
    const before = i.checked;
    i.click();
    clicked++;
    if (i.checked && !before) moved++;
  });
  return JSON.stringify({clicked: clicked, moved: moved});
})()`
}

// NoScripts is a tab with the page's scripts switched off at the
// engine. Evaluate still runs, through the debugger, so a leg can read
// a scriptless page without the page having run anything.
func NoScripts(t *testing.T, parent context.Context) context.Context {
	t.Helper()
	ctx, cancel := chromedp.NewContext(parent)
	t.Cleanup(cancel)
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("switching scripts off: %v", err)
	}
	return ctx
}

// RequireScriptsOff is NoScripts's own control, run once a page has
// loaded on its tab: data-rst-js is the one attribute gallery.js sets,
// and only after it runs, so finding it would mean this leg's "scripts
// off" page ran scripts after all, and every reading taken under it is
// a reading of the scripted page.
func RequireScriptsOff(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var on bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.hasAttribute("data-rst-js")`, &on)); err != nil {
		t.Fatalf("%s: reading data-rst-js: %v", where, err)
	}
	if on {
		t.Fatalf("%s: data-rst-js is set; gallery.js ran even though this leg switched scripts off", where)
	}
}

// WithoutAnchorPositioning replaces the page's tokens.css with a copy
// whose anchor-positioning @supports condition can never hold, which is
// what an engine without the feature gets: the menu panel positioned
// absolutely under its summary instead of fixed against it.
const WithoutAnchorPositioning = `(async () => {
  const link = document.querySelector('link[href$="/tokens.css"]');
  const css = await (await fetch(link.href)).text();
  const style = document.createElement("style");
  style.textContent = css.replaceAll("@supports (position-area: block-end) and (position-try-fallbacks: flip-block)", "@supports (position-area: rastrillo-no-such-value)");
  link.replaceWith(style);
  return true;
})()`

// RequireAnchorPositioning is the control for every leg that runs with
// and without the feature: the named menu panel computes position:
// fixed when anchor positioning applies and absolute when it does not.
// A leg that asked for "without" and still got fixed would pass on the
// path it claims to have left. selector picks one panel rather than
// "the first [rst-dropdown-menu] on the page", because a page can carry
// more than one (the bar's and the rail foot's), and the first found is
// not necessarily the one the leg is driving.
func RequireAnchorPositioning(t *testing.T, ctx context.Context, where, selector string, want bool) {
	t.Helper()
	var pos string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector(`+jsString(selector)+`)).position`, &pos)); err != nil {
		t.Fatalf("%s: reading the menu panel's position: %v", where, err)
	}
	if (pos == "fixed") != want {
		t.Fatalf("%s: the menu panel is position: %s; this leg needs anchor positioning %v", where, pos, want)
	}
}

// jsString quotes a Go string as a JS string literal, for a selector
// handed in at runtime rather than written into the script's source.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// PhoneRig is a browser whose primary pointer is a touch screen,
// serving what tree builds. It is a launch flag: CDP's touch emulation
// leaves (pointer: coarse) false in this engine. A leg that checks
// anything against the page's width passes harness.WithScrollbars too:
// see RequireDrawnScrollbar.
func PhoneRig(t *testing.T, tree func() http.Handler, opts ...harness.Option) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler { return tree() }, append([]harness.Option{harness.WithCoarsePointer()}, opts...)...)
}

// RequireDrawnScrollbar is the control for any leg that measures a box
// against the document's width, and it belongs on a rig built with
// harness.WithScrollbars. Under chromedp's default --hide-scrollbars,
// Chromium still reserves the root's stable gutter (15px) in layout but
// leaves it out of documentElement.clientWidth, and innerWidth includes
// it either way. A box that really spans the page then reads 15px short
// of a "document" it can never reach, and a sideways check reads -15,
// so it would miss an overflow of up to 15px. With the scrollbar drawn,
// clientWidth and layout agree.
func RequireDrawnScrollbar(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var gutter float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`innerWidth - document.documentElement.clientWidth`, &gutter)); err != nil {
		t.Fatalf("%s: measuring the scrollbar: %v", where, err)
	}
	if gutter <= 0 {
		t.Fatalf("%s: the root's scrollbar takes %.0fpx, so this browser draws none and documentElement.clientWidth is not the width the page lays out in. Build the rig with harness.WithScrollbars", where, gutter)
	}
}

// RequireCoarse is every touch leg's control: a leg running on a fine
// pointer would measure desktop sizes and pass.
func RequireCoarse(t *testing.T, ctx context.Context) {
	t.Helper()
	var coarse bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`matchMedia("(pointer: coarse)").matches`, &coarse)); err != nil {
		t.Fatalf("reading the pointer: %v", err)
	}
	if !coarse {
		t.Fatal("(pointer: coarse) is false; this rig is not the phone this leg claims to measure")
	}
}

// SettleMotion waits out every finite animation and any view
// transition: the shell slides between the index and a page, and the
// row the reader came back to flashes. axe reading either mid-flight
// measures colours at partial opacity that no reader sees once it has
// landed.
func SettleMotion(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(async () => {
	  if (document.activeViewTransition) await document.activeViewTransition.finished.catch(() => null);
	  await Promise.all(document.getAnimations()
	    .filter(a => a.effect && a.effect.getComputedTiming().endTime !== Infinity)
	    .map(a => a.finished.catch(() => null)));
	  return true;
	})()`, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("%s: waiting for motion to end: %v", where, err)
	}
}

// MeasureFrames reads every frame on the page: section id → [what its
// document needs, what its box gives it, 1 if it frames a sidebar-shell
// page]. The tallest state of a section is the one recorded, so one
// number per section keeps the boxes down a column the same size.
const MeasureFrames = `(() => {
  const out = {};
  for (const f of document.querySelectorAll(".ds-view__frame")) {
    const section = f.closest("article, section");
    const id = section ? section.id : "?";
    const d = f.contentDocument;
    const need = d ? Math.ceil(Math.max(d.body.getBoundingClientRect().height, d.body.scrollHeight)) : -1;
    const box = Math.round(parseFloat(getComputedStyle(f).height));
    const shell = d && d.querySelector("[rst-shell-sidebar]") ? 1 : 0;
    const was = out[id];
    if (!was || need > was[0]) out[id] = [need, box, shell];
  }
  return JSON.stringify(out);
})()`

// ReadFrames decodes one MeasureFrames reading: section id → [what its
// document needs, its box, 1 for a sidebar-shell page]. It judges no
// height, so a sweep can gather every language's readings before it
// asserts anything; it fails only on what makes a reading worthless: a
// reading that does not decode, a page with no rendered example, or a
// frame with no document in it.
func ReadFrames(t *testing.T, tab, raw string) map[string][3]int {
	t.Helper()
	var got map[string][3]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: reading the measurements: %v", tab, err)
	}
	if len(got) == 0 {
		t.Fatalf("%s: no section on this page has a rendered example at all; either the page rendered none, or no frame on it loaded", tab)
	}
	for id, r := range got {
		if r[0] < 0 {
			t.Errorf("%s %s: the frame has no document in it", tab, id)
		}
	}
	return got
}

// Measured is ReadFrames with each section held to its box. shellOnly
// decides who gets 48px of grace: the sidebar shell's rail is 100dvh
// tall, so a page framing one is always the frame's height plus the
// margin under its content, and no box can fit it. The wide drive
// grants the grace to every frame, as it always has.
func Measured(t *testing.T, tab, raw string, shellOnly bool) map[string][3]int {
	t.Helper()
	got := ReadFrames(t, tab, raw)
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, id := range names {
		need, box, shell := got[id][0], got[id][1], got[id][2]
		grace := 48
		if shellOnly && shell == 0 {
			grace = 0
		}
		if need >= 0 && need > box+grace {
			t.Errorf("%s %s: its document needs %dpx and its frame is %dpx; raise its height (previewHeights for Desktop, previewMobileHeights for Mobile) to at least %d", tab, id, need, box, need+20)
		}
		if need >= 0 && box > need*4 && box-need > 120 {
			t.Logf("%s %s: %dpx of frame for %dpx of document; deliberate headroom, or a number to bring down", tab, id, box, need)
		}
	}
	return got
}

// HeightRow is one page kind the height drives measure, the least
// number of its sections with an example, and why it owes them.
type HeightRow struct {
	Kind  string
	Least int
	Owed  string
}

// owed says why each page that is not a family page owes its sections.
var owed = map[string]string{
	"overview":   "the demo application is framed here",
	"primitives": "every sample ui.Styleguide() ships has a section here",
	"shells":     "every shell ui.LayoutNames() reports has a section here",
	"screens":    "every screen screenDocs() ships has a section here",
	"formats":    "every section formatDocs() ships has a sample here",
}

// HeightRows is the height drives' coverage table: each page kind in
// counts, in the order kinds lists them.
func HeightRows(kinds []string, counts map[string]int) []HeightRow {
	var out []HeightRow
	for _, k := range kinds {
		n, ok := counts[k]
		if !ok {
			continue
		}
		why := owed[k]
		if why == "" {
			why = "every partial samples.go puts in this family has a section here"
		}
		out = append(out, HeightRow{Kind: k, Least: n, Owed: why})
	}
	return out
}

// FramesNothing names the page kinds with no preview frame, each with
// why, so a page kind that grows frames cannot go unmeasured: a kind in
// neither table fails both height drives.
var FramesNothing = map[string]string{
	"tokens":          "a swatch grid and two scale tables; no preview frames",
	"icons":           "inline SVG drawn directly on the page, not framed",
	"getting-started": "prose, links and two source blocks",
}
