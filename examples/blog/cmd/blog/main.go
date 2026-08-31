// Command blog runs the example blog.
//
// Static assets are embedded and fingerprinted (see assets.go and
// blog.Assets), so the binary is self-contained and runs from anywhere:
//
//	cd examples/blog && go build ./cmd/blog && ./blog -addr :8080
package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"

	"amadan.net/rastrillo/rastrillo"

	"blog/gen"
	"blog/gen/locales"
	"blog/internal/blog"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Run end to end: rastrillo resolves the activation argv, opens the
	// database (pragmas, eager ping, the schema migration), and hands
	// the *sql.DB back through Router — the F4 seam. No hand-copied
	// DSN, no Resolve dance, no double-open to avoid.
	err := rastrillo.Run(rastrillo.Options{
		DBPath: "blog.db",
		// The generated postsstore.Migrations creates the posts table
		// first; blog.Migrations already carries that plus the app's
		// own additive published column, in that order (see
		// internal/blog/store.go).
		Migrations: blog.Migrations,
		// BaseCatalog layers under any app catalog the manifest
		// system's generated templates reference (resource.posts.*,
		// ui.*) — gen/locales/locales.go's var, emitted from the same
		// map as the human-readable gen/locales/en.toml. The blog is
		// monolingual today (no Options.Locales), so this doesn't
		// drive rastrillo's own request-scoped T; internal/blog's
		// render adapter (genrender.go) reads locales.BaseCatalog
		// directly for that. Wiring it here regardless keeps main.go
		// honest about where the catalog comes from, and is what a
		// later locale-aware revision would already need in place.
		BaseCatalog: locales.BaseCatalog,
		Logger:      logger,
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			// A fresh Ctx per request. Actor.Human is true and
			// Actor.Name empty: honest for an app with no auth, and
			// the one line a real deployment would replace with a
			// session lookup. Render is the manifest system's seam
			// (design doc): a generated action calls ctx.Render, and
			// blog.Render is the one function that now serves both
			// the app's own hand pages and the generated ones (see
			// its own doc comment and genrender.go).
			mux := gen.Router(func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: blog.Render}
			})

			// The app serves its own static files — the framework
			// never does. They are embedded (see assets.go), so the
			// binary is self-contained wherever it starts (F8), and
			// fingerprinted: the layout's {{asset ...}} hrefs carry
			// each file's content hash, and blog.Assets.Handler
			// serves those URLs cacheable-forever (the same Assets
			// instance, so href and handler always agree).
			// "GET /static/" is a longer pattern than "GET /{$}", so
			// the stdlib mux prefers it and no ordering care is
			// needed.
			mux.Handle("GET /static/", blog.Assets.Handler())
			return mux, nil
		},
	})
	if err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
