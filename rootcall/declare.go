package rootcall

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:       "rootcall_used",
		Key:        []string{"jti"},
		Kind:       migrate.System,
		NotAudited: "replay guard of spent one-time IDs",
	})
}
