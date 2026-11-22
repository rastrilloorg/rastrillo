// Package notes declares one named operation, so the app declares
// operations and routes --check holds every route to the marking rule.
package notes

import (
	"context"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// Input is what creating a note takes.
type Input struct {
	Text string `json:"text"`
}

// Note is what creating one returns.
type Note struct {
	ID int64 `json:"id"`
}

// Create is the operation the page at POST /notes serves.
var Create = op.Define("notes.create", op.Spec{
	Description: "Create a note.", Access: rastrillo.Write,
}, create)

func create(
	ctx context.Context, rastrilloCtx *rastrillo.Ctx, input Input,
) (Note, error) {
	return Note{}, nil
}
