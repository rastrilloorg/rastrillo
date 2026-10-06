//go:build browser

package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// shellDoc is a page in the vocabulary the sidebar and console layouts
// write for shell.js: a root marked with its view, a back link, a rail
// of nav links, and a main with a same-page link. The markup is by hand
// because this drive is about the script. It links shell.css but not
// tokens.css, so what is measured is shell.js and not the stylesheet's
// narrow layout: a link the stylesheet hides cannot take focus, and the
// drive would fail for a reason that has nothing to do with the script.
func shellDoc(view, up, nav, main string) string {
	return `<!doctype html><html lang="en" dir="ltr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>shell</title>` +
		`<link rel="stylesheet" href="/shell.css">` +
		`<script defer blocking="render" src="/shell.js"></script></head><body>` +
		`<div rst-shell-sidebar="` + view + `"><div rst-shell-back><a id="back" href="` + up + `" rel="up">Sections</a></div>` +
		`<aside rst-shell-rail><nav rst-shell-nav>` + nav + `</nav></aside>` +
		`<main rst-shell-main id="main">` + main + `</main></div></body></html>`
}

// shellSite is three small apps on one origin. / has ids on its nav and
// fragments on its up links; /b/ has neither (focus return must come
// from the record alone); /q/ has an index with a query, nav hrefs
// written absolute, and an id that needs decoding. /old redirects to /,
// the "up that redirects" case, and /self is its own up.
func shellSite(t *testing.T) func(origin string) http.Handler {
	return func(origin string) http.Handler {
		nav := `<a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a><a id="nav-r" href="/r/page">Redirecting</a>`
		content := func(name string) string {
			return `<h1>` + name + `</h1><p><a id="section-link" href="#part">To a part of this page</a></p><div style="block-size: 150vh"></div><h2 id="part">Part</h2>`
		}
		navB := `<a href="/b/invoices">Invoices</a><a href="/b/orders">Orders</a>`
		navQ := `<a id="nav-q-orders" href="` + origin + `/q/orders?status=open">Orders</a><a id="nav-café" href="/q/cafe">Café</a>`
		pages := map[string]string{
			"/":           shellDoc("index", "/", nav, `<h1>Home</h1>`),
			"/invoices":   shellDoc("page", "/#nav-invoices", nav, content("Invoices")),
			"/orders":     shellDoc("page", "/#nav-orders", nav, content("Orders")),
			"/r/page":     shellDoc("page", "/old#nav-r", nav, content("Redirecting")),
			"/self":       shellDoc("page", "/self", nav, content("Self")),
			"/b/":         shellDoc("index", "/b/", navB, `<h1>B</h1>`),
			"/b/invoices": shellDoc("page", "/b/", navB, content("B invoices")),
			"/q/":         shellDoc("index", "/q/?tab=all", navQ, `<h1>Q</h1>`),
			"/q/orders":   shellDoc("page", "/q/?tab=all#nav-q-orders", navQ, content("Q orders")),
			"/q/cafe":     shellDoc("page", "/q/?tab=all#nav-caf%C3%A9", navQ, content("Café")),
			// A content page whose back strip also holds a menu, the way
			// the gallery puts its display settings there, with a link to
			// the very page the back control points at.
			"/strip": strings.Replace(shellDoc("page", "/#nav-invoices", nav, content("Strip")),
				`rel="up">Sections</a></div>`,
				`rel="up">Sections</a><details open><summary>Settings</summary><a id="strip-menu-link" href="/?theme=other">Other theme</a></details></div>`, 1),
		}
		mux := http.NewServeMux()
		stylesheets(t, mux)
		for name, body := range map[string][]byte{"shell.js": ShellJS(), "shell.css": ShellCSS()} {
			body, ct := body, map[bool]string{true: "text/javascript", false: "text/css"}[strings.HasSuffix(name, ".js")]
			mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", ct)
				w.Write(body)
			})
		}
		mux.HandleFunc("GET /old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusFound) })
		for path, html := range pages {
			html, pattern := html, "GET "+path
			if strings.HasSuffix(path, "/") {
				pattern += "{$}"
			}
			mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, html)
			})
		}
		return mux
	}
}

