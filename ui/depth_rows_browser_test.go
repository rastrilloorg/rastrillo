//go:build browser

package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The rows fixture's real ids (rows_browser_test.go): #row-a holds
// #link-a and the kebab #kebab-a inside its <details>; #row-n in #inert
// is the row with nothing to open; #panel-a is row A's open menu.
const (
	rowA, linkA, rowInert = "#row-a", "#link-a", "#row-n"
)

// TestARowLiftsOnHoverAndSinksOnPress (spec §3, C7, A1), with a mouse:
// an actionable row's hover changes its background to the hover tint
// with only inset shadows and no stacking context; pressed, or with
// its overlay link :active, it takes the press tint; an inert row
// changes nothing; a menu item takes the same tint; the media tile and
// the list-row-action row do too (the depth fixture).
func TestARowLiftsOnHoverAndSinksOnPress(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 844))
	home(t, ctx, rig.Origin)
	still(t, ctx)
	rest := computed(t, ctx, rowA, "background-color")
	forceState(t, ctx, rowA, "hover")
	if got := computed(t, ctx, rowA, "background-color"); got != "rgb(245, 247, 253)" {
		t.Errorf("hovered row background %s, want the hover tint rgb(245, 247, 253)", got)
	}
	if sh := computed(t, ctx, rowA, "box-shadow"); !strings.Contains(sh, "inset") || strings.Count(sh, "inset") != strings.Count(sh, "rgb") {
		t.Errorf("hovered row box-shadow %q, want inset layers only (C7)", sh)
	}
	if pos, z := computed(t, ctx, rowA, "position"), computed(t, ctx, rowA, "z-index"); pos != "relative" || z != "auto" {
		t.Errorf("hovered row position %s z-index %s; the lift must add no stacking context (C7)", pos, z)
	}
	forceState(t, ctx, rowA)
	forceState(t, ctx, linkA, "active")
	if got := computed(t, ctx, rowA, "background-color"); got != "rgb(236, 241, 252)" {
		t.Errorf("row with its link :active has background %s, want the press tint rgb(236, 241, 252) (A1)", got)
	}
	forceState(t, ctx, linkA)
	forceState(t, ctx, rowInert, "hover", "active")
	if got := computed(t, ctx, rowInert, "background-color"); got != rest {
		t.Errorf("an inert row changed on hover/press: %s -> %s", rest, got)
	}
	forceState(t, ctx, rowInert)
	mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("kebab-a").click(), true`, nil))
	forceState(t, ctx, "#panel-a a", "hover")
	if got := computed(t, ctx, "#panel-a a", "background-color"); got != "rgb(245, 247, 253)" {
		t.Errorf("hovered menu item background %s, want the hover tint", got)
	}
	forceState(t, ctx, "#panel-a a")
}

// TestMenusTilesAndOptionsTakeTheTint: the depth fixture's menu link,
// menu button, keyboard-active option, media tile and list-row-action
// row take the hover tint (the option through .is-active with no
// pointer at all), the current menu item keeps the accent tint, and a
// dropdown nested in a menu keeps no button face in any state (A12).
// A tile or a row is pressed through its overlay link: the press
// selectors read the link's :active with the container hovered, so the
// hover is held on the container and the active forced on the link.
func TestMenusTilesAndOptionsTakeTheTint(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("dd").open = true, true`, nil))
	for _, c := range []struct{ sel, press string }{{"#menu-a", "#menu-a"}, {"#menu-btn", "#menu-btn"}, {"#tile", "#tile-link"}, {"#row-c", "#link-c"}} {
		forceState(t, ctx, c.sel, "hover")
		if got := computed(t, ctx, c.sel, "background-color"); got != "rgb(245, 247, 253)" {
			t.Errorf("%s hovered is %s, want the hover tint", c.sel, got)
		}
		if c.press == c.sel {
			forceState(t, ctx, c.sel, "hover", "active")
		} else {
			forceState(t, ctx, c.press, "active")
		}
		if got := computed(t, ctx, c.sel, "background-color"); got != "rgb(236, 241, 252)" {
			t.Errorf("%s hovered and pressed is %s, want the press tint", c.sel, got)
		}
		forceState(t, ctx, c.sel)
		forceState(t, ctx, c.press)
	}
	for _, sel := range []string{"#menu-a", "#menu-btn"} {
		forceState(t, ctx, sel, "active")
		if got := computed(t, ctx, sel, "background-color"); got != "rgb(236, 241, 252)" {
			t.Errorf("%s tapped (active, no hover) is %s, want the tap tint", sel, got)
		}
		forceState(t, ctx, sel)
	}
	if got := computed(t, ctx, "#opt-active", "background-color"); got != "rgb(245, 247, 253)" {
		t.Errorf("the keyboard-active option is %s, want the hover tint with no pointer", got)
	}
	if got := computed(t, ctx, "#menu-cur", "background-color"); got != "rgb(235, 241, 254)" {
		t.Errorf("the current menu item is %s, want --rst-accent-soft", got)
	}
	if bg, bw, sh := computed(t, ctx, "#dd-sum", "background-color"), computed(t, ctx, "#dd-sum", "border-top-width"), computed(t, ctx, "#dd-sum", "box-shadow"); bg != "rgb(255, 255, 255)" || bw != "0px" || !strings.HasPrefix(sh, "rgb(208, 212, 220) 0px 0px 0px 1px") {
		t.Errorf("the list-bar summary is %s, border-width %s, shadow %q; want the summary face, no border, and the edge as a 1px ring", bg, bw, sh)
	}
	// Day rounds the face through --rst-summary-radius; a flat theme
	// keeps 0, and the matrix's focused #dd-sum cell holds that side.
	if got := computed(t, ctx, "#dd-sum", "border-top-left-radius"); got != "4px" {
		t.Errorf("the list-bar summary's corner is %s, want day's --rst-radius-sm (4px)", got)
	}
	for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}} {
		forceState(t, ctx, "#dd-nested-sum", states...)
		if bg, bw, sh := computed(t, ctx, "#dd-nested-sum", "background-color"), computed(t, ctx, "#dd-nested-sum", "border-top-width"), computed(t, ctx, "#dd-nested-sum", "box-shadow"); bg != "rgba(0, 0, 0, 0)" || bw != "0px" || sh != "none" {
			t.Errorf("a dropdown nested in a menu, state %v: %s, border %s, shadow %q; it is a row of the menu, not a button (A12)", states, bg, bw, sh)
		}
		forceState(t, ctx, "#dd-nested-sum")
	}
	forceState(t, ctx, "#lra", "hover")
	if got := computed(t, ctx, "#lra", "background-color"); got != "rgb(245, 247, 253)" {
		t.Errorf("a hovered list-row-action row is %s, want --rst-row-tint-hover", got)
	}
	forceState(t, ctx, "#lra")
	for _, sel := range []string{"#link-a", "#lra-link", "#tile-link", "#dd-sum", "#menu-a", "#menu-btn", "#opt-a", "#sw"} {
		if got := computed(t, ctx, sel, "-webkit-tap-highlight-color"); got != "rgba(0, 0, 0, 0)" {
			t.Errorf("%s tap highlight is %s, want transparent (A1)", sel, got)
		}
	}
}

