//go:build browser

// The browser drives for the gallery's own script. gallery.js is the
// only JavaScript in this tree that is not the framework's, and the
// only one whose whole job — write an attribute on <html>, remember it,
// reveal a control, hide half a list of links — is invisible to a Go
// test: nothing about it shows up in the rendered bytes, because
// everything it does happens after the bytes arrive.
//
// Build-tagged like ui/browser_test.go, and for the same reasons. Run
// them with:
//
//	go test -tags browser ./internal/designsystem/
//
// Two tests, one journey each. The scheme toggle: load, click Dark,
// reload, click System — the reload is the half a unit test could never
// fake, because persistence that survives a fresh document is the only
// kind worth having. The sidebar filter: type a partial's name, type a
// family's, type junk, press Escape — and the collapse a reader made by
// hand, which a search has to borrow and give back.
package designsystem

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/carlosframework/rastrillo/harness"
	"github.com/carlosframework/rastrillo/ui"
)

// treeHandler serves the rendered tree at the mount path the pages
// expect. Every URL in a page is absolute under /design-system/, so
// serving it anywhere else would 404 the stylesheet and the script and
// the drive would be measuring an unstyled, unscripted page.
func treeHandler(t *testing.T) http.Handler {
	t.Helper()
	files, err := Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	types := map[string]string{
		".html": "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "text/javascript; charset=utf-8",
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, mountPrefix)
		body, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if ct, ok := types[path.Ext(name)]; ok {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(body)
	})
}

// TestSchemeToggleDrivesTheWholeJourney is the drive.
//
// Bug classes it exists to catch — each of which leaves a page that
// renders perfectly and says nothing wrong:
//
//   - the toggle writes data-theme on the wrong element, or writes
//     "system" as a third value, so the themes' two toggle rules never
//     match and clicking does nothing visible;
//   - the choice is applied but never stored, so it evaporates on the
//     next page in the tree;
//   - the choice is stored but never re-applied, which is the same bug
//     from the other end;
//   - System sets an attribute instead of removing one, so a reader can
//     leave the OS but never go back to it;
//   - aria-pressed is painted once at render and never repainted, so a
//     screen reader hears System while the page is dark;
//   - the reveal marker is never set, so the control the whole feature
//     lives in stays display:none for everybody.
func TestSchemeToggleDrivesTheWholeJourney(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })

	// Same budget and the same reasoning as ui/browser_test.go's: this
	// is wall-clock against a real browser, and the deadline exists so
	// a hang fails as itself rather than to race a busy machine.
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	url := rig.Origin + indexHref(RootTheme(), "en")

	var (
		jsMarker                          string
		toggleShown, toggleHiddenWithNoJS bool
		freshTheme, darkTheme             string
		afterReloadTheme, systemTheme     string
		freshStored, darkStored           string
		reloadPressed, systemPressed      string
		afterSystemStored                 string
	)

	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}

	// pressed reads the label of whichever scheme button is currently
	// pressed. One probe for the whole ARIA question: exactly one
	// button carries aria-pressed="true", and it is the right one.
	const pressed = `(() => {
	  const on = document.querySelectorAll('[data-ds-scheme][aria-pressed="true"]');
	  return on.length === 1 ? on[0].dataset.dsScheme : "pressed=" + on.length;
	})()`

	const stored = `localStorage.getItem("rst-ds-scheme") ?? "(none)"`
	const themeAttr = `document.documentElement.getAttribute("data-theme") ?? "(none)"`

	if err := chromedp.Run(ctx,
		chromedp.Navigate(url), at("navigated"),
		chromedp.WaitVisible(`[data-ds-scheme="dark"]`, chromedp.ByQuery), at("toggle-visible"),

		// A first visit is System: no attribute, nothing stored, and
		// the toggle is on screen because gallery.js said so.
		chromedp.Evaluate(`document.documentElement.getAttribute("data-rst-js") ?? "(none)"`, &jsMarker),
		chromedp.Evaluate(themeAttr, &freshTheme),
		chromedp.Evaluate(stored, &freshStored),
		chromedp.Evaluate(`getComputedStyle(document.querySelector(".ds-scheme")).display !== "none"`, &toggleShown),
		// The scriptless half, asked of the real engine rather than of
		// the stylesheet text: take the marker away and the control
		// goes with it, which is exactly what a reader with JavaScript
		// off sees.
		chromedp.Evaluate(`(() => {
		  document.documentElement.removeAttribute("data-rst-js");
		  const gone = getComputedStyle(document.querySelector(".ds-scheme")).display === "none";
		  document.documentElement.setAttribute("data-rst-js", "on");
		  return gone;
		})()`, &toggleHiddenWithNoJS),
		at("first-visit-read"),

		// Choose Dark.
		chromedp.Click(`[data-ds-scheme="dark"]`, chromedp.ByQuery), at("clicked-dark"),
		chromedp.WaitReady(`html[data-theme="dark"]`, chromedp.ByQuery), at("dark-applied"),
		chromedp.Evaluate(themeAttr, &darkTheme),
		chromedp.Evaluate(stored, &darkStored),

		// Reload: the choice has to survive a fresh document, and it
		// has to be on the element before the first paint — which is
		// what WaitReady on the attribute selector is asking.
		chromedp.Navigate(url), at("reloaded"),
		chromedp.WaitReady(`html[data-theme="dark"]`, chromedp.ByQuery), at("dark-persisted"),
		chromedp.Evaluate(themeAttr, &afterReloadTheme),
		chromedp.Evaluate(pressed, &reloadPressed),

		// Back to System: the attribute goes, and so does the memory.
		chromedp.Click(`[data-ds-scheme="system"]`, chromedp.ByQuery), at("clicked-system"),
		chromedp.WaitReady(`html:not([data-theme])`, chromedp.ByQuery), at("system-applied"),
		chromedp.Evaluate(themeAttr, &systemTheme),
		chromedp.Evaluate(stored, &afterSystemStored),
		chromedp.Evaluate(pressed, &systemPressed),
		at("done"),
	); err != nil {
		t.Fatalf("drive failed after %q: %v", reached, err)
	}

	if jsMarker != "on" {
		t.Errorf("data-rst-js = %q, want %q — the toggle is revealed by this marker and nothing else", jsMarker, "on")
	}
	if !toggleShown {
		t.Error("the scheme toggle is not visible to a reader who has JavaScript")
	}
	if !toggleHiddenWithNoJS {
		t.Error("the scheme toggle is still visible without data-rst-js — with scripts off it would look like a control that works")
	}
	if freshTheme != "(none)" {
		t.Errorf("a first visit set data-theme=%q; System is no attribute at all", freshTheme)
	}
	if freshStored != "(none)" {
		t.Errorf("a first visit stored %q; nothing should be remembered until something is chosen", freshStored)
	}
	if darkTheme != "dark" {
		t.Errorf("after clicking Dark, data-theme = %q, want %q", darkTheme, "dark")
	}
	if darkStored != "dark" {
		t.Errorf("after clicking Dark, localStorage holds %q, want %q", darkStored, "dark")
	}
	if afterReloadTheme != "dark" {
		t.Errorf("after a reload, data-theme = %q, want %q — the choice did not survive the new document", afterReloadTheme, "dark")
	}
	if reloadPressed != "dark" {
		t.Errorf("after a reload, the pressed button is %q, want %q — the page looks dark and reads as System", reloadPressed, "dark")
	}
	if systemTheme != "(none)" {
		t.Errorf("after choosing System, data-theme = %q, want no attribute", systemTheme)
	}
	if afterSystemStored != "(none)" {
		t.Errorf("after choosing System, localStorage still holds %q", afterSystemStored)
	}
	if systemPressed != "system" {
		t.Errorf("after choosing System, the pressed button is %q, want %q", systemPressed, "system")
	}
}

