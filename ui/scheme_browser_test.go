//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/locales"
)

// schemeApp is the smallest real app the switch runs in: the column
// shell rendered through ui.Pages from ForRequest, so data-theme and
// the menu's aria-current come from the cookie exactly as an app's
// would; POST /_theme; and the shipped shim, stylesheet and theme.
func schemeApp(t *testing.T) http.Handler {
	t.Helper()
	src, _ := Layout("column")
	cfg, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	app := fstest.MapFS{
		"layout.html": {Data: src},
		"page.html":   {Data: []byte(`{{define "content"}}<h1>Orders</h1><p>x</p>{{end}}`)},
	}
	p, err := NewPages(PagesConfig{
		App: app, Layout: "layout.html", Pages: map[string]PageFile{"page": {Path: "page.html"}},
		Locale: cfg, LocaleFS: app,
		Funcs: template.FuncMap{
			"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
			"icon":       func(n string) template.HTML { return template.HTML(sizingIcon) },
			"iconAssets": func() template.HTML { return "" },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	stylesheets(t, mux)
	for name, body := range map[string][]byte{"rastrillo.js": ShimJS(), "busy.js": BusyJS(), "anchor.js": AnchorJS(), "select.js": SelectJS(), "datetime.js": DatetimeJS(), "calendar.js": CalendarJS()} {
		body := body
		mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(body)
		})
	}
	mux.Handle("POST "+rastrillo.SchemeSwitchPath, rastrillo.SchemeSwitchHandler())
	mux.Handle("GET /", p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := p.Execute(w, "page", ForRequest(r)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})))
	return mux
}

// schemeState is what the page says about itself after each step.
type schemeState struct {
	Theme   string // data-theme on <html>, "" for none
	Cookie  string // document.cookie
	Current string // the scheme whose button is aria-current
	Open    bool   // the menu is still open
	Mark    bool   // window.__mark survived: no navigation happened
	Navs    int    // navigation entries: 1 means this page load only
}

const schemeStateJS = `(() => {
  const m = document.querySelector("[rst-scheme]");
  const cur = m.querySelector("button[aria-current]");
  return JSON.stringify({
    Theme: document.documentElement.getAttribute("data-theme") || "",
    Cookie: document.cookie,
    Current: cur ? cur.form.elements.scheme.value : "",
    Open: m.open,
    Mark: window.__mark === 1,
    Navs: performance.getEntriesByType("navigation").length
  });
})()`

