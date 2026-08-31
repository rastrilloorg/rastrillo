package blobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
)

func testStores(t *testing.T) map[string]Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "blobs.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	dir, err := Dir(t.TempDir())
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	return map[string]Store{"inline": Inline(d.Writer()), "dir": dir}
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	for name, s := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			body := []byte("hello, content addressing")
			want := sha256.Sum256(body)

			ref, err := s.Put(ctx, strings.NewReader(string(body)), "text/plain")
			if err != nil {
				t.Fatalf("Put: %v", err)
			}
			if ref.Hash != hex.EncodeToString(want[:]) {
				t.Fatalf("Hash = %s, want the content's SHA-256", ref.Hash)
			}
			if ref.Size != int64(len(body)) || ref.ContentType != "text/plain" {
				t.Fatalf("Ref = %+v", ref)
			}

			// Idempotent by construction: same bytes, same Ref, no error.
			ref2, err := s.Put(ctx, strings.NewReader(string(body)), "text/plain")
			if err != nil || ref2.Hash != ref.Hash {
				t.Fatalf("second Put: %+v, %v", ref2, err)
			}

			rc, err := s.Get(ctx, ref.Hash)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			got, _ := io.ReadAll(rc)
			rc.Close()
			if string(got) != string(body) {
				t.Fatalf("Get = %q", got)
			}

			if err := s.Delete(ctx, ref.Hash); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, err := s.Get(ctx, ref.Hash); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Get after delete: %v, want ErrNotFound", err)
			}
			// Deleting what is not there is not an error.
			if err := s.Delete(ctx, ref.Hash); err != nil {
				t.Fatalf("second Delete: %v", err)
			}
			// A malformed hash is rejected before it can be a path or a key.
			if _, err := s.Get(ctx, "../../etc/passwd"); err == nil || errors.Is(err, ErrNotFound) {
				t.Fatalf("traversal-shaped hash: %v, want a validation error", err)
			}
		})
	}
}

func TestSealedStore(t *testing.T) {
	ctx := context.Background()
	dir, _ := Dir(t.TempDir())
	key := make([]byte, 32)
	key[0] = 7
	s := Sealed(dir, key)

	plain := []byte("the server never sees this")
	ref, err := s.Put(ctx, strings.NewReader(string(plain)), "application/octet-stream")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ref.Size <= int64(len(plain)) {
		t.Fatalf("Ref.Size = %d, want the sealed (larger) size", ref.Size)
	}

	// The underlying store holds ciphertext, addressed by its own hash.
	rc, err := dir.Get(ctx, ref.Hash)
	if err != nil {
		t.Fatalf("underlying Get: %v", err)
	}
	raw, _ := io.ReadAll(rc)
	rc.Close()
	if strings.Contains(string(raw), "never sees") {
		t.Fatal("plaintext reached the underlying store")
	}

	rc, err = s.Get(ctx, ref.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != string(plain) {
		t.Fatalf("Get = %q", got)
	}

	wrong := Sealed(dir, make([]byte, 32))
	if _, err := wrong.Get(ctx, ref.Hash); err == nil {
		t.Fatal("Get under the wrong key succeeded")
	}
}

// fakeS3 stores objects by path and requires a SigV4-shaped
// Authorization header — a wire-level exercise of the client without a
// bucket or a bill.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
		http.Error(w, "no sigv4", http.StatusForbidden)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[r.URL.Path] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		if b, ok := f.objects[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	case http.MethodDelete:
		delete(f.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestS3StoreAgainstFake(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	s, err := NewS3(S3Config{
		Bucket: "testbucket", Region: "eu-west-1",
		Endpoint:  srv.URL,
		AccessKey: "AKIATEST", SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	ctx := context.Background()
	body := []byte("bytes for the bucket")
	ref, err := s.Put(ctx, strings.NewReader(string(body)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	wantPath := "/testbucket/blobs/" + ref.Hash
	if _, ok := fake.objects[wantPath]; !ok {
		t.Fatalf("object not stored at %q; stored keys: %v", wantPath, keys(fake.objects))
	}

	rc, err := s.Get(ctx, ref.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != string(body) {
		t.Fatalf("Get = %q", got)
	}

	if _, err := s.Get(ctx, strings.Repeat("0", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing blob: %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(fake.objects) != 0 {
		t.Fatal("Delete left the object behind")
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestS3FromEnv(t *testing.T) {
	t.Setenv("CARLOS_STORE_BUCKET", "")
	s, err := S3FromEnv()
	if s != nil || err != nil {
		t.Fatalf("no bucket: %v, %v; want nil, nil (the clean 'no store' answer)", s, err)
	}

	t.Setenv("CARLOS_STORE_BUCKET", "myapp-store")
	t.Setenv("CARLOS_STORE_REGION", "eu-west-1")
	t.Setenv("CARLOS_STORE_ACCESS_KEY", "AKIA")
	t.Setenv("CARLOS_STORE_SECRET_KEY", "sk")
	s, err = S3FromEnv()
	if err != nil || s == nil {
		t.Fatalf("configured store: %v, %v", s, err)
	}
	if s.cfg.Bucket != "myapp-store" || s.cfg.Prefix != "blobs/" {
		t.Fatalf("cfg = %+v", s.cfg)
	}
}

// TestPresignVector pins query-parameter presigning against the
// official AWS worked example ("Authenticating Requests: Using Query
// Parameters (AWS Signature Version 4)": examplebucket, test.txt,
// 86400s, 2013-05-24).
func TestPresignVector(t *testing.T) {
	ts := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	u, err := presignURL(http.MethodGet,
		"https://examplebucket.s3.amazonaws.com/test.txt",
		"us-east-1", "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		86400*time.Second, ts)
	if err != nil {
		t.Fatalf("presignURL: %v", err)
	}
	const wantSig = "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if !strings.HasSuffix(u, "X-Amz-Signature="+wantSig) {
		t.Fatalf("presigned URL signature mismatch:\n%s\nwant …X-Amz-Signature=%s", u, wantSig)
	}
}

func TestPresignedGetWorksAgainstFake(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A presigned request carries no Authorization header; the
		// fake only checks the signature params exist.
		if r.URL.Query().Get("X-Amz-Signature") == "" {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if b, ok := fake.objects[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	s, _ := NewS3(S3Config{
		Bucket: "b", Region: "r", Endpoint: srv.URL,
		AccessKey: "ak", SecretKey: "sk",
	})
	hash := strings.Repeat("a", 64)
	fake.objects["/b/blobs/"+hash] = []byte("presigned bytes")

	u, err := s.PresignGet(hash, time.Hour)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "presigned bytes" {
		t.Fatalf("presigned GET: %d %q", resp.StatusCode, body)
	}
}