// railState is one reading of the sidebar: what it is showing, what it
// has folded away, and where the focus is. Every assertion below is
// made against one of these rather than against a handful of separate
// evaluations, so a step's whole picture is caught at one instant.
type railState struct {
	Links int      `json:"links"`
	Shown []string `json:"shown"`
	// Pages is the rail's page links — the sections that have nothing
	// anchored under them yet and are drawn as a plain link rather than
	// a disclosure. They are how a reader reaches that page, not
	// entries in a list of anchors, so the filter leaves them alone and
	// this is where that is asserted rather than assumed.
	Pages   []string `json:"pages"`
	Folded  int      `json:"folded"`
	Open    []bool   `json:"open"`
	Empty   bool     `json:"empty"`
	Focused string   `json:"focused"`
	Value   string   `json:"value"`
	BoxSeen bool     `json:"boxSeen"`
}

// railProbe reads the whole rail in one evaluation.
//
// Every "is it there" question is asked of getComputedStyle, never of
// the .hidden property. That is not fussiness: gallery.js sets the
// property, and what actually takes a rail link off the screen is a CSS
// rule in dsCSS outranking the shell's display:block. A probe reading
// .hidden would be asking the script to confirm its own opinion — and
// would pass, green, with that rule deleted and sixty "hidden" links
// painted down the page. It did.
const railProbe = `(() => {
  const seen = el => getComputedStyle(el).display !== "none";
  const links = Array.from(document.querySelectorAll("#ds-nav details a"));
  const pages = Array.from(document.querySelectorAll("#ds-nav > a"));
  const secs = Array.from(document.querySelectorAll("#ds-nav details"));
  const box = document.querySelector("[data-ds-filter]");
  const empty = document.querySelector("[data-ds-filter-empty]");
  return {
    links: links.length,
    shown: links.filter(seen).map(a => a.getAttribute("href")),
    pages: pages.filter(seen).map(a => a.getAttribute("href")),
    folded: secs.filter(d => !seen(d)).length,
    open: secs.map(d => d.open),
    empty: seen(empty),
    focused: document.activeElement ? document.activeElement.id : "",
    value: box.value,
    boxSeen: seen(box.closest(".ds-search")),
  };
})()`

