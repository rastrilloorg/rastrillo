package apitoken

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:     "api_tokens",
		Key:      []string{"id"},
		Kind:     migrate.Personal,
		Owner:    migrate.Subject("subject"),
		Recorded: []string{"access"},
		Erase:    migrate.EraseRow,
		Export:   true,
	})
}
