package objrec_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo/objrec"
)

// fakeS3 implements the conditional-write subset of S3 that objrec
// relies on, with the real status codes: 412 for a failed condition,
// 404 NoSuchKey for If-Match on a missing key, and ListObjectsV2
// pagination. conflict409 makes the next conditional PUT answer 409,
// which S3 sends when two conditional writes race.
type fakeS3 struct {
	mu          sync.Mutex
	objs        map[string][]byte
	pageSize    int
	conflict409 bool
	conflicts   int  // how many conditional PUTs in a row answer 409
	noBucket    bool // every request answers 404 NoSuchBucket
	putStatus   int  // status for a successful PUT; 0 means 200
	noETag      bool // successful PUTs send no ETag
	sawAuth     bool
}

func etag(b []byte) string { s := md5.Sum(b); return `"` + hex.EncodeToString(s[:]) + `"` }

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		f.sawAuth = true
	}
	key := strings.TrimPrefix(r.URL.Path, "/bkt/")
	if f.noBucket {
		http.Error(w, "<Code>NoSuchBucket</Code>", http.StatusNotFound)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.list(w, r)
	case r.Method == http.MethodGet:
		b, ok := f.objs[key]
		if !ok {
			http.Error(w, "<Code>NoSuchKey</Code>", http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", etag(b))
		w.Write(b)
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		cur, exists := f.objs[key]
		conditional := r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Match") != ""
		if conditional && (f.conflict409 || f.conflicts > 0) {
			f.conflict409 = false
			if f.conflicts > 0 {
				f.conflicts--
			}
			http.Error(w, "<Code>ConditionalRequestConflict</Code>", http.StatusConflict)
			return
		}
		if r.Header.Get("If-None-Match") == "*" && exists {
			http.Error(w, "<Code>PreconditionFailed</Code>", http.StatusPreconditionFailed)
			return
		}
		if m := r.Header.Get("If-Match"); m != "" {
			if !exists {
				http.Error(w, "<Code>NoSuchKey</Code>", http.StatusNotFound)
				return
			}
			if m != etag(cur) {
				http.Error(w, "<Code>PreconditionFailed</Code>", http.StatusPreconditionFailed)
				return
			}
		}
		f.objs[key] = body
		if !f.noETag {
			w.Header().Set("ETag", etag(body))
		}
		if f.putStatus != 0 {
			w.WriteHeader(f.putStatus)
		}
	case r.Method == http.MethodDelete:
		delete(f.objs, key)
		w.WriteHeader(204)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	after := q.Get("continuation-token")
	var keys []string
	for k := range f.objs {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	type content struct{ Key string }
	res := struct {
		XMLName               xml.Name  `xml:"ListBucketResult"`
		Contents              []content `xml:"Contents"`
		IsTruncated           bool
		NextContinuationToken string
	}{}
	max, _ := strconv.Atoi(q.Get("max-keys"))
	if f.pageSize > 0 && (max == 0 || f.pageSize < max) {
		max = f.pageSize
	}
	if max > 0 && len(keys) > max {
		res.IsTruncated = true
		res.NextContinuationToken = keys[max-1]
		keys = keys[:max]
	}
	for _, k := range keys {
		res.Contents = append(res.Contents, content{k})
	}
	xml.NewEncoder(w).Encode(res)
}

func newFake(t *testing.T) (*fakeS3, *objrec.S3Store) {
	f := &fakeS3{objs: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s, err := objrec.NewS3(objrec.S3Config{
		Bucket: "bkt", Region: "eu-west-1", Endpoint: srv.URL,
		AccessKey: "AK", SecretKey: "SK", Prefix: "records/",
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func TestS3CreateThenCreateIsExists(t *testing.T) {
	f, s := newFake(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, "a.json", []byte(`1`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "a.json", []byte(`2`)); !errors.Is(err, objrec.ErrExists) {
		t.Fatalf("got %v, want ErrExists", err)
	}
	if !f.sawAuth {
		t.Fatal("requests were not SigV4-signed")
	}
	if _, ok := f.objs["records/a.json"]; !ok {
		t.Fatal("the store prefix was not applied to the key")
	}
}

// S3 answers 409 ConditionalRequestConflict when another conditional
// write to the same key is still in flight, and says nothing about who
// won. Reading it as a loss lets two racers both lose with nothing
// written, so a single writer must retry through it.
func TestS3RetriesThroughAConflict(t *testing.T) {
	f, s := newFake(t)
	ctx := context.Background()
	f.conflicts = 2
	if _, err := s.Create(ctx, "a.json", []byte(`1`)); err != nil {
		t.Fatalf("create through two 409s: %v", err)
	}
	_, ver, _ := s.Get(ctx, "a.json")
	f.conflicts = 2
	if _, err := s.Update(ctx, "a.json", []byte(`2`), ver); err != nil {
		t.Fatalf("update through two 409s: %v", err)
	}
}

func TestS3PersistentConflictIsALoss(t *testing.T) {
	f, s := newFake(t)
	f.conflicts = 1000
	if _, err := s.Create(context.Background(), "a.json", []byte(`1`)); !errors.Is(err, objrec.ErrExists) {
		t.Fatalf("got %v, want ErrExists once retries are spent", err)
	}
}

func TestS3MissingBucketIsNotNotFound(t *testing.T) {
	f, s := newFake(t)
	f.noBucket = true
	ctx := context.Background()
	if _, _, err := s.Get(ctx, "a.json"); err == nil || errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("get: got %v; a missing bucket must not read as a missing record", err)
	}
	if _, err := s.Create(ctx, "a.json", []byte(`1`)); err == nil || errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("create: got %v; a missing bucket must not read as a missing record", err)
	}
	if _, err := s.Update(ctx, "a.json", []byte(`1`), objrec.Version(`"x"`)); err == nil || errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("update: got %v; a missing bucket must not read as a missing record", err)
	}
}

func TestS3PrefixMustBeWholeSegments(t *testing.T) {
	for _, p := range []string{"records", "a b/", "../x/", "/records/"} {
		_, err := objrec.NewS3(objrec.S3Config{Bucket: "b", Region: "r", AccessKey: "a", SecretKey: "s", Prefix: p})
		if !errors.Is(err, objrec.ErrKey) {
			t.Errorf("Prefix %q: got %v, want ErrKey", p, err)
		}
	}
}

func TestS3AcceptsAny2xxForAWrite(t *testing.T) {
	f, s := newFake(t)
	f.putStatus = 201
	if _, err := s.Create(context.Background(), "a.json", []byte(`1`)); err != nil {
		t.Fatalf("201 from a compatible backend: %v", err)
	}
}

func TestS3WriteWithoutAnETagIsAnError(t *testing.T) {
	f, s := newFake(t)
	f.noETag = true
	if _, err := s.Create(context.Background(), "a.json", []byte(`1`)); err == nil {
		t.Fatal("a write with no ETag returned no error; the next Update would send an empty If-Match")
	}
}

func TestS3StaleUpdateIsChanged(t *testing.T) {
	_, s := newFake(t)
	ctx := context.Background()
	v1, _ := s.Create(ctx, "a.json", []byte(`1`))
	if _, err := s.Update(ctx, "a.json", []byte(`2`), v1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, "a.json", []byte(`3`), v1); !errors.Is(err, objrec.ErrChanged) {
		t.Fatalf("got %v, want ErrChanged", err)
	}
}

func TestS3UpdateOfAMissingRecordIsNotFound(t *testing.T) {
	_, s := newFake(t)
	ctx := context.Background()
	if _, err := s.Update(ctx, "gone.json", []byte(`1`), objrec.Version(`"abc"`)); !errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if _, _, err := s.Get(ctx, "gone.json"); !errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("get: got %v, want ErrNotFound", err)
	}
}

func TestS3VersionRoundTripsWithItsQuotes(t *testing.T) {
	_, s := newFake(t)
	ctx := context.Background()
	v, _ := s.Create(ctx, "a.json", []byte(`1`))
	_, gv, _ := s.Get(ctx, "a.json")
	if v != gv || !strings.HasPrefix(string(v), `"`) {
		t.Fatalf("create %q, get %q: the ETag must pass back exactly as S3 sent it", v, gv)
	}
}

func TestS3ListFollowsTheCursor(t *testing.T) {
	f, s := newFake(t)
	f.pageSize = 2
	ctx := context.Background()
	for _, k := range []string{"m/c", "m/a", "m/b", "n/a"} {
		s.Create(ctx, k, []byte(`{}`))
	}
	var got []string
	cursor := ""
	for {
		keys, next, err := s.List(ctx, "m/", cursor, 1000)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, keys...)
		if next == "" {
			break
		}
		cursor = next
	}
	if strings.Join(got, ",") != "m/a,m/b,m/c" {
		t.Fatalf("list: %v (keys must come back without the store prefix)", got)
	}
}

func TestS3FromEnvWithNoBucketIsNilNil(t *testing.T) {
	t.Setenv("CARLOS_STORE_BUCKET", "")
	s, err := objrec.S3FromEnv()
	if s != nil || err != nil {
		t.Fatalf("got %v, %v; want nil, nil (no store is an answer, not an error)", s, err)
	}
}
