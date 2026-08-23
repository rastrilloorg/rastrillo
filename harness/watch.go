//go:build browser

package harness

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/go-json-experiment/json/jsontext"
)

// Allow registers an expected probe: responses with this method, path
// and status stop being problems — and so do the console-error mirrors
// Chromium logs for them, which arrive via the CDP log domain carrying
// only a URL and so are matched by path alone.
func (r *Rig) Allow(method, path string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allows = append(r.allows, allowance{method: method, path: path, status: status})
}

func (r *Rig) add(problem string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems = append(r.problems, problem)
}

// take drains the accumulated problems — Screen's flush.
func (r *Rig) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.problems
	r.problems = nil
	return out
}

// consoleArgText renders one console-call argument's CDP value for a
// problem line. A JS string arrives as a JSON-quoted value — so
// console.error("kaboom") would otherwise read `"kaboom"`, quotes and
// all — so a string is unquoted first; anything else (number, object,
// ...) is reported as its raw JSON text, which is already readable.
func consoleArgText(v jsontext.Value) string {
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}
	return string(v)
}

func pathOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Path
}

func (r *Rig) responseAllowed(method, rawURL string, status int) bool {
	p := pathOf(rawURL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.allows {
		if a.method == method && a.path == p && a.status == status {
			return true
		}
	}
	return false
}

// logEntryAllowed matches a CDP log entry by URL alone: the log
// domain's mirror of an HTTP failure carries no method or status, so
// an allowed path excuses its mirror too.
func (r *Rig) logEntryAllowed(rawURL string) bool {
	p := pathOf(rawURL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.allows {
		if a.path == p {
			return true
		}
	}
	return false
}

func (r *Rig) requestInfoFor(id network.RequestID) requestInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[id]
}

// watch wires the loud-failure listeners: console error/assert, thrown
// exceptions, failed requests, responses >= 400 — all accumulate into
// the problem list Screen flushes. Three chromedp-specific facts this
// port honors: a 4xx/5xx response also surfaces as a console-error
// MIRROR, which in CDP arrives via log.EventEntryAdded, not
// runtime.EventConsoleAPICalled; a response has no method of its own,
// so it is correlated from network.EventRequestWillBeSent by
// RequestID; and the browser probes /favicon.ico by itself, which New
// pre-allows. chromedp enables the log and network domains on target
// attach, so there is nothing to switch on here.
func (r *Rig) watch() {
	chromedp.ListenTarget(r.ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type == "error" || e.Type == "assert" {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, consoleArgText(a.Value))
				}
				r.add("console." + string(e.Type) + ": " + strings.Join(parts, " "))
			}
		case *runtime.EventExceptionThrown:
			r.add("uncaught: " + e.ExceptionDetails.Error())
		case *cdplog.EventEntryAdded:
			if e.Entry.Level != cdplog.LevelError {
				return
			}
			if r.logEntryAllowed(e.Entry.URL) {
				return
			}
			r.add(fmt.Sprintf("log.error: %s (%s)", e.Entry.Text, e.Entry.URL))
		case *network.EventRequestWillBeSent:
			r.mu.Lock()
			r.requests[e.RequestID] = requestInfo{method: e.Request.Method, url: e.Request.URL}
			r.mu.Unlock()
		case *network.EventResponseReceived:
			status := int(e.Response.Status)
			if status < http.StatusBadRequest {
				return
			}
			method := r.requestInfoFor(e.RequestID).method
			if r.responseAllowed(method, e.Response.URL, status) {
				return
			}
			r.add(fmt.Sprintf("HTTP %d %s %s", status, method, e.Response.URL))
		case *network.EventLoadingFailed:
			if e.Canceled {
				// Navigating away cancels in-flight loads; routine,
				// not a failure.
				return
			}
			req := r.requestInfoFor(e.RequestID)
			r.add(fmt.Sprintf("request failed: %s %s — %s", req.method, req.url, e.ErrorText))
		}
	})
}
