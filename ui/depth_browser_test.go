//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/css"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// depthBody is the page every depth drive reads: both spellings of
// every component the depth language touches, with stable ids. It
// links no light.js; lightDoc (light_browser_test.go) adds the tag.
// markup-spelling: old-spelling begin — the depth fixture carries both spellings of each component it touches, so every rule is read in each.
const depthBody = `<div rst-page>
<p id="btns"><button rst-btn id="b-plain">Plain</button> <button class="rst-btn" id="b-plain-c">Plain</button> <button rst-btn="primary" id="b-primary">Save</button> <button class="rst-btn rst-btn--primary" id="b-primary-c">Save</button> <button rst-btn="danger" id="b-danger">Delete</button> <button rst-btn="ghost" id="b-ghost">Cancel</button> <a rst-btn="primary lg" href="#" id="b-link">Go</a> <button rst-btn disabled id="b-off">Off</button> <button rst-btn aria-busy="true" id="b-busy"><span rst-spin></span>Busy</button></p>
<section rst-box id="box"><p>A box.</p></section>
<div rst-stats id="stats"><div rst-stat><span rst-stat-label>Open</span><span rst-stat-num>12</span></div></div>
<div rst-list id="list"><div rst-row id="lra"><span rst-row-main><a href="#lra" id="lra-link">Release notes</a></span></div></div>
<div rst-card id="card" style="--rst-cols: 1fr auto">
<div rst-lrow="head"><span>Name</span><span></span></div>
<div rst-lrow id="row-a"><a class="rst-nm" href="#a" id="link-a">Alpha</a><span class="rst-cell-mut">one</span></div>
<div class="rst-lrow" id="row-c"><a class="rst-nm" href="#c" id="link-c">Class row</a><span class="rst-cell-mut">two</span></div>
<div rst-lrow id="row-inert"><span class="rst-nm">Gamma</span><span class="rst-cell-mut">three</span></div>
</div>
<div rst-list id="media-list"><div rst-media><div rst-media-tile id="tile"><div rst-media-frame><img src="/images/a.svg" alt=""></div><div rst-media-caption><div rst-media-name><a href="#t" id="tile-link">Picture</a></div></div></div></div></div>
<div rst-shell-sidebar id="sidebar"><nav rst-shell-rail><div rst-shell-nav><a href="#x" id="nav-x">Overview</a><a href="#y" aria-current="page" id="nav-y">Requests</a></div></nav></div>
<div rst-shell-console id="console"><nav rst-shell-rail><div rst-shell-nav><a href="#cx" id="cnav-x">Overview</a><a href="#cy" aria-current="page" id="cnav-y">Requests</a></div></nav></div>
<div rst-shell-topbar id="topbar"><div rst-shell-tail><div rst-shell-nav><a href="#tx" id="tnav-x">Overview</a><a href="#ty" aria-current="page" id="tnav-y">Requests</a></div></div></div>
<nav rst-seg-tabs id="tabs"><a href="#t1" id="tab-1">Open</a><a href="#t2" aria-current="page" id="tab-2">Closed</a></nav>
<nav rst-pagination id="pages"><a href="#p1" id="page-1">1</a><a href="#p2" aria-current="page" id="page-2">2</a></nav>
<details rst-dropdown id="dd"><summary id="dd-sum">Filter</summary><div rst-dropdown-menu id="dd-menu"><a href="#m1" id="menu-a">All</a><a href="#m2" aria-current="true" id="menu-cur">Open</a><button type="button" id="menu-btn">Archive</button><details rst-dropdown id="dd-nested"><summary id="dd-nested-sum">More</summary><div rst-dropdown-menu><a href="#n1">Nested</a></div></details></div></details>
<div rst-combo><div rst-combo-list role="listbox"><div rst-combo-option role="option" id="opt-a">Alpha</div><div rst-combo-option class="is-active" role="option" id="opt-active">Beta</div></div></div>
<p><label rst-switch id="sw"><input type="checkbox"><span rst-switch-track aria-hidden="true"></span> Email me</label></p>
<p><input rst-input id="field" value="x"></p>
<div rst-shell-sidebar="index" id="isidebar"><nav rst-shell-rail><div rst-shell-nav><a href="#ix" id="inav-x">Overview</a><a href="#iy" aria-current="page" id="inav-y">Requests</a></div></nav></div>
<p><label rst-switch id="sw-on"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span> Email me</label></p>
<div rst-tblock id="tb"><label rst-tblock-head id="tb-head"><input type="checkbox" checked><span rst-switch-track aria-hidden="true"></span><span rst-tblock-title>Block</span></label><div rst-tblock-body>Body</div></div>
<p><textarea rst-textarea id="area">x</textarea></p>
<form rst-search id="search"><input type="search" id="search-in" value="x"></form>
</div>`

// markup-spelling: old-spelling end

func depthDoc(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{"/": sizingDoc("depth", depthBody)}
}

