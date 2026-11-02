package perftest_test

import (
	"fmt"
	"html/template"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	"amadan.net/rastrillo/rastrillo/perf/perftest"
	"amadan.net/rastrillo/rastrillo/ui"
)

// shellData is a shell page's data: the localizer, and the two fields
// the select and date pages draw.
type shellData struct {
	ui.Localized
	Select, Date map[string]any
}

// framePages are the pages each shell is weighed with: no field, an
// enhanced select, a date field, and a live region.
var framePages = map[string]string{
	"plain": `{{define "content"}}<h1>Orders</h1><p>31 October 2026</p>{{end}}`,
	"select": `{{define "content"}}{{$L := localizer $}}` +
		`{{template "field-select" (withL .Select $L)}}{{end}}`,
	"date": `{{define "content"}}{{$L := localizer $}}` +
		`{{template "field-date" (withL .Date $L)}}{{end}}`,
	"live": `{{define "content"}}<h1>Jobs</h1>{{end}}` +
		`{{define "region:items"}}<p>Running</p>{{end}}`,
}

func twelveOptions() []any {
	var options []any
	for number := 1; number <= 12; number++ {
		options = append(options, map[string]any{
			"Value": fmt.Sprint(number), "Label": fmt.Sprint("Option ", number),
		})
	}
	return options
}

// shellApp is a ui.Pages app over the named shell's layout, serving
// the framework's files for theme through rastrillo.Assets.
func shellApp(t *testing.T, shell, theme string) chi.Router {
	t.Helper()
	layout, _ := ui.Layout(shell)
	files, _ := ui.VendoredAssets(theme)
	static := fstest.MapFS{}
	for name, body := range files {
		static["static/"+name] = &fstest.MapFile{Data: body}
	}
	assets := rastrillo.NewAssets(static)
	app := fstest.MapFS{"layout.html": {Data: layout}}
	pageFiles := map[string]ui.PageFile{}
	for name, body := range framePages {
		app[name+".html"] = &fstest.MapFile{Data: []byte(body)}
		pageFiles[name] = ui.PageFile{Path: name + ".html"}
	}
	config, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ui.NewPages(ui.PagesConfig{
		App: app, Layout: "layout.html", Pages: pageFiles,
		Locale: config, LocaleFS: app,
		Funcs: template.FuncMap{"asset": assets.Path},
	})
	if err != nil {
		t.Fatal(err)
	}
	data := shellData{
		Localized: ui.Localized{L: pages.Localizer("en")},
		Select: map[string]any{"ID": "owner", "Name": "owner",
			"Label": "Owner", "Options": twelveOptions()},
		Date: map[string]any{"ID": "due", "Name": "due", "Label": "Due"},
	}
	router := chi.NewRouter()
	router.Handle("GET /static/*", assets.Handler())
	for name := range framePages {
		router.Get("/"+name, func(
			writer http.ResponseWriter, request *http.Request,
		) {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := pages.Execute(writer, name, data); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		})
	}
	return router
}

// TestEveryShellsPagesMeetTheFirstLoadBudget weighs every shipped
// shell, in every theme, as an app serves it.
func TestEveryShellsPagesMeetTheFirstLoadBudget(t *testing.T) {
	for _, shell := range ui.LayoutNames() {
		for _, theme := range ui.ThemeNames() {
			t.Run(shell+"/"+theme, func(t *testing.T) {
				router := shellApp(t, shell, theme)
				perftest.FirstLoad(t, perftest.ScreenConfig{
					Routes: router, Handler: router,
					Opaque: []perftest.Opaque{{
						Patterns: []string{"/static/*"},
						Screens: []perftest.OpaqueScreen{{
							Key:  "GET /static/tokens.css",
							Path: "/static/tokens.css",
						}},
					}},
				})
			})
		}
	}
}
