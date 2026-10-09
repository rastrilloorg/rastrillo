//go:build browser

package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// profileDefs are the blocks a page fills for the profile menu: the
// app's name, the person, and the account's links. The language menu
// (#rail-locale, English and Gaeilge) comes from shellLayoutPage.
func profileDefs(brand string) []string {
	return []string{
		`{{define "brand"}}<a rst-shell-brand href="/">` + brand + `</a>{{end}}`,
		`{{define "profile"}}<span rst-person-av aria-hidden="true">G</span><span rst-person-name>Grace Hopper</span><span rst-person-email>grace@example.com</span>{{end}}`,
		`{{define "account"}}<a id="acct-profile" href="/go/profile">Profile</a><a id="acct-out" href="/go/signout">Sign out</a>{{end}}`,
		`{{define "content"}}<h1>Invoices</h1><p><a id="main-link" href="/go/main">A link in the page</a></p>{{end}}`,
	}
}

// profileShell is where each shell keeps the profile menu: the sidebar's
// own <details rst-shell-profile> in the rail's foot, and the console's
// Menu, whose summary shows the avatar on the index and whose card is
// the tail beside it. The two cards read the same: the name and email,
// the Language menu, then the account's links laid out flat.
type profileShell struct {
	trigger, card, account string
}

var profileShells = map[string]profileShell{
	"sidebar": {"[rst-shell-profile] > summary", "[rst-shell-profile] > [rst-dropdown-menu]", ""},
	"console": {"[rst-shell-menu] > summary", "[rst-shell-tail]", ""},
}

// profileJS reads the index's header row and the card: the heading's
// text, size and height, where the avatar sits against the root's box
// (tokens.css reserves a stable scrollbar gutter, which RTL puts on the
// left, so the viewport's edge is not the page's), and for each element
// the card should show, whether it is on screen and the topmost thing
// at its centre: in the card and above the page.
const profileJS = `((trigger, card) => {
  const q = s => document.querySelector(s), h = document.documentElement.getBoundingClientRect();
  const rtl = document.documentElement.dir === "rtl", sum = q(trigger), r = sum.getBoundingClientRect();
  const title = q("[rst-shell-title]"), t = title.getBoundingClientRect(), foot = q("[rst-shell-rail-foot]");
  const nav = [...document.querySelectorAll("[rst-shell-nav] > a")].pop().getBoundingClientRect();
  const seen = sel => [...document.querySelectorAll(sel)].filter(el => el.checkVisibility()).map(el => {
    const b = el.getBoundingClientRect(), hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    return {Sel: sel, Text: el.textContent.trim(), On: b.width > 0 && b.top >= 0 && b.bottom <= innerHeight && b.left >= h.left && b.right <= h.right, Top: !!hit && (hit === el || el.contains(hit))};
  });
  const c = q(card), cb = c.getBoundingClientRect();
  return JSON.stringify({
    Open: sum.parentElement.open, Title: title.innerText.trim(), TitleH: Math.round(t.height), TitleSize: parseFloat(getComputedStyle(title).fontSize),
    TitleOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    AvTop: Math.round(r.top), AvEnd: Math.round(rtl ? r.left - h.left : h.right - r.right), AvW: Math.round(r.width), AvH: Math.round(r.height),
    FootBelowNav: !!foot && foot.getBoundingClientRect().top > nav.bottom,
    CardShown: c.checkVisibility(), CardTop: Math.round(cb.top), AvBottom: Math.round(r.bottom),
    Items: [].concat(seen("[rst-shell-who] [rst-person-name]"), seen("[rst-shell-who] [rst-person-email]"),
      seen("#acct-profile"), seen("#acct-out"), seen("#rail-locale [rst-dropdown-menu] a")),
    Focus: document.activeElement === sum,
  });
})(%q, %q)`

type profileItem struct {
	Sel, Text string
	On, Top   bool
}

type profileReading struct {
	Open, CardShown, FootBelowNav, Focus bool
	Title                                string
	TitleH, TitleOverflow                int
	TitleSize                            float64
	AvTop, AvEnd, AvW, AvH               int
	CardTop, AvBottom                    int
	Items                                []profileItem
}

