package notestest

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/audit"

	"notes/internal/notes"
)

// A table nobody declared is one erasure and export would never reach.
func TestTablesAreDeclared(t *testing.T) {
	t.Parallel()
	audit.CheckSchema(t, notes.BootSchema)
}
