package auth

import (
	"context"
	"errors"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/privacy"
)

func TestIdentifyDefaultModeSubjectIsAddress(t *testing.T) {
	auth, _ := newTestAuth(t, nil)
	who, err := auth.Identify(context.Background(), " Ana@Example.org ")
	if err != nil {
		t.Fatal(err)
	}
	if who.Subject != " Ana@Example.org " {
		t.Errorf("Subject = %q", who.Subject)
	}
	if !slices.Equal(who.Addresses, []string{"Ana@example.org"}) {
		t.Errorf("Addresses = %v", who.Addresses)
	}
}

func TestIdentifyRefusesEmptySubject(t *testing.T) {
	ctx := context.Background()
	plain, _ := newTestAuth(t, nil)
	blindLinks := newBlindHarness(t, true).auth
	blindRecords, _, _ := newRecordsAuth(t, func(cfg *Config) {
		cfg.Blind = true
	})
	for name, auth := range map[string]*Auth{
		"default": plain, "blind links": blindLinks,
		"blind records": blindRecords,
	} {
		for _, subject := range []string{"", "  \t"} {
			_, err := auth.Identify(ctx, subject)
			if err == nil ||
				err.Error() != "rastrillo/auth: identify: empty subject" {
				t.Errorf("%s %q: err = %v", name, subject, err)
			}
		}
	}
}

func TestIdentifyRecordsModeReadsTheUser(t *testing.T) {
	auth, _, _ := newRecordsAuth(t, nil)
	ctx := context.Background()
	user, err := auth.EnsureUser(ctx, "Ana@Example.org")
	if err != nil {
		t.Fatal(err)
	}
	who, err := auth.Identify(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if who.Subject != user.ID ||
		!slices.Equal(who.Addresses, []string{user.Email}) {
		t.Errorf("identity = %+v", who)
	}
	_, err = auth.Identify(ctx, "u-nobody")
	if !errors.Is(err, objrec.ErrNotFound) {
		t.Errorf("unknown subject: err = %v, want ErrNotFound", err)
	}
}

func TestIdentifyBlindRecordsHasNoAddresses(t *testing.T) {
	auth, _, _ := newRecordsAuth(t, func(cfg *Config) { cfg.Blind = true })
	ctx := context.Background()
	user, err := auth.EnsureUser(ctx, "ana@example.org")
	if err != nil {
		t.Fatal(err)
	}
	who, err := auth.Identify(ctx, user.ID)
	if err != nil || len(who.Addresses) != 0 || who.Subject != user.ID {
		t.Errorf("identity = %+v, err = %v", who, err)
	}
	// Only the blind branch skips the user lookup, so an unknown
	// subject succeeds there and would fail through User.
	if _, err := auth.Identify(ctx, "u-nobody"); err != nil {
		t.Errorf("blind records must not look the user up: %v", err)
	}
}

func TestIdentifyBlindLinksHasNoAddresses(t *testing.T) {
	harness := newBlindHarness(t, true)
	who, err := harness.auth.Identify(context.Background(), blindSubject)
	if err != nil || len(who.Addresses) != 0 || who.Subject != blindSubject {
		t.Errorf("identity = %+v, err = %v", who, err)
	}
}

func TestIdentifySubjectForUsesAddressesFor(t *testing.T) {
	remap := func(string) (string, error) { return "p-1", nil }
	auth, _ := newTestAuth(t, func(cfg *Config) { cfg.SubjectFor = remap })
	_, err := auth.Identify(context.Background(), "p-1")
	if !errors.Is(err, privacy.ErrUnidentifiable) {
		t.Errorf("err = %v, want ErrUnidentifiable", err)
	}
	auth, _ = newTestAuth(t, func(cfg *Config) {
		cfg.SubjectFor = remap
		cfg.AddressesFor = func(string) ([]string, error) {
			return []string{"B@x.org", "a@X.org", " B@x.org"}, nil
		}
	})
	who, err := auth.Identify(context.Background(), "p-1")
	want := []string{"B@x.org", "a@x.org"}
	if err != nil || !slices.Equal(who.Addresses, want) {
		t.Errorf("identity = %+v, err = %v", who, err)
	}
}

func TestIdentifyAddressesForError(t *testing.T) {
	boom := errors.New("directory down")
	auth, _ := newTestAuth(t, func(cfg *Config) {
		cfg.SubjectFor = func(string) (string, error) { return "p-1", nil }
		cfg.AddressesFor = func(string) ([]string, error) { return nil, boom }
	})
	_, err := auth.Identify(context.Background(), "p-1")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the AddressesFor error", err)
	}
}
