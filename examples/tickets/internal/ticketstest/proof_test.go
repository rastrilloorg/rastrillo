// proof_test.go holds the action-shape proof (internal/proof) to the
// generated edit-basics route it rewrites: the same requests on both
// routers must answer alike, except that a field refusal is a 422 in
// the proof where the generated route answers 400 (behaviour change 1
// of docs/superpowers/specs/2026-10-29-readable-code-design.md).
package ticketstest

import (
	"context"
	"database/sql"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"

	ops_ticket_types "tickets/gen/ops/ticket_types"
)

func TestTheProofAnswersLikeTheGeneratedEditForm(t *testing.T) {
	oversized := "Name=" + strings.Repeat("x", 1<<20)
	cases := []parityCase{
		{name: "unchanged save",
			form: withValues(moneyPost("12.50", "en", "USD"),
				"Price.seed", "12.50"),
			status: http.StatusSeeOther},
		{name: "changed save",
			form: withValues(moneyPost("15.00", "en", "USD"),
				"Name", "Gold"),
			status: http.StatusSeeOther},
		{name: "blank required price",
			form:   moneyPost("", "en", "USD"),
			status: http.StatusBadRequest},
		{name: "an amount that does not parse",
			form:   moneyPost("twelve", "en", "USD"),
			status: http.StatusBadRequest},
		{name: "an amount in another currency (an error with args)",
			form:   moneyPost("12.50", "en", "EUR"),
			status: http.StatusBadRequest,
			shows: []string{html.EscapeString(
				catalogEN("rastrillo.ui.money_currency_changed"),
			)}},
		{name: "an amount in another currency's sign",
			form:   moneyPost("€5", "en", "USD"),
			status: http.StatusBadRequest,
			shows: []string{html.EscapeString(strings.ReplaceAll(
				catalogEN("rastrillo.ui.money_wrong_currency"),
				"{currency}", "USD"))}},
		{name: "an amount with two readings",
			form:   moneyPost("1.500", "en", "USD"),
			status: http.StatusBadRequest,
			shows: []string{html.EscapeString(strings.NewReplacer(
				"{a}", "1.5 USD", "{b}", "1500 USD",
			).Replace(catalogEN("rastrillo.ui.money_ambiguous")))}},
		{name: "a parse error and a validation error together",
			form: withValues(moneyPost("twelve", "en", "USD"),
				"Name", ""),
			status: http.StatusBadRequest,
			shows:  []string{"Name is required"}},
		{name: "another field's error keeps the verified seed",
			form: withValues(moneyPost("12.50", "en", "USD"),
				"Price.seed", "12.50", "Name", ""),
			status: http.StatusBadRequest,
			shows: []string{
				`name="Price.seed" value="12.50"`, "Name is required",
			}},
		{name: "a rejected locale reseeds from the record",
			form: withoutValues(moneyPost("99", "", "USD"),
				"Price.locale"),
			status: http.StatusBadRequest,
			shows:  []string{`value="12.50"`}},
		{name: "an oversized body", body: oversized,
			status: http.StatusBadRequest},
		{name: "an oversized body to a missing record", body: oversized,
			path:   "/admin/ticket_types/99/edit-basics",
			status: http.StatusBadRequest},
		{name: "an oversized body to an id that is not one",
			body:   oversized,
			path:   "/admin/ticket_types/one/edit-basics",
			status: http.StatusBadRequest},
		{name: "no such ticket type",
			form:   moneyPost("15.00", "en", "USD"),
			path:   "/admin/ticket_types/99/edit-basics",
			status: http.StatusNotFound},
		{name: "an id that is not one",
			form:   moneyPost("15.00", "en", "USD"),
			path:   "/admin/ticket_types/one/edit-basics",
			status: http.StatusNotFound},
		{name: "a re-shown form submitted again",
			form: withoutValues(moneyPost("99", "", "USD"),
				"Price.locale"),
			then:   url.Values{"Name": {"Gold"}},
			status: http.StatusSeeOther},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			generated := ask(t, newApp, example)
			proofed := ask(t, newProofApp, example)
			if generated.status != example.status {
				t.Fatalf("the generated route answered %d, want %d; "+
					"the case is not the scenario it names:\n%s",
					generated.status, example.status, generated.body)
			}
			compareAnswers(t, generated, proofed)
			for _, text := range example.shows {
				wantContains(t, generated.body, text)
			}
		})
	}
}

func TestABadPriceBesideAGoodNameWritesNothing(t *testing.T) {
	calls := 0
	app, database := newProofAppWith(t, proofSetup{update: func(
		ctx context.Context, rc *rastrillo.Ctx, id int64,
		in ops_ticket_types.UpdateInput,
	) (ops_ticket_types.TicketType, error) {
		calls++
		return ops_ticket_types.Update(ctx, rc, id, in)
	}})
	seed(t, database, "VIP", 1250, "draft", "4")
	submitted := withValues(moneyPost("twelve", "en", "USD"),
		"Name", "Gold")
	wantStatus(t, post(t, app, editBasics, submitted),
		http.StatusUnprocessableEntity)
	if calls != 0 {
		t.Errorf("Update ran %d times for a price that does not parse",
			calls)
	}
	wantRow(t, database, "VIP 1250 USD")
}

