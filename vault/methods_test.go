package vault_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/vault"
)

// fakeMethods serves the /v1/methods half of the wire: wrapped seeds
// keyed by method id, ceremony-gated writes, the last-method guard.
type fakeMethods struct {
	wrapped map[string][]byte // method id → wrapped seed
}

func (f *fakeMethods) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no bearer", http.StatusUnauthorized)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v1/methods")
	if !ok {
		http.NotFound(w, r)
		return
	}
	ceremony := r.Header.Get("X-Ceremony-Proof") == "proof"
	switch {
	case rest == "" && r.Method == http.MethodGet:
		var list []map[string]string
		for id := range f.wrapped {
			list = append(list, map[string]string{"id": id, "kind": "passkey", "created_at": "2026-08-28T00:00:00Z"})
		}
		json.NewEncoder(w).Encode(map[string]any{"methods": list})
	case strings.HasSuffix(rest, "/wrapped") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "/"), "/wrapped")
		b, ok := f.wrapped[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"wrapped": b})
	case strings.HasSuffix(rest, "/wrapped") && r.Method == http.MethodPut:
		if !ceremony {
			http.Error(w, "ceremony required", http.StatusForbidden)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "/"), "/wrapped")
		var body struct {
			Wrapped []byte `json:"wrapped"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.wrapped[id] = body.Wrapped
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete:
		if !ceremony {
			http.Error(w, "ceremony required", http.StatusForbidden)
			return
		}
		id := strings.TrimPrefix(rest, "/")
		if len(f.wrapped) == 1 {
			http.Error(w, "last method", http.StatusConflict)
			return
		}
		delete(f.wrapped, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func newMethodsClient(t *testing.T) (*vault.Client, *fakeMethods) {
	t.Helper()
	f := &fakeMethods{wrapped: map[string][]byte{"cred-a": []byte("wrapped-a")}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg := testConfig()
	cfg.Home = srv.URL
	cfg.Client = srv.Client()
	c, err := vault.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, f
}

// TestMethodsListAndWrapped: list the ways in, fetch one wrapped seed.
func TestMethodsListAndWrapped(t *testing.T) {
	c, _ := newMethodsClient(t)

	ms, err := c.Methods(t.Context())
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	if len(ms) != 1 || ms[0].ID != "cred-a" || ms[0].Kind != "passkey" {
		t.Fatalf("Methods = %+v, want one passkey cred-a", ms)
	}

	wrapped, err := c.Wrapped(t.Context(), "cred-a")
	if err != nil {
		t.Fatalf("Wrapped: %v", err)
	}
	if string(wrapped) != "wrapped-a" {
		t.Fatalf("Wrapped = %q", wrapped)
	}
}

// TestEnrolStoresWrappedSeed: Enrol PUTs the new wrap under the
// ceremony proof; the store gains the method.
func TestEnrolStoresWrappedSeed(t *testing.T) {
	c, f := newMethodsClient(t)

	if err := c.Enrol(t.Context(), "cred-b", []byte("wrapped-b"), "proof"); err != nil {
		t.Fatalf("Enrol: %v", err)
	}
	if string(f.wrapped["cred-b"]) != "wrapped-b" {
		t.Fatalf("store has %q", f.wrapped["cred-b"])
	}

	// Without a valid ceremony proof the server refuses and the client
	// surfaces it.
	if err := c.Enrol(t.Context(), "cred-c", []byte("x"), "wrong"); err == nil {
		t.Fatal("Enrol without ceremony succeeded")
	}
}

// TestRemoveMethodGuardsLast: removing succeeds ceremony-gated, and
// the server's last-method refusal surfaces as an error.
func TestRemoveMethodGuardsLast(t *testing.T) {
	c, f := newMethodsClient(t)
	f.wrapped["cred-b"] = []byte("wrapped-b")

	if err := c.RemoveMethod(t.Context(), "cred-b", "proof"); err != nil {
		t.Fatalf("RemoveMethod: %v", err)
	}
	if err := c.RemoveMethod(t.Context(), "cred-a", "proof"); err == nil {
		t.Fatal("RemoveMethod removed the last method")
	}
}

// TestBlobsLists: names and opaque versions only — never content.
func TestBlobsLists(t *testing.T) {
	c, _ := newTestClient(t)
	if _, err := c.Put(t.Context(), "servers", []byte("s"), vault.Create); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ls, err := c.Blobs(t.Context())
	if err != nil {
		t.Fatalf("Blobs: %v", err)
	}
	if len(ls) != 1 || ls[0].Name != "servers" || ls[0].Version == "" {
		t.Fatalf("Blobs = %+v", ls)
	}
}
