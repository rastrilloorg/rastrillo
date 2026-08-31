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
	"fmt"
	"math"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// treeHandler serves the rendered tree at the mount path the pages
// expect. Every URL in a page is absolute under /design-system/, so
// serving it anywhere else would 404 the stylesheet and the script and
// the drive would be measuring an unstyled, unscripted page.
//
// It serves Render's output rather than a directory, which is now the
// only thing there is to serve: the tree is not committed, and the site
// generates it at build time. That is stricter than reading a copy off
// disk as well as simpler — the accessibility gate in a11y_test.go, which
// used to scan the committed files, now scans the exact bytes dsgen
// would write.
func treeHandler(t *testing.T) http.Handler {
	t.Helper()
	files, err := Render(mountPath)
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

	url := rig.Origin + indexHref(mountPath, RootTheme(), "en")

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
// rule in gallery.css outranking the shell's display:block. A probe reading
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
//
// discloseIndex is where one page kind's section sits among the rail's
// <details> elements, which is not its position in pageKinds(): a page
// kind with nothing anchored on it yet draws as a plain <a> rather than
// a disclosure and takes no slot here.
//
// Derived rather than written down. This drive used to say 1 for the
// Components section, and 1 stopped being Components the day a page
// kind landed ahead of it — a stale index in a test is a test that has
// quietly changed what it asserts. It panics on a kind that is not in
// the table for the same reason fileOf does: the caller read the name
// out of the table.
func discloseIndex(kind string) int {
	at := 0
	for _, pk := range pageKinds() {
		if pk.Kind == kind {
			return at
		}
		if pk.Nav != nil {
			at++
		}
	}
	panic("designsystem: no page kind " + kind)
}

func TestTheSidebarFilterDrivesTheWholeJourney(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// A component page: the rail is the same on every page of the
	// gallery, and this is one whose own section is a long one, so a
	// filter that folded the current section away would show up here.
	url := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("form"))
	// A section's own overview link: the page address with no fragment
	// on it, which is what the rail draws at the head of every section
	// that discloses anything.
	section := func(locale, kind string) string {
		return pageHref(mountPath, RootTheme(), locale, fileOf(kind))
	}
	// The rail's entries are absolute page addresses with a fragment on
	// the end, the current page's included, so every expectation below
	// names the page it links as well as the fragment.
	on := func(locale, kind, id string) string {
		return anchorHrefIn(mountPath, RootTheme(), locale, fileOf(kind), id)
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

		// A section's name stands for the run under it, while the rest
		// of the rail keeps only what matches on its own: "form" is
		// the Form page's name AND part of form-error's, which is in
		// Display. "shells" below cannot show that, because nothing
		// outside the Shells section matches it.
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
		chromedp.Evaluate(fmt.Sprintf(`document.querySelectorAll("#ds-nav details")[%d].open = false`, discloseIndex("display")), nil),
		typing("badge"), at("typed-into-collapsed"),
		chromedp.KeyEvent(kb.Escape), at("escaped-again"),
		chromedp.Evaluate(railProbe, &restored),

		// The Spanish page, for the half of the fold a monolingual
		// journey can never reach. "Presentación" is a section heading
		// there and "Superficies y líneas" a token group; a reader
		// typing them off an English keyboard types neither accent, and
		// with the fold replaced by a plain toLowerCase both queries
		// find nothing at all.
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "es", fileOf("display"))), at("navigated-es"),
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
	if at := discloseIndex("form"); len(fresh.Open) > at && !fresh.Open[at] {
		t.Errorf("the Form section (disclosure %d of %d) is not the one open on the form page", at, len(fresh.Open))
	}

	if scriptless.BoxSeen {
		t.Error("the filter box is still visible without data-rst-js — with scripts off it would look like a control that works")
	}
	if len(scriptless.Shown) != fresh.Links || navWithNoJS != fresh.Links {
		t.Errorf("with the marker off the rail shows %d of %d links; the nav is complete with or without a script", len(scriptless.Shown), fresh.Links)
	}

	if !showing(badge, on("en", "display", "partial-badge")) {
		t.Error(`typing "badge" hid the badge partial`)
	}
	if showing(badge, on("en", "display", "partial-meter")) {
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

	if !showing(family, section("en", "form")) {
		t.Error(`typing "form" hid the Form section's own overview link`)
	}
	if !showing(family, on("en", "form", "partial-field-check")) {
		t.Error(`typing "form" hid field-check, which is in the section that matched`)
	}
	// Display is not the section to check here: form-error lives in it,
	// so "form" legitimately keeps it. List screen has nothing in it
	// that matches, which is the case worth asserting.
	if showing(family, section("en", "list-screen")) {
		t.Error(`typing "form" left the List screen section on screen with nothing under it that matches`)
	}
	if !showing(family, on("en", "display", "partial-form-error")) {
		t.Error(`typing "form" hid form-error, which matches on its own name`)
	}

	if !showing(titled, on("en", "shells", "shell-column")) || !showing(titled, on("en", "shells", "shell-topbar")) || !showing(titled, on("en", "shells", "shell-sidebar")) {
		t.Errorf(`typing a section's own name ("shells") did not reveal the section: %v`, titled.Shown)
	}
	// The section's own entries and nothing else: one shell demo
	// anchor per layout, plus the section overview link that §12 put at
	// the head of every disclosing section. The overview link is an
	// entry like any other here — it says "Overview", so it is hidden
	// by a query that does not match it and revealed, like the rest of
	// the run, by the section's own name.
	if want := len(ui.LayoutNames()) + 1; len(titled.Shown) != want {
		t.Errorf(`typing "shells" left %d entries on screen, want the section's %d: %v`, len(titled.Shown), want, titled.Shown)
	}
	if !showing(titled, pageHref(mountPath, "day", "en", fileOf("shells"))) {
		t.Errorf(`typing "shells" hid the section's own overview link: %v`, titled.Shown)
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
	if !showing(accented, section("es", "display")) {
		t.Errorf(`on the Spanish page, "presentacion" did not find "Presentación": %v`, accented.Shown)
	}
	if !showing(accented, on("es", "display", "partial-badge")) {
		t.Error(`"presentacion" found the section and not the partials under it`)
	}
	if showing(accented, section("es", "form")) {
		t.Error(`"presentacion" left another section on screen`)
	}
	if !showing(unaccented, on("es", "tokens", "tokens-surfaces-and-lines")) {
		t.Errorf(`on the Spanish page, "lineas" did not find "Superficies y líneas": %v`, unaccented.Shown)
	}

	if at := discloseIndex("display"); len(restored.Open) <= at || restored.Open[at] {
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
	// The box's CONTENT width — Painted less its 1px border on each
	// side. The frame lives inside that, so it is the number a fluid
	// frame must equal; comparing against Painted instead makes the
	// border look like a 2px layout error.
	Content float64
	// What the framed document itself reports: the width its <html>
	// laid out to, and whether its body really has the sample in it.
	Inner float64
	Body  string
	// The scrollbar gutter the framed document reserved. tokens.css
	// sets scrollbar-gutter: stable on html, so Inner is the frame's
	// own width LESS this — which is the fix working, not the frame
	// laying out short, and the assertions below add the two back
	// together rather than settling for a tolerance.
	Gutter float64
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

	// The Display page: since the split by family it is the one the
	// callout sample lives on.
	url := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("display"))

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
		    Content: box.clientWidth,
		    Inner: d ? d.documentElement.getBoundingClientRect().width : -1,
		    Gutter: d ? d.defaultView.innerWidth - d.documentElement.getBoundingClientRect().width : 0,
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
		    Body: "", Virtual: 0, Painted: 0, Inner: 0, Gutter: 0, Scheme: ""
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

	// Desktop, for a component: the frame is the box and the box is the
	// column, at 1:1. This is discussion #7 — the callout's 12.5px type
	// used to be painted at 9px here, because the frame was laid out at
	// a virtual 1200px and scaled by 958/1200 to fit. A shell is still
	// scaled that way and TestAShellPreviewIsStillScaledIntoItsColumn
	// holds it, which is the half of this claim that is easy to lose:
	// "not scaled" is the right answer for a component and the wrong
	// one for a page frame, so neither test means much without the
	// other.
	//
	// Frame against box and document against frame are separate
	// readings because they fail differently. The first going wrong is
	// the fluid rule not applying at all; the second is a frame laying
	// out short for a reason of its own, which a single end-to-end
	// comparison would report as the same thing.
	if d.Virtual != d.Content {
		t.Errorf("the component frame lays out at %gpx inside a box %gpx wide inside its border — a fluid preview is exactly its column, so these are one number", d.Virtual, d.Content)
	}
	if d.Painted >= 1280 || d.Painted < 200 {
		t.Errorf("the component frame is painted %gpx wide in a 1280px window, which is not this page's column", d.Painted)
	}
	// Inner plus the gutter, not Inner alone: tokens.css reserves the
	// scrollbar's width inside every framed document too (§6-v2.1b.4),
	// so the document lays out 15px short on a platform with classic
	// scrollbars and full width on one with overlay scrollbars. Adding
	// the two back together is exact on both, where a tolerance would
	// have quietly accepted a frame laying out short for some other
	// reason.
	if d.Inner+d.Gutter != d.Virtual {
		t.Errorf("the framed document laid out at %gpx with a %gpx scrollbar gutter, %gpx in all, in a %gpx frame", d.Inner, d.Gutter, d.Inner+d.Gutter, d.Virtual)
	}
	// The column's width, logged rather than written down anywhere: it
	// moves whenever [rst-page]'s cap moves, and a number in a comment
	// would be a number nobody re-measures. This is where to read it
	// after changing the column.
	t.Logf("component preview: a %gpx frame painted %gpx wide in a 1280px window — scale %.3f", d.Virtual, d.Painted, d.Painted/d.Virtual)
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
	if m.Inner+m.Gutter != 390 {
		t.Errorf("the framed document laid out at %gpx with a %gpx scrollbar gutter on the mobile tab, %gpx in all, want 390", m.Inner, m.Gutter, m.Inner+m.Gutter)
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

// TestAShellPreviewIsStillScaledIntoItsColumn is the other half of the
// drive above, and it exists because discussion #7 is easy to overshoot.
//
// The complaint was that scaling an INPUT down to 0.72 made its label
// tiny for no gain, and the fix was to stop scaling components. A shell
// is the case that argument does not reach: the question it answers is
// where a rail sits beside its content, which is a question about a
// window, so a window shrunk to fit the column is the honest rendering
// and the column's own width would be a different shell rather than the
// same one smaller. Delete the modifier class from the template and
// every preview in the gallery goes fluid; nothing in the component
// drives would notice, and this is what does.
func TestAShellPreviewIsStillScaledIntoItsColumn(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	// The column shell: the plain one, on the page in every language.
	const widget = `#shell-column .ds-view`
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("shells"))),
		chromedp.WaitVisible(`#shell-column .ds-view__frame`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
		  document.querySelectorAll(".ds-view__frame").forEach(f => { f.loading = "eager"; });
		  return "ok";
		})()`, new(string)),
		chromedp.Sleep(4*time.Second),
		chromedp.Evaluate(`(() => {
		  const v = document.querySelector(`+"`"+widget+"`"+`);
		  const f = v.querySelector(".ds-view__frame");
		  const box = v.querySelector(".ds-view__box");
		  return JSON.stringify({
		    Fluid: v.classList.contains("ds-view--fluid"),
		    Virtual: parseFloat(getComputedStyle(f).width),
		    Painted: box.getBoundingClientRect().width,
		    Width: getComputedStyle(box).getPropertyValue("--ds-w").trim(),
		    Scale: getComputedStyle(f).transform
		  });
		})()`, &raw),
	); err != nil {
		t.Fatalf("measuring the column shell's preview: %v", err)
	}

	var got struct {
		Fluid            bool
		Virtual, Painted float64
		Width, Scale     string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading the shell preview (%q): %v", raw, err)
	}

	if got.Fluid {
		t.Fatal("the column shell's preview carries ds-view--fluid — a shell is a page frame, and the readings below would be measuring a component")
	}
	if got.Width != "1200px" || got.Virtual != 1200 {
		t.Errorf("the shell frame lays out at %gpx with --ds-w: %s, want a virtual 1200px — a shell shown at the reader's own width is a different shell, not this one smaller", got.Virtual, got.Width)
	}
	if got.Painted >= 1200 || got.Painted < 200 {
		t.Errorf("the shell frame is painted %gpx wide in a 1280px window; a 1200px page in a narrower column has to be scaled DOWN into it", got.Painted)
	}
	// Read off the transform and not inferred from the two widths
	// agreeing: a frame that had lost its transform and been given the
	// box's width instead would paint at exactly the same number.
	if k := scaleOf(t, "the column shell", previewBox{ID: "shell-column", Scale: got.Scale}); k <= 0 || k >= 1 {
		t.Errorf("the shell frame's transform is %q (scale %v); the scaling is what makes a 1200px page fit a %gpx column", got.Scale, k, got.Painted)
	}
	t.Logf("shell preview: a %gpx virtual page painted %gpx wide in a 1280px window — scale %.3f", got.Virtual, got.Painted, got.Painted/got.Virtual)
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
	// table over the whole tree: the partial samples are spread over
	// the five component pages, the markup idioms are on primitives.html
	// and the shells on shells.html, and a height measured on one
	// of them says nothing about the others.
	// The floor per page is derived, not guessed: every partial in a
	// family has a section on that family's page, every idiom
	// ui.Styleguide ships has one on the primitives page, and every
	// shell has one on the shells page. The component rows are read off
	// componentPages(), so a family added to samples.go is measured the
	// day its row lands.
	//
	// This is also the only place "documented" and "has an example to
	// look at" are joined, so the message it fails with has to name
	// that first. A component documented with an empty section reads
	// exactly like a frame that did not load from here, and the first
	// of those is a rendering bug in the tree while the second is a
	// flake in this job; a message that only offers the second sends
	// the reader looking in the wrong place.
	rows := []struct {
		kind  string
		least int
		owed  string
	}{
		{"overview", 1, "the demo application is framed here"},
		{"primitives", len(ui.Styleguide()), "every sample ui.Styleguide() ships has a section here"},
		{"shells", len(ui.LayoutNames()), "every shell ui.LayoutNames() reports has a section here"},
	}
	for _, fam := range families() {
		rows = append(rows, struct {
			kind  string
			least int
			owed  string
		}{fam.Key, len(fam.Partials), "every partial samples.go puts in this family has a section here"})
	}
	for _, tc := range rows {
		kind := tc.kind
		var desktop, mobile string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1500, 1000),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf(kind))),
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
				t.Errorf("%s %s: %d sections have a rendered example, want at least %d — %s. "+
					"Either something ui ships is documented with nothing to look at (a partial with no sample "+
					"state, an idiom with no styleguide entry, a shell with no demo), or the frames on this "+
					"page did not all load", kind, tab.name, n, tc.least, tc.owed)
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
		t.Fatalf("%s: no section on this page has a rendered example at all — either the page rendered none, or no frame on it loaded", tab)
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

// ── The demo application ─────────────────────────────────────────────

// The demo application is the page the Overview frames before it says a
// word about a token, and its whole claim is that it is an application
// with no JavaScript in it: three screens, three addresses, and the
// switching done by CSS reading the address bar.
//
// That claim cannot be checked in Go — a static reading of demo.html
// sees three sections and a stylesheet, and every one of them is
// present whether the rules work or not. So it is driven, with script
// execution DISABLED in the engine, which is the strongest form of the
// claim: not "the framework's scripts are not needed", but "no script
// runs at all and the app still works".
//
// The journey is the one a reader takes. Land on it, and the dashboard
// is what you get. Follow the rail to the list. Follow a row into the
// record. Follow the back link out again. At every stop, exactly one
// screen is on the page — a second visible view would be two <h1>s and
// two page headers stacked, which reads as a broken build rather than
// as an app.
func TestTheDemoApplicationSwitchesViewsWithNoScript(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	// Which of the three views the engine is actually painting, read
	// off computed style rather than off the markup: display: none is
	// the whole mechanism, so it is the thing to ask about.
	const shown = `(() => {
	  const out = [];
	  for (const v of document.querySelectorAll(".app-view")) {
	    if (getComputedStyle(v).display !== "none") out.push(v.id);
	  }
	  return out.join(",");
	})()`

	url := rig.Origin + demoHref(mountPath, RootTheme(), "en")
	var landed, ranScript, list, detail, back, views string
	if err := chromedp.Run(ctx,
		emulation.SetScriptExecutionDisabled(true),
		// Wide enough that the sidebar shell shows its rail: below
		// 800px it folds behind a disclosure, and the rail's links are
		// not clickable until a reader opens it. The app's navigation
		// is what this drive follows, so it drives the width where the
		// navigation is on screen.
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.querySelectorAll(".app-view").length + ""`, &views),
		// gallery.js is the only script the page loads, and with
		// execution disabled it has not run — so nothing below can be
		// a script doing the work.
		chromedp.Evaluate(`document.documentElement.getAttribute("data-rst-js") ?? "(none)"`, &ranScript),
		chromedp.Evaluate(shown, &landed),
		chromedp.Click(`[rst-shell-nav] a[href="#view-requests"]`, chromedp.ByQuery),
		chromedp.Evaluate(shown, &list),
		chromedp.Click(`#view-requests [rst-lrow] a[href="#view-request"]`, chromedp.ByQuery),
		chromedp.Evaluate(shown, &detail),
		chromedp.Click(`#view-request [rst-back-nav] a`, chromedp.ByQuery),
		chromedp.Evaluate(shown, &back),
	); err != nil {
		t.Fatalf("driving the demo application: %v", err)
	}

	if views != "3" {
		t.Fatalf("the demo application has %s views, want 3 (a dashboard, a list and a record)", views)
	}
	if ranScript != "(none)" {
		t.Fatalf("gallery.js ran with script execution disabled (data-rst-js=%q) — this drive proves nothing", ranScript)
	}
	for _, step := range []struct{ where, got, want string }{
		{"landing on it with no fragment", landed, "view-dashboard"},
		{"following the rail to the list", list, "view-requests"},
		{"following a row into the record", detail, "view-request"},
		{"following the back link out again", back, "view-requests"},
	} {
		if step.got != step.want {
			t.Errorf("%s: the visible views are %q, want exactly %q", step.where, step.got, step.want)
		}
	}
}

