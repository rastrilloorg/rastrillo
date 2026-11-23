package granttoken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
)

// Grants says whether subject still holds grant. A site passes its
// *roster.Receiver; the root passes its own grant records.
type Grants interface {
	GrantActive(ctx context.Context, subject, grant string) (bool, error)
}

// DoorConfig is what a Door needs. Verifier and Grants are required.
type DoorConfig struct {
	Verifier *Verifier
	Grants   Grants
	Now      func() time.Time // nil means time.Now
}

// ErrRevoked is a token whose grant its holder no longer has: revoked,
// ended, or not yet in the roster the site holds.
var ErrRevoked = errors.New("granttoken: grant not active")

// Door is the api.Verifier and api.Rechecker for grant tokens. A token
// is only as good as its grant, so the grant is asked about on every
// check, the recheck as each answer goes out included.
type Door struct {
	verifier *Verifier
	grants   Grants
	now      func() time.Time
}

// NewDoor returns ErrConfig when Verifier or Grants is nil.
func NewDoor(config DoorConfig) (*Door, error) {
	if config.Verifier == nil || config.Grants == nil {
		return nil, ErrConfig
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Door{
		verifier: config.Verifier, grants: config.Grants, now: now,
	}, nil
}

// Verify implements api.Verifier.
func (door *Door) Verify(
	ctx context.Context, request *http.Request,
) (rastrillo.Actor, error) {
	return door.check(ctx, request)
}

// Recheck implements api.Rechecker. It makes every check Verify makes,
// so a token that expired or lost its grant mid-answer is cut off.
func (door *Door) Recheck(
	ctx context.Context, request *http.Request, _ rastrillo.Actor,
) (rastrillo.Actor, error) {
	return door.check(ctx, request)
}

// check is every check Verify and Recheck share, and it changes
// nothing. It is one function so a rule added to one cannot be missed
// by the other. The grant is asked about last, so a forged or foreign
// token never costs a roster read.
func (door *Door) check(
	ctx context.Context, request *http.Request,
) (rastrillo.Actor, error) {
	token, err := bearer(request)
	if err != nil {
		return rastrillo.Actor{}, err
	}
	claims, err := door.verifier.Verify(token, door.now())
	if err != nil {
		return rastrillo.Actor{}, fmt.Errorf(
			"%w: %w", api.ErrUnauthorized, err)
	}
	active, err := door.grants.GrantActive(
		ctx, claims.Subject, claims.Grant)
	if err != nil {
		// Never wrap err: it may carry submitted text, or a class
		// of its own (401, 409, 422) that would override the 503.
		return rastrillo.Actor{}, fmt.Errorf(
			"%w: granttoken: grant lookup failed", api.ErrUnavailable)
	}
	if !active {
		return rastrillo.Actor{}, fmt.Errorf(
			"%w: %w", api.ErrUnauthorized, ErrRevoked)
	}
	return actorOf(claims), nil
}

// actorOf names the grant as the credential, so the gate counts the
// caller as a token holder: human-only operations stay refused. No
// Session is set, so an agent's write still needs its own header.
func actorOf(claims Claims) rastrillo.Actor {
	// Claims are validated by now, so Access is Read or Write.
	access := rastrillo.Read
	if claims.Access == Write {
		access = rastrillo.Write
	}
	return rastrillo.Actor{
		Subject: claims.Subject, Agent: claims.Agent,
		Credential: claims.Grant, Access: access,
		Grant: &rastrillo.Grant{
			ID: claims.Grant, Issuer: claims.Issuer,
			IdentityIssuer: claims.Issuer, Audience: claims.Audience,
		},
	}
}

// bearer reads the token from the one Authorization header. Never a
// cookie, so a page cannot make a browser spend it, and never the
// query string, so it stays out of access logs.
func bearer(request *http.Request) (string, error) {
	headers := request.Header.Values("Authorization")
	if len(headers) != 1 {
		return "", fmt.Errorf(
			"%w: want one Authorization header", api.ErrUnauthorized)
	}
	scheme, token, _ := strings.Cut(headers[0], " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", fmt.Errorf(
			"%w: not a Bearer token", api.ErrUnauthorized)
	}
	return strings.TrimLeft(token, " "), nil
}
