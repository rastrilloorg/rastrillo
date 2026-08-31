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
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"slices"

	"amadan.net/rastrillo/rastrillo"

	ticketsassets "tickets"
	"tickets/gen"
	"tickets/gen/locales"
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
		BaseCatalog: locales.BaseCatalog,
		Logger:      logger,
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			// A fresh Ctx per request. Actor.Human is true and
			// Actor.Name empty: honest for an app with no auth (see
			// the blog's F7). Render is the manifest system's seam
			// (design doc): every one of gen/router.go's seven routes
			// is a generated action, and tickets.Render is the only
			// function any of them ever calls into.
			mux := gen.Router(func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: tickets.Render}
			})

			// The app serves its own static files — the framework
			// never does. They are embedded (assets.go), so the
			// binary is self-contained wherever it starts (F8).
			mux.Handle("GET /static/", http.FileServerFS(ticketsassets.StaticFS))
			return mux, nil
		},
	})
	if err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
