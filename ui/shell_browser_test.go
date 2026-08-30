//go:build browser

// Browser drives for the things a Go test cannot see, because they are
// geometry a real engine computes: whether a menu opened inside a card
// is actually painted outside it, whether a child that owns its own
// corners keeps them inside one, and where the sidebar rail puts the
// person and which way its language menu opens.
//
// Run with the rest of the family:
//
//	go test -tags browser -p 1 ./harness/ ./ui/ ./internal/designsystem/
//
// There is no script under test in this file at all: every page here
// is static markup plus tokens.css and a theme, and the only
// interaction is a click on a <details> summary, which the browser
// handles synchronously.
//
// For a while that made this file the only part of ./ui/ CI would run,
// because the script-driven drives in browser_test.go were red on
// GitHub's runner (issue #86). They are fixed, ./ui/ runs whole, and
// nothing here needs naming in the workflow any more —
// TestTheUIDrivesRunWholeInTheBrowserJob, in the plain suite, fails if
// anyone narrows it back down.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/harness"
)

// stylesheets wires the two routes every page in this file needs: the
// library stylesheet under test, and a theme to fill in its colour and
// radius tokens. Without the theme --rst-radius is undefined and the
// corner assertions below would read 0px for the wrong reason.
func stylesheets(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
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
}

// clipReading is what the clipping drive measures. Overhang and
// HitIsInMenu are the two halves of one claim: the menu really does
// hang below the card (or the hit test proves nothing), and the part
// that hangs below is really painted (or the card is still clipping).
type clipReading struct {
	Overhang     int
	HitIsInMenu  bool
	Hit          string
	FirstCorner  string
	LastCorner   string
	CardOverflow string
}

// TestAMenuOpenedInsideAListCardEscapesTheCard is the live-page bug
// Paul found: a bulk bar's Actions menu opened inside a .rst-list was
// sliced off at the card's edge.
//
// The cause was .rst-list's overflow: hidden, which was there to clip
// the rows' corners to the card's radius and, in doing so, made the
// card a clipping context for every absolutely positioned panel inside
// it.
//
// getBoundingClientRect is NOT the measurement: an ancestor's overflow
// does not change a descendant's layout rect, so the menu's rect hangs
// below the card whether or not it is visible. What clipping does
// change is hit testing, so the assertion is elementFromPoint at a
// spot below the card's own bottom edge: clipped, nothing of the menu
// is there; unclipped, the menu is.
//
// The second half is the fix's own risk. Dropping the overflow must
// not leave the first and last rows square inside a rounded card, so
// the corner radii are read back off the engine too.
func TestAMenuOpenedInsideAListCardEscapesTheCard(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	mux := http.NewServeMux()
	stylesheets(t, mux)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>list card</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css"></head><body>` +
			`<div class="rst-page"><form method="post" action="/act">` +
			`<div class="rst-list">`)
		data := map[string]any{
			"DoneHref": "/posts", "Count": "3 selected",
			"MenuLabel": "Actions",
			"Actions": []any{
				map[string]any{"Value": "export", "Label": "Export"},
				map[string]any{"Value": "unpublish", "Label": "Unpublish"},
				map[string]any{"Value": "delete", "Label": "Delete…", "Danger": true},
			},
		}
		if err := tmpl.ExecuteTemplate(&body, "bulk-bar", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body.WriteString(`</div></form></div></body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body.String())
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	var raw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-list .rst-bulkbar`, chromedp.ByQuery),
		// The menu is a native <details>: one real click on the
		// summary, no script anywhere.
		chromedp.Click(`.rst-list .rst-bulkbar details > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`.rst-list .rst-dropdown__menu`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
		  const card = document.querySelector(".rst-list");
		  const menu = document.querySelector(".rst-list .rst-dropdown__menu");
		  const c = card.getBoundingClientRect();
		  const m = menu.getBoundingClientRect();
		  // A point inside the menu and below the card's own bottom
		  // edge — the part a clipping card throws away.
		  const x = Math.round(m.left + m.width / 2);
		  const y = Math.round(c.bottom + Math.max(2, (m.bottom - c.bottom) / 2));
		  const hit = document.elementFromPoint(x, y);
		  const first = card.firstElementChild, last = card.lastElementChild;
		  return JSON.stringify({
		    Overhang: Math.round(m.bottom - c.bottom),
		    HitIsInMenu: !!(hit && menu.contains(hit)),
		    Hit: hit ? (hit.tagName.toLowerCase() + "." + (hit.className || "")) : "nothing",
		    FirstCorner: getComputedStyle(first).borderStartStartRadius,
		    LastCorner: getComputedStyle(last).borderEndEndRadius,
		    CardOverflow: getComputedStyle(card).overflowY
		  });
		})()`, &raw),
	); err != nil {
		t.Fatalf("driving the list card: %v", err)
	}

	var got clipReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading the measurement (%q): %v", raw, err)
	}

	// Sanity first: without an overhang the hit test below proves
	// nothing at all.
	if got.Overhang <= 4 {
		t.Fatalf("the open menu hangs only %dpx below the card; this drive cannot tell clipped from contained", got.Overhang)
	}
	if !got.HitIsInMenu {
		t.Errorf("a point %dpx of overhang inside the open menu hit %s, not the menu: the card is still clipping it (overflow-y: %s)",
			got.Overhang, got.Hit, got.CardOverflow)
	}
	// And the card still draws itself as a card: the rows it holds are
	// rounded to its corners rather than squared off at them.
	for _, corner := range []struct{ name, value string }{
		{"the first row's start corner", got.FirstCorner},
		{"the last row's end corner", got.LastCorner},
	} {
		if corner.value == "" || corner.value == "0px" {
			t.Errorf("%s is %q — dropping the card's overflow left its rows square inside a rounded border", corner.name, corner.value)
		}
	}
}

// railReading is where the sidebar shell's rail puts things, in the
// window Paul was looking at.
type railReading struct {
	RailHeight int
	FootGap    int
	MenuLift   int
	// The frame the first version of this drive did not have. FootGap
	// answers "is the person at the foot of the rail", which was true
	// on the day the rail overflowed the window by its own padding —
	// the rail was 932px in a 900px viewport, the number was in this
	// test's own passing output, and the person was at the foot of a box
	// whose last 32px were under the fold. Viewport and the two
	// overhangs are what turn a claim about the rail into a claim about
	// the window it has to fit.
	Viewport       int
	RailOverhang   int
	PersonOverhang int
	// Whether the rail overflows, and — a different question that was
	// once measured as if it were the same one — whether it SCROLLS.
	//
	// RailScroll is scrollHeight - clientHeight: how much content does
	// not fit. That is a fine reading and a useless assertion, because
	// overflow: visible reports it too — with overflow-y removed and a
	// twenty-link rail it still read 371px. So it is the PREMISE now:
	// it says the fixture is big enough for this leg to mean anything.
	//
	// The claim is the three below it. RailOverflowY is what the engine
	// computed. RailScrolled is where scrollTop actually landed after
	// the box was asked to go to its own bottom, which a non-scrollable
	// box clamps to 0. PersonOverhangScrolled is the promise itself:
	// after that scroll, is the thing below the fold on screen.
	RailScroll             int
	RailOverflowY          string
	RailScrolled           int
	PersonOverhangScrolled int
	LocaleBefore           bool
}

// railMeasure is the one reading every leg of the rail drive takes, so
// a viewport added below cannot quietly measure less than the ones
// above it — which is how the frame went missing the first time.
const railMeasure = `(() => {
  const rail = document.querySelector(".rst-shell__rail");
  const person = document.querySelector("#rail-person");
  const loc = document.querySelector("#rail-locale");
  const sum = loc.querySelector("summary");
  const menu = loc.querySelector(".rst-dropdown__menu");
  // Both scrollers wound back to the top first. chromedp.Click scrolls
  // its target into view, and the locale summary sits at the foot of
  // the rail — so on a rail that overflows, opening the language menu
  // has already scrolled something, and every "before" reading below
  // would be a reading of an already-scrolled box. Which one it
  // scrolled depends on the very property under test: the rail, if the
  // rail is a scroll container, and otherwise the whole page. Winding
  // both back is what makes the two cases comparable.
  window.scrollTo(0, 0);
  rail.scrollTop = 0;
  const r = rail.getBoundingClientRect();
  const p = person.getBoundingClientRect();
  const s = sum.getBoundingClientRect();
  const m = menu.getBoundingClientRect();
  // Every rect above is taken BEFORE the scroll probe below, because
  // scrolling the rail moves everything inside it and FootGap is a
  // reading of where the person sits in an unscrolled rail.
  const overflowY = getComputedStyle(rail).overflowY;
  const railScroll = rail.scrollHeight - rail.clientHeight;
  rail.scrollTop = rail.scrollHeight;
  const scrolled = Math.round(rail.scrollTop);
  const pAfter = person.getBoundingClientRect();
  const personAfter = Math.round(pAfter.bottom - window.innerHeight);
  rail.scrollTop = 0;
  return JSON.stringify({
    RailHeight: Math.round(r.height),
    FootGap: Math.round(r.bottom - p.bottom),
    MenuLift: Math.round(s.top - m.bottom),
    Viewport: window.innerHeight,
    RailOverhang: Math.round(r.bottom - window.innerHeight),
    RailScroll: railScroll,
    RailOverflowY: overflowY,
    RailScrolled: scrolled,
    PersonOverhang: Math.round(p.bottom - window.innerHeight),
    PersonOverhangScrolled: personAfter,
    LocaleBefore: !!(loc.compareDocumentPosition(person) & Node.DOCUMENT_POSITION_FOLLOWING)
  });
})()`

