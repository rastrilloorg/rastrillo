//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// shimOSListener is the line of rastrillo.js that lands an OS scheme
// flip in its own document. The gallery has a listener of its own,
// which also lands every preview frame, and on a page that links both
// the shim's masks it: TestTheGallerySwitchLandsOnTheNextFrame passed
// with the gallery's deleted (#104 item 6).
const shimOSListener = `if (osScheme) osScheme.onchange = function () { if (!document.documentElement.hasAttribute("data-theme")) land(); };`

// treeWithoutTheShimsListener is the gallery tree with that line cut
// out of the served rastrillo.js, so whatever lands an OS flip on the
// page is the gallery's own code.
func treeWithoutTheShimsListener(t *testing.T) http.Handler {
	t.Helper()
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var shim []string
	for name := range files {
		base := name[strings.LastIndex(name, "/")+1:]
		if strings.HasPrefix(base, "rastrillo.") && strings.HasSuffix(base, ".js") {
			shim = append(shim, name)
		}
	}
	// The tree writes the shim under its plain name and its hashed one;
	// the pages link the hashed one, and both are cut so the choice
	// cannot matter.
	if len(shim) == 0 {
		t.Fatal("the tree carries no rastrillo.js")
	}
	for _, name := range shim {
		src := string(files[name])
		if strings.Count(src, shimOSListener) != 1 {
			t.Fatalf("%s no longer carries its OS listener as this test knows it; update shimOSListener:\n%s", name, shimOSListener)
		}
		files[name] = []byte(strings.Replace(src, shimOSListener, "/* OS listener removed by the test */", 1))
	}
	return galleryrig.Tree(t, files, mountPrefix)
}

// The gallery's own OS listener lands a flip under System while a rail
// item is hovered, with the shim's listener out of the way: the item's
// text colour read a frame after the flip is already the value it
// settles to. The listener's job is to land every transition running
// in the page and its frames; the framework's own hover tints no
// longer need it (#104 item 2: they ride a number, not a colour), so
// the thing to land here is a page rule of the gallery's own kind, a
// 0.25s transition on the rail item's colour, which its :hover changes
// to --rst-text. Without a listener the frame after the flip is a
// quarter of the way between the two schemes' text colours. The rule
// is test-only and carries !important, because the hovered item's
// transition list is tokens.css's own; nothing shipped carries one.
func TestTheGalleryLandsAnOSFlipWithItsOwnListener(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeWithoutTheShimsListener(t) }, harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()
	where := "the overview at 1280px, shim listener removed"
	os := func(scheme string) chromedp.Action {
		return emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}})
	}
	if err := chromedp.Run(ctx, os("light"), chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+indexHref(mountPath, RootTheme(), "en")),
		chromedp.WaitReady(`body`, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	until(t, ctx, where, `document.readyState === "complete" && !document.documentElement.hasAttribute("data-theme") && matchMedia("(hover: hover)").matches`)
	// The control: the page really did lose the shim's listener, or
	// the drive proves nothing about the gallery's.
	var shimHasListener bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(async () => { const s = [...document.scripts].find(s => /rastrillo\.[0-9a-f]+\.js$/.test(s.src)); const body = await (await fetch(s.src)).text(); return body.includes("osScheme.onchange"); })()`, &shimHasListener, awaitPromise)); err != nil {
		t.Fatalf("%s: reading the served shim: %v", where, err)
	}
	if shimHasListener {
		t.Fatalf("%s: the served rastrillo.js still carries its OS listener", where)
	}
	// A rail item that is not the current one, hovered by the mouse,
	// and its colour allowed to settle. The overview shows only its own
	// link; the rest sit in closed sections, so the first is opened.
	var r struct{ X, Y float64 }
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const s = document.createElement("style"); s.textContent = "[rst-shell-nav] a, [rst-shell-nav] a:hover { transition: color 0.25s ease !important; }"; document.head.append(s); document.querySelector('.ds-nav details').open = true; const a = [...document.querySelectorAll('[rst-shell-nav] a:not([aria-current])')].find(a => a.checkVisibility()); window.__hov = a; const b = a.getBoundingClientRect(); return {X: b.left + b.width / 2, Y: b.top + b.height / 2}; })()`, &r)); err != nil {
		t.Fatalf("%s: finding a rail item: %v", where, err)
	}
	settled := func(leg string) string {
		t.Helper()
		var bg string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(async () => { await Promise.all(window.__hov.getAnimations().map(a => a.finished.catch(() => null))); return getComputedStyle(window.__hov).color; })()`, &bg, awaitPromise)); err != nil {
			t.Fatalf("%s, %s: %v", where, leg, err)
		}
		return bg
	}
	var rest string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(window.__hov).color`, &rest)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, r.X, r.Y)); err != nil {
		t.Fatalf("%s: hovering: %v", where, err)
	}
	hoverLight := settled("hovered, OS light")
	if hoverLight == rest {
		t.Fatalf("%s: the rail item's colour is %s hovered and at rest; nothing fades, so the drive would prove nothing", where, rest)
	}
	// The flip, read a frame later: the change event reaches script a
	// frame after the style update that starts the fade.
	var next string
	if err := chromedp.Run(ctx, os("dark"), chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(getComputedStyle(window.__hov).color))))`, &next, awaitPromise)); err != nil {
		t.Fatalf("%s: flipping the OS: %v", where, err)
	}
	hoverDark := settled("hovered, OS dark")
	t.Logf("%s: hovered light %s, a frame after the flip %s, settled dark %s", where, hoverLight, next, hoverDark)
	if hoverDark == hoverLight {
		t.Fatalf("%s: the hovered item's colour does not change with the scheme: %s", where, hoverLight)
	}
	if next != hoverDark {
		t.Errorf("%s: a frame after the OS flip the hovered item is %s; settled it is %s. The gallery's own listener did not land the flip.", where, next, hoverDark)
	}
}
