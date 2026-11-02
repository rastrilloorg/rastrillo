package rastrillo

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// commentedCSS and commentedJS stand in for the vendored files, which
// carry their design notes with them.
const (
	commentedCSS = "/* why the rule */\nbody {\n  color: red;\n}\n"
	commentedJS  = "// why the script\nfunction add(first, second) {\n" +
		"  /* why the sum */\n  return first + second;\n}\nadd(1, 2);\n"
)

func vendoredTree() fstest.MapFS {
	return fstest.MapFS{
		"static/tokens.css":   {Data: []byte(commentedCSS)},
		"static/rastrillo.js": {Data: []byte(commentedJS)},
	}
}

// getAsset runs one GET through Handler with the given
// Accept-Encoding, none when it is empty.
func getAsset(
	t *testing.T, assets *Assets, target, acceptEncoding string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	recorder := httptest.NewRecorder()
	assets.Handler().ServeHTTP(recorder, request)
	return recorder
}

func fingerprintOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:assetHashLen]
}

func TestAVendoredFileIsServedMinified(t *testing.T) {
	assets := NewAssets(vendoredTree())
	for name, source := range map[string]string{
		"static/tokens.css": commentedCSS, "static/rastrillo.js": commentedJS,
	} {
		body := getAsset(t, assets, assets.Path(name), "").Body.String()
		if strings.Contains(body, "why") || len(body) >= len(source) {
			t.Errorf("%s served as %q, want it minified", name, body)
		}
	}
}

func TestPathFingerprintsTheBytesSent(t *testing.T) {
	assets := NewAssets(vendoredTree())
	href := assets.Path("static/tokens.css")
	body := getAsset(t, assets, href, "").Body.Bytes()
	want := "/static/tokens." + fingerprintOf(body) + ".css"
	if href != want {
		t.Errorf("Path = %s, want %s, the hash of the bytes served", href, want)
	}
}

func TestAnAppsOwnFileIsServedAsWritten(t *testing.T) {
	tree := fstest.MapFS{
		"static/app.css":         {Data: []byte(commentedCSS)},
		"static/admin/select.js": {Data: []byte(commentedJS)},
		"tokens.css":             {Data: []byte(commentedCSS)},
	}
	assets := NewAssets(tree)
	for name, file := range tree {
		recorder := getAsset(t, assets, assets.Path(name), "gzip")
		if recorder.Body.String() != string(file.Data) ||
			recorder.Header().Get("Content-Encoding") != "" {
			t.Errorf("%s served as %q, want it as written",
				name, recorder.Body.String())
		}
	}
}

func TestAVendoredFileIsGzippedForAClientThatAsks(t *testing.T) {
	assets := NewAssets(vendoredTree())
	href := assets.Path("static/tokens.css")
	identity := getAsset(t, assets, href, "").Body.Bytes()
	recorder := getAsset(t, assets, href, "gzip, deflate")
	header := recorder.Header()
	if header.Get("Content-Encoding") != "gzip" ||
		header.Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Content-Encoding %q, Vary %q; want gzip, Accept-Encoding",
			header.Get("Content-Encoding"), header.Get("Vary"))
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(unzipped, identity) {
		t.Errorf("gunzipped %q (%v), want the identity body %q",
			unzipped, err, identity)
	}
	want := `"` + fingerprintOf(identity) + `-gzip"`
	if header.Get("ETag") != want {
		t.Errorf("ETag %s, want %s", header.Get("ETag"), want)
	}
}

func TestAClientRefusingGzipGetsTheMinifiedBytes(t *testing.T) {
	assets := NewAssets(vendoredTree())
	href := assets.Path("static/tokens.css")
	for _, acceptEncoding := range []string{"gzip;q=0", "identity", ""} {
		recorder := getAsset(t, assets, href, acceptEncoding)
		header := recorder.Header()
		body := recorder.Body.Bytes()
		switch {
		case header.Get("Content-Encoding") != "":
			t.Errorf("%q: Content-Encoding %q, want none",
				acceptEncoding, header.Get("Content-Encoding"))
		case header.Get("Vary") != "Accept-Encoding":
			t.Errorf("%q: Vary %q, want Accept-Encoding",
				acceptEncoding, header.Get("Vary"))
		case header.Get("ETag") != `"`+fingerprintOf(body)+`"`:
			t.Errorf("%q: ETag %s for %q", acceptEncoding,
				header.Get("ETag"), body)
		case bytes.Contains(body, []byte("why")):
			t.Errorf("%q: got %q, want the minified bytes",
				acceptEncoding, body)
		}
	}
}

