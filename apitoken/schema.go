package apitoken

import (
	"embed"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the api_tokens table, applied with migrate.Apply alongside
// the app's own sets, as sessions.Schema is. It depends on no other
// package's table, so it can sit anywhere in a migrate.Merge.
//
// The table never holds a secret: only its SHA-256. A copy of the
// database (a backup, a replica, a laptop) therefore hands nobody a
// working token.
var Schema = migrate.MustFromFS(migrationFS, "apitoken")
