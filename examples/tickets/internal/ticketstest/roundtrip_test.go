// roundtrip_test.go is this app's restatement, at the whole-app level,
// of slice 1's Money Critical: a Money field's display formatter
// (formatCents, "$12.00") and its form-seed formatter (formatCentsPlain,
// "12.00", no leading "$" — parseCents rejects one) must round-trip
// through an untouched Edit save without drifting or 400ing. Every
// step goes through the real generated actions; nothing here is
// hand-written app logic to route around a gap.
package ticketstest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	ticket_typesstore "tickets/gen/store/ticket_types"
)

// TestFourStateRoundTrip walks create → 303 → show → edit-basics save
// UNCHANGED → 303 → list, all through the fully generated
// ticket_types resource: create (all fields, including the Advanced
// MaxPerOrder field), the show screen rendering Price as "$12.00", an
// edit-basics save that resubmits the Basics group's exact current
// values (the Money round-trip regression this app exists to guard),
// and the list screen showing the row afterward.
func TestFourStateRoundTrip(t *testing.T) {
	app, db := newApp(t)

	create := post(t, app, "/admin/ticket_types", url.Values{
		"Name":        {"VIP pass"},
		"Price":       {"12.00"},
		"Status":      {"draft"},
		"MaxPerOrder": {"4"},
	})
	wantStatus(t, create, http.StatusSeeOther)
	loc := create.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/ticket_types/") {
		t.Fatalf("Location = %q, want /admin/ticket_types/<id>", loc)
	}

	// Show: Price renders as a formatted dollar string, never raw cents.
	show := get(t, app, loc)
	wantStatus(t, show, http.StatusOK)
	showBody := show.Body.String()
	wantContains(t, showBody, "VIP pass")
	wantContains(t, showBody, "$12.00")
	wantContains(t, showBody, "draft")
	wantContains(t, showBody, "4") // MaxPerOrder, the Advanced field

	// Edit form: current values seed the inputs, Price WITHOUT the "$"
	// (formatCentsPlain) — the exact text parseCents must accept back.
	edit := get(t, app, loc+"/edit")
	wantStatus(t, edit, http.StatusOK)
	editBody := edit.Body.String()
	wantContains(t, editBody, `value="VIP pass"`)
	wantContains(t, editBody, `value="12.00"`)
	wantNotContains(t, editBody, `value="$12.00"`)
	wantContains(t, editBody, fmt.Sprintf(`action="%s/edit-basics"`, loc))

	// Edit-basics save UNCHANGED: resubmit exactly what the form seeded.
	// A regressed round-trip either 400s here (parseCents rejecting its
	// own formatter's output) or silently drifts the stored cents value.
	save := post(t, app, loc+"/edit-basics", url.Values{
		"Name":   {"VIP pass"},
		"Price":  {"12.00"},
		"Status": {"draft"},
	})
	wantStatus(t, save, http.StatusSeeOther)
	if got := save.Header().Get("Location"); got != loc {
		t.Errorf("Location = %q, want %q", got, loc)
	}

	id := idFromLocation(t, loc)
	row, err := ticket_typesstore.New(db).GetTicketType(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Price != 1200 {
		t.Errorf("Price = %d cents after an unchanged save, want 1200 (the Money round-trip regression)", row.Price)
	}
	if row.Name != "VIP pass" || row.Status != "draft" {
		t.Errorf("row = %+v, want Name=VIP pass Status=draft unchanged", row)
	}

	// List: the row shows up, Price formatted the same way show.html
	// formats it.
	list := get(t, app, "/admin/ticket_types")
	wantStatus(t, list, http.StatusOK)
	listBody := list.Body.String()
	wantContains(t, listBody, "VIP pass")
	wantContains(t, listBody, "$12.00")
}

func idFromLocation(t *testing.T, loc string) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscanf(strings.TrimPrefix(loc, "/admin/ticket_types/"), "%d", &id); err != nil {
		t.Fatalf("parse id from %q: %v", loc, err)
	}
	return id
}
