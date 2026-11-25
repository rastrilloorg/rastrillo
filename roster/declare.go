package roster

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:  "roster_state",
		Key:   []string{"id"},
		Kind:  migrate.Personal,
		Erase: migrate.EraseKeep,
		KeepReason: "the root's member sync rewrites it; erasure " +
			"through the root is the follow-up spec's",
	})
}
