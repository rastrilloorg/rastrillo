package roster

import (
	"encoding/base64"
	stdjson "encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
)

const (
	rootOrigin     = "https://root.example.com"
	instanceOrigin = "https://acme.example.com"
	issuedAt       = 1700000000
)

func goodSnapshot() Snapshot {
	return Snapshot{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		KeyID: "key_1", Sequence: 7, IssuedAt: issuedAt,
		ExpiresAt: issuedAt + 300, State: Active,
		Members: []Member{
			{Subject: "person_1", Email: "ada@example.com",
				Name: "Ada Lovelace", Role: "owner"},
			{Subject: "person_2", Role: "member"},
		},
	}
}

func setup(t *testing.T) (*crypto.Keypair, *Verifier) {
	t.Helper()
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(
		rootOrigin, instanceOrigin, map[string][]byte{"key_1": key.SignPub()},
	)
	if err != nil {
		t.Fatal(err)
	}
	return key, verifier
}

// rawToken signs a payload without Sign's checks, to reach refusals that
// Sign would stop first.
func rawToken(t *testing.T, key *crypto.Keypair, payload []byte) string {
	t.Helper()
	signature, err := crypto.Sign(key, signingContext, payload)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature)
}

func rawSnapshot(t *testing.T, key *crypto.Keypair, snapshot Snapshot) string {
	t.Helper()
	payload, err := stdjson.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return rawToken(t, key, payload)
}

