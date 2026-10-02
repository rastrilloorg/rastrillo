//go:build browser

package ui

import (
	"context"
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
    SkipDisplay: getComputedStyle(q("[rst-skip]")).display, Title: shown(title) ? title.textContent.trim() : "",
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

// scriptErrors is every uncaught exception except a skipped transition.
// Measured on this machine's Chromium with the real layouts: on roughly
// one navigation in five the incoming transition is skipped before
// pagereveal (the event carries no viewTransition, so no script ever saw
// it) and the browser reports its own unhandled promise. The skip
// happens with every deferred script the layouts load and not with
// shell.js alone, and navigation, history and focus are unaffected: the
// page simply arrives without the slide. Anything else thrown is still a
// failure.
func scriptErrors(t *testing.T, thrown []string) []string {
	t.Helper()
	var errs []string
	for _, e := range thrown {
		if strings.Contains(e, skippedTransition) && strings.Contains(e, "Uncaught (in promise)") {
			t.Logf("the browser skipped a transition (no slide this time): %s", e)
			continue
		}
		errs = append(errs, e)
	}
	return errs
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
				if shell == "sidebar" && pg.BackTop > 1 {
					t.Errorf("the sidebar's back control is %dpx from the top", pg.BackTop)
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
				if errs := scriptErrors(t, *thrown); len(errs) > 0 {
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
				if errs := scriptErrors(t, *deepThrown); len(errs) > 0 {
					t.Errorf("uncaught in the deep-link tab: %v", errs)
				}
			})
		}
	}
}

// boxesJS is the rail's and main's boxes, for comparing two layouts.
const boxesJS = `(() => { const b = s => { const r = document.querySelector(s).getBoundingClientRect(); return [r.left, r.top, r.width, r.height].map(Math.round).join(","); };
  return JSON.stringify({Rail: b("[rst-shell-rail]"), Main: b("[rst-shell-main]")}); })()`

// TestTheWideLayoutIsUnchanged: at 1280, in both directions, both views
// are exactly the old layout (the same markup rendered through the
// legacy layout is the golden), with no back control and no second h1.
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
			golden, _ := read("/legacy")
			for _, path := range []string{"/index", "/page"} {
				got, v := read(path)
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
// the bar, then the rail full screen, then the foot below it. With a
// two-link nav the rail is still at least the window's height and the
// foot follows it.
func TestTheConsoleIndexRailFillsTheScreen(t *testing.T) {
	src, _ := Layout("console")
	pages := map[string]string{"/": shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`,
		`{{define "nav"}}<a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a>{{end}}`,
		`{{define "foot"}}<a href="/about">About</a>{{end}}`)}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var g struct{ RailH, RailBottom, FootTop, FootBottom, VH, NavBottom float64 }
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body"))
	at(t, ctx, `(() => { const b = s => document.querySelector(s).getBoundingClientRect();
	  return JSON.stringify({RailH: b("[rst-shell-rail]").height, RailBottom: b("[rst-shell-rail]").bottom, NavBottom: b("[rst-shell-nav]").bottom, FootTop: b("[rst-shell-foot]").top, FootBottom: b("[rst-shell-foot]").bottom, VH: innerHeight}); })()`, &g)
	if g.NavBottom > g.VH/2 {
		t.Fatalf("the two-link nav ends at %.0f of %.0f; the short-nav case this leg is for has not arisen", g.NavBottom, g.VH)
	}
	if g.RailH < g.VH-1 {
		t.Errorf("the console index's rail is %.0fpx in a %.0fpx window; it is the page there, full screen", g.RailH, g.VH)
	}
	if math.Abs(g.FootTop-g.RailBottom) > 1 {
		t.Errorf("the foot starts at %.0f and the rail ends at %.0f; the foot follows the rail", g.FootTop, g.RailBottom)
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
