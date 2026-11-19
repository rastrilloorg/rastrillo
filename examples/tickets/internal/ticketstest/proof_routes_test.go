package ticketstest

import (
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"

	"tickets/internal/proof"
)

// The proof's Routes must register on a recorder given an empty
// TicketTypes: `rastrillo routes` has no database to give it.
func TestTheProofRegistersOnARecorderWithNothingWired(t *testing.T) {
	recorder := action.NewRecorder()
	proof.Routes(recorder, &proof.TicketTypes{})
	want := []action.Route{{
		Method:  "POST",
		Path:    "/admin/ticket_types/{id}/edit-basics",
		Handler: "updateBasics",
		Action:  "tickets/internal/proof.(*TicketTypes).updateBasics",
	}}
	if got := recorder.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %+v, want %+v", got, want)
	}
}
