//go:build browser

// Browser drives for the console shell — the fourth page frame, and the
// only one with two pieces of chrome to fold rather than one.
//
// Run with the rest of the family:
//
//	go test -tags browser -p 1 ./harness/ ./ui/ ./internal/designsystem/
//
// Every drive in this file carries a CONTROL: a second page, served by
// the same mux, measured by the same JavaScript, in the same browser,
// in the same run — differing from the page under test by the few
// declarations that ARE the thing being gated. The control has to come
// out the other way. Design spec §7-v2: a drive that finds nothing must
// be demonstrated finding something, or its zero cannot be told apart
// from a broken instrument. This project has shipped twelve gates that
// gated nothing, and the most recent survived independent reproduction
// by the simple expedient of nobody asking it a question whose answer
// was already known.
//
// There is no script under test here. The pages are markup, tokens.css
// and a theme; the shell's own <script defer> tags resolve to addresses
// this mux does not serve, exactly as the other shell drives leave
// them. Every state change below is a click on a <summary>.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/harness"
	"github.com/chromedp/chromedp"
)

// consoleReading is one measurement of a console page. Everything here
// is read off getBoundingClientRect and window.innerHeight — geometry
// the engine computed, never a claim about the text of a rule.
type consoleReading struct {
	// The chrome, and how much of it a reader can see.
	MenuShown        bool
	MenuOpen         bool
	TailShown        bool
	RailShown        bool
	VisibleSummaries int
	AccountMenu      bool

	// The wide frame.
	TailEndPx      int
	BarAboveRail   bool
	RailBeforeMain bool
	// And the narrow one: the bar's tail above the rail's nav, which is
	// also their DOM order. Nothing is reordered at any width.
	TailAboveRail bool

	// The viewport question. NavHeight is the border box, because
	// max-block-size is a promise about the window and the window
	// measures border boxes — that is the whole of the regression this
	// gate exists for.
	Viewport    int
	NavTop      int
	NavBottom   int
	NavHeight   int
	NavOverhang int
	NavScrolls  int

	Overflow int
	Dir      string

	// The legacy-engine readings. RailBottom against MainBottom is the
	// discriminator between "the rail is in its declared grid area"
	// (which spans the page row AND the footer row) and "the rail was
	// auto-placed into row 2" (which stops where main stops). They look
	// identical in a screenshot.
	RailBottom    int
	MainBottom    int
	RailBorderEnd string
	NavLinksShown int
}

// consoleMeasure is the one instrument. Both drives use it, and so does
// each drive's control, which is what makes the control a control: a
// different reading of the same measurement rather than a different
// measurement.
const consoleMeasure = `(() => {
  const shown = el => { if (!el) return false; const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
  const de = document.documentElement;
  const bar = document.querySelector("[rst-shell-bar]");
  const menu = document.querySelector("[rst-shell-menu]");
  const rail = document.querySelector("[rst-shell-rail]");
  const nav = document.querySelector("[rst-shell-nav]");
  const account = document.querySelector("[rst-shell-account]");
  const locale = document.querySelector("#bar-locale");
  const main = document.querySelector("[rst-shell-main]");
  const panel = account.querySelector("[rst-dropdown-menu]");
  const br = bar.getBoundingClientRect();
  const rr = rail.getBoundingClientRect();
  const nr = nav.getBoundingClientRect();
  const mr = main.getBoundingClientRect();
  const ar = account.getBoundingClientRect();
  // The inline-end gap of the LAST thing on the bar. Measured on the
  // locale menu rather than on the account, because the account is not
  // last: margin-inline-start: auto pushes the whole tail to the end
  // and the locale menu is what lands against it. Measuring the account
  // would read the width of the word "Language" and call it a layout.
  const lr = locale.getBoundingClientRect();
  const ltr = getComputedStyle(de).direction !== "rtl";
  // Every <summary> a reader can see anywhere in the document. The
  // claim "one control" is a count, not an assertion about one element:
  // a second disclosure added beside this one would satisfy every
  // other reading here and fail only this.
  let summaries = 0;
  document.querySelectorAll("summary").forEach(s => { if (shown(s)) summaries++; });
  return JSON.stringify({
    MenuShown: shown(menu.querySelector("summary")),
    MenuOpen: menu.open,
    TailShown: shown(account) || shown(locale),
    RailShown: shown(rail),
    VisibleSummaries: summaries,
    AccountMenu: shown(panel),
    TailEndPx: Math.round(ltr ? br.right - lr.right : lr.left - br.left),
    BarAboveRail: Math.round(br.bottom) <= Math.round(rr.top) + 1,
    RailBeforeMain: ltr ? Math.round(rr.right) <= Math.round(mr.left) + 1
                        : Math.round(rr.left) >= Math.round(mr.right) - 1,
    TailAboveRail: Math.round(ar.bottom) <= Math.round(rr.top) + 1,
    Viewport: window.innerHeight,
    NavTop: Math.round(nr.top),
    NavBottom: Math.round(nr.bottom),
    NavHeight: Math.round(nr.height),
    NavOverhang: Math.round(nr.bottom - window.innerHeight),
    NavScrolls: nav.scrollHeight - nav.clientHeight,
    Overflow: de.scrollWidth - de.clientWidth,
    Dir: getComputedStyle(de).direction,
    RailBottom: Math.round(rr.bottom),
    MainBottom: Math.round(mr.bottom),
    RailBorderEnd: getComputedStyle(rail).borderInlineEndWidth,
    NavLinksShown: [...nav.querySelectorAll("a")].filter(shown).length
  });
})()`

