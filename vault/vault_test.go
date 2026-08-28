package vault_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/carlosframework/rastrillo/keyring"
	"github.com/carlosframework/rastrillo/vault"
)

func testConfig() vault.Config {
	return vault.Config{
		Home:  "https://home.test",
		Ring:  keyring.Ring{Namespace: "app"},
		Blobs: []string{"servers", "drafts"},
		Token: "tok",
		Seed:  bytes.Repeat([]byte{7}, 32),
	}
}

// TestNewValidates: New refuses what the doctrine refuses — a missing
// home (strictly additive: no home, no client), a namespace-less ring,
// and a blob name outside [a-z0-9-]{1,64} (the closed namespace is
// checkable at construction, and the name lands inside a derivation
// context string).
func TestNewValidates(t *testing.T) {
	if _, err := vault.New(testConfig()); err != nil {
		t.Fatalf("New(valid) = %v", err)
	}

	bad := []func(*vault.Config){
		func(c *vault.Config) { c.Home = "" },
		func(c *vault.Config) { c.Home = "home.test" }, // no scheme
		func(c *vault.Config) { c.Ring = keyring.Ring{} },
		func(c *vault.Config) { c.Blobs = []string{"Servers"} },
		func(c *vault.Config) { c.Blobs = []string{"a/b"} },
		func(c *vault.Config) { c.Blobs = []string{""} },
		func(c *vault.Config) { c.Blobs = nil },
		func(c *vault.Config) { c.Token = "" },
		func(c *vault.Config) { c.Seed = nil },
	}
	for i, mut := range bad {
		cfg := testConfig()
		mut(&cfg)
		if _, err := vault.New(cfg); err == nil {
			t.Fatalf("bad config %d: New accepted it", i)
		}
	}
}

// TestUndeclaredNameRefusesLocally: the closed namespace enforced
// before any request — ErrUndeclared, no dial.
func TestUndeclaredNameRefusesLocally(t *testing.T) {
	cfg := testConfig()
	cfg.Client = failingHTTPClient(t) // fails the test on any dial
	c, err := vault.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, err = c.Get(t.Context(), "settings")
	if !errors.Is(err, vault.ErrUndeclared) {
		t.Fatalf("Get(undeclared) err = %v, want ErrUndeclared", err)
	}
	if _, err := c.Put(t.Context(), "settings", []byte("x"), vault.Create); !errors.Is(err, vault.ErrUndeclared) {
		t.Fatalf("Put(undeclared) err = %v, want ErrUndeclared", err)
	}
}