// TestATouchedRowShowsThePressNotAHover: on a coarse pointer there is
// no hover styling (the hover rules sit in (hover: hover)); a row whose
// link is active takes the press tint, and the kebab's tap highlight is
// off.
func TestATouchedRowShowsThePressNotAHover(t *testing.T) {
	rig := sizingRig(t, true, rowsPages(t))
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844))
	home(t, ctx, rig.Origin)
	requirePointer(t, ctx, true)
	still(t, ctx)
	rest := computed(t, ctx, rowA, "background-color")
	forceState(t, ctx, rowA, "hover")
	if got := computed(t, ctx, rowA, "background-color"); got != rest {
		t.Errorf("a coarse pointer's sticky hover tinted the row %s", got)
	}
	forceState(t, ctx, rowA)
	forceState(t, ctx, linkA, "active")
	if got := computed(t, ctx, rowA, "background-color"); got != "rgb(236, 241, 252)" {
		t.Errorf("touched row is %s, want the press tint", got)
	}
	forceState(t, ctx, linkA)
	if got := computed(t, ctx, "#kebab-a", "-webkit-tap-highlight-color"); got != "rgba(0, 0, 0, 0)" {
		t.Errorf("kebab tap highlight %s, want transparent", got)
	}
}

// TestARowsLiftNeverMovesItsOpenMenu (review focus 1): a row menu
// opened on a lifted row stays where it opened while the lift eases
// away. The lift is asserted first, so this fails before the lift
// exists and is not a check of behaviour that already held. Not
// frozen: the easing is the thing watched, so only the inspector is
// enabled.
func TestARowsLiftNeverMovesItsOpenMenu(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 844))
	home(t, ctx, rig.Origin)
	inspect(t, ctx)
	forceState(t, ctx, rowA, "hover")
	time.Sleep(300 * time.Millisecond)
	if got := computed(t, ctx, rowA, "background-color"); got != "rgb(245, 247, 253)" {
		t.Fatalf("row A hovered is %s; the lift this test guards is missing", got)
	}
	mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("kebab-a").click(), true`, nil))
	var before, after struct{ X, Y float64 }
	at(t, ctx, `(() => { const r = document.getElementById("panel-a").getBoundingClientRect(); return JSON.stringify({X: r.left, Y: r.top}); })()`, &before)
	forceState(t, ctx, rowA)
	time.Sleep(300 * time.Millisecond)
	at(t, ctx, `(() => { const r = document.getElementById("panel-a").getBoundingClientRect(); return JSON.stringify({X: r.left, Y: r.top}); })()`, &after)
	if before != after {
		t.Errorf("the open panel moved from %+v to %+v while the row's lift eased away", before, after)
	}
}

// shellTailBody is a topbar in both spellings with its phone card open:
// the tail holds the nav, the account menu and the locale and scheme
// menus, as layouts/topbar.html renders them, and the page holds a
// locale menu of its own, which is the control (a dropdown standing
// alone keeps the face).
// markup-spelling: old-spelling begin — the shell tail is drawn in both spellings, so the face exclusion is held in each.
const shellTailBody = `<div rst-shell-topbar><header rst-shell-bar><a rst-shell-brand href="/">Home</a>
<details rst-shell-menu name="rst-shell-menu" open><summary>Menu</summary></details>
<div rst-shell-tail><nav rst-shell-nav><a href="#o">Overview</a></nav>
<details rst-dropdown rst-shell-account name="rst-menus"><summary>Account</summary><div rst-dropdown-menu><a href="#s">Sign out</a></div></details>
<details rst-dropdown rst-locale name="rst-menus"><summary id="tail-loc">Language</summary><div rst-dropdown-menu><form><button type="button" aria-current="true">English</button></form></div></details>
<details rst-dropdown rst-scheme name="rst-menus"><summary id="tail-sch">Theme</summary><div rst-dropdown-menu><form><button type="button">Dark</button></form></div></details>
</div></header>
<main rst-page id="main"><details rst-dropdown rst-locale name="rst-menus"><summary id="page-loc">Language</summary><div rst-dropdown-menu><form><button type="button">English</button></form></div></details></main></div>
<div class="rst-shell-topbar"><header class="rst-shell__bar">
<details class="rst-shell__menu" name="rst-shell-menu-c" open><summary>Menu</summary></details>
<div class="rst-shell__tail"><details class="rst-dropdown rst-scheme" name="rst-menus"><summary id="tail-sch-c">Theme</summary><div class="rst-dropdown__menu"><form><button type="button">Dark</button></form></div></details></div>
</header></div>`

// markup-spelling: old-spelling end

// TestTheShellTailsMenusWearNoFace (A12, ruling 6.I2): in day, the
// locale and scheme summaries in a shell's tail stay flat, as their
// neighbours do, in the desktop bar at 1280 and as rows of the phone
// card at 390, in every state; the same menu on the page wears the
// summary face, so the check can see a face when there is one. Each
// width has its own rig, opened at that width: a second
// EmulateViewport loses the mouse (TestEveryNavSurfaceHasItsStates).
func TestTheShellTailsMenusWearNoFace(t *testing.T) {
	for _, width := range []int64{1280, 390} {
		rig := sizingRig(t, false, map[string]string{"/": sizingDoc("tail", shellTailBody)}, harness.WithHover())
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		mustRun(t, ctx, chromedp.EmulateViewport(width, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#page-loc", chromedp.ByQuery))
		still(t, ctx)
		var hov bool
		at(t, ctx, `JSON.stringify(matchMedia("(hover: hover)").matches)`, &hov)
		if !hov {
			t.Fatalf("%d: (hover: hover) is false; the face's hover rules cannot be read here", width)
		}
		if bg := computed(t, ctx, "#page-loc", "background-color"); bg != "rgb(255, 255, 255)" {
			t.Errorf("%d: CONTROL: the page's locale summary is %s, want the summary face; the check below cannot see a face", width, bg)
		}
		for _, sel := range []string{"#tail-loc", "#tail-sch", "#tail-sch-c"} {
			for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}} {
				forceState(t, ctx, sel, states...)
				if bg, sh := computed(t, ctx, sel, "background-color"), computed(t, ctx, sel, "box-shadow"); bg != "rgba(0, 0, 0, 0)" || sh != "none" {
					t.Errorf("%d: shell-tail summary %s %v is %s with box-shadow %q; want no face (rgba(0, 0, 0, 0), none)", width, sel, states, bg, sh)
				}
				forceState(t, ctx, sel)
			}
		}
		cancel()
	}
}

// TestATBlockHeadTapsWithoutAFlash: a tap on the head's title (not the
// track) toggles the block, which is native <label> behaviour and
// already held; the new claims are that the head is the press surface
// (no browser tap highlight) and that the pressed head takes no tint
// of its own, because its switch is the feedback. The highlight check
// passes before Task 7 too: Task 6's tap-highlight rule already names
// the head; the background check is what fails if the head's hover
// tint leaves the hover query.
func TestATBlockHeadTapsWithoutAFlash(t *testing.T) {
	body := `<div rst-page><div rst-tblock id="tb"><label rst-tblock-head id="head"><input type="checkbox"><span rst-switch-track aria-hidden="true"></span><span rst-tblock-title id="title">Block</span></label><div rst-tblock-body>body</div></div></div>`
	rig := sizingRig(t, true, map[string]string{"/": sizingDoc("tblock", body)})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#head", chromedp.ByQuery))
	requirePointer(t, ctx, true)
	x, y, w, h := rect(t, ctx, "#title")
	mustRun(t, ctx, input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: x + w/2, Y: y + h/2}}), input.DispatchTouchEvent(input.TouchEnd, nil))
	var checked bool
	mustRun(t, ctx, chromedp.Evaluate(`document.querySelector("#head input").checked`, &checked))
	if !checked {
		t.Fatal("a tap on the title did not toggle the block")
	}
	if got := computed(t, ctx, "#head", "-webkit-tap-highlight-color"); got != "rgba(0, 0, 0, 0)" {
		t.Errorf("head tap highlight %s, want transparent", got)
	}
	// A tap leaves the head :hover on touch, in Chromium and WebKit, so a
	// hover tint outside (hover: hover) stuck on the tapped head.
	if got := computed(t, ctx, "#head", "background-color"); got != "rgba(0, 0, 0, 0)" {
		t.Errorf("head background after a tap %s, want transparent: the hover tint stuck", got)
	}
}