// ── §10's placement clause ───────────────────────────────────────────

// "Previous at the inline start, next at the inline end", and "the
// missing side leaves its space rather than shifting the other across".
// Both sentences of the ruling are implemented by two CSS declarations
// and by nothing else:
//
//	.ds-updown__prev { grid-column: 1; justify-self: start; }
//	.ds-updown__next { grid-column: 2; justify-self: end; text-align: end; }
//
// TestEveryPageEndsWithItsPlaceInTheSequence reads the markup — the
// classes, the hrefs, the labels, the order, exactly one strip, last in
// the column — and markup is exactly what does not change when those
// declarations go. A reviewer deleted them and swapped them; both
// mutations passed the whole suite, browser tag included, because the
// only gate that noticed was the tree's freshness check, and the answer
// to that one is `go generate`.
//
// The deletion is the one worth having a drive for. With both links
// present, auto-placement puts them in columns 1 and 2 anyway, so the
// page looks right and the bug is invisible. It shows up on the ENDS of
// the sequence, where there is one item and two columns: the Overview's
// lone "Next: Tokens" auto-places into column 1 and lands exactly where
// Previous would have been — the shift the ruling forbids, in the
// sentence forbidding it.
//
// So the drive walks both ends and a middle page, and does it in both
// writing directions, because "inline start" is the whole claim: a grid
// column is logical, and column 1 is on the right in Arabic. Everything
// is measured against the strip's own box rather than the viewport, so
// the numbers do not move with the column width.
func TestThePrevNextPairSitsAtTheEndsOfItsRow(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
	defer cancel()

	// startGap and endGap are the distances from the strip's own inline
	// edges, so both directions are one set of numbers.
	const measure = `(() => {
	  const box = document.querySelector(".ds-updown");
	  if (!box) return JSON.stringify({error: "no prev/next strip on this page"});
	  const dir = getComputedStyle(document.documentElement).direction;
	  const b = box.getBoundingClientRect();
	  const read = (sel) => {
	    const el = box.querySelector(sel);
	    if (!el) return null;
	    const r = el.getBoundingClientRect();
	    return {
	      startGap: Math.round(dir === "rtl" ? b.right - r.right : r.left - b.left),
	      endGap:   Math.round(dir === "rtl" ? r.left - b.left   : b.right - r.right),
	      size:     Math.round(r.width),
	    };
	  };
	  return JSON.stringify({dir: dir, width: Math.round(b.width),
	    prev: read(".ds-updown__prev"), next: read(".ds-updown__next")});
	})()`

	type edge struct {
		StartGap int `json:"startGap"`
		EndGap   int `json:"endGap"`
		Size     int `json:"size"`
	}
	type strip struct {
		Error string `json:"error"`
		Dir   string `json:"dir"`
		Width int    `json:"width"`
		Prev  *edge  `json:"prev"`
		Next  *edge  `json:"next"`
	}

	kinds := pageKinds()
	// The two ends and one middle page, in both directions. The ends
	// are where a missing grid-column shows; the middle is where a
	// swapped one does.
	for _, locale := range []string{"en", "ar"} {
		wantDir := rastrillo.Dir(locale)
		for _, pos := range []int{0, 1, len(kinds) - 1} {
			// Positions rather than rows, so "has a previous" and "has
			// a next" come off the table rather than off a guess about
			// which three pages these are.
			pk := kinds[pos]
			name := RootTheme() + "/" + locale + "/" + pk.File
			var raw string
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(1280, 900),
				chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), locale, pk.File)),
				chromedp.WaitVisible(`.ds-updown`, chromedp.ByQuery),
				chromedp.Evaluate(measure, &raw),
			); err != nil {
				t.Fatalf("%s: measuring the prev/next strip: %v", name, err)
			}
			var got strip
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("%s: reading the measurement: %v", name, err)
			}
			if got.Error != "" {
				t.Errorf("%s: %s", name, got.Error)
				continue
			}
			if got.Dir != wantDir {
				t.Errorf("%s: the page renders dir=%q, want %q — this leg is not measuring the direction it thinks it is", name, got.Dir, wantDir)
				continue
			}
			if got.Width < 200 {
				t.Errorf("%s: the strip is %dpx wide; there is nothing to measure", name, got.Width)
				continue
			}
			half := got.Width / 2
			// Present or absent, off the table.
			if (got.Prev != nil) != (pos > 0) {
				t.Errorf("%s: previous link present=%v, want %v", name, got.Prev != nil, pos > 0)
			}
			if (got.Next != nil) != (pos < len(kinds)-1) {
				t.Errorf("%s: next link present=%v, want %v", name, got.Next != nil, pos < len(kinds)-1)
			}
			// Previous is AT the inline start and does not cross the
			// middle. Not "is somewhere in the first half": flush to the
			// edge is what justify-self: start in column 1 means, and a
			// gap there is a placement that has stopped working.
			if p := got.Prev; p != nil {
				if p.StartGap > 2 {
					t.Errorf("%s: previous starts %dpx from the strip's inline start, want 0 (%s, strip %dpx)", name, p.StartGap, got.Dir, got.Width)
				}
				if p.StartGap+p.Size > half+2 {
					t.Errorf("%s: previous runs %dpx into a %dpx strip, past its half (%d)", name, p.StartGap+p.Size, got.Width, half)
				}
			}
			// Next is AT the inline end, and its own inline start is at
			// or past the middle. On the Overview that second assertion
			// IS "the missing side leaves its space": with no Previous
			// to push it, a next that has lost its column auto-places
			// into the first one and this fails.
			if n := got.Next; n != nil {
				if n.EndGap > 2 {
					t.Errorf("%s: next ends %dpx from the strip's inline end, want 0 (%s, strip %dpx)", name, n.EndGap, got.Dir, got.Width)
				}
				if start := got.Width - n.EndGap - n.Size; start < half-2 {
					t.Errorf("%s: next begins %dpx into a %dpx strip, before its half (%d) — the missing previous has not left its space", name, start, got.Width, half)
				}
			}
			if got.Prev == nil && got.Next == nil {
				t.Errorf("%s: the strip is empty", name)
			}
		}
	}
}

