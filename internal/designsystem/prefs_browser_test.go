//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// prefsJS reads the display settings menu on a phone: where its button
// sits (measured against the root's box, since tokens.css reserves a
// stable scrollbar gutter that RTL puts on the left), whether the card
// is open and shown, and for every theme link, scheme button, the
// language summary and, once that is open, every language, whether it
// can be brought on screen inside the card and is then the topmost
// thing at its centre: in the card and above the page.
const prefsJS = `(() => {
  const q = s => document.querySelector(s), h = document.documentElement.getBoundingClientRect();
  const rtl = document.documentElement.dir === "rtl", d = q(".ds-prefs"), s = d.querySelector("summary"), r = s.getBoundingClientRect();
  const card = d.querySelector(":scope > [rst-dropdown-menu]");
  const controls = [...card.querySelectorAll("[rst-seg-tabs] a, [data-ds-scheme], [rst-locale] > summary, [rst-locale] [rst-dropdown-menu] a")]
    .filter(el => el.checkVisibility()).map(el => {
      el.scrollIntoView({block: "nearest", inline: "nearest"});
      const b = el.getBoundingClientRect(), hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
      return {Text: el.textContent.trim(), On: b.top >= 0 && b.bottom <= innerHeight && b.left >= h.left && b.right <= h.right,
        Top: !!hit && (hit === el || el.contains(hit)), H: Math.round(b.height)};
    });
  const strip = q("[rst-shell-back]");
  return JSON.stringify({Open: d.open, Shown: card.checkVisibility(), Top: Math.round(r.top), End: Math.round(rtl ? r.left - h.left : h.right - r.right),
    W: Math.round(r.width), H: Math.round(r.height), InStrip: !!strip && strip.contains(d), Controls: controls,
    Themes: card.querySelectorAll("[rst-seg-tabs] a").length, Locales: card.querySelectorAll("[rst-locale] [rst-dropdown-menu] a").length,
    Focus: document.activeElement === s, Path: location.pathname, Hash: location.hash});
})()`

type prefsControl struct {
	Text    string
	On, Top bool
	H       int
}

type prefsReading struct {
	Open, Shown, InStrip, Focus bool
	Top, End, W, H              int
	Themes, Locales             int
	Controls                    []prefsControl
	Path, Hash                  string
}

func readPrefs(t *testing.T, ctx context.Context, where string) prefsReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(prefsJS, &raw)); err != nil {
		t.Fatalf("%s: reading the display settings: %v", where, err)
	}
	var p prefsReading
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return p
}

