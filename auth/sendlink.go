package auth

import (
	"context"

	"github.com/keymaildev/signin"
)

// Budget refusals, as signin defines them, so an app that reaches
// SendLink can tell them apart without importing signin itself.
var (
	// ErrRateLimited means the address, or the caller's ipKey, is over
	// its budget for the window.
	ErrRateLimited = signin.ErrRateLimited
	// ErrBadAddress means the address is not syntactically an address.
	ErrBadAddress = signin.ErrBadAddress
)

// SendLink mails address its sign-in link, from code rather than from
// the form. It is for a caller the app already trusts to have decided
// the address should get one — a request relayed from another service
// the app holds a key for, say — and that cannot post Begin's form: a
// POST from a server carries no browser's same-origin evidence and no
// proof, and Begin refuses it, rightly.
//
// What Begin does for the link itself happens here too, so a link sent
// this way is the same link: the same single-use row with the same
// life, the same letter through Config.Mailer, and the same budgets.
// ipKey stands where a browser's client address would in the per-IP
// budget, so a caller passes a fixed name for the channel it is —
// "relay", not an address it was told, which it could be lied to about
// — and that whole channel then shares one budget of 20 per 15 minutes,
// a bound on what the service it trusts can make it send. The address
// budget (5 per 15 minutes) is the one Begin spends, shared with the
// form. An empty ipKey spends no IP budget at all, as signin has it,
// which is a thing to pass on purpose and not by default.
//
// What does not happen: no proof is checked (the caller is trusted,
// which is the point of calling this rather than posting), the address
// is not classified for keymail (a link is what was asked for, as the
// form's force field asks), and no attempt cookie is written (there is
// no response to write one to). A refusal comes back as ErrRateLimited
// or ErrBadAddress — for a caller to log, never to answer with: a
// caller that must not be an oracle answers the same whatever this
// returns.
func (a *Auth) SendLink(ctx context.Context, address, ipKey string) error {
	_, err := a.flow.Begin(ctx, address, ipKey, signin.MethodMagicLink)
	return err
}
