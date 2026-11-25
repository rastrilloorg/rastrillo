package passkey

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:       "passkey_challenges",
		Key:        []string{"challenge"},
		Kind:       migrate.Personal,
		Owner:      migrate.Subject("subject"),
		NotAudited: "ceremony state that lives for minutes",
		Erase:      migrate.EraseRow,
	}).Declare(migrate.Data{
		Name:   "passkey_credentials",
		Key:    []string{"id"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	}).Declare(migrate.Data{
		Name:       "passkey_pending",
		Key:        []string{"token_hash"},
		Kind:       migrate.Personal,
		Owner:      migrate.Subject("subject"),
		NotAudited: "ceremony state that lives for minutes",
		Erase:      migrate.EraseRow,
	}).Declare(migrate.Data{
		Name:   "passkey_recovery_codes",
		Key:    []string{"code_hash"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
