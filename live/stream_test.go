package live

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// streamServer mounts h.Handler on a real server: a stream needs a live
// connection, because httptest's recorder can neither deliver flushed
// bytes to a reading client nor observe a disconnect. Every handler
// return sends on the returned channel, which is how the tests see a
// stream end on the server side rather than guessing from the client.
func streamServer(t *testing.T, h *Hub) (*httptest.Server, chan struct{}) {
	t.Helper()
	return startStreamServer(t, h, httptest.NewUnstartedServer(nil))
}

func startStreamServer(t *testing.T, h *Hub, ts *httptest.Server) (*httptest.Server, chan struct{}) {
	t.Helper()
	returned := make(chan struct{}, 64)
	stream := h.Handler()
	ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream.ServeHTTP(w, r)
		returned <- struct{}{}
	})
	ts.Start()
	t.Cleanup(ts.Close)
	// Registered after ts.Close so it runs first: open streams end
	// before the server waits on them.
	t.Cleanup(h.Close)
	return ts, returned
}

// smallBuffers shrinks every accepted connection's send buffer so a
// client that stops reading stalls the handler's writes after a few
// kilobytes instead of the megabytes loopback autotuning allows.
type smallBuffers struct{ net.Listener }

func (l smallBuffers) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		c.(*net.TCPConn).SetWriteBuffer(4096)
	}
	return c, err
}

func smallBufferServer(t *testing.T, h *Hub) (*httptest.Server, chan struct{}) {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	ts.Listener = smallBuffers{ts.Listener}
	return startStreamServer(t, h, ts)
}

// stuckClient opens a stream over a raw connection and never reads it,
// so the server's writes back up once the TCP windows fill.
func stuckClient(t *testing.T, ts *httptest.Server, query string) {
	t.Helper()
	c, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.(*net.TCPConn).SetReadBuffer(4096)
	if _, err := fmt.Fprintf(c, "GET /live?%s HTTP/1.1\r\nHost: test\r\n\r\n", query); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, ctx context.Context, url string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// openStream GETs the stream and fails unless it was accepted. The
// handler subscribes before it sends headers, so a 200 means the
// stream is registered with the hub.
func openStream(t *testing.T, ctx context.Context, url string, header http.Header) <-chan unit {
	t.Helper()
	resp := get(t, ctx, url, header)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", url, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	return units(resp.Body)
}

// unit is one parsed piece of the stream: an event, or a comment
// (name "comment").
type unit struct{ name, data, id string }

// units parses a stream in the background; the channel closes when the
// stream does. Reading in a goroutine lets a test wait on "nothing
// arrives within 100ms" without blocking on the body.
func units(body io.Reader) <-chan unit {
	ch := make(chan unit, 256)
	go func() {
		defer close(ch)
		r := bufio.NewReader(body)
		var u unit
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case strings.HasPrefix(line, ":"):
				ch <- unit{name: "comment", data: strings.TrimSpace(line[1:])}
			case strings.HasPrefix(line, "event: "):
				u.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				u.data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, "id: "):
				u.id = strings.TrimPrefix(line, "id: ")
			case line == "" && u.name != "":
				ch <- u
				u = unit{}
			}
		}
	}()
	return ch
}

// nextEvent skips heartbeats and returns the next event.
func nextEvent(t *testing.T, ch <-chan unit) unit {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				t.Fatal("stream ended, want an event")
			}
			if u.name != "comment" {
				return u
			}
		case <-timeout:
			t.Fatal("no event within 5s")
		}
	}
}

// quiet fails on any event within d and returns how many heartbeats
// arrived, so a test can tell a quiet stream from a dead one.
func quiet(t *testing.T, ch <-chan unit, d time.Duration) int {
	t.Helper()
	pings := 0
	timeout := time.After(d)
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				t.Fatal("stream ended, want it open and quiet")
			}
			if u.name != "comment" {
				t.Fatalf("got event %+v, want none", u)
			}
			pings++
		case <-timeout:
			return pings
		}
	}
}

func waitReturns(t *testing.T, returned chan struct{}, n int, within time.Duration) {
	t.Helper()
	timeout := time.After(within)
	for i := 0; i < n; i++ {
		select {
		case <-returned:
		case <-timeout:
			t.Fatalf("%d of %d handlers returned within %v", i, n, within)
		}
	}
}

// waitSubscribed waits for a raw client's stream to register, since
// there is no response for it to read.
func waitSubscribed(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for subscribers(h) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the stream never subscribed")
		}
		time.Sleep(time.Millisecond)
	}
}

