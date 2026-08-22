// Package migrate applies an app's schema exactly once per migration
// and records what it did, replacing the two mechanisms — GORM
// AutoMigrate for models, raw Migrations []string for framework
// subsystems — that a Rastrillo app used to run side by side at boot.
//
// A Set is an ordered, namespaced list. Merge's argument order is
// apply order, which is how a package that must run after another
// (auth after sessions) states that requirement at the call site
// instead of in a comment.
//
// Migrations are forward-only. Production rollback for a CARLOS app
// is a point-in-time restore of the SQLite file the activator
// replicates, not a Down function, so none is offered.
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Migration is one step. Exactly one of SQL or Fn is set: SQL is the
// default and the only thing `rastrillo migration generate` emits; Fn
// is the escape hatch for a change SQL cannot express.
//
// A Go migration must not reference the app's live model structs — a
// model changes over time and would silently change the meaning of a
// migration that already ran. Copy the struct into the migration.
type Migration struct {
	ID  string
	SQL string
	Fn  func(*gorm.DB) error
}

// Set is an ordered list of migrations sharing one namespace.
type Set struct {
	namespace  string
	migrations []Migration
}

// fileName is the only shape FromFS accepts: four digits, underscore,
// a lowercase name. schema.sql is excluded by not matching, which is
// how the accumulated snapshot lives beside the migrations it
// summarises without ever being applied.
var fileName = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)

func FromFS(fsys fs.FS, namespace string) (*Set, error) {
	s := &Set{namespace: namespace}
	entries, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	seen := map[string]bool{}
	for _, e := range entries {
		base := path.Base(e)
		if base == "schema.sql" {
			continue
		}
		if !fileName.MatchString(base) {
			return nil, fmt.Errorf("migrate: %s/%s: name must be NNNN_name.sql", namespace, base)
		}
		body, err := fs.ReadFile(fsys, e)
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(base, ".sql")
		if seen[id] {
			return nil, fmt.Errorf("migrate: %s/%s: duplicate migration id", namespace, id)
		}
		seen[id] = true
		s.migrations = append(s.migrations, Migration{ID: id, SQL: string(body)})
	}
	return s, nil
}

func MustFromFS(fsys fs.FS, namespace string) *Set {
	s, err := FromFS(fsys, namespace)
	if err != nil {
		panic(err)
	}
	return s
}

// Add appends a Go migration. It shares the ID space with the SQL
// files, so ordering between the two is just declaration order.
func (s *Set) Add(m Migration) *Set {
	s.migrations = append(s.migrations, m)
	return s
}

// Merge concatenates sets in argument order, which is apply order.
func Merge(sets ...*Set) *Set {
	out := &Set{}
	for _, s := range sets {
		if s == nil {
			continue
		}
		for _, m := range s.migrations {
			if s.namespace != "" {
				m.ID = s.namespace + "/" + m.ID
			}
			out.migrations = append(out.migrations, m)
		}
	}
	// Already qualified; All must not qualify a second time.
	out.namespace = ""
	return out
}

// All returns the migrations in apply order, with namespace-qualified
// IDs.
func (s *Set) All() []Migration {
	out := make([]Migration, 0, len(s.migrations))
	for _, m := range s.migrations {
		if s.namespace != "" {
			m.ID = s.namespace + "/" + m.ID
		}
		out = append(out, m)
	}
	return out
}

// Checksum detects a migration edited after it was applied. Whitespace
// is normalised so reformatting does not raise a false alarm; the cost
// is that a change confined to whitespace inside a string literal goes
// unnoticed, which is an acceptable trade for a change-detector.
func Checksum(sql string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(sql), " ")))
	return hex.EncodeToString(sum[:])
}
