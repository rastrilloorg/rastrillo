package vault_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/vault"
)

// fakeHome is an in-memory Pegamento vault: the v1 blob wire, opaque
// versions (a counter the client must never parse), bearer auth. It
// records every sealed body so tests can assert what crossed the wire.
type fakeHome struct {
	t     *testing.T
	blobs map[string]struct {
		sealed  []byte
		version int
	}
	lastSealed []byte
}

func newFakeHome(t *testing.T) (*fakeHome, *httptest.Server) {
	f := &fakeHome{t: t, blobs: map[string]struct {
		sealed  []byte
		version int
	}{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeHome) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no bearer", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/v1/blobs" && r.Method == http.MethodGet {
		var list []map[string]string
		for name, b := range f.blobs {
			list = append(list, map[string]string{"name": name, "version": strconv.Itoa(b.version)})
		}
		json.NewEncoder(w).Encode(map[string]any{"blobs": list})
		return
	}
	name, ok := strings.CutPrefix(r.URL.Path, "/v1/blobs/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		b, ok := f.blobs[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sealed": b.sealed, "version": strconv.Itoa(b.version),
		})
	case http.MethodPut:
		var body struct {
			Sealed []byte `json:"sealed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cur := f.blobs[name]
		have := "0"
		if cur.version != 0 {
			have = strconv.Itoa(cur.version)
		}
		if r.Header.Get("If-Match") != have {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"version": have})
			return
		}
		f.blobs[name] = struct {
			sealed  []byte
			version int
		}{body.Sealed, cur.version + 1}
		f.lastSealed = body.Sealed
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": strconv.Itoa(cur.version + 1)})
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func newTestClient(t *testing.T) (*vault.Client, *fakeHome) {
	t.Helper()
	f, srv := newFakeHome(t)
	cfg := testConfig()
	cfg.Home = srv.URL
	cfg.Client = srv.Client()
	c, err := vault.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, f
}

// TestPutGetRoundTrip: create, read back, overwrite with the returned
// version, read again. Plaintext in, plaintext out; versions opaque.
func TestPutGetRoundTrip(t *testing.T) {
	c, _ := newTestClient(t)

	v1, err := c.Put(t.Context(), "servers", []byte(`{"servers":[]}`), vault.Create)
	if err != nil {
		t.Fatalf("Put(Create): %v", err)
	}
	got, gotV, err := c.Get(t.Context(), "servers")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != `{"servers":[]}` || gotV != v1 {
		t.Fatalf("Get = %q version %q, want the Put plaintext at version %q", got, gotV, v1)
	}

	v2, err := c.Put(t.Context(), "servers", []byte(`{"servers":["a"]}`), v1)
	if err != nil {
		t.Fatalf("Put(v1): %v", err)
	}
	if v2 == v1 {
		t.Fatal("version did not change on overwrite")
	}
}

// TestPutRefusesStale: a wrong version answers ErrStale carrying the
// current version for the merge-and-retry loop; Create refuses when
// the blob exists.
func TestPutRefusesStale(t *testing.T) {
	c, _ := newTestClient(t)

	v1, err := c.Put(t.Context(), "servers", []byte("one"), vault.Create)
	if err != nil {
		t.Fatalf("Put(Create): %v", err)
	}
	for _, stale := range []string{vault.Create, "999"} {
		var es vault.ErrStale
		_, err := c.Put(t.Context(), "servers", []byte("two"), stale)
		if !errors.As(err, &es) {
			t.Fatalf("Put(version %q) err = %v, want ErrStale", stale, err)
		}
		if es.Current != v1 {
			t.Fatalf("ErrStale.Current = %q, want %q", es.Current, v1)
		}
	}
}

// TestSealedAtRest: what crosses the wire opens under nothing the
// server holds — it is not the plaintext, and two puts of the same
// plaintext differ (fresh nonces).
func TestSealedAtRest(t *testing.T) {
	c, f := newTestClient(t)

	plain := []byte("the plaintext")
	if _, err := c.Put(t.Context(), "servers", plain, vault.Create); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if bytes.Contains(f.lastSealed, plain) {
		t.Fatal("plaintext crossed the wire")
	}
	first := f.lastSealed
	v, _, err := c.Get(t.Context(), "servers")
	_ = v
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.Put(t.Context(), "drafts", plain, vault.Create); err != nil {
		t.Fatalf("Put(drafts): %v", err)
	}
	if bytes.Equal(first, f.lastSealed) {
		t.Fatal("two seals of one plaintext were byte-identical")
	}
}

// TestPutPaddedUniformLength: padded puts of different plaintexts
// produce equal sealed lengths, and open back to the exact plaintext.
func TestPutPaddedUniformLength(t *testing.T) {
	c, f := newTestClient(t)

	if _, err := c.PutPadded(t.Context(), "servers", []byte("a"), 1024, vault.Create); err != nil {
		t.Fatalf("PutPadded(a): %v", err)
	}
	lenA := len(f.lastSealed)
	if _, err := c.PutPadded(t.Context(), "drafts", bytes.Repeat([]byte("b"), 900), 1024, vault.Create); err != nil {
		t.Fatalf("PutPadded(b): %v", err)
	}
	if len(f.lastSealed) != lenA {
		t.Fatalf("padded lengths differ: %d vs %d", lenA, len(f.lastSealed))
	}
	got, _, err := c.Get(t.Context(), "servers")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "a" {
		t.Fatalf("padded round trip = %q, want %q", got, "a")
	}

	// A plaintext larger than the pad target still round-trips — the
	// target is a floor, not a cap.
	big := bytes.Repeat([]byte("c"), 2048)
	if _, err := c.PutPadded(t.Context(), "servers", big, 1024, "1"); err != nil {
		t.Fatalf("PutPadded(big): %v", err)
	}
	got, _, err = c.Get(t.Context(), "servers")
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("oversize padded round trip failed: err %v", err)
	}
}

// TestGetMissing: a blob that has never been written answers
// ErrNotFound — first-run, not an outage.
func TestGetMissing(t *testing.T) {
	c, _ := newTestClient(t)
	_, _, err := c.Get(t.Context(), "servers")
	if !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

var _ = fmt.Sprintf // keep fmt if assertions above change