// TestTheSidebarRailPutsThePersonAtItsFootAndTheLanguageMenuOpensUpward
// is the other live-page bug: the sidebar shell left the person block
// mid-rail, directly under the nav, with acres of empty rail below it.
//
// Two claims, both measured rather than asserted about the CSS text:
//
//  1. The person sits at the rail's foot — the distance from the
//     bottom of the person block to the bottom of the rail is small.
//     Before the fix it was most of a 900px window.
//  2. The language switcher above it is a DROPUP. Its panel's bottom
//     edge is at or above its summary's top edge; a dropdown's would
//     be below it. Zero script does this: it is the same <details>
//     menu with inset-block-end: 100% instead of top: 100%.
func TestTheSidebarRailPutsThePersonAtItsFootAndTheLanguageMenuOpensUpward(t *testing.T) {
	src, ok := Layout("sidebar")
	if !ok {
		t.Fatal("no sidebar layout")
	}
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	template.Must(tmpl.Parse(`{{define "content"}}<p>Content.</p>{{end}}`))
	template.Must(tmpl.Parse(`{{define "nav"}}<a href="#" aria-current="page">Posts</a><a href="#">Drafts</a>{{end}}`))
	template.Must(tmpl.Parse(`{{define "account"}}<div class="rst-shell__account" id="rail-person"><a class="rst-person" href="#"><span class="rst-person__av" aria-hidden="true">G</span><span class="rst-person__meta"><span class="rst-person__name">Grace Hopper</span><span class="rst-person__email">grace@example.com</span></span></a></div>{{end}}`))
	template.Must(tmpl.Parse(`{{define "locale"}}<details class="rst-dropdown rst-locale" id="rail-locale" name="rst-menus"><summary>Language</summary><div class="rst-dropdown__menu"><a href="#" lang="en">English</a><a href="#" lang="ga">Gaeilge</a></div></details>{{end}}`))

	// Cloned BEFORE anything executes: html/template refuses to Clone a
	// tree that has already run.
	tall := template.Must(tmpl.Clone())

	var page strings.Builder
	if err := tmpl.ExecuteTemplate(&page, "layout", nil); err != nil {
		t.Fatalf("rendering the sidebar shell: %v", err)
	}
	html := page.String()

	// The same shell with a rail that does not fit a short window, for
	// the third leg below. A separate page rather than a taller nav on
	// the one above, because the first two legs assert numbers that a
	// twenty-link rail would move — "the person is at the foot" and
	// "the collapsed rail is not stretched" are claims about a rail
	// with room to spare, and they are the claims worth keeping.
	//
	// Twenty links is not decoration: the design system's own rail
	// carries about thirty, and the leg below exists because a rail
	// that overflows is the case a two-link fixture can never reach.
	// railTallNav is what makes it overflow at 420px, and the leg
	// FAILS if it stops doing so.
	template.Must(tall.Parse(`{{define "nav"}}` + railTallNav + `{{end}}`))
	var tallPage strings.Builder
	if err := tall.ExecuteTemplate(&tallPage, "layout", nil); err != nil {
		t.Fatalf("rendering the tall sidebar shell: %v", err)
	}
	tallHTML := tallPage.String()

	mux := http.NewServeMux()
	stylesheets(t, mux)
	mux.HandleFunc("GET /tall", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, tallHTML)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	var raw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`#rail-person`, chromedp.ByQuery),
		chromedp.Click(`#rail-locale > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`#rail-locale .rst-dropdown__menu`, chromedp.ByQuery),
		chromedp.Evaluate(railMeasure, &raw),
	); err != nil {
		t.Fatalf("driving the sidebar rail: %v", err)
	}

	var got railReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading the measurement (%q): %v", raw, err)
	}

	// Logged, not just asserted: the number that gave the regression
	// away was already in this test's passing output — "a 932px rail" at
	// a 900px viewport — and nobody read it, because nothing in the
	// output said what it was supposed to be compared to. It says so now.
	t.Logf("wide: rail %dpx in a %dpx viewport (rail overhang %dpx, person overhang %dpx), person %dpx above the rail's foot",
		got.RailHeight, got.Viewport, got.RailOverhang, got.PersonOverhang, got.FootGap)

	// Sanity: the rail is the full-height column this claim is about.
	if got.RailHeight < 600 {
		t.Fatalf("the rail is only %dpx tall in a 900px window; it is not the sticky full-height rail this drive measures", got.RailHeight)
	}

	// THE FRAME. Everything below asks where things sit inside the rail;
	// this asks whether the rail sits inside the window, which is the
	// question the first version of this drive never put and the one the
	// regression it missed was an answer to.
	//
	// block-size: 100dvh is a promise about the viewport, so the box
	// that has to keep it is the BORDER box: with the rail as a content
	// box its padding was added on top, the border box came to
	// 100dvh + 2×var(--rst-sp-4), and a rail sticky at
	// inset-block-start: 0 hung its last 32px under the foot of the
	// window. overflow-y: auto is no defence — the content fits the
	// content box, so no scrollbar is ever owed — and no assertion about
	// where the person sits INSIDE the rail can see it.
	if got.RailHeight > got.Viewport {
		t.Errorf("the rail's border box is %dpx in a %dpx viewport: it is %dpx taller than the window it is sized against, so whatever sits at its foot is under the fold. box-sizing on the rail is the fix",
			got.RailHeight, got.Viewport, got.RailHeight-got.Viewport)
	}
	if got.RailOverhang > 0 {
		t.Errorf("the sticky rail's bottom edge is %dpx below the foot of a %dpx window", got.RailOverhang, got.Viewport)
	}
	// And the symptom, measured where a reader met it: the person block
	// at the rail's foot, clipped by the window rather than by the rail.
	if got.PersonOverhang > 0 {
		t.Errorf("the person block at the rail's foot ends %dpx below the foot of the window: it is off-screen with nothing to scroll to reach it", got.PersonOverhang)
	}
	// A rail's padding is var(--rst-sp-4); anything inside a couple of
	// steps of the bottom edge is at the foot, anything further is not.
	if got.FootGap > 48 {
		t.Errorf("the person block ends %dpx above the foot of a %dpx rail; the profile is not at the rail's foot", got.FootGap, got.RailHeight)
	}
	if !got.LocaleBefore {
		t.Error("the person block comes before the language switcher in the rail; the switcher belongs directly above the profile")
	}
	if got.MenuLift < 0 {
		t.Errorf("the language menu's panel ends %dpx BELOW its summary's top edge: it is a dropdown, not the dropup a menu at the foot of the rail has to be", -got.MenuLift)
	}

	// And the collapse. Below 800px the rail is not a full-height
	// column at all. It is still a flex column — the media query adds
	// block-size: 100dvh and nothing else — but a content-height one
	// the chrome strip discloses, with the whole page under it, so the
	// auto margin has no free space to distribute and the foot simply
	// follows the nav. RailHeight is asserted below for that reason
	// rather than as a sanity check: it is what would catch a later
	// min-block-size on the disclosed rail, which would put the gap
	// back while FootGap stayed small. And the language menu goes back
	// to opening DOWNWARD, because up there is the nav and down there
	// is the rest of the page.
	var narrow string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 780),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-shell__chrome > summary`, chromedp.ByQuery),
		chromedp.Click(`.rst-shell__chrome > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`#rail-person`, chromedp.ByQuery),
		chromedp.Click(`#rail-locale > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`#rail-locale .rst-dropdown__menu`, chromedp.ByQuery),
		chromedp.Evaluate(railMeasure, &narrow),
	); err != nil {
		t.Fatalf("driving the collapsed rail: %v", err)
	}
	var small railReading
	if err := json.Unmarshal([]byte(narrow), &small); err != nil {
		t.Fatalf("reading the collapsed measurement (%q): %v", narrow, err)
	}
	t.Logf("collapsed: rail %dpx in a %dpx viewport, person %dpx above the rail's foot", small.RailHeight, small.Viewport, small.FootGap)
	if small.RailHeight == 0 {
		t.Fatal("the disclosed rail has no height below 800px; the chrome strip does not open it")
	}
	if small.RailHeight > 700 {
		t.Errorf("the collapsed rail is %dpx tall in a 780px window; the auto margin is still stretching it to the viewport", small.RailHeight)
	}
	if small.FootGap > 48 {
		t.Errorf("the collapsed rail leaves %dpx under the person block; the foot is not simply following the nav", small.FootGap)
	}
	if small.MenuLift >= 0 {
		t.Errorf("the language menu still opens upward in the collapsed rail (%dpx above its summary); below 800px there is nothing above it but the nav", small.MenuLift)
	}

	// And a SHORT window, which is the other half of "fits the
	// viewport" and the half a desktop reviewer never sees. 1280×420 is
	// a laptop with the devtools open, or a phone in landscape: wide
	// enough for the full-height rail, too short for its content.
	//
	// The claim here is deliberately weaker than the one above, because
	// the honest one is weaker: the rail must still fit the window, and
	// whatever does not fit INSIDE the rail must be reachable by
	// scrolling it. A person below the fold of a rail that scrolls is
	// fine; a person below the fold of a rail that does not is the bug
	// this test is named after, one viewport smaller.
	//
	// Two things this leg used to get wrong, and both made it a gate
	// that gated nothing — removing overflow-y: auto from the rail
	// passed it green:
	//
	//   - it ran on the two-link fixture, which fits 420px with room
	//     to spare, so the overflow case it was written for never
	//     arose. It now runs on /tall, and it FAILS if that page stops
	//     overflowing: a leg whose premise has quietly become false is
	//     worse than no leg, because it reads as coverage.
	//   - it measured scrollHeight - clientHeight, which is overflow,
	//     not scrollability: overflow: visible reports it too. It now
	//     asks the engine what it computed, asks the box to scroll to
	//     its own bottom, and asks whether the person came into view.
	var shortRaw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 420),
		chromedp.Navigate(rig.Origin+"/tall"),
		chromedp.WaitVisible(`#rail-person`, chromedp.ByQuery),
		chromedp.Click(`#rail-locale > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`#rail-locale .rst-dropdown__menu`, chromedp.ByQuery),
		chromedp.Evaluate(railMeasure, &shortRaw),
	); err != nil {
		t.Fatalf("driving the rail in a short window: %v", err)
	}
	var short railReading
	if err := json.Unmarshal([]byte(shortRaw), &short); err != nil {
		t.Fatalf("reading the short-window measurement (%q): %v", shortRaw, err)
	}
	t.Logf("short: rail %dpx in a %dpx viewport (overhang %dpx, %dpx of content over, overflow-y %s), person overhang %dpx before the scroll, %dpx after it, scrollTop %dpx",
		short.RailHeight, short.Viewport, short.RailOverhang, short.RailScroll, short.RailOverflowY,
		short.PersonOverhang, short.PersonOverhangScrolled, short.RailScrolled)
	if short.RailHeight > short.Viewport {
		t.Errorf("the rail's border box is %dpx in a %dpx viewport: a short window overflows for the same reason a tall one did", short.RailHeight, short.Viewport)
	}
	if short.RailOverhang > 0 {
		t.Errorf("the sticky rail hangs %dpx below the foot of a %dpx window", short.RailOverhang, short.Viewport)
	}
	// The premise, asserted before anything that depends on it, and in
	// terms of the FIXTURE rather than of the property under test: if
	// the rail's content fits, every assertion below is vacuously true
	// and this leg is decoration, which is exactly what it was.
	if short.RailScroll <= 0 {
		t.Fatalf("the tall rail's content fits inside a %dpx rail (%dpx over): this leg is measuring nothing. Give railTallNav more links.", short.RailHeight, short.RailScroll)
	}
	if short.PersonOverhang <= 0 {
		t.Fatalf("the person is %dpx ABOVE the fold of a %dpx window before anything scrolls: the case this leg exists for has not arisen. Give railTallNav more links.", -short.PersonOverhang, short.Viewport)
	}
	// It overflows, so it has to scroll. Three readings, because the
	// one that used to be here could not tell a scrollable box from a
	// leaking one.
	if short.RailOverflowY != "auto" && short.RailOverflowY != "scroll" {
		t.Errorf("the rail computed overflow-y: %s. Its content is %dpx below the fold of a %dpx window with no way to bring it back", short.RailOverflowY, short.PersonOverhang, short.Viewport)
	}
	if short.RailScrolled <= 0 {
		t.Errorf("the rail was asked to scroll to its own bottom and scrollTop stayed at %dpx: it does not scroll, it just overflows", short.RailScrolled)
	}
	if short.PersonOverhangScrolled > 0 {
		t.Errorf("the person block is still %dpx below the window after the rail was scrolled to its bottom: it is unreachable, not merely off-screen", short.PersonOverhangScrolled)
	}
}

