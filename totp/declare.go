package totp

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:   "totp_secrets",
		Key:    []string{"subject"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
