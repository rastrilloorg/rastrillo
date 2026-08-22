package crypto

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

// inviteVectors is testdata/invites.json: eleven's own pinned invite
// vectors, replayed here as the cross-implementation contract the
// package doc promises ("they land when a consumer pins their
// contract" — eleven did).
type inviteVectors struct {
	Context string `json:"context"`
	Invites []struct {
		T               string `json:"t"`
		ID              string `json:"id"`
		ClaimSecret     string `json:"claimSecret"`
		ClaimHash       string `json:"claimHash"`
		GroupKey        string `json:"groupKey"`
		WrappedGroupKey string `json:"wrappedGroupKey"`
	} `json:"invites"`
}

func loadInviteVectors(t *testing.T) inviteVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/invites.json")
	if err != nil {
		t.Fatal(err)
	}
	var v inviteVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Invites) == 0 {
		t.Fatal("no invite vectors")
	}
	return v
}

func TestDeriveInviteReplaysVectors(t *testing.T) {
	v := loadInviteVectors(t)
	for i, vec := range v.Invites {
		inv, err := DeriveInvite(v.Context, vec.T)
		if err != nil {
			t.Fatalf("vector %d: DeriveInvite: %v", i, err)
		}
		if inv.ID != vec.ID {
			t.Errorf("vector %d: ID = %q, want %q", i, inv.ID, vec.ID)
		}
		if inv.ClaimSecret != vec.ClaimSecret {
			t.Errorf("vector %d: ClaimSecret = %q, want %q", i, inv.ClaimSecret, vec.ClaimSecret)
		}
		if inv.ClaimHash != vec.ClaimHash {
			t.Errorf("vector %d: ClaimHash = %q, want %q", i, inv.ClaimHash, vec.ClaimHash)
		}

		// The pinned wrapped blob (random IV, so only the unwrap side
		// is deterministic) opens under the derived WrapKey to the
		// pinned group key.
		got, err := UnwrapKey(inv.WrapKey, vec.WrappedGroupKey)
		if err != nil {
			t.Fatalf("vector %d: UnwrapKey: %v", i, err)
		}
		want, err := base64.RawURLEncoding.DecodeString(vec.GroupKey)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("vector %d: unwrapped key mismatch", i)
		}
	}
}

func TestWrapKeyRoundTrip(t *testing.T) {
	secret, err := NewInviteSecret()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := DeriveInvite("app-invite", secret)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapKey(inv.WrapKey, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapKey(inv.WrapKey, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, key) {
		t.Fatal("round trip lost the key")
	}

	// A different context derives a different WrapKey — the wrong one.
	other, _ := DeriveInvite("other-app", secret)
	if _, err := UnwrapKey(other.WrapKey, wrapped); err == nil {
		t.Fatal("unwrap under a foreign context's key succeeded")
	}
}

func TestDeriveInviteDomainSeparation(t *testing.T) {
	secret, _ := NewInviteSecret()
	a, _ := DeriveInvite("ctx", secret)
	b, _ := DeriveInvite("ctx", secret)
	if a.ID != b.ID || a.ClaimSecret != b.ClaimSecret {
		t.Fatal("DeriveInvite is not deterministic")
	}
	if a.ID == a.ClaimSecret || bytes.Equal(a.WrapKey, []byte(a.ClaimSecret)) {
		t.Fatal("derived values collide across purposes")
	}

	if _, err := DeriveInvite("ctx", "!!!not-base64url!!!"); err == nil {
		t.Fatal("garbage secret accepted")
	}
}
