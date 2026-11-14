package password

import (
	"strings"
	"testing"
)

// decoyHash is a constant, so nothing moves it when the parameters do:
// an unknown email would then verify faster than a known one.
func TestDecoyHashMatchesTheParameters(t *testing.T) {
	fresh, err := Hash("anything")
	if err != nil {
		t.Fatal(err)
	}
	params := func(encoded string) string {
		return strings.Join(strings.SplitN(encoded, "$", 4)[:3], "$")
	}
	if got, want := params(decoyHash), params(fresh); got != want {
		t.Fatalf("decoyHash is %s but Hash now makes %s: "+
			"replace it with Hash of a random string nobody keeps",
			got, want)
	}
}
