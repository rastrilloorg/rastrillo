package ui

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var dataLink = regexp.MustCompile(
	`<link rel="stylesheet" href="([^"]*/static/data\.[^"]*)">`)

// linkedData is every data.css URL a page links.
func linkedData(page string) []string {
	var links []string
	for _, match := range dataLink.FindAllStringSubmatch(page, -1) {
		links = append(links, match[1])
	}
	return links
}

// dataContents are a page with no table or chart, a table, a chart
// drawn by a partial of the app's own, and a live page that shows
// neither itself.
var dataContents = map[string]string{
	"plain": shellContents["plain"],
	"table": content(`<div rst-card rst-table-scroll tabindex="0">` +
		`<table rst-table><tr><th>Week</th></tr></table></div>`),
	"chart": content(`{{template "sales" .}}`),
	"live": content(`<h1>Jobs</h1>`) +
		`{{define "region:items"}}<p>Running</p>{{end}}`,
}

// salesPartial is the app's shared partial that draws a chart.
const salesPartial = `{{define "sales"}}<figure rst-chart>` +
	`<p rst-chart-desc>Up</p></figure>{{end}}`

// withSales adds the app's shared partial to every page of contents.
func withSales(contents map[string]string) map[string]string {
	out := map[string]string{}
	for name, body := range contents {
		out[name] = body + salesPartial
	}
	return out
}

// fetchSlice serves link from assets and returns its body.
func fetchSlice(t *testing.T, handler http.Handler, link string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, link, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", link, recorder.Code)
	}
	return recorder.Body.String()
}

func TestOnlyAPageShowingATableOrAChartLinksDataCSS(t *testing.T) {
	for _, shell := range LayoutNames() {
		layout, _ := Layout(shell)
		assets := appAssets()
		pages := renderPagesWith(t, layout, withSales(dataContents),
			sliced(assets, ""))
		if links := linkedData(pages["plain"]); len(links) != 0 {
			t.Errorf("%s: the plain page links %q", shell, links)
		}
		table, chart := linkedData(pages["table"]), linkedData(pages["chart"])
		whole := assets.Path("static/data.css")
		if len(table) != 1 || len(chart) != 1 || table[0] == chart[0] ||
			table[0] == whole || chart[0] == whole {
			t.Fatalf("%s: the table and chart pages link %q and %q, "+
				"want a slice each, not %q", shell, table, chart, whole)
		}
		tableRules := fetchSlice(t, assets.Handler(), table[0])
		if strings.Contains(tableRules, "rst-chart-plot") {
			t.Errorf("%s: the table page's slice keeps the chart's rules",
				shell)
		}
		if !strings.Contains(fetchSlice(t, assets.Handler(), chart[0]),
			"[rst-chart-desc]") {
			t.Errorf("%s: the chart page's slice lacks rst-chart-desc, "+
				"which its shared partial shows", shell)
		}
	}
}

// A live page may be sent any page's markup, so it links the slice of
// every page's hooks, data.css's included.
func TestALivePageLinksTheDataSliceOfEveryPage(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	pages := renderPagesWith(t, layout, withSales(dataContents),
		sliced(assets, ""))
	live := linkedData(pages["live"])
	if len(live) != 1 {
		t.Fatalf("the live page links %q, want one data.css", live)
	}
	rules := fetchSlice(t, assets.Handler(), live[0])
	for _, hook := range []string{"[rst-table]", "[rst-chart-desc]"} {
		if !strings.Contains(rules, hook) {
			t.Errorf("the live page's slice lacks %s", hook)
		}
	}
}

// An app scaffolded before data.css was vendored has no copy until
// rastrillo doctor -fix writes one, and a link to it would 404.
func TestAnAppWithoutDataCSSLinksNone(t *testing.T) {
	layout, _ := Layout("column")
	pages := renderPagesWith(t, layout, withSales(dataContents),
		sliced(appAssets("data.css"), ""))
	for name, page := range pages {
		if links := linkedData(page); len(links) != 0 {
			t.Errorf("%s links %q, which the app does not have", name, links)
		}
	}
}

func TestTheDataSliceHonoursTheBasePath(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	pages := renderPagesWith(t, layout, withSales(dataContents),
		sliced(assets, "/acme"))
	links := linkedData(pages["table"])
	if len(links) != 1 || !strings.HasPrefix(links[0], "/acme/static/data.") {
		t.Fatalf("under /acme the table page links %q, want "+
			"/acme/static/data.<slice>.css", links)
	}
	fetchSlice(t, assets.Handler(), strings.TrimPrefix(links[0], "/acme"))
}

