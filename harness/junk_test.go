//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func staticPage(body string) func(string) http.Handler {
	return func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>junk</title></head><body>%s</body></html>`, body)
		})
		return mux
	}
}

// The three places junk hides: rendered text, an input's value, and
// the label a screen reader announces — each hit shown with its
// surroundings, so a prose false-positive is legible at a glance.
func TestJunkScanReadsTextInputsAndARIA(t *testing.T) {
	r := New(t, staticPage(`<main id="app">
<p>the price is undefined today</p>
<input value="[object Object]">
<button aria-label="null">x</button>
</main>`))
	r.Run(chromedp.Navigate(r.Origin+"/"), chromedp.WaitVisible("#app", chromedp.ByQuery))
	hits := r.junkHits("#app")
	joined := strings.Join(hits, "\n")
	for _, want := range []string{
		`undefined in: "the price is undefined today`,
		"[object Object]",
		"null",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("scan missed %q:\n%s", want, joined)
		}
	}
	if len(hits) != 3 {
		t.Errorf("want 3 hits, got %d:\n%s", len(hits), joined)
	}
}

// AllowText spares deliberate prose; the junk value stays banned
// everywhere else on the screen.
func TestAllowTextSparesDeliberateProse(t *testing.T) {
	r := New(t, staticPage(`<main id="app"><p>this contract is null and void</p></main>`))
	r.AllowText("null and void")
	r.Run(chromedp.Navigate(r.Origin+"/"), chromedp.WaitVisible("#app", chromedp.ByQuery))
	if hits := r.junkHits("#app"); len(hits) > 0 {
		t.Errorf("allowed prose still hit: %v", hits)
	}
	r.Screen("#app", "the prose screen") // and the full gate passes too
}

// A missing scan root is itself a finding, never a silent pass.
func TestJunkScanReportsAMissingRoot(t *testing.T) {
	r := New(t, staticPage(`<p id="up">no app root here</p>`))
	r.Run(chromedp.Navigate(r.Origin+"/"), chromedp.WaitVisible("#up", chromedp.ByQuery))
	hits := r.junkHits("#app")
	if len(hits) != 1 || !strings.Contains(hits[0], "#app") {
		t.Errorf("missing root not reported: %v", hits)
	}
}

// The whole gate, green on a clean page: wait, scan, flush.
func TestScreenPassesACleanPage(t *testing.T) {
	r := New(t, staticPage(`<main id="app"><h1>all well</h1></main>`))
	r.Run(chromedp.Navigate(r.Origin + "/"))
	r.Screen("#app", "clean page")
}
