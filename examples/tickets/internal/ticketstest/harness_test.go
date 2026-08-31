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

	"tickets/gen"
	announcementsstore "tickets/gen/store/announcements"
	ticket_typesstore "tickets/gen/store/ticket_types"
	"tickets/internal/tickets"
)

// newApp builds a whole app per test: a fresh file-backed SQLite
// database (matching main.go's SetMaxOpenConns(1)+WAL configuration —
// see rastrillo.OpenDB) and the real generated router, wired exactly
// as cmd/tickets/main.go wires it. Migrations is the two generated
// stores' own vars concatenated, unaltered — the exclusive table plus
// the mergeable resource's eventlog schema; this app has no additive
// column of its own (contrast the blog's newApp/blog.Open, which
// layers on `published`).
func newApp(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "tickets.db"),
		slices.Concat(ticket_typesstore.Migrations, announcementsstore.Migrations))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := gen.Router(func(*http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: tickets.Render}
	})
	return mux, db
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
	id, err := ticket_typesstore.New(db).CreateTicketType(context.Background(), ticket_typesstore.CreateTicketTypeParams{
		Name:        name,
		Price:       priceCents,
		Status:      status,
		MaxPerOrder: maxPerOrder,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("seed %q: %v", name, err)
	}
	return id
}
