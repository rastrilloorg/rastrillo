package migrate

import (
	"testing"
	"testing/fstest"
)

func TestFromFSOrdersAndIgnoresNonMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE b (n INTEGER);")},
		"migrations/0001_first.sql":  {Data: []byte("CREATE TABLE a (n INTEGER);")},
		"migrations/schema.sql":      {Data: []byte("-- snapshot, never applied")},
		"migrations/notes.md":        {Data: []byte("# hi")},
	}
	s, err := FromFS(fsys, "notes")
	if err != nil {
		t.Fatal(err)
	}
	got := s.All()
	if len(got) != 2 {
		t.Fatalf("got %d migrations, want 2: %+v", len(got), got)
	}
	if got[0].ID != "notes/0001_first" || got[1].ID != "notes/0002_second" {
		t.Fatalf("IDs = %q, %q; want notes/0001_first, notes/0002_second", got[0].ID, got[1].ID)
	}
}

func TestMergePreservesArgumentOrder(t *testing.T) {
	a := (&Set{namespace: "a"}).Add(Migration{ID: "0001_a", SQL: "SELECT 1;"})
	b := (&Set{namespace: "b"}).Add(Migration{ID: "0001_b", SQL: "SELECT 2;"})
	got := Merge(b, a).All()
	if got[0].ID != "b/0001_b" || got[1].ID != "a/0001_a" {
		t.Fatalf("Merge order = %q, %q; want b then a", got[0].ID, got[1].ID)
	}
}

func TestChecksumIgnoresWhitespace(t *testing.T) {
	if Checksum("CREATE  TABLE\n a (n INTEGER);") != Checksum("CREATE TABLE a (n INTEGER);") {
		t.Fatal("checksum changed on reformatting")
	}
	if Checksum("CREATE TABLE a (n INTEGER);") == Checksum("CREATE TABLE b (n INTEGER);") {
		t.Fatal("checksum collided on different SQL")
	}
}

func TestFromFSRejectsBadNameAndDuplicateID(t *testing.T) {
	_, err := FromFS(fstest.MapFS{
		"migrations/1_short.sql": {Data: []byte("SELECT 1;")},
	}, "x")
	if err == nil {
		t.Fatal("want error for a .sql file that is neither NNNN_name.sql nor schema.sql")
	}
}

func TestMergeComposesWithItself(t *testing.T) {
	a := (&Set{namespace: "a"}).Add(Migration{ID: "0001_a", SQL: "SELECT 1;"})
	b := (&Set{namespace: "b"}).Add(Migration{ID: "0001_b", SQL: "SELECT 2;"})
	c := (&Set{namespace: "c"}).Add(Migration{ID: "0001_c", SQL: "SELECT 3;"})

	inner := Merge(a, b)     // Result has empty namespace
	outer := Merge(inner, c) // Merge with another namespaced set
	got := outer.All()

	// Check no ID has a leading slash (the bug would produce "/a/0001_a", "/b/0001_b")
	if got[0].ID != "a/0001_a" {
		t.Fatalf("got[0].ID = %q; want a/0001_a", got[0].ID)
	}
	if got[1].ID != "b/0001_b" {
		t.Fatalf("got[1].ID = %q; want b/0001_b", got[1].ID)
	}
	if got[2].ID != "c/0001_c" {
		t.Fatalf("got[2].ID = %q; want c/0001_c", got[2].ID)
	}

	// Verify argument order is preserved through nesting
	if len(got) != 3 {
		t.Fatalf("got %d migrations, want 3", len(got))
	}
}
