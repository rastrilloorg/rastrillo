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
	RailHeight   int
	FootGap      int
	MenuLift     int
	LocaleBefore bool
}

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

	var page strings.Builder
	if err := tmpl.ExecuteTemplate(&page, "layout", nil); err != nil {
		t.Fatalf("rendering the sidebar shell: %v", err)
	}
	html := page.String()

	mux := http.NewServeMux()
	stylesheets(t, mux)
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
		chromedp.Evaluate(`(() => {
		  const rail = document.querySelector(".rst-shell__rail");
		  const person = document.querySelector("#rail-person");
		  const loc = document.querySelector("#rail-locale");
		  const sum = loc.querySelector("summary");
		  const menu = loc.querySelector(".rst-dropdown__menu");
		  const r = rail.getBoundingClientRect();
		  const p = person.getBoundingClientRect();
		  const s = sum.getBoundingClientRect();
		  const m = menu.getBoundingClientRect();
		  return JSON.stringify({
		    RailHeight: Math.round(r.height),
		    FootGap: Math.round(r.bottom - p.bottom),
		    MenuLift: Math.round(s.top - m.bottom),
		    LocaleBefore: !!(loc.compareDocumentPosition(person) & Node.DOCUMENT_POSITION_FOLLOWING)
		  });
		})()`, &raw),
	); err != nil {
		t.Fatalf("driving the sidebar rail: %v", err)
	}

	var got railReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading the measurement (%q): %v", raw, err)
	}

	// Sanity: the rail is the full-height column this claim is about.
	if got.RailHeight < 600 {
		t.Fatalf("the rail is only %dpx tall in a 900px window; it is not the sticky full-height rail this drive measures", got.RailHeight)
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
		chromedp.Evaluate(`(() => {
		  const rail = document.querySelector(".rst-shell__rail");
		  const person = document.querySelector("#rail-person");
		  const loc = document.querySelector("#rail-locale");
		  const sum = loc.querySelector("summary");
		  const menu = loc.querySelector(".rst-dropdown__menu");
		  const r = rail.getBoundingClientRect();
		  const p = person.getBoundingClientRect();
		  const s = sum.getBoundingClientRect();
		  const m = menu.getBoundingClientRect();
		  return JSON.stringify({
		    RailHeight: Math.round(r.height),
		    FootGap: Math.round(r.bottom - p.bottom),
		    MenuLift: Math.round(s.top - m.bottom),
		    LocaleBefore: !!(loc.compareDocumentPosition(person) & Node.DOCUMENT_POSITION_FOLLOWING)
		  });
		})()`, &narrow),
	); err != nil {
		t.Fatalf("driving the collapsed rail: %v", err)
	}
	var small railReading
	if err := json.Unmarshal([]byte(narrow), &small); err != nil {
		t.Fatalf("reading the collapsed measurement (%q): %v", narrow, err)
	}
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
