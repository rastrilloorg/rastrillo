package migrate_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/blobs"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// legacySQL is each package's schema exactly as it shipped before the
// migrate conversion — main's `Migrations []string` as of v0.16.0,
// copied verbatim. A deployed database was built from these
// statements, so adopting one must produce zero DDL.
//
// These lists track main, not history: when main adds a statement to a
// package's Migrations (eventlog_writer, passkey_recovery_codes), it
// lands here too, or the test proves adoption against a schema nobody
// runs any more.
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
	  ON eventlog (stream, lamport, ts, writer, seq);`,
		`CREATE TABLE IF NOT EXISTS eventlog_writer (
	  id     INTEGER PRIMARY KEY CHECK (id = 1),
	  writer TEXT    NOT NULL
	);`},
	"passkey": {`CREATE TABLE IF NOT EXISTS passkey_credentials (
	  id         TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  public_key BLOB NOT NULL,
	  sign_count INTEGER NOT NULL DEFAULT 0,
	  created_at TEXT NOT NULL
	);`,
		`CREATE INDEX IF NOT EXISTS passkey_credentials_subject
	  ON passkey_credentials (subject);`,
		`CREATE TABLE IF NOT EXISTS passkey_challenges (
	  challenge  TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  purpose    TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
		`CREATE TABLE IF NOT EXISTS passkey_pending (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  method     TEXT NOT NULL DEFAULT '',
	  return_to  TEXT NOT NULL DEFAULT '',
	  expires_at TEXT NOT NULL
	);`,
		`CREATE TABLE IF NOT EXISTS passkey_recovery_codes (
	  code_hash  TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  created_at TEXT NOT NULL
	);`,
		`CREATE INDEX IF NOT EXISTS passkey_recovery_codes_subject
	  ON passkey_recovery_codes (subject);`},
}

