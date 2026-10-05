//go:build browser

package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// topbarPage renders the real topbar layout with a nav whose current
// item is Posts, an account menu, a language menu, and a link in the
// page far enough down that the card never covers it.
func topbarPage(t *testing.T, dir string) string {
	t.Helper()
	return topbarPageWith(t, dir, `<h1>Posts</h1><div class="spacer"></div><p><a id="main-link" href="/go/main">A link in the page</a></p><div class="spacer"></div>`)
}

// topbarPageWith is topbarPage with content of the caller's choosing.
func topbarPageWith(t *testing.T, dir, content string) string {
	t.Helper()
	src, ok := Layout("topbar")
	if !ok {
		t.Fatal("no topbar layout")
	}
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	for _, def := range []string{
		`{{define "dir"}}` + dir + `{{end}}`,
		`{{define "nav"}}<a id="nav-posts" href="/go/posts" aria-current="page">Posts</a><a id="nav-drafts" href="/go/drafts">Drafts</a><a id="nav-settings" href="/go/settings">Settings</a>{{end}}`,
		`{{define "account"}}<a id="acct-profile" href="/go/profile">Profile</a><a href="/go/signout">Sign out</a>{{end}}`,
		`{{define "locale"}}<details rst-dropdown rst-locale id="bar-locale" name="rst-menus"><summary>Language</summary><div rst-dropdown-menu><a href="/go/en" lang="en">English</a><a href="/go/ga" lang="ga">Gaeilge</a></div></details>{{end}}`,
		`{{define "content"}}` + content + `{{end}}`,
		`{{define "head"}}<style>.spacer { block-size: 450px; }</style>{{end}}`,
	} {
		template.Must(tmpl.Parse(def))
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
		t.Fatalf("rendering the topbar: %v", err)
	}
	return b.String()
}

// cardRig serves pages with the assets the layouts link, on a touch
// screen. scripts=false disables script execution in the engine, which
// is the no-JavaScript half of every claim below.
func cardRig(t *testing.T, pages map[string]string, scripts bool) (*harness.Rig, context.Context, context.CancelFunc) {
	t.Helper()
	pages["/go/"] = sizingDoc("landed", `<p id="landed">landed</p>`)
	rig := harness.New(t, func(string) http.Handler { return sizingMux(t, pages) }, harness.WithCoarsePointer())
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	if !scripts {
		if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
			cancel()
			t.Fatal(err)
		}
	}
	return rig, ctx, cancel
}

// cardJS is one reading of the bar: the card's box, main's box, what is
// open, where focus is, and the current item's marks.
const cardJS = `(() => {
  const q = s => document.querySelector(s);
  const menu = q("[rst-shell-menu], .rst-shell__menu"), tail = q("[rst-shell-tail], .rst-shell__tail");
  const acct = q("[rst-shell-account], .rst-shell__account"), main = q("main");
  const cur = q("#nav-posts"), cs = getComputedStyle(cur), t = tail.getBoundingClientRect(), m = main.getBoundingClientRect();
  const a = document.activeElement;
  return JSON.stringify({MenuOpen: menu.open, AcctOpen: acct.open, TailDisplay: getComputedStyle(tail).display,
    CardLeft: t.left, CardRight: t.right, CardTop: t.top,
    RootLeft: document.documentElement.getBoundingClientRect().left, RootRight: document.documentElement.getBoundingClientRect().right,
    MainBox: [m.left, m.top, m.width, m.height].map(Math.round).join(","),
    Focus: a ? (a.id || (a.closest("[rst-shell-menu], .rst-shell__menu") ? "menu-summary" : a.closest("[rst-shell-account], .rst-shell__account") ? "account-summary" : a.tagName)) : "none",
    FocusShown: !!a && a.getClientRects().length > 0,
    CurBg: cs.backgroundColor, CurShadow: cs.boxShadow, CurStart: cs.borderInlineStartWidth, CurEnd: cs.borderBlockEndWidth,
    AccentSoft: getComputedStyle(document.documentElement).getPropertyValue("--rst-accent-soft").trim(),
    AcctPanel: getComputedStyle(acct.querySelector("[rst-dropdown-menu], .rst-dropdown__menu")).position,
    Path: location.pathname});
})()`

type cardReading struct {
	MenuOpen, AcctOpen, FocusShown     bool
	TailDisplay, MainBox, Focus, Path  string
	CardLeft, CardRight, CardTop       float64
	RootLeft, RootRight                float64
	CurBg, CurShadow, CurStart, CurEnd string
	AccentSoft, AcctPanel              string
}

