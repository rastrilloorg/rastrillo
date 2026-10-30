package action

import (
	"errors"
	"net/http"

	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/view"
	"github.com/go-chi/chi/v5"
)

// WholeForm is the form.Errors key for a message about the whole form
// rather than one field. A template passes it to the form-error partial.
const WholeForm = ""

// Form is a page's form as its action needs to see it again: the one
// method an action calls when a refusal should show the form with what
// the person typed and what was wrong.
type Form interface {
	ShowAgain(visit *Visit, status int, errors form.Errors)
}

// Refused answers err and reports whether it did, so an action reads
// `if visit.Refused(err) { return }`. A nil err writes nothing.
func (visit *Visit) Refused(err error) bool {
	if err == nil {
		return false
	}
	if badRequest, ok := badRequestIn(err); ok {
		http.Error(visit.response, badRequest.Message, badRequest.Status)
		return true
	}
	view.Refuse(
		visit.ctx, visit.response, visit.request, visit.route(), err,
	)
	return true
}

// badRequestIn finds a BadRequest in err's chain, as a value or as a
// pointer. Both implement error, and errors.As matches only the exact
// type it is given, so a pointer would otherwise fall through to a 500.
func badRequestIn(err error) (BadRequest, bool) {
	var value BadRequest
	if errors.As(err, &value) {
		return value, true
	}
	var pointer *BadRequest
	if errors.As(err, &pointer) && pointer != nil {
		return *pointer, true
	}
	return BadRequest{}, false
}

// RefusedShowing is Refused for a page with a form: a field error or a
// conflict shows the form again, and every other refusal is Refused's.
// It panics with no form, because an action that has none would answer
// a person's typo with a 500, and that mistake should fail in the first
// test run rather than in production.
func (visit *Visit) RefusedShowing(err error, again Form) bool {
	if again == nil {
		panic("action: RefusedShowing needs a form; use Refused if " +
			"the page has none")
	}
	var invalid op.Invalid
	var conflict op.Conflict
	switch {
	case errors.As(err, &invalid):
		again.ShowAgain(visit, http.StatusUnprocessableEntity, invalid.Fields)
	case errors.As(err, &conflict):
		again.ShowAgain(visit, http.StatusConflict,
			form.Errors{WholeForm: conflict.Message})
	default:
		return visit.Refused(err)
	}
	return true
}

// route names the request for a log line: the method and the route
// pattern, so a line says "POST /things/{id}" and not "POST /things/7",
// which would make every row its own line to search for. A request that
// did not come through chi falls back to the mux pattern, then the path.
func (visit *Visit) route() string {
	pattern := visit.request.Pattern
	if routing := chi.RouteContext(visit.request.Context()); routing != nil {
		if chosen := routing.RoutePattern(); chosen != "" {
			pattern = chosen
		}
	}
	if pattern == "" {
		pattern = visit.request.URL.Path
	}
	return visit.request.Method + " " + pattern
}
