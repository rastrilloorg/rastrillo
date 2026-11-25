package secondfactor

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	// 0002_adopt_passkey drops these two tables from passkey.
	set.Retire("passkey_pending", "passkey_recovery_codes")
	return set.Declare(migrate.Data{
		Name:       "secondfactor_pending",
		Key:        []string{"token_hash"},
		Kind:       migrate.Personal,
		Owner:      migrate.Subject("subject"),
		NotAudited: "ceremony state that lives for minutes",
		Erase:      migrate.EraseRow,
	}).Declare(migrate.Data{
		Name:   "secondfactor_recovery_codes",
		Key:    []string{"code_hash"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("subject"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
