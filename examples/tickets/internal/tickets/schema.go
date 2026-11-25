package tickets

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/migrate"

	ticket_typesstore "tickets/gen/store/ticket_types"
)

// BootSchema is this app's whole schema, with each table's
// declaration, for audit.CheckSchema. eventlog.Schema is what
// announcementsstore.Migrations is read out of, and carries eventlog's
// declarations. A new resource's statements go here and nowhere else.
var BootSchema = migrate.Merge(ticketTypes(), eventlog.Schema)

// Migrations is BootSchema's statements for Options.Migrations, so main
// and the tests apply the schema the check reads.
var Migrations = statements(BootSchema)

func statements(set *migrate.Set) []string {
	var list []string
	for _, migration := range set.All() {
		list = append(list, migration.SQL)
	}
	return list
}

// ticketTypes' IDs are never recorded: Options.Migrations keeps no
// ledger.
func ticketTypes() *migrate.Set {
	set := new(migrate.Set)
	for index, statement := range ticket_typesstore.Migrations {
		set.Add(migrate.Migration{
			ID:  fmt.Sprintf("ticket_types_gen/%04d", index+1),
			SQL: statement,
		})
	}
	return set.Declare(ticket_typesstore.Declaration)
}