func TestSignedSnapshotVerifies(t *testing.T) {
	key, verifier := setup(t)
	want := goodSnapshot()
	token, err := Sign(key, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(token, time.Unix(issuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAnEmptyRosterIsValid(t *testing.T) {
	key, verifier := setup(t)
	for _, members := range [][]Member{nil, {}} {
		snapshot := goodSnapshot()
		snapshot.Members = members
		token, err := Sign(key, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		got, err := verifier.Verify(token, time.Unix(issuedAt, 0))
		if err != nil || len(got.Members) != 0 || got.Members == nil {
			t.Fatalf("members %v, err %v", got.Members, err)
		}
	}
}

func TestVerifyRefuses(t *testing.T) {
	key, verifier := setup(t)
	other, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	with := func(change func(*Snapshot)) string {
		snapshot := goodSnapshot()
		change(&snapshot)
		return rawSnapshot(t, key, snapshot)
	}
	good, err := Sign(key, goodSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	payload, signature, _ := strings.Cut(good, ".")
	flipped := []byte(signature)
	if flipped[0] == 'A' {
		flipped[0] = 'B'
	} else {
		flipped[0] = 'A'
	}
	now := int64(issuedAt)
	rows := []struct {
		name  string
		token string
		now   int64
		want  error
	}{
		{"wrong issuer", with(func(snapshot *Snapshot) {
			snapshot.Issuer = "https://evil.example.com"
		}), now, ErrIssuer},
		{"wrong audience", with(func(snapshot *Snapshot) {
			snapshot.Audience = "https://other.example.com"
		}), now, ErrAudience},
		{"unknown key id", with(func(snapshot *Snapshot) {
			snapshot.KeyID = "key_2"
		}), now, ErrUnknownKey},
		{"flipped signature", payload + "." + string(flipped), now,
			ErrSignature},
		{"signed by another key", rawSnapshot(t, other, goodSnapshot()), now,
			ErrSignature},
		{"lifetime over 300 seconds", with(func(snapshot *Snapshot) {
			snapshot.ExpiresAt = snapshot.IssuedAt + 301
		}), now, ErrLifetime},
		{"lifetime of zero", with(func(snapshot *Snapshot) {
			snapshot.ExpiresAt = snapshot.IssuedAt
		}), now, ErrLifetime},
		{"issued in the future", good, now - 31, ErrLifetime},
		{"expired", good, now + 300, ErrExpired},
		{"version 2", with(func(snapshot *Snapshot) {
			snapshot.Version = 2
		}), now, ErrVersion},
		{"sequence zero", with(func(snapshot *Snapshot) {
			snapshot.Sequence = 0
		}), now, ErrClaims},
		{"unknown state", with(func(snapshot *Snapshot) {
			snapshot.State = "deleted"
		}), now, ErrClaims},
		{"duplicate subject", with(func(snapshot *Snapshot) {
			snapshot.Members[1].Subject = snapshot.Members[0].Subject
		}), now, ErrClaims},
		{"empty subject", with(func(snapshot *Snapshot) {
			snapshot.Members[0].Subject = ""
		}), now, ErrClaims},
		{"empty role", with(func(snapshot *Snapshot) {
			snapshot.Members[0].Role = ""
		}), now, ErrClaims},
		{"null members", rawToken(t, key, []byte(strings.Replace(
			string(mustPayload(t, goodSnapshot())),
			`"members":[`, `"members":null,"x":[`, 1))), now, ErrClaims},
		{"unknown field", rawToken(t, key, []byte(strings.Replace(
			string(mustPayload(t, goodSnapshot())),
			`"seq":7`, `"seq":7,"extra":1`, 1))), now, ErrClaims},
		{"unknown member field", rawToken(t, key, []byte(strings.Replace(
			string(mustPayload(t, goodSnapshot())),
			`"role":"owner"`, `"role":"owner","admin":true`, 1))), now,
			ErrClaims},
		{"duplicate field", rawToken(t, key, []byte(strings.Replace(
			string(mustPayload(t, goodSnapshot())),
			`"seq":7`, `"seq":7,"seq":8`, 1))), now, ErrMalformed},
		{"not an object", rawToken(t, key, []byte(`[]`)), now, ErrMalformed},
		{"no dot", "abc", now, ErrMalformed},
		{"two dots", good + ".x", now, ErrMalformed},
		{"short signature", payload + ".AAAA", now, ErrMalformed},
		{"token over 1 MiB", strings.Repeat("A", maxToken) + "." + signature,
			now, ErrMalformed},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, err := verifier.Verify(row.token, time.Unix(row.now, 0))
			if !errors.Is(err, row.want) {
				t.Fatalf("got %v, want %v", err, row.want)
			}
			if !reflect.DeepEqual(got, Snapshot{}) {
				t.Fatalf("a refusal returned %+v", got)
			}
		})
	}
}

func mustPayload(t *testing.T, snapshot Snapshot) []byte {
	t.Helper()
	payload, err := stdjson.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestSignRefusesWhatWouldNotVerify(t *testing.T) {
	key, _ := setup(t)
	snapshot := goodSnapshot()
	snapshot.Sequence = 0
	if _, err := Sign(key, snapshot); !errors.Is(err, ErrClaims) {
		t.Fatalf("sequence zero: %v", err)
	}
	snapshot = goodSnapshot()
	snapshot.ExpiresAt = snapshot.IssuedAt + 301
	if _, err := Sign(key, snapshot); !errors.Is(err, ErrLifetime) {
		t.Fatalf("long lifetime: %v", err)
	}
	if _, err := Sign(nil, goodSnapshot()); !errors.Is(err, ErrSigning) {
		t.Fatalf("nil key: %v", err)
	}
	if _, err := Sign(&crypto.Keypair{}, goodSnapshot()); !errors.Is(
		err, ErrSigning,
	) {
		t.Fatalf("empty key: %v", err)
	}
	other, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	mismatched := *key
	mismatched.SignPriv.PublicKey = other.SignPriv.PublicKey
	if _, err := Sign(&mismatched, goodSnapshot()); !errors.Is(
		err, ErrSigning,
	) {
		t.Fatalf("mismatched halves: %v", err)
	}
}

func TestATokenOverOneMebibyteIsRefused(t *testing.T) {
	key, verifier := setup(t)
	snapshot := goodSnapshot()
	snapshot.Members = nil
	// About 400 bytes each, so 3000 members is well past a mebibyte.
	for index := 0; index < 3000; index++ {
		snapshot.Members = append(snapshot.Members, Member{
			Subject: "person_" + strconv.Itoa(index), Role: "member",
			Email: strings.Repeat("e", 300),
		})
	}
	if _, err := Sign(key, snapshot); err == nil {
		t.Fatal("signed an oversize snapshot")
	}
	token := rawSnapshot(t, key, snapshot)
	if _, err := verifier.Verify(token, time.Unix(issuedAt, 0)); !errors.Is(
		err, ErrMalformed,
	) {
		t.Fatalf("got %v", err)
	}
}

func TestAnAssertionIsNotASnapshot(t *testing.T) {
	key, verifier := setup(t)
	token, err := assertion.Sign(key, assertion.Claims{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		Subject: "person_1", KeyID: "key_1", ID: "AAAAAAAAAAAAAAAAAAAAAA",
		Nonce: "AQEBAQEBAQEBAQEBAQEBAQ", IssuedAt: issuedAt,
		ExpiresAt: issuedAt + 120, AuthTime: issuedAt - 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = verifier.Verify(token, time.Unix(issuedAt, 0))
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("got %v, want a signature refusal", err)
	}
}

func TestNewVerifierRefusesBadConfiguration(t *testing.T) {
	key, _ := setup(t)
	keys := map[string][]byte{"key_1": key.SignPub()}
	cases := map[string]func() (*Verifier, error){
		"http issuer": func() (*Verifier, error) {
			return NewVerifier("http://root.example.com", instanceOrigin, keys)
		},
		"path in audience": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, instanceOrigin+"/x", keys)
		},
		"same origin": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, rootOrigin, keys)
		},
		"no keys": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, instanceOrigin, nil)
		},
		"short key": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, instanceOrigin,
				map[string][]byte{"key_1": {1, 2, 3}})
		},
		"bad key id": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, instanceOrigin,
				map[string][]byte{"key 1": key.SignPub()})
		},
		"point off the curve": func() (*Verifier, error) {
			off := append([]byte{4}, make([]byte, 64)...)
			return NewVerifier(rootOrigin, instanceOrigin,
				map[string][]byte{"key_1": off})
		},
	}
	for name, build := range cases {
		if _, err := build(); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestGoldenVector(t *testing.T) {
	var vector struct {
		PublicKey string   `json:"publicKey"`
		Now       int64    `json:"now"`
		Snapshot  Snapshot `json:"snapshot"`
		Token     string   `json:"token"`
	}
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := stdjson.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	public, err := base64.RawURLEncoding.DecodeString(vector.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(
		vector.Snapshot.Issuer, vector.Snapshot.Audience,
		map[string][]byte{vector.Snapshot.KeyID: public},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(vector.Token, time.Unix(vector.Now, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, vector.Snapshot) {
		t.Fatalf("got %+v, want %+v", got, vector.Snapshot)
	}
}
