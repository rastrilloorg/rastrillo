package pow

import (
	"embed"
	"io/fs"
)

//go:embed browser/*.js
var browserFS embed.FS

// Assets is the browser half — pow.js, pow-worker.js, powcore.js and
// sha256.js — as a file tree to serve.
//
// It is served from the module rather than vendored into the app the
// way tokens.css and rastrillo.js are, and that difference is the whole
// reason this package exists. A vendored copy is a copy: an app that
// edits it, or upgrades the framework without refreshing it, ends up
// running a solver that disagrees with the verifier it is linked
// against — and the disagreement is silent, in the browser, for
// whichever addresses happen to hit it. Serving both halves from the
// same module means they cannot be different versions of each other.
//
// Mount it wherever you like, with an Assets registry in front so the
// URLs are content-hashed:
//
//	powAssets := rastrillo.NewAssets(pow.Assets())
//	mux.Handle("GET /pow/", http.StripPrefix("/pow/", powAssets.Handler()))
//
// then link the module and hand the form its worker URL:
//
//	<script type="module" src="/pow/{{powAsset "pow.js"}}"></script>
//
// The modules import each other by relative name, which the Assets
// handler serves unhashed with no-cache — so only the entry points need
// a fingerprinted URL.
func Assets() fs.FS {
	sub, err := fs.Sub(browserFS, "browser")
	if err != nil {
		panic(err) // browser/ is embedded above; this cannot fail
	}
	return sub
}
