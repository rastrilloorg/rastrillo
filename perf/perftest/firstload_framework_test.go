package perftest_test

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	"amadan.net/rastrillo/rastrillo/perf/perftest"
	"amadan.net/rastrillo/rastrillo/ui"
)

// shellData is a shell page's data: the localizer, the two fields the
// select and date pages draw, and the table and charts of the list and
// dashboard pages.
type shellData struct {
	ui.Localized
	Select, Date, Table, Line, Bars map[string]any
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

// dataPages are a list with a data table and a dashboard of stats, a
// line and bars. Each links its slice of data.css (824 and 1,502
// bytes). In the column shell, which rastrillo new writes, they weigh
// 12.9 and 13.1 kB in Day, and in the stage shell under 11 kB. The
// topbar, sidebar and console shells' own chrome takes them to between
// 15.2 and 16.7 kB, which their data.css allowance covers.
var dataPages = map[string]string{
	"list": `{{define "content"}}{{$L := localizer $}}` +
		`{{template "page-header" dict "Title" "Orders"}}` +
		`{{template "data-table" (withL .Table $L)}}{{end}}`,
	"dashboard": `{{define "content"}}{{$L := localizer $}}<div rst-stats>` +
		`{{template "stat" dict "L" $L "Label" "Sold" "Value" 690}}` +
		`{{template "stat" dict "L" $L "Label" "Refunds" "Value" 8}}</div>` +
		`{{template "chart-line" (withL .Line $L)}}` +
		`{{template "chart-bar" (withL .Bars $L)}}{{end}}`,
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

// orders is a list's table: a linked name, numbers, a sorted column
// and a footer of totals.
func orders() map[string]any {
	var rows []any
	for number := 1; number <= 25; number++ {
		rows = append(rows, []any{
			map[string]any{"Value": fmt.Sprint("Order ", number),
				"Href": fmt.Sprint("/orders/", number)},
			number * 3, number * 120})
	}
	return map[string]any{"ID": "orders", "Title": "Orders",
		"Columns": []any{map[string]any{"Label": "Order"},
			map[string]any{"Label": "Items", "Num": true},
			map[string]any{"Label": "Total", "Num": true,
				"Href": "/orders?sort=total", "Sort": "descending"}},
		"Rows": rows, "Foot": []any{"Total", 975, 39000}}
}

// weeklySales is a month of daily sales against a target, with hover.
func weeklySales() map[string]any {
	var points []any
	for day := 1; day <= 31; day++ {
		points = append(points, map[string]any{"Label": day,
			"Value": 100 + day*day%37})
	}
	return map[string]any{"ID": "sales", "Title": "Sales",
		"Desc": "Sales held steady all month.", "Category": "Day",
		"Measure": "Sold", "Points": points, "Goal": 120,
		"GoalLabel": "Target", "Hover": true}
}

func ticketSources() map[string]any {
	var points []any
	for index, source := range []string{"Online", "Box office",
		"Partners", "Newsletter"} {
		points = append(points, map[string]any{"Label": source,
			"Value": 400 - index*100})
	}
	return map[string]any{"ID": "sources", "Title": "Tickets by source",
		"Category": "Source", "Measure": "Tickets", "Points": points}
}

// shellApp is a ui.Pages app over the named shell's layout, serving
// the framework's files for theme through rastrillo.Assets, which its
// pages are given, so each links its slice of tokens.css.
func shellApp(
	t *testing.T, shell, theme string, contents map[string]string,
) (chi.Router, *rastrillo.Assets) {
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
	for name, body := range contents {
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
		Funcs:  template.FuncMap{"asset": assets.Path},
		Assets: assets,
	})
	if err != nil {
		t.Fatal(err)
	}
	data := shellData{
		Localized: ui.Localized{L: pages.Localizer("en")},
		Select: map[string]any{"ID": "owner", "Name": "owner",
			"Label": "Owner", "Options": twelveOptions()},
		Date:  map[string]any{"ID": "due", "Name": "due", "Label": "Due"},
		Table: orders(), Line: weeklySales(), Bars: ticketSources(),
	}
	router := chi.NewRouter()
	router.Handle("GET /static/*", assets.Handler())
	for name := range contents {
		router.Get("/"+name, func(
			writer http.ResponseWriter, request *http.Request,
		) {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := pages.Execute(writer, name, data); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		})
	}
	return router, assets
}

// TestEveryShellsPagesMeetTheFirstLoadBudget weighs every shipped
// shell, in every theme, as an app serves it: sliced, so under the
// sliced limits and with every hook a page shows styled by its slice.
func TestEveryShellsPagesMeetTheFirstLoadBudget(t *testing.T) {
	for _, shell := range ui.LayoutNames() {
		for _, theme := range ui.ThemeNames() {
			t.Run(shell+"/"+theme, func(t *testing.T) {
				weighShell(t, shell, theme, framePages)
			})
		}
	}
}

// A table and a chart cost a page its slice of data.css and no more,
// in every shell and theme, and every hook they show is styled by that
// slice.
func TestTheDataPagesMeetTheirBudgetInEveryShell(t *testing.T) {
	for _, shell := range ui.LayoutNames() {
		for _, theme := range ui.ThemeNames() {
			t.Run(shell+"/"+theme, func(t *testing.T) {
				weighShell(t, shell, theme, dataPages)
			})
		}
	}
}

// weighShell serves contents in the shell and theme and holds every
// page to the first-load budget.
func weighShell(
	t *testing.T, shell, theme string, contents map[string]string,
) {
	router, assets := shellApp(t, shell, theme, contents)
	requireSliced(t, router, assets, contents)
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
}

// requireSliced fails unless every page links a slice, so the budget
// holds them to the sliced limits, not the unsliced one.
func requireSliced(
	t *testing.T, router http.Handler, assets *rastrillo.Assets,
	contents map[string]string,
) {
	t.Helper()
	wholes := []string{assets.Path("static/tokens.css"),
		assets.Path("static/data.css")}
	for name := range contents {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/"+name, nil))
		for _, whole := range wholes {
			if strings.Contains(recorder.Body.String(), whole) {
				t.Errorf("the %s page links the whole %s", name, whole)
			}
		}
	}
}
