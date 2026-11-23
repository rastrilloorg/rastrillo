package granttoken

import (
	"context"
	"net/http"
	"strings"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
)

// Fallback is what *apitoken.Store already is.
type Fallback interface {
	api.Verifier
	api.Rechecker
	Redact(text string) string
}

// Either is the one verifier a site hands its API tree.
type Either struct {
	door   *Door
	others Fallback
}

// Or returns a verifier for grant tokens and others' tokens.
func (door *Door) Or(others Fallback) *Either {
	return &Either{door: door, others: others}
}

// Verify implements api.Verifier.
func (either *Either) Verify(
	ctx context.Context, request *http.Request,
) (rastrillo.Actor, error) {
	if isGrantToken(request) {
		return either.door.Verify(ctx, request)
	}
	return either.others.Verify(ctx, request)
}

// Recheck implements api.Rechecker.
func (either *Either) Recheck(
	ctx context.Context, request *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error) {
	if isGrantToken(request) {
		return either.door.Recheck(ctx, request, was)
	}
	return either.others.Recheck(ctx, request, was)
}

// Redact hides both kinds of token.
func (either *Either) Redact(text string) string {
	return either.others.Redact(Redact(text))
}

// isGrantToken is the only test that picks the door. A token with the
// prefix is never tried as a personal token, so a grant the door
// refuses cannot be accepted by another store. Anything the test does
// not recognise, such as two headers, goes to others, which refuses a
// malformed request itself.
func isGrantToken(request *http.Request) bool {
	token, err := bearer(request)
	return err == nil && strings.HasPrefix(token, Prefix)
}
