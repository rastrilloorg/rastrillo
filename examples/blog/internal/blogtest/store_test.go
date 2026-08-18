// Package blogtest holds the example's tests: the store's own, and
// route-level tests over the real generated mux. Plain stdlib testing and
// httptest — no assertion library, mirroring the rastrillo repo's own
// style.
//
// It is a directory of _test.go files and nothing else — a valid Go
// package that go test runs and go vet passes over silently. It lives
// outside internal/blog because it imports blog/gen, which imports the
// generated actions, which import blog/internal/blog: an internal test of
// that package would be an import cycle. Keeping the tests here also
// means they see the app exactly as main.go does, through gen.
//
// These tests seed rows through the generated postsstore (via seed, in
// harness_test.go) rather than blog.Create/blog.Update, which no longer
// exist: the manifest adoption (task 10) moved create/update for posts
// onto the generated store, and internal/blog/store.go shrank to what
// nothing generated covers — Get, List/Count, ListPublished/
// CountPublished, SetPublished, Delete, and the paging helpers.
package blogtest

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	postsstore "blog/gen/store/posts"
	"blog/internal/blog"
)

// newDB opens a fresh file-backed database per test. A file, not
// :memory:, because SetMaxOpenConns(1) plus WAL is the configuration
// being exercised.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := blog.Open(filepath.Join(t.TempDir(), "blog.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blog.db")
	first, err := blog.Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	seed(t, first, "Kept", "Body.", false)
	first.Close()

	second, err := blog.Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()
	n, err := blog.Count(second, "", "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1: re-running the migration lost data", n)
	}
}

func TestCreateStartsADraftWithTimestamps(t *testing.T) {
	db := newDB(t)
	id := seed(t, db, "First post", "Body.", false)

	post, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if post.Title != "First post" || post.Body != "Body." {
		t.Errorf("post = %q/%q", post.Title, post.Body)
	}
	if post.Published {
		t.Errorf("a new post is published; it should start as a draft")
	}
	if post.CreatedAt.IsZero() || post.UpdatedAt.IsZero() {
		t.Errorf("timestamps did not round-trip: %v / %v", post.CreatedAt, post.UpdatedAt)
	}
}

func TestGetAMissingPostIsErrNoRows(t *testing.T) {
	db := newDB(t)
	if _, err := blog.Get(db, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestListOrdersNewestFirstWithAnIDTiebreak(t *testing.T) {
	db := newDB(t)
	// Two posts created in the same second are ordinary in a test, which
	// is exactly why the ordering carries an id tiebreak.
	older := seed(t, db, "Older", "Body.", false)
	newer := seed(t, db, "Newer", "Body.", false)

	posts, err := blog.List(db, "", "", 0, blog.PageSize)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 2 || posts[0].ID != newer || posts[1].ID != older {
		t.Fatalf("order = %v, want [%d %d]", ids(posts), newer, older)
	}
}

func TestListPublishedExcludesDrafts(t *testing.T) {
	db := newDB(t)
	live := seed(t, db, "Live", "Body.", true)
	seed(t, db, "Draft", "Body.", false)

	posts, err := blog.ListPublished(db, 0, blog.PageSize)
	if err != nil {
		t.Fatalf("list published: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != live {
		t.Fatalf("published = %v, want [%d]", ids(posts), live)
	}
	n, err := blog.CountPublished(db)
	if err != nil {
		t.Fatalf("count published: %v", err)
	}
	if n != 1 {
		t.Errorf("count published = %d, want 1", n)
	}
}

func TestListPagesWithOffsetAndLimit(t *testing.T) {
	db := newDB(t)
	for i := 0; i < 3; i++ {
		seed(t, db, "Post", "Body.", false)
	}
	posts, err := blog.List(db, "", "", 2, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 1 {
		t.Errorf("got %d posts at offset 2, want 1", len(posts))
	}
}

func TestSearchMatchesTitlesCaseInsensitively(t *testing.T) {
	db := newDB(t)
	seed(t, db, "Going to production", "Body.", false)
	seed(t, db, "Unrelated", "mentions going in the body", false)

	posts, err := blog.List(db, "GOING", "", 0, blog.PageSize)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 1 || posts[0].Title != "Going to production" {
		t.Fatalf("search matched %v, want just the title match", titles(posts))
	}
	n, err := blog.Count(db, "GOING", "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

// Searching for "100%" finds a literal per cent sign, not everything:
// the wildcards in a user's string are escaped before the LIKE.
func TestSearchEscapesLikeWildcards(t *testing.T) {
	db := newDB(t)
	seed(t, db, "Shipping 100% of the time", "Body.", false)
	seed(t, db, "Shipping sometimes", "Body.", false)

	posts, err := blog.List(db, "100%", "", 0, blog.PageSize)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("search matched %v, want just the literal match", titles(posts))
	}
	if underscores, err := blog.Count(db, "_", ""); err != nil {
		t.Fatalf("count: %v", err)
	} else if underscores != 0 {
		t.Errorf("_ matched %d titles as a wildcard, want 0", underscores)
	}
}

func TestUpdateAndSetPublishedAndDelete(t *testing.T) {
	db := newDB(t)
	id := seed(t, db, "Before", "Old body.", false)

	// The generated store's UpdatePostBasics is what the edit-basics
	// action now calls too (see gen/actions/admin/posts/id/edit_basics_post).
	now := time.Now().UTC().Format(time.RFC3339)
	if err := postsstore.New(db).UpdatePostBasics(context.Background(), postsstore.UpdatePostBasicsParams{
		Title: "After", Body: "New body.", Now: now, ID: id,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	post, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if post.Title != "After" || post.Body != "New body." {
		t.Errorf("post = %q/%q, want After/New body.", post.Title, post.Body)
	}

	if err := blog.SetPublished(db, id, true); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if post, err = blog.Get(db, id); err != nil {
		t.Fatalf("get: %v", err)
	} else if !post.Published {
		t.Errorf("post is still a draft")
	}
	if err := blog.SetPublished(db, id, false); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if post, err = blog.Get(db, id); err != nil {
		t.Fatalf("get: %v", err)
	} else if post.Published {
		t.Errorf("post is still published")
	}

	if err := blog.Delete(db, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := blog.Get(db, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v after delete, want sql.ErrNoRows", err)
	}
}

func TestListFiltersByStatus(t *testing.T) {
	db := newDB(t)
	draftID := seed(t, db, "Draft one", "b", false)
	pubID := seed(t, db, "Published one", "b", true)

	drafts, err := blog.List(db, "", "draft", 0, blog.PageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != draftID {
		t.Errorf("draft filter: got %v", drafts)
	}

	pubs, err := blog.List(db, "", "published", 0, blog.PageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubs) != 1 || pubs[0].ID != pubID {
		t.Errorf("published filter: got %v", pubs)
	}

	// Search and status compose with AND.
	both, err := blog.List(db, "one", "draft", 0, blog.PageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 1 || both[0].ID != draftID {
		t.Errorf("search+status: got %v", both)
	}

	n, err := blog.Count(db, "", "draft")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Count draft = %d, want 1", n)
	}
}

func ids(posts []blog.Post) []int64 {
	out := make([]int64, len(posts))
	for i, p := range posts {
		out[i] = p.ID
	}
	return out
}

func titles(posts []blog.Post) []string {
	out := make([]string, len(posts))
	for i, p := range posts {
		out[i] = p.Title
	}
	return out
}
