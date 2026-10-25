package op

import (
	"context"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// Check is the one authorization point both doors pass through: every
// operation, generated or hand-written, calls it first (spec section 4).
// Ownership is not authorization. A page can sit behind a role check or
// a fresh sign-in that the API never sees, so the question is asked
// here, where the page door and the API door both meet it, rather than
// in middleware only one of them runs.
//
// The order is fixed. The read-only and no-subject checks run before
// the app's policy so that a policy which allows too much cannot hand a
// read-only token a write, or an anonymous caller a scoped row: those
// two are properties of the credential and the operation, not choices
// an app makes. The policy's error comes back unchanged, so a refusal
// keeps its type for errors.As and anything else is a 500.
func Check(ctx context.Context, rc *rastrillo.Ctx, o rastrillo.Operation) error {
	a := rc.Actor
	// A session carries Write and the app's zero actor carries no
	// Access at all, so only a read-only token is refused here.
	if o.Access == rastrillo.Write && a.Access == rastrillo.Read {
		return Forbidden{Reason: "read-only credential"}
	}
	if o.Scoped && a.Subject == "" {
		return Forbidden{Reason: "no subject"}
	}
	if rc.Authorize != nil {
		return rc.Authorize(ctx, a, o)
	}
	return DefaultPolicy(ctx, a, o)
}

// DefaultPolicy is Check's answer when Ctx.Authorize is nil, exported so
// an app can name it in its own policy and add rules around it.
//
// A scoped operation is allowed: Check has already required a person,
// and the owner filter keeps them to their own rows. An unscoped
// operation is allowed for every caller without a credential, which is
// how pages behave today. It is refused for a token, because unscoped
// tables are often admin tables guarded by page middleware the API
// cannot see; letting a token through by default would open them to
// anyone holding one. An app that wants tokens there says so in its
// Authorize.
func DefaultPolicy(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error {
	if !o.Scoped && a.IsToken() {
		return Forbidden{Reason: "unscoped operation " + o.ID + " has no Authorize policy for tokens"}
	}
	return nil
}