// still freezes the page (C14): no transition, no animation, and the
// light held straight above if light.js is on the page. Every drive
// that reads a state snapshot calls it first, because a hover read in
// the first 250ms of a 0.25s transition is a number the test did not
// choose. The injected sheet is test-only; nothing shipped carries an
// !important. It also enables the inspector for forceState (inspect).
func still(t *testing.T, ctx context.Context) {
	t.Helper()
	inspect(t, ctx)
	mustRun(t, ctx, chromedp.Evaluate(`(() => { const s = document.createElement("style"); s.textContent = "*, *::before, *::after { transition: none !important; animation: none !important; }"; document.head.append(s); document.dispatchEvent(new CustomEvent("rst:light", {detail: {x: 0, y: -1, hold: true}})); return true; })()`, nil))
}

// inspect enables the DOM and CSS domains once for the page, which
// forceState needs: enabling them again per call cleared every
// pseudo-state forced so far, so a drive that pinned hover on two
// buttons read the first one at rest. On its own, for a drive that
// forces a state and then watches a transition run (a row's lift
// easing away under an open menu), which still() would freeze.
func inspect(t *testing.T, ctx context.Context) {
	t.Helper()
	mustRun(t, ctx, chromedp.ActionFunc(func(c context.Context) error {
		if err := dom.Enable().Do(c); err != nil {
			return err
		}
		if err := css.Enable().Do(c); err != nil {
			return err
		}
		// One DOM.getDocument per page: every call invalidates the node
		// ids handed out before it, and a forced pseudo-state is keyed
		// by node id, so a second call dropped the first button's hover.
		root, err := dom.GetDocument().Do(c)
		if err != nil {
			return err
		}
		forcedRoot = root.NodeID
		return nil
	}))
}

// forcedRoot is the page's document node, set by inspect() for forceState.
var forcedRoot cdp.NodeID

// forceState pins pseudo-classes on one element through the inspector
// (what DevTools' :hov panel uses). The list REPLACES the element's
// forced set, so a drive that wants hover and focus together passes
// both in one call; states on other elements stay pinned. still() or
// inspect() must have run on the page first.
func forceState(t *testing.T, ctx context.Context, selector string, states ...string) {
	t.Helper()
	if states == nil {
		states = []string{}
	}
	if forcedRoot == cdp.EmptyNodeID {
		t.Fatal("forceState before still() or inspect(): the document node is not known")
	}
	mustRun(t, ctx, chromedp.ActionFunc(func(c context.Context) error {
		id, err := dom.QuerySelector(forcedRoot, selector).Do(c)
		if err != nil {
			return err
		}
		if id == cdp.EmptyNodeID {
			return fmt.Errorf("forceState: no element matches %q", selector)
		}
		return css.ForcePseudoState(id, states).Do(c)
	}))
}

