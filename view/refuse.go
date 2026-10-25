package view

import (
	"errors"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// Refuse answers an operation's error the way a page door must: a row
// that is not there, or is not the caller's, is the 404 page, and a
// refused actor is the 403 page, so a page says what the API door says.
// Anything else, including an op.Invalid or op.Conflict from a door
// with no form to show them on, is Fail's 500, logged with what.
func Refuse(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, what string, err error) {
	var forbidden op.Forbidden
	switch {
	case errors.As(err, new(op.NotFound)):
		NotFound(ctx, w, r)
	case errors.As(err, &forbidden):
		if ctx.Logger != nil && forbidden.Reason != "" {
			ctx.Logger.Info(what, "refused", forbidden.Reason)
		}
		Forbidden(ctx, w, r)
	default:
		Fail(ctx, w, r, what, err)
	}
}