// consoleControlCSS is the control's whole difference from the page
// under test, and each half re-creates a bug this project has actually
// shipped rather than one imagined for the occasion.
//
// The first half defeats the :has() rule that hides the rail while the
// disclosure is closed — the failure "the tail collapsed and the rail
// did not", which is what a console with two disclosures and one
// forgotten looks like.
//
// The second half is the rail regression itself, verbatim: a box sized
// against the viewport that also carries padding, as a CONTENT box, so
// its border box is 32px taller than the window it promised to fit.
// The gate that missed it asked where the person sat inside the rail
// and got a true answer about a rail hanging off the screen.
const consoleControlCSS = `
@media (max-width: 799px) {
  [rst-shell-console] > [rst-shell-rail] { display: flex !important; }
}
@media (min-width: 800px) {
  [rst-shell-console] > [rst-shell-rail] > [rst-shell-nav] {
    block-size: 100dvh !important;
    box-sizing: content-box !important;
    max-block-size: none !important;
    padding: 1rem !important;
  }
}`

// consolePage renders the console shell with a given nav and direction,
// and returns the page and its control twin.
func consolePage(t *testing.T, nav, dir string) (page, control string) {
	t.Helper()
	src, ok := Layout("console")
	if !ok {
		t.Fatal("no console layout")
	}
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	template.Must(tmpl.Parse(`{{define "content"}}<p>Content.</p>{{end}}`))
	template.Must(tmpl.Parse(`{{define "dir"}}` + dir + `{{end}}`))
	template.Must(tmpl.Parse(`{{define "nav"}}` + nav + `{{end}}`))
	template.Must(tmpl.Parse(`{{define "account"}}<a href="#">Profile</a><a href="#">Sign out</a>{{end}}`))
	template.Must(tmpl.Parse(`{{define "locale"}}<details rst-dropdown rst-locale id="bar-locale" name="rst-menus"><summary>Language</summary><div rst-dropdown-menu><a href="#" lang="en">English</a><a href="#" lang="ga">Gaeilge</a></div></details>{{end}}`))
	template.Must(tmpl.Parse(`{{define "foot"}}<a href="#">Made with rastrillo</a>{{end}}`))

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", nil); err != nil {
		t.Fatalf("rendering the console shell: %v", err)
	}
	page = buf.String()
	const head = "</head>"
	i := strings.Index(page, head)
	if i < 0 {
		t.Fatal("the console shell rendered no </head> to inject the control's stylesheet into")
	}
	control = page[:i] + "<style>" + consoleControlCSS + "</style>" + page[i:]
	return page, control
}

// consoleServe puts the page at / and its control twin at /control.
func consoleServe(t *testing.T, page, control string) *harness.Rig {
	t.Helper()
	mux := http.NewServeMux()
	stylesheets(t, mux)
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, body)
		}
	}
	mux.HandleFunc("GET /control", serve(control))
	mux.HandleFunc("GET /", serve(page))
	return harness.New(t, func(string) http.Handler { return mux })
}

func readConsole(t *testing.T, raw string) consoleReading {
	t.Helper()
	var got consoleReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading the measurement (%q): %v", raw, err)
	}
	return got
}