// requireCardItems holds an open card's readings to what it must show:
// the name and the email, both account links and both languages, each
// present with its own text, on screen and the topmost thing at its
// centre. Asserting only the ones that were found would pass a card
// that had lost an item.
func requireCardItems(t *testing.T, where string, items []profileItem) {
	t.Helper()
	want := map[string]string{"[rst-shell-who] [rst-person-name]": "Grace Hopper", "[rst-shell-who] [rst-person-email]": "grace@example.com", "#acct-profile": "Profile", "#acct-out": "Sign out"}
	got := map[string]bool{}
	locales := map[string]bool{}
	for _, it := range items {
		if !it.On || !it.Top {
			t.Errorf("%s: %s (%q) in the open card: on screen %v, topmost %v; want it on screen and above the page", where, it.Sel, it.Text, it.On, it.Top)
		}
		if w, ok := want[it.Sel]; ok && it.Text != w {
			t.Errorf("%s: %s reads %q, want %q", where, it.Sel, it.Text, w)
		}
		got[it.Sel] = true
		if it.Sel == "#rail-locale [rst-dropdown-menu] a" {
			locales[it.Text] = true
		}
	}
	for sel := range want {
		if !got[sel] {
			t.Errorf("%s: the open card never showed %s", where, sel)
		}
	}
	if !locales["English"] || !locales["Gaeilge"] {
		t.Errorf("%s: the card showed the languages %v, want English and Gaeilge", where, locales)
	}
}

func readProfile(t *testing.T, ctx context.Context, p profileShell) profileReading {
	t.Helper()
	var r profileReading
	at(t, ctx, fmt.Sprintf(profileJS, p.trigger, p.card), &r)
	return r
}

