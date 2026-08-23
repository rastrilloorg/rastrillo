package keyring

import (
	"bytes"
	"testing"
)

// TestJSEmbedded pins the embed wiring: the twin travels with the
// binary, and it is the keyring module — sibling import intact — not
// an accidental file.
func TestJSEmbedded(t *testing.T) {
	js := JS()
	if len(js) == 0 {
		t.Fatal("JS() returned no bytes; embed broken")
	}
	if !bytes.Contains(js, []byte("export function ring(")) {
		t.Fatal("JS() does not look like the keyring twin")
	}
	if !bytes.Contains(js, []byte(`from "./crypto.mjs"`)) {
		t.Fatal("JS() lost its ./crypto.mjs sibling import — the serve-time contract")
	}
}