// ── The preview widget on a phone ────────────────────────────────────

// minShownSample is the floor this drive is actually about, and it is
// deliberately NOT a box height.
//
// The first version of this gate asserted that the preview BOX cleared
// 64px, and it passed on a state where every box was 146px tall and
// every sample inside one was a 14px sliver in the top-left corner.
// The box is not the quantity that broke: --ds-k scales the sample,
// and a box floor moves the frame around the sliver without touching
// the sliver. So what is measured here is what a reader sees — the
// framed document's own height, times the scale actually applied to
// it, clipped to what the box can show — and 32px of that is roughly
// two lines of the sample's own body type at a legible scale. The
// smallest sample in the gallery is 56 virtual pixels tall, so at the
// scale floor below this lands at 40px and the margin is real.
const minShownSample = 32.0

// kMin is the least the frame may be scaled to, and it mirrors
// --ds-kmin in gallery.css. 12.5px is the type that dominates most
// pages of this gallery (--rst-fs-sm) and 12.5 × 0.72 = 9.0px, which
// is about where rendered text stops being read and starts being
// texture. Everything else follows from it, including stageThreshold.
const kMin = 0.72

// scrollbarGutter is what a classic scrollbar takes out of a box's
// content when the box can pan, in CSS pixels, measured in this engine
// with scrollbar-width: thin and the platform's scrollbars drawn.
//
// It is here because the harness hides scrollbars — chromedp's headless
// default, copied from Puppeteer — so every reading this drive takes is
// of a platform that charges nothing for one. That is the condition
// under which a scrollbar eating a third of a 52px box is invisible,
// and it was: measured, a classic bar took 15px of a 50.4px content box
// and clipped 5px off the sample. scrollbar-width: thin brought it to
// 10px, which the slack in previewHeights covers. The allowance is
// subtracted below wherever a box can pan, so the floor is asserted
// against the worse of the two platforms rather than the one that
// happens to be running.
const scrollbarGutter = 10.0