// showing reports whether the rail is showing a link to this fragment.
func showing(state railState, href string) bool {
	for _, s := range state.Shown {
		if s == href {
			return true
		}
	}
	return false
}

// TestTheSidebarFilterDrivesTheWholeJourney is the second drive: the
// type-to-filter box over the rail.
//
// Everything it asserts is invisible to a Go test, because none of it
// is in the rendered bytes — the page ships one complete list of links
// and the whole feature is what happens to that list after a keystroke.
// The bug classes, each of which leaves a page that renders perfectly:
//
//   - the filter matches nothing, or matches everything, because the
//     folding is applied to one side only;
//   - a section whose every entry is hidden stays on screen as a
//     heading over a gap;
//   - the family label inside Partials outlives the partials under it,
//     or disappears when the family's own name is what matched;
//   - "no matches" never appears, or never goes away;
//   - Escape does not clear, or clears and leaves the rail filtered;
//   - a section the reader collapsed by hand is left open by a search
//     they have finished with;
//   - the box is visible with scripts off, which is a control that
//     cannot work.
func TestTheSidebarFilterDrivesTheWholeJourney(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// The components page: the rail is the same on all five, and this
	// is the one whose own section is the long one, so a filter that
	// folded the current section away would show up here first.
	url := rig.Origin + pageHref(RootTheme(), "en", fileOf("components"))
	// The rail's entries are absolute page addresses with a fragment on
	// the end, the current page's included, so every expectation below
	// names the page it links as well as the fragment.
	on := func(locale, kind, id string) string {
		return anchorHrefIn(RootTheme(), locale, fileOf(kind), id)
	}

	var (
		fresh, scriptless, badge, family, titled railState
		junk, cleared, handled, restored         railState
		accented, unaccented                     railState
		navWithNoJS                              int
	)

	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}

	// Each step is a query on its own rather than one appended to the
	// last, so the box is emptied through the same input event a reader
	// selecting-all and typing over would raise.
	typing := func(q string) chromedp.Tasks {
		return chromedp.Tasks{
			chromedp.Focus(`#ds-filter`, chromedp.ByQuery),
			chromedp.Evaluate(`(() => {
			  const box = document.querySelector("[data-ds-filter]");
			  box.value = "";
			  box.dispatchEvent(new Event("input", {bubbles: true}));
			})()`, nil),
			chromedp.SendKeys(`#ds-filter`, q, chromedp.ByQuery),
		}
	}

	if err := chromedp.Run(ctx,
		// A window wide enough for the rail to be a rail. Below 800px
		// the sidebar shell folds it behind the <details> chrome strip
		// and it is display:none until a reader opens it — correct
		// behaviour, and not the behaviour this drive is about, so the
		// viewport is stated rather than inherited from whatever size
		// the harness happened to open.
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url), at("navigated"),
		chromedp.WaitVisible(`#ds-filter`, chromedp.ByQuery), at("filter-visible"),
		chromedp.Evaluate(railProbe, &fresh),

		// The scriptless half, asked of the real engine: take the
		// marker away and the box goes with it, while every link in the
		// rail stays exactly where it was.
		chromedp.Evaluate(`(() => {
		  document.documentElement.removeAttribute("data-rst-js");
		  const seen = el => getComputedStyle(el).display !== "none";
		  const links = Array.from(document.querySelectorAll("#ds-nav details a"));
		  const pages = Array.from(document.querySelectorAll("#ds-nav > a"));
		  const state = {
		    links: links.length,
		    shown: links.filter(seen).map(a => a.getAttribute("href")),
		    pages: pages.filter(seen).map(a => a.getAttribute("href")),
		    folded: 0, open: [], empty: false, focused: "", value: "",
		    boxSeen: seen(document.querySelector(".ds-search")),
		  };
		  document.documentElement.setAttribute("data-rst-js", "on");
		  return state;
		})()`, &scriptless),
		chromedp.Evaluate(`document.querySelectorAll("#ds-nav details a").length`, &navWithNoJS),
		at("scriptless-read"),

		// A partial's own name: one entry left in Partials, every other
		// section folded away.
		typing("badge"), at("typed-badge"),
		chromedp.Evaluate(railProbe, &badge),

		// A family's name stands for the run under it: matching "form"
		// keeps the whole family, heading included.
		typing("form"), at("typed-form"),
		chromedp.Evaluate(railProbe, &family),

		// A section's own name: the whole section, and nothing from any
		// other. "shells" is not a substring of any entry's label
		// anywhere else in the rail, so what is left can only have come
		// from the summary matching.
		typing("shells"), at("typed-section-name"),
		chromedp.Evaluate(railProbe, &titled),

		// Junk: nothing left, and the page's own sentence saying so.
		typing("zzqqxx"), at("typed-junk"),
		chromedp.Evaluate(railProbe, &junk),

		// Escape clears it, from inside the box, with focus unmoved.
		chromedp.KeyEvent(kb.Escape), at("escaped"),
		chromedp.Evaluate(railProbe, &cleared),

		// The same key again, this time as an untrusted event. Chromium
		// clears an <input type="search"> on Escape all by itself, so
		// the real keypress above proves the journey and proves nothing
		// about this file: it passes just as happily with the handler
		// deleted. A synthetic event gets no default action, so what
		// clears the box here is gallery.js or nothing — and Firefox,
		// which has no native Escape on a search field, is the engine
		// that makes the difference matter.
		typing("zzqqxx"), at("typed-junk-again"),
		chromedp.Evaluate(`document.querySelector("[data-ds-filter]").dispatchEvent(
		  new KeyboardEvent("keydown", {key: "Escape", bubbles: true, cancelable: true}))`, nil),
		chromedp.Evaluate(railProbe, &handled),

		// A section the reader collapses by hand is opened by a query
		// that finds something in it, and folded back when the query
		// goes.
		chromedp.Evaluate(`document.querySelectorAll("#ds-nav details")[1].open = false`, nil),
		typing("badge"), at("typed-into-collapsed"),
		chromedp.KeyEvent(kb.Escape), at("escaped-again"),
		chromedp.Evaluate(railProbe, &restored),

		// The Spanish page, for the half of the fold a monolingual
		// journey can never reach. "Presentación" is a family heading
		// there and "Superficies y líneas" a token group; a reader
		// typing them off an English keyboard types neither accent, and
		// with the fold replaced by a plain toLowerCase both queries
		// find nothing at all.
		chromedp.Navigate(rig.Origin+pageHref(RootTheme(), "es", fileOf("components"))), at("navigated-es"),
		chromedp.WaitVisible(`#ds-filter`, chromedp.ByQuery), at("es-filter-visible"),
		typing("presentacion"), at("typed-unaccented-family"),
		chromedp.Evaluate(railProbe, &accented),
		typing("lineas"), at("typed-unaccented-group"),
		chromedp.Evaluate(railProbe, &unaccented),
		at("done"),
	); err != nil {
		t.Fatalf("drive failed after %q: %v", reached, err)
	}

	if !fresh.BoxSeen {
		t.Error("the filter box is not visible to a reader who has JavaScript")
	}
	if fresh.Links < 50 {
		t.Errorf("the rail holds %d links; the page has far more than that to link", fresh.Links)
	}
	if len(fresh.Shown) != fresh.Links {
		t.Errorf("%d of %d rail links are hidden before anything is typed", fresh.Links-len(fresh.Shown), fresh.Links)
	}
	if fresh.Empty {
		t.Error("the no-matches line is on screen before anything is typed")
	}
	// One section arrives open: the page you are on. The rail carries
	// the whole vocabulary on every page since the split, so opening
	// all of it would be six hundred lines of links over the reader's
	// content — the folded sections are what makes a shared rail
	// usable, and the filter is what reaches into them.
	var open int
	for _, o := range fresh.Open {
		if o {
			open++
		}
	}
	if open != 1 {
		t.Errorf("%d of %d sidebar sections arrive open, want exactly 1 (the page you are on)", open, len(fresh.Open))
	}
	if len(fresh.Open) > 1 && !fresh.Open[1] {
		t.Error("the Components section is not the one open on the components page")
	}

	if scriptless.BoxSeen {
		t.Error("the filter box is still visible without data-rst-js — with scripts off it would look like a control that works")
	}
	if len(scriptless.Shown) != fresh.Links || navWithNoJS != fresh.Links {
		t.Errorf("with the marker off the rail shows %d of %d links; the nav is complete with or without a script", len(scriptless.Shown), fresh.Links)
	}

	if !showing(badge, on("en", "components", "partial-badge")) {
		t.Error(`typing "badge" hid the badge partial`)
	}
	if showing(badge, on("en", "components", "partial-meter")) {
		t.Error(`typing "badge" left the meter partial on screen`)
	}
	if showing(badge, on("en", "tokens", "tokens-accent")) {
		t.Error(`typing "badge" left a token group on screen`)
	}
	if badge.Folded == 0 {
		t.Error(`typing "badge" folded no section away; Tokens, Shells and Demos have nothing in them that matches`)
	}
	if badge.Empty {
		t.Error(`typing "badge" said there were no matches`)
	}

	if !showing(family, on("en", "components", "family-form")) {
		t.Error(`typing "form" hid the Form family's own heading`)
	}
	if !showing(family, on("en", "components", "partial-field-check")) {
		t.Error(`typing "form" hid field-check, which is in the family that matched`)
	}
	// Display is not the family to check here: form-error lives in it,
	// so "form" legitimately keeps it. List screen has nothing in it
	// that matches, which is the case worth asserting.
	if showing(family, on("en", "components", "family-list-screen")) {
		t.Error(`typing "form" left the List screen family's heading over an empty gap`)
	}
	if !showing(family, on("en", "components", "partial-form-error")) {
		t.Error(`typing "form" hid form-error, which matches on its own name`)
	}

	if !showing(titled, on("en", "shells", "shell-column")) || !showing(titled, on("en", "shells", "shell-topbar")) || !showing(titled, on("en", "shells", "shell-sidebar")) {
		t.Errorf(`typing a section's own name ("shells") did not reveal the section: %v`, titled.Shown)
	}
	if len(titled.Shown) != 3 {
		t.Errorf(`typing "shells" left %d entries on screen, want the section's 3: %v`, len(titled.Shown), titled.Shown)
	}

	if len(junk.Shown) != 0 {
		t.Errorf("junk left %d entries on screen: %v", len(junk.Shown), junk.Shown)
	}
	// The page links are not entries; a query that matches nothing
	// still leaves a reader a way to every page of the gallery.
	if len(junk.Pages) != len(fresh.Pages) || len(fresh.Pages) == 0 {
		t.Errorf("junk left %d of %d page links on screen; a page link is how you leave this page, not a search result", len(junk.Pages), len(fresh.Pages))
	}
	if !junk.Empty {
		t.Error("junk matched nothing and the page never said so")
	}
	if junk.Focused != "ds-filter" {
		t.Errorf("focus moved to %q while typing; the filter must not take it anywhere", junk.Focused)
	}

	if cleared.Value != "" {
		t.Errorf("Escape left %q in the box", cleared.Value)
	}
	if len(cleared.Shown) != cleared.Links {
		t.Errorf("Escape cleared the box and left %d of %d entries hidden", cleared.Links-len(cleared.Shown), cleared.Links)
	}
	if cleared.Empty {
		t.Error("Escape cleared the box and left the no-matches line up")
	}
	if cleared.Focused != "ds-filter" {
		t.Errorf("Escape moved focus to %q", cleared.Focused)
	}

	if handled.Value != "" {
		t.Errorf("a synthetic Escape left %q in the box — gallery.js is not handling the key, the browser is", handled.Value)
	}
	if len(handled.Shown) != handled.Links {
		t.Errorf("a synthetic Escape cleared the box and left %d of %d entries hidden", handled.Links-len(handled.Shown), handled.Links)
	}

	// Both accent checks are stated as "the accented label is on
	// screen", so a fold that has stopped folding fails as an empty
	// rail rather than as a count nobody can read.
	if !showing(accented, on("es", "components", "family-display")) {
		t.Errorf(`on the Spanish page, "presentacion" did not find "Presentación": %v`, accented.Shown)
	}
	if !showing(accented, on("es", "components", "partial-badge")) {
		t.Error(`"presentacion" found the family and not the partials under it`)
	}
	if showing(accented, on("es", "components", "family-form")) {
		t.Error(`"presentacion" left another family's heading on screen`)
	}
	if !showing(unaccented, on("es", "tokens", "tokens-surfaces-and-lines")) {
		t.Errorf(`on the Spanish page, "lineas" did not find "Superficies y líneas": %v`, unaccented.Shown)
	}

	if len(restored.Open) < 2 || restored.Open[1] {
		t.Error("a section the reader collapsed by hand was left open by a search that has been cleared")
	}
	if len(restored.Shown) != restored.Links {
		t.Errorf("after the search was cleared, %d of %d entries are still hidden", restored.Links-len(restored.Shown), restored.Links)
	}
}

