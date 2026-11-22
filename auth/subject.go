package auth

import (
	"context"
	"net/http"

	"amadan.net/rastrillo/rastrillo/sessions"
)

func (auth *Auth) subjectFor(
	ctx context.Context, address string,
) (string, error) {
	if auth.cfg.BlindLinks != nil {
		return auth.blindSubject(ctx, address)
	}
	if auth.users != nil {
		user, err := auth.users.ensure(ctx, address)
		if err != nil {
			return "", err
		}
		if user.Suspended {
			return "", sessions.ErrNoUser
		}
		return user.ID, nil
	}
	if auth.cfg.SubjectFor != nil {
		return auth.cfg.SubjectFor(address)
	}
	return address, nil
}

// Sessions is the session store this Auth mints into and reads from.
// Give it to signin.Config.Sessions, so the ways signin adds share
// auth's cookie, lifetime and table instead of a second store's.
func (auth *Auth) Sessions() *sessions.Sessions { return auth.sessions }

func (auth *Auth) sessionFrom(request *http.Request) (sessions.Session, bool) {
	session, ok := auth.sessions.From(request)
	// Refuse old subjects without rewriting plaintext into WAL frames.
	if auth.cfg.BlindLinks != nil && !opaque(session.Subject) {
		return sessions.Session{}, false
	}
	return session, ok
}

func (auth *Auth) refuseAdmission(response http.ResponseWriter,
	request *http.Request, identity Identity,
) {
	if auth.cfg.Refused != nil {
		auth.cfg.Refused(response, request, identity)
		return
	}
	http.Error(response, "This address is verified but not admitted here.",
		http.StatusForbidden)
}
