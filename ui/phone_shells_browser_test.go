//go:build browser

package ui

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
)

// The laid-out half of phone_shells_test.go: the three shells with
// chrome, rendered through ui.Pages with an app's blocks, on a phone.
// The one-row reading is Fiesta's (check/one_row_browser_test.go),
// ported to read Rastrillo's layouts rather than Fiesta's examples.

// phoneShellDefines are an app's blocks on every page here: a brand,
// a nav with ids for the up link's fragment, a person and two account
// links. view and up are the case's.
func phoneShellDefines(shell, view string) string {
	up := "/#nav-posts"
	if shell == "topbar" {
		up = "/menu#nav-posts"
	}
	return `{{define "title"}}Posts{{end}}` +
		`{{define "brand"}}` +
		`<a rst-shell-brand href="/">Party planner</a>{{end}}` +
		`{{define "nav"}}` +
		`<a id="nav-posts" href="/posts" aria-current="page">Posts</a>` +
		`<a id="nav-comments" href="/comments">Comments</a>` +
		`<a id="nav-settings" href="/settings">Settings</a>{{end}}` +
		`{{define "profile"}}` +
		`<span rst-person-av aria-hidden="true">G</span>` +
		`<span rst-person-name>Grace Hopper</span>` +
		`<span rst-person-email>grace@example.com</span>{{end}}` +
		`{{define "account"}}<a id="acct-profile" href="#">Profile</a>` +
		`<a href="#">Sign out</a>{{end}}` +
		`{{define "view"}}` + view + `{{end}}` +
		`{{define "up"}}` + up + `{{end}}`
}

// phoneCase is one phone a shell is read on.
type phoneCase struct {
	shell, view, dir string
	width            int64
}

func (phone phoneCase) String() string {
	return fmt.Sprintf("%s %s %s %d",
		phone.shell, phone.view, phone.dir, phone.width)
}

func (phone phoneCase) path() string {
	return "/" + phone.dir + "/" + phone.shell + "-" + phone.view
}

func (phone phoneCase) root() string {
	return "[rst-shell-" + phone.shell + "]"
}

func (phone phoneCase) index() bool { return phone.view == "index" }

func (phone phoneCase) identity() string {
	if phone.dir == "rtl" {
		return "ar"
	}
	return "en"
}

var phoneCases = func() []phoneCase {
	var out []phoneCase
	for _, shell := range phoneShells {
		for _, view := range []string{"index", "page"} {
			for _, dir := range []string{"ltr", "rtl"} {
				for _, width := range []int64{390, 320} {
					out = append(out, phoneCase{shell, view, dir, width})
				}
			}
		}
	}
	return out
}()

// phoneShellRig serves every phoneCase's page at its path, on a touch
// screen.
func phoneShellRig(
	t *testing.T,
) (*harness.Rig, context.Context, context.CancelFunc) {
	t.Helper()
	pages := map[string]string{}
	data := map[string]any{
		"Locales": threeLocales, "Return": "/posts", "Scheme": "dark",
	}
	for _, phone := range phoneCases {
		pages[phone.path()] = phoneShellPage(t,
			shippedLayout(t, phone.shell), phone.identity(),
			phoneShellDefines(phone.shell, phone.view), data)
	}
	serve := func(string) http.Handler { return shellAssets(t, pages) }
	rig := harness.New(t, serve, harness.WithCoarsePointer())
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	return rig, ctx, cancel
}

func openPhoneCase(
	t *testing.T, ctx context.Context, origin string, phone phoneCase,
) {
	t.Helper()
	mustRun(t, ctx, chromedp.EmulateViewport(phone.width, 844),
		chromedp.Navigate(origin+phone.path()), chromedp.WaitReady("body"))
	requirePointer(t, ctx, true)
	settled(t, ctx)
}

// cardShape is the account card and the up link as the DOM has them.
type cardShape struct {
	Ups, Old, Cards, LanguageIDs int
	SchemeInCard, LanguageRow    bool
	CardOrder                    string
}

