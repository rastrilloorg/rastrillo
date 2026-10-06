//go:build browser

package history

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// geometryJS is the rule a switch falls back to, written again here so
// the drive checks the script rather than agreeing with it: among the
// anchors whose top is at or above the reading line (the scroll
// padding plus a pixel), those with the greatest top, the first of
// them in document order.
const geometryJS = `(() => {
  const line = parseFloat(getComputedStyle(document.documentElement).scrollPaddingBlockStart) + 1;
  let best = null, top = -Infinity;
  for (const a of document.querySelectorAll("[data-ds-anchor]")) {
    const r = a.getBoundingClientRect();
    if (!r.width && !r.height) continue;
    if (r.top <= line && r.top > top) { best = a; top = r.top; }
  }
  return best ? best.id : "";
})()`

const landingJS = `(() => {
  const id = decodeURIComponent(location.hash.slice(1)), el = id && document.getElementById(id);
  return JSON.stringify({Hash: location.hash, Path: location.pathname, Top: el ? el.getBoundingClientRect().top : -1,
    Line: parseFloat(getComputedStyle(document.documentElement).scrollPaddingBlockStart) + 1,
    End: Math.abs(scrollY + innerHeight - document.documentElement.scrollHeight) < 2, Y: scrollY});
})()`

type landing struct {
	Hash, Path   string
	Top, Line, Y float64
	End          bool
}

// place drives one tab through switches on desktop, where the
// switchers are.
type place struct {
	t   *testing.T
	ctx context.Context
}

func (p *place) run(where string, actions ...chromedp.Action) {
	p.t.Helper()
	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("%s: %v", where, err)
	}
}

// clickWith is a real mouse click on the first match, with a modifier
// or another button: a synthetic click cannot open a tab, and the legs
// below are about what a real Ctrl-click and middle-click do. It clicks
// at the match's centre rather than through chromedp.MouseClickNode,
// which scrolls the page to the node first and so moves the very place
// a click from the pinned bar is meant to carry. A match outside the
// viewport, a rail link, is brought to its nearest edge first.
func (p *place) clickWith(where, sel string, opts ...chromedp.MouseOption) {
	p.t.Helper()
	var at []float64
	p.run(where, chromedp.Evaluate(`(() => { const el = document.querySelector(`+strconv.Quote(sel)+`), r0 = el.getBoundingClientRect();
	  if (r0.top < 0 || r0.bottom > innerHeight) el.scrollIntoView({block: "nearest"});
	  const r = el.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`, &at))
	p.run(where, chromedp.MouseClickXY(at[0], at[1], opts...))
}