// Without Assets nothing can be sliced, so a page that shows a table
// links the whole file through the app's own asset func, which may
// mount it anywhere; a page with none still links nothing.
func TestPagesWithoutAssetsLinkTheWholeDataCSSWhereItIsUsed(t *testing.T) {
	layout, _ := Layout("sidebar")
	moved := func(name string) string {
		return "/assets/" + strings.TrimPrefix(name, "static/")
	}
	pages := renderPagesWith(t, layout, withSales(dataContents),
		shellSetup{asset: assetMount(moved)})
	if links := linkedData(pages["plain"]); len(links) != 0 {
		t.Errorf("the plain page links %q", links)
	}
	if !strings.Contains(pages["table"],
		`<link rel="stylesheet" href="/assets/data.css">`) {
		t.Errorf("the table page does not link /assets/data.css:\n%s",
			pages["table"])
	}
}

// A render outside ui.Pages cannot read its page, so it links the
// whole file, as it links every script.
func TestARenderOutsidePagesLinksTheWholeDataCSS(t *testing.T) {
	layout, _ := Layout("topbar")
	set := template.Must(template.New("layout").Funcs(Funcs()).Funcs(
		template.FuncMap{"asset": func(name string) string {
			return "/" + name
		}}).ParseFS(Templates(), "*.html"))
	template.Must(set.Parse(string(layout)))
	template.Must(set.Parse(shellContents["plain"]))
	var page strings.Builder
	if err := set.ExecuteTemplate(&page, "layout", nil); err != nil {
		t.Fatal(err)
	}
	if got := linkedData(page.String()); len(got) != 1 ||
		got[0] != "/static/data.css" {
		t.Errorf("outside Pages the layout links %q, want "+
			"/static/data.css", got)
	}
}

// An app's own layout calls pageComponentStylesheets where it likes;
// a layout that never calls it renders as it did.
func TestAnAppsOwnLayoutLinksDataCSSWhereItAsks(t *testing.T) {
	own := `{{define "layout"}}<head>{{range pageComponentStylesheets}}` +
		`<link rel="stylesheet" href="{{path $ .}}">{{end}}</head>` +
		`<body>{{block "content" .}}{{end}}</body>{{end}}`
	older := `{{define "layout"}}<body>{{block "content" .}}{{end}}` +
		`</body>{{end}}`
	assets := appAssets()
	pages := renderPagesWith(t, []byte(own), withSales(dataContents),
		sliced(assets, ""))
	if links := linkedData(pages["chart"]); len(links) != 1 {
		t.Errorf("an app's own layout links %q on the chart page", links)
	}
	pages = renderPagesWith(t, []byte(older), withSales(dataContents),
		sliced(assets, ""))
	if links := linkedData(pages["chart"]); len(links) != 0 {
		t.Errorf("a layout that never asks links %q", links)
	}
}

func TestAnAppCannotReplacePageComponentStylesheets(t *testing.T) {
	if !reservedFunc("pageComponentStylesheets") {
		t.Error("an app's pageComponentStylesheets would replace the " +
			"framework's")
	}
}

// A table's variants are chosen by its data, so a page that calls
// data-table keeps the dense and bounded rules whichever it shows.
func TestADataTablePageKeepsEveryVariantOfTheTable(t *testing.T) {
	assets := appAssets()
	layout, _ := Layout("column")
	pages := renderPagesWith(t, layout, map[string]string{
		"table": content(`{{template "data-table" dict "L" (localizer $) ` +
			`"ID" "t" "Title" "Orders" "Columns" (list (dict "Label" ` +
			`"Order"))}}`),
	}, sliced(assets, ""))
	links := linkedData(pages["table"])
	if len(links) != 1 {
		t.Fatalf("the table page links %q", links)
	}
	rules := fetchSlice(t, assets.Handler(), links[0])
	for _, variant := range []string{"bounded", "dense"} {
		if !strings.Contains(rules, variant) {
			t.Errorf("the slice drops the %s table's rules", variant)
		}
	}
	if strings.Contains(rules, "rst-chart") {
		t.Error("a table's slice keeps the charts' rules")
	}
}