// railTallNav is the short-window leg's rail content: twenty links, so
// the rail cannot fit a 420px window. Written out rather than ranged
// over so the fixture is the markup an app would write, and named so
// the failure message above can tell whoever hits it what to edit.
const railTallNav = `<a href="#" aria-current="page">Posts</a><a href="#">Drafts</a>` +
	`<a href="#">Comments</a><a href="#">Media</a><a href="#">Pages</a>` +
	`<a href="#">Tags</a><a href="#">Categories</a><a href="#">Authors</a>` +
	`<a href="#">Subscribers</a><a href="#">Invitations</a><a href="#">Roles</a>` +
	`<a href="#">Webhooks</a><a href="#">Imports</a><a href="#">Exports</a>` +
	`<a href="#">Redirects</a><a href="#">Domains</a><a href="#">Billing</a>` +
	`<a href="#">Usage</a><a href="#">Audit log</a><a href="#">Settings</a>`

// barReading is one measurement of the topbar shell's bar: what the
// collapse control and the tail are doing, how the tail's three blocks
// are laid out, and whether the bar spills sideways.
type barReading struct {
	MenuShown    bool // the disclosure's summary is drawn
	MenuOpen     bool // ...and its open attribute is set
	TailShown    bool // nav/account/locale are on the page at all
	Rows         int  // distinct block-start positions among the three: 1 = inline, 3 = stacked
	AccountEndPx int  // gap from the account menu's inline end to the bar's
	AccountMenu  bool // the account dropdown's panel is drawn
	Overflow     int  // how far the document scrolls past its own client width
}

// TestTheTopbarCollapsesItsTailBehindOneDisclosure is §9: the topbar
// shell had no narrow layout at all. .rst-shell__bar is a wrapping flex
// row and .rst-shell__account has margin-inline-start: auto, so
// narrowing the window did not collapse anything — it wrapped the
// account and locale menus onto a second row and shoved them to the
// trailing edge. Nothing was broken; nothing had been written.
//
// Three claims, in the order they matter:
//
//  1. Wide, nothing changed. Above 800px the disclosure is not drawn and
//     nav, account and locale sit inline on the bar's own row with the
//     account at the inline end, which is the layout that shipped before
//     the collapse existed.
//  2. Narrow, one control. Below 800px the tail is not drawn until the
//     disclosure is opened, and opened it is a stack rather than a
//     second wrapped row.
//  3. THE TRAP. The account menu is <details name="rst-menus">. Opening
//     it must not close the navigation it was opened from. <details
//     name> exclusivity is document-wide rather than sibling-scoped, so
//     this is not bought by keeping the two elements out of each other's
//     subtree — the disclosure has to be in a different group, and this
//     is the assertion that says so out loud.
//
// There is no script on this page: the shell's <script> tags resolve to
// addresses this mux does not serve, exactly as the rail drive above
// leaves them. So everything here is <details>, a media query and two
// display rules, which is the claim §9 makes.
func TestTheTopbarCollapsesItsTailBehindOneDisclosure(t *testing.T) {
	src, ok := Layout("topbar")
	if !ok {
		t.Fatal("no topbar layout")
	}
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	template.Must(tmpl.Parse(`{{define "content"}}<p>Content.</p>{{end}}`))
	template.Must(tmpl.Parse(`{{define "nav"}}<a href="#" aria-current="page">Posts</a><a href="#">Drafts</a>{{end}}`))
	template.Must(tmpl.Parse(`{{define "account"}}<a href="#">Profile</a><a href="#">Sign out</a>{{end}}`))
	template.Must(tmpl.Parse(`{{define "locale"}}<details class="rst-dropdown rst-locale" id="bar-locale" name="rst-menus"><summary>Language</summary><div class="rst-dropdown__menu"><a href="#" lang="en">English</a><a href="#" lang="ga">Gaeilge</a></div></details>{{end}}`))

	var page strings.Builder
	if err := tmpl.ExecuteTemplate(&page, "layout", nil); err != nil {
		t.Fatalf("rendering the topbar shell: %v", err)
	}
	html := page.String()

	mux := http.NewServeMux()
	stylesheets(t, mux)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()

	const measure = `(() => {
	  const shown = el => { if (!el) return false; const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
	  const bar = document.querySelector(".rst-shell__bar");
	  const menu = document.querySelector(".rst-shell__menu");
	  const tail = document.querySelector(".rst-shell__tail");
	  const nav = document.querySelector(".rst-shell__nav");
	  const account = document.querySelector(".rst-shell__account");
	  const locale = document.querySelector("#bar-locale");
	  const panel = account.querySelector(".rst-dropdown__menu");
	  // Rows by overlap, not by equal tops: the bar is align-items:
	  // center, so three inline blocks of three different heights sit on
	  // one row with three different top edges. Two boxes are on the
	  // same row when their vertical ranges overlap.
	  const boxes = [nav, account, locale].filter(shown)
	    .map(el => el.getBoundingClientRect()).sort((a, b) => a.top - b.top);
	  let rows = 0, edge = -Infinity;
	  for (const b of boxes) {
	    if (b.top >= edge - 1) rows++;
	    if (b.bottom > edge) edge = b.bottom;
	  }
	  const de = document.documentElement;
	  return JSON.stringify({
	    MenuShown: shown(menu.querySelector("summary")),
	    MenuOpen: menu.open,
	    TailShown: shown(nav) || shown(account) || shown(locale),
	    Rows: rows,
	    AccountEndPx: Math.round(bar.getBoundingClientRect().right - account.getBoundingClientRect().right),
	    AccountMenu: shown(panel),
	    Overflow: de.scrollWidth - de.clientWidth
	  });
	})()`

	read := func(t *testing.T, raw string) barReading {
		t.Helper()
		var got barReading
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("reading the measurement (%q): %v", raw, err)
		}
		return got
	}

	// 1. Wide: the collapse is not there.
	var wideRaw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-shell__bar`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &wideRaw),
	); err != nil {
		t.Fatalf("driving the wide topbar: %v", err)
	}
	wide := read(t, wideRaw)
	if wide.MenuShown {
		t.Error("the collapse disclosure is drawn at 1280px; above 800px the bar lays its tail out inline and the control has nothing to do")
	}
	if !wide.TailShown {
		t.Fatal("nav, account and locale are not drawn at 1280px at all: the wide layout is gone, not preserved")
	}
	if wide.Rows != 1 {
		t.Errorf("the bar's three tail blocks sit on %d rows at 1280px, want 1 — the wide layout is one row and the collapse must not have changed it", wide.Rows)
	}
	if wide.AccountEndPx > 120 {
		t.Errorf("the account menu ends %dpx short of the bar's inline end; margin-inline-start: auto is no longer reaching it", wide.AccountEndPx)
	}

	// 1b. And at exactly 800px, which is the breakpoint itself. A
	// min-width query matches AT its own width, so 800 is the wide
	// layout and 799 is the narrow one; an off-by-one here is a window
	// width at which the bar has neither layout.
	var edgeRaw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(800, 780),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-shell__bar`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &edgeRaw),
	); err != nil {
		t.Fatalf("driving the topbar at the breakpoint: %v", err)
	}
	if edge := read(t, edgeRaw); edge.MenuShown || !edge.TailShown || edge.Rows != 1 {
		t.Errorf("at exactly 800px the bar shows control=%v tail=%v on %d rows; a min-width query matches at its own width, so 800px is the wide layout",
			edge.MenuShown, edge.TailShown, edge.Rows)
	}

	// 2. Narrow: one control, and the tail behind it.
	var closedRaw, openRaw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 780),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-shell__menu > summary`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &closedRaw),
		chromedp.Click(`.rst-shell__menu > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`.rst-shell__tail .rst-shell__nav`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &openRaw),
	); err != nil {
		t.Fatalf("driving the collapsed topbar: %v", err)
	}
	closed, open := read(t, closedRaw), read(t, openRaw)
	if !closed.MenuShown {
		t.Fatal("there is no collapse control at 390px: the topbar still has no narrow layout")
	}
	if closed.TailShown {
		t.Error("nav, account and locale are still drawn at 390px with the disclosure closed; the tail did not collapse, it only gained a button")
	}
	if closed.Overflow > 1 {
		t.Errorf("the collapsed topbar spills %dpx sideways at 390px", closed.Overflow)
	}
	if !open.TailShown {
		t.Fatal("opening the disclosure at 390px revealed nothing")
	}
	if open.Rows != 3 {
		t.Errorf("the opened tail puts its three blocks on %d rows, want 3 — collapsed, they stack; a wrapped row is the state this replaced", open.Rows)
	}
	if open.Overflow > 1 {
		t.Errorf("the opened topbar spills %dpx sideways at 390px", open.Overflow)
	}

	// 3. The trap: opening the account menu must not close the
	// navigation it was opened from.
	var trapRaw string
	if err := chromedp.Run(ctx,
		chromedp.Click(`.rst-shell__account > summary`, chromedp.ByQuery),
		// Settle rather than WaitVisible: the failure this step exists
		// for is the account menu never becoming visible, because it
		// closed the disclosure it lives behind. Waiting for it would
		// report that as a timeout instead of as the three sentences
		// below, and a timeout says nothing about <details name>.
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(measure, &trapRaw),
	); err != nil {
		t.Fatalf("opening the account menu inside the collapsed tail: %v", err)
	}
	trap := read(t, trapRaw)
	if !trap.MenuOpen {
		t.Error("opening the account menu CLOSED the disclosure it sits behind: the two <details> are in one exclusivity group, and <details name> exclusivity is document-wide rather than sibling-scoped. Give the disclosure a group of its own")
	}
	if !trap.TailShown {
		t.Error("the tail vanished when the account menu opened; the navigation a reader opened the menu from is gone from under them")
	}
	if !trap.AccountMenu {
		t.Error("the account menu's panel is not drawn inside the collapsed tail")
	}

	// 4. And the smallest viewport the shells promise, with the tail
	// open, because a stack that fits at 390px can still spill at 320.
	var tinyRaw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(320, 640),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-shell__menu > summary`, chromedp.ByQuery),
		chromedp.Click(`.rst-shell__menu > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`.rst-shell__tail .rst-shell__nav`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &tinyRaw),
	); err != nil {
		t.Fatalf("driving the collapsed topbar at 320px: %v", err)
	}
	if tiny := read(t, tinyRaw); tiny.Overflow > 1 {
		t.Errorf("the opened topbar spills %dpx sideways at 320px", tiny.Overflow)
	}
}