func subscribers(h *Hub) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// fastHub is a hub whose clocks fit inside a test.
func fastHub(t *testing.T, names ...string) *Hub {
	t.Helper()
	h := newHub(t, names...)
	h.heartbeat = 20 * time.Millisecond
	h.lifetime = 30 * time.Second
	h.writeDeadline = 200 * time.Millisecond
	return h
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestStreamCatchesUpThenRelaysLive(t *testing.T) {
	h := fastHub(t, "a")
	h.Notify("a")
	h.Notify("a")
	ts, _ := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@0", nil)
	if u := nextEvent(t, ch); u.name != "change" || u.data != "a@2" {
		t.Fatalf("catch-up = %+v, want change a@2", u)
	}
	h.Notify("a")
	if u := nextEvent(t, ch); u.name != "change" || u.data != "a@3" {
		t.Fatalf("live = %+v, want change a@3", u)
	}
}

func TestAQuietReconnectSendsNothing(t *testing.T) {
	h := fastHub(t, "a", "b")
	h.Notify("a")
	h.Notify("a")
	ts, _ := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@2&t=b@0", nil)
	if pings := quiet(t, ch, 100*time.Millisecond); pings == 0 {
		t.Fatal("no heartbeat within 100ms, want the stream alive and quiet")
	}
}

func TestADifferentBootChangesEveryTopic(t *testing.T) {
	h := fastHub(t, "a", "b")
	h.Notify("a")
	ts, _ := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot=000000000000&t=a@1&t=b@0", nil)
	got := []string{nextEvent(t, ch).data, nextEvent(t, ch).data}
	if got[0] != "a@1" || got[1] != "b@0" {
		t.Fatalf("events = %v, want [a@1 b@0]", got)
	}
	quiet(t, ch, 60*time.Millisecond)
}

func TestLastEventIDOverridesTheURL(t *testing.T) {
	h := fastHub(t, "a")
	h.Notify("a")
	h.Notify("a")
	ts, _ := streamServer(t, h)
	url := ts.URL + "/live?boot=" + h.Boot() + "&t=a@0"

	ch := openStream(t, testCtx(t), url, http.Header{"Last-Event-Id": {h.Boot() + " a@2"}})
	quiet(t, ch, 100*time.Millisecond)

	// An ID from an earlier boot says nothing about this one's versions,
	// so the URL's stand.
	ch = openStream(t, testCtx(t), url, http.Header{"Last-Event-Id": {"000000000000 a@2"}})
	if u := nextEvent(t, ch); u.data != "a@2" {
		t.Fatalf("with a foreign boot's ID: %+v, want a@2 from the URL's a@0", u)
	}
}

func TestLastEventIDFromAnotherBootLeavesURLTopicsChanged(t *testing.T) {
	// The URL's boot is old but the ID's matches: topics the ID names
	// are current, and a topic only the URL names still counts as
	// changed, because its version is from the old boot.
	h := fastHub(t, "a", "b")
	h.Notify("a")
	ts, _ := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot=000000000000&t=a@0&t=b@0",
		http.Header{"Last-Event-Id": {h.Boot() + " a@1"}})
	if u := nextEvent(t, ch); u.data != "b@0" {
		t.Fatalf("event = %+v, want b@0 only", u)
	}
	quiet(t, ch, 60*time.Millisecond)
}

func TestEventIDsCarryEverySubscribedVersion(t *testing.T) {
	h := fastHub(t, "a", "b")
	h.Notify("a")
	ts, _ := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@1&t=b@0", nil)
	h.Notify("b")
	u := nextEvent(t, ch)
	if want := h.Boot() + " a@1 b@1"; u.data != "b@1" || u.id != want {
		t.Fatalf("event = %+v, want data b@1 id %q", u, want)
	}
}

func TestCatchUpIDsNameOnlyWhatWasSent(t *testing.T) {
	// Two topics behind: the first event's ID must not already claim the
	// second topic's new version, or a drop between the two would resume
	// past a change the client never saw.
	h := fastHub(t, "a", "b")
	h.Notify("a", "b")
	ts, _ := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@0&t=b@0", nil)
	first, second := nextEvent(t, ch), nextEvent(t, ch)
	if first.id != h.Boot()+" a@1 b@0" || second.id != h.Boot()+" a@1 b@1" {
		t.Fatalf("ids = %q, %q", first.id, second.id)
	}
}