// previewBox is one widget's geometry as the engine has it. Tabs are
// identified by POSITION — 0 Desktop, 1 Mobile, 2 Code — and not by
// the modifier classes, so this reading says the same thing about the
// markup before the fix and after it.
type previewBox struct {
	ID      string
	Fluid   bool    // a component: the column's own width at 1:1, not a scaled 1200px page
	Hidden  bool    // the Code tab is showing, so there is no frame to measure
	Box     float64 // the painted height of .ds-view__box
	Width   string  // --ds-w as the box computes it: which rendering is on screen
	Scale   string  // the frame's computed transform
	Lit     []int   // the tabs the reader sees highlighted
	Checked []int   // the radios that are actually checked
	View    float64 // .ds-view, which is the query container
	Stage   float64 // .ds-view__stage, which is what 100cqw used to be
	Doc     float64 // the framed document's own height, in VIRTUAL px
	Inner   float64 // the box's content height: what it can show
	PanX    float64 // how much wider than the box its scrolled content is
	OverX   string  // computed overflow-x: whether a reader can reach it
}

const readBoxes = `(() => {
  const out = [];
  document.querySelectorAll(".ds-view").forEach(v => {
    const stage = v.querySelector(".ds-view__stage");
    const box   = v.querySelector(".ds-view__box");
    const frame = v.querySelector(".ds-view__frame");
    const d     = frame.contentDocument;
    const tabs  = [...v.querySelectorAll(".ds-view__tab")];
    const lit = [], checked = [];
    tabs.forEach((l, n) => {
      if (getComputedStyle(l).fontWeight === "600") lit.push(n);
      const i = l.querySelector("input");
      if (i && i.checked) checked.push(n);
    });
    const section = v.closest("article, section");
    out.push({
      ID: section && section.id ? section.id : "widget-" + out.length,
      Fluid: v.classList.contains("ds-view--fluid"),
      Hidden: getComputedStyle(stage).display === "none",
      Box: Math.round(box.getBoundingClientRect().height * 10) / 10,
      Width: getComputedStyle(box).getPropertyValue("--ds-w").trim(),
      Scale: getComputedStyle(frame).transform,
      Lit: lit,
      Checked: checked,
      View: Math.round(v.getBoundingClientRect().width * 100) / 100,
      Stage: Math.round(stage.getBoundingClientRect().width * 100) / 100,
      // The document the box is a window ON. A box that is tall and
      // full and a box that is tall and empty read the same to a
      // box-only ruler, and that was the whole of the last bug.
      Doc: d ? Math.max(d.body.getBoundingClientRect().height, d.body.scrollHeight) : -1,
      Inner: Math.round(box.clientHeight * 10) / 10,
      // scrollWidth reports the content even under overflow: hidden,
      // so how much is out there and whether anyone can get to it are
      // two readings, not one.
      PanX: Math.round((box.scrollWidth - box.clientWidth) * 10) / 10,
      OverX: getComputedStyle(box).overflowX
    });
  });
  return JSON.stringify(out);
})()`

// clickedMobile clicks every Mobile radio on the page and reports
// whether the clicks moved anything. It is the instrument's own
// control: a reading taken after a click that did not land is a
// reading of the state before it.
var clickedMobile = clickEvery("m")

// clickedDesktop is the same for the Desktop tab. The drive used to
// click one label and then report on thirty widgets, twenty-nine of
// which were still in the default state.
var clickedDesktop = clickEvery("d")

func clickEvery(mod string) string {
	return `(() => {
  let clicked = 0, moved = 0;
  document.querySelectorAll(".ds-view__tab--` + mod + ` input").forEach(i => {
    const before = i.checked;
    i.click();
    clicked++;
    if (i.checked && !before) moved++;
  });
  return JSON.stringify({clicked: clicked, moved: moved});
})()`
}

// eagerly turns every lazy frame on the page on and waits for the
// documents to arrive. Measuring what a reader SEES means reaching
// inside each srcdoc frame, and a frame that never loaded reports zero
// height — which is indistinguishable from a collapsed preview, and
// would read as this gate's own headline failure.
func eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
	  document.querySelectorAll(".ds-view__frame").forEach(f => { f.loading = "eager"; });
	  return "ok";
	})()`, new(string)), chromedp.Sleep(6*time.Second)); err != nil {
		t.Fatalf("%s: loading the framed documents: %v", where, err)
	}
}

// clickAll clicks every radio of one kind and refuses to go on unless
// the clicks moved something. A reading taken after a click that did
// not land is a reading of the state before it.
func clickAll(t *testing.T, ctx context.Context, where, script, what string) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw), chromedp.Sleep(400*time.Millisecond)); err != nil {
		t.Fatalf("%s: choosing %s: %v", where, what, err)
	}
	var got struct{ Clicked, Moved int }
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: reading the %s click (%q): %v", where, what, raw, err)
	}
	if got.Clicked == 0 || got.Moved != got.Clicked {
		t.Fatalf("%s: %d %s radios clicked and %d moved — the readings after this click are readings of the state before it", where, got.Clicked, what, got.Moved)
	}
}

// scrolls says whether a computed overflow lets a reader reach what is
// past the edge. hidden does not, and it is the one that looks like a
// scroller to scrollWidth.
func scrolls(overflow string) bool {
	return overflow == "auto" || overflow == "scroll"
}

// noSidewaysPage is the other half of letting a box pan: the overflow
// has to stay inside the box. A page that scrolls sideways on a phone
// is a worse bug than the one the panning fixes.
func noSidewaysPage(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var over float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.documentElement.scrollWidth - document.documentElement.clientWidth`, &over)); err != nil {
		t.Fatalf("%s: measuring the page's own overflow: %v", where, err)
	}
	if over > 1 {
		t.Errorf("%s: the page itself scrolls %.1fpx sideways. A preview that pans has to pan inside its own box", where, over)
	}
}

