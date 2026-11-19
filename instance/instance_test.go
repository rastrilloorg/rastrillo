package instance_test

import (
	"errors"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/instance"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func publicKey(t *testing.T) string {
	t.Helper()
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	return key.Public()
}

func TestFromEnvRootedReadsTheRootAndItsKeys(t *testing.T) {
	setup, err := instance.FromEnv(environment(map[string]string{
		"RASTRILLO_ROOT":      "https://example.com",
		"RASTRILLO_ROOT_KEYS": publicKey(t),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if setup.Mode != instance.Rooted {
		t.Errorf("mode = %v, want Rooted", setup.Mode)
	}
	if setup.Root != "https://example.com" {
		t.Errorf("root = %q", setup.Root)
	}
	if len(setup.Keys) != 1 {
		t.Errorf("keys = %d, want 1", len(setup.Keys))
	}
}

func TestFromEnvStandalone(t *testing.T) {
	setup, err := instance.FromEnv(environment(map[string]string{
		"RASTRILLO_STANDALONE": "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if setup.Mode != instance.Standalone {
		t.Errorf("mode = %v, want Standalone", setup.Mode)
	}
	if setup.Root != "" || len(setup.Keys) != 0 {
		t.Errorf("standalone carries root %q, keys %v",
			setup.Root, setup.Keys)
	}
}

func TestFromEnvRefusesNeither(t *testing.T) {
	_, err := instance.FromEnv(environment(nil))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, name := range []string{
		"RASTRILLO_ROOT", "RASTRILLO_ROOT_KEYS", "RASTRILLO_STANDALONE",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestFromEnvRefusesBoth(t *testing.T) {
	_, err := instance.FromEnv(environment(map[string]string{
		"RASTRILLO_STANDALONE": "1",
		"RASTRILLO_ROOT":       "https://example.com",
		"RASTRILLO_ROOT_KEYS":  publicKey(t),
	}))
	want := "rastrillo/instance: RASTRILLO_STANDALONE=1 and " +
		"RASTRILLO_ROOT are both set; set only one"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestFromEnvRefusesARootWithoutKeys(t *testing.T) {
	_, err := instance.FromEnv(environment(map[string]string{
		"RASTRILLO_ROOT": "https://example.com",
	}))
	want := "rastrillo/instance: RASTRILLO_ROOT is set but " +
		"RASTRILLO_ROOT_KEYS is empty; set the root app's public keys"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestFromEnvRefusesKeysWithoutARoot(t *testing.T) {
	_, err := instance.FromEnv(environment(map[string]string{
		"RASTRILLO_ROOT_KEYS": publicKey(t),
	}))
	want := "rastrillo/instance: RASTRILLO_ROOT_KEYS is set but " +
		"RASTRILLO_ROOT is empty; set the root app's origin"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestFromEnvRefusesARootThatIsNotAnHTTPSOrigin(t *testing.T) {
	keys := publicKey(t)
	for _, root := range []string{
		"http://example.com",
		"https://example.com/app",
		"https://example.com/",
		"https://example.com?x=1",
		"https://user@example.com",
		"example.com",
	} {
		_, err := instance.FromEnv(environment(map[string]string{
			"RASTRILLO_ROOT":      root,
			"RASTRILLO_ROOT_KEYS": keys,
		}))
		want := "rastrillo/instance: RASTRILLO_ROOT must be an HTTPS " +
			"origin with no path, for example https://example.com"
		if err == nil || err.Error() != want {
			t.Errorf("root %q: error = %v, want %q", root, err, want)
		}
	}
}

func TestFromEnvRefusesKeysItCannotParse(t *testing.T) {
	_, err := instance.FromEnv(environment(map[string]string{
		"RASTRILLO_ROOT":      "https://example.com",
		"RASTRILLO_ROOT_KEYS": "not-a-key",
	}))
	if err == nil {
		t.Fatal("want an error")
	}
	_, parseErr := sessions.ParsePublicKeys("not-a-key")
	if !errors.Is(err, parseErr) {
		t.Errorf("error %q does not wrap the parser's %q", err, parseErr)
	}
	if strings.Contains(err.Error(), "not-a-key") {
		t.Errorf("error %q repeats the submitted keys", err)
	}
}
