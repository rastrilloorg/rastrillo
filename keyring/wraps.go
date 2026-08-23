package keyring

import "errors"

// Wrap is one hold on the seed: a WrapSeed output plus the identifiers
// an app needs to manage it. The keyring stores nothing — the app
// persists wraps wherever it likes; the guard below travels with the
// type.
//
// Two invariants from messenger travel as documentation, not code,
// because they are policy about which wraps coexist and the app's flow
// owns them: a durable enrolment replaces a member handoff, and
// handoffs are Kind "member".
type Wrap struct {
	// ID names the wrap; AddWrap dedupes on it.
	ID string
	// Kind says what holds the wrap — "passkey" for a credential,
	// "member" for a handoff to another member.
	Kind string
	// Label is display text ("Paul's phone"); never load-bearing.
	Label string
	// UID names the member the wrap belongs to. Messenger's
	// enroll/give flows need it; dropping it would leave the type
	// kass-shaped only.
	UID string
	// CredentialID is the WebAuthn credential the wrap is stored
	// under when Kind is "passkey" — the transport contract's lookup
	// key (see the package doc).
	CredentialID []byte
	// Wrapped is the WrapSeed output: iv(12) ‖ ciphertext.
	Wrapped []byte
}

var (
	// ErrLastWrap is RemoveWrap refusing to strand the seed: a seed
	// with zero wraps is a seed no one can ever open again
	// (messenger's vault.revoke guard — the one lifecycle rule worth
	// enforcing in code, because getting it wrong loses data forever).
	// An event-sourced consumer folding untrusted input must treat it
	// as a no-op, not a crash — errors are for interactive callers.
	ErrLastWrap = errors.New("rastrillo/keyring: cannot remove the last wrap")

	// ErrUnknownWrap is RemoveWrap finding no wrap with the given ID.
	ErrUnknownWrap = errors.New("rastrillo/keyring: unknown wrap")
)

// AddWrap returns wraps with w added, deduplicating by ID (messenger's
// addWrap): an existing wrap with w.ID is replaced in place — a
// re-enrolment is an update, not a sibling — otherwise w is appended.
// Pure: the input slice is never mutated, so a caller replaying events
// can hold both generations.
func AddWrap(wraps []Wrap, w Wrap) []Wrap {
	out := make([]Wrap, len(wraps), len(wraps)+1)
	copy(out, wraps)
	for i := range out {
		if out[i].ID == w.ID {
			out[i] = w
			return out
		}
	}
	return append(out, w)
}

// RemoveWrap returns wraps without the wrap named id. It returns
// ErrUnknownWrap when no wrap has that id — a miss removes nothing, so
// it is a miss even on a one-element list — and ErrLastWrap when the
// named wrap is the only one left. Pure, like AddWrap.
func RemoveWrap(wraps []Wrap, id string) ([]Wrap, error) {
	idx := -1
	for i := range wraps {
		if wraps[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, ErrUnknownWrap
	}
	if len(wraps) == 1 {
		return nil, ErrLastWrap
	}
	out := make([]Wrap, 0, len(wraps)-1)
	out = append(out, wraps[:idx]...)
	out = append(out, wraps[idx+1:]...)
	return out, nil
}
