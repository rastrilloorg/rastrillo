// Command notes runs the front-door example: accounts plus
// owner-scoped note CRUD on the middle layer (db, sessions, password,
// csrf, flash, scope). See internal/notes for the ~150-line domain
// surface this whole app exists to keep small.
package main

import (
	"log/slog"
	"os"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/db"

	"notes/internal/notes"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	origin := os.Getenv("NOTES_ORIGIN")
	if origin == "" {
		origin = "http://localhost:8080"
	}

	// Resolve the platform's activation argv/env ourselves (Resolve's
	// own doc comment names this exact case): this app opens its own
	// database handle via db.Open — a *gorm.DB with a split reader/
	// writer pool, not the bare *sql.DB Options.Router would hand
	// back — so Options.DBPath must stay empty for the Serve call
	// below, or Serve would open a second, unused connection to the
	// same file.
	opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "notes.db", Logger: logger})
	if err != nil {
		logger.Error("resolve activation", "err", err)
		os.Exit(1)
	}

	d, err := db.Open(opts.DBPath, logger)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	mux, err := notes.App(d, origin, logger)
	if err != nil {
		logger.Error("build app", "err", err)
		os.Exit(1)
	}

	opts.Mux = mux
	opts.DBPath = ""
	if err := rastrillo.Serve(opts); err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
