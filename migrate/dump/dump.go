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
}

func Compute(ms []migrate.Migration, models []any) (Payload, error) {
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
	return p, nil
}

// Main is the whole body of the generated loader program.
func Main(ms []migrate.Migration, models []any) {
	p, err := Compute(ms, models)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
