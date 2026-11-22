package totp_test

import (
	"strings"
	"testing"
)

// A page in another language hands the QR its own words for a screen
// reader; the code drawn is the same one QR draws.
func TestLabelledQRSaysTheLabelItIsGiven(t *testing.T) {
	enrolment, err := newEnv(t).h.Begin("alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	labelled, err := enrolment.LabelledQR(`Cód QR "d'aip"`)
	if err != nil {
		t.Fatal(err)
	}
	want := `aria-label="Cód QR &quot;d'aip&quot;"`
	if !strings.Contains(labelled, want) {
		t.Fatalf("LabelledQR lacks %s: %.200s", want, labelled)
	}
	english := `aria-label="QR code for your authenticator app"`
	if strings.Replace(labelled, want, english, 1) != enrolment.QR {
		t.Fatal("LabelledQR draws a different code from QR")
	}
}
