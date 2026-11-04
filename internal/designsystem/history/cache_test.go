//go:build browser

package history

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The scheme follows a page back from the back/forward cache. On a
// phone shell.js sends Back through history, so a section page comes
// back from the cache without re-running anything: open Form in
// System, go Back to the index, choose Dark in its display settings
// menu, go Forward, and Form must be dark, previews and all, and its
// own menu's toggle pressed on Dark. The control first: the Forward
// really was a cache restore. A leg that silently reloaded Form would
// pass on start-up code alone.
//
// This has a package of its own, apart from both internal/designsystem
// and internal/designsystem/sweep, because the headless shell the cloud
// runner links as chromium never restores a page from the back/forward
// cache (always "masked"), so the leg needs full Chromium. Full Chromium
// is too slow for the whole design-system package (hack/gotest.sh's
// FULL_CHROMIUM comment), and it is still too slow for sweep's own heavy
// sweeps, which measure real rendering at many widths and locales and
// blew past the 20-minute package timeout under full Chromium on the
// cloud runner. This package holds only the handful of legs that
// actually need full Chromium, so it stays cheap to run there.
func TestTheSchemeFollowsAPageBackFromTheCache(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	where := "day/en at 390px"
	if err := chromedp.Do(ctx,
		addInit(`addEventListener("pageshow", e => { window.__restored = e.persisted; });`),
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(rig.Origin+indexHref(mountPath, "day", "en")),
		chromedp.WaitVisible(chromedp.CSS(`.ds-index`))); err != nil {
		t.Fatalf("%s: loading the index: %v", where, err)
	}
	requireCoarse(t, ctx)
	for _, c := range []struct{ scheme, attr string }{{"dark", "dark"}, {"system", ""}} {
		step := where + ", " + c.scheme
		leave(t, ctx, step+": opening Form",
			`document.getElementById("nav-form").click(); true`)
		until(t, ctx, step+", Form", `location.pathname.endsWith("/form.html") && document.readyState === "complete"`)
		// The previews are lazy and can start after the page is complete,
		// and a frame still navigating when Form is left keeps Form out of
		// the cache, so every one loads first.
		eagerly(t, ctx, step+", Form's previews")
		started, err := chromedp.Run(ctx, chromedp.Evaluate[float64](`performance.getEntriesByType("navigation")[0].startTime + performance.timeOrigin`))
		if err != nil {
			t.Fatalf("%s: reading Form's navigation: %v", step, err)
		}
		leave(t, ctx, step+": going back", `history.back(); true`)
		until(t, ctx, step+", the index", `location.pathname.endsWith("/index.html") && !!document.querySelector('#ds-prefs [data-ds-scheme="`+c.scheme+`"]')`)
		leave(t, ctx, step+": choosing "+c.scheme+" and going forward",
			`document.getElementById("ds-prefs").open = true;
			document.querySelector(
			  '#ds-prefs [data-ds-scheme="`+c.scheme+`"]').click();
			history.forward(); true`)
		until(t, ctx, step+", Form again", `location.pathname.endsWith("/form.html") && window.__restored !== undefined`)
		raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](
			`JSON.stringify({Restored: window.__restored,
		  Started: performance.getEntriesByType("navigation")[0].startTime + performance.timeOrigin,
		  Why: JSON.stringify((performance.getEntriesByType("navigation")[0] || {}).notRestoredReasons || null)})`))
		if err != nil {
			t.Fatalf("%s: reading the restore: %v", step, err)
		}
		var r struct {
			Restored bool
			Started  float64
			Why      string
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("%s: decoding %q: %v", step, raw, err)
		}
		if !r.Restored || r.Started != started {
			t.Fatalf("%s: Forward was not a back/forward cache restore (persisted %v, navigation %s then %s; the engine says %s). The tree handler must not send Cache-Control: no-store", step, r.Restored, strconv.FormatFloat(started, 'f', 0, 64), strconv.FormatFloat(r.Started, 'f', 0, 64), r.Why)
		}
		// Frames that have not loaded are skipped, but at least one must
		// have: every() over none is true, and the previews would pass
		// without one being looked at.
		until(t, ctx, step+", painted", fmt.Sprintf(`(() => {
		  const loaded = [...document.querySelectorAll(".ds-view__frame")].filter(f => { const d = f.contentDocument; return d && d.URL === f.src; });
		  return (document.documentElement.getAttribute("data-theme") || "") === %q && loaded.length > 0 &&
		    loaded.every(f => (f.contentDocument.documentElement.getAttribute("data-theme") || "") === %q) &&
		    [...document.querySelectorAll('[data-ds-scheme=%q]')].every(b => b.getAttribute("aria-pressed") === "true");
		})()`, c.attr, c.attr, c.scheme))
		leave(t, ctx, step+": back to the index", `history.back(); true`)
		until(t, ctx, step+", index again", `location.pathname.endsWith("/index.html")`)
	}
}
