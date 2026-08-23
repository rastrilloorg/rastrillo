package keyring

import (
	"errors"
	"testing"
)

func TestAddWrapDedupesByID(t *testing.T) {
	wraps := AddWrap(nil, Wrap{ID: "a", Label: "first"})
	wraps = AddWrap(wraps, Wrap{ID: "b", Label: "second"})
	if len(wraps) != 2 {
		t.Fatalf("len = %d, want 2", len(wraps))
	}

	wraps = AddWrap(wraps, Wrap{ID: "a", Label: "re-enrolled"})
	if len(wraps) != 2 {
		t.Fatalf("dedupe failed: len = %d, want 2", len(wraps))
	}
	if wraps[0].Label != "re-enrolled" {
		t.Fatalf("AddWrap did not replace the wrap with the same ID: %+v", wraps[0])
	}
	if wraps[1].ID != "b" {
		t.Fatalf("replacement disturbed order: %+v", wraps)
	}
}

func TestRemoveWrap(t *testing.T) {
	wraps := []Wrap{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	out, err := RemoveWrap(wraps, "b")
	if err != nil {
		t.Fatalf("RemoveWrap: %v", err)
	}
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "c" {
		t.Fatalf("RemoveWrap(b) = %+v", out)
	}

	if _, err := RemoveWrap(wraps, "nope"); !errors.Is(err, ErrUnknownWrap) {
		t.Fatalf("unknown id: err = %v, want ErrUnknownWrap", err)
	}

	if _, err := RemoveWrap([]Wrap{{ID: "only"}}, "only"); !errors.Is(err, ErrLastWrap) {
		t.Fatalf("last wrap: err = %v, want ErrLastWrap", err)
	}

	// A miss on a one-element list is a miss, not a stranding: nothing
	// would have been removed.
	if _, err := RemoveWrap([]Wrap{{ID: "only"}}, "nope"); !errors.Is(err, ErrUnknownWrap) {
		t.Fatalf("miss on len==1: err = %v, want ErrUnknownWrap", err)
	}
}

func TestWrapHelpersArePure(t *testing.T) {
	in := []Wrap{{ID: "a", Label: "original"}, {ID: "b"}}

	AddWrap(in, Wrap{ID: "a", Label: "mutated?"})
	if in[0].Label != "original" {
		t.Fatal("AddWrap mutated its input")
	}

	if _, err := RemoveWrap(in, "a"); err != nil {
		t.Fatalf("RemoveWrap: %v", err)
	}
	if len(in) != 2 || in[0].ID != "a" || in[1].ID != "b" {
		t.Fatal("RemoveWrap mutated its input")
	}
}
