package migrate_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/carlosframework/rastrillo/blobs"
	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/eventlog"
	"github.com/carlosframework/rastrillo/migrate"
	"github.com/carlosframework/rastrillo/passkey"
	"github.com/carlosframework/rastrillo/sessions"
)

// legacySQL is each package's schema exactly as it shipped before the
// migrate conversion. A deployed database was built from these
// statements, so adopting one must produce zero DDL.
var legacySQL = map[string][]string{
	"sessions": {`CREATE TABLE IF NOT EXISTS sessions (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  method     TEXT NOT NULL DEFAULT '',
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`},
	"blobs": {`CREATE TABLE IF NOT EXISTS blobs (
	  hash         TEXT    PRIMARY KEY,
	  content_type TEXT    NOT NULL,
	  size         INTEGER NOT NULL,
	  data         BLOB    NOT NULL
	);`},
	"eventlog": {`CREATE TABLE IF NOT EXISTS eventlog (
	  stream  TEXT    NOT NULL,
	  writer  TEXT    NOT NULL,
	  seq     INTEGER NOT NULL,
	  lamport INTEGER NOT NULL,
	  ts      TEXT    NOT NULL,
	  actor   TEXT    NOT NULL,
	  kind    TEXT    NOT NULL,
	  payload TEXT    NOT NULL,
	  PRIMARY KEY (stream, writer, seq)
	);`,
		`CREATE INDEX IF NOT EXISTS eventlog_merge_order
	  ON eventlog (stream, lamport, ts, writer, seq);`},
}

func TestPackagesAdoptLegacyDatabases(t *testing.T) {
	sets := map[string]*migrate.Set{
		"sessions": sessions.Schema,
		"blobs":    blobs.Schema,
		"eventlog": eventlog.Schema,
	}
	for name, stmts := range legacySQL {
		t.Run(name, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), name+".db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			for _, s := range stmts {
				if err := d.G.Exec(s).Error; err != nil {
					t.Fatal(err)
				}
			}
			r, err := migrate.Apply(context.Background(), d, sets[name])
			if err != nil {
				t.Fatalf("a database built from the shipped Migrations must adopt cleanly: %v", err)
			}
			if !r.Adopted || len(r.Applied) != 0 {
				t.Fatalf("Result = %+v, want adopted with zero DDL applied", r)
			}
		})
	}
}

func TestPackagesApplyToEmptyDatabase(t *testing.T) {
	for name, s := range map[string]*migrate.Set{
		"sessions": sessions.Schema,
		"blobs":    blobs.Schema,
		"eventlog": eventlog.Schema,
		"passkey":  passkey.Schema,
	} {
		t.Run(name, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), name+".db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if _, err := migrate.Apply(context.Background(), d, s); err != nil {
				t.Fatal(err)
			}
			// Idempotent second boot.
			if _, err := migrate.Apply(context.Background(), d, s); err != nil {
				t.Fatalf("second boot: %v", err)
			}
		})
	}
}
