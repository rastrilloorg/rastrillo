package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// SetSuspended suspends or resumes uid. Suspending also moves the epoch,
// as EndSessions does, so cookies already out stop refreshing; resuming
// leaves it, because the old cookies must stay dead. An unknown uid
// answers sessions.ErrNoUser and nothing is written.
func (auth *Auth) SetSuspended(
	ctx context.Context, uid string, suspended bool,
) error {
	if auth.users == nil {
		return errNoRecords
	}
	if !uidRE.MatchString(uid) {
		return sessions.ErrNoUser
	}
	_, _, err := objrec.Mutate(ctx, auth.users.store, userKey(uid),
		func(current User, exists bool) (User, error) {
			if !exists {
				return User{}, sessions.ErrNoUser
			}
			if suspended && !current.Suspended {
				current.Epoch++
			}
			current.Suspended = suspended
			// Mutate writes back the whole record it read, so blind mode
			// strips the address here for the reason EndSessions does.
			if auth.users.blind {
				current.Email = ""
			}
			return current, nil
		})
	return err
}

// normalAddress is the form sign-in keys a person by.
func normalAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// UserByEmail finds the user for an address and never creates one. With
// no user it answers an error wrapping objrec.ErrNotFound.
func (auth *Auth) UserByEmail(
	ctx context.Context, address string,
) (User, error) {
	if auth.users == nil {
		return User{}, errNoRecords
	}
	hash := emailHash(auth.users.pepper, normalAddress(address))
	claim, _, err := objrec.GetJSON[emailClaim](
		ctx, auth.users.store, claimKey(hash))
	if err != nil {
		return User{}, fmt.Errorf("rastrillo/auth: user for address: %w", err)
	}
	return auth.User(ctx, claim.UID)
}

// EnsureUser returns the user for an address, creating it as the first
// sign-in would. An invitation needs the person to exist before they
// have signed in.
func (auth *Auth) EnsureUser(
	ctx context.Context, address string,
) (User, error) {
	if auth.users == nil {
		return User{}, errNoRecords
	}
	return auth.users.ensure(ctx, normalAddress(address))
}

// Users pages through every user in key order, limit at a time. An
// empty cursor starts at the beginning, and the cursor returned is empty
// after the last page. Keys that are not users are skipped without
// counting against limit. The cursor is the store's, opaque, so each
// list asks for only the users still owed: a page is never overshot.
// A limit of 0 or less is an error: objrec reads 0 as no limit, which a
// pager never wants.
func (auth *Auth) Users(
	ctx context.Context, cursor string, limit int,
) ([]User, string, error) {
	if auth.users == nil {
		return nil, "", errNoRecords
	}
	if limit <= 0 {
		return nil, "", errors.New(
			"rastrillo/auth: Users needs a limit above 0")
	}
	var found []User
	for len(found) < limit {
		keys, next, err := auth.users.store.List(
			ctx, "users/", cursor, limit-len(found))
		if err != nil {
			return nil, "", err
		}
		for _, key := range keys {
			name := strings.TrimPrefix(key, "users/")
			uid, ok := strings.CutSuffix(name, ".json")
			if !ok || !uidRE.MatchString(uid) {
				continue
			}
			user, err := auth.users.read(ctx, uid)
			if errors.Is(err, objrec.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, "", err
			}
			found = append(found, user)
		}
		cursor = next
		if cursor == "" {
			break
		}
	}
	return found, cursor, nil
}

// SessionCurrent is SessionFrom plus one read of the user. A signed
// cookie is otherwise trusted for its whole life without a read, so
// anything that mints a longer credential or acts with authority calls
// this: a reset or a suspension then holds at once.
func (auth *Auth) SessionCurrent(request *http.Request) (Identity, error) {
	if auth.users == nil {
		return Identity{}, errNoRecords
	}
	identity, ok := auth.SessionFrom(request)
	if !ok {
		return Identity{}, sessions.ErrNoUser
	}
	cookieEpoch, ok := auth.sessions.CookieEpoch(request)
	if !ok {
		return Identity{}, sessions.ErrNoUser
	}
	// The subject is the uid in records mode. epoch refuses a suspended
	// user as it does at refresh.
	current, err := auth.users.epoch(request.Context(), identity.Address)
	if err != nil {
		return Identity{}, err
	}
	if current != cookieEpoch {
		return Identity{}, sessions.ErrNoUser
	}
	return identity, nil
}
