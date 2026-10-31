package corpus

import (
	"embed"
	"html/template"
	"io"
	"io/fs"
	"log/slog"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
	"amadan.net/rastrillo/rastrillo/tmpl"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The ui cases are the sets ui's own tests build from ui/testdata/pages,
// which testdata/uipages copies (TestUIPagesCopyIsCurrent keeps the two
// the same): ui's partials, a layout and the fixture pages, made by
// ui.NewPages with the configuration and data those tests use. The sets
// are made inside ui, but tmpl gives them to the first caller outside
// it, so their compiled code is in this package.

//go:embed testdata/uipages
var uiPagesFS embed.FS

func uiApp() fs.FS {
	sub, err := fs.Sub(uiPagesFS, "testdata/uipages")
	if err != nil {
		panic(err) // embedded above; this cannot fail
	}
	return sub
}

// PagesCase is a ui.Pages and the pages to render with it.
type PagesCase struct {
	Name  string
	Build func() (*ui.Pages, error)
	// Runs lists what to render, with data that needs the Pages for its
	// localizers.
	Runs func(pages *ui.Pages) []PageRun
}

// PageRun is one page rendered with one value.
type PageRun struct {
	Page string
	Data any
}

// PagesCases returns every ui case.
func PagesCases() []PagesCase {
	return []PagesCase{fixturePages(false), fixturePages(true), scaffoldPages()}
}

// quiet is the logger the cases give ui: compat mode logs on purpose.
var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// pagesConfig is ui's pagesConfig in pages_test.go.
func pagesConfig(def string, compat bool) (ui.PagesConfig, error) {
	cfg, err := locales.CheckConfig(def, []string{"en", "es"})
	if err != nil {
		return ui.PagesConfig{}, err
	}
	app := uiApp()
	return ui.PagesConfig{
		App: app, Layout: "layout.html", Shared: []string{"who.html"},
		Pages: map[string]ui.PageFile{
			"corpus": {Path: "corpus.html"},
			"anon":   {Path: "anon.html"},
			"legacy": {
				Path: "legacy.html",
			},
			"nil":    {Path: "nil.html"},
			"nilnew": {Path: "nilnew.html"},
			"arity":  {Path: "arity.html"},
			"errors": {Path: "errors.html"},
		},
		Locale:   cfg,
		LocaleFS: app,
		Funcs: template.FuncMap{
			"asset": func(path string) string { return "/static/" + path },
			"icon": func(name string) template.HTML {
				return template.HTML("<i>" + name + "</i>")
			},
		},
		Compat: compat,
		Logger: quiet,
	}, nil
}

// The page data of ui's pages_test.go.
type pagePerson struct {
	ui.Localized
	Name string
}

type pageAnon struct{ Name string }

type pageData struct {
	ui.Localized
	Evil, URL, User, Cur string
	N                    int
	Price                int64
	Person               pagePerson
	Anon                 pageAnon
	Nil                  any
}

func pageFor(localizer *ui.Localizer, user string) pageData {
	return pageData{
		Localized: ui.Localized{L: localizer},
		Evil:      `<script>"x"</script>`,
		URL:       "javascript:alert(1)",
		User:      user,
		Cur:       "EUR",
		N:         1234567,
		Price:     128450,
		Person:    pagePerson{ui.Localized{L: localizer}, "Ana"},
		Anon:      pageAnon{"Bo"},
	}
}

// fixturePages is the fixture app in strict mode (TestPagesCorpusExecutes,
// TestPagesStrictFailures) or in compat mode with a Spanish default
// (TestPagesCompatUsesTheAppsDefault, TestPagesNilValues).
func fixturePages(compat bool) PagesCase {
	name, def := "ui-pages-strict", "en"
	if compat {
		name, def = "ui-pages-compat", "es"
	}
	return PagesCase{
		Name: name,
		Build: func() (*ui.Pages, error) {
			cfg, err := pagesConfig(def, compat)
			if err != nil {
				return nil, err
			}
			return ui.NewPages(cfg)
		},
		Runs: func(pages *ui.Pages) []PageRun {
			return inBothLanguages(pages, fixtureRuns)
		},
	}
}

// inBothLanguages is runs for Spanish's localizer, then for English's.
func inBothLanguages(
	pages *ui.Pages,
	runs func(localizer *ui.Localizer) []PageRun,
) []PageRun {
	var all []PageRun
	for _, lang := range []string{"es", "en"} {
		all = append(all, runs(pages.Localizer(lang))...)
	}
	return all
}

// fixtureRuns are the fixture app's pages, with localizer.
func fixtureRuns(localizer *ui.Localizer) []PageRun {
	data := pageFor(localizer, "u")
	special := pageFor(localizer, specials)
	special.Evil, special.Cur, special.Nil = specials, specials, specials
	typed := pageFor(localizer, "t")
	typed.Nil = template.HTML("<b>trusted</b>")
	return []PageRun{
		{"corpus", data},
		{"corpus", &data},
		{"corpus", special},
		{"corpus", typed},
		{"anon", data},
		{"legacy", data},
		{"arity", data},
		{"nil", data},
		{"nilnew", data},
		{"nilnew", typed},
		{"errors", map[string]any{"L": localizer, "Status": 500, "Ref": "r1"}},
		{"errors", map[string]any{"L": localizer, "Status": 404}},
		{"errors", map[string]any{
			"L":      localizer,
			"Status": "<odd>",
			"Ref":    specials,
		}},
	}
}

// scaffoldPages is budgetConfig in pages_budget_test.go: the scaffold's
// shell and pages, offered in two locales as TestPagesBudgetPerRequest
// offers them, with budgetData's data.
func scaffoldPages() PagesCase {
	return PagesCase{
		Name:  "ui-pages-scaffold",
		Build: buildScaffoldPages,
		Runs: func(pages *ui.Pages) []PageRun {
			return inBothLanguages(pages, scaffoldRuns)
		},
	}
}

// buildScaffoldPages makes the scaffold's Pages.
func buildScaffoldPages() (*ui.Pages, error) {
	cfg, err := locales.CheckConfig("en", []string{"es", "en"})
	if err != nil {
		return nil, err
	}
	app := uiApp()
	return ui.NewPages(ui.PagesConfig{
		App:    app,
		Layout: "scaffold/layout.html",
		Shared: []string{"who.html"},
		Pages: map[string]ui.PageFile{
			"overview": {Path: "scaffold/overview.html"},
			"index":    {Path: "scaffold/index.html"},
			"errors":   {Path: "scaffold/errors.html"},
			"form":     {Path: "scaffold/form.html"},
			"corpus":   {Path: "corpus.html"},
		},
		Locale: cfg, LocaleFS: app,
		Funcs: template.FuncMap{
			"asset": func(path string) string { return "/" + path },
		},
		Logger: quiet,
	})
}

// scaffoldRuns are the scaffold's pages, with localizer.
func scaffoldRuns(localizer *ui.Localizer) []PageRun {
	return []PageRun{
		{"overview", map[string]any{"L": localizer}},
		{"overview", map[string]any{"L": localizer, "Base": "/base"}},
		{"index", map[string]any{"L": localizer}},
		{"errors", map[string]any{"L": localizer, "Status": 500, "Ref": "r1"}},
		{"errors", map[string]any{
			"L":      localizer,
			"Status": 404,
			"Ref":    specials,
		}},
		{"form", map[string]any{
			"L":        localizer,
			"Count":    1234567,
			"Price":    int64(128450),
			"Currency": "EUR",
			"Name":     "Ana",
		}},
		{"form", map[string]any{
			"L":        localizer,
			"Count":    0,
			"Price":    int64(0),
			"Currency": "<x>",
			"Name":     specials,
		}},
		{"corpus", pageFor(localizer, "u")},
	}
}

// uiLegacy is oneTreeOverview in pages_budget_test.go: ui's partials and
// the scaffold's unmigrated layout and overview on one set with
// ui.Funcs and no localizer.
func uiLegacy() Case {
	funcs := func() template.FuncMap {
		en := rastrillo.BaseCatalog()
		funcs := ui.Funcs(
			ui.WithT(func(key string, _ ...any) string { return en[key] }),
		)
		funcs["asset"] = func(path string) string { return "/" + path }
		return funcs
	}
	files := []string{
		"scaffold-legacy/layout.html",
		"scaffold-legacy/overview.html",
	}
	return Case{
		Name:  "ui-scaffold-legacy",
		Entry: "layout",
		Data: []any{
			map[string]any{},
			nil,
			map[string]any{"Base": "/b"},
			map[string]any{"Base": specials},
		},
		Build: func() (*tmpl.Template, error) {
			template, err := tmpl.New("layout").
				Funcs(funcs()).
				ParseFS(ui.Templates(), "*.html")
			if err != nil {
				return nil, err
			}
			return template.ParseFS(uiApp(), files...)
		},
		std: func() (*template.Template, error) {
			template, err := template.New("layout").
				Funcs(funcs()).
				ParseFS(ui.Templates(), "*.html")
			if err != nil {
				return nil, err
			}
			return template.ParseFS(uiApp(), files...)
		},
	}
}
