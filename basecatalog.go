package rastrillo

// baseCatalog is the framework's own strings — the third fallback
// layer the §10 design reserved ("locale → default → framework base →
// key"). Keys are namespaced rastrillo.ui.* so an app catalog can
// override any of them per locale without colliding with app keys.
//
// This list must exactly match the distinct English strings ui's
// partials fall back to via {{T "rastrillo.ui.*"}} — one key per
// distinct string, no more; a key may back more than one {{T ...}} call
// when their hardcoded English values coincide (list-bar-search's
// aria-label default reuses rastrillo.ui.search_submit rather than
// declaring a fourth key, since both read "Search"). See ui/funcs.go's
// defaultT and the partials it backs (pagination, list-search-submit,
// list-bar-search, confirm-form, bulk-bar).
var baseCatalog = Catalog{
	"rastrillo.ui.pagination":    "Pagination",
	"rastrillo.ui.search_submit": "Search",
	"rastrillo.ui.cancel":        "Cancel",
	"rastrillo.ui.done":          "Done selecting",
}

// BaseCatalog returns a copy of the framework's base strings, so a
// caller can inspect them without being able to mutate the layer every
// app shares.
func BaseCatalog() Catalog {
	out := make(Catalog, len(baseCatalog))
	for k, v := range baseCatalog {
		out[k] = v
	}
	return out
}
