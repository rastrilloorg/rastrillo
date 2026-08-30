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

	"github.com/chromedp/chromedp"

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