// cornerReading is one element's four logical corner radii, as strings
// straight off getComputedStyle so a comparison is exact rather than
// parsed-and-rounded.
type cornerReading struct {
	StartStart string
	StartEnd   string
	EndStart   string
	EndEnd     string
}

func (c cornerReading) String() string {
	return c.StartStart + "/" + c.StartEnd + " " + c.EndStart + "/" + c.EndEnd
}

func (c cornerReading) uniform() bool {
	return c.StartStart == c.StartEnd && c.StartStart == c.EndStart && c.StartStart == c.EndEnd
}

// TestSelfShapedChildrenKeepTheirCornersInsideACard gates the cascade
// relationship the corner-rounding rules depend on, which a comment
// cannot: the rules are wrapped in :where() so they weigh (0,0,0), and
// therefore lose to ANY child that declares a radius of its own.
//
// Written before it was true. A bare `.rst-list > :first-child` weighs
// (0,2,0) and beats .rst-search's and .rst-empty's (0,1,0) in every
// source order, so a hand-written <form class="rst-search"> as the
// direct first child of a list card painted 7px on its top corners and
// 6px on its bottom ones in day — lopsided, and the exact arrangement
// ui/partials/list-bar-search.html exists to support. The gallery does
// not show it, because list-bar-search renders inside a <search>
// landmark and the direct child is that unpainted element, so this gate
// is built on bare markup rather than on a rendered page.
//
// The assertion is deliberately theme-independent and token-independent:
// the SAME element is rendered inside a card and outside one, and the
// two readings must agree. No pixel value appears in this test, so a
// theme that changes its radii cannot make it stale. Every theme is
// measured, because --rst-radius and --rst-radius-sm differ between them
// and a rule that ties in one could win in another.
func TestSelfShapedChildrenKeepTheirCornersInsideACard(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
	var searchForm strings.Builder
	if err := tmpl.ExecuteTemplate(&searchForm, "list-bar-search", map[string]any{"Action": "/posts"}); err != nil {
		t.Fatalf("rendering list-bar-search: %v", err)
	}
	// The partial wraps its form in the <search> landmark. Strip that
	// wrapper: the bare form as a card's direct child is the case under
	// test, and it is the case an app hand-writes.
	bare := searchForm.String()
	if i, j := strings.Index(bare, "<form"), strings.LastIndex(bare, "</form>"); i >= 0 && j > i {
		bare = bare[i : j+len("</form>")]
	} else {
		t.Fatalf("list-bar-search no longer renders a <form>: %q", searchForm.String())
	}
	empty := `<div class="rst-empty"><p>Nothing here yet.</p></div>`
	row := `<div class="rst-row"><span>A row</span></div>`

	// Each case: a self-shaped element in the position under test
	// inside a card, and the same element loose on the page. Plus the
	// negative control — a plain row, which declares no radius and so
	// MUST take the card's corners, or the rule has stopped working
	// altogether and the rest of this test would pass vacuously.
	cases := []struct {
		name, inCard, loose string
		wantOwn             bool
	}{
		{"a bare search form as the first child", `<div class="rst-list">` + bare + row + `</div>`, bare, true},
		{"an empty state as the last child", `<div class="rst-list">` + row + empty + `</div>`, empty, true},
		{"a plain row as the first child", `<div class="rst-list">` + row + row + `</div>`, row, false},
	}

	var body strings.Builder
	for i, c := range cases {
		fmt.Fprintf(&body, `<div id="in-%d" class="rst-page">%s</div><div id="out-%d" class="rst-page">%s</div>`, i, c.inCard, i, c.loose)
	}
	pageHTML := body.String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	// One route per theme, named by ThemeNames() rather than hardcoded:
	// a fourth theme has to appear here without an edit.
	for _, name := range ThemeNames() {
		css, ok := ThemeCSS(name)
		if !ok {
			t.Fatalf("ThemeCSS(%q) reports no such theme", name)
		}
		mux.HandleFunc("GET /theme-"+name+".css", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			w.Write(css)
		})
	}
	mux.HandleFunc("GET /{theme}/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>corners</title>`+
			`<link rel="stylesheet" href="/tokens.css"><link rel="stylesheet" href="/theme-%s.css">`+
			`</head><body>%s</body></html>`, r.PathValue("theme"), pageHTML)
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	const measure = `(() => {
	  const read = el => {
	    const s = getComputedStyle(el);
	    return {
	      StartStart: s.borderStartStartRadius, StartEnd: s.borderStartEndRadius,
	      EndStart: s.borderEndStartRadius, EndEnd: s.borderEndEndRadius
	    };
	  };
	  const out = [];
	  let i = 0;
	  for (;;) {
	    const inCard = document.querySelector("#in-" + i);
	    const loose = document.querySelector("#out-" + i);
	    if (!inCard || !loose) break;
	    // The element under test is the first child of the card for the
	    // first-child cases and the last for the last-child ones; the
	    // loose copy is the page div's only child either way.
	    const probe = loose.firstElementChild;
	    const tag = probe.tagName + "." + probe.className;
	    const card = inCard.firstElementChild;
	    let mine = null;
	    for (const kid of card.children) {
	      if (mine === null && kid.tagName + "." + kid.className === tag) { mine = kid; }
	    }
	    out.push({ In: mine ? read(mine) : null, Out: read(probe) });
	    i++;
	  }
	  return JSON.stringify(out);
	})()`

	for _, theme := range ThemeNames() {
		var raw string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(rig.Origin+"/"+theme+"/"),
			chromedp.WaitVisible(`#in-0`, chromedp.ByQuery),
			chromedp.Evaluate(measure, &raw),
		); err != nil {
			t.Fatalf("%s: driving the corner probe: %v", theme, err)
		}
		var got []struct{ In, Out *cornerReading }
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: reading the measurement (%q): %v", theme, raw, err)
		}
		if len(got) != len(cases) {
			t.Fatalf("%s: measured %d cases, want %d", theme, len(got), len(cases))
		}
		for i, c := range cases {
			name := theme + ": " + c.name
			if got[i].In == nil {
				t.Errorf("%s: the element was not found inside the card", name)
				continue
			}
			in, out := *got[i].In, *got[i].Out
			if c.wantOwn {
				if in != out {
					t.Errorf("%s: it computes %s inside a card and %s outside one — the card's corner rule is out-weighing the child's own radius", name, in, out)
				}
				// The symptom a reader sees is lopsidedness, so name it
				// as well as the mismatch: a self-shaped child that
				// keeps one pair of corners and loses the other reads
				// as a rendering bug, not as a cascade one.
				if !in.uniform() {
					t.Errorf("%s: its corners are %s inside a card — lopsided, so only some of them lost the cascade", name, in)
				}
				continue
			}
			// The negative control. Without it every assertion above
			// would also pass if the corner rules were simply deleted.
			if in == out {
				t.Errorf("%s: it computes the same %s inside a card as outside one; the card is no longer rounding the rows that have no radius of their own", name, in)
			}
			if in.StartStart == "0px" {
				t.Errorf("%s: it computes %s inside a card — the card's own corner rule is not reaching a full-bleed row", name, in)
			}
		}
	}
}

// menuCorner is one corner of the window with a twelve-item menu in it —
// the shape of the bug §6-v2.1b was written about, since the language
// menu is exactly twelve entries and measures 388px.
type menuCorner struct {
	id      string
	style   string
	rowMenu bool
	// short cuts the menu to three items and mid says it sits in open
	// window with room on every side. They go together, and they exist
	// for one reason: a corner fixture cannot tell a right default from
	// a wrong one. flip-block is symmetric, so a stylesheet that opened
	// every menu UPWARD by default would be flipped back to correct in
	// all four corners and the drive would stay green while every
	// ordinary page in every app dropped its menus the wrong way. That
	// mutation was run against the first version of this drive and it
	// passed. The mid fixture is what fails it.
	short bool
	mid   bool
}

var menuCorners = []menuCorner{
	{id: "tl", style: "inset-block-start:8px;inset-inline-start:8px"},
	{id: "tr", style: "inset-block-start:8px;inset-inline-end:8px"},
	{id: "bl", style: "inset-block-end:8px;inset-inline-start:8px", rowMenu: true},
	{id: "br", style: "inset-block-end:8px;inset-inline-end:8px"},
	{id: "mid", style: "inset-block-start:50%;inset-inline-start:40%", short: true, mid: true},
}

// menuReading is one opened menu, measured against the window it opened
// in. Every field is here because an assertion below needs it AND
// because a failure is unreadable without it: "the menu did not flip"
// says nothing without the two rectangles that prove it.
type menuReading struct {
	SummaryTop, SummaryBottom       int
	SummaryLeft, SummaryRight       int
	MenuTop, MenuBottom             int
	MenuLeft, MenuRight             int
	ScrollHeight, ClientHeight      int
	MaxBlockSize, OverflowY         string
	SupportsArea, SupportsTry       bool
	SupportsScope, SupportsNonsense bool
	SupportsControl                 bool
	ViewportW, ViewportH            int
	Direction                       string
}

