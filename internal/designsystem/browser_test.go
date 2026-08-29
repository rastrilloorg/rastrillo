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
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/carlosframework/rastrillo/harness"
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
	Links   int      `json:"links"`
	Shown   []string `json:"shown"`
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
  const links = Array.from(document.querySelectorAll("#ds-nav a"));
  const secs = Array.from(document.querySelectorAll("#ds-nav details"));
  const box = document.querySelector("[data-ds-filter]");
  const empty = document.querySelector("[data-ds-filter-empty]");
  return {
    links: links.length,
    shown: links.filter(seen).map(a => a.getAttribute("href")),
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

	url := rig.Origin + indexHref(RootTheme(), "en")

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
		  const links = Array.from(document.querySelectorAll("#ds-nav a"));
		  const state = {
		    links: links.length,
		    shown: links.filter(seen).map(a => a.getAttribute("href")),
		    folded: 0, open: [], empty: false, focused: "", value: "",
		    boxSeen: seen(document.querySelector(".ds-search")),
		  };
		  document.documentElement.setAttribute("data-rst-js", "on");
		  return state;
		})()`, &scriptless),
		chromedp.Evaluate(`document.querySelectorAll("#ds-nav a").length`, &navWithNoJS),
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
		chromedp.Navigate(rig.Origin+indexHref(RootTheme(), "es")), at("navigated-es"),
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
	for i, open := range fresh.Open {
		if !open {
			t.Errorf("sidebar section %d arrives collapsed; the rail is a table of contents first", i)
		}
	}

	if scriptless.BoxSeen {
		t.Error("the filter box is still visible without data-rst-js — with scripts off it would look like a control that works")
	}
	if len(scriptless.Shown) != fresh.Links || navWithNoJS != fresh.Links {
		t.Errorf("with the marker off the rail shows %d of %d links; the nav is complete with or without a script", len(scriptless.Shown), fresh.Links)
	}

	if !showing(badge, "#partial-badge") {
		t.Error(`typing "badge" hid the badge partial`)
	}
	if showing(badge, "#partial-meter") {
		t.Error(`typing "badge" left the meter partial on screen`)
	}
	if showing(badge, "#tokens-accent") {
		t.Error(`typing "badge" left a token group on screen`)
	}
	if badge.Folded == 0 {
		t.Error(`typing "badge" folded no section away; Tokens, Shells and Demos have nothing in them that matches`)
	}
	if badge.Empty {
		t.Error(`typing "badge" said there were no matches`)
	}

	if !showing(family, "#family-form") {
		t.Error(`typing "form" hid the Form family's own heading`)
	}
	if !showing(family, "#partial-field-check") {
		t.Error(`typing "form" hid field-check, which is in the family that matched`)
	}
	// Display is not the family to check here: form-error lives in it,
	// so "form" legitimately keeps it. List screen has nothing in it
	// that matches, which is the case worth asserting.
	if showing(family, "#family-list-screen") {
		t.Error(`typing "form" left the List screen family's heading over an empty gap`)
	}
	if !showing(family, "#partial-form-error") {
		t.Error(`typing "form" hid form-error, which matches on its own name`)
	}

	if !showing(titled, "#shell-column") || !showing(titled, "#shell-topbar") || !showing(titled, "#shell-sidebar") {
		t.Errorf(`typing a section's own name ("shells") did not reveal the section: %v`, titled.Shown)
	}
	if len(titled.Shown) != 3 {
		t.Errorf(`typing "shells" left %d entries on screen, want the section's 3: %v`, len(titled.Shown), titled.Shown)
	}

	if len(junk.Shown) != 0 {
		t.Errorf("junk left %d entries on screen: %v", len(junk.Shown), junk.Shown)
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
	if !showing(accented, "#family-display") {
		t.Errorf(`on the Spanish page, "presentacion" did not find "Presentación": %v`, accented.Shown)
	}
	if !showing(accented, "#partial-badge") {
		t.Error(`"presentacion" found the family and not the partials under it`)
	}
	if showing(accented, "#family-form") {
		t.Error(`"presentacion" left another family's heading on screen`)
	}
	if !showing(unaccented, "#tokens-surfaces-and-lines") {
		t.Errorf(`on the Spanish page, "lineas" did not find "Superficies y líneas": %v`, unaccented.Shown)
	}

	if len(restored.Open) < 2 || restored.Open[1] {
		t.Error("a section the reader collapsed by hand was left open by a search that has been cleared")
	}
	if len(restored.Shown) != restored.Links {
		t.Errorf("after the search was cleared, %d of %d entries are still hidden", restored.Links-len(restored.Shown), restored.Links)
	}
}