// TestTheConsoleFoldsBothChromesBehindOneControl is the design work of
// the fourth shell, measured.
//
// Every other shell has ONE thing to put away below 800px. This one has
// two — the bar's tail and the navigation rail — and the wrong answer
// is two disclosures, one for "the account" and one for "the
// navigation", stacked on a phone for a reader to learn. So: one
// <details rst-shell-menu>, gating its own next sibling with + and the
// rail from the shell root with :has(). One [open], two reveals.
//
// The claims, in the order they matter:
//
//  1. Wide, both chromes are drawn and neither is behind anything: bar
//     across the top, rail beside the page, account at the bar's inline
//     end. This is the layout the shell exists for.
//  2. Narrow and closed, ONE control. Not "a control" — exactly one
//     visible summary in the whole document, which is the assertion a
//     second disclosure would fail and nothing else here would.
//  3. One click reveals BOTH. If a future edit splits them, the tail
//     opens and the rail stays hidden, and this is where that shows.
//  4. THE TRAP. The account menu is <details name="rst-menus">.
//     <details name> exclusivity is document-wide rather than
//     sibling-scoped, so a disclosure in that group would be closed by
//     the menu it just revealed — and in this shell that takes the rail
//     with it, because the rail is gated on the same [open].
//  5. Nothing is reordered at any width, so the reader's eye and the
//     tab key agree: bar, then rail, at 320px and at 1280px, in both
//     directions of the language.
//
// THE CONTROL, and what it is for: /control is this page with the
// rail's hide rule defeated. Leg 2's reading of RailShown must be false
// on the page and TRUE on the control. A drive whose RailShown is
// hard-wired false — a stale selector, a rail that never rendered,
// getBoundingClientRect on a detached node — passes leg 2 and fails
// here, which is the whole point.
func TestTheConsoleFoldsBothChromesBehindOneControl(t *testing.T) {
	page, control := consolePage(t, `<a href="#" aria-current="page">Posts</a><a href="#">Drafts</a><a href="#">Settings</a>`, "ltr")
	rig := consoleServe(t, page, control)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	at := func(t *testing.T, w, h int, path string, before ...chromedp.Action) consoleReading {
		t.Helper()
		acts := []chromedp.Action{
			chromedp.EmulateViewport(int64(w), int64(h)),
			chromedp.Navigate(rig.Origin + path),
			chromedp.WaitVisible(`[rst-shell-bar]`, chromedp.ByQuery),
		}
		acts = append(acts, before...)
		var raw string
		acts = append(acts, chromedp.Evaluate(consoleMeasure, &raw))
		if err := chromedp.Run(ctx, acts...); err != nil {
			t.Fatalf("driving %s at %dx%d: %v", path, w, h, err)
		}
		return readConsole(t, raw)
	}

	// 1. Wide: both chromes, no control, and the frame in the right
	//    shape.
	wide := at(t, 1280, 900, "/")
	t.Logf("wide 1280x900: menu=%v tail=%v rail=%v summaries=%d barAboveRail=%v railBeforeMain=%v tailEnd=%dpx",
		wide.MenuShown, wide.TailShown, wide.RailShown, wide.VisibleSummaries, wide.BarAboveRail, wide.RailBeforeMain, wide.TailEndPx)
	if wide.MenuShown {
		t.Error("the collapse control is drawn at 1280px; above 800px both chromes are laid out and it has nothing to do")
	}
	if !wide.TailShown || !wide.RailShown {
		t.Fatalf("at 1280px tail=%v rail=%v: the console is not showing both chromes, which is the only reason this shell exists", wide.TailShown, wide.RailShown)
	}
	if !wide.BarAboveRail {
		t.Error("the bar is not above the rail at 1280px; a console is a bar ACROSS the top with the rail beneath it")
	}
	if !wide.RailBeforeMain {
		t.Error("the rail is not on the inline-start side of the page column at 1280px")
	}
	if wide.TailEndPx > 40 {
		t.Errorf("the bar's tail ends %dpx short of the bar's inline end; margin-inline-start: auto is no longer reaching it (the bar's own inline padding is 16px)", wide.TailEndPx)
	}
	if wide.Overflow > 1 {
		t.Errorf("the wide console spills %dpx sideways at 1280px", wide.Overflow)
	}

	// 1b. And at exactly 800px, the breakpoint itself: a min-width query
	//     matches AT its own width, so 800 is wide and 799 is narrow. An
	//     off-by-one here is a window width with neither layout.
	edge := at(t, 800, 780, "/")
	if edge.MenuShown || !edge.TailShown || !edge.RailShown {
		t.Errorf("at exactly 800px control=%v tail=%v rail=%v; a min-width query matches at its own width, so 800px is the wide layout",
			edge.MenuShown, edge.TailShown, edge.RailShown)
	}

	// 2. Narrow and closed: one control, and both chromes behind it.
	closed := at(t, 390, 780, "/")
	t.Logf("narrow 390x780 closed: menu=%v tail=%v rail=%v visible summaries=%d",
		closed.MenuShown, closed.TailShown, closed.RailShown, closed.VisibleSummaries)
	if !closed.MenuShown {
		t.Fatal("there is no collapse control at 390px: the console has no narrow layout")
	}
	if closed.TailShown {
		t.Error("the bar's account and language menus are still drawn at 390px with the disclosure closed")
	}
	if closed.RailShown {
		t.Error("the navigation rail is still drawn at 390px with the disclosure closed; the tail folded and the rail did not")
	}
	if closed.VisibleSummaries != 1 {
		t.Errorf("a reader at 390px meets %d disclosures, want exactly 1. Two chromes fold behind ONE control here; a second summary is a second thing to learn and is the answer this shell was designed against",
			closed.VisibleSummaries)
	}
	if closed.Overflow > 1 {
		t.Errorf("the collapsed console spills %dpx sideways at 390px", closed.Overflow)
	}

	// 2b. THE CONTROL for leg 2. Same instrument, same viewport, same
	//     closed disclosure — one stylesheet apart. RailShown must come
	//     out the other way, or the false above proves nothing.
	ctrl := at(t, 390, 780, "/control")
	if !ctrl.RailShown {
		t.Errorf("CONTROL FAILED: the control page defeats the rule that hides the rail while the disclosure is closed, and this drive still reports the rail hidden (rail=%v, summaries=%d). "+
			"The measurement is not reading the rail, so leg 2's pass says nothing about the shell", ctrl.RailShown, ctrl.VisibleSummaries)
	}
	if !ctrl.MenuShown || ctrl.MenuOpen {
		t.Errorf("CONTROL FAILED: the control is meant to differ from the page only in the rail's hide rule, and its disclosure reads shown=%v open=%v", ctrl.MenuShown, ctrl.MenuOpen)
	}

	// 3. One click, both reveals.
	open := at(t, 390, 780, "/",
		chromedp.Click(`[rst-shell-menu] > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`[rst-shell-rail] [rst-shell-nav] a`, chromedp.ByQuery),
	)
	if !open.TailShown {
		t.Error("opening the one control revealed the rail but not the bar's tail")
	}
	if !open.RailShown {
		t.Error("opening the one control revealed the bar's tail but not the rail; the :has() half of the fold is not working")
	}
	if !open.TailAboveRail {
		t.Error("the disclosed panel puts the rail's navigation above the bar's tail; the DOM order is bar-then-rail and nothing here reorders it, so this is a layout that has drifted from the reading order")
	}
	if open.Overflow > 1 {
		t.Errorf("the opened console spills %dpx sideways at 390px", open.Overflow)
	}

	// 4. The trap: the account menu must not close the disclosure it
	//    sits behind, and must not take the rail with it.
	var trapRaw string
	if err := chromedp.Run(ctx,
		// Settle rather than WaitVisible: the failure this step exists
		// for is the account menu never appearing, because it closed the
		// disclosure it lives behind. Waiting would report that as a
		// timeout, and a timeout says nothing about <details name>.
		chromedp.Click(`[rst-shell-account] > summary`, chromedp.ByQuery),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(consoleMeasure, &trapRaw),
	); err != nil {
		t.Fatalf("opening the account menu inside the collapsed console: %v", err)
	}
	trap := readConsole(t, trapRaw)
	if !trap.MenuOpen {
		t.Error("opening the account menu CLOSED the disclosure it sits behind: the two <details> are in one exclusivity group, and <details name> exclusivity is document-wide rather than sibling-scoped")
	}
	if !trap.TailShown {
		t.Error("the bar's tail vanished when the account menu opened")
	}
	if !trap.RailShown {
		t.Error("the RAIL vanished when the account menu opened. This shell gates the rail on the same [open] as the tail, so one wrong name attribute takes both chromes away at once — which is why rst-shell-menu is a group of its own")
	}
	if !trap.AccountMenu {
		t.Error("the account menu's panel is not drawn inside the collapsed console")
	}

	// 5. The smallest viewport the shells promise, with everything open.
	tiny := at(t, 320, 640, "/",
		chromedp.Click(`[rst-shell-menu] > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`[rst-shell-rail] [rst-shell-nav] a`, chromedp.ByQuery),
	)
	if tiny.Overflow > 1 {
		t.Errorf("the opened console spills %dpx sideways at 320px", tiny.Overflow)
	}
	if !tiny.TailShown || !tiny.RailShown {
		t.Errorf("at 320px, opened: tail=%v rail=%v", tiny.TailShown, tiny.RailShown)
	}

	// 6. And the mirror. Every rule in this shell is a logical property
	//    and the wide frame is a named grid area, so RTL should need no
	//    second rule — which is a claim, and claims get measured.
	rtlPage, rtlControl := consolePage(t, `<a href="#" aria-current="page">Posts</a><a href="#">Drafts</a>`, "rtl")
	rtlRig := consoleServe(t, rtlPage, rtlControl)
	rtlCtx, rtlCancel := context.WithTimeout(rtlRig.Context(), 60*time.Second)
	defer rtlCancel()
	var rtlRaw string
	if err := chromedp.Run(rtlCtx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rtlRig.Origin+"/"),
		chromedp.WaitVisible(`[rst-shell-bar]`, chromedp.ByQuery),
		chromedp.Evaluate(consoleMeasure, &rtlRaw),
	); err != nil {
		t.Fatalf("driving the mirrored console: %v", err)
	}
	rtl := readConsole(t, rtlRaw)
	if rtl.Dir != "rtl" {
		t.Fatalf("the mirrored page reports direction %q; the fixture is not mirrored and legs below prove nothing", rtl.Dir)
	}
	if !rtl.RailBeforeMain {
		t.Error("in RTL the rail is not on the inline-start (right) side of the page column: the grid areas are not following the writing mode")
	}
	if rtl.TailEndPx > 40 {
		t.Errorf("in RTL the bar's tail ends %dpx short of the bar's inline end; the auto margin is not following the writing mode", rtl.TailEndPx)
	}
	if rtl.Overflow > 1 {
		t.Errorf("the mirrored console spills %dpx sideways at 1280px", rtl.Overflow)
	}
}

