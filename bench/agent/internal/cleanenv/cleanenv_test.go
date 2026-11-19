package cleanenv

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestEnvKeepsOnlyTheAllowlist(t *testing.T) {
	parent := []string{
		"AIKIDO_API_KEY=x", "GOFLAGS=-mod=mod", "HOME=/h",
		"CLAUDE_CODE_OAUTH_TOKEN=t", "CLAUDE_CONFIG_DIR=/operator/.claude",
		"PATH=/bin",
	}

	env, err := Env(parent, "/run/config", map[string]string{"GOPROXY": "file:///p"})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"HOME=/h", "PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=t",
		"CLAUDE_CONFIG_DIR=/run/config", "GOPROXY=file:///p",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s: %v", want, env)
		}
	}
	for _, leaked := range []string{"AIKIDO_API_KEY", "GOFLAGS", "CLAUDE_CONFIG_DIR=/operator"} {
		for _, entry := range env {
			if strings.HasPrefix(entry, leaked) {
				t.Errorf("env carries the operator's %s", entry)
			}
		}
	}
}

func TestEnvRefusesWithoutToken(t *testing.T) {
	for _, parent := range [][]string{
		{"HOME=/h"}, {"HOME=/h", "CLAUDE_CODE_OAUTH_TOKEN="},
	} {
		if _, err := Env(parent, "/c", nil); err == nil {
			t.Errorf("Env(%v) gave no error without a token", parent)
		}
	}
}

func TestConfigAddsHostsAndNoPlugins(t *testing.T) {
	dir := t.TempDir()

	path, err := Config("../..", dir, []string{"*.fly.dev", "100.1.2.3"},
		[]string{"/secrets/elsewhere/token"})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"enabledPlugins", "outputStyle", "hooks", "env"} {
		if _, present := settings[absent]; present {
			t.Errorf("clean settings carry %q", absent)
		}
	}
	if settings["disableAllHooks"] != true {
		t.Error("hooks are not disabled")
	}
	// File tools run outside the Bash sandbox, so the operator's own
	// config and keys are denied to them by permission rules.
	deny, _ := settings["permissions"].(map[string]any)["deny"].([]any)
	for _, want := range []string{"Read(~/.claude/**)", "Read(~/.ssh/**)",
		"Read(~/.config/agent-bench/**)", "Read(//secrets/elsewhere/token)",
		"Edit(//secrets/elsewhere/token)"} {
		if !slices.Contains(deny, any(want)) {
			t.Errorf("permissions.deny lacks %s: %v", want, deny)
		}
	}
	sandbox := settings["sandbox"].(map[string]any)
	if sandbox["enabled"] != true {
		t.Error("the sandbox is off")
	}
	domains := sandbox["network"].(map[string]any)["allowedDomains"].([]any)
	for _, want := range []string{"*.fly.dev", "100.1.2.3", "proxy.golang.org", "*.oncarlos.com"} {
		if !slices.Contains(domains, any(want)) {
			t.Errorf("allowedDomains lacks %s", want)
		}
	}
}

func TestReadTokenFileRefusesOneOthersCanRead(t *testing.T) {
	path := t.TempDir() + "/token"
	os.WriteFile(path, []byte("sk-ant-oat01-abc\n"), 0o644)

	if _, err := ReadTokenFile(path); err == nil {
		t.Error("a world-readable token file was accepted")
	}
	os.Chmod(path, 0o600)
	token, err := ReadTokenFile(path)
	if err != nil || token != "sk-ant-oat01-abc" {
		t.Errorf("ReadTokenFile = %q, %v; want the trimmed token", token, err)
	}
}

func TestEnvTakesTheTokenFromExtra(t *testing.T) {
	env, err := Env([]string{"HOME=/h"}, "/c",
		map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "from-file"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=from-file") {
		t.Errorf("env = %v", env)
	}
}

// A deny rule is a gitignore pattern: a token at token[1] would be
// written as a rule matching token1, and left readable.
func TestConfigRefusesATokenPathItCannotDenyLiterally(t *testing.T) {
	for _, path := range []string{"/s/token[1]", "/s/to*ken", "/s/t?k", "/s/{a}", "/s/!x"} {
		if _, err := Config("../..", t.TempDir(), nil, []string{path}); err == nil {
			t.Errorf("Config accepted %s, which no literal rule can deny", path)
		}
	}
}
