package contract

import (
	"encoding/json"
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

// destructiveNotes names each operation whose file recorded it as not
// destructive and which now is. It is a note and not a refusal because
// a client keeps working; it only gets a warning it did not get
// before. A file from before the flags existed recorded nothing, so
// nothing could have relied on it and nothing is said.
func destructiveNotes(was, now []byte) []string {
	var before struct {
		Operations []struct {
			ID          string `json:"id"`
			Destructive *bool  `json:"destructive"`
		} `json:"operations"`
	}
	after, err := parse(now)
	if err != nil || json.Unmarshal(was, &before) != nil {
		return nil
	}
	recordedSafe := map[string]bool{}
	for _, operation := range before.Operations {
		recordedSafe[operation.ID] = operation.Destructive != nil &&
			!*operation.Destructive
	}
	var notes []string
	for _, operation := range after.Operations {
		if operation.Destructive && recordedSafe[operation.ID] {
			notes = append(notes, fmt.Sprintf(
				"  %s: now marked destructive, so an MCP client may ask first",
				operation.ID))
		}
	}
	return notes
}
