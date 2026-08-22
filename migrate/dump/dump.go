// Package dump is the bridge between the rastrillo binary and an
// app's model structs.
//
// rastrillo cannot import an app's models: it is a separate binary,
// and parsing models.go to reimplement GORM's struct-tag-to-DDL
// mapping would duplicate GORM badly and drift from it. So
// `rastrillo migration generate` writes a tiny program into the app
// module that imports the app package and this one, runs it with
// `go run`, and reads the JSON this package prints.
package dump

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/carlosframework/rastrillo/migrate"
)

// Payload is what the loader program prints and the CLI parses.
type Payload struct {
	Changes []migrate.Change `json:"changes"`
	Schema  string           `json:"schema"`
	Diff    []string         `json:"diff"`

	// Boot is the app's composed boot set (migrate.Merge of every
	// framework subsystem plus the app's own Schema, in apply order)
	// — app.BootSchema.All(), not app.Schema.All(). It carries each
	// migration's SQL, not just its ID: baseline's --through hands
	// this straight to migrate.Stamp, which writes a checksum computed
	// from the SQL, and a ledger row with the wrong checksum makes the
	// next real boot refuse with "applied with different SQL".
	//
	// This is the only way the CLI can learn the order an app composes
	// its subsystems in — rastrillo could import sessions/auth/etc.
	// directly (same module), but not the app's own Merge call, and
	// guessing that order risks stamping the wrong prefix, which is
	// the exact harm baseline's --through validation exists to
	// prevent. So the app computes it and this payload carries it
	// across the go run boundary instead.
	Boot []migrate.Migration `json:"boot"`
}

// Compute always generates and checks against ms, the app's *own*
// migration set — never boot. Replaying the composed boot set here
// instead would make the structural drop-pass in migrate.Generate
// compare framework tables (sessions, auth_links, blobs, ...) against
// a Models list that knows nothing about them, and it would propose
// dropping every one. boot is passed straight through to the payload,
// untouched, for baseline's use — it never reaches Generate or
// SchemaSQL.
func Compute(ms []migrate.Migration, boot []migrate.Migration, models []any) (Payload, error) {
	ctx := context.Background()
	var p Payload
	var err error
	if p.Changes, err = migrate.Generate(ctx, ms, models); err != nil {
		return p, err
	}
	if p.Schema, err = migrate.SchemaSQL(ctx, ms); err != nil {
		return p, err
	}
	for _, c := range p.Changes {
		p.Diff = append(p.Diff, c.SQL)
	}
	p.Boot = boot
	return p, nil
}

// Main is the whole body of the generated loader program.
func Main(ms []migrate.Migration, boot []migrate.Migration, models []any) {
	p, err := Compute(ms, boot, models)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
