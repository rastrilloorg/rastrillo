package blindtest

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/mail"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/sessions"
	"github.com/keymaildev/signin"
)

type Config = auth.Config

var New = auth.New

const blindCanary = "  Blind.Canary+NeverPersist@EXAMPLE.COM  "

type discardMail struct{ recipient string }

func (sender *discardMail) Send(
	_ context.Context, recipient, _, _ string,
) error {
	sender.recipient = recipient
	return nil
}
func (sender *discardMail) sentTo() string { return sender.recipient }

type forbiddenStore struct{ writes int }

func (store *forbiddenStore) Put(context.Context, auth.BlindLink) error {
	store.writes++
	return nil
}
func (*forbiddenStore) Take(context.Context, string) (
	auth.BlindLink, bool, error,
) {
	return auth.BlindLink{}, false, nil
}
func (*forbiddenStore) Spend(context.Context, string) (int, error) {
	return 0, nil
}
func (*forbiddenStore) Sweep(context.Context, time.Time) error { return nil }

type blindHarness struct {
	auth   *auth.Auth
	config Config
	mail   *discardMail
	store  *forbiddenStore
}

func newBlindHarness(t *testing.T, _ bool) *blindHarness {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "config.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	_, err = migrate.Apply(context.Background(), database,
		migrate.Merge(sessions.Schema, auth.Schema))
	if err != nil {
		t.Fatal(err)
	}
	harness := &blindHarness{mail: &discardMail{}, store: &forbiddenStore{}}
	harness.config = Config{DB: database.Writer(), Origin: "http://app.test",
		InstanceKey: "test-key", ProofOff: true, Mailer: harness.mail,
		BlindLinks: &auth.BlindLinks{Store: harness.store,
			LookupKey: func(context.Context, string) (string, error) {
				return "email-index", nil
			},
			ResolveSubject: func(context.Context, string) (string, error) {
				return "person", nil
			},
		}}
	harness.configure(t)
	return harness
}
func (harness *blindHarness) configure(t *testing.T) {
	t.Helper()
	instance, err := New(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	harness.auth = instance
}
func (harness *blindHarness) scan(t *testing.T) {
	t.Helper()
	if harness.store.writes != 0 {
		t.Fatal("invalid key reached the store")
	}
}

func TestBlindLinksRejectUnsafeConfiguration(t *testing.T) {
	changes := map[string]func(*Config){
		"nil-store": func(config *Config) { config.BlindLinks.Store = nil },
		"nil-lookup": func(config *Config) {
			config.BlindLinks.LookupKey = nil
		},
		"nil-resolver": func(config *Config) {
			config.BlindLinks.ResolveSubject = nil
		},
		"nil-mailer": func(config *Config) { config.Mailer = nil },
		"log-mailer": func(config *Config) {
			config.Mailer = mail.Logged(slog.Default())
		},
		"upstream-log-mailer": func(config *Config) {
			config.Mailer = &signin.LogMailer{}
		},
		"address-authorize": func(config *Config) {
			config.Authorize = func(string) bool { return true }
		},
		"address-subject": func(config *Config) {
			config.SubjectFor = func(address string) (string, error) {
				return address, nil
			}
		},
		"screen": func(config *Config) { config.SigninScreen = true },
		"keymail": func(config *Config) {
			config.KeymailServers = []string{"provider.test"}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			change(&harness.config)
			if _, err := New(harness.config); err == nil {
				t.Fatal("unsafe blind configuration accepted")
			}
		})
	}
}

func TestBlindLinksRejectNonOpaqueLookupKeys(t *testing.T) {
	keys := []string{"", " ", blindCanary, strings.TrimSpace(blindCanary)}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			harness.config.BlindLinks.LookupKey = func(context.Context,
				string) (string, error) {
				return key, nil
			}
			harness.configure(t)
			if err := harness.auth.SendLink(context.Background(), blindCanary,
				"test"); err == nil {
				t.Fatal("non-opaque lookup key accepted")
			}
			if harness.mail.sentTo() != "" {
				t.Fatal("unsafe link was delivered")
			}
			harness.scan(t)
		})
	}
}

func TestBlindLinksRejectEveryRecordsModeCombination(t *testing.T) {
	for _, mode := range []string{"Records", "Blind", "Pepper", "Signed"} {
		t.Run(mode, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			switch mode {
			case "Records":
				key, err := sessions.NewSigningKey("test")
				if err != nil {
					t.Fatal(err)
				}
				harness.config.DB = nil
				harness.config.Records = objrec.Dir(t.TempDir())
				harness.config.Pepper = bytes.Repeat([]byte{7}, 32)
				harness.config.Signed = &sessions.Signed{Name: "test", Key: key}
				legacy := harness.config
				legacy.BlindLinks = nil
				if _, err := New(legacy); err != nil {
					t.Fatalf("records fixture is invalid: %v", err)
				}
			case "Blind":
				harness.config.Blind = true
			case "Pepper":
				harness.config.Pepper = []byte("consumer-pepper")
			case "Signed":
				harness.config.Signed = &sessions.Signed{}
			}
			if _, err := New(harness.config); err == nil {
				t.Fatal("mixed records and blind-link configuration accepted")
			}
		})
	}
}