func readCard(t *testing.T, ctx context.Context) cardReading {
	t.Helper()
	var c cardReading
	at(t, ctx, cardJS, &c)
	return c
}

// resolvedColor turns a colour custom property into the colour a
// computed style reports, so the current item's fill can be compared.
func resolvedColor(t *testing.T, ctx context.Context, prop string) string {
	t.Helper()
	var s string
	mustRun(t, ctx, chromedp.Evaluate(`(() => { const e = document.createElement("span"); e.style.backgroundColor = "var(`+prop+`)"; document.body.appendChild(e); const c = getComputedStyle(e).backgroundColor; e.remove(); return c; })()`, &s))
	if s == "" || s == "rgba(0, 0, 0, 0)" {
		t.Fatalf("%s resolved to %q; a colour compared with it would be compared with nothing", prop, s)
	}
	return s
}

// TestTheTopbarMenuIsACardOverThePage checks the topbar's card with
// scripts off and on, LTR and RTL, at 390 with a coarse pointer.
func TestTheTopbarMenuIsACardOverThePage(t *testing.T) {
	for _, dir := range []string{"ltr", "rtl"} {
		for _, scripts := range []bool{false, true} {
			name := dir + map[bool]string{false: ", no script", true: ", scripts on"}[scripts]
			t.Run(name, func(t *testing.T) {
				rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, dir)}, scripts)
				defer cancel()
				mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
					chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery))
				requirePointer(t, ctx, true)
				closed := readCard(t, ctx)
				mustRun(t, ctx, chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery))
				open := readCard(t, ctx)
				if !open.MenuOpen {
					t.Fatal("the Menu did not open")
				}
				if open.MainBox != closed.MainBox {
					t.Errorf("opening the card moved the page: main %s -> %s", closed.MainBox, open.MainBox)
				}
				// Against the root box, not clientWidth: this rig's touch
				// Chromium reports clientWidth as the whole 390 while
				// tokens.css's scrollbar-gutter: stable still reserves a
				// 15px classic gutter, so the page's own inline end is
				// 375 and clientWidth would count the gutter as gap.
				gap := open.RootRight - open.CardRight
				if dir == "rtl" {
					gap = open.CardLeft - open.RootLeft
				}
				if gap < 0 || gap > 12+1 {
					t.Errorf("the card's inline-end edge is %.1fpx from the viewport's, want within 0.75rem + 1px", gap)
				}
				if soft := resolvedColor(t, ctx, "--rst-accent-soft"); open.CurBg != soft || open.CurShadow != "none" || open.CurStart != "0px" || open.CurEnd != "0px" {
					t.Errorf("the current item is bg %s shadow %s border-start %s border-end %s; want %s, none, 0, 0", open.CurBg, open.CurShadow, open.CurStart, open.CurEnd, soft)
				}
				// The premise of the no-script leg: rastrillo.js would close
				// the card on Escape, so a card that survives one proves no
				// script is running, and the close below is the CSS layer's.
				if !scripts {
					mustRun(t, ctx, chromedp.KeyEvent(kb.Escape))
					if !readCard(t, ctx).MenuOpen {
						t.Fatal("Escape closed the card with scripts disabled; something is still running script, and this leg would not be the no-JavaScript case")
					}
				}
				// An outside tap over a link in the page closes the card
				// and does not follow the link. A page with no Menu also
				// ends the wait: that is the link followed, which
				// clickAndStay then reports as the navigation it was.
				clickAndStay(t, ctx, probeScripted(t, ctx, scripts, "#main-link", 0.5, 0.5, 0, 0), "", `(() => { const m = document.querySelector("[rst-shell-menu]"); return !m || !m.open; })()`)
			})
		}
	}
}

