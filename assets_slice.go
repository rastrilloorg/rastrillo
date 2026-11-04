package rastrillo

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// slicesOfFile holds the slices cut from one version of a vendored
// stylesheet, by the hook set that asked for each and by the hash its
// URL carries. A new version of the file replaces the whole record, so
// no slice of the old rules outlives the rules themselves.
//
// The record grows by one slice per distinct hook set, and hook sets
// come from the app's pages, not from requests, so it is bounded by the
// app's pages.
type slicesOfFile struct {
	source     string
	unreadable bool
	byHooks    map[string]assetInfo
	byHash     map[string]assetInfo
}

// Stylesheet returns the URL path of a vendored stylesheet sliced to
// the rules a page carrying hooks can show, shaped like Path's and
// fingerprinted by the slice's bytes, so a browser caches it as it
// caches any hashed file and pages with the same hooks share it:
//
//	Stylesheet("static/tokens.css", hooks) → "/static/tokens.<hash>.css"
//
// It returns Path(name), the whole file, for a file that is not a
// vendored stylesheet, is missing, or cannot be sliced: a page styled
// by the whole file beats a page with an empty or truncated one. The
// last is logged once per version of the file.
func (assets *Assets) Stylesheet(name string, hooks []string) string {
	if !vendored.Is(name) || path.Ext(name) != ".css" {
		return assets.Path(name)
	}
	entry, err := assets.lookup(name)
	if err != nil {
		return assets.Path(name)
	}
	assets.slicing.Lock()
	defer assets.slicing.Unlock()
	record := assets.slicesOf(name, entry.hash)
	if record.unreadable {
		return hashedPath(name, entry.hash)
	}
	key := hookKey(hooks)
	slice, ok := record.byHooks[key]
	if !ok {
		slice, err = sliceOf(entry, key)
		if err != nil {
			record.unreadable = true
			slog.Default().Warn("rastrillo: stylesheet cannot be "+
				"sliced; every page links it whole",
				"file", name, "err", err)
			return hashedPath(name, entry.hash)
		}
		if shared, ok := record.byHash[slice.hash]; ok {
			slice = shared
		}
		record.byHooks[key] = slice
		record.byHash[slice.hash] = slice
	}
	return hashedPath(name, slice.hash)
}

// slicesOf returns name's record for the version hashed source,
// replacing one kept for an older version. The caller holds slicing.
func (assets *Assets) slicesOf(name, source string) *slicesOfFile {
	record, ok := assets.slices[name]
	if !ok || record.source != source {
		record = &slicesOfFile{
			source:  source,
			byHooks: make(map[string]assetInfo),
			byHash:  make(map[string]assetInfo),
		}
		assets.slices[name] = record
	}
	return record
}

// hookKey names a hook set the same whatever order or repeats it
// arrives in. Only hook names and the values a page writes for them are
// kept, since only they choose rules; a value may hold a space, and
// none a newline (ShownValue quotes it), so a newline joins them.
func hookKey(hooks []string) string {
	kept := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		if vendored.IsHook(hook) || vendored.IsShownValue(hook) {
			kept = append(kept, hook)
		}
	}
	slices.Sort(kept)
	return strings.Join(slices.Compact(kept), "\n")
}

// sliceOf cuts the slice for the hooks in key from entry's minified
// bytes, which is what the slicer reads, and gzips it once here rather
// than per request. A slice that keeps nothing is the whole file, as
// servedForms keeps a file the minifier empties: a stylesheet that
// arrives empty looks like a broken deploy.
func sliceOf(entry assetInfo, key string) (assetInfo, error) {
	hooks := make(map[string]bool)
	for hook := range strings.SplitSeq(key, "\n") {
		hooks[hook] = true
	}
	sliced, err := vendored.Slice(entry.served, hooks)
	if err != nil {
		return assetInfo{}, err
	}
	if len(sliced) == 0 {
		return entry, nil
	}
	return assetInfo{
		hash:    fingerprint(sliced),
		mtime:   entry.mtime,
		size:    int64(len(sliced)),
		served:  sliced,
		gzipped: gzipOf(sliced),
	}, nil
}

// serveSlice writes the slice of name whose bytes hash to hashInURL,
// reporting whether there is one. Only a slice of the file as it is now
// counts: a URL for a slice of an older version falls through to
// serveFile, which answers it with the current whole file and no-cache.
func (assets *Assets) serveSlice(
	writer http.ResponseWriter,
	request *http.Request,
	name, hashInURL string,
) bool {
	entry, err := assets.lookup(name)
	if err != nil {
		return false
	}
	var slice assetInfo
	found := false
	assets.slicing.Lock()
	record, ok := assets.slices[name]
	if ok && record.source == entry.hash {
		slice, found = record.byHash[hashInURL]
	}
	assets.slicing.Unlock()
	if !found {
		return false
	}
	writeVendored(writer, request, name, hashInURL, slice)
	return true
}

// Has reports whether name is a file in the tree, so a page can leave
// out a link to a file the app has deleted.
func (assets *Assets) Has(name string) bool {
	info, err := fs.Stat(assets.fsys, name)
	return err == nil && !info.IsDir()
}
