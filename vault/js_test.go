package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// TestJSEmbedded pins the embed wiring, keyring's pattern: the twin
// travels with the binary, sibling import intact.
func TestJSEmbedded(t *testing.T) {
	js := JS()
	if len(js) == 0 {
		t.Fatal("JS() returned no bytes; embed broken")
	}
	if !bytes.Contains(js, []byte("export async function restoreRequest(")) {
		t.Fatal("JS() does not look like the vault twin")
	}
	if !bytes.Contains(js, []byte(`from "./crypto.mjs"`)) {
		t.Fatal("JS() lost its ./crypto.mjs sibling import — the serve-time contract")
	}
}

// TestJSTwin materialises the serve-time layout in a temp dir, writes
// a GO-sealed restore-return fixture, and runs the twin's tests under
// `node --test` — so the cross-language claim is a Go artifact opened
// by JS, not two monologues. Skipped without node (crypto's rule).
func TestJSTwin(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS twin not exercised")
	}

	dir := t.TempDir()
	jsDir := filepath.Join(dir, "js")
	tdDir := filepath.Join(dir, "testdata")
	for _, d := range []string{jsDir, tdDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	write := func(dst string, src []byte) {
		t.Helper()
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
	}
	b, err := os.ReadFile(filepath.Join("js", "handoff.test.mjs"))
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	write(filepath.Join(jsDir, "crypto.mjs"), crypto.JS())
	write(filepath.Join(jsDir, "vault.mjs"), JS())
	write(filepath.Join(jsDir, "handoff.test.mjs"), b)

	// The Go-sealed fixture: a restore return this process seals to a
	// keypair the JS side imports.
	kp, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	marshaled, err := crypto.MarshalKeypair(kp)
	if err != nil {
		t.Fatalf("MarshalKeypair: %v", err)
	}
	const nonce, token = "fixture-nonce", "fixture-token"
	sealed, err := crypto.Seal(kp.BoxPub(), restoreContext,
		[]byte(`{"token":"`+token+`","nonce":"`+nonce+`"}`))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	fx, err := json.Marshal(map[string]string{
		"keypair":  string(marshaled),
		"sign_pub": base64.StdEncoding.EncodeToString(kp.SignPub()),
		"box_pub":  base64.StdEncoding.EncodeToString(kp.BoxPub()),
		"nonce":    nonce,
		"token":    token,
		"sealed":   base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	write(filepath.Join(tdDir, "handoff.json"), fx)

	cmd := exec.Command(node, "--test", "handoff.test.mjs")
	cmd.Dir = jsDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test handoff.test.mjs failed: %v\n%s", err, out)
	}
}
