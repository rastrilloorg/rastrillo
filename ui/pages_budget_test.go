//go:build !race

package ui

import (
	"html/template"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/bn"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
	_ "amadan.net/rastrillo/rastrillo/locales/ga"
	_ "amadan.net/rastrillo/rastrillo/locales/hi"
	_ "amadan.net/rastrillo/rastrillo/locales/ja"
	_ "amadan.net/rastrillo/rastrillo/locales/pt"
	_ "amadan.net/rastrillo/rastrillo/locales/ru"
	_ "amadan.net/rastrillo/rastrillo/locales/vi"
	_ "amadan.net/rastrillo/rastrillo/locales/yue"
	_ "amadan.net/rastrillo/rastrillo/locales/zh"
)

// Spec §10's budgets 1 to 3. They measure memory, so they are not
// parallel and are left out of -race builds, whose instrumentation
// changes allocation. Every measurement first proves the work it
// measures succeeded: a template that fails early allocates less, and a
// budget must never pass because a page broke.

// budgetConfig is the scaffold's shell and pages (overview, index,
// errors, and a representative form page built from ui's partials) plus
// the fixture app's corpus page, with the fixture's app catalogs.
func budgetConfig(t testing.TB, cfg locales.Config) PagesConfig {
	app := pagesApp(t)
	return PagesConfig{
		App: app, Layout: "scaffold/layout.html", Shared: []string{"who.html"},
		Pages: map[string]PageFile{
			"overview": {Path: "scaffold/overview.html"}, "index": {Path: "scaffold/index.html"},
			"errors": {Path: "scaffold/errors.html"}, "form": {Path: "scaffold/form.html"},
			"corpus": {Path: "corpus.html"},
		},
		Locale: cfg, LocaleFS: app,
		Funcs: template.FuncMap{"asset": func(p string) string { return "/" + p }},
	}
}

// budgetMarkers maps each page to a framework string that page shows,
// in id's language, as html/template writes it.
func budgetMarkers(id string) map[string]string {
	cat := rastrillo.BaseCatalogs()[id]
	m := func(key string) string { return template.HTMLEscapeString(cat[key]) }
	return map[string]string{
		"overview": m("rastrillo.ui.shell_skip"),
		"index":    m("rastrillo.ui.shell_skip"),
		"errors":   m("rastrillo.ui.error_500_title"),
		"form":     m("rastrillo.ui.date_set"),
		"corpus":   m("rastrillo.ui.date_set"),
	}
}

// budgetData is the data each page's handler would pass.
func budgetData(l *Localizer, page string) any {
	switch page {
	case "errors":
		return map[string]any{"L": l, "Status": 500, "Ref": "r1"}
	case "form":
		return map[string]any{"L": l, "Count": 1234567, "Price": int64(128450), "Currency": "EUR", "Name": "Ana"}
	case "corpus":
		return pageFor(l, "u")
	}
	return overviewData(l)
}

// overviewData is the scaffold overview handler's data: nothing but the
// localizer, which the one-tree baseline does not have.
func overviewData(l *Localizer) map[string]any {
	if l == nil {
		return map[string]any{}
	}
	return map[string]any{"L": l}
}

