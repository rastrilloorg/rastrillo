package rastrillo

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"
)

// Assets fingerprints an app's static files so they can be cached
// forever and still update on an ordinary reload (see the assets +
// TDD-scaffold design doc). Path maps a file to a URL carrying its
// content hash; Handler serves that URL with an immutable
// Cache-Control. Because the hash changes whenever the content does,
// the HTML always links a URL the browser has never cached stale.
//
// The FS is served with http.FileServerFS semantics — URL path = "/" +
// FS path — matching how the scaffold embeds static/ (assets.go's
// //go:embed static): NewAssets(app.StaticFS), mounted at
// "GET /static/" with no StripPrefix.
type Assets struct {
	fsys fs.FS

	mu    sync.Mutex
	cache map[string]assetInfo
}

// assetInfo caches one file's hash keyed by the stat that produced it.
// For an embedded FS the mtime is the zero time and never changes —
// one hash per process, which is right, because embedded content can't
// change without a rebuild and restart. For a live directory
// (os.DirFS), an edit changes (mtime, size) and the next lookup
// rehashes — which is what keeps a non-embedding app fresh without a
// restart.
type assetInfo struct {
	hash  string
	mtime time.Time
	size  int64
}

// NewAssets wraps a file tree — the scaffold's embedded StaticFS, or
// os.DirFS for an app serving a live directory — in a content-hash
// registry.
func NewAssets(fsys fs.FS) *Assets {
	return &Assets{fsys: fsys, cache: make(map[string]assetInfo)}
}

// assetHashLen is how much of the SHA-256 hex survives into the URL.
// 16 hex chars = 64 bits: collisions would need billions of asset
// versions, and shorter names keep the HTML readable.
const assetHashLen = 16

// Path maps an FS path to its currently-hashed absolute URL path:
//
//	Path("static/tokens.css") → "/static/tokens.d1e8a70b5ccab1dc.css"
//
// A missing file returns "/" + name unchanged, so the 404 surfaces at
// request time — visible in the network tab — instead of a render-time
// panic.
func (a *Assets) Path(name string) string {
	h, err := a.hashFor(name)
	if err != nil {
		return "/" + name
	}
	dir, base := path.Split(name)
	ext := path.Ext(base)
	return "/" + dir + strings.TrimSuffix(base, ext) + "." + h + ext
}

// hashFor returns name's content hash, recomputing only when the
// file's (mtime, size) differ from the cached stat.
func (a *Assets) hashFor(name string) (string, error) {
	info, err := fs.Stat(a.fsys, name)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fs.ErrNotExist
	}

	a.mu.Lock()
	cached, ok := a.cache[name]
	a.mu.Unlock()
	if ok && cached.mtime.Equal(info.ModTime()) && cached.size == info.Size() {
		return cached.hash, nil
	}

	b, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	h := hex.EncodeToString(sum[:])[:assetHashLen]

	a.mu.Lock()
	a.cache[name] = assetInfo{hash: h, mtime: info.ModTime(), size: info.Size()}
	a.mu.Unlock()
	return h, nil
}
