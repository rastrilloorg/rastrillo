package rastrillo

import (
	"regexp"
	"testing"
)

// The shape is the contract: a person has to read this back to support,
// so it is six characters, lowercase, and drawn from an alphabet with no
// confusable digits.
func TestNewRefShape(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z2-7]{6}$`)
	for i := 0; i < 200; i++ {
		if got := NewRef(); !shape.MatchString(got) {
			t.Fatalf("NewRef() = %q, want %v", got, shape)
		}
	}
}

// Fresh entropy per call: a constant reference would join every error in
// the log to every error a user saw, which is the opposite of the point.
func TestNewRefIsRandom(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		ref := NewRef()
		if seen[ref] {
			t.Fatalf("NewRef() repeated %q within 200 calls", ref)
		}
		seen[ref] = true
	}
}