// computed reads one computed property of one element, with any
// color(srgb …) or oklch(…) in it rewritten as rgb()/rgba(): a derived
// day token serialises as one of those, and every literal below is an
// eight-bit triple (canonColours, derived_test.go).
func computed(t *testing.T, ctx context.Context, selector, prop string) string {
	t.Helper()
	var out string
	mustRun(t, ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector(`+jsStr(selector)+`)).getPropertyValue(`+jsStr(prop)+`)`, &out))
	return canonColours(strings.TrimSpace(out))
}

func jsStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// layers splits a computed background-image into its top-level layers.
func layers(v string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range v {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(v[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(v[start:]))
}

func isGrey(rgb string) bool {
	var r, g, b float64
	if _, err := fmt.Sscanf(rgb, "rgb(%f, %f, %f)", &r, &g, &b); err != nil {
		return false
	}
	return math.Abs(r-g) < 8 && math.Abs(g-b) < 8 && math.Abs(r-b) < 8
}

func rect(t *testing.T, ctx context.Context, selector string) (x, y, w, h float64) {
	t.Helper()
	var r struct{ X, Y, W, H float64 }
	at(t, ctx, `(() => { const r = document.querySelector(`+jsStr(selector)+`).getBoundingClientRect(); return JSON.stringify({X: r.left, Y: r.top, W: r.width, H: r.height}); })()`, &r)
	return r.X, r.Y, r.W, r.H
}

func open(t *testing.T, ctx context.Context, rig *harness.Rig, path string, reduce bool) {
	t.Helper()
	if reduce {
		mustRun(t, ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}))
	}
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible("#field", chromedp.ByQuery))
}

// TestEveryButtonVariantHasItsOwnHoverAndPress is the C11 table read back
// from the browser, frozen first (C14): rest, hover and press differ for
// every variant in both spellings; a filled hover keeps its fill and
// never turns grey (the mockup regression); the sheen is on only while
// hovered and not pressed; nothing hovers or presses while disabled or
// busy; the class and attribute spellings compute the same.
func TestEveryButtonVariantHasItsOwnHoverAndPress(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	type face struct{ bg, img, border, color, shadow string }
	read := func(id string) face {
		return face{computed(t, ctx, id, "background-color"), computed(t, ctx, id, "background-image"), computed(t, ctx, id, "border-top-color"), computed(t, ctx, id, "color"), computed(t, ctx, id, "box-shadow")}
	}
	for _, id := range []string{"#b-plain", "#b-primary", "#b-danger", "#b-ghost", "#b-link"} {
		rest := read(id)
		forceState(t, ctx, id, "hover")
		hover := read(id)
		forceState(t, ctx, id, "hover", "active")
		press := read(id)
		forceState(t, ctx, id)
		if rest == hover {
			t.Errorf("%s: hover is the rest face %+v", id, rest)
		}
		if hover == press {
			t.Errorf("%s: press is the hover face %+v; a pressed face replaces the hovered face (C11)", id, hover)
		}
		if id == "#b-primary" || id == "#b-danger" || id == "#b-link" {
			if hover.bg == rest.bg {
				t.Errorf("%s: hover keeps the rest fill %s; every state reads its own token (C3)", id, rest.bg)
			}
			if isGrey(hover.bg) {
				t.Errorf("%s: hovered fill %s is grey; a filled button's hover keeps its fill", id, hover.bg)
			}
			if !strings.Contains(hover.img, "radial-gradient(120px") {
				t.Errorf("%s: hover has no 120px sheen: %q", id, hover.img)
			}
		}
		if id == "#b-plain" && !strings.Contains(hover.img, "radial-gradient(90px") {
			t.Errorf("#b-plain: hover has no 90px sheen: %q", hover.img)
		}
		if strings.Contains(press.img, "radial-gradient(90px") || strings.Contains(press.img, "radial-gradient(120px") {
			t.Errorf("%s: the sheen is still on while pressed: %q", id, press.img)
		}
	}
	for _, pair := range [][2]string{{"#b-plain", "#b-plain-c"}, {"#b-primary", "#b-primary-c"}} {
		for _, states := range [][]string{{}, {"hover"}, {"hover", "active"}} {
			forceState(t, ctx, pair[0], states...)
			forceState(t, ctx, pair[1], states...)
			if a, b := read(pair[0]), read(pair[1]); a != b {
				t.Errorf("%s and %s differ in state %v: %+v vs %+v; both spellings are one rule (C12)", pair[0], pair[1], states, a, b)
			}
			forceState(t, ctx, pair[0])
			forceState(t, ctx, pair[1])
		}
	}
	// A disabled or busy button hovers like any other (today's
	// behaviour in every theme) and never presses: the press rule
	// excludes it, and a browser never makes a disabled button :active.
	// The sheen is not compared: it is keyed on :not(:active), which a
	// forced state can set on a disabled button where a pointer cannot.
	for _, id := range []string{"#b-off", "#b-busy"} {
		forceState(t, ctx, id, "hover")
		hover := read(id)
		forceState(t, ctx, id, "hover", "active")
		got := read(id)
		forceState(t, ctx, id)
		hover.img, got.img = "", ""
		if got != hover {
			t.Errorf("%s pressed while hovered: %+v -> %+v; a disabled or busy button has no press", id, hover, got)
		}
	}
}

// cssColour spells a theme token the way Chromium serialises a computed
// colour: a hex as rgb(r, g, b), an rgba() with its alpha as the
// shortest decimal, transparent as rgba(0, 0, 0, 0). channelsOf and
// alphaOf are contrast_face_test.go's.
func cssColour(t *testing.T, v string) string {
	t.Helper()
	v = strings.TrimSpace(v)
	if v == "transparent" {
		return "rgba(0, 0, 0, 0)"
	}
	c, a := channelsOf(t, v), alphaOf(t, v)
	if a >= 1 {
		return fmt.Sprintf("rgb(%d, %d, %d)", int(c[0]), int(c[1]), int(c[2]))
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", int(c[0]), int(c[1]), int(c[2]), strconv.FormatFloat(a, 'f', -1, 64))
}

// expectedStack is the four-layer background-image the C11 table says
// a button paints, in Chromium's computed serialisation (measured, not
// guessed: a 70px radial at the default position serialises without
// the position, a colour stop without a position as the colour alone,
// calc(1 * 0.16) as 0.16). sheen is "" for none.
func expectedStack(sheen, spotAlpha, washAlpha, face string) string {
	if sheen == "" {
		sheen = "none"
	}
	return sheen + ", radial-gradient(70px, rgba(0, 0, 0, " + spotAlpha + "), rgba(0, 0, 0, 0) 75%), linear-gradient(rgba(0, 0, 0, " + washAlpha + ") 0px, rgba(0, 0, 0, " + washAlpha + ") 0px), " + face
}

func alphaStr(a float64) string { return strconv.FormatFloat(a, 'f', -1, 64) }

// TestEveryButtonPaintsItsWholeStack (A4, A18, C4): for primary, danger
// and plain, at rest, hovered and pressed, in both schemes, the resolved
// background-image is exactly the four layers the table composes from
// day's tokens, the background-colour is the state's fill and the
// colour its label, so a dropped declaration (a shorter list, a face
// that is not last) or a disconnected token (a colour that is not the
// theme's) is caught in every cell the contrast arithmetic assumes. The
// negative control severs the danger face in dark, hovered, and proves
// the check fails there.
func TestEveryButtonPaintsItsWholeStack(t *testing.T) {
	for _, scheme := range []string{"light", "dark"} {
		t.Run(scheme, func(t *testing.T) {
			rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			open(t, ctx, rig, "/", false)
			mustRun(t, ctx, chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), true`, nil))
			still(t, ctx)
			tok := resolvedTokens(t, "day")[scheme]
			wash, spot := pressNumber(t, "day", scheme, "--rst-press-wash"), pressNumber(t, "day", scheme, "--rst-press-spot")
			washP, spotP := pressNumber(t, "day", scheme, "--rst-press-wash-plain"), pressNumber(t, "day", scheme, "--rst-press-spot-plain")
			face := "linear-gradient(" + cssColour(t, "rgba(255, 255, 255, "+tok["--rst-depth-face"]+")") + ", " + cssColour(t, "rgba(0, 0, 0, "+tok["--rst-depth-face"]+")") + ")"
			sheenFill := "radial-gradient(120px at 50% -40%, " + cssColour(t, tok["--rst-sheen-fill"]) + ", rgba(0, 0, 0, 0) 70%)"
			sheenPlain := "radial-gradient(90px at 50% -40%, " + cssColour(t, tok["--rst-sheen-plain"]) + ", rgba(0, 0, 0, 0) 70%)"
			type cell struct {
				sel           string
				states        []string
				stack, bg, fg string
			}
			cells := []cell{
				{"#b-primary", nil, expectedStack("", "0", "0", face), cssColour(t, tok["--rst-accent-fill"]), cssColour(t, tok["--rst-on-accent-fill"])},
				{"#b-primary", []string{"hover"}, expectedStack(sheenFill, "0", "0", face), cssColour(t, tok["--rst-accent-fill-hover"]), cssColour(t, tok["--rst-on-accent-fill"])},
				{"#b-primary", []string{"hover", "active"}, expectedStack("", alphaStr(spot), alphaStr(wash), face), cssColour(t, tok["--rst-accent-fill-hover"]), cssColour(t, tok["--rst-on-accent-fill"])},
				{"#b-danger", nil, expectedStack("", "0", "0", face), cssColour(t, tok["--rst-danger-fill"]), cssColour(t, tok["--rst-on-danger-fill"])},
				{"#b-danger", []string{"hover"}, expectedStack(sheenFill, "0", "0", face), cssColour(t, tok["--rst-danger-fill-hover"]), cssColour(t, tok["--rst-on-danger-fill-hover"])},
				{"#b-danger", []string{"hover", "active"}, expectedStack("", alphaStr(spot), alphaStr(wash), face), cssColour(t, tok["--rst-danger-fill-hover"]), cssColour(t, tok["--rst-on-danger-fill-hover"])},
				{"#b-plain", nil, expectedStack("", "0", "0", "none"), cssColour(t, tok["--rst-control-face"]), cssColour(t, tok["--rst-text"])},
				{"#b-plain", []string{"hover"}, expectedStack(sheenPlain, "0", "0", "none"), cssColour(t, tok["--rst-control-face-hover"]), cssColour(t, tok["--rst-control-text-hover"])},
				{"#b-plain", []string{"hover", "active"}, expectedStack("", alphaStr(spotP), alphaStr(washP), "none"), cssColour(t, tok["--rst-control-face-hover"]), cssColour(t, tok["--rst-control-text-hover"])},
				// A ghost hovers with no sheen (the C11 ghost row): the plain
				// sheen rule is :hover:not(:active) at (0,3,0), and a ghost
				// rule at (0,2,0) once lost to it and painted the accent.
				{"#b-ghost", []string{"hover"}, expectedStack("", "0", "0", "none"), cssColour(t, tok["--rst-ghost-face-hover"]), cssColour(t, tok["--rst-text"])},
			}
			for _, c := range cells {
				forceState(t, ctx, c.sel, c.states...)
				got, bg, fg := computed(t, ctx, c.sel, "background-image"), computed(t, ctx, c.sel, "background-color"), computed(t, ctx, c.sel, "color")
				forceState(t, ctx, c.sel)
				if norm(got) != norm(c.stack) {
					t.Errorf("%s %v stack:\n got  %s\n want %s", c.sel, c.states, got, c.stack)
				}
				if bg != c.bg || fg != c.fg {
					t.Errorf("%s %v is %s on %s, want %s on %s", c.sel, c.states, fg, bg, c.fg, c.bg)
				}
			}
			if scheme == "dark" {
				// The negative control: sever the danger face and a cell the
				// loop above accepted must now fail.
				mustRun(t, ctx, chromedp.Evaluate(`(() => { const s = document.createElement("style"); s.textContent = "[rst-btn~=danger] { --rst-btn-face: none; }"; document.head.append(s); return true; })()`, nil))
				forceState(t, ctx, "#b-danger", "hover")
				cut := computed(t, ctx, "#b-danger", "background-image")
				forceState(t, ctx, "#b-danger")
				if norm(cut) == norm(expectedStack(sheenFill, "0", "0", face)) {
					t.Fatal("severing the danger face left its hovered stack unchanged; this check would not catch a dropped declaration")
				}
			}
		})
	}
}

