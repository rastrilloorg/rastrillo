// required_test.go exercises manifest/ticket_types.toml's `required =
// true` on Name and Price — the generated create and edit-basics
// actions' server-side validation branch (internal/generate/
// actions.go's parseField/requiredMessage). Status and MaxPerOrder
// declare no `required`, so they're never checked here.
package ticketstest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	ticket_typesstore "tickets/gen/store/ticket_types"
)

func TestCreateBlankNameIs400WithTheFieldErrorVisible(t *testing.T) {
	app, db := newApp(t)

	rec := post(t, app, "/admin/ticket_types", url.Values{
		"Name":   {"   "},
		"Price":  {"5.00"},
		"Status": {"draft"},
	})
	wantStatus(t, rec, http.StatusBadRequest)
	body := rec.Body.String()
	wantContains(t, body, "required")
	wantContains(t, body, "Name is required")
	// The 400 re-render is the form again, not a bare error page: the
	// submitted (non-Name) values survive for correction.
	wantContains(t, body, `value="5.00"`)

	n, err := ticket_typesstore.New(db).CountTicketTypes(context.Background(), ticket_typesstore.CountTicketTypesParams{Search: "", FilterStatus: ""})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("created %d rows, want 0: a 400 must not write", n)
	}
}

func TestCreateBlankPriceIs400WithTheFieldErrorVisible(t *testing.T) {
	app, db := newApp(t)

	rec := post(t, app, "/admin/ticket_types", url.Values{
		"Name":   {"General"},
		"Price":  {""},
		"Status": {"draft"},
	})
	wantStatus(t, rec, http.StatusBadRequest)
	body := rec.Body.String()
	wantContains(t, body, "required")
	wantContains(t, body, "Price is required")
	wantContains(t, body, `value="General"`)

	n, err := ticket_typesstore.New(db).CountTicketTypes(context.Background(), ticket_typesstore.CountTicketTypesParams{Search: "", FilterStatus: ""})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("created %d rows, want 0: a 400 must not write", n)
	}
}

// "0" is a valid required Money value: required-ness checks the RAW
// submitted text, never the parsed cents value, so an explicit zero
// price is accepted, only a truly blank input is rejected.
func TestCreateZeroPriceIsAccepted(t *testing.T) {
	app, db := newApp(t)

	rec := post(t, app, "/admin/ticket_types", url.Values{
		"Name":   {"Free entry"},
		"Price":  {"0"},
		"Status": {"draft"},
	})
	wantStatus(t, rec, http.StatusSeeOther)

	rows, err := ticket_typesstore.New(db).ListTicketTypes(context.Background(), ticket_typesstore.ListTicketTypesParams{
		Search: "", FilterStatus: "", PageOffset: 0, PageLimit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Price != 0 {
		t.Fatalf("rows = %+v, want exactly one row with Price 0", rows)
	}
}

func TestEditBasicsBlankNameIs400AndLeavesTheRecordUnchanged(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "General", 1500, "draft", "")

	rec := post(t, app, "/admin/ticket_types/"+strconv.FormatInt(id, 10)+"/edit-basics", url.Values{
		"Name":   {""},
		"Price":  {"15.00"},
		"Status": {"draft"},
	})
	wantStatus(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), "Name is required")

	row, err := ticket_typesstore.New(db).GetTicketType(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Name != "General" {
		t.Errorf("Name = %q after a 400, want unchanged %q", row.Name, "General")
	}
}

func TestEditBasicsBlankPriceIs400AndLeavesTheRecordUnchanged(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "General", 1500, "draft", "")

	rec := post(t, app, "/admin/ticket_types/"+strconv.FormatInt(id, 10)+"/edit-basics", url.Values{
		"Name":   {"General"},
		"Price":  {""},
		"Status": {"draft"},
	})
	wantStatus(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), "Price is required")

	row, err := ticket_typesstore.New(db).GetTicketType(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Price != 1500 {
		t.Errorf("Price = %d after a 400, want unchanged 1500", row.Price)
	}
}
