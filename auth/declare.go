package auth

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:  "auth_links",
		Key:   []string{"hash"},
		Kind:  migrate.Personal,
		Owner: migrate.Address("address"),
		NotAudited: "one-time sign-in links; issuing and using one " +
			"are security events",
		Erase:  migrate.EraseRow,
		Export: true,
	}).Declare(migrate.Data{
		// Measurement in phase 2 may change whether this is audited.
		Name:   "auth_sessions",
		Key:    []string{"token_hash"},
		Kind:   migrate.Personal,
		Owner:  migrate.Address("address"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
