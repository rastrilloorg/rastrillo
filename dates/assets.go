package dates

import (
	"embed"
	"io/fs"
)

//go:embed browser/dates.mjs
var browserFS embed.FS

// Assets is the browser half, dates.mjs, as a file tree to serve. It is
// served from the module, never copied into an app: a copy drifts from
// the Go formatter it must match, and the drift is silent. Mount it
// behind an Assets registry so its URL is content-hashed:
//
//	dateAssets := rastrillo.NewAssets(dates.Assets())
//	mux.Handle("GET /dates/",
//		http.StripPrefix("/dates/", dateAssets.Handler()))
//
// then import it from a module script and hand it the page's data,
// dates.For(locale).Client(), as JSON.
func Assets() fs.FS {
	sub, err := fs.Sub(browserFS, "browser")
	if err != nil {
		panic(err) // browser/ is embedded above; this cannot fail
	}
	return sub
}