// menuMeasure opens every menu on the page and reads it back. The
// capability probes ride along with the geometry deliberately: they are
// read from the same engine in the same run, so a failure names both
// halves at once.
const menuMeasure = `(() => {
  const out = {};
  for (const box of document.querySelectorAll('[data-corner]')) {
    const d = box.querySelector('details');
    d.open = true;
    const s = d.querySelector('summary').getBoundingClientRect();
    const m = d.querySelector('.rst-dropdown__menu, .rst-row-menu__panel');
    const r = m.getBoundingClientRect();
    const cs = getComputedStyle(m);
    out[box.dataset.corner] = {
      SummaryTop: Math.round(s.top), SummaryBottom: Math.round(s.bottom),
      SummaryLeft: Math.round(s.left), SummaryRight: Math.round(s.right),
      MenuTop: Math.round(r.top), MenuBottom: Math.round(r.bottom),
      MenuLeft: Math.round(r.left), MenuRight: Math.round(r.right),
      ScrollHeight: m.scrollHeight, ClientHeight: m.clientHeight,
      MaxBlockSize: cs.maxBlockSize, OverflowY: cs.overflowY,
      SupportsArea: CSS.supports('position-area: block-end'),
      SupportsTry: CSS.supports('position-try-fallbacks: flip-block'),
      SupportsScope: CSS.supports('anchor-scope: --rst-menu'),
      SupportsNonsense: CSS.supports('position-area: rastrillo-no-such-value'),
      SupportsControl: CSS.supports('color: red'),
      ViewportW: window.innerWidth, ViewportH: window.innerHeight,
      Direction: getComputedStyle(document.documentElement).direction,
    };
  }
  return JSON.stringify(out);
})()`

// TestMenusCapTheirHeightScrollAndFlipToFitTheViewport is design spec
// §6-v2.1b, sub-sections 1 and 2, in one drive because they are one
// surface: .rst-dropdown__menu and .rst-row-menu__panel had no height
// cap, no scroll, and no idea what was underneath them.
//
// THE CAP AND THE SCROLL are the everywhere half. A twelve-locale
// language menu is 388px; on a short window its last entries were
// simply unreachable, with nothing to drag. The drive requires the menu
// to be really scrolling — scrollHeight above clientHeight — so a
// future change that makes the fixture short enough to fit fails here
// rather than quietly measuring nothing.
//
// THE FLIP is CSS anchor positioning, and it is Chromium-only today.
// This drive runs Chromium, so it does not ask permission: the flip is
// asserted flat, and the four capability probes are asserted alongside
// it. That combination is the point, and it is what stops this being
// the tenth gate on this branch that gated nothing:
//
//   - The geometry is asserted unconditionally. If the properties are
//     renamed, dropped, or land behind a flag, the menu stops flipping
//     and this goes red. There is no `if supported` for it to fall
//     through.
//   - The probes are asserted TRUE for the exact declarations
//     tokens.css writes. A rename that CSS.supports starts answering
//     false to fails here too, which is what tells the next reader
//     WHY the geometry broke.
//   - The probe is controlled in both directions: a nonsense
//     position-area value must be answered FALSE and `color: red` must
//     be answered TRUE. A CSS.supports that has started agreeing with
//     everything, or refusing everything, cannot make this test green.
//   - The fixture is proved to NEED the flip before the flip is
//     asserted: the bottom corners are checked to have less room below
//     the summary than the menu is tall. Without that a taller window
//     would pass this test with no flip happening at all.
//
// RTL is a second pass over the same fixture, since every declaration
// involved is a logical one and the inline flip is the half that would
// give that away.
func TestMenusCapTheirHeightScrollAndFlipToFitTheViewport(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	// Twelve, because twelve locales is the menu that broke. The
	// autonyms are transliterated here rather than in their own scripts:
	// this drive measures boxes, and a font fallback that changed a
	// glyph's width would move a number nobody meant to move.
	var items []any
	for _, name := range []string{
		"English", "Espanol", "Portugues", "Gaeilge", "Arabiyya", "Hindi",
		"Bangla", "Nihongo", "Russkiy", "Tieng Viet", "Jyutping", "Zhongwen",
	} {
		items = append(items, map[string]any{"Href": "#", "Label": name})
	}

	render := func(dir string) string {
		var b strings.Builder
		b.WriteString(`<!doctype html><html lang="en" dir="` + dir + `"><head><meta charset="utf-8">` +
			`<title>menus</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css"></head><body>`)
		for _, c := range menuCorners {
			items := items
			if c.short {
				// Three entries, so this fixture has room on every side
				// and the default drop direction is what shows. Twelve
				// would not fit above AND below a 620px window, and a
				// fixture that only fits one way is a corner again.
				items = items[:3]
			}
			b.WriteString(`<div data-corner="` + c.id + `" style="position:fixed;` + c.style + `">`)
			if c.rowMenu {
				// Hand-written, the way tokens.css says the row-menu
				// idiom is used, and here so the drive covers BOTH
				// panels the new rule names rather than one of them.
				b.WriteString(`<details class="rst-row-menu" name="rst-menu-` + c.id + `">` +
					`<summary aria-label="Actions">` + string(rastrillo.Icon("kebab")) + `</summary>` +
					`<div class="rst-row-menu__panel">`)
				for _, it := range items {
					b.WriteString(`<a href="#">` + it.(map[string]any)["Label"].(string) + `</a>`)
				}
				b.WriteString(`</div></details>`)
			} else if err := tmpl.ExecuteTemplate(&b, "dropdown", map[string]any{
				"Label": "Language", "Items": items, "MenuGroup": "rst-menu-" + c.id,
			}); err != nil {
				t.Fatalf("rendering the %s menu: %v", c.id, err)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</body></html>`)
		return b.String()
	}

	ltr, rtl := render("ltr"), render("rtl")
	mux := http.NewServeMux()
	stylesheets(t, mux)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, ltr)
	})
	mux.HandleFunc("GET /rtl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, rtl)
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	for _, leg := range []struct{ name, path string }{{"ltr", "/"}, {"rtl", "/rtl"}} {
		var raw string
		if err := chromedp.Run(ctx,
			// 620px is short on purpose: it is the window in which a
			// 388px menu opened from either bottom corner has nowhere
			// to go, which is the whole subject.
			chromedp.EmulateViewport(760, 620),
			chromedp.Navigate(rig.Origin+leg.path),
			chromedp.WaitVisible(`[data-corner="tl"] details`, chromedp.ByQuery),
			chromedp.Evaluate(menuMeasure, &raw),
		); err != nil {
			t.Fatalf("%s: driving the menus: %v", leg.name, err)
		}
		var got map[string]menuReading
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: reading the measurement (%q): %v", leg.name, raw, err)
		}

		for _, c := range menuCorners {
			m, ok := got[c.id]
			if !ok {
				t.Fatalf("%s/%s: the menu was never measured", leg.name, c.id)
			}
			where := leg.name + "/" + c.id
			t.Logf("%s: summary [%d,%d]-[%d,%d], menu [%d,%d]-[%d,%d] in %dx%d; %d of %dpx shown, max-block-size %s",
				where, m.SummaryLeft, m.SummaryTop, m.SummaryRight, m.SummaryBottom,
				m.MenuLeft, m.MenuTop, m.MenuRight, m.MenuBottom,
				m.ViewportW, m.ViewportH, m.ClientHeight, m.ScrollHeight, m.MaxBlockSize)

			// THE PROBE, AND THE PROBE'S OWN CONTROL. Read first,
			// because everything below it is meaningless if CSS.supports
			// has stopped discriminating.
			if !m.SupportsControl {
				t.Fatalf("%s: CSS.supports says this engine does not support `color: red`; the probe itself is broken, so nothing this drive reports about anchor positioning can be believed", where)
			}
			if m.SupportsNonsense {
				t.Fatalf("%s: CSS.supports agreed to a position-area value that does not exist; it is answering true to everything, so a support check here would be worthless", where)
			}
			if !m.SupportsArea || !m.SupportsTry || !m.SupportsScope {
				t.Fatalf("%s: this engine reports position-area %v, position-try-fallbacks %v, anchor-scope %v. tokens.css writes all three; if one has been renamed the menus below have silently stopped flipping", where, m.SupportsArea, m.SupportsTry, m.SupportsScope)
			}

			// THE CAP AND THE SCROLL.
			if m.OverflowY != "auto" && m.OverflowY != "scroll" {
				t.Errorf("%s: the menu computes overflow-y: %s, so a menu taller than the window still cannot be scrolled", where, m.OverflowY)
			}
			switch {
			case c.short && m.ScrollHeight > m.ClientHeight:
				// The mid fixture's three entries have to FIT, or it is
				// not the "room on every side" fixture the default
				// direction is read off below.
				t.Errorf("%s: the three-item menu is scrolling — %d of %dpx shown — so it no longer has room on every side and cannot pin the default direction", where, m.ClientHeight, m.ScrollHeight)
			case !c.short && m.ScrollHeight <= m.ClientHeight:
				t.Errorf("%s: the menu shows all %dpx of its %dpx of entries, so this fixture is not tall enough to prove anything about the cap. Add entries or shorten the window.", where, m.ClientHeight, m.ScrollHeight)
			}
			if height := m.MenuBottom - m.MenuTop; height > m.ViewportH {
				t.Errorf("%s: the menu is %dpx tall in a %dpx window — the cap is not holding", where, height, m.ViewportH)
			}

			// IN VIEW AT ALL. The plainest statement of what Paul
			// asked for, and the one that fails whichever of the two
			// flips has stopped working.
			if m.MenuTop < -1 || m.MenuLeft < -1 || m.MenuBottom > m.ViewportH+1 || m.MenuRight > m.ViewportW+1 {
				t.Errorf("%s: the menu sits at [%d,%d]-[%d,%d], outside the %dx%d window", where,
					m.MenuLeft, m.MenuTop, m.MenuRight, m.MenuBottom, m.ViewportW, m.ViewportH)
			}

			// THE BLOCK AXIS, and first the proof that this fixture is
			// the one it claims to be. Without those checks a taller
			// window would make the corner assertions pass with nothing
			// flipping, and a mid fixture that had drifted into a
			// corner would stop pinning anything.
			menuHeight := m.MenuBottom - m.MenuTop
			above, below := m.SummaryTop, m.ViewportH-m.SummaryBottom
			switch {
			case c.mid:
				// THE DEFAULT DIRECTION. A corner fixture cannot assert
				// this: flip-block is symmetric, so a stylesheet that
				// preferred block-start would be flipped back to
				// correct in every corner and this drive would stay
				// green while every ordinary page opened its menus
				// upward. Here there is room both ways, so what shows
				// IS the default.
				if above < menuHeight+4 || below < menuHeight+4 {
					t.Fatalf("%s: the menu is %dpx tall with %dpx above the summary and %dpx below. This fixture is supposed to have room on BOTH sides — with room on only one, flip-block rescues a wrong default and the assertion below stops meaning anything.", where, menuHeight, above, below)
				}
				if m.MenuTop < m.SummaryBottom-1 {
					t.Errorf("%s: the menu's head is at %d, above the summary's foot at %d — with room on both sides a menu must drop DOWNWARD; the default direction has been inverted", where, m.MenuTop, m.SummaryBottom)
				}
				// The same hole on the inline axis: with room both ways
				// the default alignment is what shows, and it is
				// span-inline-start — the panel's inline-END edge on
				// the summary's inline-end edge, which is what
				// inset-inline-end: 0 always did.
				gotEdge, wantEdge := m.MenuRight, m.SummaryRight
				if m.Direction == "rtl" {
					gotEdge, wantEdge = m.MenuLeft, m.SummaryLeft
				}
				if gotEdge < wantEdge-1 || gotEdge > wantEdge+1 {
					t.Errorf("%s: the menu's inline-end edge is at %d and the summary's is at %d (dir=%s) — with room on both sides the default alignment has changed", where, gotEdge, wantEdge, m.Direction)
				}
			case strings.HasPrefix(c.id, "b"):
				if below >= menuHeight {
					t.Fatalf("%s: there is %dpx under the summary and the menu is %dpx tall, so it fits below and no flip is required. This fixture no longer tests the flip.", where, below, menuHeight)
				}
				if m.MenuBottom > m.SummaryTop+1 {
					t.Errorf("%s: the menu's foot is at %d and the summary's head is at %d — it opened downward off the bottom of the window instead of flipping up", where, m.MenuBottom, m.SummaryTop)
				}
			default:
				if m.MenuTop < m.SummaryBottom-1 {
					t.Errorf("%s: the menu's head is at %d, above the summary's foot at %d — it flipped upward where there was room below", where, m.MenuTop, m.SummaryBottom)
				}
			}
		}
	}
}

// TestTheScrollbarGutterHoldsTheLayoutStill is design spec §6-v2.1b.4:
// clicking between a short screen and a long one moved the whole page
// sideways by the scrollbar's width, and opening a modal did the same
// thing in the other direction because its scroll lock takes the
// scrollbar away.
//
// The control is the whole drive. --rst-scrollbar-gutter: auto is the
// documented opt-out, so the same fixture with the token overridden is
// the page as it was before the fix, and it MUST show the shift. If it
// does not, this browser is drawing overlay scrollbars — the macOS
// default, and chromedp's headless default until harness.WithScrollbars
// turns it off — and the drive fails saying exactly that rather than
// reporting a fix it never saw.
func TestTheScrollbarGutterHoldsTheLayoutStill(t *testing.T) {
	// A ruler and, for the tall pages, something to scroll. The ruler
	// is a width: 100% block and body's own first child, so it reports
	// the inline space the layout actually got and nothing in between
	// can absorb a difference.
	//
	// document.documentElement.clientWidth is NOT that number, and was
	// the first thing this drive tried: Chromium reports the full window
	// width there whether or not a gutter is reserved, so every
	// measurement agreed with every other one and the drive proved
	// nothing at all.
	const ruler = `<div id="ruler" style="width:100%;height:4px"></div>`
	page := func(extra, body string) string {
		return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>gutter</title>` +
			`<link rel="stylesheet" href="/tokens.css"><link rel="stylesheet" href="/theme.css">` + extra +
			`</head><body>` + ruler + body + `</body></html>`
	}
	const optOut = `<style>:root{--rst-scrollbar-gutter:auto}</style>`
	const tall = `<div style="height:3000px"></div>`

	mux := http.NewServeMux()
	stylesheets(t, mux)
	for path, html := range map[string]string{
		"/short":      page("", ""),
		"/tall":       page("", tall),
		"/short-auto": page(optOut, ""),
		"/tall-auto":  page(optOut, tall),
	} {
		html := html
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		})
	}

	rig := harness.New(t, func(string) http.Handler { return mux }, harness.WithScrollbars())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	// The modal half is measured on one page rather than two, by adding
	// the backdrop and reading the ruler again: nothing else about the
	// document changes between the two numbers, which is the only way
	// the difference can be attributed to the scroll lock.
	const measure = `(() => {
  const w = () => document.getElementById('ruler').getBoundingClientRect().width;
  const before = w();
  const b = document.createElement('div');
  b.className = 'rst-backdrop';
  document.body.appendChild(b);
  const after = w();
  b.remove();
  return JSON.stringify({before, after});
})()`

	read := func(path string) (width int) {
		t.Helper()
		var raw string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(900, 600),
			chromedp.Navigate(rig.Origin+path),
			chromedp.WaitVisible(`#ruler`, chromedp.ByQuery),
			chromedp.Evaluate(`JSON.stringify(Math.round(document.getElementById('ruler').getBoundingClientRect().width))`, &raw),
		); err != nil {
			t.Fatalf("measuring %s: %v", path, err)
		}
		if err := json.Unmarshal([]byte(raw), &width); err != nil {
			t.Fatalf("reading %s (%q): %v", path, raw, err)
		}
		return width
	}

	short, tallW := read("/short"), read("/tall")
	shortAuto, tallAuto := read("/short-auto"), read("/tall-auto")
	t.Logf("stable: short %dpx, long %dpx — auto: short %dpx, long %dpx", short, tallW, shortAuto, tallAuto)

	// THE CONTROL, first, because every assertion after it depends on
	// this browser being able to show the bug at all.
	if shortAuto == tallAuto {
		t.Fatalf("with --rst-scrollbar-gutter: auto a short page and a long one both measure %dpx. This browser's scrollbars take no layout space, so nothing here can tell a working gutter from a missing one — check harness.WithScrollbars still reaches the browser.", shortAuto)
	}

	// THE FIX.
	if short != tallW {
		t.Errorf("a short page measures %dpx and a long one %dpx: the layout still slides by %dpx when the scrollbar comes and goes", short, tallW, short-tallW)
	}

	// THE SECOND BUG, which nobody had attributed to the scrollbar: the
	// modal's scroll lock removes it, and the page jumps sideways while
	// the reader is watching.
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(900, 600),
		chromedp.Navigate(rig.Origin+"/tall"),
		chromedp.WaitVisible(`#ruler`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &raw),
	); err != nil {
		t.Fatalf("driving the modal scroll lock: %v", err)
	}
	var stable struct{ Before, After float64 }
	if err := json.Unmarshal([]byte(raw), &stable); err != nil {
		t.Fatalf("reading the modal measurement (%q): %v", raw, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/tall-auto"),
		chromedp.WaitVisible(`#ruler`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &raw),
	); err != nil {
		t.Fatalf("driving the modal scroll lock without the gutter: %v", err)
	}
	var auto struct{ Before, After float64 }
	if err := json.Unmarshal([]byte(raw), &auto); err != nil {
		t.Fatalf("reading the modal control (%q): %v", raw, err)
	}
	t.Logf("modal scroll lock: stable %.0f→%.0fpx, auto %.0f→%.0fpx", stable.Before, stable.After, auto.Before, auto.After)

	if auto.Before == auto.After {
		t.Fatalf("without the gutter, opening a modal left the page %vpx wide either way — the scroll lock is not removing a scrollbar here, so this leg is measuring nothing", auto.Before)
	}
	if stable.Before != stable.After {
		t.Errorf("opening a modal took the page from %vpx to %vpx: body:has(.rst-backdrop) { overflow: hidden } is still yanking the scrollbar out from under the layout", stable.Before, stable.After)
	}
}

