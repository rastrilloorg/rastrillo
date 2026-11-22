package totp

import (
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// beforeClaim runs between PendingOrBegin's new secret and its write,
// so a test can land another caller's write there. Nil but in tests.
var beforeClaim func()

// PendingOrBegin returns subject's unconfirmed enrolment, starting one
// only when there is none, in one write that never replaces a row: two
// tabs that open a set-up page at once both get whichever secret was
// stored first, so a code from either confirms it. A confirmed
// authenticator is ErrEnrolled. Begin, by contrast, replaces a pending
// secret, for a page that means to start over.
func (handlers *Handlers) PendingOrBegin(
	subject, account string,
) (Enrolment, error) {
	secret, err := newSecret()
	if err != nil {
		return Enrolment{}, err
	}
	sealed, err := crypto.SealSym(handlers.cfg.Key, secret)
	if err != nil {
		return Enrolment{}, err
	}
	if beforeClaim != nil {
		beforeClaim()
	}
	if _, err := handlers.cfg.DB.Exec(
		`INSERT INTO totp_secrets
		 (subject, secret, confirmed_at, last_counter, created_at)
		 VALUES (?, ?, '', 0, ?) ON CONFLICT (subject) DO NOTHING`,
		subject, sealed, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return Enrolment{}, err
	}
	var stored []byte
	var confirmedAt string
	if err := handlers.cfg.DB.QueryRow(
		`SELECT secret, confirmed_at FROM totp_secrets WHERE subject = ?`,
		subject).Scan(&stored, &confirmedAt); err != nil {
		return Enrolment{}, err
	}
	if confirmedAt != "" {
		return Enrolment{}, ErrEnrolled
	}
	opened, err := crypto.OpenSym(handlers.cfg.Key, stored)
	if err != nil {
		return Enrolment{}, err
	}
	return handlers.enrolment(opened, account)
}
