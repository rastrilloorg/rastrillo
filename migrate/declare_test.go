package migrate

import (
	"testing"
	"testing/fstest"
)

func TestDeclareCarriesNamespace(t *testing.T) {
	set := MustFromFS(fstest.MapFS{}, "tickets").Declare(Data{Name: "tickets"})
	got := set.Declarations()
	if len(got) != 1 {
		t.Fatalf("got %d declarations, want 1: %+v", len(got), got)
	}
	if got[0].Namespace != "tickets" {
		t.Fatalf("Namespace = %q, want tickets", got[0].Namespace)
	}
}

func TestMergeKeepsDeclarationOrder(t *testing.T) {
	first := MustFromFS(fstest.MapFS{}, "a").Declare(Data{Name: "one"})
	second := MustFromFS(fstest.MapFS{}, "b").Declare(Data{Name: "two"})
	got := Merge(first, nil, second).Declarations()
	if len(got) != 2 {
		t.Fatalf("got %d declarations, want 2: %+v", len(got), got)
	}
	if got[0].Name != "one" || got[0].Namespace != "a" {
		t.Fatalf("first = %+v, want one in a", got[0])
	}
	if got[1].Name != "two" || got[1].Namespace != "b" {
		t.Fatalf("second = %+v, want two in b", got[1])
	}
}

func TestOwnerConstructors(t *testing.T) {
	if got, want := Via("ticket_id", "tickets"),
		(Owner{Column: "ticket_id", Parent: "tickets"}); got != want {
		t.Fatalf("Via = %+v, want %+v", got, want)
	}
	if Subject("owner").Holds != HoldsSubject {
		t.Fatal("Subject does not hold HoldsSubject")
	}
	if Address("email").Holds != HoldsAddress {
		t.Fatal("Address does not hold HoldsAddress")
	}
	if UserID("user_id").Holds != HoldsUserID {
		t.Fatal("UserID does not hold HoldsUserID")
	}
}

func TestRetireDropsEarlierDeclaration(t *testing.T) {
	owner := MustFromFS(fstest.MapFS{}, "a").Declare(Data{Name: "t"})
	dropper := MustFromFS(fstest.MapFS{}, "b").Retire("t")
	if got := Merge(owner, dropper).Declarations(); len(got) != 0 {
		t.Fatalf("got %+v, want no declarations", got)
	}
}

func TestRetireBeforeDeclareIsNoOp(t *testing.T) {
	dropper := MustFromFS(fstest.MapFS{}, "b").Retire("t")
	owner := MustFromFS(fstest.MapFS{}, "a").Declare(Data{Name: "t"})
	got := Merge(dropper, owner).Declarations()
	if len(got) != 1 || got[0].Name != "t" {
		t.Fatalf("got %+v, want t still declared", got)
	}
}

func TestRetireSurvivesNestedMerge(t *testing.T) {
	owner := MustFromFS(fstest.MapFS{}, "a").Declare(Data{Name: "t"})
	dropper := MustFromFS(fstest.MapFS{}, "b").Retire("t")
	inner := Merge(dropper, MustFromFS(fstest.MapFS{}, "c"))
	if got := Merge(owner, inner).Declarations(); len(got) != 0 {
		t.Fatalf("got %+v, want no declarations", got)
	}
}