// TestEscapeClosesTheWholeCard: from a plain nav link in the card, and
// from inside the account menu in it, one Escape closes everything and
// focus lands on the Menu summary; opening the account menu keeps the
// card open. Both spellings of the whole topbar.
func TestEscapeClosesTheWholeCard(t *testing.T) {
	for _, spelling := range []string{"attribute", "class"} {
		t.Run(spelling, func(t *testing.T) {
			page := topbarPage(t, "ltr")
			menu, acct := "[rst-shell-menu] > summary", "[rst-shell-account] > summary"
			if spelling == "class" {
				page, menu, acct = classSpelling(t, page), ".rst-shell__menu > summary", ".rst-shell__account > summary"
				if !strings.Contains(page, "rst-shell__tail") {
					t.Fatal("the class spelling of the topbar has no rst-shell__tail; the leg would test the attribute spelling twice")
				}
			}
			rig, ctx, cancel := cardRig(t, map[string]string{"/": page}, true)
			defer cancel()
			open := func() {
				mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
					chromedp.WaitVisible(menu, chromedp.ByQuery), chromedp.Click(menu, chromedp.ByQuery))
			}
			open()
			mustRun(t, ctx, chromedp.Focus("#nav-drafts", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
			if c := readCard(t, ctx); c.MenuOpen || c.Focus != "menu-summary" {
				t.Errorf("Escape from a nav link: open=%v focus=%s, want closed and the Menu summary", c.MenuOpen, c.Focus)
			}
			open()
			mustRun(t, ctx, chromedp.Click(acct, chromedp.ByQuery))
			if c := readCard(t, ctx); !c.MenuOpen || !c.AcctOpen {
				t.Errorf("opening the account menu inside the card: card open=%v, account open=%v; want both", c.MenuOpen, c.AcctOpen)
			}
			mustRun(t, ctx, chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
			if c := readCard(t, ctx); c.MenuOpen || c.AcctOpen || c.Focus != "menu-summary" {
				t.Errorf("Escape from inside the account menu: card %v account %v focus %s; want both closed by one Escape and focus on the Menu summary", c.MenuOpen, c.AcctOpen, c.Focus)
			}
		})
	}
}

// TestTheCardSurvivesAResize: open the card at 390 and widen to 1280,
// and the bar is today's (the tail inline, the current item underlined)
// and a click on a link in the page follows it while the <details> is
// still open (no leftover layer); open the account menu in the card,
// widen, and it is today's positioned panel, and Escape focuses its
// visible summary; narrow again and the card is closed, because that
// Escape closed every menu, the hidden one included.
func TestTheCardSurvivesAResize(t *testing.T) {
	rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, "ltr")}, true)
	defer cancel()
	load := func() {
		mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
			chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery),
			chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery))
	}
	load()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 900))
	wide := readCard(t, ctx)
	if !wide.MenuOpen {
		t.Fatal("the <details> closed on the resize; the leftover-layer case this leg is for has not arisen")
	}
	if wide.TailDisplay != "contents" {
		t.Errorf("widened with the card open: the tail is display %s, want today's contents", wide.TailDisplay)
	}
	if wide.CurEnd != "2px" {
		t.Errorf("widened: the current item's underline is %s, want today's 2px", wide.CurEnd)
	}
	// A leftover layer would swallow this click; landing is the proof
	// there is none.
	clickAndLand(t, ctx, probe(t, ctx, "#main-link", 0.5, 0.5, 0, 0), "/go/main")

	load()
	mustRun(t, ctx, chromedp.Click("[rst-shell-account] > summary", chromedp.ByQuery), chromedp.EmulateViewport(1280, 900))
	if c := readCard(t, ctx); !c.AcctOpen || c.AcctPanel == "static" {
		t.Errorf("widened with the account menu open: open %v, panel position %s; want today's positioned panel", c.AcctOpen, c.AcctPanel)
	}
	mustRun(t, ctx, chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
	if c := readCard(t, ctx); c.Focus != "account-summary" || !c.FocusShown {
		t.Errorf("Escape after widening focused %q (shown %v); want the account summary, which is visible", c.Focus, c.FocusShown)
	}
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844))
	if c := readCard(t, ctx); c.MenuOpen {
		t.Error("narrowed again after Escape: the card is open, but Escape closes every menu, the hidden one included")
	}
}