// ── The preview widget ───────────────────────────────────────────────

// framing is one reading of one preview widget, taken from the engine.
type framing struct {
	// The frame's own layout: the width it lays out at (the virtual
	// viewport its document sees) and the width it occupies on the
	// page after the scale.
	Virtual float64
	Painted float64
	// What the framed document itself reports: the width its <html>
	// laid out to, and whether its body really has the sample in it.
	Inner float64
	Body  string
	// Which panel is on screen.
	FrameShown bool
	CodeShown  bool
	// The colour scheme the framed document resolved to, and the
	// background it painted with it. It should be the gallery's own —
	// not by inheritance, which does not reach a framed document that
	// declares a scheme of its own, but because gallery.js writes the
	// attribute on each frame. See the assertion at the foot of this
	// test for the whole of that argument.
	Scheme string
}

// TestPreviewWidgetDrivesTheWholeJourney is the drive for the widget
// the whole page is now built out of.
//
// Bug classes it exists to catch, all of which leave a page that
// renders and reads perfectly:
//
//   - the Desktop frame lays out at the reader's own width instead of
//     1200px, so the "desktop rendering" is whatever the column
//     happens to be and the mobile tab shows the same thing twice;
//   - the scale is dropped, so the frame is 1200px wide inside a
//     700px column and the sample is cropped;
//   - the tabs are wired to something that needs JavaScript, so a
//     reader with scripts off has one view and no way to say so;
//   - the frame's document is not the theme the gallery is in, or does
//     not follow the scheme the reader chose;
//   - a link inside a sample still points at /posts/1/edit, so
//     clicking one in the preview navigates the frame to a 404.
func TestPreviewWidgetDrivesTheWholeJourney(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// The components page: since the split it is the one the callout
	// sample lives on, and the one that frames most of the tree.
	url := rig.Origin + pageHref(RootTheme(), "en", fileOf("components"))

	// One example, named rather than picked by position: the callout
	// is small, has no script and no menu, and is on the page in every
	// language.
	const widget = `document.querySelector("#partial-callout .ds-view")`

	// read measures the chosen widget. The frame is made eager first —
	// every frame on this page is loading="lazy", and 110 documents is
	// exactly why — and the reading waits for its document to exist.
	read := func(sel string) string {
		return `(() => {
		  const v = ` + sel + `;
		  const f = v.querySelector(".ds-view__frame");
		  const box = v.querySelector(".ds-view__box");
		  const stage = v.querySelector(".ds-view__stage");
		  const code = v.querySelector(".ds-view__code");
		  const d = f.contentDocument;
		  return JSON.stringify({
		    Virtual: parseFloat(getComputedStyle(f).width),
		    Painted: box.getBoundingClientRect().width,
		    Inner: d ? d.documentElement.getBoundingClientRect().width : -1,
		    Body: d ? d.body.innerHTML.trim().slice(0, 120) : "",
		    FrameShown: getComputedStyle(stage).display !== "none",
		    CodeShown: code ? getComputedStyle(code).display !== "none" : false,
		    Scheme: d ? getComputedStyle(d.documentElement).colorScheme + " " + getComputedStyle(d.body).backgroundColor : ""
		  });
		})()`
	}

	const eager = `(() => {
	  document.querySelectorAll(".ds-view__frame").forEach(f => { f.loading = "eager"; });
	  return "ok";
	})()`

	var desktop, mobile, code, dark, scriptless, resized string
	var deadLinks int
	var mechanism bool

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#partial-callout .ds-view__frame`, chromedp.ByQuery),
		chromedp.Evaluate(eager, new(string)),
		chromedp.Sleep(2*time.Second),

		// 1. Desktop, the tab the page opens on with nothing clicked.
		chromedp.Evaluate(read(widget), &desktop),

		// 2. Mobile. Clicking the LABEL, the way a reader does — which
		// is also the proof that the label/input pairing is right.
		chromedp.Click(`#partial-callout .ds-view__tab--m`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(read(widget), &mobile),

		// 3. Code.
		chromedp.Click(`#partial-callout .ds-view__tab--c`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(read(widget), &code),

		// 4. Every link in every framed document goes nowhere. Read
		// off the documents themselves, not off the page's bytes: this
		// is the browser's own idea of where an href points.
		chromedp.Evaluate(`(() => {
		  let bad = 0;
		  for (const f of document.querySelectorAll(".ds-view__frame")) {
		    const d = f.contentDocument;
		    if (!d) continue;
		    for (const a of d.querySelectorAll("a[href]")) {
		      const href = a.getAttribute("href");
		      if (href !== "#" && !href.startsWith("#") && !href.startsWith("/design-system/")) bad++;
		    }
		  }
		  return bad;
		})()`, &deadLinks),

		// 5. Back to Desktop — the grip is a control on the frame, and
		// the frame is not on screen while Code is. Which is also a
		// second reading of the Desktop tab, this time arrived at by
		// clicking rather than by the checked attribute.
		chromedp.Click(`#partial-callout .ds-view__tab:first-of-type`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),

		// The grip. resize: vertical is a visible control, so it
		// has to do something: the box is what the reader drags, and
		// the frame takes its height back off the box, so the
		// document inside really does get taller. It did not, once —
		// a fixed height on the frame left 300px of empty box under an
		// unchanged rendering, which is the bug this leg pins.
		chromedp.Evaluate(`(() => {
		  const box = document.querySelector("#partial-callout .ds-view__box");
		  const f = document.querySelector("#partial-callout .ds-view__frame");
		  const before = f.contentDocument.documentElement.clientHeight;
		  box.style.height = Math.round(box.getBoundingClientRect().height * 2) + "px";
		  return JSON.stringify({
		    before: before,
		    after: f.contentDocument.documentElement.clientHeight,
		    painted: Math.round(f.getBoundingClientRect().height),
		    box: Math.round(box.getBoundingClientRect().height)
		  });
		})()`, &resized),

		// 6. The reader chooses Dark, and the previews follow without
		// a line of script inside them.
		chromedp.Click(`[data-ds-scheme="dark"]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(read(widget), &dark),
	); err != nil {
		t.Fatalf("driving the widget: %v", err)
	}

	// 7. The same journey with JavaScript switched off at the engine.
	// A fresh tab, because scripts have already run in the one above.
	noJS, cancelNoJS := chromedp.NewContext(rig.Context())
	defer cancelNoJS()
	noJSCtx, cancelNoJSTimeout := context.WithTimeout(noJS, 120*time.Second)
	defer cancelNoJSTimeout()
	if err := chromedp.Run(noJSCtx,
		chromedp.EmulateViewport(1280, 900),
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#partial-callout .ds-view__frame`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// The tabs are radios and labels; the click is the browser's
		// own, and :has() does the rest.
		chromedp.Click(`#partial-callout .ds-view__tab--c`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`(() => {
		  const v = document.querySelector("#partial-callout .ds-view");
		  const stage = v.querySelector(".ds-view__stage");
		  const code = v.querySelector(".ds-view__code");
		  return JSON.stringify({
		    FrameShown: getComputedStyle(stage).display !== "none",
		    CodeShown: getComputedStyle(code).display !== "none",
		    Body: "", Virtual: 0, Painted: 0, Inner: 0, Scheme: ""
		  });
		})()`, &scriptless),
	); err != nil {
		t.Fatalf("driving the widget with scripts off: %v", err)
	}
	// Evaluate itself is script execution, so the reading above is
	// taken through the debugger with page scripts disabled — the
	// click and the CSS are the page's own. Assert that gallery.js
	// really was inert, or the leg proves nothing.
	if err := chromedp.Run(noJSCtx, chromedp.Evaluate(
		`document.documentElement.hasAttribute("data-rst-js")`, &mechanism)); err != nil {
		t.Fatalf("checking the scriptless page: %v", err)
	}
	if mechanism {
		t.Error("gallery.js ran on the scriptless page — the leg below proves nothing")
	}

	var d, m, c, dk, off framing
	for _, p := range []struct {
		raw  string
		into *framing
	}{{desktop, &d}, {mobile, &m}, {code, &c}, {dark, &dk}, {scriptless, &off}} {
		if err := json.Unmarshal([]byte(p.raw), p.into); err != nil {
			t.Fatalf("reading a framing (%q): %v", p.raw, err)
		}
	}

	// Desktop: the frame lays out at 1200px whatever the column is,
	// and is painted at the column's width instead.
	if d.Virtual != 1200 {
		t.Errorf("the desktop frame lays out at %gpx, want 1200 — a preview that is only the reader's own width is not a desktop preview", d.Virtual)
	}
	if d.Inner != 1200 {
		t.Errorf("the framed document reports a %gpx viewport, want 1200", d.Inner)
	}
	if d.Painted >= 1200 || d.Painted < 200 {
		t.Errorf("the desktop frame is painted %gpx wide in a 1280px window; it is not being scaled into its column", d.Painted)
	}
	// The scale factor, logged rather than written down anywhere: it is
	// 100cqw / --ds-w, so it moves whenever .rst-page's cap moves, and
	// a number in a comment would be a number nobody re-measures. This
	// is where to read it after changing the column.
	t.Logf("desktop preview: a %gpx virtual page painted %gpx wide in a 1280px window — scale %.3f", d.Virtual, d.Painted, d.Painted/d.Virtual)
	if !d.FrameShown || d.CodeShown {
		t.Error("the page does not open on the framed rendering")
	}
	if !strings.Contains(d.Body, "rst-callout") {
		t.Errorf("the framed document is not the callout sample: %q", d.Body)
	}

	// Mobile: the same document, 390px wide, unscaled in this window.
	if m.Virtual != 390 {
		t.Errorf("the mobile frame lays out at %gpx, want 390", m.Virtual)
	}
	if m.Inner != 390 {
		t.Errorf("the framed document reports a %gpx viewport on the mobile tab, want 390", m.Inner)
	}
	if m.Body != d.Body {
		t.Error("Desktop and Mobile are not the same document")
	}

	// Code: the frame goes, the source arrives.
	if c.FrameShown || !c.CodeShown {
		t.Errorf("the Code tab shows frame=%v code=%v", c.FrameShown, c.CodeShown)
	}

	var grip struct{ Before, After, Painted, Box int }
	if err := json.Unmarshal([]byte(resized), &grip); err != nil {
		t.Fatalf("reading the resize (%q): %v", resized, err)
	}
	if grip.After <= grip.Before {
		t.Errorf("the box was dragged to %dpx and the framed document's viewport stayed %dpx; the grip moves the box and not the document", grip.Box, grip.After)
	}
	// And the rendering fills what was dragged, rather than leaving
	// empty box under it.
	if grip.Painted < grip.Box-4 {
		t.Errorf("after the drag the box is %dpx and the rendering in it is %dpx; %dpx of it is empty", grip.Box, grip.Painted, grip.Box-grip.Painted)
	}

	if deadLinks != 0 {
		t.Errorf("%d links inside the preview documents still point at a route this site does not serve", deadLinks)
	}

	// The scheme. A frame does NOT inherit the reader's choice — a
	// colour scheme is not propagated into an embedded document that
	// declares one, and every preview links a theme that does — so
	// gallery.js writes the attribute on each frame's own <html>. Read
	// as the used colour scheme AND the painted background, because
	// the first without the second passed once on a document that had
	// resolved dark and painted nothing.
	if !strings.HasPrefix(dk.Scheme, "dark ") {
		t.Errorf("after choosing Dark the framed document is %q; the previews are not following the gallery", dk.Scheme)
	}
	if dk.Scheme == d.Scheme {
		t.Errorf("the framed document is %q both before and after Dark was chosen — the reading proves nothing", d.Scheme)
	}

	// And the whole widget with scripts off.
	if off.FrameShown || !off.CodeShown {
		t.Errorf("with scripts disabled the Code tab shows frame=%v code=%v — the tabs need JavaScript", off.FrameShown, off.CodeShown)
	}
}

// The heights in previewHeights are measurements, and this is where
// they were measured. Every frame on the page is asked what its
// document actually needs and held to the box the renderer gave it: a
// frame smaller than its content is a sample a reader has to scroll
// inside a box the size of a paragraph, which is the failure the table
// exists to prevent.
//
// Only the upper bound is a failure. Several frames are deliberately
// taller than their content at rest — a field whose script opens a
// panel, a menu that opens downwards, the modal — so the slack is
// logged and not gated.
func TestPreviewFrameHeightsFitTheirContent(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 420*time.Second)
	defer cancel()

	const measure = `(() => {
		  const out = {};
		  for (const f of document.querySelectorAll(".ds-view__frame")) {
		    const section = f.closest("article, section");
		    const id = section ? section.id : "?";
		    const d = f.contentDocument;
		    const need = d ? Math.ceil(Math.max(d.body.getBoundingClientRect().height, d.body.scrollHeight)) : -1;
		    const box = Math.round(parseFloat(getComputedStyle(f).height));
		    const was = out[id];
		    if (!was || need > was[0]) out[id] = [need, box];
		  }
		  return JSON.stringify(out);
		})()`

	// Every page that frames anything, because previewHeights is one
	// table over the whole tree: the partial samples are on
	// components.html, the class idioms on primitives.html and the
	// three shells on shells.html, and a height measured on one of them
	// says nothing about the other two.
	// The floor per page is derived, not guessed: every partial ui
	// defines has a section on the components page, every idiom
	// ui.Styleguide ships has one on the primitives page, and every
	// shell has one on the shells page. A page whose frames did not
	// load measures fewer, which is the failure this catches.
	for _, tc := range []struct {
		kind  string
		least int
	}{
		{"components", len(definedPartials(t))},
		{"primitives", len(ui.Styleguide())},
		{"shells", len(ui.LayoutNames())},
	} {
		kind := tc.kind
		var desktop, mobile string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1500, 1000),
			chromedp.Navigate(rig.Origin+pageHref(RootTheme(), "en", fileOf(kind))),
			chromedp.WaitVisible(`.ds-view__frame`, chromedp.ByQuery),
			chromedp.Evaluate(`(() => {
			  document.querySelectorAll(".ds-view__frame").forEach(f => { f.loading = "eager"; });
			  return "ok";
			})()`, new(string)),
			chromedp.Sleep(8*time.Second),
			chromedp.Evaluate(measure, &desktop),
			// And the same page on the other tab. The mobile height is
			// one factor off the desktop one rather than a second table,
			// so this is where that factor is checked.
			chromedp.Evaluate(`document.querySelectorAll(".ds-view__tab--m input").forEach(i => i.click()); "ok"`, new(string)),
			chromedp.Sleep(4*time.Second),
			chromedp.Evaluate(measure, &mobile),
		); err != nil {
			t.Fatalf("measuring the %s frames: %v", kind, err)
		}
		for _, tab := range []struct{ name, raw string }{{"Desktop", desktop}, {"Mobile", mobile}} {
			if n := measured(t, kind+" "+tab.name, tab.raw); n < tc.least {
				t.Errorf("%s %s: measured %d sections, want at least %d — the frames did not all load", kind, tab.name, n, tc.least)
			}
		}
	}
}

