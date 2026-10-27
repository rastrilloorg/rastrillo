package ui

import (
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

// live.js is scaffolded into apps and read by their owners, so it has
// the shim's shape: one commented IIFE, small enough to read in one
// sitting, nothing a CSP would reject, nothing fetched from elsewhere.
func TestLiveIsSmallAndSelfContained(t *testing.T) {
	js := string(LiveJS())
	// 28 KiB (28,672 bytes). The stream and region half is about 16.6 KB.
	// The forms section brought the file to 24,470 bytes, against a 24
	// KiB cap. Sending a swapping form from the window, after any
	// handler that cancels it, instead of from the capture listener,
	// added about 970 bytes, for 25,438. Navigating to the answer's own
	// URL, so a locale prefix survives, made it 26,144. The final
	// review's fixes (non-HTML answers as text, the send a tick after
	// the submit, the edit check for other targets, the checks on a 204,
	// the paused note kept off a flash, the revive on restore) added
	// about 2.3 KB, for 28,411. Handing back a form whose 204 a later
	// form superseded made it 28,563: 109 bytes of headroom.
	if n := len(js); n > 28*1024 {
		t.Errorf("live.js is %d bytes, over 28 KiB; an app owner should be able to read it in one sitting", n)
	}
	if !strings.HasPrefix(js, "/*") {
		t.Error("live.js should open with its contract comment")
	}
	if strings.Count(js, "\n(function () {\n") != 1 || !strings.HasSuffix(strings.TrimSpace(js), "\n})();") {
		t.Error("live.js should be a single IIFE")
	}
	for _, bad := range []string{"eval(", "new Function", "http://", "https://", "import ", "require(", "\t"} {
		if strings.Contains(js, bad) {
			t.Errorf("live.js contains %q", bad)
		}
	}
}

// The vocabulary the header promises and the server speaks. A rename on
// either side would leave pages static with nothing in the console.
func TestLiveContract(t *testing.T) {
	js := string(LiveJS())
	for _, want := range []string{
		`meta[name="rastrillo-live"]`, "data-paused", "data-live", "data-live-boot", "data-live-stale",
		`"change"`, "lastEventId", "boot=", "&t=",
		"Rastrillo-Fragment", "Rastrillo-Regions", "Rastrillo-Location",
		// Swapping forms: the opt-in, the header that names the page
		// the form is on, and the note for an unknown outcome.
		"data-live-form", "Rastrillo-Page", "data-unconfirmed", `[aria-invalid="true"]`,
		// Claimed in capture, sent from the window after every handler;
		// busy.js reads the claim.
		"e.liveForm = true", `document.addEventListener("submit", claim, true)`, `addEventListener("submit", submitted)`,
		"prerenderingchange", "visibilitychange", "pagehide", "pageshow", "persisted", "focusout",
		// The same local-path guard rastrillo.js applies to the header
		// that navigates.
		"localPath", "\\u0000-\\u001f\\u007f",
		// Topic names are user-shaped strings ("constructor" is a valid
		// one), so lookups keyed by them must not inherit from Object.
		"Object.create(null)",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("live.js does not mention %s", want)
		}
	}
}

func liveNode(t *testing.T, scenario string, out any) {
	t.Helper()
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"live_node.mjs", scenario}}), out); err != nil {
		t.Fatal(err)
	}
}

// streamTokens reads back the t parameters, in order, and the boot.
func streamTokens(t *testing.T, raw string) (path, boot string, tokens []string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("stream URL %q: %v", raw, err)
	}
	q := u.Query()
	return u.Path, q.Get("boot"), q["t"]
}

func TestLiveIsInertWithoutTheMeta(t *testing.T) {
	type count struct{ Sources, Fetches int }
	var got struct{ NoMeta, NoRegion count }
	liveNode(t, "inert", &got)
	if got.NoMeta != (count{}) {
		t.Errorf("a page with regions but no rastrillo-live meta started %+v, want nothing", got.NoMeta)
	}
	if got.NoRegion != (count{}) {
		t.Errorf("a page with the meta but no data-live region started %+v, want nothing", got.NoRegion)
	}
}

func TestLiveOpensOneStreamForManyRegions(t *testing.T) {
	var got struct {
		Sources int
		URL     string
	}
	liveNode(t, "many", &got)
	if got.Sources != 1 {
		t.Fatalf("three regions opened %d EventSources, want 1", got.Sources)
	}
	path, boot, tokens := streamTokens(t, got.URL)
	if path != "/live" || boot != "b00t" {
		t.Errorf("stream URL %q: path %q boot %q, want /live and b00t", got.URL, path, boot)
	}
	if want := []string{"a@2", "b/x_1@9"}; !reflect.DeepEqual(tokens, want) {
		t.Errorf("stream asks for %q, want each topic once at its lowest version, %q", tokens, want)
	}
}

