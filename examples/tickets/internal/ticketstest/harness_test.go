// Package ticketstest is the permanent no-mask regression host: every
// test here drives the real generated mux (tickets/gen.Router) through
// httptest, exactly as cmd/tickets/main.go wires it. There is nothing
// hand-owned to route around — no ejected template, no hand action —
// so a failure here can only mean the generator itself (internal/
// generate) regressed, never that this app patched over a gap by
// hand.
package ticketstest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/locales"

	"tickets/gen"
	genlocales "tickets/gen/locales"
	announcementsstore "tickets/gen/store/announcements"
	ticket_typesstore "tickets/gen/store/ticket_types"
	"tickets/internal/tickets"
)

// newApp builds a whole app per test in the manifest's declared
// locale configuration — genlocales.DefaultLocale and genlocales.
// Locales, exactly as main.go wires it. newAppWith builds one in an
// arbitrary configuration, for the two-locale suite (locales_test.go).
func newApp(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	return newAppWith(t, genlocales.DefaultLocale, genlocales.Locales)
}

// newAppAt is newApp against the database file at path, so a test can
// prepare that file the way an older binary left it and see this
// app's own startup upgrade it.
func newAppAt(t *testing.T, path string) (http.Handler, *sql.DB) {
	t.Helper()
	return newAppWithAt(t, path, genlocales.DefaultLocale, genlocales.Locales, basepath.Config{})
}

// newAppWith builds a whole app per test: a fresh file-backed SQLite
// database (matching main.go's SetMaxOpenConns(1)+WAL configuration —
// see rastrillo.OpenDB) and the real generated router, wired exactly
// as cmd/tickets/main.go wires it, with def and offered standing in
// for the manifest's own locale declaration so a test can offer more
// than tickets' app.toml does (locales_test.go's two-locale suite).
func newAppWith(t *testing.T, def string, offered []string) (http.Handler, *sql.DB) {
	t.Helper()
	return newAppWithAt(t, filepath.Join(t.TempDir(), "tickets.db"), def, offered, basepath.Config{})
}

// newAppWithAt is newAppWith against the database file at path, so a
// test can prepare that file the way an older binary left it and see
// this app's own startup upgrade it (upgrade_test.go). base serves it
// under a base path, as Options.BasePath does (basepath_test.go); the
// zero Config serves it at the root.
//
// It goes through rastrillo.Handler, exactly as cmd/tickets/main.go's
// Router does, rather than calling gen.Boot/gen.Router directly: with
// two or more offered locales that is what strips the "/es/" prefix
// and puts the negotiated locale on the request before app.Middleware
// ever runs, and a harness that skipped it would pass against a mux
// that never sees the prefix a real deploy serves.
func newAppWithAt(t *testing.T, path, def string, offered []string, base basepath.Config) (http.Handler, *sql.DB) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var db *sql.DB
	handler, closeApp, err := rastrillo.Handler(rastrillo.Options{
		DBPath:        path,
		Migrations:    slices.Concat(ticket_typesstore.Migrations, announcementsstore.Migrations),
		BaseCatalog:   genlocales.BaseCatalog,
		Logger:        logger,
		Locales:       offered,
		DefaultLocale: def,
		BasePath:      base,
		Router: func(d *sql.DB) (*http.ServeMux, error) {
			db = d
			booted, err := gen.Boot(context.Background(), d)
			if err != nil {
				return nil, err
			}
			// Already validated by rastrillo.Handler above; this call
			// is only to get the value back for tickets.NewApp, as
			// main.go's own Router does.
			cfg, err := locales.CheckConfig(def, offered)
			if err != nil {
				return nil, err
			}
			app, err := tickets.NewApp(cfg)
			if err != nil {
				return nil, err
			}
			mux := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: d, Logger: logger, Actor: rastrillo.Actor{}, Render: app.Render}
			})
			outer := http.NewServeMux()
			outer.Handle("/", app.Middleware(mux))
			return outer, nil
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	t.Cleanup(func() { closeApp() })
	return handler, db
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func post(t *testing.T, h http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// seed creates a ticket type through the generated store directly —
// the same one the generated actions use — so a test can set up rows
// without going through the HTTP create path it isn't exercising.
func seed(t *testing.T, db *sql.DB, name string, priceCents int64, status, maxPerOrder string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	usd := "USD" // the resource default; every amount has its currency
	id, err := ticket_typesstore.New(db).CreateTicketType(context.Background(), ticket_typesstore.CreateTicketTypeParams{
		Name:          name,
		PriceMinor:    &priceCents,
		PriceCurrency: &usd,
		Status:        status,
		MaxPerOrder:   maxPerOrder,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("seed %q: %v", name, err)
	}
	return id
}