// servedAll builds Pages for budgetConfig with the given offered
// locales, executes every page in every locale, and fails the test
// unless each execution succeeds and contains that page's marker in
// that locale's language.
func servedAll(t *testing.T, offered []string) *Pages {
	t.Helper()
	cfg, err := locales.CheckConfig("en", offered)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPages(budgetConfig(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range offered {
		for page, marker := range budgetMarkers(id) {
			if marker == "" {
				t.Fatalf("%s/%s: no marker; the framework catalog lacks the key", id, page)
			}
			var b strings.Builder
			if err := p.Execute(&b, page, budgetData(p.Localizer(id), page)); err != nil {
				t.Fatalf("%s/%s: %v", id, page, err)
			}
			if !strings.Contains(b.String(), marker) {
				t.Fatalf("%s/%s rendered without %q; a broken page cannot be measured", id, page, marker)
			}
		}
	}
	return p
}

func mustLocales(t *testing.T, codes []string) *rastrillo.Locales {
	t.Helper()
	l, err := rastrillo.NewLocales(codes, "en", nil, pagesApp(t))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func heapAfter(f func() any) (int64, any) {
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	v := f()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	return int64(after.HeapAlloc) - int64(before.HeapAlloc), v
}

// Budgets 1 and 2, measured on the real structure at 1 and 12 offered
// locales. Pages retains templates (which must not grow per locale),
// one localizer per locale, and the catalogs. The catalogs alone are
// measured with the same codes through rastrillo.NewLocales, so the
// localizer's share is what remains.
func TestPagesBudgetMemoryAtOneAndTwelveLocales(t *testing.T) {
	twelve := rastrillo.BaseLocales()
	// The first render in a locale fills process-wide caches, such as
	// currency's per-locale resolution cache. The process pays for them
	// once whatever Pages exist, and unwarmed they land on whichever
	// measurement runs first: 2 to 7 KB per locale, against under 200
	// B that Pages itself retains.
	servedAll(t, twelve)
	pages1, keep1 := heapAfter(func() any { return servedAll(t, []string{"en"}) })
	pages12, keep12 := heapAfter(func() any { return servedAll(t, twelve) })
	cat1, keepC1 := heapAfter(func() any { return mustLocales(t, []string{"en"}) })
	cat12, keepC12 := heapAfter(func() any { return mustLocales(t, twelve) })
	runtime.KeepAlive([]any{keep1, keep12, keepC1, keepC12})

	templates1, templates12 := pages1-cat1, pages12-cat12
	perLocale := (float64(templates12) - float64(templates1)) / 11
	t.Logf("Pages: %d B at 1 locale, %d B at 12; catalogs %d and %d; per extra locale beyond catalogs %.0f B",
		pages1, pages12, cat1, cat12, perLocale)
	if d := math.Abs(float64(templates12)-float64(templates1)) / float64(templates1); d > 0.10 {
		t.Errorf("template memory %d B at 1 locale, %d B at 12 (%.0f%%); budget 10%%", templates1, templates12, d*100)
	}
	if perLocale > 4096 {
		t.Errorf("each offered locale costs %.0f B beyond its catalog; budget 4 KB (1.4 KB measured in the spec)", perLocale)
	}
}

// oneTreeOverview is the baseline: ui's partials and the scaffold's
// unmigrated layout and overview on one escaped tree with ui.Funcs, no
// localization at all. T is one lookup in a catalog read once, the
// least English can cost: Funcs()'s default T copies the whole base
// catalog on every call (BaseCatalog returns a copy), which makes this
// page about 70 KB a request and would let the budget pass however
// much Pages allocated.
func oneTreeOverview(t *testing.T) *template.Template {
	t.Helper()
	en := rastrillo.BaseCatalog()
	funcs := Funcs(WithT(func(key string, _ ...any) string { return en[key] }))
	funcs["asset"] = func(p string) string { return "/" + p }
	base, err := template.New("layout").Funcs(funcs).ParseFS(Templates(), "*.html")
	if err == nil {
		base, err = base.ParseFS(os.DirFS("testdata/pages"), "scaffold-legacy/layout.html", "scaffold-legacy/overview.html")
	}
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// Budget 3: per request, the scaffold overview page through Pages is
// within 1.5x the bytes of the same page on one escaped tree with no
// localization; Pages.Localizer allocates nothing.
func TestPagesBudgetPerRequest(t *testing.T) {
	p := servedAll(t, []string{"es", "en"})
	base := oneTreeOverview(t)
	es := p.Localizer("es")
	shared := bytesPerRun(t, func(w io.Writer) error {
		return p.Execute(w, "overview", overviewData(es))
	}, budgetMarkers("es")["overview"])
	oneTree := bytesPerRun(t, func(w io.Writer) error {
		return base.ExecuteTemplate(w, "layout", overviewData(nil))
	}, budgetMarkers("en")["overview"])
	t.Logf("overview page: %d bytes per request through Pages, %d on one tree", shared, oneTree)
	if float64(shared) > 1.5*float64(oneTree) {
		t.Errorf("overview page: %d bytes per request, one-tree baseline %d; budget 1.5x", shared, oneTree)
	}
	if n := testing.AllocsPerRun(1000, func() { p.Localizer("es") }); n != 0 {
		t.Errorf("Pages.Localizer allocates %v", n)
	}
}

// bytesPerRun checks one run renders correctly, then measures 200 into
// io.Discard. Not into a strings.Builder: Pages hands its writer one
// finished buffer, while the one-tree baseline writes piece by piece and
// a Builder doubles its way up under it, nearly doubling the baseline's
// bytes and loosening the 1.5x budget to about 2.4x.
func bytesPerRun(t *testing.T, render func(io.Writer) error, marker string) uint64 {
	t.Helper()
	var out strings.Builder
	if err := render(&out); err != nil || !strings.Contains(out.String(), marker) {
		t.Fatalf("the measured render failed (%v) or lacks %q; refusing to measure it", err, marker)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 200; i++ {
		if err := render(io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 200
}