func boxes(t *testing.T, ctx context.Context, where string) []previewBox {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(readBoxes, &raw)); err != nil {
		t.Fatalf("%s: reading the preview boxes: %v", where, err)
	}
	var got []previewBox
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: decoding the preview boxes (%q): %v", where, raw, err)
	}
	if len(got) == 0 {
		t.Fatalf("%s: the page has no preview widget on it at all — this reading measures nothing", where)
	}
	return got
}

// tabNames indexes the tabs the way the reading does.
var tabNames = [...]string{"Desktop", "Mobile", "Code"}

func tabName(n int) string {
	if n < 0 || n >= len(tabNames) {
		return fmt.Sprintf("tab %d", n)
	}
	return tabNames[n]
}

// showsItsSample is the assertion this drive exists for, and it is on
// the rendering rather than on the box.
//
// What a reader can see of a sample is its own height in the frame's
// virtual pixels, times the scale the frame is actually drawn at,
// clipped to what the box can show. Every failure mode this widget has
// had lands on that one number: a 1200px page squeezed into a phone
// column (scale 0.26, so a 56px sample renders 14px tall), a box
// floored to 146px around the same 14px sliver, and a --ds-k an engine
// cannot resolve (box collapses to nothing, so zero). A box-height
// assertion catches only the third.
//
// The scale is asserted beside it, because it is the mechanism: the
// height floor holds only while the smallest sample in the gallery
// stays about 56 virtual pixels tall, and kMin is true whatever anyone
// adds to samples.go.
func showsItsSample(t *testing.T, where string, rows []previewBox) {
	t.Helper()
	say := loudly(t, where, len(rows))
	worst, worstID := math.Inf(1), ""
	for _, r := range rows {
		if r.Hidden {
			continue
		}
		if r.Doc < 0 {
			t.Fatalf("%s: %s has no reachable document in its frame; a frame that never loaded measures zero and would read exactly like the collapse this gate is looking for", where, r.ID)
		}
		if r.Doc == 0 {
			t.Fatalf("%s: %s frames a document with no height at all — the reading is of an empty frame, not of a sample", where, r.ID)
		}
		k := scaleOf(t, where, r)
		inner := r.Inner
		if r.PanX > 1 {
			inner -= scrollbarGutter
		}
		shown := math.Min(r.Doc*k, inner)
		if shown < worst {
			worst, worstID = shown, r.ID
		}
		if shown < minShownSample {
			say("%s: %s shows %.1fpx of its sample. The document is %.0f virtual px tall, the frame is scaled to %.3f, and the box can show %.1fpx (%.1fpx once a classic scrollbar has taken its gutter) — a reader sees a sliver whatever the box measures",
				where, r.ID, shown, r.Doc, k, r.Inner, inner)
		}
		if k < kMin-0.005 {
			say("%s: %s is scaled to %.3f, under the %.2f floor. At that scale this gallery's 12.5px type renders at %.1fpx",
				where, r.ID, k, kMin, 12.5*k)
		}
		// Clamping the scale means the sample can be wider than the
		// box, and the whole difference between panning and cropping
		// is whether the box is a scroller. scrollWidth answers the
		// first question and says nothing about the second, so both
		// are read.
		if r.PanX > 1 && !scrolls(r.OverX) {
			say("%s: %s has %.0fpx of sample past the right edge of its box and overflow-x is %q — that part of it is cropped and a reader has no way to reach it",
				where, r.ID, r.PanX, r.OverX)
		}
	}
	t.Logf("%s: %d widgets, least of a sample shown %.1fpx (%s)", where, len(rows), worst, worstID)
}

// agree is the assertion a CSS-only default has to earn. The rendering
// is chosen by a container query on .ds-view and the highlight by the
// same one — the same one is the point, and it is why the container
// had to move up off .ds-view__stage, which the tabs are not inside —
// and if the two ever drift a reader is looking at a 390px rendering
// with "Desktop" lit, which is a worse bug than the one that made this
// widget unreadable in the first place.
//
// Not a media query, and not by accident: the rail takes 240px out of
// the column at 800px, so the viewport is not monotone in the stage
// and a viewport rule switches renderings the wrong way round. See
// TestThePreviewDefaultIsMonotoneInStageWidth, which fails on one.
func agree(t *testing.T, where string, rows []previewBox) {
	t.Helper()
	say := loudly(t, where, len(rows))
	for _, r := range rows {
		if len(r.Lit) != 1 {
			say("%s: %s has %d tabs lit at once (%v); a reader cannot tell which rendering they are looking at", where, r.ID, len(r.Lit), r.Lit)
			continue
		}
		if len(r.Checked) > 1 {
			say("%s: %s has %d radios checked (%v) — they are not one group", where, r.ID, len(r.Checked), r.Checked)
		}
		if r.Hidden {
			if r.Lit[0] != 2 {
				say("%s: %s shows the source with %s lit", where, r.ID, tabName(r.Lit[0]))
			}
			continue
		}
		w, ok := virtualWidthFor(r)[r.Lit[0]]
		if !ok {
			say("%s: %s lights %s and still shows a frame", where, r.ID, tabName(r.Lit[0]))
			continue
		}
		if r.Width != w {
			say("%s: %s lights %s and renders at --ds-w: %s (want %s) — the lit tab and the rendering disagree", where, r.ID, tabName(r.Lit[0]), r.Width, w)
		}
		if r.Scale == "none" || r.Scale == "" {
			say("%s: %s has no transform on its frame (%q) — --ds-k did not resolve, and a box whose only child is absolutely positioned collapses to nothing when it does not", where, r.ID, r.Scale)
		}
	}
}

// openingTab is the tab a widget of each kind lights with nothing
// clicked, in a column too narrow for a 1200px page.
func openingTab(fluid bool) int {
	if fluid {
		return 0
	}
	return 1
}

// virtualWidthFor is what --ds-w must read under each lit tab, which
// is the one thing that differs between the gallery's two kinds of
// preview. A page frame is laid out at a virtual 1200px and scaled
// into the column; a component has no virtual width at all and is
// simply the column, which the stylesheet writes as --ds-w: 100% so
// this reading can tell the two apart rather than seeing a frame
// claiming 1200px it does not have. Mobile is 390px for both — a
// phone rendering is a phone rendering whatever it contains.
//
// Keyed off the widget's own class and not off what it happens to
// report, so a page frame that quietly lost its scaling reads as a
// disagreement rather than as the other rendering.
func virtualWidthFor(r previewBox) map[int]string {
	if r.Fluid {
		return map[int]string{0: "100%", 1: "390px"}
	}
	return map[int]string{0: "1200px", 1: "390px"}
}

