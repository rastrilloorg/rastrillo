package ticketstest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/basepath/basepathtest"

	genlocales "tickets/gen/locales"
)

var acme = basepath.Config{Origin: "https://example.com", Slug: "acme"}

// under sends a request on the product origin, as the edge forwards it
// to the tenant at /acme. A POST says it is same-origin, as a browser's
// form post does, so csrf lets it through.
func under(t *testing.T, h http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Host = "example.com"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// page GETs target under /acme and fails on any link that would leave
// the tenant for the hub.
func page(t *testing.T, h http.Handler, target string) string {
	t.Helper()
	rec := under(t, h, http.MethodGet, target, nil)
	wantStatus(t, rec, http.StatusOK)
	if leaks := basepathtest.Leaks(rec.Body.Bytes(), "/acme"); len(leaks) > 0 {
		t.Errorf("GET %s links outside /acme: %q", target, leaks)
	}
	return rec.Body.String()
}

// Every generated screen, served under /acme, links, posts and
// redirects within /acme. A link without the base would take the
// reader to the hub's page of the same name, or to its 404.
func TestTicketsUnderABasePath(t *testing.T) {
	app, db := newAppWithAt(t, filepath.Join(t.TempDir(), "t.db"), genlocales.DefaultLocale, genlocales.Locales, acme)

	page(t, app, "/acme/admin/ticket_types")
	page(t, app, "/acme/admin/ticket_types/new")

	create := under(t, app, http.MethodPost, "/acme/admin/ticket_types", url.Values{
		"Name": {"VIP pass"}, "Price": {"12.00"}, "Price.locale": {"en"}, "Price.currency": {"USD"},
		"Status": {"draft"}, "MaxPerOrder": {"4"},
	})
	wantStatus(t, create, http.StatusSeeOther)
	loc := create.Header().Get("Location")
	if loc != "/acme/admin/ticket_types/1" {
		t.Fatalf("Location = %q, want /acme/admin/ticket_types/1", loc)
	}
	page(t, app, loc)
	wantContains(t, page(t, app, loc+"/edit"), `action="/acme/admin/ticket_types/1/edit-basics"`)
	wantContains(t, page(t, app, loc+"/delete"), `action="/acme/admin/ticket_types/1/delete"`)

	// Eleven matches make two pages, so the list draws its pagination.
	for i := range 10 {
		seed(t, db, fmt.Sprintf("VIP %d", i), 1000, "draft", "")
	}
	list := page(t, app, "/acme/admin/ticket_types?q=VIP&status=draft")
	wantContains(t, list, `action="/acme/admin/ticket_types"`)
	wantContains(t, list, `href="/acme/admin/ticket_types?q=VIP&amp;status=on_sale"`)
	wantContains(t, list, `href="/acme/admin/ticket_types?q=VIP&amp;status=draft&amp;page=2"`)

	if rec := under(t, app, http.MethodGet, "/admin/ticket_types", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin/ticket_types on the product origin: %d, want 404: it is the hub's path, not this tenant's", rec.Code)
	}
}

// With a second locale the layout draws the switcher, which posts to
// /acme/_locale: /_locale is the hub's own switch.
func TestTicketsLocaleSwitchUnderABasePath(t *testing.T) {
	app, _ := newAppWithAt(t, filepath.Join(t.TempDir(), "t.db"), "en", []string{"en", "es"}, acme)
	wantContains(t, page(t, app, "/acme/admin/ticket_types"), `action="/acme/_locale"`)
}
