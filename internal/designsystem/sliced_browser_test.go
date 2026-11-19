//go:build browser

package designsystem

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/ui"
)

// slicedLocales are the languages the previews are compared in: one
// written left to right and the one written right to left, whose rules
// under [dir="rtl"] and :dir(rtl) a slice must keep as the whole file
// has them. The other locales change only the words.
var slicedLocales = []string{"en", "ar"}

// TestEveryPreviewLooksTheSameOnItsSlice is the evidence that slicing
// tokens.css changes nothing a reader sees. Every preview document, in
// each theme and both schemes, is loaded once on the whole stylesheet
// and once on the slice its hooks and its scripts' hooks keep, made as
// an app's Assets makes it, from the minified file; each element's
// computed style, and its ::before's and ::after's, must be the same
// on both, property by property. What it does not compare: an open
// listbox or calendar popup, a coarse pointer, forced colours, print,
// and the shells, which ui's drive holds in their own pages.
func TestEveryPreviewLooksTheSameOnItsSlice(t *testing.T) {
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	tree := galleryrig.Tree(t, files, mountPrefix)
	served := httptest.NewRecorder()
	tree.ServeHTTP(served,
		httptest.NewRequest(http.MethodGet, assetHref("tokens.css"), nil))
	if served.Code != http.StatusOK {
		t.Fatalf("the tree answers tokens.css with %d", served.Code)
	}
	whole, _ := io.ReadAll(served.Body)
	sliced := slicedPreviews(t, files, whole)
	if len(sliced.documents) == 0 {
		t.Fatal("no preview documents to compare")
	}
	rig := harness.New(t, func(string) http.Handler {
		return sliced.handler(tree)
	})
	var pairs []galleryrig.LookPair
	for _, name := range slices.Sorted(maps.Keys(sliced.documents)) {
		address := mountPrefix + name
		pairs = append(pairs, galleryrig.LookPair{
			Name: name, Whole: address, Sliced: address + "?sliced",
		})
	}
	galleryrig.SameLooks(t, rig, pairs)
}

// previewSlices is each preview document as it reads on its slice, by
// tree path, and each slice by the address those documents link it at.
type previewSlices struct {
	documents map[string][]byte
	slices    map[string][]byte
}

// handler serves the tree, a document's sliced form when it is asked
// for with ?sliced, the slices, and the comparison's host page.
func (sliced previewSlices) handler(tree http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+galleryrig.SameLookPath,
		galleryrig.ServeSameLookHost)
	mux.HandleFunc("/", func(
		writer http.ResponseWriter, request *http.Request,
	) {
		name := strings.TrimPrefix(request.URL.Path, mountPrefix)
		if document, ok := sliced.documents[name]; ok &&
			request.URL.RawQuery == "sliced" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Write(document)
			return
		}
		if slice, ok := sliced.slices[request.URL.Path]; ok {
			writer.Header().Set("Content-Type", "text/css; charset=utf-8")
			writer.Write(slice)
			return
		}
		tree.ServeHTTP(writer, request)
	})
	return mux
}

var (
	inlineStyle = regexp.MustCompile(`(?s)<style>.*?</style>`)
	scriptSrc   = regexp.MustCompile(`<script[^>]*\ssrc="([^"]*)"`)
)

// slicedPreviews slices whole, the minified tokens.css the tree serves,
// for each preview in slicedLocales: to the hooks its markup names and
// those its scripts create. The gallery's own <style> is left out of
// the reading: it is CSS, not markup, and the hooks its selectors name
// would keep rules no element on the page can match.
func slicedPreviews(
	t *testing.T, files map[string][]byte, whole []byte,
) previewSlices {
	t.Helper()
	plainNames := map[string]string{}
	for plain, hashed := range hashedNames() {
		plainNames[hashed] = plain
	}
	wholeLink := `href="` + assetHref("tokens.css") + `"`
	sliced := previewSlices{
		documents: map[string][]byte{}, slices: map[string][]byte{},
	}
	addresses := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		document := string(files[name])
		if !isPreviewDoc(document) || !inSlicedLocale(name) {
			continue
		}
		hooks := map[string]bool{}
		markup := inlineStyle.ReplaceAllString(document, "")
		for _, hook := range vendored.HooksIn([]byte(markup)) {
			hooks[hook] = true
		}
		for _, match := range scriptSrc.FindAllStringSubmatch(document, -1) {
			plain, ok := plainNames[path.Base(match[1])]
			if !ok {
				t.Fatalf("%s loads %s, which names no file in the tree",
					name, match[1])
			}
			for _, hook := range vendored.CreatedHooks(plain) {
				hooks[hook] = true
			}
		}
		slice, err := vendored.Slice(whole, hooks)
		if err != nil {
			t.Fatalf("%s: slicing tokens.css: %v", name, err)
		}
		if len(slice) >= len(whole) {
			t.Errorf("%s keeps all of tokens.css: its slice proves nothing",
				name)
		}
		address, ok := addresses[string(slice)]
		if !ok {
			address = fmt.Sprintf("%stokens.slice-%d.css", mountPrefix,
				len(addresses))
			addresses[string(slice)] = address
			sliced.slices[address] = slice
		}
		if strings.Count(document, wholeLink) != 1 {
			t.Fatalf("%s links tokens.css %d times, want once", name,
				strings.Count(document, wholeLink))
		}
		sliced.documents[name] = []byte(strings.Replace(document,
			wholeLink, `href="`+address+`"`, 1))
	}
	return sliced
}

// inSlicedLocale reports whether the tree path name, a preview's, is in
// one of ui's themes and one of slicedLocales.
func inSlicedLocale(name string) bool {
	parts := strings.SplitN(name, "/", 3)
	return len(parts) == 3 && slices.Contains(ui.ThemeNames(), parts[0]) &&
		slices.Contains(slicedLocales, parts[1])
}