// TestTheConsoleRailFitsTheViewport is the gate this project earned the
// hard way, asked of the fourth shell before it ships rather than after.
//
// The regression: a rail with `padding` and `block-size: 100dvh` under
// content-box has a BORDER box 32px taller than the window it was sized
// against. Sticky at the top, its last 32px hung under the foot of the
// screen at every scroll position, with the person block in them.
// overflow-y: auto was no defence — the content fitted the content box,
// so no scrollbar was ever owed — and the gate that missed it asserted
// that the person sat at the foot OF THE RAIL, which stayed perfectly
// true while the rail hung off the screen.
//
// So this drive asks the viewport, not the container:
//
//  1. The nav's BORDER box is never taller than the window.
//  2. Scrolled to the foot of the document, the whole of it — top edge
//     and bottom edge — is inside the window. Nothing in it is
//     unreachable at every scroll position, which is what the
//     regression actually cost.
//  3. And the fixture is doing its job: the nav is longer than the cap,
//     so the cap is engaged. A "it fits" from a nav with three links in
//     it is a measurement of nothing.
//
// THE CONTROL: /control is this page with the regression put back,
// declaration for declaration. The same three readings must come out
// the other way there. A drive that reports "fits" because it is
// measuring a detached node, or the wrong element, or a box that never
// laid out, passes 1–3 and fails the control.
func TestTheConsoleRailFitsTheViewport(t *testing.T) {
	page, control := consolePage(t, railTallNav, "ltr")
	rig := consoleServe(t, page, control)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	// A deliberately SHORT window, which is where the regression lived:
	// a viewport-sized box overhangs by its padding whatever the window
	// height is, but a short window is where a reader meets it and
	// where twenty links certainly exceed the cap.
	const w, h = 1280, 420

	measureAt := func(t *testing.T, path string, scroll bool) consoleReading {
		t.Helper()
		acts := []chromedp.Action{
			chromedp.EmulateViewport(w, h),
			chromedp.Navigate(rig.Origin + path),
			chromedp.WaitVisible(`[rst-shell-rail] [rst-shell-nav] a`, chromedp.ByQuery),
		}
		if scroll {
			acts = append(acts,
				chromedp.Evaluate(`window.scrollTo(0, document.documentElement.scrollHeight); "ok"`, new(string)),
				chromedp.Sleep(250*time.Millisecond))
		}
		var raw string
		acts = append(acts, chromedp.Evaluate(consoleMeasure, &raw))
		if err := chromedp.Run(ctx, acts...); err != nil {
			t.Fatalf("driving %s at %dx%d (scrolled=%v): %v", path, w, h, scroll, err)
		}
		return readConsole(t, raw)
	}

	// Logged before anything is asserted, and named against what it is
	// supposed to be compared to: the number that gave the regression
	// away was already in a passing test's output — "a 932px rail" at a
	// 900px viewport — and nobody read it, because nothing said what it
	// should have been.
	top := measureAt(t, "/", false)
	bottom := measureAt(t, "/", true)
	t.Logf("console rail, %dx%d viewport: nav border box %dpx (viewport %dpx), %dpx of it scrolls inside itself; at scroll top %d..%d, at document foot %d..%d",
		w, h, top.NavHeight, top.Viewport, top.NavScrolls, top.NavTop, top.NavBottom, bottom.NavTop, bottom.NavBottom)

	// 3 first, because 1 and 2 mean nothing without it.
	if top.NavScrolls <= 0 {
		t.Fatalf("the nav is %dpx and scrolls %dpx inside itself: it is SHORTER than the cap, so this drive is measuring a box that was never constrained. The fixture has to overflow or the fit proves nothing",
			top.NavHeight, top.NavScrolls)
	}
	// 1. The border box against the window. This is the regression's own
	//    shape, and it is a claim about the box the window measures.
	//
	//    Its caveat, recorded where it lives rather than in a report
	//    nobody greps: in THIS shell the padding is on the rail, not on
	//    the nav, so a content-box mutation alone does not inflate the
	//    nav's border box and this leg does not fire on it — leg 2 is
	//    what catches that one, by 16px. The leg is kept for three
	//    reasons: it fires the day somebody gives the nav padding of its
	//    own, which is the exact edit that shipped the regression the
	//    first time; it is the reading the control below pins to the
	//    pixel (452 = 420 + 32), which is what proves the height
	//    measurement is live rather than inert; and its message names
	//    the fix, which leg 2's cannot.
	if top.NavHeight > top.Viewport {
		t.Errorf("the rail's nav is %dpx in a %dpx window: %dpx taller than the viewport it is sized against, so whatever is at its foot is under the fold at every scroll position. box-sizing: border-box on the capped box is the fix",
			top.NavHeight, top.Viewport, top.NavHeight-top.Viewport)
	}
	// 2. And reachability, which is what the reader actually lost.
	if bottom.NavTop < 0 || bottom.NavBottom > bottom.Viewport {
		t.Errorf("scrolled to the foot of the document the rail's nav occupies %d..%dpx of a %dpx window: part of it is off-screen with nothing left to scroll to reach it",
			bottom.NavTop, bottom.NavBottom, bottom.Viewport)
	}

	// THE CONTROL. Same drive, same instrument, same window — the
	// regression put back in a <style> block. All three readings must
	// invert. If they do not, this test's zero is a broken harness and
	// not a shell that fits.
	ctop := measureAt(t, "/control", false)
	cbottom := measureAt(t, "/control", true)
	t.Logf("control (the regression restored): nav border box %dpx (viewport %dpx), scrolls %dpx; at scroll top %d..%d, at document foot %d..%d",
		ctop.NavHeight, ctop.Viewport, ctop.NavScrolls, ctop.NavTop, ctop.NavBottom, cbottom.NavTop, cbottom.NavBottom)

	if ctop.NavHeight <= ctop.Viewport {
		t.Errorf("CONTROL FAILED: the control page sizes the nav at 100dvh as a content box with 1rem of padding, so its border box must be %dpx taller than the %dpx window — and this drive measured %dpx. The height reading is not measuring a border box against the viewport, so the pass above says nothing",
			32, ctop.Viewport, ctop.NavHeight)
	}
	if cbottom.NavTop >= 0 && cbottom.NavBottom <= cbottom.Viewport {
		t.Errorf("CONTROL FAILED: the control's oversized nav cannot fit a %dpx window at any scroll position, and this drive reports it fitting at %d..%dpx. The reachability reading is inert",
			cbottom.Viewport, cbottom.NavTop, cbottom.NavBottom)
	}
	// And the control differs from the page in exactly the way it says
	// it does, arithmetically: block-size: 100dvh on a content box with
	// 1rem of padding is a border box of viewport + 32, to the pixel.
	// This is the calibration proper — not "the control failed" but
	// "the control failed BY THE PREDICTED AMOUNT". An instrument that
	// reports some other number is measuring some other box, and its
	// agreement with the page above would be luck.
	if want := ctop.Viewport + 32; ctop.NavHeight != want {
		t.Errorf("CONTROL FAILED: the control's nav should measure exactly %dpx (a %dpx content box plus 2×1rem of padding) and this drive read %dpx. The height reading is not the border box of the element the rule names",
			want, ctop.Viewport, ctop.NavHeight)
	}
}

