//go:build browser

package ui

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
)

// prerenderSite is a sidebar app served through rastrillo.Handler, so
// the headers are the framework's own (the CSP included): the index,
// two sections, a plain link in the index's content, a stage page with
// a link on it, and a page in a sidebar layout copied before the phone
// index. Every document request is reported with its
// Sec-Purpose, which is how a prerender is seen (headless Chromium
// fetches the page for it; enabling CDP's Preload domain would switch
// prerendering off, measured).
func prerenderSite(t *testing.T, noRules bool, hits chan<- string) func(string) http.Handler {
	return func(string) http.Handler {
		src, _ := Layout("sidebar")
		stage, _ := Layout("stage")
		pages := map[string]string{
			"/":         shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "content"}}<h1>Harbour</h1><p><a id="plain" href="/plain">A link in the page</a></p>{{end}}`),
			"/invoices": shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-invoices{{end}}`),
			"/orders":   shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-orders{{end}}`),
			"/team":     shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-team{{end}}`),
			"/plain":    shellLayoutPage(t, src, "ltr"),
			"/signin":   shellLayoutPage(t, stage, "ltr", `{{define "content"}}<p><a id="stage-link" href="/orders">Orders</a></p>{{end}}`),
			"/legacy":   shellLayoutPage(t, legacyLayout(t, "sidebar"), "ltr"),
		}
		mux := shellAssets(t, pages)
		h, closeAll, err := rastrillo.Handler(rastrillo.Options{Mux: recordDocuments(mux, hits), NoSpeculationRules: noRules})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeAll() })
		return h
	}
}

// recordDocuments reports every request for a page (not an asset) with
// its Sec-Purpose.
func recordDocuments(next *http.ServeMux, hits chan<- string) *http.ServeMux {
	out := http.NewServeMux()
	out.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ".") {
			select {
			case hits <- r.URL.Path + " " + r.Header.Get("Sec-Purpose"):
			default:
			}
		}
		next.ServeHTTP(w, r)
	})
	return out
}

func drain(c chan string) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

// waitFor reports the first hit for path within d, or "".
func waitFor(c chan string, path string, d time.Duration) string {
	deadline := time.After(d)
	for {
		select {
		case h := <-c:
			if strings.HasPrefix(h, path+" ") {
				return h
			}
		case <-deadline:
			return ""
		}
	}
}

func centre(t *testing.T, ctx context.Context, sel string) (float64, float64) {
	p := probe(t, ctx, sel, 0.5, 0.5, 0, 0)
	return p.X, p.Y
}

// landsOn is centre plus the check centre skips: a negative leg proves
// nothing if the pointer never reached the element it claims to have
// missed, so these assert the probe's hit before trusting a "no
// prerender" reading. wantID is the element's id attribute, which is
// what probe reports for an element that has one.
func landsOn(t *testing.T, ctx context.Context, sel, wantID string) (float64, float64) {
	t.Helper()
	p := probe(t, ctx, sel, 0.5, 0.5, 0, 0)
	if p.Hit != wantID {
		t.Fatalf("the probe point for %s landed on %q, not %q; a hover there proves nothing", sel, p.Hit, wantID)
	}
	return p.X, p.Y
}

