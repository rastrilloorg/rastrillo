package blog

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo/migrate"

	postsstore "blog/gen/store/posts"
)

// BootSchema is Migrations as a migrate.Set, so audit.CheckSchema checks
// the very statements main boots with. main hands Options.Migrations the
// plain list, which keeps no ledger, so these IDs are never recorded.
var BootSchema = statements(Migrations).Declare(postsstore.Declaration)

func statements(list []string) *migrate.Set {
	set := new(migrate.Set)
	for index, statement := range list {
		set.Add(migrate.Migration{
			ID: fmt.Sprintf("blog/%04d", index+1), SQL: statement,
		})
	}
	return set
}
