package wire_test

import (
	"bytes"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
)

func TestSplitUndoesJoin(t *testing.T) {
	payload := []byte(`{"v":1}`)
	signature := bytes.Repeat([]byte{7}, 64)
	gotPayload, gotSignature, ok := wire.Split(
		wire.Join(payload, signature), 1024)
	if !ok || !bytes.Equal(gotPayload, payload) ||
		!bytes.Equal(gotSignature, signature) {
		t.Fatalf("Split(Join) = %q, %x, %v", gotPayload, gotSignature, ok)
	}
}

func TestSplitRefusesEverySecondSpelling(t *testing.T) {
	good := wire.Join([]byte(`{"v":1}`), bytes.Repeat([]byte{7}, 64))
	payload, signature, found := strings.Cut(good, ".")
	if !found || len(signature) != 86 {
		t.Fatalf("Join wrote %q, want payload.signature", good)
	}
	cases := map[string]string{
		"no dot":            payload + signature,
		"two dots":          good + ".",
		"padded payload":    payload + "=." + signature,
		"padded signature":  good + "=",
		"short signature":   payload + "." + signature[:85],
		"standard alphabet": payload + "." + "+" + signature[1:],
		"empty payload":     "." + signature,
		"over the limit":    good,
	}
	for name, token := range cases {
		limit := 1024
		if name == "over the limit" {
			limit = len(good) - 1
		}
		if _, _, ok := wire.Split(token, limit); ok {
			t.Errorf("%s: Split accepted %q", name, token)
		}
	}
}

func TestSplitRefusesASignatureThatIsNot64Bytes(t *testing.T) {
	// 86 characters decode to 64 bytes only; 65 bytes take 87.
	token := wire.Join([]byte(`{}`), bytes.Repeat([]byte{7}, 65))
	if _, _, ok := wire.Split(token, 1024); ok {
		t.Fatal("Split accepted a 65-byte signature")
	}
}

func TestTrustSetCopiesValidKeysAndRefusesTheRest(t *testing.T) {
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	public := key.SignPub()
	keys := map[string][]byte{"key_1": public}
	trusted, ok := wire.TrustSet(keys)
	if !ok || !bytes.Equal(trusted["key_1"], public) {
		t.Fatalf("TrustSet = %v, %v", trusted, ok)
	}
	public[1] ^= 0xff
	if bytes.Equal(trusted["key_1"], public) {
		t.Error("TrustSet kept the caller's slice, not a copy")
	}
	nine := map[string][]byte{}
	for _, keyID := range strings.Split("a b c d e f g h i", " ") {
		nine[keyID] = key.SignPub()
	}
	refused := map[string]map[string][]byte{
		"none":          {},
		"nine":          nine,
		"bad key id":    {"key 1": key.SignPub()},
		"short key":     {"key_1": {4, 1, 2}},
		"off the curve": {"key_1": append([]byte{4}, make([]byte, 64)...)},
	}
	for name, set := range refused {
		if _, ok := wire.TrustSet(set); ok {
			t.Errorf("%s: TrustSet accepted it", name)
		}
	}
}