func TestStreamRefusesAForeignKey(t *testing.T) {
	var keys []string
	h, err := New(Config{Path: "/live", Topics: map[string]Allow{
		"notes": func(r *http.Request, key string) bool {
			keys = append(keys, key)
			return key == "me"
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := streamServer(t, h)
	ctx := testCtx(t)

	for _, q := range []string{"t=notes/other@0", "t=notes/me@0&t=notes/other@0"} {
		resp := get(t, ctx, ts.URL+"/live?boot="+h.Boot()+"&"+q, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", q, resp.StatusCode)
		}
		if n := subscribers(h); n != 0 {
			t.Errorf("%s: hub has %d subscribers, want 0", q, n)
		}
	}
	if strings.Join(keys, ",") != "other,me,other" {
		t.Errorf("Allow saw keys %v", keys)
	}
	openStream(t, ctx, ts.URL+"/live?boot="+h.Boot()+"&t=notes/me@0", nil)
}

func TestStreamRefusesUndeclaredAndMalformed(t *testing.T) {
	h := fastHub(t, "a")
	ts, _ := streamServer(t, h)
	ctx := testCtx(t)
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"t=zzz@0", http.StatusForbidden},
		{"t=a@0&t=zzz@0", http.StatusForbidden},
		{"t=a", http.StatusBadRequest},
		{"t=a@0&t=a@x", http.StatusBadRequest},
		{"t=", http.StatusBadRequest},
		{"", http.StatusBadRequest},
	} {
		resp := get(t, ctx, ts.URL+"/live?boot="+h.Boot()+"&"+tc.query, nil)
		if resp.StatusCode != tc.want {
			t.Errorf("%q: status %d, want %d", tc.query, resp.StatusCode, tc.want)
		}
	}
	resp, err := http.Post(ts.URL+"/live?t=a@0", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d, want 405", resp.StatusCode)
	}
	if n := subscribers(h); n != 0 {
		t.Errorf("hub has %d subscribers, want 0", n)
	}
}

func TestStreamCapRefuses(t *testing.T) {
	h := fastHub(t, "a")
	h.cfg.MaxStreams = 1
	ts, returned := streamServer(t, h)
	url := ts.URL + "/live?boot=" + h.Boot() + "&t=a@0"

	ctx, cancel := context.WithCancel(testCtx(t))
	openStream(t, ctx, url, nil)
	if resp := get(t, testCtx(t), url, nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second stream: status %d, want 503", resp.StatusCode)
	}
	waitReturns(t, returned, 1, time.Second) // the refused one

	// Ending the first stream frees its place.
	cancel()
	waitReturns(t, returned, 1, time.Second)
	openStream(t, testCtx(t), url, nil)
}

func TestNotifyDoesNotBlockOnAStuckClient(t *testing.T) {
	h := fastHub(t, "a")
	h.writeDeadline = 10 * time.Second
	ts, _ := smallBufferServer(t, h)
	stuckClient(t, ts, "boot="+h.Boot()+"&t=a@0")
	waitSubscribed(t, h)

	start := time.Now()
	for i := 0; i < 10000; i++ {
		h.Notify("a")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("10k Notify calls took %v with a stuck client", d)
	}
}

func TestCloseEndsStreamsAndRefusesNew(t *testing.T) {
	h := fastHub(t, "a")
	ts, returned := streamServer(t, h)
	url := ts.URL + "/live?boot=" + h.Boot() + "&t=a@0"
	var streams []<-chan unit
	for i := 0; i < 3; i++ {
		streams = append(streams, openStream(t, testCtx(t), url, nil))
	}

	h.Close()
	waitReturns(t, returned, 3, h.writeDeadline+100*time.Millisecond)
	for _, ch := range streams {
		for u := range ch {
			if u.name != "comment" {
				t.Fatalf("got event %+v on close, want none", u)
			}
		}
	}
	h.Close() // idempotent
	if resp := get(t, testCtx(t), url, nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stream after Close: status %d, want 503", resp.StatusCode)
	}
}

func TestCloseEndsAStreamBlockedOnWrite(t *testing.T) {
	// Many topics make every wake write kilobytes, so the client's
	// window fills at once.
	var names, tokens []string
	for i := 0; i < 20; i++ {
		names = append(names, fmt.Sprintf("a/k%d", i))
		tokens = append(tokens, fmt.Sprintf("t=a/k%d@0", i))
	}
	h := fastHub(t, "a")
	h.writeDeadline = 500 * time.Millisecond
	ts, returned := smallBufferServer(t, h)
	stuckClient(t, ts, "boot="+h.Boot()+"&"+strings.Join(tokens, "&"))
	waitSubscribed(t, h)

	// The flood runs past Close so the handler is never left idle in
	// its select, where Close alone would end it: only the write
	// deadline can free it from the blocked write.
	var stop atomic.Bool
	flooded := make(chan struct{})
	go func() {
		defer close(flooded)
		for !stop.Load() {
			h.Notify(names...)
			time.Sleep(50 * time.Microsecond)
		}
	}()
	defer func() { stop.Store(true); <-flooded }()

	time.Sleep(100 * time.Millisecond)
	select {
	case <-returned:
		t.Fatal("handler returned before Close; the test needs it blocked")
	default:
	}
	h.Close()
	waitReturns(t, returned, 1, h.writeDeadline+100*time.Millisecond)
}

func TestLifetimeEndsTheStream(t *testing.T) {
	h := fastHub(t, "a")
	h.lifetime = 50 * time.Millisecond
	ts, returned := streamServer(t, h)

	ch := openStream(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@0", nil)
	waitReturns(t, returned, 1, time.Second)
	for u := range ch {
		if u.name != "comment" {
			t.Fatalf("got event %+v at end of lifetime, want none", u)
		}
	}
	if n := subscribers(h); n != 0 {
		t.Fatalf("hub has %d subscribers after the stream ended", n)
	}
}

func TestANilHubIgnoresNotifyAndHasNoBoot(t *testing.T) {
	// An app whose hub is optional calls Notify after every write; with
	// the hub off that must not panic the request that just committed.
	// Close is the same: shutdown must not panic for an app without live.
	var h *Hub
	h.Notify("a")
	h.Close()
	if b := h.Boot(); b != "" {
		t.Fatalf("Boot() = %q, want empty", b)
	}
}

func TestNoWriteAfterClose(t *testing.T) {
	// Close and a pending change made ready together: select picks
	// among ready cases at random, so without a check before each write
	// a stream would write again after Close about half the time, and
	// to a slow client each such write can take up to WriteDeadline.
	// Holding the hub's lock while both are set makes every write the
	// handler then attempts a write after Close.
	for i := 0; i < 30; i++ {
		h := fastHub(t, "a")
		h.heartbeat = time.Hour
		ts, returned := streamServer(t, h)
		ch := openStream(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@0", nil)

		h.mu.Lock()
		h.closed = true
		close(h.done)
		h.seq++
		h.topics["a"] = &topicState{counter: 1, changedAt: h.seq}
		for s := range h.subs {
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
		h.mu.Unlock()

		waitReturns(t, returned, 1, h.writeDeadline)
		for u := range ch {
			if u.name == "change" {
				t.Fatalf("run %d: got %+v after Close, want no write", i, u)
			}
		}
	}
}

func TestAStreamEndsCleanlyAfterAQuietSpell(t *testing.T) {
	// The last write's deadline must not still be in force when the
	// handler returns, or net/http's closing chunk fails and the client
	// sees a truncated stream instead of a clean end.
	for _, end := range []string{"lifetime", "close"} {
		h := fastHub(t, "a")
		h.Notify("a")
		h.heartbeat = time.Hour
		h.writeDeadline = 50 * time.Millisecond
		if end == "lifetime" {
			h.lifetime = 200 * time.Millisecond
		}
		ts, _ := streamServer(t, h)
		resp := get(t, testCtx(t), ts.URL+"/live?boot="+h.Boot()+"&t=a@0", nil)
		if end == "close" {
			time.AfterFunc(200*time.Millisecond, h.Close)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s: reading to the end: %v", end, err)
		}
		if !strings.Contains(string(body), "data: a@1") {
			t.Fatalf("%s: body %q lacks the catch-up event", end, body)
		}
	}
}

func TestStreamCapsTopics(t *testing.T) {
	h := fastHub(t, "a")
	ts, _ := streamServer(t, h)
	query := func(n int) string {
		var q []string
		for i := 0; i < n; i++ {
			q = append(q, fmt.Sprintf("t=a/k%d@0", i))
		}
		return ts.URL + "/live?boot=" + h.Boot() + "&" + strings.Join(q, "&")
	}
	openStream(t, testCtx(t), query(maxTopics), nil)
	if resp := get(t, testCtx(t), query(maxTopics+1), nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("%d topics: status %d, want 400", maxTopics+1, resp.StatusCode)
	}
}