// TestShellNavigationIsPrerendered drives the two ways a browser starts
// a speculation: a hover on a nav link (desktop) and a pointer-down on
// one (phone) start a prerender of it, with no CSP violation; a link
// outside the shell's nav and back control starts none; a stage page
// and an old layout's nav prerender nothing; and with Options.NoSpeculationRules the same
// hover starts nothing, which is the control that says the header is
// what did it.
func TestShellNavigationIsPrerendered(t *testing.T) {
	hits := make(chan string, 64)
	rig := harness.New(t, prerenderSite(t, false, hits))
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	rig.Run(chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"))
	rig.Screen("[rst-shell-nav]", "the index, served with the framework's CSP")
	drain(hits)
	x, y := centre(t, ctx, "#nav-invoices")
	mustRun(t, ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/invoices", 4*time.Second); !strings.Contains(h, "prerender") {
		t.Errorf("a hover on a nav link started no prerender (got %q)", h)
	}
	x, y = landsOn(t, ctx, "#plain", "plain")
	mustRun(t, ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/plain", 2*time.Second); h != "" {
		t.Errorf("a link outside the shell's navigation was prerendered: %q", h)
	}
	rig.Run(chromedp.Navigate(rig.Origin + "/signin"))
	rig.Screen("#stage-link", "the stage page")
	drain(hits)
	x, y = landsOn(t, ctx, "#stage-link", "stage-link")
	mustRun(t, ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/orders", 2*time.Second); h != "" {
		t.Errorf("a stage page prerendered %q; the rules match nothing outside the sidebar and console", h)
	}
	// An app's own layout.html, copied before the phone index, has no
	// view on its root, and its pages were written with no thought of
	// running before they are shown: a startup script there would run on
	// a hover. Its nav links must start nothing.
	rig.Run(chromedp.Navigate(rig.Origin + "/legacy"))
	rig.Screen("[rst-shell-nav]", "a sidebar layout copied before the phone index")
	drain(hits)
	x, y = landsOn(t, ctx, "#nav-team", "nav-team")
	mustRun(t, ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/team", 3*time.Second); h != "" {
		t.Errorf("a layout copied before the phone index prerendered %q; only a root that names its view may", h)
	}

	touchHits := make(chan string, 64)
	touch := harness.New(t, prerenderSite(t, false, touchHits), harness.WithCoarsePointer())
	tctx, tcancel := context.WithTimeout(touch.Context(), 60*time.Second)
	defer tcancel()
	touch.Run(chromedp.EmulateViewport(390, 844), chromedp.Navigate(touch.Origin+"/"))
	touch.Screen("[rst-shell-nav]", "the phone index")
	requirePointer(t, tctx, true)
	drain(touchHits)
	x, y = centre(t, tctx, "#nav-orders")
	mustRun(t, tctx, chromedp.MouseEvent(input.MousePressed, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	if h := waitFor(touchHits, "/orders", 4*time.Second); !strings.Contains(h, "prerender") {
		t.Errorf("a pointer-down on a nav row on a phone started no prerender (got %q)", h)
	}
	mustRun(t, tctx, chromedp.MouseEvent(input.MouseReleased, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))

	offHits := make(chan string, 64)
	off := harness.New(t, prerenderSite(t, true, offHits))
	octx, ocancel := context.WithTimeout(off.Context(), 60*time.Second)
	defer ocancel()
	off.Run(chromedp.EmulateViewport(1280, 900), chromedp.Navigate(off.Origin+"/"))
	drain(offHits)
	x, y = landsOn(t, octx, "#nav-invoices", "nav-invoices")
	mustRun(t, octx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(offHits, "/invoices", 3*time.Second); h != "" {
		t.Errorf("CONTROL: with NoSpeculationRules a hover still fetched %q, so the header is not what the legs above measured", h)
	}
}

// TestAPrefetchedIndexStillReturnsFocus: headless Chromium does not
// activate a prerender (it delivers the page from the prefetch,
// measured: deliveryType "navigational-prefetch"), so this is the
// nearest a headless drive gets to the prerendered-index leg: a deep
// link's back control, pressed long enough for the rules to fetch the
// index, lands on an index delivered from the speculation, with focus
// on the section it left. The true activation leg stays on the by-hand
// list.
func TestAPrefetchedIndexStillReturnsFocus(t *testing.T) {
	hits := make(chan string, 64)
	rig := harness.New(t, prerenderSite(t, false, hits), harness.WithCoarsePointer())
	ctx, done, thrown := tab(t, rig, "")
	defer done()
	visit(t, ctx, rig.Origin+"/invoices")
	settleUntil(t, ctx, `!!document.querySelector("[rst-shell-back] a")`)
	x, y := centre(t, ctx, "[rst-shell-back] a")
	mustRun(t, ctx, chromedp.MouseEvent(input.MousePressed, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	if h := waitFor(hits, "/", 4*time.Second); !strings.Contains(h, "prerender") {
		t.Fatalf("pressing the back control fetched no speculation of the index (got %q); the leg has not arisen", h)
	}
	mustRun(t, ctx, chromedp.MouseEvent(input.MouseReleased, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	settleUntil(t, ctx, `location.pathname === "/"`)
	var delivery string
	mustRun(t, ctx, chromedp.Evaluate(`performance.getEntriesByType("navigation")[0].deliveryType`, &delivery))
	if delivery != "navigational-prefetch" {
		t.Fatalf("the index was delivered as %q, not from the speculation; the leg has not arisen", delivery)
	}
	settleUntil(t, ctx, `document.activeElement.id === "nav-invoices"`)
	// scriptErrors, not a raw length check: the back-control navigation
	// is the same shape as TestThePhoneIndexWorksWithScripts's, which
	// headless Chromium itself skips the incoming transition for on
	// roughly one navigation in five (see scriptErrors's comment) —
	// an unfiltered check would flake on exactly that.
	errs, _ := scriptErrors(t, *thrown)
	if len(errs) > 0 {
		t.Errorf("uncaught: %v", errs)
	}
}
