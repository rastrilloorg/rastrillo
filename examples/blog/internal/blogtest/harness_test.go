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

	"github.com/carlosframework/rastrillo"

	"blog/gen"
	postsstore "blog/gen/store/posts"
	"blog/internal/blog"
)

// newApp builds a whole app per test: a fresh file-backed SQLite database
// (a file, not :memory:, because SetMaxOpenConns(1) plus WAL is the
// configuration being exercised) and the real generated router, wired
// exactly as cmd/blog/main.go wires it.
func newApp(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, err := blog.Open(filepath.Join(t.TempDir(), "blog.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := gen.Router(func(*http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: blog.Render}
	})
	// The same fingerprinting mount main.go wires, so asset tests
	// exercise the real handler behind the layout's {{asset}} hrefs.
	mux.Handle("GET /static/", blog.Assets.Handler())
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