// With scripts on, a choice applies in place: the attribute changes,
// the cookie is written, the menu closes, and nothing navigates. A
// reload then shows the server painted the same scheme from the
// cookie — the shim never applies the cookie on load, so after a reload
// the attribute can only have come from the server. System undoes all
// of it.
func TestTheSchemeSwitchAppliesInPlaceAndSurvivesAReload(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return schemeApp(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()

	choose := func(scheme string) {
		t.Helper()
		mustRun(t, ctx,
			chromedp.Evaluate(`window.__mark = 1; true`, nil),
			chromedp.Click(`[rst-scheme] > summary`, chromedp.ByQuery),
			chromedp.WaitVisible(fmt.Sprintf(`[rst-scheme] input[value=%q] ~ button`, scheme), chromedp.ByQuery),
			chromedp.Click(fmt.Sprintf(`[rst-scheme] input[value=%q] ~ button`, scheme), chromedp.ByQuery),
			chromedp.Sleep(150*time.Millisecond),
		)
	}
	var got schemeState
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/orders?page=2"), chromedp.WaitVisible(`[rst-scheme]`, chromedp.ByQuery))
	at(t, ctx, schemeStateJS, &got)
	if got.Theme != "" || got.Current != "system" || got.Cookie != "" {
		t.Fatalf("first visit: %+v", got)
	}

	choose("dark")
	at(t, ctx, schemeStateJS, &got)
	if got.Theme != "dark" || got.Current != "dark" || !strings.Contains(got.Cookie, "rastrillo_scheme=dark") || got.Open || !got.Mark || got.Navs != 1 {
		t.Fatalf("after choosing Dark in place: %+v", got)
	}

	mustRun(t, ctx, chromedp.Reload(), chromedp.WaitVisible(`[rst-scheme]`, chromedp.ByQuery))
	at(t, ctx, schemeStateJS, &got)
	if got.Theme != "dark" || got.Current != "dark" || got.Mark {
		t.Fatalf("after a reload the server did not paint the chosen scheme: %+v", got)
	}
	var path string
	mustRun(t, ctx, chromedp.Evaluate(`location.pathname + location.search`, &path))
	if path != "/orders?page=2" {
		t.Errorf("the reader moved to %s", path)
	}

	choose("system")
	at(t, ctx, schemeStateJS, &got)
	if got.Theme != "" || got.Current != "system" || strings.Contains(got.Cookie, "rastrillo_scheme") || !got.Mark {
		t.Fatalf("after choosing System in place: %+v", got)
	}
	mustRun(t, ctx, chromedp.Reload(), chromedp.WaitVisible(`[rst-scheme]`, chromedp.ByQuery))
	at(t, ctx, schemeStateJS, &got)
	if got.Theme != "" || got.Current != "system" {
		t.Fatalf("after a reload System did not hold: %+v", got)
	}
}

// axe over the page with the switch open, in both schemes, held to
// WCAG 2.2 AA the way internal/designsystem's gate holds the gallery.
// The engine is the vendored copy in testdata; it is never shipped.
func TestTheSchemeSwitchPassesAxeInBothSchemes(t *testing.T) {
	engine, err := os.ReadFile(filepath.FromSlash("testdata/axe/axe.min.js"))
	if err != nil {
		t.Fatalf("reading the vendored axe-core: %v", err)
	}
	rig := harness.New(t, func(string) http.Handler { return schemeApp(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(`[rst-scheme]`, chromedp.ByQuery),
		chromedp.Evaluate(string(engine), nil))

	tags, _ := json.Marshal([]string{"wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"})
	run := fmt.Sprintf(`(async () => {
  const res = await axe.run(document, { runOnly: {type: "tag", values: %s}, resultTypes: ["violations"] });
  return JSON.stringify({
    passed: res.passes.length,
    violations: res.violations.map(v => ({ id: v.id, impact: v.impact, help: v.help,
      nodes: v.nodes.map(n => n.target.map(t => Array.isArray(t) ? t.join(" >> ") : String(t)).join(" >> ")) }))
  });
})()`, tags)

	for _, scheme := range []string{"light", "dark"} {
		mustRun(t, ctx,
			chromedp.Evaluate(fmt.Sprintf(`document.documentElement.setAttribute("data-theme", %q); document.querySelector("[rst-scheme]").open = true; true`, scheme), nil),
			chromedp.WaitVisible(`[rst-scheme] button`, chromedp.ByQuery),
		)
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(run, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			t.Fatalf("%s: axe.run: %v", scheme, err)
		}
		var out struct {
			Passed     int
			Violations []struct {
				ID, Impact, Help string
				Nodes            []string
			}
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("%s: decoding %.200q: %v", scheme, raw, err)
		}
		// A scan that examined nothing reports no violations; the
		// designsystem gate's floor of five rules is the proof it ran.
		if out.Passed < 5 {
			t.Errorf("%s: only %d axe rules ran to completion", scheme, out.Passed)
		}
		sort.Slice(out.Violations, func(i, j int) bool { return out.Violations[i].ID < out.Violations[j].ID })
		for _, v := range out.Violations {
			t.Errorf("%s: %s [%s] — %s: %s", scheme, v.ID, v.Impact, v.Help, strings.Join(v.Nodes, "; "))
		}
		t.Logf("%s: %d axe rules passed", scheme, out.Passed)
	}
}

// schemeFlipBody is the page a scheme flip is read on: a sidebar's
// current nav item and a linked media tile, the two things whose
// background fades between schemes (0.2s and 0.25s, for the press
// and the hover) while their text changes at once, and the switch's
// own forms as an app renders them.
const schemeFlipBody = `<div rst-shell-sidebar><nav rst-shell-rail><div rst-shell-nav><a href="#x">Overview</a><a href="#y" aria-current="page" id="nav-cur">Requests</a></div></nav></div>
<div rst-list><div rst-media><div rst-media-tile id="tile"><div rst-media-frame><img src="/images/a.svg" alt=""></div><div rst-media-caption><div rst-media-name><a href="#t">Picture</a></div></div></div></div></div>
<details rst-dropdown rst-scheme><summary>Theme</summary><div rst-dropdown-menu>
<form method="post" action="/_theme"><input type="hidden" name="scheme" value="light"><button>Light</button></form>
<form method="post" action="/_theme"><input type="hidden" name="scheme" value="dark"><button>Dark</button></form>
<form method="post" action="/_theme"><input type="hidden" name="scheme" value="system"><button>System</button></form>
</div></details>`

// schemeFlipState is the two backgrounds and the scheme as read on
// one frame.
type schemeFlipState struct {
	Theme string // data-theme on <html>, "" for none
	Nav   string // the current nav item's computed background
	Tile  string // the linked media tile's computed background
}

// schemeFlipReadJS reads the state on the next animation frame. On
// the next frame rather than now: a transition's first frame is its
// start value, which is the same number the test would read without
// a flip at all.
const schemeFlipReadJS = `new Promise(r => requestAnimationFrame(() => r(JSON.stringify({
  Theme: document.documentElement.getAttribute("data-theme") || "",
  Nav: getComputedStyle(document.getElementById("nav-cur")).backgroundColor,
  Tile: getComputedStyle(document.getElementById("tile")).backgroundColor
}))))`

// schemeFlipLandedJS waits out every finite animation, then reads the
// same state: the value a reader sees once the page has settled.
const schemeFlipLandedJS = `(async () => {
  await Promise.all(document.getAnimations().filter(a => a.effect && a.effect.getComputedTiming().endTime !== Infinity).map(a => a.finished.catch(() => null)));
  return await ` + schemeFlipReadJS + `;
})()`

// A scheme flip lands in the frame it happens in. The current nav
// item and a linked tile fade their background for the press and the
// hover, and a change of data-theme started the same fade while the
// text snapped: for about a tenth of a second after the click, light
// text sat on a still-light ground. Read on the very next frame, both
// backgrounds already equal what they settle to. The same through the
// OS flipping while the page is on System, where no form is involved.
func TestASchemeFlipLandsOnTheNextFrame(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("scheme flip", schemeFlipBody)})
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()
	awaitPromise := func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }
	os := func(scheme string) chromedp.Action {
		return emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}})
	}
	mustRun(t, ctx, os("light"), chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`[rst-scheme]`, chromedp.ByQuery), chromedp.WaitReady(`body`, chromedp.ByQuery))
	// choose submits one of the switch's forms the way a click does,
	// and reads the state on the next frame, in one script so no
	// round trip sits between the two.
	choose := func(scheme string) schemeFlipState {
		t.Helper()
		var got schemeFlipState
		at(t, ctx, fmt.Sprintf(`(() => { document.querySelector('[rst-scheme] input[value=%q] ~ button').click(); return %s; })()`, scheme, schemeFlipReadJS), &got, awaitPromise)
		return got
	}
	landed := func(where string) schemeFlipState {
		t.Helper()
		var got schemeFlipState
		at(t, ctx, schemeFlipLandedJS, &got, awaitPromise)
		t.Logf("%s, landed: %+v", where, got)
		return got
	}
	check := func(where string, got, want schemeFlipState) {
		t.Helper()
		if got.Theme != want.Theme {
			t.Fatalf("%s: data-theme %q, want %q", where, got.Theme, want.Theme)
		}
		if got.Nav != want.Nav || got.Tile != want.Tile {
			t.Errorf("%s: on the next frame the nav item is %s and the tile %s; settled they are %s and %s. The flip faded instead of landing.", where, got.Nav, got.Tile, want.Nav, want.Tile)
		}
	}

	// Learn the settled values of each scheme first, so the test holds
	// the theme's own numbers rather than a copy of them.
	choose("dark")
	dark := landed("dark chosen")
	choose("light")
	light := landed("light chosen")
	if dark.Nav == light.Nav || dark.Tile == light.Tile {
		t.Fatalf("the page does not change with the scheme: dark %+v, light %+v", dark, light)
	}

	check("Light to Dark by the switch", choose("dark"), dark)
	landed("after Dark")
	check("Dark to Light by the switch", choose("light"), light)
	landed("after Light")

	// System, with the OS flipping underneath: the attribute is gone,
	// light-dark() repaints on its own, and no form is submitted. Read
	// on the second frame, not the next: Chromium finds the media
	// change in the style update that starts the fade, and reports it
	// to listeners at the start of the frame after, so the shim can
	// land an OS flip one frame late at best. Without it the second
	// frame is a tenth of the way into the fade, and still light.
	got := choose("system")
	if got.Theme != "" {
		t.Fatalf("after choosing System data-theme is %q", got.Theme)
	}
	landed("system, OS light")
	mustRun(t, ctx, os("dark"))
	at(t, ctx, schemeFlipSecondFrameJS, &got, awaitPromise)
	check("System, OS light to dark", got, schemeFlipState{Nav: dark.Nav, Tile: dark.Tile})
	landed("system, OS dark")
	mustRun(t, ctx, os("light"))
	at(t, ctx, schemeFlipSecondFrameJS, &got, awaitPromise)
	check("System, OS dark to light", got, schemeFlipState{Nav: light.Nav, Tile: light.Tile})
}