// tab opens a fresh tab (a fresh session history, so "what is behind
// this entry" is only what the leg put there) at 390 wide, with an
// optional script run in every document before the page's own.
func tab(t *testing.T, rig *harness.Rig, init string, media ...*emulation.MediaFeature) (context.Context, func(), *[]string) {
	t.Helper()
	ctx, cancel := chromedp.NewContext(rig.Context())
	ctx, stop := context.WithTimeout(ctx, 90*time.Second)
	thrown := &[]string{}
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*runtime.EventExceptionThrown); ok {
			*thrown = append(*thrown, e.ExceptionDetails.Error())
		}
	})
	acts := []chromedp.Action{chromedp.EmulateViewport(390, 844)}
	if len(media) > 0 {
		acts = append(acts, emulation.SetEmulatedMedia().WithFeatures(media))
	}
	if init != "" {
		acts = append(acts, chromedp.ActionFunc(func(c context.Context) error {
			_, err := cdppage.AddScriptToEvaluateOnNewDocument(init).Do(c)
			return err
		}))
	}
	if err := chromedp.Run(ctx, acts...); err != nil {
		t.Fatal(err)
	}
	return ctx, func() { stop(); cancel() }, thrown
}

type shellState struct {
	Path, Focus string
	Len         int
}

// state is where the tab is, how long its history is, and what has
// focus (an id, else the href, else the tag).
func state(t *testing.T, ctx context.Context) shellState {
	t.Helper()
	var s shellState
	at(t, ctx, `JSON.stringify({Path: location.pathname + location.search + location.hash, Len: history.length,
	  Focus: (a => a.id || a.getAttribute("href") || a.tagName)(document.activeElement)})`, &s)
	return s
}

// visit navigates with a real load; follow clicks through the page,
// which is what works after a back/forward-cache restore (no load event
// fires there, so chromedp's own waits would hang).
func visit(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}
}

// follow clicks sel from a timer, so the evaluation returns before the
// document it runs in is replaced, then waits until the destination's
// own marker holds.
func follow(t *testing.T, ctx context.Context, sel, until string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`setTimeout(() => document.querySelector(%q).click(), 0), true`, sel), nil)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, until)
}

