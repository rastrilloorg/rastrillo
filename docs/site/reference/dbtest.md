# 🤖 dbtest

`amadan.net/rastrillo/rastrillo/dbtest`

A fresh, migrated SQLite database for every test, without migrating it
every time. The first test that asks builds one migrated template, and
every test gets its own copy of that file.

A copy is much cheaper than a migration. Migrating means SQLite parsing
and running every statement in your schema, and every test pays it
again. In Tito Go a fresh database took 207ms and a copy took 2.5ms.

## Template

```go
type Template struct { /* unexported */ }

func FromSet(s *migrate.Set) *Template
func New(build func(path string) error) *Template
func (tp *Template) Remove()
```

Declare one template per test package, beside the tests that use it.
`FromSet` builds it with `migrate.Apply`, so a migration you add is in
the template the next time the tests run.

```go
var schema = dbtest.FromSet(migrate.Merge(sessions.Schema, app.Schema))

func TestMain(m *testing.M) {
	code := m.Run()
	schema.Remove()
	os.Exit(code)
}
```

Use `New` when your migrations are not a `migrate.Set`. Your `build`
function must leave a migrated database at `path` and close every
connection to it before it returns. If a connection is still open, the
template is refused: its newest pages are still in the `-wal` file, and
a copy would miss them.

Call `Remove` from `TestMain` after `m.Run`. The template outlives every
test, so no test can clean it up.

## Getting a copy

```go
func (tp *Template) Open(t testing.TB) *sql.DB
func (tp *Template) Path(t testing.TB) string
func (tp *Template) CopyTo(t testing.TB, path string)
```

`Open` copies the template and opens the copy the way `db.Open` opens
its writer: foreign keys on, one connection, so `migrate.Pool` accepts
it. It is closed when the test ends.

If your app opens its database itself, use `Path`: it puts the copy in
the test's temporary directory and returns its path, as in
`db.Open(schema.Path(t), nil)`. `CopyTo` puts the copy at a path you
choose.

If the template cannot be built, the test that asked fails with the
build's error. Every later test fails the same way; the build is not
tried again.

## What stays out of the template

Keep the template to schema. Create test data in the test itself: a row
made when the template was built carries that time, not the time the
test ran.

If a migration creates a value that must be different in every database,
such as a random key, every copy gets the template's. Set a new one
after you copy.
