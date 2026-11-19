//go:build browser

package designsystem

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// previewWith is the first preview document in files, by name, that
// shows hook: a preview is the one kind of document that tells robots
// to stay out.
func previewWith(t *testing.T, files map[string][]byte, hook string) string {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		body := string(files[name])
		if strings.HasSuffix(name, ".html") &&
			strings.Contains(body, `content="noindex"`) &&
			strings.Contains(body, hook) {
			return name
		}
	}
	t.Fatalf("no preview shows %s", hook)
	return ""
}

// loaded is true once a preview has loaded and fiesta.js has run.
const loaded = `document.readyState === "complete" && ` +
	`window.fiestaShim === true`

// A preview's menu closes on a click outside it, and its switch's knob
// drags, with fiesta.js: rastrillo.js only polls now, so a preview that
// linked it alone would say menus never close and knobs never slide.
func TestAPreviewsMenusAndSwitchesStillWork(t *testing.T) {
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	menu := previewWith(t, files, "<details rst-dropdown")
	knob := previewWith(t, files, "rst-switch-track")
	rig := harness.New(t, func(string) http.Handler {
		return galleryrig.Tree(t, files, mountPrefix)
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()

	var open bool
	run := func(where string, actions ...chromedp.Action[chromedp.Void]) {
		t.Helper()
		if err := chromedp.Do(ctx, actions...); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
	}
	run(menu, chromedp.EmulateViewport(1024, 800),
		chromedp.Navigate(rig.Origin+mountPrefix+menu),
		chromedp.WaitVisible(chromedp.CSS(`[rst-dropdown] > summary`)))
	until(t, ctx, menu, loaded)
	run(menu, chromedp.Click(chromedp.CSS(`[rst-dropdown] > summary`)),
		harness.Into(&open, chromedp.Evaluate[bool](
			`document.querySelector("[rst-dropdown]").open`)))
	if !open {
		t.Fatalf("%s: the menu did not open", menu)
	}
	run(menu, chromedp.MouseClickXY(2, 2))
	until(t, ctx, menu+", after a click outside",
		`!document.querySelector("[rst-dropdown]").open`)

	type track struct{ Left, Right, Y float64 }
	var at track
	run(knob, chromedp.Navigate(rig.Origin+mountPrefix+knob),
		chromedp.WaitVisible(chromedp.CSS(`[rst-switch-track]`)))
	until(t, ctx, knob, loaded)
	run(knob, harness.Into(&at, chromedp.Evaluate[track](`(() => {
	  const t = document.querySelector("[rst-switch-track]");
	  t.scrollIntoView({block: "center"});
	  const b = t.getBoundingClientRect();
	  window.__box = t.closest("label").querySelector("input");
	  window.__was = window.__box.checked;
	  return {Left: b.left + b.height / 2, Right: b.right - b.height / 2,
	    Y: b.top + b.height / 2}; })()`)))
	from, to := at.Left, at.Right
	if strings.Contains(string(files[knob]), `dir="rtl"`) {
		from, to = to, from
	}
	var was bool
	run(knob, harness.Into(&was, chromedp.Evaluate[bool](`window.__was`)))
	if was {
		from, to = to, from
	}
	press := func(
		kind input.DispatchMouseEventType, across float64,
	) chromedp.Action[chromedp.Void] {
		return chromedp.MouseEvent(kind, across, at.Y,
			chromedp.ButtonLeft, chromedp.ClickCount(1))
	}
	// A press that moves is a drag only with fiesta.js; without it the
	// release is the label's click, which toggles all the same.
	var dragging bool
	run(knob, press(chromedp.MousePressed, from),
		press(chromedp.MouseMoved, from+(to-from)/3),
		press(chromedp.MouseMoved, from+2*(to-from)/3),
		press(chromedp.MouseMoved, to),
		harness.Into(&dragging, chromedp.Evaluate[bool](
			`window.__box.closest("label").hasAttribute("data-rst-drag")`)),
		press(chromedp.MouseReleased, to))
	if !dragging {
		t.Errorf("%s: the knob does not drag", knob)
	}
	until(t, ctx, knob+", after a drag across the track",
		`window.__box.checked !== window.__was`)
}
