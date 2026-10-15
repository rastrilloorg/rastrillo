// Command tickets runs the fully generated example: two manifest
// resources (manifest/ticket_types.toml on the exclusive store,
// manifest/announcements.toml on the mergeable one), zero hand
// actions, zero ejected templates. See the module README for what
// that proves.
//
// Static assets are embedded (see the F8 note below), so the binary
// runs the same from any starting directory:
//
//	cd examples/tickets && go build ./cmd/tickets && ./tickets -addr :8080
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"slices"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"

	ticketsassets "tickets"
	"tickets/gen"
	genlocales "tickets/gen/locales"
	announcementsstore "tickets/gen/store/announcements"
	ticket_typesstore "tickets/gen/store/ticket_types"
	"tickets/internal/tickets"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Run end to end: rastrillo resolves the activation argv, opens
	// the database (pragmas, eager ping, the schema migration), and
	// hands the *sql.DB back through Router — the F4 seam (see
	// examples/blog/cmd/blog/main.go, the same wiring). Migrations is
	// the two generated stores' own vars concatenated, unaltered: the
	// exclusive resource's table plus the mergeable resource's eventlog
	// schema (announcementsstore.Migrations re-exports it) — this app
	// adds no column of its own, unlike the blog's published (internal/
	// blog/store.go); there is nothing here but what the manifests
	// declared.
	err := rastrillo.Run(rastrillo.Options{
		DBPath:      "tickets.db",
		Migrations:  slices.Concat(ticket_typesstore.Migrations, announcementsstore.Migrations),
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
			app, err := tickets.NewApp(cfg)
			if err != nil {
				return nil, err
			}

			// A fresh Ctx per request. Actor.Human is true and
			// Actor.Name empty: honest for an app with no auth (see
			// the blog's F7). Render is the manifest system's seam
			// (design doc): every one of gen/router.go's seven routes
			// is a generated action, and app.Render is the only
			// function any of them ever calls into.
			mux := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: app.Render}
			})

			// The app serves its own static files — the framework
			// never does. They are embedded (assets.go), so the
			// binary is self-contained wherever it starts (F8).
			mux.Handle("GET /static/", http.FileServerFS(ticketsassets.StaticFS))

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
