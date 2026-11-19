package rastrillo

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const twoHookCSS = "[rst-a]{color:red}[rst-b]{color:blue}"

func twoHookTree(css string) fstest.MapFS {
	return fstest.MapFS{"static/tokens.css": {Data: []byte(css)}}
}

func TestStylesheetLinksASliceAndServesIt(t *testing.T) {
	assets := NewAssets(twoHookTree(twoHookCSS))
	href := assets.Stylesheet("static/tokens.css", []string{"rst-a"})
	whole := assets.Path("static/tokens.css")
	if href == whole || !hashedCSS.MatchString(href) {
		t.Fatalf("Stylesheet gave %q, want a hashed URL other than %q",
			href, whole)
	}
	plain := getAsset(t, assets, href, "")
	body := plain.Body.String()
	if plain.Code != 200 || !strings.Contains(body, "rst-a") ||
		strings.Contains(body, "rst-b") {
		t.Fatalf("got %d %q, want 200 and only rst-a's rule",
			plain.Code, body)
	}
	if got := plain.Header().Get("Cache-Control"); got !=
		"public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control %q, want immutable", got)
	}
	hash := fingerprintOf(plain.Body.Bytes())
	if !strings.Contains(href, "."+hash+".") ||
		plain.Header().Get("ETag") != `"`+hash+`"` {
		t.Errorf("URL %s and ETag %s, want both to carry %s",
			href, plain.Header().Get("ETag"), hash)
	}
	zipped := getAsset(t, assets, href, "gzip")
	reader, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatalf("gzip answer does not unzip: %v", err)
	}
	unzipped, _ := io.ReadAll(reader)
	if string(unzipped) != body ||
		zipped.Header().Get("Content-Encoding") != "gzip" ||
		zipped.Header().Get("Vary") != "Accept-Encoding" ||
		zipped.Header().Get("ETag") != `"`+hash+`-gzip"` {
		t.Errorf("gzip answer %q with headers %v, want the slice gzipped",
			unzipped, zipped.Header())
	}
}

func TestStylesheetSharesASliceBetweenEqualHookSets(t *testing.T) {
	assets := NewAssets(twoHookTree(twoHookCSS))
	first := assets.Stylesheet(
		"static/tokens.css", []string{"rst-a", "rst-b"})
	second := assets.Stylesheet(
		"static/tokens.css", []string{"rst-b", "rst-a"})
	if first != second {
		t.Errorf("equal hook sets linked %s and %s", first, second)
	}
}

// Review Focus 1: a stylesheet the slicer cannot read is linked and
// served whole on every page, and the fallback is logged once for that
// version of the file, not once per page. Each of these survives the
// minifier unchanged and still cannot be read.
func TestAnUnreadableStylesheetIsLinkedWhole(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)
	for _, unreadable := range []string{
		"[rst-a]{color:red}@unknown-thing x{[rst-b]{color:blue}}",
		"p{a:(]}[rst-a]{color:red}",
		"[rst-a]{color:red;@media (x){color:blue}}",
		// :not() escaped: read as written, it would need rst-b.
		`[rst-a]{color:red}p:n\6ft([rst-b]){color:blue}`,
	} {
		logged.Reset()
		assets := NewAssets(twoHookTree(unreadable))
		whole := assets.Path("static/tokens.css")
		served := getAsset(t, assets, whole, "").Body.String()
		for _, hooks := range [][]string{{"rst-a"}, {"rst-b"}} {
			href := assets.Stylesheet("static/tokens.css", hooks)
			body := getAsset(t, assets, href, "").Body.String()
			if href != whole || body != served {
				t.Errorf("%s: Stylesheet(%v) linked %s serving %q, "+
					"want %s serving %q", unreadable, hooks, href, body,
					whole, served)
			}
		}
		if lines := strings.Count(logged.String(), "\n"); lines != 1 {
			t.Errorf("%s: logged %d lines, want the fallback once:\n%s",
				unreadable, lines, logged.String())
		}
	}
}

// An unclosed block is closed by the minifier, as a browser closes it
// at the end of the file, so the page's slice keeps the rule rather
// than arriving empty or cut short.
func TestAnUnclosedBlockIsClosedNotCutShort(t *testing.T) {
	assets := NewAssets(twoHookTree("[rst-a]{color:red"))
	href := assets.Stylesheet("static/tokens.css", []string{"rst-a"})
	body := getAsset(t, assets, href, "").Body.String()
	if body != "[rst-a]{color:red}" {
		t.Errorf("%s served %q, want the rule closed", href, body)
	}
}

// Review Focus 3: an app serving a live directory edits tokens.css
// while running. The next page links a new slice, and the old slice's
// URL is never again answered as a year-cached copy of the old rules.
func TestAnEditedStylesheetGetsANewSliceURL(t *testing.T) {
	tree := twoHookTree(twoHookCSS)
	assets := NewAssets(tree)
	hooks := []string{"rst-a"}
	before := assets.Stylesheet("static/tokens.css", hooks)
	edited := "[rst-a]{color:green}[rst-b]{color:blue}"
	tree["static/tokens.css"] = &fstest.MapFile{
		Data: []byte(edited), ModTime: time.Unix(1, 0),
	}
	checkStale := func(when string) {
		t.Helper()
		stale := getAsset(t, assets, before, "")
		if got := stale.Header().Get("Cache-Control"); got != "no-cache" ||
			stale.Body.String() != edited {
			t.Errorf("%s, the old slice's URL answered %q with %q, "+
				"want no-cache and the whole current file",
				when, got, stale.Body.String())
		}
	}
	checkStale("before the next page asks for a slice")
	after := assets.Stylesheet("static/tokens.css", hooks)
	if after == before {
		t.Fatalf("after the edit Stylesheet still gave %s", before)
	}
	checkStale("after it does")
}

// A slice that keeps nothing would arrive empty, which looks like a
// broken deploy, so the page links the whole file instead.
func TestAnEmptySliceIsLinkedWhole(t *testing.T) {
	assets := NewAssets(twoHookTree(twoHookCSS))
	href := assets.Stylesheet("static/tokens.css", []string{"rst-c"})
	if whole := assets.Path("static/tokens.css"); href != whole {
		t.Errorf("Stylesheet gave %s, want the whole file's %s", href, whole)
	}
}

func TestHasNamesOnlyFilesInTheTree(t *testing.T) {
	assets := NewAssets(twoHookTree(twoHookCSS))
	for name, want := range map[string]bool{
		"static/tokens.css": true,
		"static/light.js":   false,
		"static":            false,
	} {
		if got := assets.Has(name); got != want {
			t.Errorf("Has(%q) = %v, want %v", name, got, want)
		}
	}
}
