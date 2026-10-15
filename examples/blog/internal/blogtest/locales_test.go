// locales_test.go is the example in two locales (spec §10, §12 item
// 8): the same screens, hand-written and generated alike, one
// template set, rendered through ui.Pages in English and Spanish by
// the request's locale. The hand-written public post list ("/") and
// the fully generated admin detail screen ("posts/show") cover both
// kinds this app renders.
package blogtest

import (
	"fmt"
	"html/template"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"

	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
)

// The example in two locales: the same screens, one template set,
// rendered in English and Spanish by the request's locale. App keys
// (resource.posts.*, the hand screens' own literal strings) have no
// Spanish or Arabic catalog, so they stay in English, as the layered
// catalogs promise (blog.NewApp's own doc comment); the framework's
// chrome (the switcher) renders in each reader's language.
func TestBlogInTwoLocales(t *testing.T) {
	app, db := newAppWith(t, "en", []string{"en", "es", "ar"})
	published := seed(t, db, "Go post", "First paragraph.\n\nSecond paragraph.", true)

	for _, c := range []struct{ list, show, locale string }{
		{"/", fmt.Sprintf("/admin/posts/%d", published), "en"},
		{"/es/", fmt.Sprintf("/es/admin/posts/%d", published), "es"},
	} {
		want := template.HTMLEscapeString(rastrillo.BaseCatalogs()[c.locale]["rastrillo.ui.shell_language"])
		listRec := get(t, app, c.list)
		wantStatus(t, listRec, 200)
		wantContains(t, listRec.Body.String(), want)
		showRec := get(t, app, c.show)
		wantStatus(t, showRec, 200)
		wantContains(t, showRec.Body.String(), want)
	}
	// The real embedded layout rendered strictly, in the request's
	// language and direction, with the switcher.
	esBody := get(t, app, "/es/").Body.String()
	wantContains(t, esBody, `<html lang="es" dir="ltr">`)
	wantContains(t, esBody, "rst-locale")
	wantContains(t, get(t, app, fmt.Sprintf("/ar/admin/posts/%d", published)).Body.String(), `<html lang="ar" dir="rtl">`)

	one, _ := newAppWith(t, "en", nil)
	if body := get(t, one, "/").Body.String(); strings.Contains(body, "rst-locale") || !strings.Contains(body, `<html lang="en" dir="ltr">`) {
		t.Errorf("a one-locale blog has a switcher or the wrong language:\n%.400s", body)
	}
}