// measured holds one tab's readings: section id → [what the document
// needs, what the frame gives it]. It returns how many sections it saw,
// so the caller can insist the three pages between them measured the
// whole gallery.
func measured(t *testing.T, tab, raw string) int {
	t.Helper()
	var got map[string][2]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: reading the measurements: %v", tab, err)
	}
	if len(got) == 0 {
		t.Fatalf("%s: measured no sections at all — the frames did not load", tab)
	}
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, id := range names {
		name := tab + " " + id
		need, box := got[id][0], got[id][1]
		if need < 0 {
			t.Errorf("%s: the frame has no document in it", name)
			continue
		}
		// The 48px is the sidebar shell, and it is a property of the
		// shell rather than of the number: its rail is
		// block-size: 100dvh, so the page is always exactly as tall as
		// whatever window it is in plus the margin under its content.
		// No frame height can fit it, and chasing one is a loop —
		// raising the box raises the requirement by the same amount.
		// Everything else fits with room to spare.
		if need > box+48 {
			t.Errorf("%s: its document needs %dpx and its frame is %dpx; raise previewHeights[%q] to at least %d", name, need, box, name, need+20)
		}
		if box > need*4 && box-need > 120 {
			t.Logf("%s: %dpx of frame for %dpx of document — deliberate headroom, or a number to bring down", name, box, need)
		}
	}
	return len(got)
}
