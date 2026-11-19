//go:build browser

package ui

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// TestEveryShellPageLooksTheSameOnItsSlice holds each shell's plain,
// select, date and live pages, in each theme and both schemes, to the
// same computed style on the slice ui.Pages links as on the whole
// tokens.css: every element's, and its ::before's and ::after's,
// property by property. What it does not compare: an open listbox or
// calendar popup, a coarse pointer, forced colours, print, and a shell
// written right to left, as these pages are rendered in English only.
//
// It sits here rather than beside the gallery's previews because ui's
// tests build these pages: shellContents rendered through
// PagesConfig.Assets as an app renders them, so the slice compared is
// the one an app's visitors are sent. The comparison itself is
// galleryrig's, the previews' own, so the two are held to one idea of
// the same.
func TestEveryShellPageLooksTheSameOnItsSlice(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+galleryrig.SameLookPath,
		galleryrig.ServeSameLookHost)
	// The browser asks for a favicon on its own; no page links one.
	mux.HandleFunc("GET /favicon.ico", func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusNoContent)
	})
	var pairs []galleryrig.LookPair
	for _, theme := range ThemeNames() {
		pairs = append(pairs, serveShellPages(t, mux, theme)...)
	}
	rig := harness.New(t, func(string) http.Handler {
		return failedRequests(t, mux)
	})
	galleryrig.SameLooks(t, rig, pairs)
}

// serveShellPages serves, under /theme, each shell's pages as an app
// in theme sends them, linking their slices, and again with ?whole,
// linking the whole tokens.css in the slice's place.
func serveShellPages(
	t *testing.T, mux *http.ServeMux, theme string,
) []galleryrig.LookPair {
	t.Helper()
	base := "/" + theme
	assets := themeAssets(t, theme)
	mux.Handle(base+"/static/", http.StripPrefix(base, assets.Handler()))
	whole := base + assets.Path("static/tokens.css")
	var pairs []galleryrig.LookPair
	for _, shell := range LayoutNames() {
		layout, _ := Layout(shell)
		pages := renderPagesWith(t, layout, shellContents,
			sliced(assets, base))
		for _, name := range slices.Sorted(maps.Keys(pages)) {
			page, address := pages[name], base+"/"+shell+"/"+name
			slice := linkedTokens(page)
			if slice == "" || slice == whole ||
				strings.Count(page, `href="`+slice+`"`) != 1 {
				t.Fatalf("%s links %q, want a slice, once", address, slice)
			}
			forms := map[bool][]byte{
				false: []byte(page),
				true: []byte(strings.Replace(page, `href="`+slice+`"`,
					`href="`+whole+`"`, 1)),
			}
			mux.HandleFunc("GET "+address, func(
				writer http.ResponseWriter, request *http.Request,
			) {
				writer.Header().Set("Content-Type", "text/html; charset=utf-8")
				writer.Write(forms[request.URL.RawQuery == "whole"])
			})
			pairs = append(pairs, galleryrig.LookPair{
				Name:  theme + "/" + shell + "/" + name,
				Whole: address + "?whole", Sliced: address,
			})
		}
	}
	return pairs
}

// themeAssets is an app's static directory as rastrillo new writes it
// for theme: the whole vendored set, theme.css included.
func themeAssets(t *testing.T, theme string) *rastrillo.Assets {
	t.Helper()
	files := fstest.MapFS{}
	set, ok := VendoredAssets(theme)
	if !ok {
		t.Fatalf("no vendored set for %s", theme)
	}
	for name, body := range set {
		files["static/"+name] = &fstest.MapFile{Data: body}
	}
	return rastrillo.NewAssets(files)
}

// failedRequests serves handler and fails t, once the drive is done,
// for each request answered with an error: a page's missing file
// would load nothing in both forms alike, and compare equal.
func failedRequests(t *testing.T, handler http.Handler) http.Handler {
	t.Helper()
	var mutex sync.Mutex
	var failed []string
	t.Cleanup(func() {
		mutex.Lock()
		defer mutex.Unlock()
		if len(failed) > 0 {
			t.Errorf("%d request(s) failed: %v", len(failed), failed)
		}
	})
	return http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		recorder := &statusRecorder{ResponseWriter: writer}
		handler.ServeHTTP(recorder, request)
		if recorder.status >= http.StatusBadRequest {
			mutex.Lock()
			failed = append(failed, request.Method+" "+request.URL.String()+
				" "+http.StatusText(recorder.status))
			mutex.Unlock()
		}
	})
}

// statusRecorder notes the status a handler answers with.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}