// clearReading is the clear affordance measured where a finger lands:
// its own box, and what is actually on top at the middle of it.
type clearReading struct {
	W, H          float64
	Href          string
	HitIsTheClear bool
	Hit           string
	InputRight    float64
	InputTop      float64
	InputBottom   float64
	FormLeft      float64
	FormRight     float64
	ClearLeft     float64
	ClearRight    float64
}

const clearMeasure = `(() => {
  const form = document.querySelector('.rst-search');
  const input = form.querySelector('input[type=search]');
  const a = form.querySelector('.rst-search__clear');
  const r = a.getBoundingClientRect(), ir = input.getBoundingClientRect(), fr = form.getBoundingClientRect();
  const hit = document.elementFromPoint(r.left + r.width/2, r.top + r.height/2);
  return JSON.stringify({
    W: Math.round(r.width*100)/100, H: Math.round(r.height*100)/100,
    Href: a.getAttribute('href'),
    HitIsTheClear: !!(hit && hit.closest('.rst-search__clear')),
    Hit: hit ? hit.tagName + '.' + (hit.getAttribute('class') || '') : 'nothing',
    InputRight: ir.right, InputTop: ir.top, InputBottom: ir.bottom,
    FormLeft: fr.left, FormRight: fr.right,
    ClearLeft: r.left, ClearRight: r.right,
  });
})()`

