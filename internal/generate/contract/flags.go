package contract

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/opsgen"
)

// current is the contract for rs with the replaced bodies read from the
// app root, which is what Write and Check must both compare with: if
// either left them out, generate --check would call a file generate
// just wrote out of date.
func current(appRoot string, rs []rastrillo.Resource) ([]byte, error) {
	hand, err := opsgen.HandFiles(appRoot, rs)
	if err != nil {
		return nil, err
	}
	return Contract(rs, hand)
}

// destructiveNotes names each operation that was not marked destructive
// and now is. It is a note and not a refusal because a client keeps
// working; it only gets a warning it did not get before.
func destructiveNotes(was, now []byte) []string {
	before, err := parse(was)
	if err != nil {
		return nil
	}
	after, err := parse(now)
	if err != nil {
		return nil
	}
	marked := map[string]bool{}
	for _, operation := range before.Operations {
		marked[operation.ID] = operation.Destructive
	}
	var notes []string
	for _, operation := range after.Operations {
		wasMarked, existed := marked[operation.ID]
		if operation.Destructive && existed && !wasMarked {
			notes = append(notes, fmt.Sprintf(
				"  %s: now marked destructive, so an MCP client may ask first",
				operation.ID))
		}
	}
	return notes
}
