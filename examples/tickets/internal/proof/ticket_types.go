// Package proof is the action shape of
// docs/superpowers/specs/2026-10-29-readable-code-design.md written by
// hand on a generated money form: the target step 5's generator will
// emit. Step 5 deletes it, once the generator writes this itself.
package proof

import (
	"context"
	"errors"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/op"
	ops_ticket_types "tickets/gen/ops/ticket_types"
)

// TicketTypes is the ticket type pages and what they depend on. Update
// is a field rather than a direct call so that a test can count saves
// and move a record's currency in the middle of one.
type TicketTypes struct {
	Update func(
		ctx context.Context, rc *rastrillo.Ctx, id int64,
		in ops_ticket_types.UpdateInput,
	) (ops_ticket_types.TicketType, error)
}

// The paths the ticket type pages link and redirect to, each written
// once. Only editBasics is registered here: the page and the advanced
// form are still the generated router's, so those two must match
// gen/router.go until step 5 puts every route on the table.
const (
	ticketTypePage action.Path = "/admin/ticket_types/{id}"
	editBasics     action.Path = "/admin/ticket_types/{id}/edit-basics"
	editAdvanced   action.Path = "/admin/ticket_types/{id}/edit-advanced"
)

// Routes is the one function that registers everything, given what the
// pages depend on. It takes the route table, not a router, so that
// every route it adds is recorded where a test or a doc can list it,
// and it only registers: `rastrillo routes` calls it on a recorder
// with an empty TicketTypes.
func Routes(routes *action.Routes, ticketTypes *TicketTypes) {
	routes.Post(editBasics, ticketTypes.updateBasics)
}

// updateBasics reads the record before the form, because the price's
// currency and stored amount come from the record.
func (ticketTypes *TicketTypes) updateBasics(visit *action.Visit) {
	record, err := readRecord(visit)
	if visit.Refused(err) {
		return
	}
	basics, err := readBasics(visit, record)
	if visit.RefusedShowing(err, basics) {
		return
	}
	basics, err = ticketTypes.save(visit, basics)
	if visit.RefusedShowing(err, basics) {
		return
	}
	visit.Redirect(ticketTypePage.For(record.ID))
}

// readRecord reads the body before the id, so a broken body is a 400
// even on an id that is not one or a record that is gone, as the
// generated action answers.
func readRecord(visit *action.Visit) (ops_ticket_types.TicketType, error) {
	if err := visit.ReadBody(); err != nil {
		return ops_ticket_types.TicketType{}, err
	}
	id, err := visit.PathID("id")
	if err != nil {
		return ops_ticket_types.TicketType{}, err
	}
	return ops_ticket_types.Get(visit.Context(), visit.Ctx(), id)
}

// save tries the update at most twice. A currency that moved under the
// form changes what the typed amount means, so the second try reads the
// submission again in the record's new currency, where the form's own
// currency check refuses it, instead of storing it in a currency the
// person never saw. This is form work, which is why it is not in the
// operation (see "The refusal helper" in the design).
func (ticketTypes *TicketTypes) save(
	visit *action.Visit, basics *basicsForm,
) (*basicsForm, error) {
	_, err := ticketTypes.Update(
		visit.Context(), visit.Ctx(), basics.record.ID, basics.input)
	if !errors.As(err, new(op.Conflict)) {
		return basics, err
	}
	record, err := ops_ticket_types.Get(
		visit.Context(), visit.Ctx(), basics.record.ID)
	if err != nil {
		return basics, err
	}
	if basics, err = readBasics(visit, record); err != nil {
		return basics, err
	}
	_, err = ticketTypes.Update(
		visit.Context(), visit.Ctx(), record.ID, basics.input)
	if !errors.As(err, new(op.Conflict)) {
		return basics, err
	}
	// A second conflict means the record moved after the second read,
	// so the form is drawn from a third, or it would name a currency
	// the next save is refused in. Its parse errors are dropped, as
	// the generated action's last try drops them.
	if record, err = ops_ticket_types.Get(
		visit.Context(), visit.Ctx(), record.ID); err != nil {
		return basics, err
	}
	basics, _ = readBasics(visit, record)
	return basics, basics.currencyMovedAgain()
}
