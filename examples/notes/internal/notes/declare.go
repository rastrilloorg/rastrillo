package notes

import "amadan.net/rastrillo/rastrillo/migrate"

// declare says whose rows each table in migrations/ holds. notes are
// owned by the numeric user ID, but an export by the session subject:
// models.go's Export says why the two differ.
func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:  "users",
		Key:   []string{"id"},
		Kind:  migrate.Personal,
		Owner: migrate.UserID("id"),
		Fields: []migrate.Field{{
			Name: "email", Holds: migrate.HoldsAddress,
			Erase: migrate.EraseRow,
		}},
		Erase:  migrate.EraseRow,
		Export: true,
	}).Declare(migrate.Data{
		Name:   "notes",
		Key:    []string{"id"},
		Kind:   migrate.Personal,
		Owner:  migrate.UserID("user_id"),
		Erase:  migrate.EraseRow,
		Export: true,
	}).Declare(migrate.Data{
		Name:   "exports",
		Key:    []string{"id"},
		Kind:   migrate.Personal,
		Owner:  migrate.Subject("owner"),
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
