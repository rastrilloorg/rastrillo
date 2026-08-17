package rastrillo

import (
	"context"
	"net/http"
)

// This file is the agents system's vocabulary (design doc §8): actions
// opt in as tools, the generator collects the registry, and the tools
// package renders schemas and dispatches model-proposed calls back
// through the same mux — "a tool call and an HTTP POST reach the
// identical Handle function."

// Access is what a tool may do — the registry's read/write split that
// drives §8's consent gating.
type Access int

const (
	// ToolRead observes and never changes state.
	ToolRead Access = iota
	// ToolWrite changes state, and therefore requires a Confirm
	// sentence and an explicitly confirmed call — the same consent the
	// confirm page asks a human for.
	ToolWrite
)

func (a Access) String() string {
	if a == ToolWrite {
		return "write"
	}
	return "read"
}

// Tool marks an action as agent-callable. Declared in the action file
// itself, next to Handle:
//
//	var Tool = rastrillo.Tool{
//	    Description: "Cancel one order and release its tickets.",
//	    Access:      rastrillo.ToolWrite,
//	    Args:        map[string]string{"id": "the order id"},
//	    Confirm:     "Cancel order {id}? Its tickets go back on sale.",
//	}
//
// The generator reads it statically (the same AST pass that rewrites
// package clauses) and emits the registry into gen/tools.go. A
// ToolWrite with an empty Confirm fails `generate --check` — the
// buildable half of §13's agent-gate check.
type Tool struct {
	Description string
	Access      Access
	// Args maps argument names to human/model-readable descriptions.
	// At dispatch, an argument matching a {param} in the route fills
	// that path segment; the rest travel as form values (POST and
	// friends) or query parameters (GET/HEAD).
	Args map[string]string
	// Confirm is the consent sentence shown before a write executes —
	// "{arg}" placeholders interpolate the call's arguments.
	Confirm string
}

// ToolDef is one registry entry: the tool plus the route it reaches.
type ToolDef struct {
	ID     string // route-derived, stable: e.g. "orders_id_cancel_post"
	Method string
	Path   string // the mux pattern's path half, e.g. "/orders/{id}/cancel"
	Tool
}

type actorCtxKey struct{}

// WithActor stamps who is making this request onto its context — the
// tools dispatcher uses it so an agent call is attributed end to end.
// The generated router copies it onto Ctx.Actor after the app's
// ctxFactory runs, so an app factory that doesn't set Actor still gets
// honest attribution.
func WithActor(r *http.Request, a Actor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorCtxKey{}, a))
}

// ActorFromContext reports the actor WithActor stamped, if any.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorCtxKey{}).(Actor)
	return a, ok
}
