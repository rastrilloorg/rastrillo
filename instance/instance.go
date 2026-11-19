// Package instance decides whether an app runs under a root app or on
// its own, so a misconfigured instance fails at startup instead of
// trusting nobody or everybody by accident.
package instance

import (
	"errors"
	"fmt"
	"net/url"

	"amadan.net/rastrillo/rastrillo/sessions"
)

type Mode int

const (
	Standalone Mode = iota + 1
	Rooted
)

// Setup is what an instance knows about its root. Root and Keys are
// empty when Standalone.
type Setup struct {
	Mode Mode
	Root string            // root origin, for example https://example.com
	Keys map[string][]byte // root public keys by kid
}

var (
	errNeither = errors.New("rastrillo/instance: set RASTRILLO_ROOT " +
		"and RASTRILLO_ROOT_KEYS to use a root app, or set " +
		"RASTRILLO_STANDALONE=1 to run on its own")
	errBoth = errors.New("rastrillo/instance: RASTRILLO_STANDALONE=1 " +
		"and RASTRILLO_ROOT are both set; set only one")
	errRootWithoutKeys = errors.New("rastrillo/instance: " +
		"RASTRILLO_ROOT is set but RASTRILLO_ROOT_KEYS is empty; " +
		"set the root app's public keys")
	errKeysWithoutRoot = errors.New("rastrillo/instance: " +
		"RASTRILLO_ROOT_KEYS is set but RASTRILLO_ROOT is empty; " +
		"set the root app's origin")
	errRootOrigin = errors.New("rastrillo/instance: RASTRILLO_ROOT " +
		"must be an HTTPS origin with no path, for example " +
		"https://example.com")
)

// FromEnv reads the instance's mode from the environment. Exactly one
// mode must be chosen.
func FromEnv(getenv func(string) string) (Setup, error) {
	standalone := getenv("RASTRILLO_STANDALONE") == "1"
	root := getenv("RASTRILLO_ROOT")
	keys := getenv("RASTRILLO_ROOT_KEYS")
	switch {
	case standalone && root != "":
		return Setup{}, errBoth
	case standalone:
		return Setup{Mode: Standalone}, nil
	case root == "" && keys == "":
		return Setup{}, errNeither
	case root == "":
		return Setup{}, errKeysWithoutRoot
	case keys == "":
		return Setup{}, errRootWithoutKeys
	case !isHTTPSOrigin(root):
		return Setup{}, errRootOrigin
	}
	parsed, err := sessions.ParsePublicKeys(keys)
	if err != nil {
		return Setup{}, fmt.Errorf(
			"rastrillo/instance: RASTRILLO_ROOT_KEYS: %w", err)
	}
	return Setup{Mode: Rooted, Root: root, Keys: parsed}, nil
}

// isHTTPSOrigin rejects a trailing slash too, because the origin is
// compared to request origins as a plain string.
func isHTTPSOrigin(root string) bool {
	parsed, err := url.Parse(root)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Host != "" &&
		parsed.User == nil && parsed.Path == "" &&
		parsed.RawQuery == "" && !parsed.ForceQuery &&
		parsed.Fragment == ""
}
