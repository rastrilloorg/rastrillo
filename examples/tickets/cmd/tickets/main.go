// Command tickets is the manifest example: the whole admin surface
// comes from manifest/ticket_types.toml — this file only wires the
// generated pieces together. Compare examples/blog, which hand-writes
// the same screens; the two are the framework's before and after.
package main

import (
	"database/sql"
	"log"
	"log/slog"
	"net/http"

	"github.com/carlosframework/rastrillo"

	"tickets/gen"
	genmanifest "tickets/gen/manifest"
)

func main() {
	err := rastrillo.Run(rastrillo.Options{
		DBPath:     "tickets.db",
		Migrations: genmanifest.Migrations(),
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			return gen.Router(func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Logger: slog.Default(), Actor: rastrillo.Actor{Human: true}}
			}), nil
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
