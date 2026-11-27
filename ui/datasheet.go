package ui

import (
	"html/template"

	"amadan.net/rastrillo/fiesta"
	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/chart"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// DataCSS returns Fiesta's data.css: the data table's and the charts'
// rules, which a page links after its theme only when it shows one
// (pageComponentStylesheets). Fiesta keeps them out of tokens.css so a
// page without a table or a chart pays nothing for them.
func DataCSS() []byte { return fiesta.DataCSS() }

// bindComponentStylesheets binds each page's pageComponentStylesheets
// to the component stylesheets its hooks ask for: Fiesta's data.css on
// a page that shows a table or a chart, and nothing on any other, so
// a page pays only for what it shows. A live page asks with every
// page's hooks, as its slice of tokens.css does.
func (pages *Pages) bindComponentStylesheets(
	plans map[string]pagePlan, config PagesConfig,
) {
	whole := wholeFile(config.Funcs, pages.logger)
	union := everyHook(plans)
	for name, plan := range plans {
		hooks := plan.hooks
		if plan.live {
			hooks = union
		}
		pages.pages[name].Funcs(tmpl.FuncMap{
			"pageComponentStylesheets": func() []string {
				return componentLinks(hooks, config.Assets, whole)
			},
		})
	}
}

// componentLinks is the URL of each component stylesheet hooks ask
// for: with the app's assets, its slice, or nothing when the app has
// no copy, as one scaffolded before the file was vendored has not;
// without them, the whole file through the app's asset func.
func componentLinks(
	hooks []string, assets *rastrillo.Assets, whole func(string) string,
) []string {
	var links []string
	for _, sheet := range vendored.NeededStylesheets(hooks) {
		name := "static/" + sheet
		switch {
		case assets == nil:
			links = append(links, whole(name))
		case assets.Has(name):
			links = append(links, assets.Stylesheet(name, hooks))
		}
	}
	return links
}

// dataFuncs are the template functions behind the data partials: each
// reads a partial's data into package chart's model, checks it, and
// lays it out, so the marks and the table come from the same points.
func dataFuncs() template.FuncMap {
	return template.FuncMap{
		"dataTable": chart.DrawTable, "chartBar": chart.DrawBars,
		"chartLine": chart.DrawLine, "chartSpark": chart.DrawSpark,
	}
}

// everyComponentStylesheet is each component stylesheet whole, at its
// static path: what a render outside ui.Pages, which cannot read its
// page, links.
func everyComponentStylesheet() []string {
	var links []string
	for _, sheet := range vendored.ComponentStylesheets() {
		links = append(links, "/static/"+sheet)
	}
	return links
}
