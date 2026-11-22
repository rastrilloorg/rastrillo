package totp_test

import (
	"errors"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/totp"
)

// Two tabs open the set-up page at once: the second runs whole between
// the first's new secret and its write. The first must still answer
// with the stored secret, so both tabs show one key and its code
// confirms.
func TestPendingOrBeginKeepsTheSecretAnotherTabStored(t *testing.T) {
	test := newEnv(t)
	var second totp.Enrolment
	totp.SetBeforeClaim(func() {
		totp.SetBeforeClaim(nil)
		var err error
		if second, err = test.h.PendingOrBegin("ana", "ana"); err != nil {
			t.Fatal(err)
		}
	})
	t.Cleanup(func() { totp.SetBeforeClaim(nil) })
	first, err := test.h.PendingOrBegin("ana", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == "" || first.Key != second.Key || first.QR != second.QR {
		t.Fatalf("the tabs show %q and %q", first.Key, second.Key)
	}
	code, err := totp.Code(first.Key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := test.h.Confirm("ana", code); err != nil || !ok {
		t.Fatalf("Confirm with the shown key's code: %v, %v", ok, err)
	}
}

func TestPendingOrBeginReusesBeginsSecret(t *testing.T) {
	test := newEnv(t)
	begun, err := test.h.Begin("ana", "ana")
	if err != nil {
		t.Fatal(err)
	}
	again, err := test.h.PendingOrBegin("ana", "ana")
	if err != nil || again.Key != begun.Key {
		t.Fatalf("PendingOrBegin = %q, %v; want Begin's %q", again.Key, err,
			begun.Key)
	}
}

func TestPendingOrBeginRefusesAnEnrolledSubject(t *testing.T) {
	test := newEnv(t)
	enrol(t, test, "ana")
	if _, err := test.h.PendingOrBegin("ana", "ana"); !errors.Is(err,
		totp.ErrEnrolled) {
		t.Fatalf("err = %v, want ErrEnrolled", err)
	}
}
