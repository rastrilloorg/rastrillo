//go:build browser

package ui

import (
	"context"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// legacyLayout is a layout as it was before the phone index, kept as a
// fixture so the legacy rules in tokens.css stay pinned to real markup.
func legacyLayout(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/legacy/" + name + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// shellLayoutPage renders a layout's source with a nav of three
// sections (ids, so the scriptless focus return has fragments to use),
// a title, a language menu, and the given extra defines.
func shellLayoutPage(t *testing.T, src []byte, dir string, defs ...string) string {
	t.Helper()
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	base := []string{
		`{{define "dir"}}` + dir + `{{end}}`,
		`{{define "title"}}Harbour{{end}}`,
		`{{define "brand"}}<a rst-shell-brand href="/">Harbour</a>{{end}}`,
		`{{define "nav"}}<a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a><a id="nav-team" href="/team">Team</a>{{end}}`,
		`{{define "locale"}}<details rst-dropdown rst-locale id="rail-locale" name="rst-menus"><summary>Language</summary><div rst-dropdown-menu><a href="/go/en" lang="en">English</a><a href="/go/ga" lang="ga">Gaeilge</a></div></details>{{end}}`,
		`{{define "content"}}<h1>Invoices</h1><p>Content.</p>{{end}}`,
	}
	for _, d := range append(base, defs...) {
		template.Must(tmpl.Parse(d))
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// shellAssets adds shell.js and shell.css to the sizing mux. A
// *http.ServeMux, so a drive can hand it to rastrillo.Handler.
func shellAssets(t *testing.T, pages map[string]string) *http.ServeMux {
	mux := sizingMux(t, pages)
	mux.HandleFunc("GET /shell.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(ShellJS())
	})
	mux.HandleFunc("GET /shell.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(ShellCSS())
	})
	return mux
}

// viewJS is what a reader of a shell page sees. BackStart is measured
// from the root element's box, not the viewport: tokens.css reserves a
// stable scrollbar gutter, which a right-to-left page puts on the left,
// so the viewport's edge is not the page's.
const viewJS = `(() => {
  const shown = el => !!el && el.getClientRects().length > 0 && el.getBoundingClientRect().width > 0;
  const q = s => document.querySelector(s), back = q("[rst-shell-back] a"), br = back ? back.getBoundingClientRect() : null;
  const title = q("[rst-shell-title]"), hr = document.documentElement.getBoundingClientRect();
  return JSON.stringify({Rail: shown(q("[rst-shell-rail] [rst-shell-nav]")), Main: shown(q("[rst-shell-main]")),
    SkipDisplay: getComputedStyle(q("[rst-skip]")).display, Title: shown(title) ? title.innerText.trim() : "",
    Back: shown(back), BackStart: br ? Math.round(document.documentElement.dir === "rtl" ? hr.right - br.right : br.left - hr.left) : -1,
    BackTop: br ? Math.round(br.top) : -1, Drawer: !!q("[rst-shell-chrome]"),
    H1s: [...document.querySelectorAll("h1")].filter(shown).length, Len: history.length});
})()`

// settled waits until the document has loaded and its view transition,
// if any, is over. A tap during the slide lands on the transition's
// snapshot, not on the page (elementFromPoint returns <html>), so a
// click sent then does nothing, and a navigation started then skips the
// running transition, which Chromium reports as an uncaught AbortError.
// Script execution may be disabled: Runtime.evaluate still runs.
func settled(t *testing.T, ctx context.Context) {
	t.Helper()
	settleUntil(t, ctx, `document.readyState === "complete" && !document.activeViewTransition`)
}

// skippedTransition is the rejection headless Chromium reports when it
// skips a cross-document view transition before the new page reveals.
const skippedTransition = `AbortError: Transition was skipped`

// scriptErrors is every uncaught exception except a skipped transition,
// and how many skipped transitions it set aside.
// Measured on this machine's Chromium with the real layouts: on roughly
// one navigation in five the incoming transition is skipped before
// pagereveal (the event carries no viewTransition, so no script ever saw
// it) and the browser reports its own unhandled promise. The skip
// happens with every deferred script the layouts load and not with
// shell.js alone, and navigation, history and focus are unaffected: the
// page simply arrives without the slide. Anything else thrown is still a
// failure, and so is every navigation in a drive being skipped: that is
// no longer a race, it is a slide that never runs.
func scriptErrors(t *testing.T, thrown []string) (errs []string, skipped int) {
	t.Helper()
	for _, e := range thrown {
		if strings.Contains(e, skippedTransition) && strings.Contains(e, "Uncaught (in promise)") {
			t.Logf("the browser skipped a transition (no slide this time): %s", e)
			skipped++
			continue
		}
		errs = append(errs, e)
	}
	return errs, skipped
}

type viewReading struct {
	Rail, Main, Back, Drawer bool
	SkipDisplay, Title       string
	BackStart, BackTop, H1s  int
	Len                      int
}

// TestThePhoneIndexWorksWithNoScript is the scriptless leg, both
// shells, LTR and RTL, at 390 with a coarse pointer and scripts
// disabled in the engine: the index is the rail with the title as its
// one visible h1 and no main, no skip link and no drawer; a nav link
// opens the content page with the back control at the top inline-start
// and no rail; the back control lands on /#nav-… where that link is the
// target and the next Tab goes to the link after it. History grows one
// entry per step, which is the scriptless trade (logged).
func TestThePhoneIndexWorksWithNoScript(t *testing.T) {
	for _, shell := range []string{"sidebar", "console"} {
		for _, dir := range []string{"ltr", "rtl"} {
			t.Run(shell+" "+dir, func(t *testing.T) {
				src, _ := Layout(shell)
				pages := map[string]string{
					"/":         shellLayoutPage(t, src, dir, `{{define "view"}}index{{end}}`),
					"/invoices": shellLayoutPage(t, src, dir, `{{define "up"}}/#nav-invoices{{end}}`),
				}
				rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
				ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
				defer cancel()
				mustRun(t, ctx, emulation.SetScriptExecutionDisabled(true), chromedp.EmulateViewport(390, 844),
					chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body"))
				requirePointer(t, ctx, true)
				var idx, pg, back viewReading
				at(t, ctx, viewJS, &idx)
				if !idx.Rail || idx.Main || idx.SkipDisplay != "none" || idx.Title != "Harbour" || idx.Back || idx.Drawer || idx.H1s != 1 {
					t.Errorf("the index: %+v; want the rail, the title as the one h1, no main, no skip link, no back control, no drawer", idx)
				}
				// main is hidden on the index, so it being visible is a
				// marker only the content page has.
				mustRun(t, ctx, chromedp.Click("#nav-invoices", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-main]", chromedp.ByQuery))
				settled(t, ctx)
				at(t, ctx, viewJS, &pg)
				if pg.Rail || !pg.Main || !pg.Back || pg.BackStart > 8 || pg.H1s != 1 {
					t.Errorf("the content page: %+v; want main, the back control within 8px of the top inline-start, no rail, one h1", pg)
				}
				// 6px: the 44px control centred in the 56px strip at the
				// top of the screen (TestThePhoneHeaderStripIs56Pixels).
				if shell == "sidebar" && pg.BackTop > 7 {
					t.Errorf("the sidebar's back control is %dpx from the top; want it centred in the strip at the top of the screen, 6px down", pg.BackTop)
				}
				// And the rail's nav is shown only on the index.
				mustRun(t, ctx, chromedp.Click("[rst-shell-back] a", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-rail] [rst-shell-nav]", chromedp.ByQuery))
				settled(t, ctx)
				at(t, ctx, viewJS, &back)
				var target bool
				var next string
				mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("nav-invoices").matches(":target")`, &target),
					chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`document.activeElement.id`, &next))
				if !target || next != "nav-orders" {
					t.Errorf("back on the index: #nav-invoices is the target %v and the next Tab went to %q; want true and nav-orders", target, next)
				}
				t.Logf("%s %s: history %d -> %d -> %d (scripts off: one entry per step)", shell, dir, idx.Len, pg.Len, back.Len)
				if pg.Len != idx.Len+1 || back.Len != pg.Len+1 {
					t.Errorf("history grew %d then %d, want 1 and 1", pg.Len-idx.Len, back.Len-pg.Len)
				}
			})
		}
	}
}

// TestThePhoneIndexWorksWithScripts is the scripted journey on the REAL
// layouts and the real stylesheet (shell.js's own drives use
// hand-written markup): both shells, LTR and RTL, at 390 with a coarse
// pointer. Index, a section, the back control: history is reused (its
// length unchanged), the URL is the index, and focus is on the row the
// reader left. Then a deep link's back control, which follows the link
// and still focuses the row from the record.
func TestThePhoneIndexWorksWithScripts(t *testing.T) {
	for _, shell := range []string{"sidebar", "console"} {
		for _, dir := range []string{"ltr", "rtl"} {
			t.Run(shell+" "+dir, func(t *testing.T) {
				src, _ := Layout(shell)
				pages := map[string]string{
					"/":         shellLayoutPage(t, src, dir, `{{define "view"}}index{{end}}`),
					"/invoices": shellLayoutPage(t, src, dir, `{{define "up"}}/#nav-invoices{{end}}`),
					"/orders":   shellLayoutPage(t, src, dir, `{{define "up"}}/#nav-orders{{end}}`),
				}
				rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
				ctx, done, thrown := tab(t, rig, "")
				defer done()
				visit(t, ctx, rig.Origin+"/")
				requirePointer(t, ctx, true)
				settled(t, ctx)
				follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices" && document.querySelector("[rst-shell-back] a").checkVisibility()`)
				settled(t, ctx)
				before := state(t, ctx)
				follow(t, ctx, "[rst-shell-back] a", `location.pathname === "/" && document.activeElement.id === "nav-invoices"`)
				settled(t, ctx)
				if after := state(t, ctx); after.Len != before.Len {
					t.Errorf("the back control grew history %d -> %d; with scripts it reuses the entry behind it", before.Len, after.Len)
				}
				errs, skipped := scriptErrors(t, *thrown)
				if len(errs) > 0 {
					t.Errorf("uncaught: %v", errs)
				}
				// A deep link: a fresh tab opened straight on a section, so
				// nothing is behind it. The link is followed (history +1)
				// and focus still comes back to the row, from the record.
				deep, doneDeep, deepThrown := tab(t, rig, "")
				defer doneDeep()
				visit(t, deep, rig.Origin+"/orders")
				settled(t, deep)
				before = state(t, deep)
				follow(t, deep, "[rst-shell-back] a", `location.pathname === "/" && document.activeElement.id === "nav-orders"`)
				settled(t, deep)
				if after := state(t, deep); after.Len != before.Len+1 {
					t.Errorf("a deep link's back control: history %d -> %d, want the link followed (+1)", before.Len, after.Len)
				}
				deepErrs, deepSkipped := scriptErrors(t, *deepThrown)
				if len(deepErrs) > 0 {
					t.Errorf("uncaught in the deep-link tab: %v", deepErrs)
				}
				// Three navigations slide in this drive: to the section,
				// back, and the deep link's back.
				if skipped+deepSkipped >= 3 {
					t.Errorf("every one of the drive's 3 navigations skipped its transition (%d skips); the slide never ran", skipped+deepSkipped)
				}

				// The console's index with rastrillo.js loaded: the card
				// opens over the rail and an outside tap closes it, and
				// the rail, which no longer follows the Menu, stays.
				if shell == "console" {
					idx, doneIdx, _ := tab(t, rig, "")
					defer doneIdx()
					visit(t, idx, rig.Origin+"/")
					settled(t, idx)
					mustRun(t, idx, chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery),
						chromedp.WaitVisible("[rst-shell-tail] #rail-locale > summary", chromedp.ByQuery))
					clickAndStay(t, idx, probe(t, idx, "[rst-shell-rail]", 0.5, 0.8, 0, 0), "",
						`!document.querySelector("[rst-shell-menu]").open && document.querySelector("#nav-team").checkVisibility()`)
				}
			})
		}
	}
}

// boxesJS is the rail's and main's boxes, for comparing two layouts.
const boxesJS = `(() => { const b = s => { const r = document.querySelector(s).getBoundingClientRect(); return [r.left, r.top, r.width, r.height].map(Math.round).join(","); };
  return JSON.stringify({Rail: b("[rst-shell-rail]"), Main: b("[rst-shell-main]")}); })()`

// TestTheWideLayoutIsUnchanged: at 1280, in both directions, both views
// keep the old layout's boxes (the same markup rendered through the
// legacy layout is the golden), with no back control and no second h1.
//
// One deliberate change, asserted rather than tolerated: the sidebar
// rail's foot is no longer the language menu and the person laid out
// flat. It is one avatar button, the profile menu, and the language is
// inside its card, so the language menu that the legacy layout shows in
// the foot is not on screen until the avatar is opened. The rail and
// main boxes do not move, because the foot was always inside the rail.
func TestTheWideLayoutIsUnchanged(t *testing.T) {
	for _, c := range []struct{ shell, dir string }{{"sidebar", "ltr"}, {"sidebar", "rtl"}, {"console", "ltr"}, {"console", "rtl"}} {
		t.Run(c.shell+" "+c.dir, func(t *testing.T) {
			shell := c.shell
			src, _ := Layout(shell)
			pages := map[string]string{
				"/index":  shellLayoutPage(t, src, c.dir, `{{define "view"}}index{{end}}`),
				"/page":   shellLayoutPage(t, src, c.dir),
				"/legacy": shellLayoutPage(t, legacyLayout(t, shell), c.dir),
			}
			rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			read := func(path string) (map[string]string, viewReading) {
				var boxes map[string]string
				var v viewReading
				mustRun(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("body"))
				at(t, ctx, boxesJS, &boxes)
				at(t, ctx, viewJS, &v)
				return boxes, v
			}
			footJS := `JSON.stringify({Locale: document.querySelector("#rail-locale > summary").checkVisibility(), Avatar: !!document.querySelector("[rst-shell-rail-foot] [rst-shell-profile] > summary") && document.querySelector("[rst-shell-rail-foot] [rst-shell-profile] > summary").checkVisibility()})`
			var foot struct{ Locale, Avatar bool }
			golden, _ := read("/legacy")
			at(t, ctx, footJS, &foot)
			if shell == "sidebar" && (!foot.Locale || foot.Avatar) {
				t.Errorf("the legacy sidebar's foot: language menu shown %v, avatar %v; the golden is the flat foot", foot.Locale, foot.Avatar)
			}
			for _, path := range []string{"/index", "/page"} {
				got, v := read(path)
				if shell == "sidebar" {
					at(t, ctx, footJS, &foot)
					if foot.Locale || !foot.Avatar {
						t.Errorf("%s %s at 1280: the rail's foot shows the language menu %v and the avatar %v; want the avatar alone, the language inside its card", c.dir, path, foot.Locale, foot.Avatar)
					}
				}
				if got["Rail"] != golden["Rail"] || got["Main"] != golden["Main"] {
					t.Errorf("%s %s %s at 1280: rail %s main %s; the old layout gives rail %s main %s", shell, c.dir, path, got["Rail"], got["Main"], golden["Rail"], golden["Main"])
				}
				if v.Back || v.Title != "" || v.H1s != 1 {
					t.Errorf("%s %s at 1280: back %v, title %q, %d h1s; want no back control and only the page's own h1", shell, path, v.Back, v.Title, v.H1s)
				}
			}
		})
	}
}

// TestAnOldSidebarLayoutKeepsItsDrawer: an app that re-vendors
// tokens.css and keeps its old layout still opens its drawer at 390.
func TestAnOldSidebarLayoutKeepsItsDrawer(t *testing.T) {
	pages := map[string]string{"/": shellLayoutPage(t, legacyLayout(t, "sidebar"), "ltr")}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var before, after viewReading
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-chrome] > summary", chromedp.ByQuery))
	at(t, ctx, viewJS, &before)
	mustRun(t, ctx, chromedp.Click("[rst-shell-chrome] > summary", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-rail] [rst-shell-nav] a", chromedp.ByQuery))
	at(t, ctx, viewJS, &after)
	if before.Rail || !after.Rail || !after.Main {
		t.Errorf("the old layout's drawer: rail before %v, after opening %v, main %v; want closed, then open with the page still there", before.Rail, after.Rail, after.Main)
	}
}

// TestTheIndexRowsRoundEachRunOfLinks: whatever the nav starts with,
// every row has its side and bottom borders, the first row of each run
// its top border and top corners, and the last row of each run its
// bottom corners. On a coarse pointer, where the touch block's 44px
// floor for nav links would otherwise win, every row is still 3rem.
func TestTheIndexRowsRoundEachRunOfLinks(t *testing.T) {
	src, _ := Layout("sidebar")
	navs := map[string]string{
		"/group-first": `<p rst-shell-group>Sales</p><a href="/a">A</a><a href="/b">B</a><p rst-shell-group>Settings</p><a href="/c">C</a>`,
		"/one":         `<a href="/a">A</a>`,
		"/no-groups":   `<a href="/a">A</a><a href="/b">B</a><a href="/c" aria-current="page">C</a>`,
	}
	rows := map[string]int{"/group-first": 3, "/one": 1, "/no-groups": 3}
	pages := map[string]string{}
	for path, nav := range navs {
		pages[path] = shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "nav"}}`+nav+`{{end}}`)
	}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	for path := range navs {
		var got struct {
			Rows int
			Bad  []string
		}
		mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("body"))
		requirePointer(t, ctx, true)
		at(t, ctx, `(() => {
		  const bad = [], links = document.querySelectorAll("[rst-shell-nav] > a");
		  for (const a of links) {
		    const cs = getComputedStyle(a), prev = a.previousElementSibling, next = a.nextElementSibling;
		    const first = !prev || prev.hasAttribute("rst-shell-group"), last = !next || next.hasAttribute("rst-shell-group");
		    const px = v => parseFloat(v) || 0;
		    if (px(cs.borderLeftWidth) !== 1 || px(cs.borderRightWidth) !== 1 || px(cs.borderBottomWidth) !== 1) bad.push(a.textContent + ": sides/bottom");
		    if ((px(cs.borderTopWidth) === 1) !== first) bad.push(a.textContent + ": top border " + cs.borderTopWidth);
		    if ((px(cs.borderStartStartRadius) > 0) !== first) bad.push(a.textContent + ": top corner " + cs.borderStartStartRadius);
		    if ((px(cs.borderEndStartRadius) > 0) !== last) bad.push(a.textContent + ": bottom corner " + cs.borderEndStartRadius);
		    if (a.getBoundingClientRect().height < 47.5) bad.push(a.textContent + ": under 3rem (" + a.getBoundingClientRect().height + "px)");
		  }
		  return JSON.stringify({Rows: [...links].filter(a => a.getClientRects().length > 0).length, Bad: bad});
		})()`, &got)
		if got.Rows != rows[path] {
			t.Fatalf("%s: %d index rows shown, want %d; the leg would be checking nothing", path, got.Rows, rows[path])
		}
		if len(got.Bad) > 0 {
			t.Errorf("%s: %v", path, got.Bad)
		}
	}
}

// TestTheConsoleIndexRailFillsTheScreen: the console's phone index is
// the bar, then the rail, then the foot. The three share the window: a
// rail a whole window tall under the bar made every index scroll by the
// bar's and the foot's height, however short its nav. With a short nav
// they fill the window exactly and nothing scrolls, with a foot or
// without one; with a long nav the rail grows, the page scrolls, and
// every row can still be reached and hit.
func TestTheConsoleIndexRailFillsTheScreen(t *testing.T) {
	src, _ := Layout("console")
	short := `<a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a>`
	var long strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&long, `<a id="nav-%d" href="/s%d">Section %d</a>`, i, i, i)
	}
	foot := `{{define "foot"}}<a href="/about">About</a>{{end}}`
	pages := map[string]string{
		"/short":  shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "nav"}}`+short+`{{end}}`, foot),
		"/nofoot": shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "nav"}}`+short+`{{end}}`),
		"/long":   shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "nav"}}`+long.String()+`{{end}}`, foot),
	}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	type geometry struct {
		BarTop, BarH, RailTop, RailH, RailBottom, NavBottom float64
		FootShown                                           bool
		FootTop, FootBottom, DocH, VH                       float64
	}
	read := func(path string) geometry {
		t.Helper()
		var g geometry
		mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("body"))
		// Every box in document coordinates, so a scrolled page reads the
		// same as one at the top.
		at(t, ctx, `(() => { const b = s => { const r = document.querySelector(s).getBoundingClientRect(); return {top: r.top + scrollY, bottom: r.bottom + scrollY, h: r.height}; };
		  const bar = b("[rst-shell-bar]"), rail = b("[rst-shell-rail]"), foot = b("[rst-shell-foot]"), nav = b("[rst-shell-nav]");
		  return JSON.stringify({BarTop: bar.top, BarH: bar.h, RailTop: rail.top, RailH: rail.h, RailBottom: rail.bottom, NavBottom: nav.bottom,
		    FootShown: foot.h > 0, FootTop: foot.top, FootBottom: foot.bottom, DocH: document.documentElement.scrollHeight, VH: innerHeight}); })()`, &g)
		return g
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1 }

	g := read("/short")
	if g.NavBottom > g.VH/2 || !g.FootShown {
		t.Fatalf("/short: the nav ends at %.0f of %.0f, foot shown %v; the short-nav-with-a-foot case this leg is for has not arisen", g.NavBottom, g.VH, g.FootShown)
	}
	if !near(g.BarTop, 0) || !near(g.RailTop, g.BarTop+g.BarH) || !near(g.FootTop, g.RailBottom) || !near(g.FootBottom, g.VH) {
		t.Errorf("/short: the bar is %.0fpx at %.0f, the rail %.0fpx at %.0f and the foot %.0f to %.0f in a %.0fpx window; together they fill the window exactly", g.BarH, g.BarTop, g.RailH, g.RailTop, g.FootTop, g.FootBottom, g.VH)
	}
	if !near(g.DocH, g.VH) {
		t.Errorf("/short: the document is %.0fpx in a %.0fpx window; a short index does not scroll", g.DocH, g.VH)
	}

	g = read("/nofoot")
	if g.FootShown {
		t.Fatal("/nofoot: an empty foot is drawn; the no-foot case has not arisen")
	}
	if !near(g.BarH+g.RailH, g.VH) || !near(g.DocH, g.VH) {
		t.Errorf("/nofoot: bar %.0f + rail %.0f in a %.0fpx window, document %.0fpx; with no foot the index is exactly the window and nothing scrolls", g.BarH, g.RailH, g.VH, g.DocH)
	}

	g = read("/long")
	if g.NavBottom < g.VH {
		t.Fatalf("/long: thirty rows end at %.0f in a %.0fpx window; the long-nav case has not arisen", g.NavBottom, g.VH)
	}
	if !near(g.FootTop, g.RailBottom) || !near(g.DocH, g.FootBottom) {
		t.Errorf("/long: the rail ends at %.0f, the foot spans %.0f to %.0f and the document is %.0fpx; the foot follows a long rail too", g.RailBottom, g.FootTop, g.FootBottom, g.DocH)
	}
	var missed []string
	at(t, ctx, `(() => { const out = [];
	  for (const a of document.querySelectorAll("[rst-shell-nav] a")) {
	    a.scrollIntoView({block: "center"});
	    const r = a.getBoundingClientRect(), h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
	    if (!(h === a || a.contains(h))) out.push(a.id + " -> " + (h ? h.tagName : "nothing"));
	  }
	  return JSON.stringify(out); })()`, &missed)
	if len(missed) > 0 {
		t.Errorf("/long: rows that cannot be scrolled to and hit: %v", missed)
	}
}