func TestLiveUsesTheLowestVersionPerTopic(t *testing.T) {
	var got struct{ URL string }
	liveNode(t, "lowest", &got)
	if _, _, tokens := streamTokens(t, got.URL); !reflect.DeepEqual(tokens, []string{"a@3"}) {
		t.Errorf("regions at a@3 and a@5 asked for %q, want [a@3]: the lower version is the one still owed a fetch", tokens)
	}
}

// The browser's own EventSource reconnect would run beside the shim's
// and hammer a restarting server at its fixed few-second retry; the
// shim closes it and owns the backoff.
func TestLiveOwnsReconnects(t *testing.T) {
	var got struct {
		Closed            bool
		SourcesAfterError int
		PendingAfterError []int
		SourcesAfterTimer int
		Delays            []int
		AfterOpen         []int
	}
	liveNode(t, "reconnect", &got)
	if !got.Closed {
		t.Error("an error left the EventSource open, so the browser's reconnect runs beside the shim's")
	}
	if got.SourcesAfterError != 1 {
		t.Errorf("an error opened %d EventSources before the backoff ran, want 1", got.SourcesAfterError)
	}
	if !slices.Contains(got.PendingAfterError, 1000) {
		t.Errorf("after an error the pending timers are %v, want a 1000ms reconnect", got.PendingAfterError)
	}
	if got.SourcesAfterTimer != 2 {
		t.Errorf("the 1s timer opened %d EventSources in all, want 2", got.SourcesAfterTimer)
	}
	if want := []int{1000, 2000, 4000, 8000, 16000, 32000, 60000, 60000, 60000}; !reflect.DeepEqual(got.Delays, want) {
		t.Errorf("reconnect delays %v, want %v (doubling, capped at a minute)", got.Delays, want)
	}
	if !reflect.DeepEqual(got.AfterOpen, []int{1000}) {
		t.Errorf("after a stream opened, the next error waits %v, want [1000]: an open resets the backoff", got.AfterOpen)
	}
}

// A region fetch that keeps failing must stop: retries are ordinary
// requests. Then the page says live updates have paused.
func TestLiveRetriesAFailedFetchThenPauses(t *testing.T) {
	var got struct {
		Fetches            []int
		Headers            map[string]string
		Delays             []int
		PendingAfter       []int
		Notice             string
		NoticeAfterOpen    string
		NoticeAfterSuccess string
	}
	liveNode(t, "retry", &got)
	if got.Headers["rastrillo-fragment"] != "list" {
		t.Errorf("the region fetch sent headers %v, want Rastrillo-Fragment: list", got.Headers)
	}
	if want := []int{1, 2, 3, 4}; !reflect.DeepEqual(got.Fetches, want) {
		t.Errorf("fetch counts across the retries %v, want %v", got.Fetches, want)
	}
	if want := []int{1000, 2000, 4000}; !reflect.DeepEqual(got.Delays, want) {
		t.Errorf("retry delays %v, want %v", got.Delays, want)
	}
	if len(got.PendingAfter) != 0 {
		t.Errorf("after three retries timers %v are still pending, want none", got.PendingAfter)
	}
	if got.Notice != "Paused" {
		t.Errorf("after the retries ran out the notice reads %q, want the meta's data-paused text", got.Notice)
	}
	// The stream was never the trouble, so its opening is no news: the
	// region is still behind until a fetch for it lands.
	if got.NoticeAfterOpen != "Paused" {
		t.Errorf("the stream reopening with a region's fetches still failing left the notice reading %q, want the paused note kept", got.NoticeAfterOpen)
	}
	if got.NoticeAfterSuccess != "" {
		t.Errorf("the failed region's next fetch landed and the notice still reads %q, want it cleared", got.NoticeAfterSuccess)
	}
}

