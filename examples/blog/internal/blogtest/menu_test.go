package blogtest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The blog is a topbar app. On a phone its sections are the Menu page
// at /menu, the index, whose rows are the bar's nav; every other page
// is a content page with one way up. A top-level page goes up to its
// own row on the Menu page, so focus lands there with scripts off, and
// a page under one goes up to that page.
func TestMenuIsTheIndexAndEveryPageHasAWayUp(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "A post", "Body.", true)

	menu := get(t, app, "/menu")
	wantStatus(t, menu, http.StatusOK)
	_, index, ok := strings.Cut(menu.Body.String(),
		`<div rst-shell-topbar="index">`)
	if !ok || !strings.Contains(index, `<a id="nav-blog" href="/">`) ||
		!strings.Contains(index, `<a id="nav-admin" href="/admin/posts">`) {
		t.Fatalf("GET /menu is not the index with both rows:\n%s",
			menu.Body.String())
	}
	for _, page := range []struct{ path, up, label string }{
		{"/", "/menu#nav-blog", "Menu"},
		{fmt.Sprintf("/posts/%d", id), "/", "Blog"},
		{"/admin/posts", "/menu#nav-admin", "Menu"},
		{"/admin/posts/new", "/admin/posts", "Posts admin"},
		{fmt.Sprintf("/admin/posts/%d", id), "/admin/posts", "Posts admin"},
	} {
		recorder := get(t, app, page.path)
		wantStatus(t, recorder, http.StatusOK)
		body := recorder.Body.String()
		want := fmt.Sprintf(`<a rst-shell-up href="%s" rel="up" `+
			`aria-label="Back to %s">`, page.up, page.label)
		if !strings.Contains(body, `<div rst-shell-topbar="page">`) ||
			!strings.Contains(body, want) {
			t.Errorf("GET %s: want a content page going up with %s",
				page.path, want)
		}
	}
}

// The account card holds the colour scheme and, in a blog that offers
// more than one language, the language: the signed-out avatar is the
// one way to both on a phone, and the card writes no old Menu.
func TestTheAccountCardHoldsTheSchemeAndTheLanguage(t *testing.T) {
	app, _ := newAppWith(t, "en", []string{"en", "es"})
	body := get(t, app, "/").Body.String()
	_, card, ok := strings.Cut(body,
		`<details rst-dropdown rst-shell-account name="rst-shell-menu">`)
	if !ok {
		t.Fatalf("GET /: no account card:\n%s", body)
	}
	card, _, _ = strings.Cut(card, "</details>\n</header>")
	for _, want := range []string{
		`<div rst-scheme="icons"`, `action="/_theme"`,
		`popovertarget="rst-language"`,
	} {
		wantContains(t, card, want)
	}
}