// A view block with whitespace matches the index rules in the engine,
// not only in the template's output.
func TestAWhitespaceViewIsTheIndexInTheBrowser(t *testing.T) {
	src, _ := Layout("sidebar")
	pages := map[string]string{"/": shellLayoutPage(t, src, "ltr", "{{define \"view\"}}\n  index\n{{end}}")}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var v viewReading
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body"))
	at(t, ctx, viewJS, &v)
	if !v.Rail || v.Main {
		t.Errorf("a view written as \"\\n  index\\n\" renders rail %v main %v; want the index", v.Rail, v.Main)
	}
}

// TestAnOldConsoleLayoutKeepsItsRailOpen: an app that re-vendors
// rastrillo.js and tokens.css but keeps a console layout from before
// the phone index still has a rail it can use at 390. Its rail is gated
// on the Menu's [open], so dismissing that Menu on an outside click, or
// covering the page with the card's tap-to-close layer, would close the
// whole navigation on a tap at a group label or the rail's empty space.
// The control: a dropdown in the page IS dismissed by the same click,
// so rastrillo.js is running and saw it. It is in the page rather than
// in the bar's tail because the tail's open panel would lie over the
// rail and take the click itself.
func TestAnOldConsoleLayoutKeepsItsRailOpen(t *testing.T) {
	pages := map[string]string{"/": shellLayoutPage(t, legacyLayout(t, "console"), "ltr",
		`{{define "nav"}}<p rst-shell-group id="group">Sales</p><a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a>{{end}}`,
		`{{define "content"}}<h1>Invoices</h1><details rst-dropdown id="page-menu" name="rst-menus"><summary>Sort</summary><div rst-dropdown-menu><a href="/go/new">Newest</a></div></details>{{end}}`)}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	type reading struct {
		Menu, Dropdown, Rail bool
	}
	const read = `JSON.stringify({Menu: document.querySelector("[rst-shell-menu]").open, Dropdown: document.querySelector("#page-menu").open,
	  Rail: document.querySelector("#nav-invoices").getClientRects().length > 0})`
	for _, c := range []struct {
		where  string
		fx, fy float64
		sel    string
	}{
		{"a group label", 0.5, 0.5, "#group"},
		// The rail's padding under its last link: rail, and nothing else.
		{"the rail's empty space", 0.5, 1, "[rst-shell-rail]"},
	} {
		mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery))
		requirePointer(t, ctx, true)
		mustRun(t, ctx, chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery), chromedp.WaitVisible("#nav-invoices", chromedp.ByQuery),
			chromedp.Click("#page-menu > summary", chromedp.ByQuery), chromedp.WaitVisible("#page-menu [rst-dropdown-menu]", chromedp.ByQuery))
		dy := 0.0
		if c.fy == 1 {
			dy = -6
		}
		p := probe(t, ctx, c.sel, c.fx, c.fy, 0, dy)
		mustRun(t, ctx, chromedp.MouseClickXY(p.X, p.Y))
		settleUntil(t, ctx, `!document.querySelector("#page-menu").open`)
		var got reading
		at(t, ctx, read, &got)
		if !got.Menu || !got.Rail {
			t.Errorf("a tap on %s (over %s) in an old console layout: Menu open %v, rail shown %v; want both, the rail is that layout's only navigation", c.where, p.Hit, got.Menu, got.Rail)
		}
	}
}

