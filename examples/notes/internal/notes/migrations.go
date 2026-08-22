package notes

import (
	"embed"
	"fmt"

	"github.com/carlosframework/rastrillo/migrate"
	"github.com/carlosframework/rastrillo/sessions"

	bookmarksstore "notes/gen/store/bookmarks"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is this app's own migrations, in order — nothing else. It is
// what `rastrillo migration generate` and `rastrillo migration check`
// read and write: both work by replaying a set into an in-memory
// database and diffing the result against Models, so Schema must
// never carry a framework subsystem's migrations too — mixed in, the
// diff would see tables (sessions, bookmarks, ...) that Models knows
// nothing about and want to drop every one of them.
//
// Add to it with `rastrillo migration generate` after changing a model
// in models.go, or `rastrillo migration new <name>` to write one by
// hand. Migrations apply at boot and are never re-run or reversed:
// never edit one that has shipped, add a new one instead.
var Schema = migrate.MustFromFS(migrationFS, "notes")

// genSchema wraps bookmarksstore.Migrations — the []string a manifest
// resource still emits (internal/generate/store.go feeds
// Options.Migrations, the legacy pre-GORM path) — into a *migrate.Set,
// by hand, at the app level. That keeps App() down to the one apply
// mechanism this whole subsystem exists to offer, rather than running
// migrate.Apply beside a second, raw d.G.Exec loop.
//
// The cost of doing this is real and unsolved: bookmarksstore.Migrations
// is regenerated verbatim from manifest/bookmarks.toml every time
// `rastrillo generate` runs, but migrate.Apply treats an applied
// migration's SQL as immutable once its ledger row exists. Reshape the
// manifest (add a field, rename one) after this app has already booted
// once, and its regenerated SQL no longer matches the checksum the
// ledger recorded under the same "bookmarks_gen/0001_init" ID — the
// next boot refuses rather than silently reapplying it. SKILL.md §2's
// "generated store's ledger trap" note has the recovery (`rastrillo
// migration baseline`); nothing here papers over it.
func genSchemaSet() *migrate.Set {
	s := new(migrate.Set)
	for i, stmt := range bookmarksstore.Migrations {
		s.Add(migrate.Migration{ID: fmt.Sprintf("bookmarks_gen/%04d_init", i+1), SQL: stmt})
	}
	return s
}

var genSchema = genSchemaSet()

// BootSchema is everything applied at boot, in order: sessions' schema
// first (nothing here depends on it, but it is the shared core every
// multi-user app composes ahead of its own tables), then the
// generated bookmarks store, then this app's own Schema. migrate.Merge's
// argument order is apply order.
var BootSchema = migrate.Merge(sessions.Schema, genSchema, Schema)
