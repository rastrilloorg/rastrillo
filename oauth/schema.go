package oauth

import (
	"embed"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema holds the oauth tables, applied with migrate.Apply alongside
// the app's own sets. It depends on no other package's table.
//
// Codes and refresh tokens are stored as their SHA-256 only, so a copy
// of the database hands nobody a working credential.
var Schema = declare(migrate.MustFromFS(migrationFS, "oauth"))