// revealed waits until the page has loaded and painted twice more, by
// which time pagereveal has fired and shell.js has done whatever it was
// going to do with focus. A leg that asserts focus did NOT move needs
// this, or it would pass by reading too early.
func revealed(t *testing.T, ctx context.Context) {
	t.Helper()
	settleUntil(t, ctx, `document.readyState === "complete"`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))`, nil,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatal(err)
	}
}

// TestTheBackControlReusesHistoryWhenItCanProveWhatIsBehind drives the
// scripted journeys through the index and its pages: when the back
// control may reuse history, when it must follow its link instead, and
// where focus lands on the index afterwards.
func TestTheBackControlReusesHistoryWhenItCanProveWhatIsBehind(t *testing.T) {
	rig := harness.New(t, shellSite(t))

	t.Run("index, page, back: history reused, focus on the link followed", func(t *testing.T) {
		ctx, done, thrown := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/" && document.activeElement.id === "nav-invoices"`)
		if after := state(t, ctx); after.Len != before.Len || after.Path != "/" || after.Focus != "nav-invoices" {
			t.Errorf("after the back control: %+v (before it %+v); want history unchanged, at /, focus on nav-invoices", after, before)
		}
		if len(*thrown) > 0 {
			t.Errorf("uncaught: %v", *thrown)
		}
	})

	t.Run("index, page, #fragment, back: the link is followed", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		follow(t, ctx, "#section-link", `location.hash === "#part"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/" && document.activeElement.id === "nav-invoices"`)
		if after := state(t, ctx); after.Len != before.Len+1 || after.Focus != "nav-invoices" {
			t.Errorf("back from a fragment on the same page: %+v (before %+v); want the link followed (history +1) and focus on nav-invoices", after, before)
		}
	})

	t.Run("a deep link, back: the link is followed and the record focuses", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/orders")
		follow(t, ctx, "#back", `location.pathname === "/"`)
		settleUntil(t, ctx, `document.activeElement.id === "nav-orders"`)
	})

	t.Run("no ids, no fragment: focus from the record alone", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/b/invoices")
		follow(t, ctx, "#back", `location.pathname === "/b/"`)
		settleUntil(t, ctx, `document.activeElement.getAttribute("href") === "/b/invoices"`)
	})

	const brokenStorage = `Object.defineProperty(window, "sessionStorage", {configurable: true, get() { throw new DOMException("denied", "SecurityError"); }});`
	// Chromium focuses a fragment's target itself when it loads a URL
	// with one, so landing on /#nav-invoices proves nothing about the
	// script. A history return to that URL does not refocus: the drive
	// blurs, leaves, and comes back, and only shell.js's fragment rule
	// can put focus on the link then.
	t.Run("storage throws, with a fragment: focus from the fragment", func(t *testing.T) {
		ctx, done, thrown := tab(t, rig, brokenStorage)
		defer done()
		visit(t, ctx, rig.Origin+"/invoices")
		follow(t, ctx, "#back", `location.pathname === "/" && document.activeElement.id === "nav-invoices"`)
		follow(t, ctx, "#nav-orders", `location.pathname === "/orders"`)
		back(t, ctx, `location.pathname === "/"`)
		settleUntil(t, ctx, `document.activeElement.id === "nav-invoices"`)
		if len(*thrown) > 0 {
			t.Errorf("storage that throws broke the script: %v", *thrown)
		}
	})

	t.Run("storage throws, no fragment: browser default, nothing thrown", func(t *testing.T) {
		ctx, done, thrown := tab(t, rig, brokenStorage)
		defer done()
		visit(t, ctx, rig.Origin+"/b/invoices")
		follow(t, ctx, "#back", `location.pathname === "/b/"`)
		revealed(t, ctx)
		if s := state(t, ctx); s.Focus != "BODY" {
			t.Errorf("with no record and no fragment, focus is on %q, want the browser's default", s.Focus)
		}
		if len(*thrown) > 0 {
			t.Errorf("uncaught: %v", *thrown)
		}
	})

	t.Run("modified and middle clicks are not intercepted", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		for _, init := range []string{"{ctrlKey: true}", "{metaKey: true}", "{shiftKey: true}", "{altKey: true}", "{button: 1}"} {
			var prevented bool
			// A listener on window runs after shell.js's on document,
			// records whether it was intercepted, then stops the real
			// navigation so the next click starts from the same page.
			js := `(() => { let seen = null; addEventListener("click", e => { seen = e.defaultPrevented; e.preventDefault(); }, {once: true});
			  document.getElementById("back").dispatchEvent(new MouseEvent("click", Object.assign({bubbles: true, cancelable: true, button: 0}, ` + init + `)));
			  return seen; })()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(js, &prevented)); err != nil {
				t.Fatal(err)
			}
			if prevented {
				t.Errorf("a click with %s on the back control was intercepted; it must do what the browser does", init)
			}
		}
		// The control: a plain click IS intercepted here.
		var plain bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { let seen = null; addEventListener("click", e => { seen = e.defaultPrevented; }, {once: true});
		  document.getElementById("back").dispatchEvent(new MouseEvent("click", {bubbles: true, cancelable: true, button: 0})); return seen; })()`, &plain)); err != nil {
			t.Fatal(err)
		}
		if !plain {
			t.Error("CONTROL: a plain click on the back control was not intercepted either, so the modifier legs prove nothing")
		}
	})

	t.Run("an up that redirects: the link is followed", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-r", `location.pathname === "/r/page"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/"`)
		if after := state(t, ctx); after.Len != before.Len+1 {
			t.Errorf("an up behind a redirect: history %d -> %d; the link must be followed, not history reused", before.Len, after.Len)
		}
	})

	t.Run("two sections in a row: the record beats a stale fragment", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/invoices")
		follow(t, ctx, "#back", `location.pathname === "/" && location.hash === "#nav-invoices"`)
		follow(t, ctx, "#nav-orders", `location.pathname === "/orders"`)
		back(t, ctx, `location.pathname === "/" && document.activeElement.id === "nav-orders"`)
	})

	t.Run("query, absolute hrefs and an encoded id", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/q/?tab=all")
		follow(t, ctx, "#nav-q-orders", `location.pathname === "/q/orders"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/q/" && document.activeElement.id === "nav-q-orders"`)
		if after := state(t, ctx); after.Len != before.Len {
			t.Errorf("an up with a query and an absolute nav href: history %d -> %d; path+query were not compared resolved", before.Len, after.Len)
		}
		// The encoded id, through the history return the fragment leg
		// above explains: location.hash keeps the percent-encoding, and
		// only a decoded id finds the link.
		ctx2, done2, _ := tab(t, rig, brokenStorage)
		defer done2()
		visit(t, ctx2, rig.Origin+"/q/cafe")
		follow(t, ctx2, "#back", `location.pathname === "/q/" && document.activeElement.id === "nav-café"`)
		follow(t, ctx2, "#nav-q-orders", `location.pathname === "/q/orders"`)
		back(t, ctx2, `location.pathname === "/q/"`)
		settleUntil(t, ctx2, `document.activeElement.id === "nav-café"`)
	})

	// An engine with cross-document view transitions but no types: the
	// stub, installed before shell.js's listener, hands every reveal a
	// transition without a types set. The slide's direction is cosmetic;
	// the focus return must still happen and nothing may throw.
	t.Run("a transition with no types: focus still returns", func(t *testing.T) {
		const untyped = `addEventListener("pagereveal", e => Object.defineProperty(e, "viewTransition", {value: {}}));`
		ctx, done, thrown := tab(t, rig, untyped)
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		follow(t, ctx, "#back", `location.pathname === "/"`)
		settleUntil(t, ctx, `document.activeElement.id === "nav-invoices"`)
		if len(*thrown) > 0 {
			t.Errorf("a transition without types broke the script: %v", *thrown)
		}
	})

	// A record left by a content page is taken by the next index revealed,
	// whatever its width. Left behind by a wide reveal, it would focus a
	// stale link on the next phone-width index reached without leaving a
	// page (here, a reload of the index).
	t.Run("a wide reveal consumes the record", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 800)); err != nil {
			t.Fatal(err)
		}
		visit(t, ctx, rig.Origin+"/b/invoices")
		follow(t, ctx, "#back", `location.pathname === "/b/"`)
		revealed(t, ctx)
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844)); err != nil {
			t.Fatal(err)
		}
		visit(t, ctx, rig.Origin+"/b/")
		revealed(t, ctx)
		if s := state(t, ctx); s.Focus != "BODY" {
			t.Errorf("a phone-width index reached by reload focused %q: a record from a wide reveal was left behind", s.Focus)
		}
	})

	// An index left in the default page view carries a back control whose
	// up is the page itself. After a same-page #fragment, the entry behind
	// is that same URL but the same document, and going back to it would
	// be a scroll with no reveal: no slide, no focus return. The link is
	// followed instead.
	t.Run("an up that is this document: the link is followed", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/self")
		follow(t, ctx, "#section-link", `location.hash === "#part"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/self" && location.hash === ""`)
		if after := state(t, ctx); after.Len != before.Len+1 {
			t.Errorf("an up behind a same-document entry: history %d -> %d; the link must be followed, not history reused", before.Len, after.Len)
		}
	})
}