// The fetch-and-apply seam Task 6's forms call. A Headers instance
// whose Rastrillo-Fragment silently went missing would make the server
// answer with a whole page; a caller's init written into would carry
// one request's signal into the next; a throw when stopped would skip
// the caller's own failure path.
func TestLiveSendCopiesItsInit(t *testing.T) {
	var got struct {
		Method, Cache string
		Headers       map[string]string
		PlainHeaders  map[string]string
		CallerHeaders map[string]string
		CallerKeys    []string
		Stopped       string
	}
	liveNode(t, "seam", &got)
	if got.Method != "POST" || got.Cache != "no-store" {
		t.Errorf("sent method %q cache %q, want the caller's POST and no-store", got.Method, got.Cache)
	}
	want := map[string]string{"rastrillo-page": "/notes", "rastrillo-fragment": "list notice"}
	if !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("with Headers in init, sent %v, want %v", got.Headers, want)
	}
	if want := map[string]string{"rastrillo-page": "/notes", "rastrillo-fragment": "list"}; !reflect.DeepEqual(got.PlainHeaders, want) {
		t.Errorf("with a plain object in init, sent %v, want %v", got.PlainHeaders, want)
	}
	if !reflect.DeepEqual(got.CallerHeaders, map[string]string{"rastrillo-page": "/notes"}) || !slices.Equal(got.CallerKeys, []string{"method", "headers", "headers", "Rastrillo-Page"}) {
		t.Errorf("send wrote into the caller's init: headers %v, keys %v", got.CallerHeaders, got.CallerKeys)
	}
	if got.Stopped != "rejected" {
		t.Errorf("send on a stopped page %s, want a rejected promise", got.Stopped)
	}
}

// A live 204 names the page it answered for in Rastrillo-Location, as
// the server sees it: under a locale prefix, without the prefix. The
// browser knows the URL the answer came from, prefix and all, so that is
// where the page goes; the header's path is only for a Response that
// reports no URL.
func TestALiveNavigationFallsBackToRastrilloLocation(t *testing.T) {
	var got map[string][]string
	liveNode(t, "navigate", &got)
	for name, want := range map[string][]string{
		"prefixed":         {"http://app.test/fr/item"},
		"fallback":         {"/item"},
		"fallbackNotLocal": nil,
		"otherOrigin":      nil,
	} {
		if !slices.Equal(got[name], want) {
			t.Errorf("%s: navigated to %v, want %v", name, got[name], want)
		}
	}
}

// Back from the back/forward cache, a region whose fetches gave up is
// fetched again. Chromium can fire visibilitychange before pageshow,
// while the page is still stopped, so the retry belongs to the restart.
func TestLiveRetriesAFailedRegionAfterARestore(t *testing.T) {
	var got struct {
		Before, After int
		Notice        string
	}
	liveNode(t, "restoreFailed", &got)
	if got.Before != 4 || got.Notice != "Paused" {
		t.Fatalf("before leaving: %d fetches, notice %q; want 4 and the paused note", got.Before, got.Notice)
	}
	if got.After != got.Before+1 {
		t.Errorf("after the restore the failed region was fetched %d more times, want 1", got.After-got.Before)
	}
}

// The paused note shares the notice with the flash. Written over a
// "Saved." it would wipe the message someone is reading, and unpause
// clears only its own words, so the flash would be gone for good.
func TestLivePausedNoteLeavesAFlash(t *testing.T) {
	var got struct{ Flash, Empty string }
	liveNode(t, "pauseFlash", &got)
	if got.Flash != "Saved." {
		t.Errorf("the stream went down under a flash and the notice reads %q, want Saved. kept", got.Flash)
	}
	if got.Empty != "Paused" {
		t.Errorf("the stream went down under an empty notice and it reads %q, want the paused note", got.Empty)
	}
}

// A stream fetch answered "navigate" after a form's fetch superseded it,
// or after the page was hidden, is an old answer: following it would
// leave the page under a write still on its way.
func TestALiveNavigationFromAnObsoleteFetchIsDropped(t *testing.T) {
	var got struct{ Superseded, Abandoned []string }
	liveNode(t, "obsolete", &got)
	if len(got.Superseded) != 0 {
		t.Errorf("a superseded fetch's 204 navigated to %v, want nowhere", got.Superseded)
	}
	if len(got.Abandoned) != 0 {
		t.Errorf("a 204 that landed after pagehide navigated to %v, want nowhere", got.Abandoned)
	}
}

// A change event for a version the region already shows (the page's
// own form fetch got there first) costs no request, but the same
// version from a restarted server is a different counter and must.
func TestLiveSkipsAVersionTheRegionAlreadyShows(t *testing.T) {
	var got struct{ SameBoot, NewBoot int }
	liveNode(t, "current", &got)
	if got.SameBoot != 0 {
		t.Errorf("a change to the version the region holds fetched %d times, want 0", got.SameBoot)
	}
	if got.NewBoot != 1 {
		t.Errorf("a change from a different boot fetched %d times, want 1", got.NewBoot)
	}
}

func TestLiveWaitsForAPrerenderedPage(t *testing.T) {
	var got struct{ Before, After int }
	liveNode(t, "prerender", &got)
	if got.Before != 0 {
		t.Errorf("a prerendered page nobody is viewing opened %d EventSources, want 0", got.Before)
	}
	if got.After != 1 {
		t.Errorf("once activated it opened %d EventSources, want 1", got.After)
	}
}