// norm collapses whitespace differences in a serialised gradient.
func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestAPressIsInstantAndSlowToLeave is A2's drive: --rst-press is 1 and
// the button sits 1px lower while held, with the same box; on release
// the fade is a 0.7s transition on the spec's curve, and 250ms into it
// the value is what the curve gives (about 0.223); under reduced motion
// it is 0 at once and the button never moved. A real pointer, not a
// forced state: the fade is a transition, and a forced pseudo-state
// does not run one on removal. Not frozen, for the same reason. The
// transition is read through getAnimations(), paused and scrubbed to
// 250ms, not sampled by the clock: a sleep-then-read had about 110ms
// of slack on a slow runner (0.11 at 350ms, under the old floor of
// 0.1), and the scrub still catches a snap (no transition), a stick,
// and a wrong duration or curve.
func TestAPressIsInstantAndSlowToLeave(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced motion %v", reduce), func(t *testing.T) {
			rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			open(t, ctx, rig, "/", reduce)
			x, y, w, h := rect(t, ctx, "#b-primary")
			cx, cy := x+w/2, y+h/2
			mustRun(t, ctx, chromedp.MouseEvent(input.MousePressed, cx, cy, chromedp.ButtonLeft, chromedp.ClickCount(1)))
			if got := computed(t, ctx, "#b-primary", "--rst-press"); got != "1" {
				t.Fatalf("held: --rst-press is %q, want 1", got)
			}
			if got := computed(t, ctx, "#b-primary", "translate"); (got == "0px 1px") == reduce {
				t.Errorf("held: translate is %q (reduced motion %v)", got, reduce)
			}
			if _, _, w2, h2 := rect(t, ctx, "#b-primary"); w2 != w || h2 != h {
				t.Errorf("the pressed button's box changed from %vx%v to %vx%v; a press must cause no layout shift", w, h, w2, h2)
			}
			mustRun(t, ctx, chromedp.MouseEvent(input.MouseReleased, cx, cy, chromedp.ButtonLeft, chromedp.ClickCount(1)))
			if reduce {
				if got := computed(t, ctx, "#b-primary", "--rst-press"); got != "0" {
					t.Errorf("released under reduced motion: --rst-press is %q, want 0 at once", got)
				}
				return
			}
			var fade struct {
				Found    bool
				Duration float64
				Easing   string
				Value    string
			}
			at(t, ctx, `(() => { const b = document.querySelector("#b-primary"); const a = b.getAnimations().find(x => x.transitionProperty === "--rst-press"); if (!a) return JSON.stringify({Found: false}); const tm = a.effect.getTiming(); a.pause(); a.currentTime = 250; return JSON.stringify({Found: true, Duration: tm.duration, Easing: tm.easing, Value: getComputedStyle(b).getPropertyValue("--rst-press")}); })()`, &fade)
			if !fade.Found {
				t.Fatal("released: no transition is running on --rst-press; the press snapped instead of fading")
			}
			if fade.Duration != 700 || fade.Easing != "cubic-bezier(0.2, 0.6, 0.3, 1)" {
				t.Errorf("the fade runs %vms on %q, want 700ms on cubic-bezier(0.2, 0.6, 0.3, 1)", fade.Duration, fade.Easing)
			}
			var v float64
			if _, err := fmt.Sscanf(fade.Value, "%f", &v); err != nil {
				t.Fatalf("--rst-press at 250ms is %q: %v", fade.Value, err)
			}
			if math.Abs(v-0.223) > 0.005 {
				t.Errorf("250ms into the fade --rst-press is %v, want about 0.223: the curve from 1 to 0 over 0.7s gives that, a stuck or linear fade does not", v)
			}
		})
	}
}