// legacyAuthSQL is auth's pre-conversion Migrations. It gets its own
// variable rather than a legacySQL entry because auth is the one
// package whose Set cannot be adopted alone: auth/0002 backfills into
// the `sessions` table, so replaying auth.Schema by itself fails on a
// table no auth migration creates. A real deployed app composes
// migrate.Merge(sessions.Schema, auth.Schema), and that is what the
// test below adopts.
var legacyAuthSQL = []string{
	`CREATE TABLE IF NOT EXISTS auth_links (
	  hash       TEXT PRIMARY KEY,
	  address    TEXT NOT NULL,
	  purpose    TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS auth_sessions (
	  token_hash TEXT PRIMARY KEY,
	  address    TEXT NOT NULL,
	  method     TEXT NOT NULL,
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
}

func TestPackagesAdoptLegacyDatabases(t *testing.T) {
	sets := map[string]*migrate.Set{
		"sessions": sessions.Schema,
		"blobs":    blobs.Schema,
		"eventlog": eventlog.Schema,
		"passkey":  passkey.Schema,
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

// TestAuthAdoptsLegacyDatabase is design §7's per-package adoption
// test for auth, the one package it could not be written for in the
// obvious shape. auth/0002 backfills into the `sessions` table, so
// replaying auth.Schema alone fails on a table no auth migration
// creates — the set only makes sense composed, exactly as a real app
// composes it, and that composition is what a deployed database has
// to adopt.
//
// Adoption stamps 0002 without running it, which is correct here: the
// database already has the sessions core, so its rows were moved by
// the old Migrations []string long before this boot. The pre-
// sessions-core database, where that is not true, is covered in
// auth/auth_test.go — Apply refuses it outright.
func TestAuthAdoptsLegacyDatabase(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "auth.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, s := range append(append([]string{}, legacySQL["sessions"]...), legacyAuthSQL...) {
		if err := d.G.Exec(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	r, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, auth.Schema))
	if err != nil {
		t.Fatalf("a database built from the shipped Migrations must adopt cleanly: %v", err)
	}
	if !r.Adopted || len(r.Applied) != 0 {
		t.Fatalf("Result = %+v, want adopted with zero DDL applied", r)
	}
}

// storeMigrations stands in for a manifest resource's generated
// gen/store/<name>/migrations.go: a raw []string the app wraps into a
// *migrate.Set by hand, the way examples/notes does.
var storeMigrations = []string{
	`CREATE TABLE IF NOT EXISTS invoices (
	  id       INTEGER PRIMARY KEY AUTOINCREMENT,
	  user_id  INTEGER NOT NULL,
	  cents    INTEGER NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_invoices_user_id ON invoices (user_id);`,
}

// appNote is the app's own GORM model — the third of the three ways a
// table gets into a real database.
type appNote struct {
	ID     int64
	UserID int64 `gorm:"index:idx_app_notes_user_id"`
	Title  string
}

func (appNote) TableName() string { return "app_notes" }

// TestWholeAppAdoptsItsComposedBootSchema is the shape of every real
// deployed database, and nothing else covered it.
//
// The per-package tests above each adopt one Set against one package's
// legacy SQL. A real app is all three mechanisms at once: GORM
// AutoMigrate for its own models, a framework subsystem's raw
// Migrations, and a manifest resource's generated store — built
// separately, by three different code paths, into one file with no
// ledger. Boot then hands migrate.Apply a single composed BootSchema
// and every one of those tables has to be recognised, or the app
// refuses to boot on a fleet it was supposed to adopt silently.
//
// The assertion that matters is Applied being empty: adoption must
// stamp, never run. A single CREATE TABLE escaping here would fail
// against a live database on the first wake after deploy.
func TestWholeAppAdoptsItsComposedBootSchema(t *testing.T) {
	ctx := context.Background()

	// The app's own migration is what `rastrillo migration generate`
	// writes for these models on a fresh app — generated rather than
	// transcribed, so it cannot drift from what AutoMigrate builds
	// below.
	changes, err := migrate.Generate(ctx, nil, []any{&appNote{}})
	if err != nil {
		t.Fatal(err)
	}
	var appSQL strings.Builder
	for _, c := range changes {
		appSQL.WriteString(strings.TrimSpace(c.SQL))
		appSQL.WriteString("\n")
	}

	storeSet := &migrate.Set{}
	storeSet.Add(migrate.Migration{ID: "0001_init", SQL: strings.Join(storeMigrations, "\n")})
	appSet := &migrate.Set{}
	appSet.Add(migrate.Migration{ID: "0001_init", SQL: appSQL.String()})

	// migrate.Set's namespace is unexported, so the namespaces come
	// from FromFS in real code; here Merge is fed sets built through
	// the exported Add, which leaves them unqualified. Qualify them by
	// hand so the composed IDs look like an app's real boot set and
	// the two 0001_inits do not collide.
	boot := migrate.Merge(sessions.Schema, eventlog.Schema,
		namespaced("store", storeSet), namespaced("app", appSet))

	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Built the pre-branch way, by all three mechanisms.
	for _, s := range append(append([]string{}, legacySQL["sessions"]...), // the subsystems'
		legacySQL["eventlog"]...) { // raw Migrations
		if err := d.G.Exec(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range storeMigrations { // the generated store's raw Migrations
		if err := d.G.Exec(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.AutoMigrate(&appNote{}); err != nil { // the app's own models
		t.Fatal(err)
	}
	// A row in each, so a rebuild or a re-created table cannot pass
	// unnoticed.
	if err := d.G.Exec(`INSERT INTO app_notes (id, user_id, title) VALUES (1, 7, 'hi')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec(`INSERT INTO invoices (id, user_id, cents) VALUES (1, 7, 500)`).Error; err != nil {
		t.Fatal(err)
	}

	r, err := migrate.Apply(ctx, d, boot)
	if err != nil {
		t.Fatalf("a real deployed database must adopt its composed BootSchema: %v", err)
	}
	if !r.Adopted {
		t.Fatalf("Result = %+v, want Adopted", r)
	}
	if len(r.Applied) != 0 {
		t.Fatalf("Applied = %v, want none — adoption runs zero DDL against a live database", r.Applied)
	}
	if r.Skipped != len(boot.All()) {
		t.Fatalf("Skipped = %d, want all %d stamped", r.Skipped, len(boot.All()))
	}
	for _, table := range []string{"app_notes", "invoices"} {
		var n int64
		d.G.Raw("SELECT count(*) FROM " + table).Scan(&n)
		if n != 1 {
			t.Fatalf("%s has %d rows, want the pre-existing row intact", table, n)
		}
	}
	// And the second boot is a plain no-op, not a second adoption.
	r2, err := migrate.Apply(ctx, d, boot)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Adopted || len(r2.Applied) != 0 {
		t.Fatalf("second boot = %+v, want a quiet skip of everything", r2)
	}
}

// namespaced qualifies a hand-built Set's IDs the way FromFS's
// namespace would. Merge is the only exported thing that qualifies,
// and it qualifies from the Set's own unexported namespace field.
func namespaced(ns string, s *migrate.Set) *migrate.Set {
	out := &migrate.Set{}
	for _, m := range s.All() {
		m.ID = ns + "/" + m.ID
		out.Add(m)
	}
	return out
}