// back is the browser's own Back button: a traversal the back control
// did not start, so shell.js has no one-shot flag to read. Before it,
// focus is dropped, so what has focus afterwards is what the returning
// page put there and not what the page kept from before it was left.
func back(t *testing.T, ctx context.Context, until string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement && document.activeElement.blur(), setTimeout(() => history.back(), 0), true`, nil)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, until)
}

// recordReveal logs every pagereveal's transition types into
// sessionStorage. This listener is installed before the page's own
// scripts, so it runs BEFORE shell.js's, when no type has been added
// yet: the types are read in log, which runs once the reveal is over
// (after the transition's finished promise settles, or on a timer when
// there is no transition), by which point every listener has run. The
// drive waits for each entry before it starts the next navigation, so
// no step begins while the previous slide is still running.
const recordReveal = `addEventListener("pagereveal", e => { const vt = e.viewTransition;
  const log = () => { const types = vt ? [...vt.types].join("+") || "untyped" : "none";
    const l = JSON.parse(sessionStorage.getItem("reveal-log") || "[]");
    l.push(location.pathname + ":" + types); sessionStorage.setItem("reveal-log", JSON.stringify(l)); };
  if (vt) vt.finished.then(log, log); else setTimeout(log, 0); });`

// TestTheSlideKnowsWhichWayItIsGoing: forward going in; back coming out
// through the back control, through the browser's own Back (which sets
// no flag, so only the Navigation API's traverse can say which way it
// went), and through a back control that follows its link (a new entry,
// not a traverse, so only the flag can); and no transition at all under
// reduced motion (shell.css never opts in).
func TestTheSlideKnowsWhichWayItIsGoing(t *testing.T) {
	rig := harness.New(t, shellSite(t))
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced motion %v", reduced), func(t *testing.T) {
			var media []*emulation.MediaFeature
			want := `["/:none","/invoices:forward","/:back","/orders:forward","/:back","/invoices:forward","/:back"]`
			if reduced {
				media = []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}
				want = `["/:none","/invoices:none","/:none","/orders:none","/:none","/invoices:none","/:none"]`
			}
			ctx, done, _ := tab(t, rig, recordReveal, media...)
			defer done()
			logged := func(n int) string {
				return fmt.Sprintf(`JSON.parse(sessionStorage.getItem("reveal-log") || "[]").length === %d`, n)
			}
			visit(t, ctx, rig.Origin+"/")
			settleUntil(t, ctx, logged(1))
			follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices" && `+logged(2))
			follow(t, ctx, "#back", `location.pathname === "/" && `+logged(3))
			follow(t, ctx, "#nav-orders", `location.pathname === "/orders" && `+logged(4))
			back(t, ctx, `location.pathname === "/" && `+logged(5))
			// A same-page fragment first, so the back control cannot
			// reuse history and follows its link.
			follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices" && `+logged(6))
			follow(t, ctx, "#section-link", `location.hash === "#part"`)
			follow(t, ctx, "#back", `location.pathname === "/" && `+logged(7))
			var log string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`sessionStorage.getItem("reveal-log")`, &log)); err != nil {
				t.Fatal(err)
			}
			if log != want {
				t.Errorf("pagereveal saw %s, want %s", log, want)
			}
		})
	}
}

