// Package blog is the example app's shared code: SQLite storage, the
// template tree, and the view models every action renders.
//
// It exists because actions/ cannot hold shared code. The generator
// copies only <name>.<VERB>.go files into gen/actions/ and skips any file
// whose base name starts with "_", so a sibling helpers.go never reaches
// the generated tree. Ordinary packages travel fine: imports pass through
// the generator's package-clause rewrite unmodified.
package blog

import (
	"database/sql"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo"

	postsstore "blog/gen/store/posts"
)

// Migrations is the app's whole schema, generated store first: the
// manifest system's own posts.toml (manifest/posts.toml) now owns the
// table's shape — postsstore.Migrations creates it (id, title, body,
// created_at, updated_at) — and this package owns exactly one additive
// column beyond that: published, which no manifest field declares.
// Order matters (design doc's additive-migration convention): the
// generated CREATE TABLE IF NOT EXISTS must run before this ALTER, so a
// fresh database gets the table before anything tries to add a column
// to it. main.go hands the whole slice to rastrillo via
// Options.Migrations; Open applies it for tests. Re-running the ALTER
// against a database that already has the column is safe —
// rastrillo.OpenDB swallows sqlite's "duplicate column" error for
// exactly this additive-ALTER convention.
var Migrations = append(append([]string(nil), postsstore.Migrations...), publishedColumn)

const publishedColumn = `ALTER TABLE posts ADD COLUMN published INTEGER NOT NULL DEFAULT 0;`

// Post is one row of posts. Timestamps are stored as RFC3339 strings in
// UTC and parsed on scan, so formatting happens once, in Go, and no
// template ever formats a date.
type Post struct {
	ID        int64
	Title     string
	Body      string
	Published bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Open opens the app's SQLite database with rastrillo's corrected
// opener and applies the migration. The serving path doesn't use this —
// main.go lets Serve open the database and hand the *sql.DB back via
// Options.Router — but the tests still want a one-call migrated handle.
func Open(path string) (*sql.DB, error) {
	return rastrillo.OpenDB(path, Migrations)
}

const selectColumns = `id, title, body, published, created_at, updated_at`

// scanPost reads one row in selectColumns order, parsing both timestamps.
func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var (
		p         Post
		published int
		created   string
		updated   string
	)
	if err := row.Scan(&p.ID, &p.Title, &p.Body, &published, &created, &updated); err != nil {
		return Post{}, err
	}
	p.Published = published != 0
	var err error
	if p.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return Post{}, err
	}
	if p.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
		return Post{}, err
	}
	return p, nil
}

// Get returns one post by id. A missing id is sql.ErrNoRows, which every
// action turns into a 404.
func Get(db *sql.DB, id int64) (Post, error) {
	return scanPost(db.QueryRow(`SELECT `+selectColumns+` FROM posts WHERE id = ?`, id))
}

// ListPublished returns published posts, newest first.
func ListPublished(db *sql.DB, offset, limit int) ([]Post, error) {
	rows, err := db.Query(`SELECT `+selectColumns+` FROM posts WHERE published = 1
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// CountPublished counts published posts.
func CountPublished(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM posts WHERE published = 1`).Scan(&n)
	return n, err
}

// listWhere builds the WHERE clause List and Count share. A status
// outside draft/published means no status condition — the handler
// normalizes, and the store stays forgiving about raw values.
func listWhere(q, status string) (string, []any) {
	var conds []string
	var args []any
	if q != "" {
		conds = append(conds, `title LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(q))
	}
	switch status {
	case "draft":
		conds = append(conds, "published = 0")
	case "published":
		conds = append(conds, "published = 1")
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// List returns posts newest first, filtered by a title search when q is
// non-empty and by status ("draft" or "published") when set.
func List(db *sql.DB, q, status string, offset, limit int) ([]Post, error) {
	where, args := listWhere(q, status)
	args = append(args, limit, offset)
	rows, err := db.Query(`SELECT `+selectColumns+` FROM posts`+where+`
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// Count counts the posts List would page through.
func Count(db *sql.DB, q, status string) (int, error) {
	where, args := listWhere(q, status)
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM posts`+where, args...).Scan(&n)
	return n, err
}

// SetPublished flips a post's published flag and moves updated_at.
func SetPublished(db *sql.DB, id int64, v bool) error {
	n := 0
	if v {
		n = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE posts SET published = ?, updated_at = ? WHERE id = ?`, n, now, id)
	return err
}

// Delete removes a post.
func Delete(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM posts WHERE id = ?`, id)
	return err
}

// collect drains a *sql.Rows into posts, always closing it.
func collect(rows *sql.Rows) ([]Post, error) {
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// likePattern wraps a user's search string for a LIKE with ESCAPE '\'.
// The escaping is not about injection — the value is a bound parameter —
// but about meaning: someone searching for "100%" wants a literal match,
// not a wildcard.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}
