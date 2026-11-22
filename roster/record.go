package roster

import (
	"context"
	"database/sql"
	"errors"

	json "github.com/go-json-experiment/json"
)

// recorded is the receiver's own row. Contactless means the row holds only
// subjects and roles, as Config.RecordRolesOnly writes it, so a repair
// from it must keep the email and name the Store already has.
type recorded struct {
	Snapshot
	contactless bool
}

// roleOnly is a member as Config.RecordRolesOnly records it: enough to
// tell who to remove and what role each member has, and no contact.
type roleOnly struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
}

// recordedMembers is the members column. Unless rolesOnly it is the
// members as signed, the same bytes as before RecordRolesOnly existed.
func recordedMembers(members []Member, rolesOnly bool) ([]byte, error) {
	if !rolesOnly {
		return json.Marshal(members)
	}
	stripped := make([]roleOnly, 0, len(members))
	for _, member := range members {
		stripped = append(stripped, roleOnly{member.Subject, member.Role})
	}
	return json.Marshal(stripped)
}

// lastApplied is the recorded snapshot's sequence, state, members and
// origin, or sequence 0 and no members before the first.
func (receiver *Receiver) lastApplied(ctx context.Context) (recorded, error) {
	var sequence int64
	var state, members, origin string
	err := receiver.db.QueryRowContext(ctx,
		`SELECT sequence, state, members, origin
		 FROM roster_state WHERE id = 1`,
	).Scan(&sequence, &state, &members, &origin)
	if errors.Is(err, sql.ErrNoRows) {
		return recorded{Snapshot: Snapshot{
			State: Active, Members: []Member{},
		}}, nil
	}
	if err != nil {
		return recorded{}, err
	}
	applied := recorded{Snapshot: Snapshot{
		Sequence: sequence, State: State(state), Origin: origin,
	}}
	err = json.Unmarshal([]byte(members), &applied.Members)
	if err != nil {
		return recorded{}, err
	}
	applied.contactless, err = contactless(members)
	return applied, err
}

// contactless reports whether a recorded member has no email field at
// all, which only RecordRolesOnly writes: Member always writes one.
func contactless(members string) (bool, error) {
	var fields []struct {
		Email *string `json:"email"`
	}
	if err := json.Unmarshal([]byte(members), &fields); err != nil {
		return false, err
	}
	for _, field := range fields {
		if field.Email == nil {
			return true, nil
		}
	}
	return false, nil
}