// opensOn asserts what the widget shows a reader who has clicked
// nothing: the rendering, and the tab lit over it.
// The rendering is named by the tab it belongs to rather than by a
// width the caller writes out, because the width that means "Desktop"
// now depends on which kind of preview it is — 1200px for a page
// frame, the column itself for a component. Passing the tab keeps the
// caller saying what a reader sees and leaves virtualWidthFor to say
// what that costs in pixels.
func opensOn(t *testing.T, where string, rows []previewBox, tab int) {
	t.Helper()
	say := loudly(t, where, len(rows))
	for _, r := range rows {
		if len(r.Checked) != 0 {
			say("%s: %s opens with %v already checked; CSS cannot tell that from a choice the reader made, so the width can never pick the opening view", where, r.ID, r.Checked)
		}
		if width := virtualWidthFor(r)[tab]; r.Width != width {
			say("%s: %s opens rendering at --ds-w: %s, want %s — the opening view does not follow the reader's width", where, r.ID, r.Width, width)
		}
		if len(r.Lit) != 1 || r.Lit[0] != tab {
			say("%s: %s opens with %v lit, want %s", where, r.ID, r.Lit, tabName(tab))
		}
	}
}

// loudly caps a per-widget assertion at a handful of named failures
// and then counts the rest. A page carries thirty widgets and they
// fail together; ninety identical lines bury the one number a reader
// of the log needs, which is how many.
func loudly(t *testing.T, where string, of int) func(string, ...any) {
	t.Helper()
	var n int
	t.Cleanup(func() {
		if n > 4 {
			t.Errorf("%s: %d assertions failed over %d widgets (%d named above)", where, n, of, 4)
		}
	})
	return func(format string, args ...any) {
		n++
		if n <= 4 {
			t.Errorf(format, args...)
		}
	}
}

// scaleOf reads the x scale out of a computed transform.
func scaleOf(t *testing.T, where string, r previewBox) float64 {
	t.Helper()
	var a, b, c, d, e, f float64
	if n, err := fmt.Sscanf(r.Scale, "matrix(%g, %g, %g, %g, %g, %g)", &a, &b, &c, &d, &e, &f); err != nil || n != 6 {
		t.Errorf("%s: %s has transform %q, which is not a matrix — the frame is not being scaled at all", where, r.ID, r.Scale)
		return 0
	}
	return a
}