func TestEachEncodingRevalidatesOnItsOwnETag(t *testing.T) {
	assets := NewAssets(vendoredTree())
	href := assets.Path("static/tokens.css")
	plain := getAsset(t, assets, href, "").Header().Get("ETag")
	zipped := getAsset(t, assets, href, "gzip").Header().Get("ETag")
	cases := []struct {
		etag, acceptEncoding string
		want                 int
	}{
		{plain, "", http.StatusNotModified},
		{zipped, "gzip", http.StatusNotModified},
		{zipped, "", http.StatusOK},
	}
	for _, test := range cases {
		request := httptest.NewRequest(http.MethodGet, href, nil)
		request.Header.Set("If-None-Match", test.etag)
		if test.acceptEncoding != "" {
			request.Header.Set("Accept-Encoding", test.acceptEncoding)
		}
		recorder := httptest.NewRecorder()
		assets.Handler().ServeHTTP(recorder, request)
		if recorder.Code != test.want {
			t.Errorf("If-None-Match %s, Accept-Encoding %q: %d, want %d",
				test.etag, test.acceptEncoding, recorder.Code, test.want)
		}
	}
}

func TestAFileTheMinifierRefusesIsServedAsWritten(t *testing.T) {
	broken := "function ("
	assets := NewAssets(fstest.MapFS{
		"static/select.js": {Data: []byte(broken)},
	})
	recorder := getAsset(t, assets, assets.Path("static/select.js"), "")
	if recorder.Code != http.StatusOK || recorder.Body.String() != broken {
		t.Errorf("got %d %q, want 200 and the file as written",
			recorder.Code, recorder.Body.String())
	}
}

func TestALiveDirectoryEditIsMinifiedAgain(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(dir, "static", "tokens.css")
	if err := os.WriteFile(css, []byte(commentedCSS), 0o644); err != nil {
		t.Fatal(err)
	}
	assets := NewAssets(os.DirFS(dir))
	before := assets.Path("static/tokens.css")
	edited := "/* new note */\nmain {\n  color: blue;\n}\n"
	if err := os.WriteFile(css, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(css, future, future); err != nil {
		t.Fatal(err)
	}
	after := assets.Path("static/tokens.css")
	body := getAsset(t, assets, after, "").Body.String()
	if after == before || !strings.Contains(body, "blue") ||
		strings.Contains(body, "note") {
		t.Errorf("after the edit: %s serving %q, before %s",
			after, body, before)
	}
}

func TestAFileTheMinifierEmptiesIsServedAsWritten(t *testing.T) {
	written := "/* only a note */\nbody {}\n"
	assets := NewAssets(fstest.MapFS{
		"static/tokens.css": {Data: []byte(written)},
	})
	recorder := getAsset(t, assets, assets.Path("static/tokens.css"), "")
	if recorder.Body.String() != written {
		t.Errorf("got %q, want the file as written", recorder.Body.String())
	}
}

// editingFS is a live directory whose one file is edited while a
// request is being served: every Stat sees a new mtime, and the third
// read onwards sees the edit.
type editingFS struct {
	mu            sync.Mutex
	stats, reads  int
	before, after []byte
}

func (tree *editingFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (tree *editingFS) Stat(name string) (fs.FileInfo, error) {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if name != "static/tokens.css" {
		return nil, fs.ErrNotExist
	}
	tree.stats++
	return fstest.MapFS{name: {
		Data:    tree.before,
		ModTime: time.Unix(int64(tree.stats), 0),
	}}.Stat(name)
}

func (tree *editingFS) ReadFile(name string) ([]byte, error) {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	tree.reads++
	if tree.reads > 2 {
		return tree.after, nil
	}
	return tree.before, nil
}

// An immutable answer must carry the bytes its URL names: a file edited
// between choosing the cache policy and reading the body would
// otherwise be cached for a year under the old version's URL.
func TestAnImmutableAnswerCarriesTheBytesItsURLNames(t *testing.T) {
	tree := &editingFS{
		before: []byte("a{color:red}"), after: []byte("b{color:blue}"),
	}
	assets := NewAssets(tree)
	href := assets.Path("static/tokens.css")
	recorder := getAsset(t, assets, href, "")
	immutable := strings.Contains(
		recorder.Header().Get("Cache-Control"), "immutable")
	sent := fingerprintOf(recorder.Body.Bytes())
	if immutable && !strings.Contains(href, "."+sent+".") {
		t.Errorf("%s answered immutable with %q, whose hash is %s",
			href, recorder.Body.String(), sent)
	}
}
