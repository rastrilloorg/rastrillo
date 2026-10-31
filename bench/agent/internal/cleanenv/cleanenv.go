// Package cleanenv gives each run a Claude Code config and an
// environment that carry nothing of the operator's. The 2026-10-30 runs
// used the operator's own config and loaded eight plugins, a personal
// CLAUDE.md and an output style, which nobody else could reproduce.
package cleanenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Config writes dir/settings.json: the committed settings with
// extraHosts appended to the sandbox's allowed domains, and the agent's
// file tools denied each of deniedFiles (the token, wherever it lives).
func Config(benchRoot, dir string, extraHosts, deniedFiles []string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(benchRoot, "settings.json"))
	if err != nil {
		return "", err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	network := settings["sandbox"].(map[string]any)["network"].(map[string]any)
	domains, _ := network["allowedDomains"].([]any)
	for _, host := range extraHosts {
		domains = append(domains, host)
	}
	network["allowedDomains"] = domains
	permissions := settings["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	for _, file := range deniedFiles {
		absolute, err := filepath.Abs(file)
		if err != nil {
			return "", err
		}
		// A rule is a gitignore pattern, so these would make it match
		// some other name and leave this file readable.
		if strings.ContainsAny(absolute, "[]*?{}!\\") {
			return "", fmt.Errorf("%s: the agent cannot be denied a path "+
				"with any of [ ] * ? { } ! \\ in it; move the token", file)
		}
		// Claude Code reads a rule path starting // as absolute.
		deny = append(deny, "Read(/"+absolute+")", "Edit(/"+absolute+")")
	}
	permissions["deny"] = deny

	written, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "settings.json")
	return path, os.WriteFile(path, written, 0o600)
}

// allowed are the variables a shell needs to work at all. Everything
// else the operator has set (API keys, GOFLAGS, their own
// CLAUDE_CONFIG_DIR) would be a difference between this machine and
// anyone else's.
var allowed = []string{
	"HOME", "USER", "LOGNAME", "PATH", "LANG", "LC_ALL", "TERM",
	"TMPDIR", "SHELL", "CLAUDE_CODE_OAUTH_TOKEN",
}

const tokenVariable = "CLAUDE_CODE_OAUTH_TOKEN"

// Env is the child's whole environment.
func Env(
	parent []string, configDir string, extra map[string]string,
) ([]string, error) {
	var env []string
	hasToken := false
	for _, entry := range parent {
		name, value, _ := strings.Cut(entry, "=")
		if !slices.Contains(allowed, name) {
			continue
		}
		if name == tokenVariable {
			if value == "" {
				continue
			}
			hasToken = true
		}
		env = append(env, entry)
	}
	if extra[tokenVariable] != "" {
		hasToken = true
	}
	if !hasToken {
		return nil, errors.New(tokenVariable +
			" is not set, and there is no token file: create one with " +
			"`claude setup-token`")
	}
	env = append(env, "CLAUDE_CONFIG_DIR="+configDir)
	for name, value := range extra {
		env = append(env, name+"="+value)
	}
	return env, nil
}

// ReadTokenFile reads a token saved by `claude setup-token`, and refuses
// one that other users can read: this is a shared machine, and the
// token signs in as its owner.
func ReadTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is readable by other users (%s): chmod 600 it",
			path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}