// frames waits two animation frames: the record is written one frame
// after this document puts a target in place.
func (p *place) frames(where string) {
	p.t.Helper()
	p.run(where, chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))`, nil,
		func(e *runtime.EvaluateParams) *runtime.EvaluateParams { return e.WithAwaitPromise(true) }))
}

// open loads url as a document of its own. It goes by about:blank
// because navigating to the page already open with only a fragment
// added scrolls within it and loads nothing, and the legs below that
// load at a fragment are about what a fresh load does.
func (p *place) open(where, url string) {
	p.t.Helper()
	p.run(where, chromedp.EmulateViewport(1280, 900), chromedp.Navigate("about:blank"), chromedp.Navigate(url), chromedp.WaitReady(`.ds-top`, chromedp.ByQuery))
	until(p.t, p.ctx, where, `document.readyState === "complete"`)
	p.frames(where)
}

func (p *place) geometry(where string) string {
	p.t.Helper()
	var id string
	p.run(where, chromedp.Evaluate(geometryJS, &id))
	return id
}

// follow clicks a rail link to an anchor on this page, a real followed
// fragment.
func (p *place) follow(where, id string) {
	p.t.Helper()
	p.run(where, chromedp.Evaluate(fmt.Sprintf(`document.querySelector('#ds-nav a[href$="#%s"]').click(); true`, id), nil))
	p.frames(where)
}

// switchTheme clicks the bar's link to the next theme and waits for
// that theme's page; switchLanguage does the same through the language
// menu. Both report where the new page landed.
func (p *place) switchTheme(where string) landing {
	p.t.Helper()
	var next string
	p.run(where, chromedp.Evaluate(`(() => { const a = document.querySelector('.ds-top__controls [rst-seg-tabs] a:not([aria-current])'); a.click(); return a.textContent; })()`, &next))
	p.run(where, chromedp.WaitReady(`link[href$="/theme-`+next+`.css"]`, chromedp.ByQuery))
	return p.landed(where)
}

func (p *place) switchLanguage(where, code string) landing {
	p.t.Helper()
	p.run(where, chromedp.Evaluate(`(() => { const d = document.querySelector(".ds-top__controls [rst-locale]"); d.open = true; d.querySelector('a[lang="`+code+`"]').click(); return true; })()`, nil))
	p.run(where, chromedp.WaitReady(`html[lang="`+code+`"]`, chromedp.ByQuery))
	return p.landed(where)
}

func (p *place) landed(where string) landing {
	p.t.Helper()
	until(p.t, p.ctx, where, `document.readyState === "complete"`)
	var raw string
	p.run(where, chromedp.Evaluate(landingJS, &raw))
	var l landing
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		p.t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return l
}

// lands asserts a switch landed on id: the fragment names it, and its
// top is on the reading line, or the page is scrolled to its end with
// the top below the line, because the browser clamps at the end.
func (p *place) lands(where string, l landing, id string) {
	p.t.Helper()
	if l.Hash != "#"+id {
		p.t.Errorf("%s: landed at %q, want #%s", where, l.Hash, id)
		return
	}
	if math.Abs(l.Top-l.Line) > 2 && !(l.End && l.Top > l.Line) {
		p.t.Errorf("%s: #%s's top is at %.1fpx, not on the reading line at %.1fpx", where, id, l.Top, l.Line)
	}
}

// newTab reports the next page a click opens in a tab of its own.
// chromedp.WaitNewTarget would wait forever here: it takes only targets
// with an opener, and a tab opened by Ctrl- or middle-clicking a link
// has none.
func newTab(ctx context.Context, url func(string) bool) <-chan target.ID {
	ch := make(chan target.ID, 1)
	lctx, cancel := context.WithCancel(ctx)
	chromedp.ListenTarget(lctx, func(ev any) {
		e, ok := ev.(*target.EventTargetCreated)
		if !ok || e.TargetInfo.Type != "page" || e.TargetInfo.Attached || !url(e.TargetInfo.URL) {
			return
		}
		select {
		case ch <- e.TargetInfo.TargetID:
		default:
		}
		cancel()
	})
	return ch
}

// Most of this test's legs only need a frame to have geometry, which
// the headless shell renders fine. The whole test lives in this
// package anyway rather than splitting its last few legs out: the
// "canonical" section near the end checks a Back that must be a real
// back/forward cache restore, and the "only a followed link renews"
// loop's Ctrl-click leg needs the click to actually open a tab, as a
// plain click on a real browser never would. Both need full Chromium,
// and newTab, clickWith and the rest of this file's helpers exist only
// for this one test.
func TestPositionSurvivesASwitch(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	p := &place{t: t, ctx: ctx}
	form := func(theme, locale string) string {
		return rig.Origin + pageHref(mountPath, theme, locale, fileOf("form"))
	}
	// Form's last partial, read off the page: the case where the page
	// clamps before the target reaches the reading line.
	var last string
	p.open("finding the last partial", form("day", "en"))
	p.run("finding the last partial", chromedp.Evaluate(`[...document.querySelectorAll("article.ds-partial[data-ds-anchor]")].pop().id`, &last))

	// A section on the reading line survives a theme switch and then a
	// language switch.
	p.open("scrolled", form("day", "en"))
	p.run("scrolled", chromedp.Evaluate(`document.getElementById("partial-field-select").scrollIntoView({block: "start"}); true`, nil))
	p.lands("scrolled, then theme", p.switchTheme("scrolled, then theme"), "partial-field-select")
	p.lands("scrolled, then language", p.switchLanguage("scrolled, then language", "ja"), "partial-field-select")

	// A followed rail fragment, then theme, then language, with no
	// scrolling in between: the same section every time.
	p.open("followed", form("day", "en"))
	p.follow("followed", "partial-field-check")
	p.lands("followed, then theme", p.switchTheme("followed, then theme"), "partial-field-check")
	p.lands("followed, then language", p.switchLanguage("followed, then language", "ga"), "partial-field-check")

	// The two cases geometry cannot answer. The last partial: the page
	// clamps before it reaches the line, so geometry would pick the one
	// above it.
	p.open("last", form("day", "en"))
	p.follow("last", last)
	p.lands("the last partial", p.switchTheme("the last partial"), last)
	// And rule 1 lets go: a screen's scroll later the id is geometry's.
	p.open("last, scrolled", form("day", "en"))
	p.follow("last, scrolled", last)
	p.run("last, scrolled", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
	want := p.geometry("last, scrolled")
	p.lands("last, scrolled, then theme", p.switchTheme("last, scrolled, then theme"), want)

	// Icons: a row of glyphs shares one top. Following the first glyph
	// of the second row lands on it, not on the row's last.
	icons := rig.Origin + pageHref(mountPath, "day", "en", fileOf("icons"))
	p.open("icons", icons)
	var glyph string
	p.run("icons", chromedp.Evaluate(`(() => { const all = [...document.querySelectorAll(".ds-icons > [data-ds-anchor]")];
	  const firstTop = all[0].getBoundingClientRect().top; return all.find(a => a.getBoundingClientRect().top > firstTop + 1).id; })()`, &glyph))
	p.follow("icons", glyph)
	p.lands("icons, a row's first glyph", p.switchTheme("icons, a row's first glyph"), glyph)

	// The record's start, a fresh load at the last partial: the browser
	// clamps there too, so only the record written at load can name it.
	p.open("loaded, no view stored", form("day", "en")+"#"+last)
	if g := p.geometry("loaded, no view stored"); g == last {
		t.Fatalf("loaded, no view stored: geometry also picks %s, so this leg cannot tell the load record from rule 2", last)
	}
	p.lands("loaded, no view stored, then theme", p.switchTheme("loaded, no view stored, then theme"), last)
	// Code stored as the page-wide view, Form loaded at the last
	// partial, and switched at once: that partial, because gallery.js
	// put the target in place after Code changed the layout. Geometry
	// agrees here, so this checks what the reader sees, not the record;
	// the leg above with no view stored is the one that guards it.
	p.open("code stored", form("day", "en"))
	p.run("code stored", chromedp.Evaluate(`localStorage.setItem("rst-ds-view", "code"); true`, nil))
	p.open("loaded at the last partial", form("day", "en")+"#"+last)
	p.lands("loaded at the last partial, then theme", p.switchTheme("loaded at the last partial, then theme"), last)
	// The same load, a screen up, then a reload: a restored scroll is
	// not a fragment the reader is on, so geometry decides.
	p.open("reloaded", form("day", "en")+"#"+last)
	p.run("reloaded", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil), chromedp.Reload(), chromedp.WaitReady(`.ds-top`, chromedp.ByQuery))
	until(t, ctx, "reloaded", `document.readyState === "complete"`)
	p.frames("reloaded")
	want = p.geometry("reloaded")
	p.lands("reloaded, then theme", p.switchTheme("reloaded, then theme"), want)
	p.run("code cleared", chromedp.Evaluate(`localStorage.removeItem("rst-ds-view"); true`, nil))

	// The record's renewal: following the same fragment again after a
	// layout change above it (no hashchange fires) puts it back.
	p.open("renewed", form("day", "en"))
	p.follow("renewed", last)
	p.run("renewed", chromedp.Evaluate(`document.querySelector(".ds-view__tab--m input").click(); true`, nil))
	p.follow("renewed", last)
	p.lands("renewed, then theme", p.switchTheme("renewed, then theme"), last)

	// History is not a destination: Back restores a scrolled position,
	// not the target's, so the address naming the last partial again
	// does not make it the place.
	p.open("history", form("day", "en"))
	p.follow("history", last)
	p.run("history", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
	p.follow("history", "partial-field-text")
	p.run("history", chromedp.Evaluate(`history.back(); true`, nil))
	until(t, ctx, "history, back", `location.hash === "#`+last+`"`)
	p.frames("history, back")
	want = p.geometry("history, back")
	if want == last {
		t.Fatalf("history, back: geometry also picks %s, so this leg cannot tell rule 1 from rule 2; scroll further", last)
	}
	p.lands("history, back, then theme", p.switchTheme("history, back, then theme"), want)
	// And Forward, the same journey on a fresh page: Back to the last
	// partial with the reader scrolled away, then Forward to the other,
	// all within one document, then switch.
	p.open("history, forward", form("day", "en"))
	p.follow("history, forward", last)
	p.run("history, forward", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
	p.follow("history, forward", "partial-field-text")
	p.run("history, forward", chromedp.Evaluate(`scrollBy(0, innerHeight); history.back(); true`, nil))
	until(t, ctx, "history, forward, back", `location.hash === "#`+last+`"`)
	p.run("history, forward", chromedp.Evaluate(`history.forward(); true`, nil))
	until(t, ctx, "history, forward", `location.hash === "#partial-field-text"`)
	p.frames("history, forward")
	want = p.geometry("history, forward")
	p.lands("history, forward, then theme", p.switchTheme("history, forward, then theme"), want)

	// A traversal drops the record even when it restores the very spot:
	// Back to the page's top and Forward to the last partial puts the
	// page at its end again, the target exactly where the record says,
	// and still the place is geometry's.
	p.open("history, round trip", form("day", "en"))
	p.follow("history, round trip", last)
	p.run("history, round trip", chromedp.Evaluate(`history.back(); true`, nil))
	until(t, ctx, "history, round trip, back", `location.hash === ""`)
	p.run("history, round trip", chromedp.Evaluate(`history.forward(); true`, nil))
	until(t, ctx, "history, round trip, forward", `location.hash === "#`+last+`"`)
	p.frames("history, round trip, forward")
	want = p.geometry("history, round trip, forward")
	if want == last {
		t.Fatalf("history, round trip: geometry also picks %s, so this leg cannot tell rule 1 from rule 2", last)
	}
	p.lands("history, round trip, then theme", p.switchTheme("history, round trip, then theme"), want)

	// Only a followed link renews: a Ctrl-click opens a tab and moves
	// nothing here; a click another listener cancels moves nothing either.
	for _, how := range []string{"ctrl-click", "cancelled"} {
		where := "only a followed link, " + how
		p.open(where, form("day", "en"))
		p.follow(where, last)
		p.run(where, chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
		sel := `#ds-nav a[href$="#` + last + `"]`
		if how == "ctrl-click" {
			p.run(where, chromedp.Evaluate(`document.querySelector('`+sel+`').closest("details").open = true; true`, nil))
			p.clickWith(where, sel, chromedp.ButtonModifiers(input.ModifierCtrl))
		} else {
			p.run(where, chromedp.Evaluate(`document.addEventListener("click", e => e.preventDefault(), {once: true}); document.querySelector('`+sel+`').click(); true`, nil))
		}
		p.frames(where)
		want := p.geometry(where)
		p.lands(where+", then theme", p.switchTheme(where+", then theme"), want)
	}

	// A fragment that is not an anchor (the skip link's
	// #main, a typo) is never carried, and never decides the place.
	for _, frag := range []string{"main", "nope"} {
		where := "loaded at #" + frag
		p.open(where, form("day", "en")+"#"+frag)
		want := p.geometry(where)
		l := p.switchTheme(where + ", then theme")
		if want == "" && l.Hash != "" || want != "" && l.Hash != "#"+want {
			t.Errorf("%s: the switch carried %q, want %q", where, l.Hash, map[bool]string{true: "", false: "#" + want}[want == ""])
		}
	}

	// The switcher's own address is never left changed. A Ctrl-click
	// from deep in the page opens the computed place in a new tab and
	// leaves this link canonical; a middle-click from the same place
	// opens the link as rendered, the other page's top; a plain click
	// from above every anchor lands at the other page's top; and a page
	// back from the back/forward cache, left by a click that carried a
	// place, still has canonical links. The middle-click follows the
	// Ctrl-click on the same page from the same place, so the two tabs
	// differ only in the button: one opens the computed place, the other
	// the link as rendered.
	where := "canonical"
	theme := `.ds-top__controls [rst-seg-tabs] a:not([aria-current])`
	other := func(url string) bool {
		return strings.Contains(url, "/form.html") && !strings.Contains(url, "/day/")
	}
	tabURL := func(how string, opened <-chan target.ID) string {
		t.Helper()
		select {
		case id := <-opened:
			tab, closeTab := chromedp.NewContext(ctx, chromedp.WithTargetID(id))
			defer closeTab()
			// A tab that has not committed yet is about:blank, which has
			// no "#" either, so the middle-click check would pass on a
			// page that never loaded. Wait for the destination, then
			// require it.
			until(t, tab, where+", the "+how+" tab", `location.href.includes("/form.html") && !location.href.includes("/day/")`)
			var url string
			if err := chromedp.Run(tab, chromedp.Location(&url)); err != nil {
				t.Fatalf("%s: reading the %s tab: %v", where, how, err)
			}
			if !other(url) {
				t.Fatalf("%s: the %s tab is at %s, not the other theme's Form", where, how, url)
			}
			return url
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: %s a theme link opened no tab", where, how)
		}
		return ""
	}
	deep := `document.getElementById("partial-field-select").scrollIntoView({block: "start"}); true`
	canonical := `![...document.querySelectorAll(".ds-top__controls a")].some(a => a.getAttribute("href").includes("#"))`
	p.open(where, form("day", "en"))
	p.run(where, chromedp.Evaluate(deep, nil))
	opened := newTab(ctx, other)
	p.clickWith(where, theme, chromedp.ButtonModifiers(input.ModifierCtrl))
	if url := tabURL("Ctrl-clicking", opened); !strings.HasSuffix(url, "#partial-field-select") {
		t.Errorf("%s: the Ctrl-clicked tab opened %s, want the computed place #partial-field-select", where, url)
	}
	until(t, ctx, where+", restored", canonical)
	p.run(where, chromedp.Evaluate(deep, nil))
	middle := newTab(ctx, other)
	p.clickWith(where, theme, chromedp.ButtonMiddle)
	if url := tabURL("middle-clicking", middle); strings.Contains(url, "#") {
		t.Errorf("%s: a middle-click opened %s; it reads the link as rendered, the top of the page", where, url)
	}
	p.run(where, chromedp.Evaluate(`scrollTo(0, 0); true`, nil))
	top := p.switchTheme(where + ", from the top")
	if top.Hash != "" || top.Y != 0 {
		t.Errorf("%s: a plain click from above every anchor landed at %q, scrolled %.0fpx; want the other page's top", where, top.Hash, top.Y)
	}
	p.run(where, chromedp.Evaluate(`history.back(); true`, nil))
	until(t, ctx, where+", back", `location.pathname.includes("/day/") && document.readyState === "complete"`)
	// A freshly loaded page renders canonical links whatever the
	// switcher does, so the last Back has to be a cache restore or its
	// check passes on nothing. A restored document keeps its navigation
	// entry; a reload starts a new one.
	const started = `performance.getEntriesByType("navigation")[0].startTime + performance.timeOrigin`
	var left float64
	p.run(where, chromedp.Evaluate(started, &left))
	p.run(where, chromedp.Evaluate(deep, nil))
	if l := p.switchTheme(where + ", deep"); l.Hash != "#partial-field-select" {
		t.Errorf("%s: a plain click from #partial-field-select landed at %q", where, l.Hash)
	}
	p.run(where, chromedp.Evaluate(`history.back(); true`, nil))
	until(t, ctx, where+", back again", `location.pathname.includes("/day/") && `+canonical)
	var back float64
	p.run(where, chromedp.Evaluate(started, &back))
	if back != left {
		t.Fatalf("%s: the last Back was not a back/forward cache restore (navigation %s then %s), so its canonical links prove nothing. The tree handler must not send Cache-Control: no-store", where, strconv.FormatFloat(left, 'f', 0, 64), strconv.FormatFloat(back, 'f', 0, 64))
	}
}
