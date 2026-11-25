package signin

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name: "signin_failures",
		Key:  []string{"rowid"},
		Kind: migrate.Personal,
		NotAudited: "rate-limit buckets; failed sign-ins are " +
			"security events",
		Erase: migrate.EraseKeep,
		KeepReason: "rate-limit windows of minutes, pruned as " +
			"they expire",
	}).Declare(migrate.Data{
		Name:   "signin_offers",
		Key:    []string{"subject"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	}).Declare(migrate.Data{
		Name:   "signin_passphrases",
		Key:    []string{"subject"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