func TestAMovedCurrencyIsReparsedFromTheSubmission(t *testing.T) {
	calls := 0
	app, database := newProofAppWith(t, proofSetup{update: func(
		ctx context.Context, rc *rastrillo.Ctx, id int64,
		in ops_ticket_types.UpdateInput,
	) (ops_ticket_types.TicketType, error) {
		calls++
		_, err := rc.DB.ExecContext(ctx, `UPDATE ticket_types
			SET price_currency = 'EUR' WHERE id = ?`, id)
		if err != nil {
			t.Errorf("moving the currency: %v", err)
		}
		return ops_ticket_types.TicketType{}, op.Conflict{}
	}})
	seed(t, database, "VIP", 1250, "draft", "4")
	recorder := post(t, app, editBasics,
		moneyPost("15.00", "en", "USD"))
	wantStatus(t, recorder, http.StatusUnprocessableEntity)
	if calls != 1 {
		t.Errorf("Update ran %d times, want 1: the submission says "+
			"USD and the record is now in EUR", calls)
	}
	wantRow(t, database, "VIP 1250 EUR")
	body := recorder.Body.String()
	wantContains(t, body, `name="Price.currency" value="EUR"`)
	wantContains(t, body, `value="15.00"`)
	wantContains(t, body, `id="Price-error">`+html.EscapeString(
		catalogEN("rastrillo.ui.money_currency_changed")))
}

// The submission keeps the price unchanged under a verified seed: after
// two moves the generated action shows the amount as typed and drops
// the seed, so the next save reads the amount instead of trusting it.
func TestASecondConflictSaysTheCurrencyChanged(t *testing.T) {
	for _, example := range []struct {
		name      string
		conflicts int
		status    int
		row       string
	}{
		{"twice", 2, http.StatusUnprocessableEntity, "VIP 1250 USD"},
		{"once then success", 1, http.StatusSeeOther, "Gold 1250 USD"},
	} {
		t.Run(example.name, func(t *testing.T) {
			calls := 0
			app, database := newProofAppWith(t, proofSetup{update: func(
				ctx context.Context, rc *rastrillo.Ctx, id int64,
				in ops_ticket_types.UpdateInput,
			) (ops_ticket_types.TicketType, error) {
				calls++
				if calls <= example.conflicts {
					return ops_ticket_types.TicketType{}, op.Conflict{}
				}
				return ops_ticket_types.Update(ctx, rc, id, in)
			}})
			seed(t, database, "VIP", 1250, "draft", "4")
			submitted := withValues(moneyPost("12.50", "en", "USD"),
				"Price.seed", "12.50", "Name", "Gold")
			recorder := post(t, app, editBasics, submitted)
			wantStatus(t, recorder, example.status)
			if calls != 2 {
				t.Errorf("Update ran %d times, want 2", calls)
			}
			wantRow(t, database, example.row)
			if example.status == http.StatusSeeOther {
				return
			}
			body := recorder.Body.String()
			wantContains(t, body, `id="Price" name="Price" type="text" `+
				`inputmode="decimal" value="12.50"`)
			wantContains(t, body, `name="Price.seed" value=""`)
			wantContains(t, body, `id="Price-error">`+html.EscapeString(
				catalogEN("rastrillo.ui.money_currency_changed")))
		})
	}
}

// A real second conflict means the record moved after the second read,
// so the form must be drawn from a third, as the generated action's
// try == 2 reloads: otherwise it names the old currency and the next
// save is refused again.
func TestASecondMoveIsShownInTheNewCurrency(t *testing.T) {
	calls := 0
	app, database := newProofAppWith(t, proofSetup{update: func(
		ctx context.Context, rc *rastrillo.Ctx, id int64,
		in ops_ticket_types.UpdateInput,
	) (ops_ticket_types.TicketType, error) {
		calls++
		if calls == 2 {
			_, err := rc.DB.ExecContext(ctx, `UPDATE ticket_types
				SET price_currency = 'EUR' WHERE id = ?`, id)
			if err != nil {
				t.Errorf("moving the currency: %v", err)
			}
		}
		return ops_ticket_types.TicketType{}, op.Conflict{}
	}})
	seed(t, database, "VIP", 1250, "draft", "4")
	submitted := withValues(moneyPost("12.50", "en", "USD"),
		"Price.seed", "12.50", "Name", "Gold")
	recorder := post(t, app, editBasics, submitted)
	wantStatus(t, recorder, http.StatusUnprocessableEntity)
	if calls != 2 {
		t.Errorf("Update ran %d times, want 2", calls)
	}
	wantRow(t, database, "VIP 1250 EUR")
	body := recorder.Body.String()
	wantContains(t, body, `name="Price.currency" value="EUR"`)
	wantContains(t, body, `id="Price" name="Price" type="text" `+
		`inputmode="decimal" value="12.50"`)
	wantContains(t, body, `name="Price.seed" value=""`)
	wantContains(t, body, `id="Price-error">`+html.EscapeString(
		catalogEN("rastrillo.ui.money_currency_changed")))
}

func TestTheProofKeepsTheBootCheck(t *testing.T) {
	t.Run("a nil database is the booted one", func(t *testing.T) {
		app, database := newProofAppWith(t, proofSetup{
			database: func(*sql.DB) *sql.DB { return nil },
		})
		seed(t, database, "VIP", 1250, "draft", "4")
		wantStatus(t, post(t, app, editBasics,
			moneyPost("15.00", "en", "USD")), http.StatusSeeOther)
		wantRow(t, database, "VIP 1500 USD")
	})
	t.Run("another database is refused", func(t *testing.T) {
		other, err := sql.Open("sqlite",
			filepath.Join(t.TempDir(), "other.db"))
		if err != nil {
			t.Fatalf("opening another database: %v", err)
		}
		t.Cleanup(func() { other.Close() })
		app, database := newProofAppWith(t, proofSetup{
			database: func(*sql.DB) *sql.DB { return other },
		})
		seed(t, database, "VIP", 1250, "draft", "4")
		recorder := post(t, app, editBasics,
			moneyPost("15.00", "en", "USD"))
		wantStatus(t, recorder, http.StatusInternalServerError)
		wantContains(t, recorder.Body.String(), "Something went wrong.")
		wantRow(t, database, "VIP 1250 USD")
	})
}