// TestThePreviewWidgetIsUsableOnAPhone is the drive Paul's report
// earned: on 2026-08-31, from his own phone, every preview on every
// component page of rastrillo.org was a 20px strip between two tab
// rows.
//
// Three claims, and each one is a separate bug class that leaves a page
// rendering perfectly:
//
//   - the box scales with --ds-k on BOTH axes, so a 1200px virtual page
//     in a 309px column is a 70px component drawn 18px tall. Nothing
//     about that is visible to a Go test or to a screenshot diff of the
//     desktop rendering;
//   - the widget opens on Desktop whatever the reader's width is, so
//     the first thing a phone gets is the one rendering that cannot be
//     legible at that size;
//   - the tab the reader sees lit and the rendering on screen are now
//     chosen by two different rules, and a CSS-only default goes wrong
//     by letting them drift apart.
//
// The controls come first and are not optional. A wide viewport whose
// answer is already known, a click that is checked for having landed,
// and a frame whose transform proves the trig branch is live in this
// engine: without them a green run here is a reading of an instrument
// nobody calibrated.
func TestThePreviewWidgetIsUsableOnAPhone(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()

	// Both kinds of preview, because they now answer differently and
	// each answer is one this drive exists to hold. The display page
	// is components — the column's own width at 1:1, which is what
	// discussion #7 asked for. The overview frames the demo
	// application, a whole page, so it is still the scaled rendering
	// and still the only one of the two that can exercise the trig
	// branch, the scale floor and the panning.
	for _, page := range []struct {
		kind  string
		fluid bool
	}{{"display", true}, {"overview", false}} {
		kind := page.kind
		url := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf(kind))

		// CONTROL 1. A wide viewport, where the answer has been known
		// since the widget shipped: the desktop rendering, the Desktop
		// tab lit over it, every box clear of the floor, and a frame
		// scaled DOWN — which is the reading that proves the
		// @supports guard still admits this engine. If any of that is
		// wrong, the narrow numbers below are measuring something else.
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(url),
			chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("%s at 1280px: loading: %v", kind, err)
		}
		eagerly(t, ctx, kind+" at 1280px")
		wide := boxes(t, ctx, kind+" at 1280px")
		// The control proper: two readings whose answers this widget
		// has had since it shipped, so they hold on the build before
		// the fix as well as on the one after it.
		for _, r := range wide {
			if r.Fluid != page.fluid {
				t.Fatalf("%s at 1280px: %s is fluid=%v, want %v — this page is not the kind of preview the sweep below is written for", kind, r.ID, r.Fluid, page.fluid)
			}
			if want := virtualWidthFor(r)[0]; r.Width != want || len(r.Lit) != 1 || r.Lit[0] != 0 {
				t.Fatalf("%s at 1280px: %s renders at --ds-w: %s with %v lit over a %.0fpx stage, want %s under a lit Desktop. That has been this widget's answer at a laptop width since it shipped, so the narrow readings below are of something other than this widget — unless the stage has fallen under the %.0fpx threshold, in which case this is the threshold working and the sweep needs a wider window, not a fix", kind, r.ID, r.Width, r.Lit, r.Stage, want, stageThreshold)
			}
		}
		// The scale is the calibration, and what it calibrates depends
		// on the kind. A page frame scaled DOWN is what proves the
		// @supports trig branch is live in this engine; there is no
		// other reading that can tell the live branch from the
		// fallback, since both leave --ds-k a number. A component has
		// no trig to exercise, so the control there is the opposite
		// claim, and it is worth just as much: exactly 1, because a
		// component scaled at all is the bug this page was changed to
		// fix and a drifting --ds-k would reintroduce it silently.
		if k := scaleOf(t, kind+" at 1280px", wide[0]); page.fluid && math.Abs(k-1) > 0.005 {
			t.Fatalf("%s at 1280px: the first frame is scaled by %v, want exactly 1 — a component preview is the column's own pixels, and anything else is the scaling this page had removed", kind, k)
		} else if !page.fluid && (k <= 0 || k >= 1) {
			t.Fatalf("%s at 1280px: the first frame is scaled by %v; a 1200px page in a column narrower than that has to be scaled DOWN, so the @supports guard is not admitting this engine and every scale reading below is of the fallback branch", kind, k)
		}
		// And then the claims, at a width where they were never in
		// doubt.
		agree(t, kind+" at 1280px", wide)
		showsItsSample(t, kind+" at 1280px", wide)

		// The phone, opening on nothing clicked.
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(390, 844),
			chromedp.Navigate(url),
			chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("%s at 390px: loading: %v", kind, err)
		}
		eagerly(t, ctx, kind+" at 390px")
		phone := boxes(t, ctx, kind+" at 390px")
		// Which tab a phone opens on is the one thing the two kinds
		// disagree about, and the disagreement is the point. A page
		// frame falls back to the 390px rendering because its desktop
		// one would be a 0.26 sliver in this column. A component has
		// no such cliff — its desktop rendering IS this column — so it
		// opens on Desktop at every width, and the 54rem auto-switch
		// is excluded from it rather than retuned.
		opensOn(t, kind+" at 390px, opened", phone, openingTab(page.fluid))
		agree(t, kind+" at 390px, opened", phone)
		showsItsSample(t, kind+" at 390px, opened", phone)
		noSidewaysPage(t, ctx, kind+" at 390px, opened")

		// And at 320px, which is not a round number chosen for
		// symmetry: it is the reflow width tokens.css and gallery.css
		// both commit to in writing, and until this leg existed
		// nothing in the suite — the WCAG scan included — drove under
		// 390px. The bug it was added for was invisible for exactly
		// that reason: the MOBILE rendering clamps at --ds-kmin too,
		// so under 281px of stage the opening view overflowed its box
		// by 42px with nowhere to go. No reload, because the frames
		// are already loaded and the queries re-evaluate on resize.
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(320, 844),
			chromedp.Sleep(400*time.Millisecond),
		); err != nil {
			t.Fatalf("%s at 320px: resizing: %v", kind, err)
		}
		narrow := boxes(t, ctx, kind+" at 320px")
		opensOn(t, kind+" at 320px, opened", narrow, openingTab(page.fluid))
		agree(t, kind+" at 320px, opened", narrow)
		showsItsSample(t, kind+" at 320px, opened", narrow)
		noSidewaysPage(t, ctx, kind+" at 320px, opened")
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(390, 844),
			chromedp.Sleep(400*time.Millisecond),
		); err != nil {
			t.Fatalf("%s: returning to 390px: %v", kind, err)
		}

		// An explicit Desktop on a phone still works, and what it
		// gives a reader is the desktop layout at a scale they can
		// read, panning inside its own box — not the same sliver in a
		// taller frame. Every widget on the page, because the drive
		// used to click one label and then report on thirty.
		clickAll(t, ctx, kind+" at 390px", clickedDesktop, "Desktop")
		chosen := boxes(t, ctx, kind+" at 390px, Desktop chosen")
		for _, r := range chosen {
			if len(r.Checked) != 1 || r.Checked[0] != 0 {
				t.Fatalf("%s at 390px: %s has %v checked after every Desktop radio was clicked — the readings after it are readings of the state before it", kind, r.ID, r.Checked)
			}
			if want := virtualWidthFor(r)[0]; r.Width != want {
				t.Errorf("%s at 390px: %s was told Desktop and renders at --ds-w: %s, want %s — an explicit choice has to beat the width", kind, r.ID, r.Width, want)
			}
		}
		agree(t, kind+" at 390px, Desktop chosen", chosen)
		showsItsSample(t, kind+" at 390px, Desktop chosen", chosen)
		// And it pans rather than crops: at 390px the clamped scale
		// puts 864px of page in a 309px box, so there has to be
		// somewhere for the rest of it to go.
		//
		// Only for a page frame. A component's Desktop rendering is
		// the column itself, so there is nothing past the edge to
		// reach and a box that pans would mean the frame had escaped
		// its box — which is asserted as its own claim below rather
		// than left as an absence.
		if page.fluid {
			for _, r := range chosen {
				if r.PanX > 1 {
					t.Errorf("%s at 390px with Desktop chosen: %s has %.0fpx of sample past the right edge of a box it is supposed to exactly fill — a component preview is its column, so anything to pan means the frame is not taking its width from the box", kind, r.ID, r.PanX)
				}
			}
		} else {
			panned := 0
			for _, r := range chosen {
				if r.PanX > 1 && scrolls(r.OverX) {
					panned++
				}
			}
			if panned != len(chosen) {
				t.Errorf("%s at 390px with Desktop chosen: %d of %d boxes both overflow and can be scrolled. --ds-k is clamped at %.2f, so a 1200px page is %.0fpx wide inside a box the column's width; if the box is not a scroller the right-hand half of every sample is cropped and unreachable, and overflow: hidden reports the same scrollWidth as a scroller does", kind, panned, len(chosen), kMin, 1200*kMin)
			}
		}
		noSidewaysPage(t, ctx, kind+" at 390px, Desktop chosen")

		// CONTROL 2. Mobile, clicked, and checked for having moved.
		var moved string
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(clickedMobile, &moved),
			chromedp.Sleep(400*time.Millisecond),
		); err != nil {
			t.Fatalf("%s at 390px: choosing Mobile: %v", kind, err)
		}
		var click struct{ Clicked, Moved int }
		if err := json.Unmarshal([]byte(moved), &click); err != nil {
			t.Fatalf("%s: reading the Mobile click (%q): %v", kind, moved, err)
		}
		if click.Clicked == 0 || click.Moved != click.Clicked {
			t.Fatalf("%s at 390px: %d Mobile radios clicked and %d moved — the readings after this click are readings of the state before it", kind, click.Clicked, click.Moved)
		}
		back := boxes(t, ctx, kind+" at 390px, Mobile chosen")
		agree(t, kind+" at 390px, Mobile chosen", back)
		showsItsSample(t, kind+" at 390px, Mobile chosen", back)
	}

	// CONTROL 3, and the one that matters most: the whole of the above
	// with script execution switched off at the engine. The tabs are
	// radios and :has(), the default is a container query, and none of
	// it is allowed to need JavaScript — a widget gate that only ever
	// runs with script on is not testing this widget's real path.
	noJS, cancelNoJS := chromedp.NewContext(rig.Context())
	defer cancelNoJS()
	offCtx, cancelOff := context.WithTimeout(noJS, 120*time.Second)
	defer cancelOff()
	url := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("display"))
	if err := chromedp.Run(offCtx,
		chromedp.EmulateViewport(390, 844),
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("display at 390px with scripts off: loading: %v", err)
	}
	// Turning the lazy frames on is a debugger evaluation, not a page
	// script: the widget still needs none, and the assertion below
	// proves the page's own script never ran.
	eagerly(t, offCtx, "display at 390px, scripts off")
	// Evaluate is itself script execution, so the reading below is
	// taken through the debugger with the PAGE's scripts disabled.
	// Assert that, or the leg proves nothing.
	var ran bool
	if err := chromedp.Run(offCtx, chromedp.Evaluate(
		`document.documentElement.hasAttribute("data-rst-js")`, &ran)); err != nil {
		t.Fatalf("checking the scriptless page: %v", err)
	}
	if ran {
		t.Fatal("gallery.js ran on the scriptless page — the scriptless leg proves nothing")
	}
	off := boxes(t, offCtx, "display at 390px, scripts off")
	opensOn(t, "display at 390px, scripts off", off, openingTab(true))
	agree(t, "display at 390px, scripts off", off)
	showsItsSample(t, "display at 390px, scripts off", off)

	// And the tabs still switch, with the click the browser makes out
	// of a label and a radio rather than one a script dispatches.
	//
	// Mobile, and not Desktop as this leg used to click. A component
	// page opens on Desktop now, so clicking Desktop would leave the
	// widget exactly where it already was and a broken tab would read
	// as a working one — the leg would pass with the radios inert,
	// which is the single thing it exists to rule out. Mobile is the
	// tab whose arrival is visible in --ds-w.
	if err := chromedp.Run(offCtx,
		chromedp.Click(`.ds-view__tab--m`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("display at 390px with scripts off: choosing Mobile: %v", err)
	}
	offChosen := boxes(t, offCtx, "display at 390px, scripts off, Mobile chosen")
	if len(offChosen[0].Checked) != 1 || offChosen[0].Checked[0] != 1 {
		t.Fatalf("with scripts off, clicking the Mobile label left %v checked — the tabs need JavaScript", offChosen[0].Checked)
	}
	if offChosen[0].Width != "390px" || len(offChosen[0].Lit) != 1 || offChosen[0].Lit[0] != 1 {
		t.Errorf("with scripts off, choosing Mobile left the widget rendering at --ds-w: %s with %v lit", offChosen[0].Width, offChosen[0].Lit)
	}
	// One widget, because one label was clicked — a real input event
	// on the page, which is the whole point of this leg. The other
	// twenty-nine are still on the default and are not re-measured
	// here as though they had been chosen.
	agree(t, "display at 390px, scripts off, Mobile chosen", offChosen)
	showsItsSample(t, "display at 390px, scripts off, the first widget on Mobile", offChosen[:1])
}

// ── The default is a function of the stage, not of the window ────────

// stageThreshold is the stage width, in CSS pixels, at or above which
// the widget opens on the desktop rendering. It mirrors the 54rem in
// gallery.css and is here so this drive fails when the two disagree
// rather than following the stylesheet wherever it goes.
//
// It is DERIVED from kMin rather than written again, because they are
// one decision: the threshold is the stage width at which the desktop
// rendering reaches the least scale worth reading it at. 1200 × 0.72 =
// 864px = 54rem. Two numbers that could drift apart would be two
// chances to be wrong.
//
// The equality holds at a 16px root and this drive runs at one. A
// reader who scales their root type up moves the 54rem and not the
// 0.72, which is why the stylesheet says so beside it; if this ever
// runs under a different root, the threshold assertion below is what
// will say so, and it will be telling the truth.
const stageThreshold = 1200 * kMin

