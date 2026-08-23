package keyring

import (
	"bytes"
	"testing"

	"github.com/carlosframework/rastrillo/crypto"
)

func TestGrantRoundTrip(t *testing.T) {
	r := Ring{Namespace: "kass"}
	member, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	contentKey, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	sealed, err := r.Grant(member.BoxPub(), contentKey)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	// crypto.Seal's envelope: ephPub(65) ‖ iv(12) ‖ ct(len + 16-byte tag).
	if want := 65 + 12 + len(contentKey) + 16; len(sealed) != want {
		t.Fatalf("grant length = %d, want %d", len(sealed), want)
	}

	got, err := r.OpenGrant(member.BoxPriv.Bytes(), sealed)
	if err != nil {
		t.Fatalf("OpenGrant: %v", err)
	}
	if !bytes.Equal(got, contentKey) {
		t.Fatal("OpenGrant round trip lost the content key")
	}
}

func TestOpenGrantWrongMember(t *testing.T) {
	r := Ring{Namespace: "kass"}
	member, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	intruder, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	contentKey, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	sealed, err := r.Grant(member.BoxPub(), contentKey)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if _, err := r.OpenGrant(intruder.BoxPriv.Bytes(), sealed); err == nil {
		t.Fatal("OpenGrant succeeded with the wrong member's key")
	}
}

func TestGrantIsNamespacedAndPinsItsContext(t *testing.T) {
	member, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	contentKey, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	sealed, err := Ring{Namespace: "kass"}.Grant(member.BoxPub(), contentKey)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}

	if _, err := (Ring{Namespace: "app"}).OpenGrant(member.BoxPriv.Bytes(), sealed); err == nil {
		t.Fatal("a kass grant opened under the app namespace")
	}

	// The context string is wire format: crypto.Open with the literal
	// string must open what Grant sealed.
	got, err := crypto.Open(member, "kass/grant/v1", sealed)
	if err != nil {
		t.Fatalf(`crypto.Open(member, "kass/grant/v1", sealed): %v`, err)
	}
	if !bytes.Equal(got, contentKey) {
		t.Fatal("crypto.Open under the literal grant context lost the content key")
	}
}

func TestOpenGrantRejectsAMalformedKey(t *testing.T) {
	r := Ring{Namespace: "kass"}
	if _, err := r.OpenGrant([]byte{1, 2, 3}, []byte("junk")); err == nil {
		t.Fatal("OpenGrant accepted a malformed private key")
	}
}
