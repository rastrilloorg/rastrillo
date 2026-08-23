//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The origin chicken-and-egg, resolved: build gets the localhost
// origin before the server exists, the page renders it, and the
// browser reads it back off the page it navigated — one loop through
// listener, handler and Chromium proving the boot order.
func TestNewHandsBuildTheOriginBeforeServing(t *testing.T) {
	var gotOrigin string
	r := New(t, func(origin string) http.Handler {
		gotOrigin = origin
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>rig</title></head><body><p id="origin">%s</p></body></html>`, origin)
		})
		return mux
	})
	if !strings.HasPrefix(r.Origin, "http://localhost:") {
		t.Fatalf("rig origin %q is not a localhost origin — an IP is not a WebAuthn RP ID", r.Origin)
	}
	if gotOrigin != r.Origin {
		t.Fatalf("build was handed %q, the rig navigates %q", gotOrigin, r.Origin)
	}
	var onPage string
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible("#origin", chromedp.ByQuery),
		chromedp.Text("#origin", &onPage, chromedp.ByQuery),
	)
	if onPage != r.Origin {
		t.Fatalf("page shows %q, want %q", onPage, r.Origin)
	}
}
