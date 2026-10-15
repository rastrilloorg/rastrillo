// Command blog runs the example blog.
//
// Static assets are embedded and fingerprinted (see assets.go and
// blog.Assets), so the binary is self-contained and runs from anywhere:
//
//	cd examples/blog && go build ./cmd/blog && ./blog -addr :8080
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"

	"blog/gen"
	genlocales "blog/gen/locales"
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
		Migrations:  blog.Migrations,
		BaseCatalog: genlocales.BaseCatalog,
		Logger:      logger,
		// Locales and DefaultLocale come from manifest/app.toml, through
		// gen/locales. Boot refuses a configuration whose packs this
		// binary does not link.
		Locales:       genlocales.Locales,
		DefaultLocale: genlocales.DefaultLocale,
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			// Boot runs here because Router is called after OpenDB has
			// applied Migrations, the schema Boot upgrades and checks;
			// its error stops the start rather than serve that data.
			booted, err := gen.Boot(context.Background(), db)
			if err != nil {
				return nil, err
			}

			// locales.CheckConfig already ran inside rastrillo.Handler
			// (Options.Locales/DefaultLocale above); this call is only
			// to get its result back, for ui.Pages to format and
			// dispatch by (Task 9, 13).
			cfg, err := locales.CheckConfig(genlocales.DefaultLocale, genlocales.Locales)
			if err != nil {
				return nil, err
			}
			app, err := blog.NewApp(cfg)
			if err != nil {
				return nil, err
			}

			// A fresh Ctx per request. Actor.Human is true and
			// Actor.Name empty: honest for an app with no auth, and
			// the one line a real deployment would replace with a
			// session lookup. Render is the manifest system's seam
			// (design doc): a generated action calls ctx.Render, and
			// app.Render is the one function that now serves both
			// the app's own hand pages and the generated ones (see
			// its own doc comment and genrender.go).
			mux := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: app.Render}
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

			// app.Middleware puts the request's localizer on the
			// context (ui.Pages.Middleware); Router must return a
			// *http.ServeMux, so an outer one carries it instead of
			// wrapping mux in place.
			outer := http.NewServeMux()
			outer.Handle("/", app.Middleware(mux))
			return outer, nil
		},
	})
	if err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