// schemeFlipSecondFrameJS reads the state one frame later than
// schemeFlipReadJS, for the OS flip (see the test).
const schemeFlipSecondFrameJS = `new Promise(r => requestAnimationFrame(() => r(` + schemeFlipReadJS + `)))`

// TestAnAppsOwnSchemeSwitchLandsAtRest is Astra A3: the shipped switch
// lands a flip by cancelling the running transitions (land(), above),
// but an app that sets data-theme from its own script, or that took
// the new stylesheets with an old rastrillo.js, never calls it. The
// nav items', linked rows' and tiles' background transitions sit on
// their :hover and :active rules only, not at rest, so at rest a
// scheme change has no transition to start and lands on the next
// frame with no script's help. The page is the scheme-flip fixture;
// the flip is an attribute write from page script, the way an app's
// switch does it, read on the very next frame against the value the
// page settles to.
func TestAnAppsOwnSchemeSwitchLandsAtRest(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("app scheme flip", schemeFlipBody)})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	awaitPromise := func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }
	mustRun(t, ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "light"}}),
		chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(`[rst-scheme]`, chromedp.ByQuery), chromedp.WaitReady(`body`, chromedp.ByQuery))
	// set writes the attribute the way an app's own switch does and
	// reads the next frame, in one script so no round trip sits
	// between the write and the read.
	set := func(scheme string) schemeFlipState {
		t.Helper()
		var got schemeFlipState
		at(t, ctx, fmt.Sprintf(`(() => { document.documentElement.setAttribute("data-theme", %q); return %s; })()`, scheme, schemeFlipReadJS), &got, awaitPromise)
		return got
	}
	settled := func() schemeFlipState {
		t.Helper()
		var got schemeFlipState
		at(t, ctx, schemeFlipLandedJS, &got, awaitPromise)
		return got
	}
	set("dark")
	dark := settled()
	set("light")
	light := settled()
	if dark.Nav == light.Nav || dark.Tile == light.Tile {
		t.Fatalf("the page does not change with the scheme: dark %+v, light %+v", dark, light)
	}
	for _, c := range []struct {
		to   string
		want schemeFlipState
	}{{"dark", dark}, {"light", light}} {
		got := set(c.to)
		if got.Theme != c.to {
			t.Fatalf("data-theme is %q after the app set %q", got.Theme, c.to)
		}
		if got.Nav != c.want.Nav || got.Tile != c.want.Tile {
			t.Errorf("app script sets data-theme=%s: on the next frame the nav item is %s and the tile %s; settled they are %s and %s. The flip faded instead of landing, with no shipped switch to cancel it.", c.to, got.Nav, got.Tile, c.want.Nav, c.want.Tile)
		}
		settled()
	}
}
