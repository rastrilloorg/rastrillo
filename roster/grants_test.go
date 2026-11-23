package roster_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	"amadan.net/rastrillo/rastrillo/roster"
	json "github.com/go-json-experiment/json"
)

func withGrants(member roster.Member, grants ...string) roster.Member {
	member.Grants = grants
	return member
}

func TestSignedSnapshotCarriesGrants(t *testing.T) {
	fix := newFixture(t)
	member := withGrants(ownerA, "g1", "g2")
	token := fix.token(t, 1, roster.Active, member)
	got, err := fix.verifier.Verify(token, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 1 || len(got.Members[0].Grants) != 2 ||
		got.Members[0].Grants[0] != "g1" || got.Members[0].Grants[1] != "g2" {
		t.Errorf("members = %+v, want grants g1 and g2", got.Members)
	}
}

func manyGrants(count int) []string {
	grants := make([]string, count)
	for index := range grants {
		grants[index] = "g" + strconv.Itoa(index)
	}
	return grants
}

func TestVerifyRefusesBadGrants(t *testing.T) {
	long := make([]byte, 65)
	for index := range long {
		long[index] = 'a'
	}
	cases := map[string][]roster.Member{
		"empty ID":      {withGrants(ownerA, "")},
		"65 characters": {withGrants(ownerA, string(long))},
		"a space":       {withGrants(ownerA, "g 1")},
		"twice":         {withGrants(ownerA, "g1", "g1")},
		"two members": {
			withGrants(ownerA, "g1"), withGrants(memberB, "g1"),
		},
		"65 grants": {withGrants(ownerA, manyGrants(65)...)},
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			fix := newFixture(t)
			snapshot := goodSnapshot(members)
			if _, err := roster.Sign(fix.key, snapshot); !errors.Is(
				err, roster.ErrClaims) {
				t.Errorf("Sign error = %v, want ErrClaims", err)
			}
			token := signUnchecked(t, fix.key, snapshot)
			if _, err := fix.verifier.Verify(token, epoch); !errors.Is(
				err, roster.ErrClaims) {
				t.Errorf("Verify error = %v, want ErrClaims", err)
			}
		})
	}
}

// The limits are inclusive: 64 grants, one of them 64 characters long.
func TestSignAndVerifyAcceptTheGrantLimits(t *testing.T) {
	fix := newFixture(t)
	grants := append(manyGrants(63), strings.Repeat("a", 64))
	token := fix.token(t, 1, roster.Active, withGrants(ownerA, grants...))
	got, err := fix.verifier.Verify(token, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Members[0].Grants, grants) {
		t.Errorf("grants = %v, want %v", got.Members[0].Grants, grants)
	}
}

func goodSnapshot(members []roster.Member) roster.Snapshot {
	return roster.Snapshot{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		KeyID: "key_1", Sequence: 1, IssuedAt: epoch.Unix(),
		ExpiresAt: epoch.Unix() + 60, State: roster.Active,
		Members: members,
	}
}

// signUnchecked signs what Sign would refuse, so Verify is tested alone.
func signUnchecked(
	t *testing.T, key *crypto.Keypair, snapshot roster.Snapshot,
) string {
	t.Helper()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(key, "rastrillo/roster/v1", payload)
	if err != nil {
		t.Fatal(err)
	}
	return wire.Join(payload, signature)
}

func (fix *fixture) active(t *testing.T, subject, grant string) bool {
	t.Helper()
	return fix.activeOn(t, fix.receiver, subject, grant)
}

func (fix *fixture) activeOn(
	t *testing.T, receiver *roster.Receiver, subject, grant string,
) bool {
	t.Helper()
	active, err := receiver.GrantActive(context.Background(), subject, grant)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func (fix *fixture) mustPost(t *testing.T, want int, token string) {
	t.Helper()
	if code := fix.post(token); code != want {
		t.Fatalf("status = %d, want %d", code, want)
	}
}

func TestGrantActive(t *testing.T) {
	fix := newFixture(t)
	if fix.active(t, "a", "g1") {
		t.Error("a grant is active before any snapshot")
	}
	fix.mustPost(t, 204,
		fix.token(t, 1, roster.Active, withGrants(ownerA, "g1")))
	if !fix.active(t, "a", "g1") || fix.active(t, "a", "g2") {
		t.Error("after seq 1, want g1 active and g2 not")
	}
	fix.mustPost(t, 204, fix.token(t, 2, roster.Active, ownerA))
	if fix.active(t, "a", "g1") {
		t.Error("g1 is still active after seq 2 dropped it")
	}
}

func TestGrantActiveIsPerSubject(t *testing.T) {
	fix := newFixture(t)
	fix.mustPost(t, 204, fix.token(t, 1, roster.Active,
		withGrants(ownerA), withGrants(memberB, "g1")))
	if fix.active(t, "a", "g1") {
		t.Error("g1 is active for a, who does not hold it")
	}
	if !fix.active(t, "b", "g1") {
		t.Error("g1 is not active for b")
	}
}

func TestGrantActiveFalseWhenSuspended(t *testing.T) {
	fix := newFixture(t)
	fix.mustPost(t, 204,
		fix.token(t, 1, roster.Suspended, withGrants(ownerA, "g1")))
	if fix.active(t, "a", "g1") {
		t.Error("a grant is active on a suspended instance")
	}
}

func TestGrantsSurviveRolesOnlyAndRepair(t *testing.T) {
	for _, rolesOnly := range []bool{true, false} {
		t.Run(strconv.FormatBool(rolesOnly), func(t *testing.T) {
			fix := newFixture(t)
			fix.rolesOnly = rolesOnly
			fix.receiver = fix.process(t)
			fix.mustPost(t, 204,
				fix.token(t, 2, roster.Active, withGrants(ownerA, "g1")))
			if !fix.active(t, "a", "g1") {
				t.Fatal("g1 is not active after seq 2")
			}
			fix.mustPost(t, 409, fix.token(t, 1, roster.Active, ownerA))
			if !fix.active(t, "a", "g1") {
				t.Error("the repair lost g1")
			}
		})
	}
}

func TestGrantsAloneDoNotRewriteStore(t *testing.T) {
	fix := newFixture(t)
	fix.mustPost(t, 204,
		fix.token(t, 1, roster.Active, withGrants(ownerA, "g1")))
	puts := len(fix.store.puts)
	fix.mustPost(t, 204,
		fix.token(t, 2, roster.Active, withGrants(ownerA, "g1", "g2")))
	if got := len(fix.store.puts); got != puts {
		t.Errorf("Put ran %d more times, want 0", got-puts)
	}
	if !fix.active(t, "a", "g2") {
		t.Error("g2 was not recorded")
	}
}