// hasRulePattern matches one CSS rule whose selector list mentions
// :has(). Crude on purpose: it is a simulation of an engine, and the
// engine's rule is coarser than a parser's — see stripHas.
var hasRulePattern = regexp.MustCompile(`(?s)[^{}]*\{[^{}]*\}`)

// stripHas returns tokens.css as an engine that does not implement
// :has() would see it.
//
// The important part is what it drops, which is more than the selector.
// A selector list is invalid AS A WHOLE if any selector in it is
// invalid, so such an engine drops the entire rule — every declaration
// in it, including the ones that have nothing to do with :has(), and
// including the ones the OTHER selectors in the list were carrying.
// That is the finding this drive was written for: the console's wide
// rail rule used to co-list two plain selectors with two :has() ones,
// so a legacy engine lost `grid-area: rail` along with the fight it was
// having about `display`, and the rail landed in the right cell only
// because grid auto-placement happened to agree with the declared area.
// It looked correct. It was luck.
func stripHas(css string) (string, int) {
	dropped := 0
	// Comments come out FIRST, using ui_test.go's own cssComment. A
	// browser tokenises them away before it ever looks at a selector,
	// and the first version of this function did not: the wide
	// layout's comment explains why the :has() spelling is repeated
	// there, so the characters ":has(" sat in the text immediately
	// before a rule that does not use it and the stripper took the
	// rule. The wide legs of the drive below caught it, which is the
	// useful thing to say about them — they are not only a gate on the
	// stylesheet, they are the control on this function.
	css = cssComment.ReplaceAllString(css, "")
	out := hasRulePattern.ReplaceAllStringFunc(css, func(rule string) string {
		prelude, _, ok := strings.Cut(rule, "{")
		if !ok || !strings.Contains(prelude, ":has(") {
			return rule
		}
		dropped++
		return "\n"
	})
	return out, dropped
}