// tapAt taps the centre of sel the way a finger does: scrolled to only
// if it is off screen, then a press and release at a point. chromedp's
// selector click scrolls its target "into view" even when it is on
// screen, and a button in the sticky strip sits inside the scroll
// padding, so any scroll-into-view moves the page, which is the
// reading position these legs measure.
func tapAt(t *testing.T, ctx context.Context, where, sel string) {
	t.Helper()
	var xy []float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const el = document.querySelector(`+"`"+sel+"`"+`); const o = el.getBoundingClientRect(); if (o.top < 0 || o.bottom > innerHeight) el.scrollIntoView({block: "nearest"}); const r = el.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`, &xy)); err != nil || len(xy) != 2 {
		t.Fatalf("%s: finding %s: %v", where, sel, err)
	}
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(xy[0], xy[1])); err != nil {
		t.Fatalf("%s: tapping %s: %v", where, sel, err)
	}
}

// axNameOf is the accessible name the browser computes for sel, which is
// what a screen reader announces, read off the accessibility tree rather
// than off an attribute that could be overridden.
func axNameOf(t *testing.T, ctx context.Context, where, sel string) string {
	t.Helper()
	var nodes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(sel, &nodes, chromedp.ByQuery)); err != nil || len(nodes) == 0 {
		t.Fatalf("%s: no node for %s: %v", where, sel, err)
	}
	var name string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		ax, err := accessibility.GetPartialAXTree().WithBackendNodeID(nodes[0].BackendNodeID).WithFetchRelatives(false).Do(c)
		if err == nil && len(ax) > 0 && ax[0].Name != nil {
			_ = json.Unmarshal(ax[0].Name.Value, &name)
		}
		return err
	})); err != nil {
		t.Fatalf("%s: reading the accessibility tree: %v", where, err)
	}
	return name
}

// TestTheDisplaySettingsMenuOnAPhone is the phone's theme, scheme and
// language controls: one menu at the top of every page, in the index's
// header row and at the inline end of a content page's back strip, in
// English and Arabic, scripts on and off, at 390 on a coarse pointer.
// Its button is a 44px icon named with the approved string. Opened,
// every theme link, scheme button (scripts on; with scripts off they are
// not drawn, as on a desktop) and language can be brought on screen in
// the card and is above the page. An outside tap closes it and reaches
// nothing under it; with scripts on, Escape closes it and hands focus
// back to the button.
func TestTheDisplaySettingsMenuOnAPhone(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, scripts := range []bool{true, false} {
		// The scripts-off tab is opened only once the scripted legs are
		// done: a new tab sends this one to the background, where the
		// engine stops drawing frames and a running transition never ends.
		c := ctx
		if !scripts {
			c = noScripts(t, ctx)
		}
		for _, locale := range []string{"en", "ar"} {
			for _, file := range []string{"index.html", fileOf("form")} {
				where := fmt.Sprintf("day/%s/%s at 390px, scripts %v", locale, file, scripts)
				url := rig.Origin + pageHref(mountPath, "day", locale, file)
				if err := chromedp.Run(c, chromedp.EmulateViewport(390, 844), chromedp.Navigate(url), chromedp.WaitVisible(".ds-prefs > summary", chromedp.ByQuery)); err != nil {
					t.Fatalf("%s: loading: %v", where, err)
				}
				requireCoarse(t, c)
				if !scripts {
					requireScriptsOff(t, c, where)
				}
				settleMotion(t, c, where)
				p := readPrefs(t, c, where)
				index := file == "index.html"
				if p.Open || p.Shown || p.W < 44 || p.H < 44 || p.End > 24 || index && p.Top > 32 || !index && (!p.InStrip || p.Top > 1) {
					t.Errorf("%s: closed %v shown %v, a %dx%dpx button %dpx from the top and %dpx from the inline end, in the back strip %v; want a closed 44px button at the top inline-end (in the strip on a content page)",
						where, !p.Open, p.Shown, p.W, p.H, p.Top, p.End, p.InStrip)
				}
				if name, want := axNameOf(t, c, where, ".ds-prefs > summary"), proseIn(locale, "Display settings"); name != want {
					t.Errorf("%s: the button is named %q, want %q", where, name, want)
				}

				mustRun := func(acts ...chromedp.Action) {
					t.Helper()
					if err := chromedp.Run(c, acts...); err != nil {
						t.Fatalf("%s: %v", where, err)
					}
				}
				mustRun(chromedp.Click(".ds-prefs > summary", chromedp.ByQuery), chromedp.WaitVisible(".ds-prefs [rst-locale] > summary", chromedp.ByQuery),
					chromedp.Click(".ds-prefs [rst-locale] > summary", chromedp.ByQuery), chromedp.WaitVisible(".ds-prefs [rst-locale] [rst-dropdown-menu] a", chromedp.ByQuery))
				settleMotion(t, c, where)
				open := readPrefs(t, c, where)
				wantControls := open.Themes + 1 + open.Locales
				if scripts {
					wantControls += 3
				}
				if !open.Open || !open.Shown || open.Themes != 3 || open.Locales != 12 || len(open.Controls) != wantControls {
					t.Errorf("%s: open %v shown %v with %d themes, %d languages and %d controls reachable; want three themes, %s twelve languages, all reachable",
						where, open.Open, open.Shown, open.Themes, open.Locales, len(open.Controls), map[bool]string{true: "three schemes,", false: ""}[scripts])
				}
				for _, ctl := range open.Controls {
					if !ctl.On || !ctl.Top || ctl.H < 44 {
						t.Errorf("%s: %q in the open card: on screen %v, topmost %v, %dpx tall; want on screen, above the page, a 44px tap", where, ctl.Text, ctl.On, ctl.Top, ctl.H)
					}
				}

				// An outside tap, low on the screen at the inline start,
				// clear of the card. It closes the card, and the page under
				// it does not move.
				var x float64 = 24
				if locale == "ar" {
					x = 366
				}
				mustRun(chromedp.MouseClickXY(x, 780))
				until(t, c, where+", after an outside tap", `!document.querySelector(".ds-prefs").open`)
				if after := readPrefs(t, c, where); after.Path != open.Path || after.Hash != open.Hash {
					t.Errorf("%s: the outside tap went through to the page: %s%s -> %s%s", where, open.Path, open.Hash, after.Path, after.Hash)
				}

				if scripts {
					mustRun(chromedp.Click(".ds-prefs > summary", chromedp.ByQuery), chromedp.WaitVisible(".ds-prefs [rst-seg-tabs] a", chromedp.ByQuery),
						chromedp.Focus(".ds-prefs [rst-seg-tabs] a", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
					if p := readPrefs(t, c, where); p.Open || !p.Focus {
						t.Errorf("%s: Escape from a theme link: open %v, focus on the button %v", where, p.Open, p.Focus)
					}
				} else {
					mustRun(chromedp.Click(".ds-prefs > summary", chromedp.ByQuery), chromedp.WaitVisible(".ds-prefs [rst-seg-tabs] a", chromedp.ByQuery),
						chromedp.Click(".ds-prefs > summary", chromedp.ByQuery))
					until(t, c, where+", toggled shut", `!document.querySelector(".ds-prefs").open`)
				}
			}
		}
	}
}

// TestTheDisplaySettingsKeepYourPlace: on a phone, a theme link in the
// menu carries the section being read, as the desktop bar's does, so a
// reader halfway down Form who switches theme lands on the same
// partial. Then Back: the page comes back from the cache with the
// scheme chosen after it, not the one it was left in.
func TestTheDisplaySettingsKeepYourPlace(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	where := "day/en/form.html at 390px"
	const at = "partial-field-check"
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))+"#"+at),
		chromedp.WaitVisible(".ds-prefs > summary", chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	until(t, ctx, where+", at the partial", `document.readyState === "complete" && Math.abs(document.getElementById("`+at+`").getBoundingClientRect().top - parseFloat(getComputedStyle(document.documentElement).scrollPaddingBlockStart)) < 4`)
	settleMotion(t, ctx, where)
	// The previews finish loading after the jump and move the partial;
	// put it back on the reading line, where a reader would be.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("`+at+`").scrollIntoView({block: "start"}); true`, nil)); err != nil {
		t.Fatalf("%s: scrolling to the partial: %v", where, err)
	}
	settleMotion(t, ctx, where)
	tapAt(t, ctx, where, ".ds-prefs > summary")
	until(t, ctx, where+", the menu open", `document.querySelector(".ds-prefs").open`)
	settleMotion(t, ctx, where+", the menu open")
	tapAt(t, ctx, where, `.ds-prefs [rst-seg-tabs] a[href*="/signal/"]`)
	until(t, ctx, where+", switched to signal", `location.pathname.includes("/signal/") && location.hash === "#`+at+`"`)
	settleMotion(t, ctx, where+", switched to signal")
	// The page arrived by a link, not by chromedp's own navigation, so
	// these are taps at points too: chromedp's selector actions wait on
	// the document it last read.
	tapAt(t, ctx, where, ".ds-prefs > summary")
	until(t, ctx, where+", the menu open on signal", `document.querySelector(".ds-prefs").open`)
	settleMotion(t, ctx, where+", the menu open on signal")
	tapAt(t, ctx, where, `.ds-prefs [data-ds-scheme="dark"]`)
	until(t, ctx, where+", Dark chosen", `document.documentElement.getAttribute("data-theme") === "dark"`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatalf("%s: going back: %v", where, err)
	}
	until(t, ctx, where+", back in Dark", `location.pathname.includes("/day/") && document.documentElement.getAttribute("data-theme") === "dark" && [...document.querySelectorAll('[data-ds-scheme="dark"]')].every(b => b.getAttribute("aria-pressed") === "true")`)
}