// TestTheCardsControlsAreTaps: the Menu summary, the nav rows and the
// account and language summaries in the open card, at 44px.
func TestTheCardsControlsAreTaps(t *testing.T) {
	rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, "ltr")}, true)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery), chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery))
	requirePointer(t, ctx, true)
	// The Menu and the card, not the whole bar: while the card is open
	// the brand is under the outside-tap layer on purpose, so a tap there
	// closes the card instead of following it.
	got := readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify([...measure(document.querySelector("[rst-shell-menu]")), ...measure(document.querySelector("[rst-shell-tail]"))]); })()`)
	if len(got) != 6 {
		t.Fatalf("measured %d controls in the open card, want exactly 6: the Menu summary, three nav rows, the account and language summaries", len(got))
	}
	assertTargets(t, "390 touch, the open card", got)
}

// TestAModalCoversTheTopbarOnAPhone: the bar is lifted above in-page
// menu panels only while its card is open. Lifted for good, it painted
// over a modal's scrim (fixed, z-index 10, inside main) on a phone: the
// bar stayed undimmed and live, and its Menu could open a card over the
// modal. Read at the brand and at the Menu summary, the two things a tap
// on a phone's bar reaches. The sidebar's and console's content pages
// have the same exposure through their sticky back strip: above the
// scrim, Back stayed live under an open modal.
func TestAModalCoversTheTopbarOnAPhone(t *testing.T) {
	hits := func(t *testing.T, ctx context.Context, sels ...string) {
		t.Helper()
		for _, sel := range sels {
			var hit string
			mustRun(t, ctx, chromedp.Evaluate(`(() => { const r = document.querySelector(`+"`"+sel+"`"+`).getBoundingClientRect();
			  const h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
			  return !h ? "nothing" : h.closest("[rst-modal-overlay]") ? "overlay" : h.tagName + (h.getAttributeNames().filter(n => n.startsWith("rst-")).map(n => "[" + n + "]").join("")); })()`, &hit))
			if hit != "overlay" {
				t.Errorf("a tap on %s with a modal open lands on %s, want the modal's overlay: it paints above the scrim", sel, hit)
			}
		}
	}
	t.Run("topbar", func(t *testing.T) {
		rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPageWith(t, "ltr", Styleguide()["modal"])}, true)
		defer cancel()
		mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
			chromedp.WaitVisible("[rst-modal-overlay]", chromedp.ByQuery))
		requirePointer(t, ctx, true)
		hits(t, ctx, "[rst-shell-brand]", "[rst-shell-menu] > summary")
	})
	for _, name := range []string{"sidebar", "console"} {
		t.Run(name+" content page", func(t *testing.T) {
			src, ok := Layout(name)
			if !ok {
				t.Fatalf("no %s layout", name)
			}
			pages := map[string]string{"/": shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-invoices{{end}}`,
				`{{define "content"}}`+Styleguide()["modal"]+`{{end}}`)}
			rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
			ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
				chromedp.WaitVisible("[rst-modal-overlay]", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-back] a", chromedp.ByQuery))
			requirePointer(t, ctx, true)
			hits(t, ctx, "[rst-shell-back] a")
		})
	}
}

// TestAClickOnTheDocumentItselfDoesNotThrow: a click whose target is the
// document (a script's synthetic dispatch) has no closest(), and light
// dismiss runs in a capture listener on every click, so a throw there
// is an error on every such dispatch and leaves any open menu open.
func TestAClickOnTheDocumentItselfDoesNotThrow(t *testing.T) {
	rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, "ltr")}, true)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery))
	// The shim is deferred; the drive starts once it has bound, which
	// the account menu closing on an outside click shows.
	settleUntil(t, ctx, `(() => { const a = document.querySelector("[rst-shell-account]"); a.open = true; document.body.click(); return !a.open; })()`)
	var got string
	mustRun(t, ctx, chromedp.Evaluate(`(() => { let err = "none"; addEventListener("error", e => { err = e.message; });
	  const a = document.querySelector("[rst-shell-account]"); a.open = true;
	  document.dispatchEvent(new MouseEvent("click", {bubbles: true}));
	  return err + (a.open ? " (menu left open)" : ""); })()`, &got))
	if got != "none" {
		t.Errorf("a click dispatched on the document: %s; want no error and the open menu closed", got)
	}
}

// TestTheCardFitsTheSmallestPhone: at 320, with this rig's classic
// scrollbar gutter reserved, the card stays inside the page at its
// inline start too. Sized from the viewport (100vw) it counted the
// gutter as room and overhung the start edge by 3px.
func TestTheCardFitsTheSmallestPhone(t *testing.T) {
	for _, dir := range []string{"ltr", "rtl"} {
		t.Run(dir, func(t *testing.T) {
			rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, dir)}, true)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(320, 640), chromedp.Navigate(rig.Origin+"/"),
				chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery),
				chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery))
			c := readCard(t, ctx)
			if !c.MenuOpen {
				t.Fatal("the Menu did not open")
			}
			if c.CardLeft < c.RootLeft-0.5 || c.CardRight > c.RootRight+0.5 {
				t.Errorf("at 320 the card spans %.1f to %.1f and the page %.1f to %.1f; it overhangs", c.CardLeft, c.CardRight, c.RootLeft, c.RootRight)
			}
		})
	}
}

