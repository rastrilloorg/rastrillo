package privacy

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/migrate"
)

func TestMatchesNormalisesAddresses(t *testing.T) {
	who := Identity{Subject: "u1", Addresses: []string{"ana@example.org"}}
	if !who.Matches(migrate.HoldsAddress, " ana@Example.org ") {
		t.Error("padded address with a mixed-case domain should match")
	}
	if who.Matches(migrate.HoldsAddress, "bo@example.org") {
		t.Error("another address matched")
	}
	if !who.Matches(migrate.HoldsSubject, "u1") ||
		who.Matches(migrate.HoldsSubject, "U1") {
		t.Error("subject matches by exact equality only")
	}
	if who.Matches(migrate.HoldsFreeText, "u1") {
		t.Error("free text never identifies a person")
	}
}

func TestMatchesKeepsLocalPartCase(t *testing.T) {
	who := NewIdentity("Ana@Example.org", []string{" Ana@Example.org "})
	if !who.Matches(migrate.HoldsAddress, "Ana@example.org") {
		t.Error("domain case should not matter")
	}
	if who.Matches(migrate.HoldsAddress, "ana@example.org") {
		t.Error("local part case must be kept")
	}
}

func TestEmptySubjectMatchesNothing(t *testing.T) {
	who := NewIdentity("", []string{"ana@example.org"})
	for _, kind := range []migrate.Holds{
		migrate.HoldsSubject, migrate.HoldsAddress,
		migrate.HoldsUserID, migrate.HoldsFreeText,
	} {
		if who.Matches(kind, "") || who.Matches(kind, "ana@example.org") {
			t.Errorf("empty-subject identity matched for kind %v", kind)
		}
	}
}

func TestUserIDOnlyForDecimalSubjects(t *testing.T) {
	for subject, want := range map[string]int64{
		"42": 42, "ana@x": 0, "-3": 0, "+3": 0, "042": 0, "0": 0,
		"": 0, "9223372036854775808": 0,
		"9223372036854775807": 9223372036854775807,
	} {
		got := NewIdentity(subject, nil).UserID
		if got != want {
			t.Errorf("UserID(%q) = %d, want %d", subject, got, want)
		}
	}
	who := NewIdentity("42", nil)
	if !who.Matches(migrate.HoldsUserID, "42") ||
		who.Matches(migrate.HoldsUserID, "43") {
		t.Error("HoldsUserID compares the decimal UserID")
	}
	if NewIdentity("ana@x", nil).Matches(migrate.HoldsUserID, "0") {
		t.Error("no UserID matches nothing, not zero")
	}
}