const cardShapeJS = `((root) => {
  const r = document.querySelector(root);
  const cards = r.querySelectorAll(
    "[rst-shell-account], [rst-shell-profile]");
  const card = cards[0];
  const panel = card &&
    card.querySelector(":scope > [rst-dropdown-menu]");
  const button = panel &&
    panel.querySelector('button[popovertarget="rst-language"]');
  const fallback = button && button.nextElementSibling;
  const sheet = fallback && fallback.nextElementSibling;
  const name = el => el.hasAttribute("rst-shell-who") ? "who"
    : el.querySelector('[rst-scheme="icons"]') ? "appearance"
    : el.querySelector("[popovertarget]") ? "language"
    : el.tagName.toLowerCase();
  const old = "[rst-shell-menu], [rst-shell-back], " +
    "[rst-shell-tail], [rst-shell-chrome]";
  return JSON.stringify({
    Ups: r.querySelectorAll("[rst-shell-up]").length,
    Old: r.querySelectorAll(old).length,
    Cards: cards.length,
    LanguageIDs: document.querySelectorAll("#rst-language").length,
    SchemeInCard: !!(panel &&
      panel.querySelector('[rst-scheme="icons"]')),
    LanguageRow: !!(button &&
      button.parentElement.matches("[rst-menu-row]") &&
      fallback && fallback.matches("details[rst-dropdown]") &&
      sheet && sheet.matches("[rst-locale][popover]#rst-language")),
    CardOrder: panel ? [...panel.children].map(name).join(" ") : "",
  });
})(%q)`

// TestPhoneShellsWriteTheCardAndOneUpLink: in the DOM each shell's
// root holds one up link and none of the old phone chrome, and one
// account card whose panel reads the person, Appearance (the icon
// control), Language (button, fallback and sheet, adjacent), then the
// account's links: Fiesta's card, in Fiesta's order.
func TestPhoneShellsWriteTheCardAndOneUpLink(t *testing.T) {
	rig, ctx, cancel := phoneShellRig(t)
	defer cancel()
	for _, phone := range phoneCases {
		if phone.width != 390 {
			continue
		}
		openPhoneCase(t, ctx, rig.Origin, phone)
		var got cardShape
		at(t, ctx, fmt.Sprintf(cardShapeJS, phone.root()), &got)
		if got.Ups != 1 || got.Old != 0 || got.Cards != 1 ||
			got.LanguageIDs != 1 {
			t.Errorf("%s: %d up links, %d old chrome elements, "+
				"%d account cards, %d #rst-language; want 1, 0, 1, 1",
				phone, got.Ups, got.Old, got.Cards, got.LanguageIDs)
		}
		if !got.SchemeInCard || !got.LanguageRow {
			t.Errorf("%s: the card holds the icon control %v and the "+
				"Language row %v; want both",
				phone, got.SchemeInCard, got.LanguageRow)
		}
		if want := "who appearance language a a"; got.CardOrder != want {
			t.Errorf("%s: the card's panel reads %q; want %q",
				phone, got.CardOrder, want)
		}
	}
}

// rowBox is one element's border box as the viewport sees it.
type rowBox struct {
	Shown                                   bool
	Left, Right, Top, Bottom, Width, Height float64
}

// oneRowReading is a shell's chrome on a phone, read by oneRowJS.
type oneRowReading struct {
	Dir       string
	BarHeight float64
	Rows      int
	Bar       rowBox
	Up        rowBox
	Avatar    rowBox
	MenuText  bool
	Overflow  float64
}

