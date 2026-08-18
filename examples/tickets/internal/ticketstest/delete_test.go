// delete_test.go walks the generated delete flow end to end — the §9
// rule made concrete: a destructive action is its own confirm-page URL
// first (a GET that never mutates), and only the sibling POST deletes.
// Every step goes through the fully generated actions and templates.
package ticketstest

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	ticket_typesstore "tickets/gen/store/ticket_types"
)

func TestDeleteFlow(t *testing.T) {
	app, db := newApp(t)

	create := post(t, app, "/admin/ticket_types", url.Values{
		"Name": {"Doomed pass"}, "Price": {"5.00"}, "Status": {"draft"},
	})
	wantStatus(t, create, http.StatusSeeOther)
	loc := create.Header().Get("Location")
	id := idFromLocation(t, loc)

	// The confirm page: names the record, asks the question, and its
	// GET changes nothing.
	confirm := get(t, app, loc+"/delete")
	wantStatus(t, confirm, http.StatusOK)
	body := confirm.Body.String()
	wantContains(t, body, "Doomed pass")
	wantContains(t, body, "Delete this ticket type? This cannot be undone.")
	wantContains(t, body, `action="`+loc+`/delete"`)
	if _, err := ticket_typesstore.New(db).GetTicketType(context.Background(), id); err != nil {
		t.Fatalf("the confirm GET must not mutate: %v", err)
	}

	// Cancel is a plain link back to the record, first in the DOM
	// before the destructive submit (ui/partials/confirm-form.html's
	// load-bearing order).
	cancelAt := strings.Index(body, `href="`+loc+`"`)
	submitAt := strings.Index(body, `type="submit"`)
	if cancelAt < 0 || submitAt < 0 || cancelAt > submitAt {
		t.Fatalf("cancel link must precede the destructive submit:\n%s", body)
	}

	// The POST deletes and lands on the list.
	del := post(t, app, loc+"/delete", url.Values{})
	wantStatus(t, del, http.StatusSeeOther)
	if got := del.Header().Get("Location"); got != "/admin/ticket_types" {
		t.Errorf("Location = %q, want the list", got)
	}
	if _, err := ticket_typesstore.New(db).GetTicketType(context.Background(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("row survived the delete: %v", err)
	}

	// Everything about the gone row answers 404 — including a repeat
	// delete, which must never read as a silent success.
	for _, probe := range []string{loc, loc + "/delete"} {
		if w := get(t, app, probe); w.Code != http.StatusNotFound {
			t.Errorf("GET %s after delete = %d, want 404", probe, w.Code)
		}
	}
	if w := post(t, app, loc+"/delete", url.Values{}); w.Code != http.StatusNotFound {
		t.Errorf("repeat delete = %d, want 404", w.Code)
	}
}
