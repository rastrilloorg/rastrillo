package chat

import (
	"embed"

	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/signin"
	"amadan.net/rastrillo/rastrillo/totp"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is this app's own migrations, which migration generate and
// check diff against Models. Never edit one that has shipped.
var Schema = migrate.MustFromFS(migrationFS, "chat").
	Declare(migrate.Data{Name: "notes", Key: []string{"id"},
		Kind: migrate.Personal, Owner: migrate.Subject("owner"),
		Erase: migrate.EraseRow, Export: true}).
	Declare(migrate.Data{Name: "rooms", Key: []string{"id"},
		Kind: migrate.Shared}).
	Declare(migrate.Data{Name: "messages", Key: []string{"id"},
		Kind: migrate.Personal, Owner: migrate.Subject("sender"),
		Erase: migrate.EraseRow, Export: true})

// BootSchema is what boot applies. A subsystem's schema goes here, in the
// order it applies, and never in Schema: check would drop its tables.
var BootSchema = migrate.Merge(sessions.Schema, auth.Schema,
	pow.Schema, passkey.Schema, totp.Schema, signin.Schema,
	apitoken.Schema, Schema)