// TestTheProfileMenuOnThePhoneIndex is the phone index of both shells,
// LTR and RTL, scripts on and off, at 390 on a coarse pointer. The
// heading is the app's name, about 21px, on one line however long the
// name; the avatar is a 44px button at the top inline-end, named with
// the person's name; nothing is left at the foot. Opened, the card
// shows the name, the email, the account's links and every language,
// each on screen and above the page. An outside tap closes it without
// reaching the page, with and without scripts; with scripts Escape
// closes it and hands focus back to the avatar.
func TestTheProfileMenuOnThePhoneIndex(t *testing.T) {
	const long = "Harbour Freight Forwarding and Customs Brokerage"
	for _, shell := range []string{"sidebar", "console"} {
		for _, dir := range []string{"ltr", "rtl"} {
			for _, scripts := range []bool{true, false} {
				shell, dir, scripts := shell, dir, scripts
				t.Run(fmt.Sprintf("%s %s scripts %v", shell, dir, scripts), func(t *testing.T) {
					p := profileShells[shell]
					src, _ := Layout(shell)
					pages := map[string]string{
						"/":     shellLayoutPage(t, src, dir, append(profileDefs("Harbour"), `{{define "view"}}index{{end}}`)...),
						"/long": shellLayoutPage(t, src, dir, append(profileDefs(long), `{{define "view"}}index{{end}}`)...),
						"/go/":  sizingDoc("landed", `<p id="landed">landed</p>`),
					}
					rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
					ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
					defer cancel()
					acts := []chromedp.Action{chromedp.EmulateViewport(390, 844)}
					if !scripts {
						acts = append(acts, emulation.SetScriptExecutionDisabled(true))
					}
					mustRun(t, ctx, append(acts, chromedp.Navigate(rig.Origin+"/long"), chromedp.WaitVisible(p.trigger, chromedp.ByQuery))...)
					requirePointer(t, ctx, true)
					lr := readProfile(t, ctx, p)
					if lr.Title != long || lr.TitleH > 56 || lr.TitleOverflow > 0 {
						t.Errorf("a long brand: heading %q, %dpx tall, the page %dpx wider than the screen; want the whole name, on one line, eliding inside the screen", lr.Title, lr.TitleH, lr.TitleOverflow)
					}

					mustRun(t, ctx, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(p.trigger, chromedp.ByQuery))
					settled(t, ctx)
					closed := readProfile(t, ctx, p)
					if closed.Title != "Harbour" || closed.TitleH > 56 || closed.TitleSize < 19 || closed.TitleSize > 23 {
						t.Errorf("the heading is %q at %.1fpx, %dpx tall; want the brand, Harbour, at about 21px on one line in the 56px header row", closed.Title, closed.TitleSize, closed.TitleH)
					}
					if closed.AvTop > 32 || closed.AvEnd > 24 || closed.AvW < 44 || closed.AvH < 44 {
						t.Errorf("the avatar is %dx%dpx, %dpx from the top and %dpx from the inline end; want a 44px tap at the top inline-end", closed.AvW, closed.AvH, closed.AvTop, closed.AvEnd)
					}
					if closed.FootBelowNav {
						t.Error("the rail's foot is still under the nav on the phone index")
					}
					if closed.Open || closed.CardShown {
						t.Error("the card is open before anyone opened it")
					}
					if name := axName(ctx, t, p.trigger); name != "Grace Hopper" {
						t.Errorf("the avatar's accessible name is %q, want the person's name", name)
					}

					// Opened, both shells' cards read in one order with the
					// language closed: the name, the email, the Language menu,
					// then the account's links, flat, with no Account menu to
					// open first.
					mustRun(t, ctx, chromedp.Click(p.trigger, chromedp.ByQuery), chromedp.WaitVisible(p.card+" [rst-shell-who]", chromedp.ByQuery))
					var order []string
					at(t, ctx, `JSON.stringify([...document.querySelectorAll("[rst-shell-who] [rst-person-name], [rst-shell-who] [rst-person-email], #rail-locale > summary, #acct-profile, #acct-out")]
					  .filter(e => e.checkVisibility()).sort((a, b) => a.getBoundingClientRect().top - b.getBoundingClientRect().top).map(e => e.id || e.getAttribute("rst-person-name") !== null && "name" || e.getAttribute("rst-person-email") !== null && "email" || "language"))`, &order)
					if got := strings.Join(order, " "); got != "name email language acct-profile acct-out" {
						t.Errorf("the open card reads %q, top to bottom; want the name, the email, the language, then the account's links, flat", got)
					}
					var items []profileItem
					if p.account != "" {
						mustRun(t, ctx, chromedp.Click(p.account, chromedp.ByQuery), chromedp.WaitVisible("#acct-out", chromedp.ByQuery))
						items = append(items, readProfile(t, ctx, p).Items...)
					}
					mustRun(t, ctx, chromedp.Click("#rail-locale > summary", chromedp.ByQuery), chromedp.WaitVisible("#rail-locale [rst-dropdown-menu] a", chromedp.ByQuery))
					open := readProfile(t, ctx, p)
					items = append(items, open.Items...)
					if !open.Open || !open.CardShown || open.CardTop < open.AvBottom {
						t.Errorf("opened: open %v, card shown %v, card top %d under an avatar ending at %d; want a card dropping from the avatar", open.Open, open.CardShown, open.CardTop, open.AvBottom)
					}
					if open.AvW < 44 || open.AvH < 44 {
						t.Errorf("the open avatar is %dx%dpx; the open state must stay a 44px tap", open.AvW, open.AvH)
					}
					requireCardItems(t, "the phone index", items)

					// An outside tap: on a nav row, clear of the card at the
					// row's inline start. It closes the card and the page
					// under it never sees it.
					fx := 0.03
					if dir == "rtl" {
						fx = 0.97
					}
					clickAndStay(t, ctx, probeScripted(t, ctx, scripts, "#nav-team", fx, 0.5, 0, 0), "",
						fmt.Sprintf(`!document.querySelector(%q).parentElement.open`, p.trigger))

					if scripts {
						mustRun(t, ctx, chromedp.Click(p.trigger, chromedp.ByQuery), chromedp.WaitVisible(p.card+" [rst-shell-who]", chromedp.ByQuery))
						focus := "#acct-profile"
						if p.account != "" {
							mustRun(t, ctx, chromedp.Click(p.account, chromedp.ByQuery), chromedp.WaitVisible(focus, chromedp.ByQuery))
						}
						mustRun(t, ctx, chromedp.Focus(focus, chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
						if after := readProfile(t, ctx, p); after.Open || !after.Focus {
							t.Errorf("Escape from a link in the card: open %v, focus on the avatar %v; want it closed and focus handed back", after.Open, after.Focus)
						}
					} else {
						// Scripts off, the avatar's own toggle is all there is.
						mustRun(t, ctx, chromedp.Click(p.trigger, chromedp.ByQuery), chromedp.WaitVisible(p.card+" [rst-shell-who]", chromedp.ByQuery),
							chromedp.Click(p.trigger, chromedp.ByQuery))
						settleUntil(t, ctx, fmt.Sprintf(`!document.querySelector(%q).parentElement.open`, p.trigger))
					}
				})
			}
		}
	}
}

// TestTheProfileMenuIsADropUpAtTheRailsFoot is the sidebar on a
// desktop, where the profile menu is the rail's foot: an avatar at the
// bottom of the rail at its inline start, opening a card UPWARD that is
// anchored at the avatar's inline start and grows across the page. It
// has to stay on screen in a 1280x720 window and a 1024x600 one, with
// the language menu open inside it; close on an outside tap, scripts on
// or off, and on Escape; and mirror in RTL, which the inline-start
// readings cover by being measured from the inline start of each
// direction.
func TestTheProfileMenuIsADropUpAtTheRailsFoot(t *testing.T) {
	p := profileShells["sidebar"]
	src, _ := Layout("sidebar")
	// 1280x420 is a laptop with devtools open: the shortest window the
	// rail still sits beside the page in. Anchored, the card cannot
	// flip downward at the rail's foot: the foot is at the bottom of a
	// rail the window's height, so there is never more room below than
	// above, and the card's height cap shrinks with the window. What the
	// short window has to prove is that the card still fits above.
	for _, vp := range [][2]int64{{1280, 720}, {1024, 600}, {1280, 420}} {
		for _, dir := range []string{"ltr", "rtl"} {
			for _, scripts := range []bool{true, false} {
				for _, anchored := range []bool{true, false} {
					vp, dir, scripts, anchored := vp, dir, scripts, anchored
					t.Run(fmt.Sprintf("%dx%d %s scripts %v anchored %v", vp[0], vp[1], dir, scripts, anchored), func(t *testing.T) {
						pages := map[string]string{"/": shellLayoutPage(t, src, dir, profileDefs("Harbour")...), "/go/": sizingDoc("landed", `<p id="landed">landed</p>`)}
						rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
						ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
						defer cancel()
						acts := []chromedp.Action{chromedp.EmulateViewport(vp[0], vp[1])}
						if !scripts {
							acts = append(acts, emulation.SetScriptExecutionDisabled(true))
						}
						mustRun(t, ctx, append(acts, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(p.trigger, chromedp.ByQuery))...)
						settled(t, ctx)
						// Without anchor positioning, the path every engine
						// without it takes: the card absolute in the rail,
						// opened upward by the hand-written insets.
						if !anchored {
							mustRun(t, ctx, chromedp.Evaluate(galleryrig.WithoutAnchorPositioning, nil, awaitPromise))
						}
						galleryrig.RequireAnchorPositioning(t, ctx, "the drop-up", "[rst-shell-profile] > [rst-dropdown-menu]", anchored)
						const geom = `(() => {
					  const q = s => document.querySelector(s), rtl = document.documentElement.dir === "rtl";
					  const rail = q("[rst-shell-rail]").getBoundingClientRect(), s = q("[rst-shell-profile] > summary").getBoundingClientRect();
					  const c = q("[rst-shell-profile] > [rst-dropdown-menu]"), cb = c.getBoundingClientRect();
					  const start = b => rtl ? rail.right - b.right : b.left - rail.left;
					  return JSON.stringify({FootGap: Math.round(rail.bottom - s.bottom), AvStart: Math.round(start(s)),
					    Shown: c.checkVisibility(), CardStart: Math.round(start(cb)), CardBottom: Math.round(cb.bottom), AvTop: Math.round(s.top),
					    OnScreen: cb.top >= 0 && cb.bottom <= innerHeight && cb.left >= 0 && cb.right <= document.documentElement.clientWidth});
					})()`
						var g struct {
							FootGap, AvStart, CardStart, CardBottom, AvTop int
							Shown, OnScreen                                bool
						}
						at(t, ctx, geom, &g)
						if g.FootGap > 40 || g.AvStart > 24 || g.Shown {
							t.Errorf("closed: the avatar is %dpx above the rail's foot and %dpx from its inline start, card shown %v; want it at the foot's inline start, closed", g.FootGap, g.AvStart, g.Shown)
						}
						if name := axName(ctx, t, p.trigger); name != "Grace Hopper" {
							t.Errorf("the avatar's accessible name is %q, want the person's name", name)
						}
						mustRun(t, ctx, chromedp.Click(p.trigger, chromedp.ByQuery), chromedp.WaitVisible(p.card+" [rst-shell-who]", chromedp.ByQuery),
							chromedp.Click("#rail-locale > summary", chromedp.ByQuery), chromedp.WaitVisible("#rail-locale [rst-dropdown-menu] a", chromedp.ByQuery))
						at(t, ctx, geom, &g)
						if !g.Shown || g.CardBottom > g.AvTop || !g.OnScreen {
							t.Errorf("open: card shown %v, its bottom at %d over an avatar whose top is %d, on screen %v; want a drop-up wholly in the window", g.Shown, g.CardBottom, g.AvTop, g.OnScreen)
						}
						if d := g.CardStart - g.AvStart; d < -2 || d > 2 {
							t.Errorf("the card starts %dpx from the rail's inline start and the avatar %dpx; want it anchored at the avatar's inline start", g.CardStart, g.AvStart)
						}
						requireCardItems(t, "the drop-up", readProfile(t, ctx, p).Items)
						clickAndStay(t, ctx, probeScripted(t, ctx, scripts, "#main-link", 0.5, 0.5, 0, 0), "",
							`!document.querySelector("[rst-shell-profile]").open`)
						if scripts {
							mustRun(t, ctx, chromedp.Click(p.trigger, chromedp.ByQuery), chromedp.WaitVisible("#acct-profile", chromedp.ByQuery),
								chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
							if after := readProfile(t, ctx, p); after.Open || !after.Focus {
								t.Errorf("Escape from a link in the card: open %v, focus on the avatar %v", after.Open, after.Focus)
							}
						}
					})
				}
			}
		}
	}
}

// TestThePersonNeverVanishesFromTheRail: an app with a profile and
// nothing else, no language and no account links, still shows the
// signed-in person's avatar, on a desktop at the rail's foot and on the
// phone index at the top; the card then holds the name and email. Only
// the shell's placeholder, an app with no profile at all and nothing to
// choose, leaves the foot out.
func TestThePersonNeverVanishesFromTheRail(t *testing.T) {
	src, _ := Layout("sidebar")
	noLocale := `{{define "locale"}}{{if false}}x{{end}}{{end}}`
	pages := map[string]string{
		"/person":      shellLayoutPage(t, src, "ltr", profileDefs("Harbour")[0], profileDefs("Harbour")[1], noLocale),
		"/person-idx":  shellLayoutPage(t, src, "ltr", profileDefs("Harbour")[0], profileDefs("Harbour")[1], noLocale, `{{define "view"}}index{{end}}`),
		"/placeholder": shellLayoutPage(t, src, "ltr", noLocale),
	}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	shown := `(() => { const s = document.querySelector("[rst-shell-profile] > summary"); return JSON.stringify({Avatar: !!s && s.checkVisibility()}); })()`
	for _, c := range []struct {
		path string
		w, h int64
		want bool
	}{{"/person", 1280, 720, true}, {"/person-idx", 390, 844, true}, {"/placeholder", 1280, 720, false}} {
		mustRun(t, ctx, chromedp.EmulateViewport(c.w, c.h), chromedp.Navigate(rig.Origin+c.path), chromedp.WaitReady("body"))
		var got struct{ Avatar bool }
		at(t, ctx, shown, &got)
		if got.Avatar != c.want {
			t.Errorf("%s at %d: avatar shown %v, want %v", c.path, c.w, got.Avatar, c.want)
		}
	}
}

// TestABlankBrandFallsBackToTheTitle: a brand drawn from data that has
// none renders only whitespace, and the phone index's heading is then
// the page's title rather than nothing.
func TestABlankBrandFallsBackToTheTitle(t *testing.T) {
	src, _ := Layout("sidebar")
	pages := map[string]string{"/": shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`,
		`{{define "title"}}Harbour Ledger{{end}}`, "{{define \"brand\"}}\n  {{if false}}<a href=\"/\">x</a>{{end}}\n{{end}}")}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body"))
	var v viewReading
	at(t, ctx, viewJS, &v)
	if v.Title != "Harbour Ledger" || v.H1s != 1 {
		t.Errorf("a whitespace brand: the index heading reads %q (%d h1s); want the title, Harbour Ledger", v.Title, v.H1s)
	}
}
