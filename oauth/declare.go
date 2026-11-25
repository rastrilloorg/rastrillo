package oauth

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name: "oauth_clients",
		Key:  []string{"id"},
		Kind: migrate.Shared,
	}).Declare(migrate.Data{
		Name:  "oauth_codes",
		Key:   []string{"hash"},
		Kind:  migrate.Personal,
		Owner: migrate.Via("connection", "oauth_connections"),
		NotAudited: "single-use codes that live for minutes; the " +
			"connection is audited",
		Erase: migrate.EraseRow,
	}).Declare(migrate.Data{
		Name:     "oauth_connections",
		Key:      []string{"id"},
		Kind:     migrate.Personal,
		Owner:    migrate.Subject("subject"),
		Recorded: []string{"access"},
		Erase:    migrate.EraseRow,
		Export:   true,
	}).Declare(migrate.Data{
		Name:  "oauth_pending",
		Key:   []string{"id"},
		Kind:  migrate.Personal,
		Owner: migrate.Subject("subject"),
		NotAudited: "consent-screen state that lives for minutes; " +
			"the grant is audited on oauth_connections",
		Erase: migrate.EraseRow,
	}).Declare(migrate.Data{
		Name:  "oauth_refresh",
		Key:   []string{"hash"},
		Kind:  migrate.Personal,
		Owner: migrate.Via("connection", "oauth_connections"),
		NotAudited: "rotated on every refresh; the connection is " +
			"audited",
		Erase:  migrate.EraseRow,
		Export: true,
	})
}
