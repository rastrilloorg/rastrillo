// money_test.go is the money form contract (spec §5, §12 items 4, 5
// and 15) through the generated ticket_types screens: an amount is
// stored in minor units with the resource's currency, a form whose
// currency changed under it stores nothing, and an edit form's seed
// lets an unchanged amount save while a forged one cannot.
package ticketstest

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// catalogEN is the framework's approved English for key, so these
// tests follow whatever the copy review approved.
func catalogEN(key string) string { return rastrillo.BaseCatalog()[key] }

func moneyPost(price, locale, cur string) url.Values {
	return url.Values{"Name": {"VIP"}, "Price": {price}, "Price.locale": {locale}, "Price.currency": {cur}, "Status": {"draft"}}
}

func TestCreateStoresMinorUnitsAndCurrency(t *testing.T) {
	app, db := newApp(t)
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("1,284.50", "en", "USD")), http.StatusSeeOther)
	var minor int64
	var cur string
	db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types`).Scan(&minor, &cur)
	if minor != 128450 || cur != "USD" {
		t.Errorf("stored %d %s, want 128450 USD", minor, cur)
	}
	show := get(t, app, "/admin/ticket_types/1")
	wantContains(t, show.Body.String(), "$1,284.50")
}

// A create form whose currency changed between GET and POST: the text
// is kept, nothing is stored, and the person is told.
func TestCreateWithChangedCurrencyKeepsTextAndStoresNothing(t *testing.T) {
	app, db := newApp(t)
	rec := post(t, app, "/admin/ticket_types", moneyPost("12.50", "en", "EUR"))
	wantStatus(t, rec, http.StatusBadRequest)
	body := rec.Body.String()
	wantContains(t, body, `value="12.50"`)
	wantContains(t, body, `name="Price.currency" value="USD"`)
	wantContains(t, body, "The currency changed while you were editing. Check the amount and save again.")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM ticket_types`).Scan(&n)
	if n != 0 {
		t.Errorf("a mismatched currency stored %d rows", n)
	}
}

// Spec §12 item 15, through the generated tickets screens.
func TestNormalisedInputSavesAndReseeds(t *testing.T) {
	app, db := newApp(t)
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("US$ 1,284.5", "en", "USD")), http.StatusSeeOther)
	var minor int64
	db.QueryRow(`SELECT price_minor FROM ticket_types`).Scan(&minor)
	if minor != 128450 {
		t.Fatalf("stored %d, want 128450", minor)
	}
	edit := get(t, app, "/admin/ticket_types/1/edit").Body.String()
	wantContains(t, edit, `value="1284.50"`)
	wantContains(t, edit, `name="Price.seed" value="1284.50"`)
	// An unchanged edit saves through the seed check.
	v := moneyPost("1284.50", "en", "USD")
	v.Set("Price.seed", "1284.50")
	wantStatus(t, post(t, app, "/admin/ticket_types/1/edit-basics", v), http.StatusSeeOther)
	// A forged seed, and a seed on create, store nothing.
	v = moneyPost("9.99", "en", "USD")
	v.Set("Price.seed", "9.99")
	wantStatus(t, post(t, app, "/admin/ticket_types/1/edit-basics", v), http.StatusBadRequest)
	wantStatus(t, post(t, app, "/admin/ticket_types", v), http.StatusBadRequest)
	db.QueryRow(`SELECT price_minor FROM ticket_types WHERE id = 1`).Scan(&minor)
	if minor != 128450 {
		t.Errorf("a forged seed changed the record to %d", minor)
	}
	// A failure elsewhere echoes the person's own text with no seed, and
	// the resubmission reads it the same way.
	v = moneyPost("US$ 1,284.5", "en", "USD")
	v.Set("Name", "")
	rec := post(t, app, "/admin/ticket_types", v)
	wantStatus(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), `value="US$ 1,284.5"`)
	wantContains(t, rec.Body.String(), `name="Price.seed" value=""`) // typed text, not a verified seed
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("US$ 1,284.5", "en", "USD")), http.StatusSeeOther)
	// The two new messages, with their values.
	body := post(t, app, "/admin/ticket_types", moneyPost("€5", "en", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.ReplaceAll(catalogEN("rastrillo.ui.money_wrong_currency"), "{currency}", "USD")))
	body = post(t, app, "/admin/ticket_types", moneyPost("1.500", "en", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.NewReplacer("{a}", "1.5 USD", "{b}", "1500 USD").Replace(catalogEN("rastrillo.ui.money_ambiguous"))))
}

func TestEditRejectedLocaleReseedsFromTheRecord(t *testing.T) {
	app, _ := newApp(t)
	post(t, app, "/admin/ticket_types", moneyPost("12.50", "en", "USD"))
	v := moneyPost("99", "", "USD")
	v.Del("Price.locale")
	rec := post(t, app, "/admin/ticket_types/1/edit-basics", v)
	wantStatus(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), `value="12.50"`) // the server's seed, not the unparseable text
}

// Clearing: tickets' Price is required, so a blank one is refused and
// the stored amount and its currency stay together.
func TestBlankRequiredPriceKeepsTheAmountAndItsCurrency(t *testing.T) {
	app, db := newApp(t)
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("12.50", "en", "USD")), http.StatusSeeOther)
	wantStatus(t, post(t, app, "/admin/ticket_types/1/edit-basics", moneyPost("", "en", "USD")), http.StatusBadRequest)
	var minor int64
	var cur string
	db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types WHERE id = 1`).Scan(&minor, &cur)
	if minor != 1250 || cur != "USD" {
		t.Errorf("stored %d %s, want 1250 USD", minor, cur)
	}
}
