// filter_test.go exercises the generated Status dropdown
// (manifest/ticket_types.toml's [[list.filters]]) — the F3 seam's
// first fully generated consumer (design doc). Every assertion here
// is against internal/generate/actions.go's own pinned contract:
// normalize<Field> collapses anything unrecognized to "" (all, never a
// 400), a filter click carries the current search but never page, and
// pagination carries both q and the filter's own query param.
package ticketstest

import (
	"fmt"
	"net/http"
	"testing"
)

func TestFilterShowsOnlyTheAppliedStatus(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Early bird", 1000, "draft", "")
	seed(t, db, "General", 2000, "on_sale", "")
	seed(t, db, "Gone", 3000, "sold_out", "")

	rec := get(t, app, "/admin/ticket_types?status=draft")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "Early bird")
	wantNotContains(t, body, "General")
	wantNotContains(t, body, "Gone")
	// The applied choice is marked, and the summary names it.
	wantContains(t, body, `aria-current="true"`)
	wantContains(t, body, `aria-label="Filter by Status: Draft"`)
}

func TestFilterComposesWithSearch(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Weekend pass", 1000, "draft", "")
	seed(t, db, "Weekend VIP", 2000, "on_sale", "")
	seed(t, db, "Day pass", 1500, "draft", "")

	rec := get(t, app, "/admin/ticket_types?q=Weekend&status=draft")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "Weekend pass")
	wantNotContains(t, body, "Weekend VIP") // matches q, wrong status
	wantNotContains(t, body, "Day pass")    // matches status, wrong q
	// A search from a filtered list keeps the filter (the hidden carry
	// pair list-bar-search renders).
	wantContains(t, body, `<input type="hidden" name="status" value="draft">`)
}

func TestFilterAndSearchCarryIntoPaginationPageLast(t *testing.T) {
	app, db := newApp(t)
	for i := 0; i < 11; i++ {
		seed(t, db, fmt.Sprintf("Ticket %02d", i), int64(1000+i), "draft", "")
	}
	seed(t, db, "Ticket other", 9999, "on_sale", "") // wrong status, excluded from the count that pages

	rec := get(t, app, "/admin/ticket_types?q=Ticket&status=draft")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	// q, then status, then page — checked on the page-2 pagination link
	// itself so the dropdown's own hrefs (which also carry q and status,
	// but never page) can't satisfy this by accident.
	wantContains(t, body, `href="/admin/ticket_types?q=Ticket&amp;status=draft&amp;page=2"`)
}

func TestFilterAllItemClearsTheFilterButKeepsSearch(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Weekend pass", 1000, "draft", "")

	rec := get(t, app, "/admin/ticket_types?q=Weekend&status=draft")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	// The All item's own href carries q but never a status param.
	wantContains(t, body, `href="/admin/ticket_types?q=Weekend">All`)
}

func TestUnknownFilterValueNormalizesToAll(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Early bird", 1000, "draft", "")
	seed(t, db, "General", 2000, "on_sale", "")

	rec := get(t, app, "/admin/ticket_types?status=bogus")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	// A stale bookmark or hand-edited URL never 400s; it just shows
	// everything, same as no filter applied at all.
	wantContains(t, body, "Early bird")
	wantContains(t, body, "General")
	wantContains(t, body, `aria-label="Filter by Status: All"`)
}
