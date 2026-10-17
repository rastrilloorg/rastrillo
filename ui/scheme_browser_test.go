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
