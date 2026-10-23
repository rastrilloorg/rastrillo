package rastrillo

import (
	"log/slog"
	"testing"
)

// enTf is the English catalog's translator, the one a page in English
// gets, so the phrase is checked against the shipped wording rather
// than a stub that would pass whatever the catalog said.
func enTf(t *testing.T) func(key string, args ...any) string {
	t.Helper()
	l, err := NewLocales([]string{"en"}, "en", BaseCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return func(key string, args ...any) string { return l.Tf("en", key, args...) }
}

func TestActorPhrase(t *testing.T) {
	tf := enTf(t)
	paul := func(subject string) string {
		if subject == "7" {
			return "Paul"
		}
		return ""
	}
	cases := []struct {
		name string
		a    Actor
		look func(string) string
		want string
	}{
		{"the app is not named", Actor{}, paul, ""},
		{"a person with no lookup is their subject", Actor{Subject: "7", Access: Write}, nil, "7"},
		{"a person is their name", Actor{Subject: "7", Access: Write}, paul, "Paul"},
		{"a person's own token is still the person", Actor{Subject: "7", Credential: "k3Jx9QpL2mWa", Access: Read}, paul, "Paul"},
		{"an agent acts for its person", Actor{Subject: "7", Agent: "Claude", Credential: "k3Jx9QpL2mWa", Access: Write}, paul, "Claude for Paul"},
		{"an agent with no lookup acts for the subject", Actor{Subject: "7", Agent: "Claude", Credential: "k3Jx9QpL2mWa"}, nil, "Claude for 7"},
		// A lookup that cannot name the subject (a deleted account) must
		// not leave "Claude for " dangling with nobody after it.
		{"an unnamed person falls back to the subject", Actor{Subject: "9", Agent: "Claude"}, paul, "Claude for 9"},
	}
	for _, c := range cases {
		if got := ActorPhrase(c.a, c.look, tf); got != c.want {
			t.Errorf("%s: ActorPhrase(%+v) = %q, want %q", c.name, c.a, got, c.want)
		}
	}
}

func TestActorString(t *testing.T) {
	cases := []struct {
		a    Actor
		want string
	}{
		{Actor{}, "app"},
		{Actor{Subject: "paul@keymail.dev", Access: Write}, "user:paul@keymail.dev"},
		{Actor{Subject: "7", Credential: "k3Jx9QpL2mWa", Access: Read}, "user:7 token:k3Jx9QpL2mWa"},
		{Actor{Subject: "7", Agent: "Claude", Credential: "k3Jx9QpL2mWa", Access: Write}, "agent:Claude for user:7 token:k3Jx9QpL2mWa"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("%+v.String() = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestAccessZeroIsInvalid(t *testing.T) {
	var a Access
	if a.Valid() {
		t.Fatal("zero Access must be invalid: a route that forgets to say would otherwise be readable by a read-only token")
	}
	if !Read.Valid() || !Write.Valid() {
		t.Fatal("Read and Write must be valid")
	}
}

func TestActorLogAttrsCarrySessionAndHarness(t *testing.T) {
	a := Actor{Subject: "7", Agent: "Claude", Credential: "k3Jx9QpL2mWa", Access: Write, Session: "s-1", Harness: "claude-code/2.1.288"}
	got := map[string]string{}
	for _, at := range a.LogAttrs() {
		got[at.Key] = at.Value.String()
	}
	for k, v := range map[string]string{"actor": a.String(), "agent_session": "s-1", "agent_harness": "claude-code/2.1.288"} {
		if got[k] != v {
			t.Errorf("LogAttrs[%q] = %q, want %q", k, got[k], v)
		}
	}
	_ = slog.Attr{}
}