// TestAPressInsideTheFadeSnapsBackToOne (review focus 2).
func TestAPressInsideTheFadeSnapsBackToOne(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	x, y, w, h := rect(t, ctx, "#b-plain")
	cx, cy := x+w/2, y+h/2
	mustRun(t, ctx, chromedp.MouseEvent(input.MousePressed, cx, cy, chromedp.ButtonLeft, chromedp.ClickCount(1)),
		chromedp.MouseEvent(input.MouseReleased, cx, cy, chromedp.ButtonLeft, chromedp.ClickCount(1)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.MouseEvent(input.MousePressed, cx, cy, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	if got := computed(t, ctx, "#b-plain", "--rst-press"); got != "1" {
		t.Errorf("pressed again mid-fade: --rst-press is %q, want 1 at once", got)
	}
	mustRun(t, ctx, chromedp.MouseEvent(input.MouseReleased, cx, cy, chromedp.ButtonLeft, chromedp.ClickCount(1)))
}

// TestFocusOutlinesSurviveLiftAndPress (C14, review focus 4): the solid
// 2px outline is computed on a focused button that is also hovered and
// pressed, on a focused link in a hovered row, and on a focused hovered
// nav item. One forceState call per element with every state it needs.
// It passes before the press and the lift exist (the outline is the
// page's today): it is here so no later state can take it away.
func TestFocusOutlinesSurviveLiftAndPress(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	forceState(t, ctx, "#b-primary", "hover", "active", "focus", "focus-visible")
	if st, w := computed(t, ctx, "#b-primary", "outline-style"), computed(t, ctx, "#b-primary", "outline-width"); st != "solid" || w != "2px" {
		t.Errorf("a focused, hovered, pressed button has outline %s %s, want solid 2px", st, w)
	}
	forceState(t, ctx, "#b-primary")
	forceState(t, ctx, "#row-a", "hover")
	forceState(t, ctx, "#link-a", "focus", "focus-visible")
	var ring string
	mustRun(t, ctx, chromedp.Evaluate(`(() => { const s = getComputedStyle(document.querySelector("#link-a"), "::after"); return s.outlineStyle + " " + s.outlineWidth; })()`, &ring))
	if ring != "solid 2px" {
		t.Errorf("a focused link in a hovered row draws %q on its overlay, want solid 2px", ring)
	}
	forceState(t, ctx, "#row-a")
	forceState(t, ctx, "#link-a")
	forceState(t, ctx, "#nav-x", "hover", "focus", "focus-visible")
	if st, w := computed(t, ctx, "#nav-x", "outline-style"), computed(t, ctx, "#nav-x", "outline-width"); st != "solid" || w != "2px" {
		t.Errorf("a focused hovered nav item has outline %s %s, want solid 2px", st, w)
	}
	forceState(t, ctx, "#nav-x")
}

// TestCardsRestOnThePageWithNoScript: with no light.js on the page, a
// box, a stats band, a list card and a data card each compute the two
// ambient layers lit from straight above, a 0px rim radius (the static
// design), and --rst-card-lit 0. Depth adds no height: the same box is
// measured again with its decorative tokens set flat on the element,
// which changes shadows and gradients and nothing in layout.
func TestCardsRestOnThePageWithNoScript(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	for _, id := range []string{"#box", "#stats", "#list", "#card"} {
		if got := computed(t, ctx, id, "--rst-card-lit"); got != "0" {
			t.Errorf("%s: --rst-card-lit is %q with no script, want 0", id, got)
		}
		shadow := computed(t, ctx, id, "box-shadow")
		if !strings.HasPrefix(shadow, "rgba(16, 24, 40, 0.06) 0px 1px 1px 0px, rgba(16, 24, 40, 0.06) 0px 1.5px 3px 0px") {
			t.Errorf("%s: box-shadow %q, want the two ambient layers lit from straight above", id, shadow)
		}
		if img := computed(t, ctx, id, "background-image"); !strings.Contains(img, "radial-gradient(0px") {
			t.Errorf("%s: the rim gradient is %q; at lit 0 its radius is 0px", id, img)
		}
	}
	var heights struct{ Lit, Flat float64 }
	at(t, ctx, `(() => { const b = document.getElementById("box"); const lit = b.getBoundingClientRect().height; b.style.cssText = "--rst-depth-ambient: transparent; --rst-light-rim: var(--rst-line); --rst-card-lit: 1"; const flat = b.getBoundingClientRect().height; b.style.cssText = ""; return JSON.stringify({Lit: lit, Flat: flat}); })()`, &heights)
	if heights.Lit != heights.Flat {
		t.Errorf("a box is %vpx tall with depth and %vpx without; depth added height", heights.Lit, heights.Flat)
	}
}

// TestEveryNavSurfaceHasItsStates is the table above read back from
// the browser, frozen: the sidebar's and console's current item is a
// surface thumb with a ring and a contact shadow on a rail that sits on
// the page background (C15), on an index-view root at desktop width
// too, where the rail sits beside the page; a hovered item takes the
// hover tint and a pressed one the press tint, in both rails and the
// topbar's phone card; a pressed tab or chip is its hovered self; every
// surface has its tap highlight off (A1).
func TestEveryNavSurfaceHasItsStates(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	for _, c := range []struct{ rail, cur, other string }{{"#sidebar [rst-shell-rail]", "#nav-y", "#nav-x"}, {"#console [rst-shell-rail]", "#cnav-y", "#cnav-x"}, {"#isidebar [rst-shell-rail]", "#inav-y", "#inav-x"}} {
		if bg, rail := computed(t, ctx, c.cur, "background-color"), computed(t, ctx, c.rail, "background-color"); bg != "rgb(255, 255, 255)" || rail != "rgb(251, 251, 252)" {
			t.Errorf("%s: current item %s on a rail of %s; want the surface thumb on the page background", c.cur, bg, rail)
		}
		if sh := computed(t, ctx, c.cur, "box-shadow"); !strings.Contains(sh, "inset") || strings.Count(sh, "rgb") < 3 {
			t.Errorf("%s: box-shadow %q, want the highlight, the ring and the contact shadow", c.cur, sh)
		}
		rest := computed(t, ctx, c.other, "background-color")
		forceState(t, ctx, c.other, "hover")
		hover := computed(t, ctx, c.other, "background-color")
		forceState(t, ctx, c.other, "hover", "active")
		press := computed(t, ctx, c.other, "background-color")
		forceState(t, ctx, c.other)
		if rest == hover || hover != "rgb(245, 247, 253)" || press != "rgb(236, 241, 252)" {
			t.Errorf("%s rest %s hover %s press %s; want rgb(245, 247, 253) and rgb(236, 241, 252)", c.other, rest, hover, press)
		}
	}
	// The topbar's desktop links keep the underline; the phone card is
	// measured on its own rig at the end.
	if got := computed(t, ctx, "#tnav-y", "border-bottom-color"); got != "rgb(31, 86, 214)" {
		t.Errorf("topbar current link underline is %s, want the accent", got)
	}
	// The tab and chip hovers are top-level rules, outside any hover
	// query, so they read the same on a rig with or without a pointer.
	for _, sel := range []string{"#tab-1", "#page-1"} {
		forceState(t, ctx, sel, "hover")
		hover := computed(t, ctx, sel, "background-color")
		forceState(t, ctx, sel, "hover", "active")
		if got := computed(t, ctx, sel, "background-color"); got != hover {
			t.Errorf("%s pressed is %s, hovered %s; tabs and chips have no press of their own", sel, got, hover)
		}
		forceState(t, ctx, sel)
	}
	forceState(t, ctx, "#nav-x", "active")
	if got := computed(t, ctx, "#nav-x", "background-color"); got != "rgb(236, 241, 252)" {
		t.Errorf("nav item tapped (active, no hover) is %s, want the tap tint rgb(236, 241, 252)", got)
	}
	forceState(t, ctx, "#nav-x")
	for _, sel := range []string{"#nav-x", "#cnav-x", "#tnav-x", "#tab-1", "#page-1"} {
		if got := computed(t, ctx, sel, "-webkit-tap-highlight-color"); got != "rgba(0, 0, 0, 0)" {
			t.Errorf("%s tap highlight is %s, want transparent (A1)", sel, got)
		}
	}
	// Chromium serialises a zero letter-spacing as "normal", whatever
	// length the theme wrote, so both spellings of none are accepted;
	// the old 0.06em reads 0.69px and fails either way.
	if size, track := computed(t, ctx, "[rst-stat-label]", "letter-spacing"), computed(t, ctx, "[rst-stat-label]", "text-transform"); (size != "0px" && size != "normal") || track != "none" {
		t.Errorf("stat label letter-spacing %s text-transform %s; want 0px and none (C15)", size, track)
	}
	// The phone card, on a rig opened at the phone width: after a second
	// EmulateViewport this Chromium reports (hover: none) and (pointer:
	// none), so a card read on the rig above would see only the tap
	// rule. One viewport call before navigation keeps the mouse, and the
	// query is checked first (harness/rig.go says to). Day hovers to its
	// card tint and presses to the tap tint, and its current link wears
	// the thumb's three shadow layers; plain and signal, swapped in on
	// the same page, paint nothing on hover or press, as they did before
	// the card had a hover (A15).
	narrow := sizingRig(t, false, depthDoc(t), harness.WithHover())
	nctx, ncancel := context.WithTimeout(narrow.Context(), 60*time.Second)
	defer ncancel()
	mustRun(t, nctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(narrow.Origin+"/"), chromedp.WaitVisible("#field", chromedp.ByQuery))
	still(t, nctx)
	var hov bool
	at(t, nctx, `JSON.stringify(matchMedia("(hover: hover)").matches)`, &hov)
	if !hov {
		t.Fatal("(hover: hover) is false on the narrow rig; the card's hover rules cannot be read here")
	}
	cardFace := func(states ...string) string {
		forceState(t, nctx, "#tnav-x", states...)
		got := computed(t, nctx, "#tnav-x", "background-color")
		forceState(t, nctx, "#tnav-x")
		return got
	}
	if hover, press := cardFace("hover"), cardFace("hover", "active"); hover != "rgb(245, 247, 253)" || press != "rgb(236, 241, 252)" {
		t.Errorf("topbar phone-card link hover %s press %s; want rgb(245, 247, 253) and rgb(236, 241, 252)", hover, press)
	}
	if bg, sh := computed(t, nctx, "#tnav-y", "background-color"), computed(t, nctx, "#tnav-y", "box-shadow"); bg != "rgb(255, 255, 255)" || !strings.Contains(sh, "inset") || strings.Count(sh, "rgb") < 3 {
		t.Errorf("topbar phone-card current link is %s with box-shadow %q; want the surface thumb with the highlight, the ring and the contact shadow", bg, sh)
	}
	// The phone index's rows, on the same rig: they tint and nothing
	// else. The rail items' state rules match these rows too and the
	// index block outweighs them only by repeating the root's name, so
	// these cells are what holds that ladder: weaken it (drop the
	// doubled root) and the rail's ring and sink leak back as a shadow
	// on a hovered or pressed row. Hover is the hover tint, press and tap
	// the press tint (today's accent-soft in plain and signal, where the
	// tap tint is transparent and would have blanked a tapped row), and a
	// current row rests as any row, with no thumb.
	face := func(sel string, states ...string) (bg, shadow string) {
		forceState(t, nctx, sel, states...)
		bg, shadow = computed(t, nctx, sel, "background-color"), computed(t, nctx, sel, "box-shadow")
		forceState(t, nctx, sel)
		return bg, shadow
	}
	indexRows := func(theme, hoverTint, pressTint string) {
		for _, sel := range []string{"#inav-x", "#inav-y"} {
			for _, c := range []struct {
				states []string
				want   string
			}{{[]string{"hover"}, hoverTint}, {[]string{"hover", "active"}, pressTint}, {[]string{"active"}, pressTint}} {
				if bg, shadow := face(sel, c.states...); bg != c.want || shadow != "none" {
					t.Errorf("%s: index row %s %v is %s with box-shadow %q; want %s and none (tint only)", theme, sel, c.states, bg, shadow, c.want)
				}
			}
		}
		restX, _ := face("#inav-x")
		if bg, shadow := face("#inav-y"); bg != restX || shadow != "none" || computed(t, nctx, "#inav-y", "font-weight") != "450" {
			t.Errorf("%s: the current index row rests as %s with box-shadow %q and weight %s; want a plain row (%s, none, 450), the index has no thumb", theme, bg, shadow, computed(t, nctx, "#inav-y", "font-weight"), restX)
		}
	}
	indexRows("day", "rgb(245, 247, 253)", "rgb(236, 241, 252)")
	for _, c := range []struct{ theme, cardHover, hoverTint, pressTint string }{{"plain", "rgba(0, 0, 0, 0)", "rgb(237, 237, 237)", "rgb(237, 237, 237)"}, {"signal", "rgba(0, 0, 0, 0)", "rgb(233, 238, 255)", "rgb(233, 238, 255)"}} {
		css, _ := ThemeCSS(c.theme)
		mustRun(t, nctx, chromedp.Evaluate(`(() => { const l = document.querySelector('link[href="/theme.css"]'); if (l) l.remove(); for (const s of document.querySelectorAll("style[data-swap]")) s.remove(); const s = document.createElement("style"); s.dataset.swap = "1"; s.textContent = `+jsStr(string(css))+`; document.head.append(s); return true; })()`, nil))
		if hover, press := cardFace("hover"), cardFace("hover", "active"); hover != c.cardHover || press != c.cardHover {
			t.Errorf("%s: topbar phone-card link hover %s press %s; want transparent, as before the card had a hover (A15)", c.theme, hover, press)
		}
		indexRows(c.theme, c.hoverTint, c.pressTint)
	}
}

// TestFieldsSitIntoThePageAndFocusKeepsTheOutline (spec §3, C5, C14):
// an input has a 1px inset ambient shadow and a --rst-line-strong
// border at rest; focused, its border is the accent, it gains the 3px
// halo, and the solid 2px outline is still there. The textarea does
// the same, and the search box does it on :focus-within, the field
// box carrying the indicator for its input.
func TestFieldsSitIntoThePageAndFocusKeepsTheOutline(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	still(t, ctx)
	for _, f := range []struct{ box, focus string }{{"#field", "#field"}, {"#area", "#area"}, {"#search", "#search-in"}} {
		// day's light --rst-line-strong is #868691 (Task 2's ruling: the
		// sign-in border needed it darker than the brief's #8a8a95).
		if sh, border := computed(t, ctx, f.box, "box-shadow"), computed(t, ctx, f.box, "border-top-color"); !strings.HasPrefix(sh, "rgba(16, 24, 40, 0.06) 0px 1px 1px 0px inset") || border != "rgb(134, 134, 145)" {
			t.Errorf("%s at rest: box-shadow %q border %s; want the inset ambient shadow and --rst-line-strong", f.box, sh, border)
		}
		forceState(t, ctx, f.focus, "focus", "focus-visible")
		sh, border, outline := computed(t, ctx, f.box, "box-shadow"), computed(t, ctx, f.box, "border-top-color"), computed(t, ctx, f.box, "outline-style")+" "+computed(t, ctx, f.box, "outline-width")
		if border != "rgb(31, 86, 214)" || !strings.Contains(sh, "0px 0px 0px 3px") || outline != "solid 2px" {
			t.Errorf("focused %s: border %s box-shadow %q outline %s; want the accent border, the 3px halo and the solid 2px outline", f.box, border, sh, outline)
		}
		forceState(t, ctx, f.focus)
	}
}