// oneRowJS is Fiesta's reading (check/one_row_browser_test.go): the
// chrome is everything in the root outside main, the nav and any open
// menu's panel, and boxes that overlap top to bottom are one row.
// MenuText is visible text in the chrome, outside the up link, that is
// the page's own word for Menu.
const oneRowJS = `((root, menu) => {
  const r = document.querySelector(root);
  if (!r) return JSON.stringify({Rows: -1});
  const de = document.documentElement;
  const box = el => {
    if (!el || !el.checkVisibility()) return {Shown: false};
    const b = el.getBoundingClientRect();
    if (b.width <= 0 || b.height <= 0) return {Shown: false};
    return {Shown: true, Left: b.left, Right: b.right, Top: b.top,
      Bottom: b.bottom, Width: b.width, Height: b.height};
  };
  const content = el => !!el.closest(
    "main, [rst-shell-main], [rst-shell-nav], [rst-dropdown-menu]");
  const bar = r.querySelector("[rst-shell-bar]");
  const up = r.querySelector("[rst-shell-up]");
  const avatar = [...r.querySelectorAll("[rst-shell-avatar]")]
    .filter(a => a.checkVisibility()).map(a => a.closest("summary"))
    .find(Boolean);
  const below = [r.querySelector("main, [rst-shell-main]"),
    r.querySelector("[rst-shell-nav]")].map(box).find(b => b.Shown);
  const limit = below ? below.Top + 1 : Infinity;
  const chrome = [...r.querySelectorAll("[rst-shell-bar], " +
      "[rst-shell-back], [rst-shell-up], [rst-shell-title], " +
      "[rst-shell-brand], summary")]
    .filter(el => !content(el)).map(box)
    .filter(b => b.Shown && b.Bottom <= limit)
    .sort((a, b) => a.Top - b.Top);
  let rows = 0, end = -Infinity;
  for (const b of chrome) {
    if (b.Top >= end - 1) { rows++; end = b.Bottom; }
    else { end = Math.max(end, b.Bottom); }
  }
  let menuText = false;
  const walk = document.createTreeWalker(r, NodeFilter.SHOW_TEXT);
  for (let n; (n = walk.nextNode());) {
    const el = n.parentElement;
    if (n.nodeValue.trim() !== menu || content(el) ||
      (up && up.contains(el)) ||
      getComputedStyle(el).visibility !== "visible") continue;
    const range = document.createRange();
    range.selectNodeContents(n);
    if ([...range.getClientRects()]
      .some(q => q.width > 0 && q.height > 0)) menuText = true;
  }
  const barBox = box(bar);
  return JSON.stringify({
    Dir: getComputedStyle(de).direction,
    BarHeight: barBox.Shown ? barBox.Height : 0, Rows: rows,
    Bar: barBox, Up: box(up), Avatar: box(avatar), MenuText: menuText,
    Overflow: de.scrollWidth - de.clientWidth,
  });
})(%q, %q)`

// TestThePhoneShowsOneHeaderRow: on each shell's index and content
// page, LTR and RTL, at 390 and 320, exactly one header row sits above
// the page, the 56px strip, and no visible Menu label is in it. On a
// content page the up link is a 44px target at the row's inline start
// and the avatar one at its inline end. 0.37's console drew two rows
// on a content page (the bar, then the back strip) and a Menu in the
// bar; its topbar had a Menu that dropped the nav.
func TestThePhoneShowsOneHeaderRow(t *testing.T) {
	rig, ctx, cancel := phoneShellRig(t)
	defer cancel()
	for _, phone := range phoneCases {
		openPhoneCase(t, ctx, rig.Origin, phone)
		catalog := rastrillo.BaseCatalogs()[phone.identity()]
		menu := catalog["rastrillo.ui.shell_menu"]
		var row oneRowReading
		at(t, ctx, fmt.Sprintf(oneRowJS, phone.root(), menu), &row)
		if row.Rows != 1 || math.Abs(row.BarHeight-56) > 0.5 {
			t.Errorf("%s: %d header rows, the bar %.1fpx tall; "+
				"want one row, 56px", phone, row.Rows, row.BarHeight)
		}
		if row.MenuText {
			t.Errorf("%s: the chrome shows the word %q outside the up link",
				phone, menu)
		}
		if row.Overflow > 0 {
			t.Errorf("%s: the page scrolls %.0fpx sideways",
				phone, row.Overflow)
		}
		if row.Dir != phone.dir {
			t.Fatalf("%s: the page reads %s", phone, row.Dir)
		}
		fromStart := row.Up.Left - row.Bar.Left
		fromEnd := row.Bar.Right - row.Avatar.Right
		if phone.dir == "rtl" {
			fromStart = row.Bar.Right - row.Up.Right
			fromEnd = row.Avatar.Left - row.Bar.Left
		}
		if !row.Avatar.Shown || fromEnd > 8 || row.Avatar.Width < 44 ||
			row.Avatar.Height < 44 {
			t.Errorf("%s: the avatar %+v in the row %+v; "+
				"want a 44px target at the inline end",
				phone, row.Avatar, row.Bar)
		}
		if phone.index() {
			if row.Up.Shown {
				t.Errorf("%s: the index shows its up link %+v; "+
					"it is the top of the app", phone, row.Up)
			}
			continue
		}
		if !row.Up.Shown || fromStart > 8 || row.Up.Height < 44 {
			t.Errorf("%s: the up link %+v in the row %+v; "+
				"want a 44px target at the inline start",
				phone, row.Up, row.Bar)
		}
	}
}

// sheetState is the account card and its Language sheet.
type sheetState struct{ Card, Sheet bool }

const sheetStateJS = `JSON.stringify({
  Card: document.querySelector(
    "[rst-shell-account], [rst-shell-profile]").open,
  Sheet: document.querySelector("#rst-language").matches(":popover-open"),
})`

