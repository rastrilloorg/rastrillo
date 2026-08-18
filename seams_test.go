package rastrillo

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func doGet(t *testing.T, h http.Handler, path string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	for k, vs := range header {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestWrapWrapsAppMuxOnly(t *testing.T) {
	app := http.NewServeMux()
	app.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("hi"))
	})
	h, err := buildHandler(Options{
		Mux: app,
		Wrap: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Wrapped", "1")
				next.ServeHTTP(w, r)
			})
		},
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	if w := doGet(t, h, "/hello", nil); w.Header().Get("X-Wrapped") != "1" {
		t.Fatal("app route did not pass through Wrap")
	}
	// Framework endpoints stay unwrapped: a health probe must never
	// depend on app middleware.
	if w := doGet(t, h, "/healthz", nil); w.Header().Get("X-Wrapped") == "1" {
		t.Fatal("/healthz passed through Wrap; framework endpoints must stay outside it")
	}
}

func TestHandlerOpensDBAndServesFrameworkEndpoints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	var sawDB bool
	h, closeDB, err := Handler(Options{
		DBPath:     dbPath,
		Migrations: []string{`CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY)`},
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			sawDB = db != nil
			return http.NewServeMux(), nil
		},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	defer closeDB()

	if !sawDB {
		t.Fatal("Router was not handed the opened database")
	}
	if w := doGet(t, h, "/healthz", nil); w.Body.String() != "ok" {
		t.Fatalf("/healthz = %q, want ok", w.Body.String())
	}
	if w := doGet(t, h, "/api/version", nil); w.Body.String() != BuildVersion {
		t.Fatalf("/api/version = %q, want %q", w.Body.String(), BuildVersion)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database not materialized at boot: %v", err)
	}
	if err := closeDB(); err != nil {
		t.Fatalf("closeDB: %v", err)
	}
}

func TestHandlerRejectsMuxAndRouterTogether(t *testing.T) {
	_, _, err := Handler(Options{})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("Handler with neither Mux nor Router: %v", err)
	}
}

func TestNextDue(t *testing.T) {
	due := time.Unix(1765000000, 0)
	newHandler := func(nd func() time.Time) http.Handler {
		h, err := buildHandler(Options{Mux: http.NewServeMux(), NextDue: nd})
		if err != nil {
			t.Fatalf("buildHandler: %v", err)
		}
		return h
	}

	t.Run("absent without NextDue", func(t *testing.T) {
		h, _ := buildHandler(Options{Mux: http.NewServeMux()})
		if w := doGet(t, h, "/api/next-due", nil); w.Code != http.StatusNotFound {
			t.Fatalf("/api/next-due without NextDue = %d, want 404", w.Code)
		}
	})

	t.Run("forbidden without the right bearer", func(t *testing.T) {
		t.Setenv("CARLOS_ADMIN_TOKEN", "sekrit")
		h := newHandler(func() time.Time { return due })
		if w := doGet(t, h, "/api/next-due", nil); w.Code != http.StatusForbidden {
			t.Fatalf("no auth = %d, want 403", w.Code)
		}
		bad := http.Header{"Authorization": {"Bearer wrong"}}
		if w := doGet(t, h, "/api/next-due", bad); w.Code != http.StatusForbidden {
			t.Fatalf("wrong token = %d, want 403", w.Code)
		}
	})

	t.Run("forbidden when no token is configured", func(t *testing.T) {
		t.Setenv("CARLOS_ADMIN_TOKEN", "")
		h := newHandler(func() time.Time { return due })
		hdr := http.Header{"Authorization": {"Bearer "}}
		if w := doGet(t, h, "/api/next-due", hdr); w.Code != http.StatusForbidden {
			t.Fatalf("empty configured token = %d, want 403 (fail closed)", w.Code)
		}
	})

	t.Run("answers due as unix seconds", func(t *testing.T) {
		t.Setenv("CARLOS_ADMIN_TOKEN", "sekrit")
		h := newHandler(func() time.Time { return due })
		hdr := http.Header{"Authorization": {"Bearer sekrit"}}
		w := doGet(t, h, "/api/next-due", hdr)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var got map[string]int64
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["due"] != due.Unix() {
			t.Fatalf("due = %d, want %d", got["due"], due.Unix())
		}
	})

	t.Run("zero time reports due 0", func(t *testing.T) {
		t.Setenv("CARLOS_ADMIN_TOKEN", "sekrit")
		h := newHandler(func() time.Time { return time.Time{} })
		hdr := http.Header{"Authorization": {"Bearer sekrit"}}
		w := doGet(t, h, "/api/next-due", hdr)
		var got map[string]int64
		json.Unmarshal(w.Body.Bytes(), &got)
		if got["due"] != 0 {
			t.Fatalf("due = %d, want 0 for a zero time", got["due"])
		}
	})
}