// stageReading is the widget's opening state at one window width.
type stageReading struct {
	VW    int
	View  float64
	Stage float64
	DSW   float64
	Lit   int
	K     float64
	Box   float64
}

// dsw parses the "1200px" a box computes --ds-w to.
func dsw(t *testing.T, where, raw string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(strings.TrimSuffix(raw, "px"), 64)
	if err != nil {
		t.Fatalf("%s: --ds-w reads %q, which is not a length", where, raw)
	}
	return v
}

// TestThePreviewDefaultIsMonotoneInStageWidth is the drive the first
// fix earned by getting its ruler wrong.
//
// The opening view used to be picked by a media query at 800px, and
// 800px is the width this gallery's own rail arrives at: the stage
// measures 718px in a 799px window and 479px in an 800px one. So the
// widget switched to the rendering that needs the MOST room at exactly
// the width where it had the LEAST, and widening a window made the
// preview both narrower and more demanding. A viewport rule cannot
// avoid that, because the viewport is not monotone in the stage.
//
// The property asserted here is the one that cannot go wrong again:
// order every window width by the stage it produces, and the opening
// view must never go from Desktop back to Mobile as the stage gets
// wider. A container query on .ds-view satisfies it by construction; a
// media query at any threshold at all does not.
//
// The control comes first, and it is the one that makes the rest mean
// anything: this drive is only capable of catching the bug if the
// fixture really does contain a window that grows while its stage
// shrinks. So that pair is measured and required, at the exact
// boundary — 700px and 800px — that was wrong.
//
// Two more readings ride along, because moving the container from
// .ds-view__stage up to .ds-view moved what 100cqw resolves against
// and a silent change there would rescale every preview in the gallery
// at once: the two elements must be the same width, and --ds-k must
// still be the stage's width over the virtual one.
func TestThePreviewDefaultIsMonotoneInStageWidth(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
	defer cancel()

	// The shells page, not a component one. The property this drive
	// asserts is about the width-driven SWITCH between the two
	// renderings, and after discussion #7 that switch exists only for
	// a preview that has a virtual width to be switched away from: a
	// component opens on its own column at every width and is monotone
	// by having nothing to do. Driving it here would be green forever
	// and prove nothing, which is the failure this comment exists to
	// stop the next person walking into. The shells are page frames,
	// they sit in the same column under the same rail, and the 700px/
	// 800px control below is checked rather than assumed.
	url := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("shells"))
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatalf("loading the shells page: %v", err)
	}

	// Widths chosen around the rail's own line, because that is where
	// the viewport stops being monotone in the stage.
	// 1184 and 1186 straddle the threshold itself: with the rail in,
	// the stage is the window less 321px, so those two windows put it
	// at 863px and 865px. Without them the sweep's nearest pair is
	// 779px and 879px and the threshold could sit anywhere between.
	widths := []int{280, 320, 360, 390, 600, 700, 760, 799, 800, 900, 1000, 1100, 1184, 1186, 1200, 1280, 1500}
	readings := make([]stageReading, 0, len(widths))
	for _, vw := range widths {
		where := fmt.Sprintf("display at %dpx", vw)
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(int64(vw), 900),
			chromedp.Sleep(300*time.Millisecond),
		); err != nil {
			t.Fatalf("%s: resizing: %v", where, err)
		}
		rows := boxes(t, ctx, where)
		// The lit tab and the rendering agree at every one of these
		// widths, not only at the two the other drive visits.
		agree(t, where, rows)
		r := rows[0]
		if len(r.Lit) != 1 {
			t.Fatalf("%s: %v tabs lit, so there is no opening view to record", where, r.Lit)
		}
		if len(r.Checked) != 0 {
			t.Fatalf("%s: %v is checked; this drive reads the OPENING view and something has chosen for it", where, r.Checked)
		}
		readings = append(readings, stageReading{
			VW: vw, View: r.View, Stage: r.Stage,
			DSW: dsw(t, where, r.Width), Lit: r.Lit[0],
			K: scaleOf(t, where, r), Box: r.Box,
		})
	}

	for _, r := range readings {
		t.Logf("vw=%-5d view=%-7.1f stage=%-7.1f --ds-w=%-7.0f k=%.3f box=%.1f opens on %s",
			r.VW, r.View, r.Stage, r.DSW, r.K, r.Box, tabName(r.Lit))
	}

	// CONTROL. Without a window that GROWS while its stage SHRINKS,
	// the monotonicity assertion below is satisfied by every rule
	// anyone could write and proves nothing at all.
	var grew, shrank stageReading
	for _, r := range readings {
		switch r.VW {
		case 700:
			grew = r
		case 800:
			shrank = r
		}
	}
	if grew.VW == 0 || shrank.VW == 0 {
		t.Fatalf("the sweep is missing the 700px/800px pair, which is the only pair that makes this drive capable of failing")
	}
	if shrank.Stage >= grew.Stage {
		t.Fatalf("the control is gone: the stage is %.1fpx in a 700px window and %.1fpx in an 800px one, so this page no longer has a window that grows while its stage shrinks and the assertion below cannot catch the bug it exists for. Find the width where the rail now arrives and sweep across it", grew.Stage, shrank.Stage)
	}
	t.Logf("control: the stage shrinks from %.1fpx to %.1fpx as the window grows from 700px to 800px — the rail arrives", grew.Stage, shrank.Stage)

	// The property. Sorted by stage width, the opening view may go
	// from Mobile to Desktop once and never back.
	byStage := append([]stageReading(nil), readings...)
	sort.Slice(byStage, func(i, j int) bool { return byStage[i].Stage < byStage[j].Stage })
	for i, wide := range byStage {
		for _, narrow := range byStage[:i] {
			if narrow.Lit == 0 && wide.Lit == 1 {
				t.Errorf("a %.1fpx stage (a %dpx window) opens on Desktop and a WIDER %.1fpx stage (a %dpx window) opens on Mobile. The opening view is being picked by something other than the width of the box it describes",
					narrow.Stage, narrow.VW, wide.Stage, wide.VW)
			}
		}
	}

	// And the threshold is the one the stylesheet documents.
	for _, r := range readings {
		want := 1
		if r.Stage >= stageThreshold {
			want = 0
		}
		if r.Lit != want {
			t.Errorf("a %.1fpx stage (a %dpx window) opens on %s; at or over %.0fpx of stage the desktop rendering is the legible one and under it the 390px rendering is",
				r.Stage, r.VW, tabName(r.Lit), stageThreshold)
		}
	}

	// The container moved up a level, and 100cqw moved with it.
	for _, r := range readings {
		if math.Abs(r.View-r.Stage) > 0.5 {
			t.Errorf("at %dpx the query container .ds-view is %.2fpx wide and the stage inside it is %.2fpx. The stylesheet asks (min-width: 48rem) of .ds-view and this drive checks the threshold against the STAGE, so the two have to be the same ruler; give .ds-view a padding or a border and the gallery and its gate quietly start measuring different elements", r.VW, r.View, r.Stage)
		}
		// The scale is the stage over the virtual width, clamped at
		// both ends — kMin below, 1 above. Writing this as min(1, …)
		// alone passed for two rounds because no width in the sweep
		// reached the floor; adding 280px and 320px is what showed the
		// assertion had never been asked the interesting question.
		if want := math.Min(1, math.Max(kMin, r.Stage/r.DSW)); math.Abs(r.K-want) > 0.01 {
			t.Errorf("at %dpx the frame is scaled by %.4f and the stage is %.1fpx of a %.0fpx virtual page, which clamps to %.4f — 100cqw is not resolving to the stage's width, or --ds-kmin is not the floor this drive thinks it is", r.VW, r.K, r.Stage, r.DSW, want)
		}
	}
}
