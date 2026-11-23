package roster_test

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/roster"
	stdjson "encoding/json"
	json "github.com/go-json-experiment/json"
)

type goldenVector struct {
	PublicKey string          `json:"publicKey"`
	Now       int64           `json:"now"`
	Snapshot  roster.Snapshot `json:"snapshot"`
	Token     string          `json:"token"`
}

func loadGolden(t *testing.T) goldenVector {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector goldenVector
	if err := stdjson.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	return vector
}

func TestSnapshotWithoutGrantsSignsTheSameBytes(t *testing.T) {
	vector := loadGolden(t)
	want, _, ok := strings.Cut(vector.Token, ".")
	if !ok {
		t.Fatal("golden token has no signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(vector.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("signed bytes changed:\n got %s\nwant %s", got, payload)
	}
}

// A root may hold an empty, non-nil Grants for a grantless member, and
// it must still sign the bytes v0.46.0 signs.
func TestSnapshotWithEmptyGrantsSignsTheSameBytes(t *testing.T) {
	vector := loadGolden(t)
	snapshot := vector.Snapshot
	for index := range snapshot.Members {
		snapshot.Members[index].Grants = []string{}
	}
	want, _, _ := strings.Cut(vector.Token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("signed bytes changed:\n got %s\nwant %s", got, payload)
	}
}

// v0.46.0's types, frozen. A site on v0.46.0 refuses a snapshot with
// grants, and plaza never retries a refused one, so sites must run
// v0.47.0 before plaza puts grant IDs in a snapshot.
type frozenMember struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

type frozenSnapshot struct {
	Version   int64          `json:"v"`
	Issuer    string         `json:"iss"`
	Audience  string         `json:"aud"`
	KeyID     string         `json:"kid"`
	Sequence  int64          `json:"seq"`
	IssuedAt  int64          `json:"iat"`
	ExpiresAt int64          `json:"exp"`
	State     string         `json:"state"`
	Members   []frozenMember `json:"members"`
	Origin    string         `json:"origin,omitempty"`
}

func TestOldReceiverRefusesGrants(t *testing.T) {
	snapshot := loadGolden(t).Snapshot
	plain, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Members[0].Grants = []string{"g1"}
	granted, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var old frozenSnapshot
	strict := json.RejectUnknownMembers(true)
	if err := json.Unmarshal(plain, &old, strict); err != nil {
		t.Errorf("v0.46.0 refuses a snapshot without grants: %v", err)
	}
	if err := json.Unmarshal(granted, &old, strict); err == nil {
		t.Error("v0.46.0 accepts a snapshot with grants")
	}
}

func TestNewReceiverAcceptsOldSnapshot(t *testing.T) {
	vector := loadGolden(t)
	public, err := base64.RawURLEncoding.DecodeString(vector.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := vector.Snapshot
	verifier, err := roster.NewVerifier(snapshot.Issuer, snapshot.Audience,
		map[string][]byte{snapshot.KeyID: public})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := roster.New(roster.Config{
		Verifier: verifier, Store: newFakeStore(), DB: schema.Open(t),
		Roles: []string{"owner", "member"},
		Now:   func() time.Time { return time.Unix(vector.Now, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	fix := &fixture{}
	if got := fix.postTo(receiver, vector.Token); got != 204 {
		t.Fatalf("status = %d, want 204", got)
	}
	for _, member := range snapshot.Members {
		active, err := receiver.GrantActive(
			context.Background(), member.Subject, "g1")
		if err != nil || active {
			t.Errorf("%s: active = %v, err = %v", member.Subject, active, err)
		}
	}
}
