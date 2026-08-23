//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// probeBuild serves a page that walks into every watcher on purpose:
// optionally a console error, then a 500 the page asked for with POST
// (so the method-correlation is provable), then a plain 404 — and
// reports #done only after the fetches settle, which is what the test
// synchronises on.
func probeBuild(withConsoleError bool) func(string) http.Handler {
	return func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /boom", func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			noise := ""
			if withConsoleError {
				noise = `console.error("kaboom");`
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>probe</title></head><body><p id="up">up</p><script>
(async () => {
  %s
  await fetch("/boom", { method: "POST" });
  await fetch("/missing");
  const done = document.createElement("p");
  done.id = "done";
  done.textContent = "done";
  document.body.append(done);
})();
</script></body></html>`, noise)
		})
		return mux
	}
}

// waitForProblems polls until at least n problems accumulated: CDP
// event delivery is asynchronous, and a fixed sleep is either flaky or
// slow. Returns whatever accumulated by the deadline either way — the
// caller's assertions name what is missing.
func waitForProblems(t *testing.T, r *Rig, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		got := len(r.problems)
		r.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			return r.take()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWatchersCollectEveryLoudFailure(t *testing.T) {
	r := New(t, probeBuild(true))
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible("#done", chromedp.ByQuery),
	)
	probs := waitForProblems(t, r, 3)
	joined := strings.Join(probs, "\n")
	for _, want := range []string{
		"console.error: kaboom",
		// The method rides in from requestWillBeSent, correlated by
		// RequestID — a response has no method of its own.
		"HTTP 500 POST",
		"/boom",
		"HTTP 404 GET",
		"/missing",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems are missing %q:\n%s", want, joined)
		}
	}
}

// Allow quietens the response AND the log-domain mirror Chromium emits
// alongside it — the mirror carries only a URL, so it is matched by
// path. With every probe allowed, the drive must end silent.
func TestAllowQuietensExpectedProbesAndTheirMirrors(t *testing.T) {
	r := New(t, probeBuild(false))
	r.Allow(http.MethodPost, "/boom", http.StatusInternalServerError)
	r.Allow(http.MethodGet, "/missing", http.StatusNotFound)
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible("#done", chromedp.ByQuery),
	)
	// The fetches settled before #done appeared, but give straggler
	// events a beat before declaring silence.
	time.Sleep(500 * time.Millisecond)
	if probs := r.take(); len(probs) > 0 {
		t.Errorf("allowed probes still reported problems:\n%s", strings.Join(probs, "\n"))
	}
}

// The browser probes /favicon.ico on its own; New pre-allows the 404
// so every app doesn't rediscover it. The allowance is data, so this
// needs no favicon request to prove the wiring.
func TestFaviconProbeIsPreAllowed(t *testing.T) {
	r := New(t, probeBuild(false))
	if !r.responseAllowed(http.MethodGet, r.Origin+"/favicon.ico", http.StatusNotFound) {
		t.Error("GET /favicon.ico 404 is not pre-allowed")
	}
	if !r.logEntryAllowed(r.Origin + "/favicon.ico") {
		t.Error("the favicon 404's log-domain mirror is not pre-allowed")
	}
}