// backStripJS reads the back strip's bottom edge, the fragment target's
// top edge (both as the viewport sees them, after any landing scroll
// has settled), and the root's resolved scroll-padding-block-start, so
// one probe serves both the narrow leg and the wide one. -1 stands in
// for an element the page does not have, which the narrow leg's content
// page always does and the wide leg's assertion never looks at.
const backStripJS = `(() => {
  const back = document.querySelector("[rst-shell-back]"), target = document.getElementById("target");
  return JSON.stringify({
    BackBottom: back ? Math.round(back.getBoundingClientRect().bottom) : -1,
    TargetTop: target ? Math.round(target.getBoundingClientRect().top) : -1,
    ScrollPadding: getComputedStyle(document.documentElement).scrollPaddingBlockStart,
  });
})()`

type backStripReading struct {
	BackBottom, TargetTop int
	ScrollPadding         string
}

// TestAFragmentLinkLandsBelowTheBackStrip: the back strip is sticky at
// the top of a phone content page (tokens.css, just above), so the
// browser's native landing scroll for a #fragment link parks the
// target's top edge at the document root's scroll origin — which a
// sticky strip then covers, because a sticky element is not part of the
// flow a plain scroll-to-element measures against. A reader who follows
// a link to a heading finds the heading hidden under Back.
//
// Both shells, at 390 with a coarse pointer; and, because the fix must
// not reach past the media query that gates the strip itself, a second
// leg per shell at 1280 holding the root's scroll-padding unchanged.
func TestAFragmentLinkLandsBelowTheBackStrip(t *testing.T) {
	content := `{{define "content"}}<h1>Invoices</h1><div style="block-size: 150vh"></div><h2 id="target">Target</h2><div style="block-size: 150vh"></div>{{end}}`
	for _, shell := range []string{"sidebar", "console"} {
		src, _ := Layout(shell)
		pages := map[string]string{"/page": shellLayoutPage(t, src, "ltr", content)}

		t.Run(shell+" at 390", func(t *testing.T) {
			rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/page#target"), chromedp.WaitVisible("#target", chromedp.ByQuery))
			requirePointer(t, ctx, true)
			settled(t, ctx)
			var got backStripReading
			at(t, ctx, backStripJS, &got)
			if got.BackBottom < 0 || got.TargetTop < 0 {
				t.Fatalf("the back strip or #target is missing from the page: %+v", got)
			}
			if got.TargetTop < got.BackBottom {
				t.Errorf("#target's top is %dpx and the strip's bottom is %dpx; the heading landed %dpx under the strip", got.TargetTop, got.BackBottom, got.BackBottom-got.TargetTop)
			}
			// Exactly at the strip's edge, not merely below it: a scroll
			// padding that kept an old strip height, or guessed a bigger
			// one, leaves a gap or a hidden band whenever the strip grows.
			if got.TargetTop > got.BackBottom+1 {
				t.Errorf("#target's top is %dpx and the strip's bottom is %dpx; the heading landed %dpx below the strip, so the scroll padding is not the strip's height", got.TargetTop, got.BackBottom, got.TargetTop-got.BackBottom)
			}
		})

		t.Run(shell+" at 1280 is unchanged", func(t *testing.T) {
			rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/page#target"), chromedp.WaitReady("body"))
			var got backStripReading
			at(t, ctx, backStripJS, &got)
			if got.ScrollPadding != "auto" && got.ScrollPadding != "0px" {
				t.Errorf("the root's scroll-padding-block-start at 1280 is %q, want auto or 0px: the narrow-screen fix must not reach the desktop", got.ScrollPadding)
			}
		})
	}
}

