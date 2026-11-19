//go:build browser

package galleryrig

import (
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// Tree serves a rendered tree at the mount its pages expect, and fails
// the test that uses it if anything asked for a file the tree does not
// have. Every URL in a page is absolute under the mount, so serving the
// tree anywhere else would 404 the stylesheet and a drive would measure
// an unstyled page; and a preview frame naming a missing file loads a
// 404 page that settles, measures and scans like any other document, so
// the miss has to be counted here.
//
// The failure is reported from t.Cleanup rather than from the request:
// lazy frames can still be loading after the test function returns,
// and t.Errorf from a server goroutine after that panics.
func Tree(
	t *testing.T, files map[string][]byte, mountPrefix string,
) http.Handler {
	t.Helper()
	var mu sync.Mutex
	var missing []string
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if len(missing) > 0 {
			t.Errorf("the tree was asked for %d file(s) it does not have: %v",
				len(missing), missing)
		}
	})
	return TreeRecording(t, files, mountPrefix, func(asked string) {
		mu.Lock()
		missing = append(missing, asked)
		mu.Unlock()
	})
}

// TreeRecording is Tree with each miss handed to the caller, so the
// recording itself can be tested. It serves the framework's files
// minified, as rastrillo.Assets serves them to an app, so every drive
// through it runs on the bytes an app's visitors get.
func TreeRecording(
	t *testing.T,
	files map[string][]byte,
	mountPrefix string,
	miss func(path string),
) http.Handler {
	t.Helper()
	types := map[string]string{
		".html": "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "text/javascript; charset=utf-8",
	}
	files = minifiedFramework(files)
	return http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		// The browser asks for /favicon.ico on its own; nothing outside
		// the mount is the tree's to answer.
		if !strings.HasPrefix(request.URL.Path, mountPrefix) {
			http.NotFound(writer, request)
			return
		}
		name := strings.TrimPrefix(request.URL.Path, mountPrefix)
		body, ok := files[name]
		if !ok {
			miss(request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		if contentType, ok := types[path.Ext(name)]; ok {
			writer.Header().Set("Content-Type", contentType)
		}
		writer.Write(body)
	})
}

// fingerprinted is a tree asset's name, name.<16 hex>.ext, with the
// pieces that put the plain name back together.
var fingerprinted = regexp.MustCompile(`^(.+)\.[0-9a-f]{16}(\.[^./]+)$`)

// minifiedFramework is files with every framework stylesheet and
// script minified: a vendored name, or a theme-<name>.css, under its
// fingerprint. The gallery's own files, its pages and its pictures are
// left alone, and so is a file the minifier refuses, as Assets does.
func minifiedFramework(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for name, body := range files {
		out[name] = body
		match := fingerprinted.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		plain := match[1] + match[2]
		theme := strings.HasPrefix(plain, "theme-") && match[2] == ".css"
		if !theme && !slices.Contains(vendored.Names(), plain) {
			continue
		}
		minified, err := vendored.Minify(plain, body)
		if err == nil && len(minified) > 0 {
			out[name] = minified
		}
	}
	return out
}

// WithoutAnchorPositioning replaces the page's tokens.css with a copy
// whose anchor-positioning @supports condition can never hold, which is
// what an engine without the feature gets: the menu panel positioned
// absolutely under its summary instead of fixed against it. The match
// allows any spacing because the tree serves tokens.css minified, as
// apps do, and the minifier drops the spaces the source has.
const WithoutAnchorPositioning = `(async () => {
  const link = document.querySelector('link[href*="/tokens."][href$=".css"]');
  const css = await (await fetch(link.href)).text();
  const style = document.createElement("style");
  style.textContent = css.replace(
    /@supports\s*\(position-area:\s*block-end\)\s*and\s*` +
	`\(position-try-fallbacks:\s*flip-block\)/g,
    "@supports (position-area: rastrillo-no-such-value)");
  link.replaceWith(style);
  return true;
})()`
