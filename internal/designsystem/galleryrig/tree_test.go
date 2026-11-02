//go:build browser

package galleryrig

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The tree handler is what makes a missing preview file loud: a frame
// whose src names nothing loads a 404 page, which settles, measures and
// scans like any other document. The control asks for a file the tree
// does not have and requires the request to be recorded, while an
// asset that exists and a request outside the mount are not misses.
func TestTheTreeHandlerRecordsAMissingFile(t *testing.T) {
	const prefix = "/design-system/"
	files := map[string][]byte{"tokens.css": []byte(":root{}")}
	var missing []string
	h := TreeRecording(t, files, prefix, func(path string) { missing = append(missing, path) })
	for _, path := range []string{prefix + "tokens.css", prefix + "day/en/form/no-such-preview.html", "/favicon.ico"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if len(missing) != 1 || missing[0] != prefix+"day/en/form/no-such-preview.html" {
		t.Errorf("recorded %v, want exactly the missing preview", missing)
	}
}

// The gallery's drives run on the framework's files as apps serve
// them, minified; the gallery's own files are served as written.
func TestTheTreeServesTheFrameworksFilesMinified(t *testing.T) {
	const prefix = "/design-system/"
	commented := []byte("/* why */\nbody {\n  color: red;\n}\n")
	files := map[string][]byte{
		"tokens.0123456789abcdef.css":    commented,
		"theme-day.0123456789abcdef.css": commented,
		"gallery.0123456789abcdef.css":   commented,
	}
	handler := Tree(t, files, prefix)
	for name, wantComment := range map[string]bool{
		"tokens.0123456789abcdef.css":    false,
		"theme-day.0123456789abcdef.css": false,
		"gallery.0123456789abcdef.css":   true,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", prefix+name, nil)
		handler.ServeHTTP(recorder, request)
		body := recorder.Body.String()
		if strings.Contains(body, "why") != wantComment {
			t.Errorf("%s served as %q", name, body)
		}
	}
}
