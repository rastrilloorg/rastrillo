//go:build browser

// The browser drive for the gallery's own script. gallery.js is the
// only JavaScript in this tree that is not the framework's, and the
// only one whose whole job — write an attribute on <html>, remember it,
// reveal a control — is invisible to a Go test: nothing about it shows
// up in the rendered bytes, because everything it does happens after
// the bytes arrive.
//
// Build-tagged like ui/browser_test.go, and for the same reasons. Run
// it with:
//
//	go test -tags browser ./internal/designsystem/
//
// One test, one journey: load, click Dark, reload, click System. The
// reload is the half a unit test could never fake — persistence that
// survives a fresh document is the only kind worth having.
package designsystem

import (
	"context"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

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
