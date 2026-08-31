package keyring

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// goldenFile is the shape of testdata/golden.json — this package's
// pinned cross-implementation fixture, shared with the generator
// (golden_gen_test.go) so writer and reader cannot drift. Wrap and
// Grant mint random IVs and ephemeral keys, so their outputs are not
// vectorable; the file pins the deterministic directions — purpose
// derivation, UnwrapSeed of a pinned blob, OpenGrant of a pinned
// grant, the ns="kass" strings verbatim — crypto's own golden pattern.
type goldenFile struct {
	Provenance string `json:"_provenance"`
	Namespace  string `json:"namespace"`
	Strings    struct {
		PRFSalt string `json:"prf_salt"`
		Content string `json:"content_context"`
		Wrap    string `json:"wrap_context"`
		Grant   string `json:"grant_context"`
	} `json:"strings"`
	Seed        string `json:"seed"`
	ContentKey  string `json:"content_key"`
	PRF         string `json:"prf"`
	WrapKey     string `json:"wrap_key"`
	WrappedSeed string `json:"wrapped_seed"`
	Member      struct {
		BoxPrivD     string `json:"box_priv_d"`
		BoxPrivPKCS8 string `json:"box_priv_pkcs8"`
		BoxPub       string `json:"box_pub"`
	} `json:"member"`
	Grant string `json:"grant"`
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

// TestGoldenVectors replays the pinned fixture against this
// implementation — the compatibility contract that lets kass adopt
// Ring{"kass"} without a data migration.
func TestGoldenVectors(t *testing.T) {
	g := loadGolden(t)
	r := Ring{Namespace: g.Namespace}

	t.Run("kass strings verbatim", func(t *testing.T) {
		if g.Strings.PRFSalt != "kass/prf/v1" || g.Strings.Content != "kass/content/v1" ||
			g.Strings.Wrap != "kass/wrap/v1" || g.Strings.Grant != "kass/grant/v1" {
			t.Fatalf("pinned strings drifted: %+v", g.Strings)
		}
		if got := r.PRFSalt(); got != g.Strings.PRFSalt {
			t.Fatalf("PRFSalt() = %q, want pinned %q", got, g.Strings.PRFSalt)
		}
	})

	t.Run("purpose derivation reproduces the pinned keys", func(t *testing.T) {
		seed := mustB64(t, g.Seed)
		if !bytes.Equal(r.ContentKey(seed), mustB64(t, g.ContentKey)) {
			t.Fatal("ContentKey(seed) does not match pinned content_key")
		}
		if !bytes.Equal(crypto.Derive(seed, g.Strings.Content), mustB64(t, g.ContentKey)) {
			t.Fatal("pinned content_context does not derive the pinned content_key — the context string drifted")
		}
		prf := mustB64(t, g.PRF)
		if !bytes.Equal(r.WrapKey(prf), mustB64(t, g.WrapKey)) {
			t.Fatal("WrapKey(prf) does not match pinned wrap_key")
		}
		if !bytes.Equal(crypto.Derive(prf, g.Strings.Wrap), mustB64(t, g.WrapKey)) {
			t.Fatal("pinned wrap_context does not derive the pinned wrap_key — the context string drifted")
		}
	})

	t.Run("UnwrapSeed replays the pinned wrapped blob", func(t *testing.T) {
		got, err := r.UnwrapSeed(mustB64(t, g.PRF), mustB64(t, g.WrappedSeed))
		if err != nil {
			t.Fatalf("UnwrapSeed: %v", err)
		}
		if !bytes.Equal(got, mustB64(t, g.Seed)) {
			t.Fatal("UnwrapSeed(pinned) != pinned seed")
		}
	})

	t.Run("OpenGrant replays the pinned grant", func(t *testing.T) {
		priv, err := hex.DecodeString(g.Member.BoxPrivD)
		if err != nil {
			t.Fatalf("decode box_priv_d: %v", err)
		}
		got, err := r.OpenGrant(priv, mustB64(t, g.Grant))
		if err != nil {
			t.Fatalf("OpenGrant: %v", err)
		}
		if !bytes.Equal(got, mustB64(t, g.ContentKey)) {
			t.Fatal("OpenGrant(pinned) != pinned content_key")
		}
	})

	t.Run("the grant context string is pinned through crypto.Open", func(t *testing.T) {
		raw, err := hex.DecodeString(g.Member.BoxPrivD)
		if err != nil {
			t.Fatalf("decode box_priv_d: %v", err)
		}
		boxPriv, err := ecdh.P256().NewPrivateKey(raw)
		if err != nil {
			t.Fatalf("reconstruct box key: %v", err)
		}
		got, err := crypto.Open(&crypto.Keypair{BoxPriv: boxPriv}, g.Strings.Grant, mustB64(t, g.Grant))
		if err != nil {
			t.Fatalf("crypto.Open under pinned grant_context: %v", err)
		}
		if !bytes.Equal(got, mustB64(t, g.ContentKey)) {
			t.Fatal("crypto.Open under pinned grant_context != pinned content_key")
		}
	})

	t.Run("round trips for the randomised directions", func(t *testing.T) {
		seed, err := NewSeed()
		if err != nil {
			t.Fatalf("NewSeed: %v", err)
		}
		prf, err := crypto.NewKey()
		if err != nil {
			t.Fatalf("NewKey: %v", err)
		}
		wrapped, err := r.WrapSeed(prf, seed)
		if err != nil {
			t.Fatalf("WrapSeed: %v", err)
		}
		gotSeed, err := r.UnwrapSeed(prf, wrapped)
		if err != nil {
			t.Fatalf("UnwrapSeed: %v", err)
		}
		if !bytes.Equal(gotSeed, seed) {
			t.Fatal("WrapSeed/UnwrapSeed round trip lost the seed")
		}

		member, err := crypto.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		key, err := crypto.NewKey()
		if err != nil {
			t.Fatalf("NewKey: %v", err)
		}
		sealed, err := r.Grant(member.BoxPub(), key)
		if err != nil {
			t.Fatalf("Grant: %v", err)
		}
		gotKey, err := r.OpenGrant(member.BoxPriv.Bytes(), sealed)
		if err != nil {
			t.Fatalf("OpenGrant: %v", err)
		}
		if !bytes.Equal(gotKey, key) {
			t.Fatal("Grant/OpenGrant round trip lost the content key")
		}
	})
}

// TestVectorsFileUntouched pins the vectors file byte-for-byte:
// "fix implementations, not vectors" — grant bytes are wire-format-
// forever, so no change to this package may rewrite the spec it is
// tested against (eventlog's TestMergeVectorsFileUntouched pattern).
func TestVectorsFileUntouched(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	const want = "008df2f8291ad0e248dac69d48f4f6544905539cad910bdbfc549ff167180b62"
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
		t.Fatalf("golden.json changed (sha256 %s, want %s); fix implementations, not vectors", got, want)
	}
}
