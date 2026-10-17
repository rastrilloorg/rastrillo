//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// An old link to a section that moved pages still reaches it. The Form
// page split, and field-select went to Choices and actions, so a
// bookmark or a search result saying form.html#partial-field-select
// names nothing on Form. gallery.js looks a fragment with no element up
// in the rail, which links every section on every page, and follows the
// link that carries it. Nothing is listed anywhere, so the next move
// works the same way.
//
// Two legs it must not take: a fragment that IS on the page is left
// alone, and a fragment the rail does not know (a typo, the skip link's
// #main) stays where it is. Neither leg can wait for a redirect that
// must not happen, so each settles for the time the first leg took to
// arrive and then reads the address.
func TestAMovedSectionsOldLinkReachesIt(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	form := rig.Origin + pageHref(mountPath, "day", "en", fileOf("form"))

	where := "form.html#partial-field-select"
	start := time.Now()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(form+"#partial-field-select")); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	until(t, ctx, where, `location.pathname.endsWith("/`+fileOf("choices-and-actions")+`") && location.hash === "#partial-field-select" && document.readyState === "complete" && (() => { const r = document.getElementById("partial-field-select").getBoundingClientRect(); return r.top >= 0 && r.top < innerHeight; })()`)
	// Replaced, not pushed: Back from the section goes to wherever the
	// reader came from, not to a page that would send them on again.
	var depth int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.length`, &depth)); err != nil {
		t.Fatalf("%s: reading history: %v", where, err)
	}
	if depth != 2 {
		t.Errorf("%s: history is %d deep after the redirect, want 2 (about:blank, then the section); the old address was pushed rather than replaced", where, depth)
	}
	settle := 2*time.Since(start) + time.Second

	for _, c := range []struct{ hash, want string }{
		{"#partial-field-url", "a section that is on Form"},
		{"#nope", "a fragment the rail does not know"},
		{"#main", "the skip link's target, which is not a section"},
	} {
		where := "form.html" + c.hash + ", " + c.want
		if err := chromedp.Run(ctx, chromedp.Navigate(form+c.hash)); err != nil {
			t.Fatalf("%s: loading: %v", where, err)
		}
		until(t, ctx, where, `document.readyState === "complete"`)
		time.Sleep(settle)
		var href string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`location.href`, &href)); err != nil {
			t.Fatalf("%s: reading the address: %v", where, err)
		}
		if href != form+c.hash {
			t.Errorf("%s: the page moved to %s; %s is left alone", where, href, c.want)
		}
	}
}

// The same old link followed with Form already open: only the fragment
// changes, so nothing is loaded and readiness never fires again. The
// lookup has to run on hashchange too, or a reader on Form who opens a
// bookmark to a moved section stays where they are. Astra's P2 on the
// branch, found by running the readiness callback in an event harness.
func TestAMovedSectionsOldLinkReachesItFromAnOpenPage(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	form := rig.Origin + pageHref(mountPath, "day", "en", fileOf("form"))
	where := "form.html open, then #partial-field-select"
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(form)); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	until(t, ctx, where+", Form loaded", `document.readyState === "complete"`)
	// The address bar, a bookmark or a link on another site: a change
	// of fragment on a document that is already there.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.hash = "#partial-field-select"; true`, nil)); err != nil {
		t.Fatalf("%s: setting the hash: %v", where, err)
	}
	until(t, ctx, where, `location.pathname.endsWith("/`+fileOf("choices-and-actions")+`") && location.hash === "#partial-field-select" && document.readyState === "complete" && (() => { const r = document.getElementById("partial-field-select").getBoundingClientRect(); return r.top >= 0 && r.top < innerHeight; })()`)
}
