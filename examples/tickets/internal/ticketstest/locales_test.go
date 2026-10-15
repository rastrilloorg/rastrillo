// locales_test.go is the example in two locales (spec §10, §12 item
// 8): the same generated screens, one template set, rendered through
// ui.Pages in English and Spanish by the request's locale.
package ticketstest

import (
	"html"
	"html/template"
	"net/http"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/currency"

	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
)

// The example in two locales: the same generated screens, one template
// set, rendered in English and Spanish by the request's locale. App
// keys have no Spanish catalog, so they fall back to English, as the
// layered catalogs promise; framework strings and money are Spanish.
func TestTicketTypesInTwoLocales(t *testing.T) {
	app, _ := newAppWith(t, "en", []string{"en", "es", "ar"})
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("1,284.50", "en", "USD")), http.StatusSeeOther)
	for _, c := range []struct{ path, locale string }{{"/admin/ticket_types/1", "en"}, {"/es/admin/ticket_types/1", "es"}} {
		body := get(t, app, c.path).Body.String()
		want, _ := currency.Format(c.locale, 128450, "USD")
		wantContains(t, body, template.HTMLEscapeString(want))
		wantContains(t, body, template.HTMLEscapeString(rastrillo.BaseCatalogs()[c.locale]["rastrillo.ui.shell_language"]))
	}
	// The real embedded layout rendered strictly, in the request's
	// language and direction, with the switcher.
	esBody := get(t, app, "/es/admin/ticket_types/1").Body.String()
	wantContains(t, esBody, `<html lang="es" dir="ltr">`)
	wantContains(t, esBody, "rst-locale")
	wantContains(t, get(t, app, "/ar/admin/ticket_types/1").Body.String(), `<html lang="ar" dir="rtl">`)
	// The normalising messages in a translated locale (spec §12 item 15).
	es := rastrillo.BaseCatalogs()["es"]
	body := post(t, app, "/es/admin/ticket_types", moneyPost("€5", "es", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.ReplaceAll(es["rastrillo.ui.money_wrong_currency"], "{currency}", "USD")))
	body = post(t, app, "/es/admin/ticket_types", moneyPost("1,500", "es", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.NewReplacer("{a}", "1,5 USD", "{b}", "1500 USD").Replace(es["rastrillo.ui.money_ambiguous"])))
	one, _ := newAppWith(t, "en", nil)
	if body := get(t, one, "/admin/ticket_types").Body.String(); strings.Contains(body, "rst-locale") || !strings.Contains(body, `<html lang="en" dir="ltr">`) {
		t.Errorf("a one-locale tickets has a switcher or the wrong language:\n%.400s", body)
	}
	// The edit form in Spanish seeds the amount in Spanish conventions
	// and says so in its hidden locale input.
	body = get(t, app, "/es/admin/ticket_types/1/edit").Body.String()
	wantContains(t, body, `name="Price.locale" value="es"`)
	wantContains(t, body, `value="1284,50"`)
}
