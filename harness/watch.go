//go:build browser

package harness

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Allow registers an expected probe: responses with this method, path
// and status stop being problems — and so do the console-error mirrors
// Chromium logs for them, which arrive via the CDP log domain carrying
// only a URL and so are matched by path alone.
func (rig *Rig) Allow(method, path string, status int) {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	rig.allows = append(rig.allows,
		allowance{method: method, path: path, status: status})
}

func (rig *Rig) add(problem string) {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	rig.problems = append(rig.problems, problem)
}

// take drains the accumulated problems — Screen's flush.
func (rig *Rig) take() []string {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	out := rig.problems
	rig.problems = nil
	return out
}

// consoleArgText renders one console-call argument's CDP value for a
// problem line. A JS string arrives as a JSON-quoted value — so
// console.error("kaboom") would otherwise read `"kaboom"`, quotes and
// all — so a string is unquoted first; anything else (number, object,
// ...) is reported as its raw JSON text, which is already readable.
func consoleArgText(v jsonv2.Value) string {
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

func (rig *Rig) responseAllowed(method, rawURL string, status int) bool {
	p := pathOf(rawURL)
	rig.mu.Lock()
	defer rig.mu.Unlock()
	for _, a := range rig.allows {
		if a.method == method && a.path == p && a.status == status {
			return true
		}
	}
	return false
}

// logEntryAllowed matches a CDP log entry by URL alone: the log
// domain's mirror of an HTTP failure carries no method or status, so
// an allowed path excuses its mirror too.
func (rig *Rig) logEntryAllowed(rawURL string) bool {
	p := pathOf(rawURL)
	rig.mu.Lock()
	defer rig.mu.Unlock()
	for _, a := range rig.allows {
		if a.path == p {
			return true
		}
	}
	return false
}

func (rig *Rig) requestInfoFor(id network.RequestID) requestInfo {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	return rig.requests[id]
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
// attach, so there is nothing to switch on here. Listen hands each
// event over in the order the browser sent it, before the reply to the
// next command, so Screen never reads the list with a problem on its
// way and a response always finds its own request.
func (rig *Rig) watch() {
	Listen(rig, rig.ctx, runtime.ConsoleAPICalled, rig.consoleCalled)
	Listen(rig, rig.ctx, runtime.ExceptionThrown, rig.exceptionThrown)
	Listen(rig, rig.ctx, cdplog.EntryAdded, rig.logEntryAdded)
	Listen(rig, rig.ctx, network.RequestWillBeSent, rig.requestWillBeSent)
	Listen(rig, rig.ctx, network.ResponseReceived, rig.responseReceived)
	Listen(rig, rig.ctx, network.LoadingFailed, rig.loadingFailed)
}

func (rig *Rig) consoleCalled(event *runtime.EventConsoleAPICalled) {
	if event.Type != "error" && event.Type != "assert" {
		return
	}
	var parts []string
	for _, argument := range event.Args {
		parts = append(parts, consoleArgText(argument.Value))
	}
	rig.add("console." + string(event.Type) + ": " + strings.Join(parts, " "))
}

func (rig *Rig) exceptionThrown(event *runtime.EventExceptionThrown) {
	exception := &chromedp.ExceptionError{
		ExceptionDetails: event.ExceptionDetails,
	}
	rig.add("uncaught: " + exception.Error())
}

func (rig *Rig) logEntryAdded(event *cdplog.EventEntryAdded) {
	if event.Entry.Level != cdplog.EntryLevelError {
		return
	}
	if rig.logEntryAllowed(event.Entry.URL) {
		return
	}
	rig.add(fmt.Sprintf("log.error: %s (%s)",
		event.Entry.Text, event.Entry.URL))
}

func (rig *Rig) requestWillBeSent(event *network.EventRequestWillBeSent) {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	rig.requests[event.RequestID] = requestInfo{
		method: event.Request.Method, url: event.Request.URL,
	}
}

func (rig *Rig) responseReceived(event *network.EventResponseReceived) {
	status := int(event.Response.Status)
	if status < http.StatusBadRequest {
		return
	}
	method := rig.requestInfoFor(event.RequestID).method
	if rig.responseAllowed(method, event.Response.URL, status) {
		return
	}
	rig.add(fmt.Sprintf("HTTP %d %s %s", status, method, event.Response.URL))
}

func (rig *Rig) loadingFailed(event *network.EventLoadingFailed) {
	if event.Canceled {
		// Navigating away cancels in-flight loads; routine, not a
		// failure.
		return
	}
	request := rig.requestInfoFor(event.RequestID)
	rig.add(fmt.Sprintf("request failed: %s %s — %s",
		request.method, request.url, event.ErrorText))
}
