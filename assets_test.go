package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// hashedCSS is the URL shape Path promises: the FS path with 16 hex
// chars of the content hash inserted before the extension, absolute.
var hashedCSS = regexp.MustCompile(`^/static/tokens\.[0-9a-f]{16}\.css$`)

func TestPathInsertsContentHash(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	got := a.Path("static/tokens.css")
	if !hashedCSS.MatchString(got) {
		t.Errorf("Path = %q, want /static/tokens.<16 hex>.css", got)
	}
}

func TestPathIsStableForSameContent(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	if first, second := a.Path("static/tokens.css"), a.Path("static/tokens.css"); first != second {
		t.Errorf("same content, different paths: %q then %q", first, second)
	}
}

func TestPathDiffersAcrossContent(t *testing.T) {
	one := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	two := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("main{}")}})
	if p1, p2 := one.Path("static/tokens.css"), two.Path("static/tokens.css"); p1 == p2 {
		t.Errorf("different content, same path %q", p1)
	}
}

// A missing file degrades to the bare absolute path: the 404 then
// surfaces at request time, visibly, instead of a panic at render time.
func TestPathMissingFileReturnsBareName(t *testing.T) {
	a := NewAssets(fstest.MapFS{})
	if got := a.Path("static/nope.css"); got != "/static/nope.css" {
		t.Errorf("Path on missing file = %q, want /static/nope.css", got)
	}
}

// An extension-less name gets the hash appended at the end.
func TestPathExtensionless(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/LICENSE": {Data: []byte("mit")}})
	got := a.Path("static/LICENSE")
	if !regexp.MustCompile(`^/static/LICENSE\.[0-9a-f]{16}$`).MatchString(got) {
		t.Errorf("Path = %q, want /static/LICENSE.<16 hex>", got)
	}
}

// The freshness contract for a live-directory FS (an app using
// os.DirFS instead of embedding): editing the file changes the hash on
// the next lookup, no restart, because the (mtime, size) cache key
// notices the stat change.
func TestPathSeesFileEdits(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(dir, "static", "tokens.css")
	if err := os.WriteFile(css, []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewAssets(os.DirFS(dir))
	before := a.Path("static/tokens.css")

	// A distinct mtime as well as distinct content: some filesystems
	// have coarse timestamps, and the cache keys on (mtime, size).
	if err := os.WriteFile(css, []byte("main{color:red}"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(css, future, future); err != nil {
		t.Fatal(err)
	}

	after := a.Path("static/tokens.css")
	if before == after {
		t.Errorf("file edited but Path stayed %q", before)
	}
}

// serveAsset runs one GET through Handler and returns the recorder.
func serveAsset(t *testing.T, a *Assets, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHandlerServesHashedNameImmutable(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	rec := serveAsset(t, a, a.Path("static/tokens.css"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable year", got)
	}
	if rec.Body.String() != "body{}" {
		t.Errorf("body = %q, want the file content", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
}

// A bare name keeps working — deep links, hand-written URLs — it just
// forgoes the long cache.
func TestHandlerServesBareNameNoCache(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	rec := serveAsset(t, a, "/static/tokens.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}

// A stale page asking for an old version gets the *current* content,
// un-cached: a slightly-stale stylesheet on a stale page beats a 404.
func TestHandlerServesStaleHashCurrentContent(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	rec := serveAsset(t, a, "/static/tokens.0123456789abcdef.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache for a stale hash", got)
	}
	if rec.Body.String() != "body{}" {
		t.Errorf("body = %q, want current content", rec.Body.String())
	}
}

// A file whose real name happens to look hashed is served under that
// real name — literal names win over hash-stripping.
func TestHandlerLiteralNameWins(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/v.0123456789abcdef.css": {Data: []byte("real{}")}})
	rec := serveAsset(t, a, "/static/v.0123456789abcdef.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if rec.Body.String() != "real{}" {
		t.Errorf("body = %q, want the literal file", rec.Body.String())
	}
}

func TestHandlerMissing404s(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	for _, target := range []string{"/static/nope.css", "/static/nope.0123456789abcdef.css", "/static/"} {
		if rec := serveAsset(t, a, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rec.Code)
		}
	}
}

func TestHandlerSubdirectoryAsset(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/img/logo.svg": {Data: []byte("<svg/>")}})
	href := a.Path("static/img/logo.svg")
	rec := serveAsset(t, a, href)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", href, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable year", got)
	}
}

// Every answer carries the fingerprint as a strong ETag, so a no-cache
// answer revalidates to a 304: a conditional GET with the current ETag
// gets no body, one with any other ETag gets the file. The bare name, a
// legacy ?v= link and a stale hash all revalidate this way, and the
// immutable answer keeps its Cache-Control.
func TestHandlerRevalidatesByFingerprint(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	hashed := a.Path("static/tokens.css")
	fp := strings.TrimSuffix(strings.TrimPrefix(hashed, "/static/tokens."), ".css")
	want := `"` + fp + `"`
	for _, target := range []string{hashed, "/static/tokens.css", "/static/tokens.css?v=3", "/static/tokens.0123456789abcdef.css"} {
		rec := serveAsset(t, a, target)
		if got := rec.Header().Get("ETag"); got != want {
			t.Errorf("%s: ETag = %q, want %q (the fingerprint, strong, quoted)", target, got, want)
		}

		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("If-None-Match", want)
		rec = httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Errorf("%s with the current ETag: %d and %d bytes, want 304 and none", target, rec.Code, rec.Body.Len())
		}

		req = httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("If-None-Match", `"0123456789abcdef"`)
		rec = httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "body{}" {
			t.Errorf("%s with another ETag: %d %q, want 200 and the file", target, rec.Code, rec.Body.String())
		}
	}
	if got := serveAsset(t, a, hashed).Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("the current hash's Cache-Control is %q, want it unchanged", got)
	}
	if got := serveAsset(t, a, "/static/tokens.0123456789abcdef.css").Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("a stale hash's Cache-Control is %q, want it unchanged", got)
	}
}

// The ETag describes the bytes served: after an edit, the old ETag no
// longer matches and the new content comes back whole.
func TestHandlerETagFollowsTheContent(t *testing.T) {
	fsys := fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}}
	a := NewAssets(fsys)
	old := serveAsset(t, a, "/static/tokens.css").Header().Get("ETag")
	fsys["static/tokens.css"] = &fstest.MapFile{Data: []byte("body{color:red}"), ModTime: fsys["static/tokens.css"].ModTime.Add(time.Second)}
	req := httptest.NewRequest(http.MethodGet, "/static/tokens.css", nil)
	req.Header.Set("If-None-Match", old)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "body{color:red}" {
		t.Fatalf("after an edit the old ETag got %d %q, want 200 and the new content", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == old {
		t.Error("the ETag did not change with the content")
	}
}