// TestALinkBesideTheBackControlIsNotTheBackControl: a page may put more
// in the back strip than the back link (the gallery's display settings
// menu sits at its inline end), and those links are ordinary links. A
// click on one must not record a "back" for the slide, nor call
// history.back() when the entry behind happens to be the same page: it
// is followed, and history grows by one.
func TestALinkBesideTheBackControlIsNotTheBackControl(t *testing.T) {
	rig := harness.New(t, shellSite(t))
	init := `(() => { const set = Storage.prototype.setItem, back = History.prototype.back;
	  Storage.prototype.setItem = function (k, v) { if (k === "rst-shell-back") set.call(this, "__went_back", v); return set.call(this, k, v); };
	  History.prototype.back = function () { sessionStorage.setItem("__history_back", "1"); return back.call(this); }; })()`
	ctx, done, thrown := tab(t, rig, init)
	defer done()
	// The entry behind /strip is the very page its menu link names, so
	// the back control's own rule would reuse history for it.
	visit(t, ctx, rig.Origin+"/?theme=other")
	revealed(t, ctx)
	visit(t, ctx, rig.Origin+"/strip")
	revealed(t, ctx)
	before := state(t, ctx)
	follow(t, ctx, "#strip-menu-link", `location.pathname === "/" && location.search === "?theme=other"`)
	revealed(t, ctx)
	after := state(t, ctx)
	var rec struct{ WentBack, HistoryBack string }
	at(t, ctx, `JSON.stringify({WentBack: sessionStorage.getItem("__went_back") || "", HistoryBack: sessionStorage.getItem("__history_back") || ""})`, &rec)
	if rec.WentBack != "" || rec.HistoryBack != "" {
		t.Errorf("a menu link in the back strip was taken for the back control: recorded back %q, history.back called %q", rec.WentBack, rec.HistoryBack)
	}
	if after.Len != before.Len+1 {
		t.Errorf("history %d -> %d after the menu link; want it followed (+1)", before.Len, after.Len)
	}
	if errs, _ := scriptErrors(t, *thrown); len(errs) > 0 {
		t.Errorf("uncaught: %v", errs)
	}
}
