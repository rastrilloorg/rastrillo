package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo/privacy"
)

// Identify resolves a subject to every identifier for that person, so
// export and erasure can find their rows. How it finds addresses
// follows the mode, tested in the order subjectFor chooses it: the
// address itself by default; the user record in
// records mode; Config.AddressesFor when SubjectFor is set, or
// privacy.ErrUnidentifiable without it. Blind links and blind records
// keep only hashes, so Addresses is empty there and matching works on
// subjects alone. An empty subject is refused, since it would match
// every row whose subject column is empty. Addresses keep the local
// part's case and lower-case the domain, as signin.SplitAddress does.
func (auth *Auth) Identify(
	ctx context.Context, subject string,
) (privacy.Identity, error) {
	if strings.TrimSpace(subject) == "" {
		return privacy.Identity{}, errors.New(
			"rastrillo/auth: identify: empty subject")
	}
	switch {
	case auth.cfg.BlindLinks != nil:
		return privacy.NewIdentity(subject, nil), nil
	case auth.users != nil && auth.users.blind:
		return privacy.NewIdentity(subject, nil), nil
	case auth.users != nil:
		user, err := auth.User(ctx, subject)
		if err != nil {
			return privacy.Identity{}, fmt.Errorf(
				"rastrillo/auth: identify: %w", err)
		}
		return privacy.NewIdentity(subject, []string{user.Email}), nil
	case auth.cfg.SubjectFor != nil:
		if auth.cfg.AddressesFor == nil {
			return privacy.Identity{}, privacy.ErrUnidentifiable
		}
		addresses, err := auth.cfg.AddressesFor(subject)
		if err != nil {
			return privacy.Identity{}, fmt.Errorf(
				"rastrillo/auth: identify: %w", err)
		}
		return privacy.NewIdentity(subject, addresses), nil
	}
	return privacy.NewIdentity(subject, []string{subject}), nil
}
