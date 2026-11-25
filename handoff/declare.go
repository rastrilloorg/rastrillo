package handoff

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:       "handoff_used",
		Key:        []string{"jti"},
		Kind:       migrate.System,
		NotAudited: "replay guard of spent one-time IDs",
	})
}