// TestPhoneShellsLanguageSheetTakesEscapeAlone: on a phone the card's
// Language row opens its sheet over the page with the card still open
// underneath, and Escape shuts the sheet alone. fiesta.js, which every
// Rastrillo page loads, leaves Escape to the browser while Fiesta's
// language sheet is open; before that guard Escape shut the card too.
func TestPhoneShellsLanguageSheetTakesEscapeAlone(t *testing.T) {
	rig, ctx, cancel := phoneShellRig(t)
	defer cancel()
	const avatar = `:is([rst-shell-account], [rst-shell-profile]) > summary`
	const language = `button[popovertarget="rst-language"]`
	for _, phone := range phoneCases {
		if phone.width != 390 || phone.dir != "ltr" || phone.view != "page" {
			continue
		}
		openPhoneCase(t, ctx, rig.Origin, phone)
		settleUntil(t, ctx, `window.fiestaShim === true`)
		mustRun(t, ctx, chromedp.Click(chromedp.CSS(avatar)),
			chromedp.WaitVisible(chromedp.CSS(language)),
			chromedp.Click(chromedp.CSS(language)))
		settleUntil(t, ctx, `document.querySelector("#rst-language")`+
			`.matches(":popover-open")`)
		var open sheetState
		at(t, ctx, sheetStateJS, &open)
		if !open.Card || !open.Sheet {
			t.Errorf("%s: after the Language tap the card is open %v "+
				"and the sheet %v; want both", phone, open.Card, open.Sheet)
			continue
		}
		mustRun(t, ctx, chromedp.KeyEvent(kb.Escape))
		var after sheetState
		at(t, ctx, sheetStateJS, &after)
		if !after.Card || after.Sheet {
			t.Errorf("%s: after Escape the card is open %v and the sheet "+
				"%v; want the sheet shut and the card still open",
				phone, after.Card, after.Sheet)
		}
	}
}

// appPopoverJS adds an app's manual popover to a parent, opens it, puts
// focus on the card's first link, and says whether the popover opened.
const appPopoverJS = `(() => {
  const popover = document.createElement("div");
  popover.setAttribute("popover", "manual");
  popover.textContent = "An app's popover";
  %s.append(popover);
  popover.showPopover();
  document.querySelector("#acct-profile").focus();
  return popover.matches(":popover-open");
})()`

// TestEscapeStillClosesTheCardBesideAnAppsPopover mirrors Fiesta's
// TestEscapeStillClosesAMenuBesideAnAppsPopover on a Rastrillo page:
// fiesta.js's guard is keyed on Fiesta's own language sheet, so an
// app's popover, outside the open card or inside its panel, does not
// stop Escape closing the card and handing focus to its summary.
func TestEscapeStillClosesTheCardBesideAnAppsPopover(t *testing.T) {
	rig, ctx, cancel := phoneShellRig(t)
	defer cancel()
	const summary = `[rst-shell-account] > summary`
	page := phoneCase{"topbar", "page", "ltr", 390}
	for _, leg := range []struct{ name, parent string }{
		{"outside the card", "document.body"},
		{"inside the card's panel", `document.querySelector(` +
			`"[rst-shell-account] > [rst-dropdown-menu]")`},
	} {
		openPhoneCase(t, ctx, rig.Origin, page)
		mustRun(t, ctx, chromedp.EmulateViewport(1280, 800))
		settleUntil(t, ctx, `window.fiestaShim === true`)
		mustRun(t, ctx, chromedp.Click(chromedp.CSS(summary)),
			chromedp.WaitVisible(chromedp.CSS(`#acct-profile`)))
		var shown bool
		popover := fmt.Sprintf(appPopoverJS, leg.parent)
		mustRun(t, ctx,
			harness.Into(&shown, chromedp.Evaluate[bool](popover)))
		if !shown {
			t.Fatalf("%s: the app's popover never opened; "+
				"the leg proves nothing", leg.name)
		}
		mustRun(t, ctx, chromedp.KeyEvent(kb.Escape))
		var closed, focused bool
		mustRun(t, ctx,
			harness.Into(&closed, chromedp.Evaluate[bool](
				`!document.querySelector("[rst-shell-account]").open`)),
			harness.Into(&focused, chromedp.Evaluate[bool](
				`document.activeElement === `+
					`document.querySelector("`+summary+`")`)))
		if !closed || !focused {
			t.Errorf("%s: with the app's popover open, Escape left the "+
				"card closed %v and focus on its summary %v; want both",
				leg.name, closed, focused)
		}
	}
}
