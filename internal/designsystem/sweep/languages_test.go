//go:build browser

package sweep

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
)

// Every language in the index foot's menu can be reached: by Tab, with
// scripts, and by scrolling the way a finger would, the panel's own
// scroll and then the document's, in every combination. Which way the
// panel opens is not asserted: with anchor positioning it may flip
// upward, and without it, it opens downward and extends the document.
// The index is scrolled to its end first, the case with no room below.
//
// "On screen" is inside documentElement's client box, on a rig that
// draws its scrollbars: innerWidth counts the 15px under the root's
// scrollbar, and with scrollbars hidden clientWidth counts the gutter
// the page is not laid out in, so either way a link up to 15px past the
// page's edge read as on screen. See RequireDrawnScrollbar.
func TestEveryLanguageIsReachableFromThePhoneIndex(t *testing.T) {
	rig := phoneRig(t, harness.WithScrollbars())
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, scripts := range []bool{true, false} {
		tab := ctx
		if !scripts {
			tab = noScripts(t, ctx)
		}
		for _, w := range []int64{390, 320} {
			for _, locale := range []string{"en", "ar"} {
				for _, anchored := range []bool{true, false} {
					where := fmt.Sprintf("day/%s index at %dpx, scripts %v, anchor positioning %v", locale, w, scripts, anchored)
					if err := chromedp.Run(tab, chromedp.EmulateViewport(w, 700),
						chromedp.Navigate(rig.Origin+indexHref(mountPath, "day", locale)),
						chromedp.WaitVisible(`#ds-prefs`, chromedp.ByQuery)); err != nil {
						t.Fatalf("%s: loading: %v", where, err)
					}
					if !scripts {
						requireScriptsOff(t, tab, where)
					}
					if !anchored {
						if err := chromedp.Run(tab, chromedp.Evaluate(withoutAnchorPositioning, nil,
							func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
							t.Fatalf("%s: switching anchor positioning off: %v", where, err)
						}
					}
					requireAnchorPositioning(t, tab, where, "#ds-prefs [rst-dropdown-menu]", anchored)
					requireCoarse(t, tab)
					requireDrawnScrollbar(t, tab, where)
					if err := chromedp.Run(tab, chromedp.Evaluate(`scrollTo(0, document.documentElement.scrollHeight);
					  const d = document.querySelector("#ds-prefs [rst-locale]"); d.open = true; d.querySelector("summary").focus(); true`, nil)); err != nil {
						t.Fatalf("%s: opening the menu: %v", where, err)
					}
					for i, code := range rastrillo.BaseLocales() {
						if scripts {
							var at string
							// 0.5px slack: layout at a fractional rem size lands a few
							// hundredths of a pixel past the edge, which no eye and no
							// finger can tell from on screen.
							if err := chromedp.Run(tab, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`(() => { const a = document.activeElement, r = a.getBoundingClientRect();
							  const de = document.documentElement;
							  return r.top >= -0.5 && r.bottom <= de.clientHeight + 0.5 && r.left >= -0.5 && r.right <= de.clientWidth + 0.5 ? a.lang : "off screen: " + a.outerHTML.slice(0, 60); })()`, &at)); err != nil {
								t.Fatalf("%s: tab %d: %v", where, i+1, err)
							}
							if at != code {
								t.Errorf("%s: tab %d is %s, want the %s link on screen", where, i+1, at, code)
							}
						}
						var hit bool
						if err := chromedp.Run(tab, chromedp.Evaluate(fmt.Sprintf(`(() => {
						  const a = document.querySelector('#ds-prefs [rst-dropdown-menu] a[lang=%q]');
						  a.scrollIntoView({block: "nearest", inline: "nearest"});
						  const r = a.getBoundingClientRect();
						  return document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2) === a;
						})()`, code), &hit)); err != nil {
							t.Fatalf("%s: scrolling to %s: %v", where, code, err)
						}
						if !hit {
							t.Errorf("%s: scrolled into view, the %s link is not what a finger at its centre touches", where, code)
						}
					}
				}
			}
		}
	}

	// Reaching a link is not using it: a wrong href would still pass
	// every check above, since none of them follow the link anywhere.
	// Checked once, at the size the reachability sweep above already
	// covers in full: scripts on, 390px, day/en, anchor positioning on.
	// Each activation is a plain navigation (these are real anchors,
	// not a script's route), so the leg polls with until rather than
	// waiting on an event a history traversal does not raise.
	where := "day/en index at 390px, scripts true, anchor positioning true"
	for _, code := range rastrillo.BaseLocales() {
		// The viewport is set on every pass: the matrix above leaves this
		// target at whichever width it drove last, and the leg must be
		// the 390px combination it names whatever order that loop runs in.
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 700),
			chromedp.Navigate(rig.Origin+indexHref(mountPath, "day", "en")),
			chromedp.WaitVisible(`#ds-prefs`, chromedp.ByQuery),
			chromedp.Evaluate(`document.querySelector("#ds-prefs [rst-locale]").open = true; true`, nil)); err != nil {
			t.Fatalf("%s: returning to day/en to activate %s: %v", where, code, err)
		}
		want := indexHref(mountPath, "day", code)
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`document.querySelector('#ds-prefs [rst-dropdown-menu] a[lang=%q]').click(); true`, code), nil)); err != nil {
			t.Fatalf("%s: activating %s: %v", where, code, err)
		}
		until(t, ctx, where+", landed on "+code, fmt.Sprintf(`document.documentElement.lang === %q && location.pathname === %q`, code, want))
	}

	// With scripts off the link is nothing but its href: one activation
	// through a real navigation proves that, rather than only that the
	// element sits where a tap or a Tab would land.
	offWhere := "day/en index at 390px, scripts off"
	off := noScripts(t, ctx)
	if err := chromedp.Run(off, chromedp.EmulateViewport(390, 700),
		chromedp.Navigate(rig.Origin+indexHref(mountPath, "day", "en")),
		chromedp.WaitVisible(`#ds-prefs`, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading: %v", offWhere, err)
	}
	requireScriptsOff(t, off, offWhere)
	wantAr := indexHref(mountPath, "day", "ar")
	if err := chromedp.Run(off,
		chromedp.Evaluate(`document.querySelector("#ds-prefs [rst-locale]").open = true; true`, nil),
		chromedp.Evaluate(`document.querySelector('#ds-prefs [rst-dropdown-menu] a[lang="ar"]').click(); true`, nil)); err != nil {
		t.Fatalf("%s: activating ar: %v", offWhere, err)
	}
	until(t, off, offWhere+", landed on ar", fmt.Sprintf(`document.documentElement.lang === "ar" && location.pathname === %q`, wantAr))
	requireScriptsOff(t, off, offWhere+", on the ar index")
}
