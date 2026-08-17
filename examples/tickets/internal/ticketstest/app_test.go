// Package ticketstest proves the manifest pipeline end to end: the TOML
// manifest in manifest/, the screens rastrillo generate emitted into
// gen/, and rastrillo.Handler — no hand-written screen code anywhere.
package ticketstest

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"

	"tickets/gen"
	genmanifest "tickets/gen/manifest"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler, closeDB, err := rastrillo.Handler(rastrillo.Options{
		DBPath:     filepath.Join(t.TempDir(), "tickets.db"),
		Migrations: genmanifest.Migrations(),
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			return gen.Router(func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Actor: rastrillo.Actor{Human: true}}
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDB() })
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, c *http.Client, u string) (int, string) {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestManifestScreensEndToEnd(t *testing.T) {
	srv := newServer(t)
	c := srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	// The hand-written action and the framework endpoints coexist.
	if code, _ := get(t, c, srv.URL+"/healthz"); code != 200 {
		t.Fatalf("healthz: %d", code)
	}
	resp, _ := c.Get(srv.URL + "/")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("hand-written redirect: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Blank list from the generated screen.
	if code, body := get(t, c, srv.URL+"/admin/ticket_types"); code != 200 || !strings.Contains(body, "No ticket types yet.") {
		t.Fatalf("blank list: %d\n%s", code, body)
	}

	// Create through the generated action.
	resp, err := c.PostForm(srv.URL+"/admin/ticket_types", url.Values{
		"name": {"Early bird"}, "price": {"25.00"}, "status": {"draft"},
	})
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	code, body := get(t, c, srv.URL+"/admin/ticket_types")
	if code != 200 || !strings.Contains(body, "Early bird") || !strings.Contains(body, "25.00") {
		t.Fatalf("list after create: %d\n%s", code, body)
	}

	// The manifest's own confirm sentence reaches the confirm page.
	i := strings.Index(body, `<td><a href="`)
	if i < 0 {
		t.Fatalf("no row link:\n%s", body)
	}
	rest := body[i+len(`<td><a href="`):]
	showURL := rest[:strings.Index(rest, `"`)]
	if code, body := get(t, c, srv.URL+showURL+"/delete"); code != 200 ||
		!strings.Contains(body, "Orders already placed keep their line items.") {
		t.Fatalf("confirm page: %d\n%s", code, body)
	}
}
