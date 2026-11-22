package roster_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/roster"
)

// rolesOnlyFixture is a fixture whose receiver sets RecordRolesOnly.
func rolesOnlyFixture(
	t *testing.T, members ...roster.Member,
) *fixture {
	t.Helper()
	fix := newFixture(t, members...)
	fix.rolesOnly = true
	fix.receiver = fix.process(t)
	return fix
}

func (fix *fixture) recordedMembers(t *testing.T) string {
	t.Helper()
	var members string
	err := fix.db.QueryRow(
		`SELECT members FROM roster_state WHERE id = 1`).Scan(&members)
	if err != nil {
		t.Fatal(err)
	}
	return members
}

// databaseBytes is every byte SQLite has on disk for this database: the
// main file and the write-ahead log, which Litestream ships as they are.
func (fix *fixture) databaseBytes(t *testing.T) []byte {
	t.Helper()
	var sequence int
	var name, path string
	err := fix.db.QueryRow(`PRAGMA database_list`).Scan(
		&sequence, &name, &path)
	if err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, file := range []string{path, path + "-wal"} {
		raw, err := os.ReadFile(file)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		all = append(all, raw...)
	}
	return all
}

func TestRolesOnlyRecordsNoEmailOrName(t *testing.T) {
	fix := rolesOnlyFixture(t)
	if code := fix.post(fix.token(t, 1, roster.Active, ownerA)); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	want := `[{"sub":"a","role":"owner"}]`
	if got := fix.recordedMembers(t); got != want {
		t.Errorf("recorded members = %s, want %s", got, want)
	}
	for _, text := range []string{ownerA.Email, `"A"`} {
		if bytes.Contains(fix.databaseBytes(t), []byte(text)) {
			t.Errorf("the database file holds %s", text)
		}
	}
	if fix.store.members["a"] != ownerA {
		t.Errorf("store = %v, want the whole member", fix.store.members)
	}
}

func TestTheDefaultRecordsTheSnapshotAsSigned(t *testing.T) {
	fix := newFixture(t)
	if code := fix.post(fix.token(t, 1, roster.Active, ownerA)); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	want := `[{"sub":"a","email":"a@example.com","name":"A","role":"owner"}]`
	if got := fix.recordedMembers(t); got != want {
		t.Errorf("recorded members = %s, want %s", got, want)
	}
}

func TestRolesOnlyRewritesAnOldRowAtTheNextSnapshot(t *testing.T) {
	fix := newFixture(t)
	token := fix.token(t, 1, roster.Active, ownerA)
	if code := fix.post(token); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	fix.rolesOnly = true
	// The root sending the same snapshot again is enough.
	if code := fix.postTo(fix.process(t), token); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	if got := fix.recordedMembers(t); got != `[{"sub":"a","role":"owner"}]` {
		t.Errorf("recorded members = %s, want subjects and roles", got)
	}
}

// loseLeaseTo stalls this process's first Put until its lease lapses,
// while newer applies another process's snapshot. Only then does the
// stale Put land, which the receiver must repair.
func (fix *fixture) loseLeaseTo(
	t *testing.T, other *roster.Receiver, newer string,
) {
	fix.store.onPut = func() {
		fix.store.onPut = nil
		fix.now = epoch.Add(31 * time.Second)
		if code := fix.postTo(other, newer); code != 204 {
			t.Errorf("newer snapshot status = %d, want 204", code)
		}
	}
}

func TestRolesOnlyRepairKeepsTheStoresContact(t *testing.T) {
	fix := rolesOnlyFixture(t, ownerA)
	demoted := roster.Member{Subject: "a", Email: ownerA.Email,
		Name: "Old", Role: "editor"}
	fix.loseLeaseTo(t, fix.process(t),
		fix.token(t, 5, roster.Active, ownerA))
	if code := fix.post(fix.token(t, 3, roster.Active, demoted)); code != 409 {
		t.Fatalf("status = %d, want 409", code)
	}
	// The role is repaired; the email and name are the Store's, which the
	// stale write left. The next snapshot brings back the signed name.
	got := fix.store.members["a"]
	want := roster.Member{Subject: "a", Email: ownerA.Email, Name: "Old",
		Role: "owner"}
	if got.Role != want.Role {
		t.Errorf("role = %s, want owner: the stale write survived", got.Role)
	}
	if got != want {
		t.Errorf("store a = %+v, want %+v: the repair blanked contact",
			got, want)
	}
	if got := fix.recordedMembers(t); got != `[{"sub":"a","role":"owner"}]` {
		t.Errorf("recorded members = %s, want subjects and roles", got)
	}
}

func TestRolesOnlyRepairFromAnOldRowRestoresEveryField(t *testing.T) {
	fix := rolesOnlyFixture(t, ownerA)
	fix.rolesOnly = false
	older := fix.process(t)
	demoted := roster.Member{Subject: "a", Email: "old@example.com",
		Name: "Old", Role: "editor"}
	fix.loseLeaseTo(t, older, fix.token(t, 5, roster.Active, ownerA))
	if code := fix.post(fix.token(t, 3, roster.Active, demoted)); code != 409 {
		t.Fatalf("status = %d, want 409", code)
	}
	if got := fix.store.members["a"]; got != ownerA {
		t.Errorf("store a = %+v, want %+v", got, ownerA)
	}
}

func TestRolesOnlyRepairAddsBackARemovedMemberWithoutContact(t *testing.T) {
	fix := rolesOnlyFixture(t, ownerA)
	// The stale apply leaves a out, so its Remove lands after the newer
	// roster put a back.
	fix.loseLeaseTo(t, fix.process(t),
		fix.token(t, 5, roster.Active, ownerA, editorC))
	if code := fix.post(fix.token(t, 3, roster.Active, editorC)); code != 409 {
		t.Fatalf("status = %d, want 409", code)
	}
	bare := roster.Member{Subject: "a", Role: "owner"}
	if got := fix.store.members["a"]; got != bare {
		t.Errorf("store a = %+v, want %+v", got, bare)
	}
	fix.now = epoch
	next := fix.token(t, 6, roster.Active, ownerA, editorC)
	if code := fix.post(next); code != 204 {
		t.Fatalf("next snapshot status = %d, want 204", code)
	}
	if got := fix.store.members["a"]; got != ownerA {
		t.Errorf("after the next snapshot a = %+v, want %+v", got, ownerA)
	}
}

func TestARepairFromARolesOnlyRowStaysRolesOnly(t *testing.T) {
	fix := newFixture(t, ownerA)
	fix.rolesOnly = true
	writer := fix.process(t)
	demoted := roster.Member{Subject: "a", Email: ownerA.Email,
		Name: "Old", Role: "editor"}
	fix.loseLeaseTo(t, writer, fix.token(t, 5, roster.Active, ownerA))
	if code := fix.post(fix.token(t, 3, roster.Active, demoted)); code != 409 {
		t.Fatalf("status = %d, want 409", code)
	}
	if got := fix.recordedMembers(t); got != `[{"sub":"a","role":"owner"}]` {
		t.Errorf("recorded members = %s: the repair recorded the Store's "+
			"contact as if the root had signed it", got)
	}
}
