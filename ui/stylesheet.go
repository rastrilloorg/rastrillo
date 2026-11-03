package ui

import (
	"fmt"
	"html/template"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"sync"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// stylesheet is the one stylesheet a page links a slice of. theme.css
// is never sliced: its rules are the theme's colours, not components.
const stylesheet = "static/tokens.css"

// assetFailedMsg is logged once when the app's asset func fails for
// the stylesheet a page links whole.
const assetFailedMsg = "ui: asset(%q) failed, so pages link /%s: %v"

// wholeStylesheet links the whole stylesheet through the app's asset
// func when it has one, so the link keeps the file's hash and the
// app's own mount. The func may have any shape the layout's other
// asset calls accept: a named type, and a second result, an error. If
// it fails, pages link the bare path, and the error is logged once.
func wholeStylesheet(
	app template.FuncMap, logger *slog.Logger,
) func() string {
	bare := "/" + stylesheet
	asset := reflect.ValueOf(app["asset"])
	if !isAssetFunc(asset) {
		return func() string { return bare }
	}
	var once sync.Once
	return func() string {
		results := asset.Call(
			[]reflect.Value{reflect.ValueOf(stylesheet)})
		if len(results) == 2 && !results[1].IsNil() {
			once.Do(func() {
				logger.Warn(fmt.Sprintf(assetFailedMsg,
					stylesheet, stylesheet, results[1].Interface()))
			})
			return bare
		}
		return results[0].String()
	}
}

// isAssetFunc reports whether asset is a func a template could call as
// {{asset "static/tokens.css"}} for a URL: one argument a string can
// be, and a string result, alone or with an error.
func isAssetFunc(asset reflect.Value) bool {
	if asset.Kind() != reflect.Func || asset.IsNil() {
		return false
	}
	shape := asset.Type()
	errorType := reflect.TypeFor[error]()
	return shape.NumIn() == 1 && !shape.IsVariadic() &&
		reflect.TypeFor[string]().AssignableTo(shape.In(0)) &&
		(shape.NumOut() == 1 ||
			shape.NumOut() == 2 && shape.Out(1) == errorType) &&
		shape.Out(0).Kind() == reflect.String
}

// bindStylesheets binds each page's pageStylesheet to its slice, and a
// live page's to the slice of every page's hooks: a fragment it swaps
// in later is drawn from the app's own templates, so their union
// covers it, with each region's hooks as it is sent alone. Each render
// asks assets afresh, so an app serving a live directory links the
// slice of its stylesheet as it is now.
func (pages *Pages) bindStylesheets(
	plans map[string]pagePlan, assets *rastrillo.Assets,
) {
	every := map[string]bool{}
	for _, plan := range plans {
		for _, hook := range slices.Concat(plan.hooks, plan.fragments) {
			every[hook] = true
		}
	}
	union := slices.Sorted(maps.Keys(every))
	for name, plan := range plans {
		hooks := plan.hooks
		if plan.live {
			hooks = union
		}
		pages.pages[name].Funcs(tmpl.FuncMap{
			"pageStylesheet": func() string {
				return assets.Stylesheet(stylesheet, hooks)
			},
		})
	}
}
