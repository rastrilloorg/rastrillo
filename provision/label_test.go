package provision_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/provision"
)

func TestNewLabelShape(t *testing.T) {
	l, err := provision.NewLabel("acme")
	if err != nil || !regexp.MustCompile(`^acme-[a-z2-7]{6}$`).MatchString(l) {
		t.Fatalf("NewLabel(acme) = %q, %v", l, err)
	}
	if l2, _ := provision.NewLabel("acme"); l2 == l {
		t.Fatal("two labels for one slug are equal: the suffix is not random")
	}
}

func TestNewLabelFitsAnEnvironmentName(t *testing.T) {
	// Cut to MaxSlug the slug ends on its hyphen, which is trimmed, so the
	// label is the 24 a's, one hyphen and the suffix: 31 characters.
	l, err := provision.NewLabel(strings.Repeat("a", 24) + "-bcd")
	if err != nil || !regexp.MustCompile(`^a{24}-[a-z2-7]{6}$`).MatchString(l) || len(l) > 32 {
		t.Fatalf("NewLabel(long) = %q, %v: want 24 a's, one hyphen and 6 suffix characters, at most 32 (a private-storage environment name)", l, err)
	}
}

func TestNewLabelRefusesWhatCannotBeADNSLabel(t *testing.T) {
	for _, s := range []string{"", "-a", "a-", "Acme", "a.b", "a_b", "a b", "ä"} {
		if _, err := provision.NewLabel(s); !errors.Is(err, provision.ErrSlug) {
			t.Errorf("NewLabel(%q): got %v, want ErrSlug", s, err)
		}
	}
}
