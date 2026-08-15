package rastrillo

import (
	"os"
	"path/filepath"
	"regexp"
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
