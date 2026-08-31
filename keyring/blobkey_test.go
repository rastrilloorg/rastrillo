package keyring

import (
	"bytes"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// TestBlobKeyDerivation pins the context-string format: one sealing
// key per named vault blob, Derive(seed, ns+"/blob/"+name+"/v1").
func TestBlobKeyDerivation(t *testing.T) {
	r := Ring{Namespace: "kass"}
	seed := bytes.Repeat([]byte{7}, 32)

	got := r.BlobKey(seed, "servers")
	want := crypto.Derive(seed, "kass/blob/servers/v1")
	if !bytes.Equal(got, want) {
		t.Fatalf("BlobKey(servers) = %x, want Derive(seed, kass/blob/servers/v1) = %x", got, want)
	}
}

// TestBlobKeyIsolation: two names never share a key, and no blob key
// collides with the ring's content key.
func TestBlobKeyIsolation(t *testing.T) {
	r := Ring{Namespace: "kass"}
	seed := bytes.Repeat([]byte{7}, 32)

	if bytes.Equal(r.BlobKey(seed, "servers"), r.BlobKey(seed, "drafts")) {
		t.Fatal("two blob names derived the same key")
	}
	if bytes.Equal(r.BlobKey(seed, "servers"), r.ContentKey(seed)) {
		t.Fatal("a blob key collided with the content key")
	}
}
