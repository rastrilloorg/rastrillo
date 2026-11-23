package roster

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	json "github.com/go-json-experiment/json"
)

// GrantActive reports whether the latest applied snapshot lists grant
// under subject on an active instance.
func (receiver *Receiver) GrantActive(
	ctx context.Context, subject, grant string,
) (bool, error) {
	var state, members string
	err := receiver.db.QueryRowContext(ctx,
		`SELECT state, members FROM roster_state WHERE id = 1`,
	).Scan(&state, &members)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if State(state) == Suspended {
		return false, nil
	}
	var listed []struct {
		Subject string   `json:"sub"`
		Grants  []string `json:"grants"`
	}
	if err := json.Unmarshal([]byte(members), &listed); err != nil {
		return false, err
	}
	for _, member := range listed {
		if member.Subject == subject && slices.Contains(member.Grants, grant) {
			return true, nil
		}
	}
	return false, nil
}
