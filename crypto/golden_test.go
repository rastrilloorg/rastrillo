package crypto

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenFile is the shape of testdata/golden.json — amadan's pinned
// cross-implementation fixture (fixed keypairs, contexts, plaintexts and
// their expected outputs). This file is byte-for-byte amadan's
// internal/envelope/testdata/golden.json; passing it is what licenses
// amadan (and keymail, and seapointish) to delete their local copies and
// import this package instead.
type goldenFile struct {
	Envelope struct {
		Keypair json.RawMessage `json:"keypair"`
		SignPub string          `json:"sign_pub"`
		BoxPub  string          `json:"box_pub"`
		Seal    struct {
			Context   string `json:"context"`
			Plaintext string `json:"plaintext"`
			Sealed    string `json:"sealed"`
		} `json:"seal"`
		Sign struct {
			Context   string `json:"context"`
			Message   string `json:"message"`
			Signature string `json:"signature"`
		} `json:"sign"`
	} `json:"envelope"`
	Repokey struct {
		Key    string `json:"key"`
		Derive struct {
			Pack     string `json:"pack"`
			Manifest string `json:"manifest"`
			Browse   string `json:"browse"`
			CI       string `json:"ci"`
		} `json:"derive"`
		SealSym struct {
			Plaintext string `json:"plaintext"`
			Sealed    string `json:"sealed"`
		} `json:"sealsym"`
	} `json:"repokey"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatalf("read testdata/golden.json: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("unmarshal testdata/golden.json: %v", err)
	}
	return g
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode %q: %v", s, err)
	}
	return b
}

// TestGoldenVectors replays amadan's pinned fixture against this
// implementation — the compatibility contract the crypto prompt names.
func TestGoldenVectors(t *testing.T) {
	g := loadGolden(t)

	t.Run("keypair unmarshal matches recorded public keys", func(t *testing.T) {
		kp, err := UnmarshalKeypair(g.Envelope.Keypair)
		if err != nil {
			t.Fatalf("UnmarshalKeypair: %v", err)
		}
		if !bytes.Equal(kp.SignPub(), mustB64(t, g.Envelope.SignPub)) {
			t.Fatal("SignPub derived from pinned keypair JSON does not match pinned sign_pub")
		}
		if !bytes.Equal(kp.BoxPub(), mustB64(t, g.Envelope.BoxPub)) {
			t.Fatal("BoxPub derived from pinned keypair JSON does not match pinned box_pub")
		}
	})

	t.Run("Open replays the pinned sealed blob", func(t *testing.T) {
		kp, err := UnmarshalKeypair(g.Envelope.Keypair)
		if err != nil {
			t.Fatalf("UnmarshalKeypair: %v", err)
		}
		wantPlain := mustB64(t, g.Envelope.Seal.Plaintext)
		sealed := mustB64(t, g.Envelope.Seal.Sealed)

		got, err := Open(kp, g.Envelope.Seal.Context, sealed)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if !bytes.Equal(got, wantPlain) {
			t.Fatalf("Open(pinned sealed) = %q, want %q", got, wantPlain)
		}
	})

	t.Run("Verify replays the pinned signature", func(t *testing.T) {
		msg := mustB64(t, g.Envelope.Sign.Message)
		sig := mustB64(t, g.Envelope.Sign.Signature)
		signPub := mustB64(t, g.Envelope.SignPub)

		if !Verify(signPub, g.Envelope.Sign.Context, msg, sig) {
			t.Fatal("Verify rejected the pinned golden signature")
		}
	})

	t.Run("Derive reproduces the pinned subkeys", func(t *testing.T) {
		key := mustB64(t, g.Repokey.Key)

		cases := []struct {
			ctx  string
			want string
		}{
			{"amadan-pack-v1", g.Repokey.Derive.Pack},
			{"amadan-manifest-v1", g.Repokey.Derive.Manifest},
			{"amadan-browse-v1", g.Repokey.Derive.Browse},
			{"amadan-ci-v1", g.Repokey.Derive.CI},
		}
		for _, c := range cases {
			got := Derive(key, c.ctx)
			want := mustB64(t, c.want)
			if !bytes.Equal(got, want) {
				t.Errorf("Derive(key, %q) = %x, want %x", c.ctx, got, want)
			}
		}
	})

	t.Run("OpenSym replays the pinned sealed blob", func(t *testing.T) {
		key := mustB64(t, g.Repokey.Key)
		wantPlain := mustB64(t, g.Repokey.SealSym.Plaintext)
		sealed := mustB64(t, g.Repokey.SealSym.Sealed)

		got, err := OpenSym(key, sealed)
		if err != nil {
			t.Fatalf("OpenSym: %v", err)
		}
		if !bytes.Equal(got, wantPlain) {
			t.Fatalf("OpenSym(pinned sealed) = %q, want %q", got, wantPlain)
		}
	})
}
