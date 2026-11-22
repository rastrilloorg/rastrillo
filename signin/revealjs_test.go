package signin

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

// The switch shows its row and starts off, shows the passphrase while
// on, and hides it again when off and as the form is sent.
func TestRevealJSShowsThePassphraseOnlyWhileOn(t *testing.T) {
	var got struct {
		Wired struct {
			Type      string `json:"type"`
			Checked   bool   `json:"checked"`
			RowHidden bool   `json:"rowHidden"`
		} `json:"wired"`
		On, Off, Sent string
	}
	out := nodetest.Run(t, nodetest.Cmd{Args: []string{"js/reveal_node.mjs"}})
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Wired.Type != "password" || got.Wired.Checked ||
		got.Wired.RowHidden {
		t.Errorf("wired: %+v; want a password field, off, row shown",
			got.Wired)
	}
	if got.On != "text" || got.Off != "password" || got.Sent != "password" {
		t.Errorf("on %q, off %q, sent %q; want text, password, password",
			got.On, got.Off, got.Sent)
	}
}

func TestRevealJSIsTheModuleAndSelfContained(t *testing.T) {
	file, err := os.ReadFile("js/reveal.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(RevealJS(), file) {
		t.Fatal("RevealJS() is not js/reveal.mjs")
	}
	for _, bad := range []string{"http://", "https://", "eval(",
		"new Function", "innerHTML", "import "} {
		if bytes.Contains(file, []byte(bad)) {
			t.Errorf("reveal.mjs contains %q", bad)
		}
	}
}
