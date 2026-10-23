// Package op holds what operations and both of their doors share: the
// refusals an operation returns, the presence-carrying input types a
// request decodes into, and the descriptor that names an operation
// independently of transport.
//
// Refusals are value types, not pointers or sentinels, so a page door and
// an API door can both match them with errors.As and map each one their
// own way (a re-rendered form with field messages; a 422 with a fields
// map). A sentinel could not carry the fields. A pointer would let a nil
// *NotFound travel as a non-nil error that errors.As still matches, and
// the door would then read fields off nil; a value has no nil to carry.
//
// Opt exists because presence must survive decoding. A required money
// field may legitimately be 0, and a PATCH must change only the fields
// the client sent, so "absent" and "sent as zero" have to stay distinct
// after the JSON is gone. A plain int64 cannot tell them apart; Opt can.
package op

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// Invalid refuses an input. Fields is keyed by manifest field name
// ("Title"), never the wire name: the API door converts to snake_case
// and the page door uses the form's own names, so neither has to agree
// with the other here.
type Invalid struct{ Fields map[string]string }

// NotFound refuses a request for a row that is not there, or that is
// not the actor's. The two are deliberately the same answer, so a
// caller cannot probe for rows it may not see.
type NotFound struct{}

// Forbidden refuses an actor. Reason is for logs only and never reaches
// the response, so a policy can say exactly why without telling the
// caller what to try next.
type Forbidden struct{ Reason string }

// Conflict refuses a write that lost a race, such as a mergeable update
// crossing a currency change. Message is safe to show.
type Conflict struct{ Message string }

// Error names the fields, sorted so a log line is stable, but not the
// messages: those are for the response, and a log that repeats them
// says nothing the field names do not.
func (e Invalid) Error() string {
	if len(e.Fields) == 0 {
		return "invalid"
	}
	names := make([]string, 0, len(e.Fields))
	for name := range e.Fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return "invalid: " + strings.Join(names, ", ")
}

func (NotFound) Error() string    { return "not found" }
func (e Forbidden) Error() string { return "forbidden: " + e.Reason }
func (e Conflict) Error() string  { return "conflict: " + e.Message }

// Money is an amount on the wire: minor units and an ISO 4217 code.
// Currency may be empty on input; the operation fills the manifest's
// default on create and keeps the row's on update. It is validated
// there, with currency.Normalize, not here.
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Descriptor names one operation, independent of transport. It is what
// the MCP spec will list as tools and what the skill spec will describe,
// so it carries everything a caller needs to invoke the operation
// without the generated Go signature.
type Descriptor struct {
	rastrillo.Operation
	// Exposed is whether the resource is on the API. Exposure decides
	// only whether doors exist; what a caller may do is Check's business.
	Exposed     bool
	Description string
	// Confirm is the consent sentence an agent shows before a write.
	Confirm string
	// Input and Result are JSON Schema. Both are required: api.New
	// refuses a route whose descriptor leaves either empty, so a
	// hand-written operation must describe itself too.
	Input  json.RawMessage
	Result json.RawMessage
	// Invoke decodes input with the API door's rules and calls the
	// operation. It is the transport-independent call.
	Invoke func(ctx context.Context, rc *rastrillo.Ctx, input json.RawMessage) (any, error)
}
