package signin

import (
	"embed"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the package's migration set, applied with migrate.Apply
// alongside auth's, totp's and passkey's. Its tables are the
// passphrases, the dismissed offers and the misses the limiter counts,
// kept in SQL so a restart forgets none of them.
var Schema = migrate.MustFromFS(migrationFS, "signin")
