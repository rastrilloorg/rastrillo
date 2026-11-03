package rastrillo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
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

	// slicing guards slices apart from mu, so cutting a page's first
	// slice does not hold up every other file's lookup.
	slicing sync.Mutex
	slices  map[string]*slicesOfFile
}

// assetInfo caches one file's hash keyed by the stat that produced it.
// For an embedded FS the mtime is the zero time and never changes —
// one hash per process, which is right, because embedded content can't
// change without a rebuild and restart. For a live directory
// (os.DirFS), an edit changes (mtime, size) and the next lookup
// rehashes — which is what keeps a non-embedding app fresh without a
// restart.
//
// A vendored file (vendored.Is) also keeps the bytes it is served as,
// minified, and their gzip, so the minifier runs once per version of
// the file rather than once per request. served is nil for every other
// file, which is served as read.
type assetInfo struct {
	hash            string
	mtime           time.Time
	size            int64
	served, gzipped []byte
}

// NewAssets wraps a file tree — the scaffold's embedded StaticFS, or
// os.DirFS for an app serving a live directory — in a content-hash
// registry.
func NewAssets(fsys fs.FS) *Assets {
	return &Assets{
		fsys:   fsys,
		cache:  make(map[string]assetInfo),
		slices: make(map[string]*slicesOfFile),
	}
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
func (assets *Assets) Path(name string) string {
	entry, err := assets.lookup(name)
	if err != nil {
		return "/" + name
	}
	return hashedPath(name, entry.hash)
}

// hashedPath inserts hash before name's extension, the URL shape
// Handler and hashedName read back.
func hashedPath(name, hash string) string {
	dir, base := path.Split(name)
	ext := path.Ext(base)
	return "/" + dir + strings.TrimSuffix(base, ext) + "." + hash + ext
}

// lookup returns name's cached entry, rebuilding it only when the
// file's (mtime, size) differ from the cached stat. The hash is of the
// bytes served, so a minified file's URL changes with what it sends.
func (assets *Assets) lookup(name string) (assetInfo, error) {
	info, err := fs.Stat(assets.fsys, name)
	if err != nil {
		return assetInfo{}, err
	}
	if info.IsDir() {
		return assetInfo{}, fs.ErrNotExist
	}
	assets.mu.Lock()
	cached, ok := assets.cache[name]
	assets.mu.Unlock()
	if ok && cached.mtime.Equal(info.ModTime()) &&
		cached.size == info.Size() {
		return cached, nil
	}
	body, err := fs.ReadFile(assets.fsys, name)
	if err != nil {
		return assetInfo{}, err
	}
	entry := assetInfo{mtime: info.ModTime(), size: info.Size()}
	if vendored.Is(name) {
		entry.served, entry.gzipped = servedForms(name, body)
		body = entry.served
	}
	entry.hash = fingerprint(body)
	assets.mu.Lock()
	assets.cache[name] = entry
	assets.mu.Unlock()
	return entry, nil
}

// cachePolicy is immutable for a URL naming exactly the bytes sent, and
// no-cache for a bare name or a stale hash.
func cachePolicy(hash, hashInURL string) string {
	if hashInURL != "" && hash == hashInURL {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

// fingerprint is the part of body's SHA-256 that goes in a URL and an
// ETag.
func fingerprint(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:assetHashLen]
}

// hashedName matches a basename carrying an inserted hash —
// "tokens.<16 hex>.css" or extension-less "LICENSE.<16 hex>" — and
// captures the pieces needed to reconstruct the original name.
var hashedName = regexp.MustCompile(`^(.+)\.([0-9a-f]{16})(\.[^.]*)?$`)

// Handler serves the tree with the fingerprinting contract:
//
//   - a hashed name matching the file's current content is immutable —
//     Cache-Control: public, max-age=31536000, immutable — because that
//     exact URL can never serve different bytes;
//   - a hashed name of a slice of the file as it is now (Stylesheet)
//     is that slice, immutable on the same terms;
//   - a hashed name that no longer matches (a stale page asking for an
//     old version, or an old version's slice) serves the *current*
//     whole content with no-cache: a
//     slightly-stale stylesheet on a stale page beats a 404;
//   - a bare name serves no-cache, so deep links keep working;
//   - a real file whose name merely looks hashed wins over
//     hash-stripping.
//
// Mount it where the FS layout says — for the scaffold's embedded
// static/:
//
//	mux.Handle("GET /static/", assets.Handler())
func (assets *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if assets.serveFile(writer, request, name, "") {
			return
		}
		dir, base := path.Split(name)
		match := hashedName.FindStringSubmatch(base)
		if match == nil {
			http.NotFound(writer, request)
			return
		}
		original := dir + match[1] + match[3]
		if assets.serveSlice(writer, request, original, match[2]) {
			return
		}
		if !assets.serveFile(writer, request, original, match[2]) {
			http.NotFound(writer, request)
		}
	})
}

// serveFile writes name's content, reporting whether name resolved to a
// servable file. The answer is immutable only when the bytes it sends
// hash to hashInURL, and no-cache otherwise: the policy is chosen from
// the bytes written, so a file edited mid-request is never cached for a
// year under the old version's URL. ServeContent
// picks the Content-Type from the name's extension and handles ranges.
//
// Every answer carries the content's fingerprint as a strong ETag, so
// the no-cache answers — a bare name, a legacy ?v= link, a stale hash
// — revalidate to a 304 instead of downloading the file again on every
// page view; ServeContent answers If-None-Match from it. The value is
// the same 16-hex hash Path puts in the URL, computed from the bytes
// being written, so the validator can never describe different bytes
// from the ones it arrived with. Tito Go's theme assets carry the same
// ETag for the same content.
//
// A vendored file is served from its cached entry, minified, or gzipped
// for a client that accepts it (serveVendored).
func (assets *Assets) serveFile(
	writer http.ResponseWriter,
	request *http.Request,
	name, hashInURL string,
) bool {
	info, err := fs.Stat(assets.fsys, name)
	if err != nil || info.IsDir() {
		return false
	}
	if vendored.Is(name) {
		return assets.serveVendored(writer, request, name, hashInURL)
	}
	body, err := fs.ReadFile(assets.fsys, name)
	if err != nil {
		return false
	}
	hash := fingerprint(body)
	writer.Header().Set("Cache-Control", cachePolicy(hash, hashInURL))
	writer.Header().Set("ETag", `"`+hash+`"`)
	http.ServeContent(
		writer, request, name, info.ModTime(), bytes.NewReader(body),
	)
	return true
}
