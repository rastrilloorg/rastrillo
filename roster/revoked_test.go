package roster_test

import (
	"strconv"
	"testing"

	"amadan.net/rastrillo/rastrillo/roster"
)

// staleReplay revokes g1 with seq 3 (which lists next), replays seq 2 and
// checks that both the repairing receiver and a new one still refuse g1.
func staleReplay(t *testing.T, rolesOnly bool, next ...roster.Member) *fixture {
	t.Helper()
	fix := newFixture(t)
	fix.rolesOnly = rolesOnly
	fix.receiver = fix.process(t)
	seq2 := fix.token(t, 2, roster.Active, withGrants(ownerA, "g1"))
	fix.mustPost(t, 204, seq2)
	if !fix.active(t, "a", "g1") {
		t.Fatal("g1 is not active after seq 2")
	}
	fix.mustPost(t, 204, fix.token(t, 3, roster.Active, next...))
	fix.mustPost(t, 409, seq2)
	if fix.active(t, "a", "g1") {
		t.Error("the replay brought g1 back")
	}
	if fix.activeOn(t, fix.process(t), "a", "g1") {
		t.Error("a new receiver says g1 is active")
	}
	return fix
}

func TestRevokedGrantStaysRevokedAfterStaleReplay(t *testing.T) {
	for _, rolesOnly := range []bool{true, false} {
		t.Run(strconv.FormatBool(rolesOnly), func(t *testing.T) {
			staleReplay(t, rolesOnly, ownerA)
		})
	}
}

func TestRemovedMemberGrantStaysRevokedAfterStaleReplay(t *testing.T) {
	for _, rolesOnly := range []bool{true, false} {
		t.Run(strconv.FormatBool(rolesOnly), func(t *testing.T) {
			fix := staleReplay(t, rolesOnly, memberB)
			if _, held := fix.store.members["a"]; held {
				t.Error("the store holds a, who was removed")
			}
		})
	}
}

// A stale apply that overlaps a newer revocation loses its lease, and its
// Put lands after the newer snapshot's. The repair must not bring g1 back.
func TestRevokedGrantStaysRevokedWhenAStaleApplyLosesItsLease(t *testing.T) {
	for _, row := range []struct {
		name string
		next []roster.Member
		gone bool
	}{
		{"revoked", []roster.Member{ownerA, editorC}, false},
		{"member removed", []roster.Member{editorC}, true},
	} {
		for _, rolesOnly := range []bool{true, false} {
			name := row.name + "/" + strconv.FormatBool(rolesOnly)
			t.Run(name, func(t *testing.T) {
				fix := newFixture(t)
				fix.rolesOnly = rolesOnly
				fix.receiver = fix.process(t)
				granted := withGrants(ownerA, "g1")
				fix.mustPost(t, 204,
					fix.token(t, 2, roster.Active, granted))
				fix.loseLeaseTo(t, fix.process(t),
					fix.token(t, 5, roster.Active, row.next...))
				fix.mustPost(t, 409,
					fix.token(t, 3, roster.Active, granted, editorC))
				if fix.active(t, "a", "g1") {
					t.Error("the stale apply brought g1 back")
				}
				if fix.activeOn(t, fix.process(t), "a", "g1") {
					t.Error("a new receiver says g1 is active")
				}
				_, held := fix.store.members["a"]
				if held == row.gone {
					t.Errorf("store holds a = %v, want %v", held, !row.gone)
				}
			})
		}
	}
}