// TestTheConsoleDegradesTheWayItSaysItDoesWithoutHas is the gate on the
// sentence the shell, tokens.css and docs/site/templates.md all make:
// that this shell's dependence on :has() has a CHOSEN failure
// direction.
//
// The claim has two halves, and only one of them was ever true by
// accident:
//
//  1. Narrow, the rail stays VISIBLE. The rules are written as
//     hide-when-closed, so the rule an old engine drops is the one that
//     would have hidden the navigation. The page gets longer; nothing
//     becomes unreachable. Spelled the other way round, the same
//     missing selector would be a phone that cannot navigate.
//  2. Wide, the frame is UNTOUCHED. Nothing about the console's grid
//     depends on :has() — and that has to be true of the rules as
//     written, not merely of the intent, which is what a co-listed
//     selector quietly took away.
//
// Half 2 is measured on the discriminator rather than on appearance:
// the rail's declared area spans the page row AND the footer row, so
// its bottom edge is below main's. An auto-placed rail stops where main
// stops. The two are indistinguishable in a screenshot and this is the
// only reading that tells them apart.
//
// THE CONTROL, twice over. The strip itself is checked to have dropped
// rules that matter (a strip that silently did nothing would make every
// reading below a reading of the ordinary page), and the same narrow
// reading is taken on the REAL stylesheet, where the rail must be
// hidden. If the rail reads visible on both, the visibility is the
// fixture's and not the degradation's.
func TestTheConsoleDegradesTheWayItSaysItDoesWithoutHas(t *testing.T) {
	real := string(TokensCSS())
	legacy, dropped := stripHas(real)

	// Control on the instrument before anything is measured with it.
	if dropped == 0 {
		t.Fatal("the :has()-less simulation dropped no rules at all: it is serving the ordinary stylesheet under another name, and every reading below is a reading of the page as it already is")
	}
	if strings.Contains(legacy, ":has(") {
		t.Fatal("the :has()-less simulation left a :has() selector in the stylesheet; it is not the engine it claims to simulate")
	}
	consoleRules := strings.Count(legacy, "rst-shell-console")
	if consoleRules == 0 {
		t.Fatal("the simulation dropped every console rule in the file; the shell has no stylesheet left and the legs below would be measuring an unstyled document")
	}
	t.Logf(":has()-less simulation: %d rules dropped, %d console selectors survive", dropped, consoleRules)

	page, _ := consolePage(t, `<a href="#" aria-current="page">Posts</a><a href="#">Drafts</a><a href="#">Settings</a>`, "ltr")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, real)
	})
	mux.HandleFunc("GET /legacy-tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, legacy)
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /legacy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, strings.Replace(page, `href="/tokens.css"`, `href="/legacy-tokens.css"`, 1))
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()

	at := func(t *testing.T, w, h int, path string) consoleReading {
		t.Helper()
		var raw string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(int64(w), int64(h)),
			chromedp.Navigate(rig.Origin+path),
			chromedp.WaitVisible(`[rst-shell-bar]`, chromedp.ByQuery),
			chromedp.Evaluate(consoleMeasure, &raw),
		); err != nil {
			t.Fatalf("driving %s at %dx%d: %v", path, w, h, err)
		}
		return readConsole(t, raw)
	}

	// 2. Wide, on the legacy stylesheet: the frame is the declared one.
	lw := at(t, 1280, 900, "/legacy")
	t.Logf("legacy engine, 1280x900: rail shown=%v barAbove=%v beforeMain=%v borderInlineEnd=%s rail bottom %dpx vs main bottom %dpx",
		lw.RailShown, lw.BarAboveRail, lw.RailBeforeMain, lw.RailBorderEnd, lw.RailBottom, lw.MainBottom)
	if !lw.RailShown || !lw.TailShown {
		t.Fatalf("without :has() the wide console loses its chrome: rail=%v tail=%v", lw.RailShown, lw.TailShown)
	}
	if !lw.BarAboveRail || !lw.RailBeforeMain {
		t.Errorf("without :has() the wide frame is not bar-over-rail-beside-page: barAbove=%v beforeMain=%v", lw.BarAboveRail, lw.RailBeforeMain)
	}
	if lw.RailBorderEnd == "0px" {
		t.Error("without :has() the rail has no inline-end border: the wide rail rule is being dropped whole, which means it is co-listing plain selectors with :has() ones again")
	}
	// THE DISCRIMINATOR.
	if lw.RailBottom <= lw.MainBottom {
		t.Errorf("without :has() the rail ends at %dpx and main ends at %dpx: the rail is sitting in the page's row rather than in the area declared for it, which spans the footer row too. "+
			"That is grid auto-placement agreeing with the declared area by coincidence — the wide rail rule is being dropped whole. Keep the :has() selectors in a rule of their own",
			lw.RailBottom, lw.MainBottom)
	}

	// 1. Narrow, on the legacy stylesheet: the rail is visible, whole,
	//    and the page still fits.
	ln := at(t, 390, 780, "/legacy")
	t.Logf("legacy engine, 390x780 closed: rail shown=%v with %d/3 links visible, control shown=%v, overflow=%dpx",
		ln.RailShown, ln.NavLinksShown, ln.MenuShown, ln.Overflow)
	if !ln.RailShown {
		t.Error("without :has() the rail is HIDDEN at 390px: the hide rule is the one that survived, so the chosen failure direction is inverted and a reader on an old phone has no navigation at all")
	}
	if ln.NavLinksShown != 3 {
		t.Errorf("without :has() the disclosed-by-default rail shows %d of 3 navigation links at 390px", ln.NavLinksShown)
	}
	if ln.Overflow > 1 {
		t.Errorf("without :has() the console spills %dpx sideways at 390px", ln.Overflow)
	}

	// THE CONTROL for leg 1: the same reading on the REAL stylesheet,
	// where the answer is known and is the opposite one.
	rn := at(t, 390, 780, "/")
	if rn.RailShown {
		t.Errorf("CONTROL FAILED: on the real stylesheet the rail must be hidden at 390px with the disclosure closed, and this drive reports it shown. " +
			"The visible rail on the legacy page is then the fixture's doing and not the degradation's, and leg 1 measures nothing")
	}
}
