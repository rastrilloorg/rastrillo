package carlos

import (
	"errors"
	"testing"
)

func TestInstanceKey(t *testing.T) {
	// Each case sets both variables, "" included, so a CARLOS_ variable
	// in the environment running the tests cannot decide the outcome.
	for _, tc := range []struct {
		name     string
		own      string
		platform string
		socket   string
		want     string
		err      error
	}{
		// The app's own key wins over the platform's: everything already
		// sealed under it would be unreadable after a switch.
		{"the app's own key wins on CARLOS", "own", "platform", "/run/carlos.sock", "own", nil},
		{"the app's own key wins off CARLOS", "own", "platform", "", "own", nil},
		// An agent that predates key delivery sets the socket and no key.
		// An app with its own key must keep starting there, so the
		// missing platform key is checked only after own.
		{"the app's own key on CARLOS without the platform's", "own", "", "/run/carlos.sock", "own", nil},
		{"the app's own key alone off CARLOS", "own", "", "", "own", nil},
		{"the platform's key when the app has none", "", "platform", "/run/carlos.sock", "platform", nil},
		// Set by hand on a laptop, it is still a key somebody chose.
		{"the platform's variable off CARLOS", "", "platform", "", "platform", nil},
		{"on CARLOS with no key refuses", "", "", "/run/carlos.sock", "", ErrNoInstanceKey},
		{"off CARLOS with no key is the app's to mint", "", "", "", "", ErrNotOnCarlos},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(instanceKeyEnv, tc.platform)
			t.Setenv(socketEnv, tc.socket)
			got, err := InstanceKey(tc.own)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("InstanceKey(%q) = %q, %v; want %q, %v", tc.own, got, err, tc.want, tc.err)
			}
		})
	}
}

// TestInstanceKeyVerbatim: the key is only ever derived through, so a
// helper that trimmed or decoded it would change every derived key
// without anything failing. What the platform wrote is what comes back.
func TestInstanceKeyVerbatim(t *testing.T) {
	const key = " q8Vb+0/x==\t"
	t.Setenv(instanceKeyEnv, key)
	t.Setenv(socketEnv, "/run/carlos.sock")
	if got, err := InstanceKey(""); got != key || err != nil {
		t.Fatalf("InstanceKey(\"\") = %q, %v; want %q, nil", got, err, key)
	}
}
