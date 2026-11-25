package ticketstest

import (
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/audit"

	"tickets/internal/tickets"
)

// A table nobody declared is one erasure and export would never reach.
func TestTablesAreDeclared(t *testing.T) {
	t.Parallel()
	audit.CheckSchema(t, tickets.BootSchema)
}

// main boots tickets.Migrations, so it must be the audited schema's
// statements in order, or the check reads tables the app never has.
func TestBootsTheAuditedSchema(t *testing.T) {
	t.Parallel()
	var want []string
	for _, migration := range tickets.BootSchema.All() {
		want = append(want, migration.SQL)
	}
	if !slices.Equal(tickets.Migrations, want) {
		t.Errorf("tickets.Migrations has %d statements, BootSchema %d",
			len(tickets.Migrations), len(want))
	}
}