// TestTheConsoleBarIsTheSameCard: the console's tail folds into the same
// card as the topbar's: the page does not move, the card sits at the
// inline end, an outside tap closes it with and without scripts, and
// one Escape from inside its account menu closes both.
func TestTheConsoleBarIsTheSameCard(t *testing.T) {
	src, _ := Layout("console")
	for _, c := range []struct {
		dir     string
		scripts bool
	}{{"ltr", false}, {"ltr", true}, {"rtl", false}, {"rtl", true}} {
		scripts := c.scripts
		t.Run(fmt.Sprintf("%s scripts %v", c.dir, scripts), func(t *testing.T) {
			page := shellLayoutPage(t, src, c.dir, `{{define "account"}}<a id="acct-profile" href="/go/profile">Profile</a>{{end}}`,
				`{{define "content"}}<h1>Invoices</h1><div style="block-size: 450px"></div><p><a id="main-link" href="/go/main">A link in the page</a></p>{{end}}`)
			pages := map[string]string{"/": page}
			rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
			ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
			defer cancel()
			acts := []chromedp.Action{chromedp.EmulateViewport(390, 844)}
			if !scripts {
				acts = append(acts, emulation.SetScriptExecutionDisabled(true))
			}
			acts = append(acts, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery))
			mustRun(t, ctx, acts...)
			var before, open struct {
				Main, Basis, Inner string
				Gap                float64
			}
			// Gap is the card's distance from the page's inline end, which is
			// the left edge in RTL, measured against the root element's box:
			// tokens.css reserves a stable scrollbar gutter, so the viewport's
			// edge is not the page's. Basis and Inner are the card's own box
			// rules: the console's base rule for its open tail (flex-basis 100%
			// and a gap) comes earlier in the file than the card's, at the same
			// weight, so these prove the card's rules won.
			read := `(() => { const m = document.querySelector("main").getBoundingClientRect(), tail = document.querySelector("[rst-shell-tail]"), t = tail.getBoundingClientRect(), cs = getComputedStyle(tail);
			  const rtl = document.documentElement.dir === "rtl", h = document.documentElement.getBoundingClientRect();
			  return JSON.stringify({Main: [m.left, m.top, m.width].map(Math.round).join(","), Gap: rtl ? t.left - h.left : h.right - t.right, Basis: cs.flexBasis, Inner: cs.rowGap}); })()`
			at(t, ctx, read, &before)
			mustRun(t, ctx, chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-tail] [rst-shell-account] > summary", chromedp.ByQuery))
			at(t, ctx, read, &open)
			if open.Main != before.Main || open.Gap < 0 || open.Gap > 13 {
				t.Errorf("main %s -> %s, card %.1fpx from the edge; want a card over a page that did not move", before.Main, open.Main, open.Gap)
			}
			if open.Basis != "auto" || open.Inner != "0px" {
				t.Errorf("the console's open tail has flex-basis %s and gap %s; the card's auto and 0 lost to the console's own rule", open.Basis, open.Inner)
			}
			clickAndStay(t, ctx, probeScripted(t, ctx, scripts, "#main-link", 0.5, 0.5, 0, 0), "", `!document.querySelector("[rst-shell-menu]").open`)
			if scripts {
				mustRun(t, ctx, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery),
					chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-tail] [rst-shell-account] > summary", chromedp.ByQuery),
					chromedp.Click("[rst-shell-account] > summary", chromedp.ByQuery), chromedp.WaitVisible("#acct-profile", chromedp.ByQuery),
					chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
				var s struct {
					Menu, Acct bool
					Focus      string
				}
				at(t, ctx, `JSON.stringify({Menu: document.querySelector("[rst-shell-menu]").open, Acct: document.querySelector("[rst-shell-account]").open, Focus: document.activeElement.closest("[rst-shell-menu]") ? "menu" : document.activeElement.tagName})`, &s)
				if s.Menu || s.Acct || s.Focus != "menu" {
					t.Errorf("Escape from the account menu in the console's card: %+v; want both closed and focus on the Menu summary", s)
				}
			}
		})
	}
}