// headerStripJS measures a phone header strip and the tap target in
// it: the strip's border box and the target's, as the viewport sees
// them, so a target that is 44px but pinned to the strip's top edge
// reads as off centre rather than passing.
const headerStripJS = `((strip, target) => {
  const s = document.querySelector(strip), c = document.querySelector(target);
  if (!s || !c || !s.checkVisibility() || !c.checkVisibility()) return JSON.stringify({Missing: true});
  const sr = s.getBoundingClientRect(), cr = c.getBoundingClientRect();
  return JSON.stringify({Top: sr.top, H: sr.height, TargetH: cr.height, Off: (cr.top + cr.height / 2) - (sr.top + sr.height / 2)});
})(%q, %q)`

type headerStripReading struct {
	Missing         bool
	Top, H, TargetH float64
	Off             float64
}

// TestThePhoneHeaderStripIs56Pixels: on a phone every strip a shell
// puts at the top of the screen is 56px, the height of a mobile app's
// top bar. At 44px the back strip was the bare tap target with no room
// round it, and read as cramped next to every native app's header. The
// tap target inside stays 44px and sits on the strip's centre line: a
// taller strip with the control at its top edge would make the strip's
// lower part a dead zone that looks tappable. The index's header row
// and a content page's back strip are both checked, so going from one
// to the other does not change the header's height under the slide.
func TestThePhoneHeaderStripIs56Pixels(t *testing.T) {
	type strip struct{ where, page, strip, target string }
	shells := map[string][]strip{
		"sidebar": {
			{"the back strip", "/page", "[rst-shell-back]", "[rst-shell-back] > a"},
			{"the index's header row", "/", "[rst-shell-title]", "[rst-shell-profile] > summary"},
		},
		"console": {
			{"the bar over a page", "/page", "[rst-shell-bar]", "[rst-shell-menu] > summary"},
			{"the back strip", "/page", "[rst-shell-back]", "[rst-shell-back] > a"},
			{"the index's header row", "/", "[rst-shell-bar]", "[rst-shell-menu] > summary"},
		},
	}
	for shell, strips := range shells {
		src, _ := Layout(shell)
		pages := map[string]string{
			"/":     shellLayoutPage(t, src, "ltr", append(profileDefs("Harbour"), `{{define "view"}}index{{end}}`)...),
			"/page": shellLayoutPage(t, src, "ltr", profileDefs("Harbour")...),
		}
		t.Run(shell, func(t *testing.T) {
			rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			for _, s := range strips {
				mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+s.page), chromedp.WaitReady("body"))
				requirePointer(t, ctx, true)
				settled(t, ctx)
				var got headerStripReading
				at(t, ctx, fmt.Sprintf(headerStripJS, s.strip, s.target), &got)
				if got.Missing {
					t.Fatalf("%s on %s: %s or %s is not shown; the leg would be checking nothing", s.where, s.page, s.strip, s.target)
				}
				if math.Abs(got.H-56) > 0.5 || got.TargetH < 44 || math.Abs(got.Off) > 1 {
					t.Errorf("%s on %s is %.1fpx tall with a %.1fpx target %.1fpx off its centre line; want a 56px strip with a 44px target centred in it", s.where, s.page, got.H, got.TargetH, got.Off)
				}
				if s.page == "/" && math.Abs(got.Top) > 0.5 {
					t.Errorf("%s starts %.1fpx down the page; want it at the top, where a content page's strip is", s.where, got.Top)
				}
			}
		})
	}
}
