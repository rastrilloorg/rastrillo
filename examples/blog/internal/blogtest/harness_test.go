package blogtest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"

	"blog/gen"
	genlocales "blog/gen/locales"
	postsstore "blog/gen/store/posts"
	"blog/internal/blog"
)

// newApp builds a whole app per test in the manifest's declared locale
// configuration — genlocales.DefaultLocale and genlocales.Locales,
// exactly as main.go wires it. newAppWith builds one in an arbitrary
// configuration, for the two-locale suite (locales_test.go).
func newApp(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	return newAppWith(t, genlocales.DefaultLocale, genlocales.Locales)
}

// newAppWith builds a whole app per test: a fresh file-backed SQLite
// database (a file, not :memory:, because SetMaxOpenConns(1) plus WAL
// is the configuration being exercised) and the real generated router,
// wired exactly as cmd/blog/main.go wires it, with def and offered
// standing in for the manifest's own locale declaration so a test can
// offer more than blog's app.toml does (locales_test.go's two-locale
// suite).
//
// It goes through rastrillo.Handler, exactly as cmd/blog/main.go's
// Router does, rather than calling gen.Boot/gen.Router directly: with
// two or more offered locales that is what strips the "/es/" prefix
// and puts the negotiated locale on the request before app.Middleware
// ever runs, and a harness that skipped it would pass against a mux
// that never sees the prefix a real deploy serves.
func newAppWith(t *testing.T, def string, offered []string) (http.Handler, *sql.DB) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var db *sql.DB
	handler, closeApp, err := rastrillo.Handler(rastrillo.Options{
		DBPath:        filepath.Join(t.TempDir(), "blog.db"),
		Migrations:    blog.Migrations,
		BaseCatalog:   genlocales.BaseCatalog,
		Logger:        logger,
		Locales:       offered,
		DefaultLocale: def,
		Router: func(d *sql.DB) (*http.ServeMux, error) {
			db = d
			booted, err := gen.Boot(context.Background(), d)
			if err != nil {
				return nil, err
			}
			// Already validated by rastrillo.Handler above; this call
			// is only to get the value back for blog.NewApp, as
			// main.go's own Router does.
			cfg, err := locales.CheckConfig(def, offered)
			if err != nil {
				return nil, err
			}
			app, err := blog.NewApp(cfg)
			if err != nil {
				return nil, err
			}
			mux := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: d, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: app.Render}
			})
			// The same fingerprinting mount main.go wires, so asset
			// tests exercise the real handler behind the layout's
			// {{asset}} hrefs.
			mux.Handle("GET /static/", blog.Assets.Handler())
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

// seed creates a post through the generated store — the same one the
// admin create/update actions now use — so the tests exercise it too.
// Publishing stays a blog.SetPublished call: published isn't a
// manifest field, so it's not something the generated store knows
// about at all (see internal/blog/store.go's Migrations doc comment).
func seed(t *testing.T, db *sql.DB, title, body string, published bool) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	id, err := postsstore.New(db).CreatePost(context.Background(), postsstore.CreatePostParams{
		Title: title,
		Body:  body,
		Now:   now,
	})
	if err != nil {
		t.Fatalf("seed %q: %v", title, err)
	}
	if published {
		if err := blog.SetPublished(db, id, true); err != nil {
			t.Fatalf("publish %q: %v", title, err)
		}
	}
	return id
}