// TestClearingASearchIsALinkAndTheNativeCrossIsGone is design spec
// §6-v2.1b.6, the bug Paul screenshotted: a search field holding "sere",
// a ✕ beside it, and clicking the ✕ left the results and the ?q= exactly
// where they were.
//
// That ✕ was ::-webkit-search-cancel-button doing precisely what it is
// specified to do — clearing the input's VALUE — in a GET form that only
// submits on submit. list-bar-search now renders a real link instead,
// and tokens.css takes the native one away, because two affordances one
// of which lies is worse than one that works.
//
// Three things are asserted, and the second is the one with teeth:
//
//  1. The link goes where clearing a search should go: the same screen
//     with q dropped and the carried filter kept.
//  2. The native ✕ is really gone. This is proved against a CONTROL
//     page — a bare <input type="search"> with none of this library's
//     CSS on it — which is clicked in the same place and MUST clear.
//     If Chromium ever stops drawing that button, the control fails and
//     says so, instead of the subject passing because there was never
//     anything there to suppress.
//  3. The target is at least 24×24 CSS px, WCAG 2.2 SC 2.5.8 (AA), and
//     is really on top at its own centre — a 24px box under something
//     else is not a 24px target.
//
// RTL is a second leg: the ✕ has to stay inside the field box when the
// field mirrors, which is a claim about logical properties and nothing
// else.
func TestClearingASearchIsALinkAndTheNativeCrossIsGone(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
	var search strings.Builder
	if err := tmpl.ExecuteTemplate(&search, "list-bar-search", map[string]any{
		"Action": "/posts", "Query": "sere", "Placeholder": "Search posts",
		"Hidden": [][2]string{{"status", "paid"}},
	}); err != nil {
		t.Fatalf("rendering the search form: %v", err)
	}

	mux := http.NewServeMux()
	stylesheets(t, mux)
	page := func(dir string) string {
		return `<!doctype html><html lang="en" dir="` + dir + `"><head><meta charset="utf-8">` +
			`<title>search</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css"></head><body>` +
			`<div class="rst-page"><div class="rst-list">` + search.String() + `</div></div></body></html>`
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page("ltr"))
	})
	mux.HandleFunc("GET /rtl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page("rtl"))
	})
	// The control: the same input with NONE of this library's CSS, so
	// the browser draws its own ✕ exactly as it did before the fix.
	// One per direction, because the button is not in the same place in
	// both and a control in the wrong direction proves nothing about
	// the leg it is controlling.
	native := func(dir string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="en" dir="`+dir+`"><head><meta charset="utf-8">`+
				`<title>native</title></head><body style="margin:0"><form>`+
				`<input id="q" type="search" name="q" value="sere" `+
				`style="position:absolute;inset-block-start:0;inset-inline-start:0;inline-size:300px;block-size:40px;font-size:20px">`+
				`</form></body></html>`)
		}
	}
	mux.HandleFunc("GET /native", native("ltr"))
	mux.HandleFunc("GET /native-rtl", native("rtl"))

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	// clickTheNativeCross clicks where the browser draws its cancel
	// button: just inside the field's INLINE-END edge, on its centre
	// line. Inline-end, not right: Chromium mirrors the button with the
	// direction, so in an RTL document it sits at the left. The first
	// version of this drive clicked r.right - 8 in both legs, which in
	// RTL is the edge the button is never at — an assertion that could
	// not fail, on a branch that has shipped several.
	//
	// The field is focused first because Chromium only makes the button
	// live once the field has focus. Worth recording: a drive that
	// skips the focus measures a button that is there but inert, and
	// concludes it is absent.
	clickTheNativeCross := func(sel string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			var at []float64
			expr := fmt.Sprintf(`(() => {
			  const el = document.querySelector(%q);
			  const r = el.getBoundingClientRect();
			  const rtl = getComputedStyle(el).direction === "rtl";
			  return [rtl ? r.left + 8 : r.right - 8, r.top + r.height / 2];
			})()`, sel)
			if err := chromedp.Evaluate(expr, &at).Do(ctx); err != nil {
				return err
			}
			return chromedp.MouseClickXY(at[0], at[1]).Do(ctx)
		})
	}

	for _, leg := range []struct{ name, path, control string }{
		{"ltr", "/", "/native"},
		{"rtl", "/rtl", "/native-rtl"},
	} {
		// THE CONTROL, per leg and per direction: the browser's own ✕,
		// clicked in the same place, must empty a bare input. If it
		// does not, Chromium is not drawing the button where this leg
		// clicks and the leg's own assertion could not have failed.
		var nativeValue string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(900, 600),
			chromedp.Navigate(rig.Origin+leg.control),
			chromedp.WaitVisible(`#q`, chromedp.ByQuery),
			chromedp.Focus(`#q`, chromedp.ByQuery),
			clickTheNativeCross(`#q`),
			chromedp.Evaluate(`document.getElementById('q').value`, &nativeValue),
		); err != nil {
			t.Fatalf("%s: driving the control input: %v", leg.name, err)
		}
		if nativeValue != "" {
			t.Fatalf("%s: the control input still holds %q after a click on its inline-end edge: this browser is not drawing ::-webkit-search-cancel-button there, so this leg cannot tell a suppressed one from an absent one", leg.name, nativeValue)
		}

		var raw, stillThere string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(900, 600),
			chromedp.Navigate(rig.Origin+leg.path),
			chromedp.WaitVisible(`.rst-search__clear`, chromedp.ByQuery),
			chromedp.Evaluate(clearMeasure, &raw),
			chromedp.Focus(`.rst-search input[type=search]`, chromedp.ByQuery),
			clickTheNativeCross(`.rst-search input[type=search]`),
			chromedp.Evaluate(`document.querySelector('.rst-search input[type=search]').value`, &stillThere),
		); err != nil {
			t.Fatalf("%s: driving the search form: %v", leg.name, err)
		}
		var got clearReading
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: reading the measurement (%q): %v", leg.name, raw, err)
		}
		t.Logf("%s: clear target %.2f×%.2fpx to %s, hit %s; field [%.0f..%.0f], clear [%.0f..%.0f]",
			leg.name, got.W, got.H, got.Href, got.Hit, got.FormLeft, got.FormRight, got.ClearLeft, got.ClearRight)

		if want := "/posts?status=paid"; got.Href != want {
			t.Errorf("%s: the clear link goes to %q, want %q — clearing a search must drop q and keep the filter", leg.name, got.Href, want)
		}
		// The native ✕ is gone: the same click that emptied the control
		// input must leave this one alone.
		if stillThere != "sere" {
			t.Errorf("%s: clicking the field's trailing edge changed its value to %q — the native cancel button is still there beside the link, and it still lies", leg.name, stillThere)
		}
		// WCAG 2.2 SC 2.5.8, AA. A 17px chip on this branch already
		// failed this once.
		if got.W < 24 || got.H < 24 {
			t.Errorf("%s: the clear target is %.2f×%.2fpx, under WCAG 2.2 SC 2.5.8's 24×24", leg.name, got.W, got.H)
		}
		if !got.HitIsTheClear {
			t.Errorf("%s: the middle of the clear target hits %s, not the link — the box is the right size and something is over it", leg.name, got.Hit)
		}
		// It has to sit inside the field it belongs to, in both
		// directions, or the mirrored layout has put it somewhere a
		// reader will not look for it.
		if got.ClearLeft < got.FormLeft || got.ClearRight > got.FormRight {
			t.Errorf("%s: the clear target spans [%.0f..%.0f] and the field box is [%.0f..%.0f] — it has escaped the field", leg.name, got.ClearLeft, got.ClearRight, got.FormLeft, got.FormRight)
		}
	}
}

// frameReading is one preview-sized iframe with a menu open inside it.
type frameReading struct {
	FrameHeight  int
	DVH          int
	MaxBlockSize string
	ShortClient  int
	ShortScroll  int
	LongClient   int
	LongScroll   int
}

// TestAMenuOpenedInsideAShortFrameIsStillUsable is the bug the first
// version of the cap shipped, and it is worth stating plainly because
// it was the fix causing it: `max-block-size: 100dvh - 6rem` has no
// floor, and inside an iframe `dvh` is THE FRAME.
//
// In the design gallery's own 100px preview frames the cap therefore
// computed to 4px, and the bulk-bar sample's Actions menu rendered as a
// 12px sliver of its 103px of entries. Below a 96px frame the cap
// computed to zero and the menu was invisible. A fix aimed at menu
// entries nobody could reach had shipped a menu with no entries at all,
// and nothing caught it because no gate anywhere opened a menu inside a
// frame. This one does.
//
// It is not a gallery test. Any app embedding a rastrillo screen in a
// short iframe had the same bug, which is why the floor is in
// tokens.css and the drive is here.
//
// The dvh control is the load-bearing part. Every assertion below is
// about what happens when the viewport is the frame, so the drive first
// reads a 100dvh box inside the frame and requires it to measure the
// frame's own height. If a future engine changed that, this drive would
// otherwise keep passing while testing nothing.
func TestAMenuOpenedInsideAShortFrameIsStillUsable(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	var long []any
	for _, name := range []string{
		"English", "Espanol", "Portugues", "Gaeilge", "Arabiyya", "Hindi",
		"Bangla", "Nihongo", "Russkiy", "Tieng Viet", "Jyutping", "Zhongwen",
	} {
		long = append(long, map[string]any{"Href": "#", "Label": name})
	}
	short := long[:3]

	mux := http.NewServeMux()
	stylesheets(t, mux)
	// The framed document: the same shape the gallery frames, a sample
	// with its menu already open, plus a 100dvh ruler for the control.
	mux.HandleFunc("GET /frame", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>frame</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css"></head><body style="margin:0">` +
			`<div id="dvh" style="block-size:100dvh;inline-size:1px"></div>`)
		for _, m := range []struct {
			id    string
			items []any
		}{{"short", short}, {"long", long}} {
			b.WriteString(`<div id="` + m.id + `" style="position:absolute;inset-block-start:0;inset-inline-start:8px">`)
			if err := tmpl.ExecuteTemplate(&b, "dropdown", map[string]any{
				"Label": "Menu", "Items": m.items, "MenuGroup": "rst-menu-" + m.id,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, b.String())
	})
	// 100px and 190px are two of the gallery's own previewHeights; 80px
	// is under the 96px at which the unfloored cap reached zero, which
	// is the case that has to be covered rather than avoided.
	heights := []int{80, 100, 190, 260}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>frames</title></head><body style="margin:0">`)
		for _, h := range heights {
			b.WriteString(fmt.Sprintf(
				`<iframe data-h="%d" src="/frame" style="display:block;width:420px;height:%dpx;border:0"></iframe>`, h, h))
		}
		b.WriteString(`</body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, b.String())
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	const measure = `(() => {
  const out = [];
  for (const f of document.querySelectorAll("iframe[data-h]")) {
    const d = f.contentDocument;
    const open = id => {
      const details = d.querySelector("#" + id + " details");
      details.open = true;
      return details.querySelector(".rst-dropdown__menu");
    };
    const s = open("short"), l = open("long");
    out.push({
      FrameHeight: parseInt(f.dataset.h, 10),
      DVH: Math.round(d.getElementById("dvh").getBoundingClientRect().height),
      MaxBlockSize: getComputedStyle(l).maxBlockSize,
      ShortClient: s.clientHeight, ShortScroll: s.scrollHeight,
      LongClient: l.clientHeight, LongScroll: l.scrollHeight,
    });
  }
  return JSON.stringify(out);
})()`

	var raw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(900, 1200),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`iframe[data-h]`, chromedp.ByQuery),
		chromedp.Evaluate(measure, &raw),
	); err != nil {
		t.Fatalf("driving the framed menus: %v", err)
	}
	var got []frameReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading the measurement (%q): %v", raw, err)
	}
	if len(got) != len(heights) {
		t.Fatalf("measured %d frames, want %d", len(got), len(heights))
	}

	for _, f := range got {
		t.Logf("a %dpx frame: 100dvh is %dpx, max-block-size %s; three items show %d of %dpx, twelve show %d of %dpx",
			f.FrameHeight, f.DVH, f.MaxBlockSize, f.ShortClient, f.ShortScroll, f.LongClient, f.LongScroll)

		// THE CONTROL. Everything below is a claim about dvh inside a
		// frame, so prove dvh is the frame first.
		if f.DVH != f.FrameHeight {
			t.Fatalf("a %dpx frame reports 100dvh as %dpx. dvh is no longer the frame's own height, so this drive is not exercising the bug it was written for.", f.FrameHeight, f.DVH)
		}

		// A three-item menu is 103px and the floor is 8rem, so it must
		// fit whole — in an 80px frame as much as a 260px one. This is
		// the assertion that was 12-of-103 before the floor.
		if f.ShortClient < f.ShortScroll {
			t.Errorf("in a %dpx frame a three-item menu shows %d of its %dpx: the cap has collapsed it, and a reader opening this menu sees a sliver", f.FrameHeight, f.ShortClient, f.ShortScroll)
		}
		// A twelve-item menu cannot fit a short frame and is not meant
		// to — it scrolls. What it must not do is disappear.
		if f.LongClient < 96 {
			t.Errorf("in a %dpx frame a twelve-item menu shows %dpx of its %dpx. An overflowing menu can be scrolled to; a 4px one cannot be used and a 0px one cannot be seen.", f.FrameHeight, f.LongClient, f.LongScroll)
		}
	}
}

