package apitoken_test

import (
	"os"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/dbtest"
)

var schema = dbtest.FromSet(apitoken.Schema)

func TestMain(m *testing.M) {
	code := m.Run()
	schema.Remove()
	os.Exit(code)
}

// shaped builds a token-shaped string at run time. A literal one in
// this file would be committed, and gitleaks would rightly flag it;
// the public ID is the one .gitleaks.toml already allows.
func shaped(prefix, id, secret string) string { return prefix + "_" + id + "_" + secret }

const exampleID = "k3Jx9QpL2mWa"

// fakeSecret is 43 base62 characters that are not any minted secret.
var fakeSecret = strings.Repeat("Zq7", 15)[:43]

func TestDerivePrefix(t *testing.T) {
	d := schema.Open(t)
	for in, want := range map[string]string{
		"tickets":                      "tickets",
		"My-App 2":                     "myapp2",
		"a-very-long-application-name": "averylongapplica",
		"--":                           "app",
		"":                             "app",
		"9lives":                       "app9lives",
		"123-app":                      "app123app",
		"café":                         "caf",
		"Ticket_Desk":                  "ticketdesk",
		"\u212Aelvin":                  "elvin", // the Kelvin sign is not an ASCII K
		"\u0130stanbul":                "stanbul",
	} {
		if got := apitoken.DerivePrefix(in); got != want {
			t.Errorf("DerivePrefix(%q) = %q, want %q", in, got, want)
		}
		// Whatever it derives must be a prefix New accepts, or the
		// scaffold would write an app that cannot start.
		if _, err := apitoken.New(apitoken.Config{DB: d, Prefix: apitoken.DerivePrefix(in)}); err != nil {
			t.Errorf("New refuses DerivePrefix(%q) = %q: %v", in, apitoken.DerivePrefix(in), err)
		}
	}
}

func TestValidAgentName(t *testing.T) {
	for in, want := range map[string]string{
		"Claude":                "Claude",
		"  Nightly import ":     "Nightly import",
		"codex-1.2":             "codex-1.2",
		strings.Repeat("a", 40): strings.Repeat("a", 40),
	} {
		got, ok := apitoken.ValidAgentName(in)
		if !ok || got != want {
			t.Errorf("ValidAgentName(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "   ", "<script>", "Claude_Code", "Clåude", "a\tb", strings.Repeat("a", 41)} {
		if got, ok := apitoken.ValidAgentName(in); ok {
			t.Errorf("ValidAgentName(%q) = %q, true; want refused", in, got)
		}
	}
}

func TestNewRefusesBadConfig(t *testing.T) {
	d := schema.Open(t)
	if _, err := apitoken.New(apitoken.Config{Prefix: "tickets"}); err == nil {
		t.Error("New with no DB: want an error")
	}
	for _, p := range []string{"", "Tickets", "9lives", "tick_ets", "tickets-app", "abcdefghijklmnopq", "café"} {
		if _, err := apitoken.New(apitoken.Config{DB: d, Prefix: p}); err == nil {
			t.Errorf("New with Prefix %q: want an error", p)
		}
	}
	if _, err := apitoken.New(apitoken.Config{DB: d, Prefix: "abcdefghijklmnop"}); err != nil {
		t.Errorf("New with a 16-character prefix: %v", err)
	}
}

func TestRedact(t *testing.T) {
	s, err := apitoken.New(apitoken.Config{DB: schema.Open(t), Prefix: "tickets"})
	if err != nil {
		t.Fatal(err)
	}
	in := "err: " + shaped("tickets", exampleID, fakeSecret) + " failed"
	want := "err: " + shaped("tickets", exampleID, "[redacted]") + " failed"
	if got := s.Redact(in); got != want {
		t.Errorf("Redact(%q) = %q, want %q", in, got, want)
	}
	two := shaped("tickets", exampleID, fakeSecret) + "," + shaped("tickets", exampleID, fakeSecret)
	if got := s.Redact(two); strings.Contains(got, fakeSecret) {
		t.Errorf("Redact left a secret in %q", got)
	}
	// A secret one character too long or too short is still most of a
	// secret; it is redacted, not left because it misses the grammar.
	for _, sec := range []string{fakeSecret + "x", fakeSecret[:42]} {
		if got := s.Redact(shaped("tickets", exampleID, sec)); strings.Contains(got, sec[:20]) {
			t.Errorf("Redact left a near-token's secret in %q", got)
		}
	}
	for _, plain := range []string{"", "nothing to see", "tickets_ alone", "err: other_" + exampleID + " failed"} {
		if got := s.Redact(plain); got != plain {
			t.Errorf("Redact(%q) = %q, want it unchanged", plain, got)
		}
	}
}
