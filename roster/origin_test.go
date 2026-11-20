package roster_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/roster"
)

const customOrigin = "https://blog.example.org"

func originSnapshot(origin string) roster.Snapshot {
	return roster.Snapshot{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		KeyID: "key_1", Sequence: 1, IssuedAt: epoch.Unix(),
		ExpiresAt: epoch.Unix() + 60, State: roster.Active,
		Members: []roster.Member{}, Origin: origin,
	}
}

func (fix *fixture) tokenWithOrigin(
	t *testing.T, sequence int64, origin string,
) string {
	t.Helper()
	snapshot := originSnapshot(origin)
	snapshot.Sequence = sequence
	token, err := roster.Sign(fix.key, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// signedRaw signs snapshot without Sign's checks, as a root with a bug
// would, so Verify's own refusal is what the test sees.
func signedRaw(
	t *testing.T, key *crypto.Keypair, snapshot roster.Snapshot,
) string {
	t.Helper()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(key, "rastrillo/roster/v1", payload)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature)
}

func TestSnapshotCarriesOrigin(t *testing.T) {
	fix := newFixture(t)
	for _, origin := range []string{customOrigin, ""} {
		token, err := roster.Sign(fix.key, originSnapshot(origin))
		if err != nil {
			t.Fatalf("Sign(%q): %v", origin, err)
		}
		got, err := fix.verifier.Verify(token, epoch)
		if err != nil || got.Origin != origin {
			t.Fatalf("Verify(%q) = %q, %v", origin, got.Origin, err)
		}
		payload, _, _ := strings.Cut(token, ".")
		raw, err := base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			t.Fatal(err)
		}
		// Without an origin the bytes are the ones an older receiver
		// reads, because its Verify refuses a field it does not know.
		carries := strings.Contains(string(raw), `"origin"`)
		if carries != (origin != "") {
			t.Errorf("origin %q: payload %s", origin, raw)
		}
	}
}

func TestSnapshotRefusesBadOrigin(t *testing.T) {
	fix := newFixture(t)
	for _, origin := range []string{
		"https://blog.example.org/",
		"https://blog.example.org/path",
		"http://blog.example.org",
		"https://user@blog.example.org",
		"https://user:secret@blog.example.org",
		"https://blog.example.org?query",
		"https://blog.example.org:8443",
		"https://Blog.example.org",
		"blog.example.org",
		rootOrigin,
	} {
		_, err := roster.Sign(fix.key, originSnapshot(origin))
		if !errors.Is(err, roster.ErrClaims) {
			t.Errorf("Sign(%q) = %v, want ErrClaims", origin, err)
		}
		token := signedRaw(t, fix.key, originSnapshot(origin))
		_, err = fix.verifier.Verify(token, epoch)
		if !errors.Is(err, roster.ErrClaims) {
			t.Errorf("Verify(%q) = %v, want ErrClaims", origin, err)
		}
	}
}

func TestReceiverStoresOriginAndCallsOnOrigin(t *testing.T) {
	fix := newFixture(t)
	type change struct{ previous, next string }
	var changes []change
	var answered []int
	var response *httptest.ResponseRecorder
	fix.onOrigin = func(previous, next string) {
		changes = append(changes, change{previous, next})
		answered = append(answered, response.Code)
	}
	post := func(token string) int {
		request := httptest.NewRequest(http.MethodPost,
			instanceOrigin+roster.Path, strings.NewReader(token))
		response = httptest.NewRecorder()
		fix.receiver.ServeHTTP(response, request)
		return response.Code
	}
	const other = "https://www.example.org"
	steps := []struct {
		sequence int64
		origin   string
		status   int
	}{
		{1, customOrigin, 204},
		{2, customOrigin, 204}, // a repeat is not a change
		{2, customOrigin, 204}, // nor is the root's retry
		{3, other, 204},
		{1, "", 409}, // an older snapshot changes nothing
		{4, "", 204},
	}
	for _, step := range steps {
		token := fix.tokenWithOrigin(t, step.sequence, step.origin)
		if code := post(token); code != step.status {
			t.Fatalf("seq %d: status %d, want %d",
				step.sequence, code, step.status)
		}
		if step.status != 204 {
			continue
		}
		stored, err := fix.receiver.Origin(context.Background())
		if err != nil || stored != step.origin {
			t.Fatalf("seq %d: Origin = %q, %v", step.sequence, stored, err)
		}
	}
	want := []change{{"", customOrigin}, {customOrigin, other}, {other, ""}}
	if len(changes) != len(want) {
		t.Fatalf("OnOrigin calls = %v, want %v", changes, want)
	}
	for index := range want {
		if changes[index] != want[index] {
			t.Errorf("call %d = %v, want %v", index, changes[index],
				want[index])
		}
		// The root has its answer before the app starts to restart.
		if answered[index] != 204 {
			t.Errorf("call %d ran before the answer: %d", index,
				answered[index])
		}
	}
}

func TestStoredOriginAtBoot(t *testing.T) {
	fix := newFixture(t)
	ctx := context.Background()
	origin, err := roster.StoredOrigin(ctx, fix.db)
	if err != nil || origin != "" {
		t.Fatalf("before any snapshot: %q, %v", origin, err)
	}
	if code := fix.post(fix.tokenWithOrigin(t, 1, customOrigin)); code != 204 {
		t.Fatalf("status = %d", code)
	}
	// A new process reads it before it has built a receiver.
	origin, err = roster.StoredOrigin(ctx, fix.db)
	if err != nil || origin != customOrigin {
		t.Fatalf("after a snapshot: %q, %v", origin, err)
	}
	fix.now = epoch.Add(time.Second)
	if code := fix.post(fix.tokenWithOrigin(t, 2, "")); code != 204 {
		t.Fatalf("status = %d", code)
	}
	origin, err = roster.StoredOrigin(ctx, fix.db)
	if err != nil || origin != "" {
		t.Fatalf("after clearing: %q, %v", origin, err)
	}
}