// orphanReading is one menu in one scroller, read before and after the
// scroll that takes its button away.
type orphanReading struct {
	SummaryTop     float64
	SummaryBottom  float64
	ScrollerTop    float64
	ScrollerBottom float64
	MenuTop        float64
	MenuBottom     float64
	Painted        bool
	Hit            string
	Visibility     string
	Supports       bool
	SupportsBogus  bool
}

// TestAMenuDoesNotOutliveTheAnchorScrolledAwayFromUnderIt is the price
// of the fixed positioning the flip needs, paid.
//
// A fixed panel is deliberately outside every scrolling ancestor's clip
// — that is how a menu opened inside a list card escapes the card — and
// Chromium keeps it tracking its anchor through both window and
// inner-scroller scrolls. But the ANCHOR is still inside the clip, so
// scrolling a rail's nav until the menu's own button had gone left the
// menu painted, over unrelated content, with nothing under it. The
// sidebar rail with its thirty-odd links is the live case.
//
// position-visibility: anchors-visible ties the two together: when the
// button goes, the menu goes.
//
// The always-visible control beside it is what stops this drive being
// able to pass by mistake. The second scroller's panel overrides the
// declaration back to `always`, so it MUST still be painted after the
// same scroll. If the drive's way of asking "is this visible?" ever
// starts answering no to everything — a changed API, a mis-selected
// element, a scroll that moved the wrong box — the control fails and
// says so, instead of the subject passing for the wrong reason.
func TestAMenuDoesNotOutliveTheAnchorScrolledAwayFromUnderIt(t *testing.T) {
	// Hand-written markup, the way tokens.css says these idioms are
	// used, because the second scroller needs an override the partial
	// has no key for.
	scroller := func(id, panelStyle string) string {
		return `<div id="` + id + `" style="block-size:220px;inline-size:320px;overflow-y:auto;` +
			`border:1px solid #888;margin:24px">` +
			`<details class="rst-dropdown" id="` + id + `-menu" name="rst-menu-` + id + `">` +
			`<summary>Menu</summary>` +
			`<div class="rst-dropdown__menu" style="` + panelStyle + `">` +
			`<a href="#">One</a><a href="#">Two</a><a href="#">Three</a>` +
			`</div></details>` +
			`<div style="block-size:1200px">tall</div></div>`
	}

	mux := http.NewServeMux()
	stylesheets(t, mux)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
			`<title>orphan</title><link rel="stylesheet" href="/tokens.css">`+
			// The 200px of head room is the whole fixture. The scrollers
			// have to sit far enough down the page that an anchor
			// scrolled ABOVE its scroller's top edge is still at a
			// coordinate inside the window — that is the orphan case. A
			// scroller at the top of the page takes its anchor off the
			// window as well, which hides the bug rather than showing
			// it, and was this drive's first draft.
			`<link rel="stylesheet" href="/theme.css"></head>`+
			`<body style="margin:0;padding-block-start:200px">`+
			scroller("subject", "")+
			scroller("control", "position-visibility: always")+
			`</body></html>`)
	})

	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	// elementFromPoint is the measurement, and that is not a style
	// choice. position-visibility hides the box at USED-value time: the
	// element keeps its computed `visibility: visible`, keeps its
	// layout rectangle, and answers true to
	// checkVisibility({visibilityProperty: true}) — all three were
	// tried, and all three said "visible" of a menu Chromium was no
	// longer painting. What actually changes is paint and hit testing,
	// so the question has to be asked of the compositor: is the menu
	// the thing at the middle of the menu?
	//
	// This is the same technique, and the same reason, as
	// TestAMenuOpenedInsideAListCardEscapesTheCard above.
	read := func(id string) string {
		return `(() => {
  const box = document.getElementById(` + "`" + id + "`" + `);
  const d = document.getElementById(` + "`" + id + `-menu` + "`" + `);
  const s = d.querySelector("summary").getBoundingClientRect();
  const m = d.querySelector(".rst-dropdown__menu");
  const r = m.getBoundingClientRect();
  const b = box.getBoundingClientRect();
  const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
  return JSON.stringify({
    SummaryTop: s.top, SummaryBottom: s.bottom,
    ScrollerTop: b.top, ScrollerBottom: b.bottom,
    MenuTop: r.top, MenuBottom: r.bottom,
    Painted: !!(hit && (hit === m || m.contains(hit))),
    Hit: hit ? hit.tagName + "." + (hit.getAttribute("class") || "") : "nothing",
    Visibility: getComputedStyle(m).positionVisibility,
    Supports: CSS.supports("position-visibility: anchors-visible"),
    SupportsBogus: CSS.supports("position-visibility: rastrillo-no-such-value"),
  });
})()`
	}
	get := func(id string) orphanReading {
		t.Helper()
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(read(id), &raw)); err != nil {
			t.Fatalf("reading %s: %v", id, err)
		}
		var out orphanReading
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("reading %s (%q): %v", id, raw, err)
		}
		return out
	}

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(900, 700),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`#subject-menu`, chromedp.ByQuery),
		chromedp.Click(`#subject-menu > summary`, chromedp.ByQuery),
		chromedp.Click(`#control-menu > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`#subject-menu .rst-dropdown__menu`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("opening the menus: %v", err)
	}

	before := get("subject")
	t.Logf("before the scroll: summary %.0f..%.0f in a scroller %.0f..%.0f; menu %.0f..%.0f, painted=%v (hit %s, position-visibility %s)",
		before.SummaryTop, before.SummaryBottom, before.ScrollerTop, before.ScrollerBottom,
		before.MenuTop, before.MenuBottom, before.Painted, before.Hit, before.Visibility)

	if !before.Supports {
		t.Fatalf("this engine does not support position-visibility: anchors-visible. tokens.css writes it; if it has been renamed, every menu below is orphaned again and nothing else would say so.")
	}
	if before.SupportsBogus {
		t.Fatal("CSS.supports agreed to a position-visibility value that does not exist; the support check above is worthless")
	}
	if !before.Painted {
		t.Fatalf("the menu is not painted before anything has scrolled — the middle of it hits %s. This drive is measuring the wrong element.", before.Hit)
	}

	// Take the anchor away.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { for (const id of ["subject", "control"]) document.getElementById(id).scrollTop = 150; return "ok"; })()`,
		new(string))); err != nil {
		t.Fatalf("scrolling the anchors away: %v", err)
	}
	// Anchor positioning — placement and position-visibility both —
	// settles during paint, not during the scroll, so the reading waits
	// for two frames rather than racing the compositor.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r("ok"))))`,
		new(string), func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("waiting for the anchored positions to settle: %v", err)
	}

	after, control := get("subject"), get("control")
	t.Logf("after the scroll: subject summary %.0f..%.0f, scroller %.0f..%.0f, menu %.0f..%.0f painted=%v (hit %s); control menu %.0f..%.0f painted=%v (hit %s)",
		after.SummaryTop, after.SummaryBottom, after.ScrollerTop, after.ScrollerBottom,
		after.MenuTop, after.MenuBottom, after.Painted, after.Hit,
		control.MenuTop, control.MenuBottom, control.Painted, control.Hit)

	// THE FIXTURE'S OWN PROOF, in two halves. The anchor really did
	// leave the scroller that clips it — and it is still at a
	// coordinate inside the window, which is what makes this the
	// orphan case rather than a menu that simply scrolled off the top
	// of the page with everything else.
	if after.SummaryBottom > after.ScrollerTop {
		t.Fatalf("the summary's foot is at %.0f and the scroller's head is at %.0f — the anchor has not been scrolled out of the box that clips it, so nothing below is being tested", after.SummaryBottom, after.ScrollerTop)
	}
	if after.SummaryTop < 0 {
		t.Fatalf("the summary is at %.0f, off the top of the WINDOW rather than merely out of its scroller. A menu that follows its anchor off the page is not orphaned, so this fixture would pass with the declaration deleted.", after.SummaryTop)
	}
	// THE CONTROL: the same scroll, the declaration overridden back.
	if !control.Painted {
		t.Fatalf("the control menu — position-visibility: always — is also gone after the same scroll; the middle of it hits %s. Whatever this drive is measuring, it is not the declaration under test.", control.Hit)
	}
	// THE SUBJECT.
	if after.Painted {
		t.Errorf("the menu is still painted at %.0f..%.0f with its button scrolled out of the box that clips it — it is floating over unrelated content with nothing under it", after.MenuTop, after.MenuBottom)
	}
}
