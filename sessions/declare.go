package sessions

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		// Measurement in phase 2 may change whether this is audited.
		Name:   "sessions",
		Key:    []string{"token_hash"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
