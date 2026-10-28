package live

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// testClock is the hub's clock in tests that drive fire directly, so
// none of them races a real timer: times are an hour or more ahead,
// the real timer never fires within the test, and the test moves the
// clock and calls fire itself.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// withClock gives h a test clock, before any timer exists, and closes
// the hub at the end so no real timer outlives the test.
func withClock(t *testing.T, h *Hub) *testClock {
	t.Helper()
	c := &testClock{t: time.Now().Round(0)}
	h.now = c.now
	t.Cleanup(h.Close)
	return c
}

// pendingLen and pendingTimes read the scheduled set under the hub's lock.
func pendingLen(h *Hub) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}

func pendingTimes(h *Hub) map[time.Duration]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[time.Duration]bool{}
	for _, p := range h.pending {
		out[time.Until(p.at).Round(time.Minute)] = true
	}
	return out
}

func changedAt(h *Hub, topic string) (counter, seq uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st := h.topics[topic]; st != nil {
		return st.counter, st.changedAt
	}
	return 0, 0
}

// The one test on the real timer: a time 30ms away fires. The deadline
// is generous because it bounds a hang, not promptness.
func TestNotifyAtFiresAtTheTime(t *testing.T) {
	h := newHub(t, "a")
	t.Cleanup(h.Close)
	s := h.register()
	defer h.unregister(s)
	h.NotifyAt(time.Now().Add(30*time.Millisecond), "a")
	select {
	case <-s.wake:
	case <-time.After(5 * time.Second):
		t.Fatal("no wake within 5s of a time 30ms away")
	}
	if v := h.version("a", 1<<20); v != 1 {
		t.Errorf("version %d after the time; want 1", v)
	}
	if n := pendingLen(h); n != 0 {
		t.Errorf("%d still pending after firing; want 0", n)
	}
}

func TestNotifyAtWaitsForTheTime(t *testing.T) {
	h := newHub(t, "a")
	c := withClock(t, h)
	h.NotifyAt(c.now().Add(time.Hour), "a")
	c.advance(59 * time.Minute)
	h.fire()
	if v, n := h.version("a", 1<<20), pendingLen(h); v != 0 || n != 1 {
		t.Fatalf("a minute early: version %d, %d pending; want 0 and 1", v, n)
	}
	c.advance(time.Minute)
	h.fire()
	if v, n := h.version("a", 1<<20), pendingLen(h); v != 1 || n != 0 {
		t.Errorf("at the time: version %d, %d pending; want 1 and 0", v, n)
	}
}

// A deadline built with time.Now().Add keeps a monotonic reading, and
// Go compares two such times by it. Were it kept, a wall clock stepped
// back would leave the timer's elapsed time saying "due" while the
// publication time on the wall is still ahead: pages would refetch
// data not yet published, and nothing would announce the real moment.
// Go offers no way to build a time whose wall and monotonic readings
// disagree, so this holds the cause: every stored time is wall-only,
// which makes the due check, the heap order and the dedup key all
// wall-clock. The order holds when the caller mixes both kinds.
func TestNotifyAtKeepsWallClockTimesOnly(t *testing.T) {
	h := newHub(t, "a", "b", "c")
	c := withClock(t, h)
	base := time.Now()
	h.NotifyAt(base.Add(3*time.Hour), "c")
	h.NotifyAt(base.Add(time.Hour).Round(0), "a")
	h.NotifyAt(base.Add(2*time.Hour), "b")
	h.mu.Lock()
	for _, p := range h.pending {
		if p.at != p.at.Round(0) {
			t.Errorf("%s is stored with a monotonic reading", p.topic)
		}
	}
	h.mu.Unlock()
	// Each step lands half an hour past one more time.
	c.advance(30 * time.Minute)
	for i := range 3 {
		c.advance(time.Hour)
		h.fire()
		for j, x := range []string{"a", "b", "c"} {
			want := uint64(0)
			if j <= i {
				want = 1
			}
			if v := h.version(x, 1<<20); v != want {
				t.Errorf("step %d: %s = %d; want %d", i+1, x, v, want)
			}
		}
	}
}

// A time already past notifies synchronously, before NotifyAt returns,
// so a boot that replays stored times needs no timer for the past ones.
func TestNotifyAtPastTimeNotifiesAtOnce(t *testing.T) {
	h := newHub(t, "a")
	h.NotifyAt(time.Now().Add(-time.Hour), "a")
	if v := h.version("a", 1<<20); v != 1 {
		t.Errorf("version %d right after NotifyAt in the past; want 1", v)
	}
	h.NotifyAt(time.Now(), "a")
	if v := h.version("a", 1<<20); v != 2 {
		t.Errorf("version %d right after NotifyAt now; want 2", v)
	}
	if n := pendingLen(h); n != 0 {
		t.Errorf("%d pending; want 0", n)
	}
}

func TestNotifyAtFiresInOrderOnceEach(t *testing.T) {
	h := newHub(t, "a", "b")
	c := withClock(t, h)
	now := c.now()
	// Scheduled out of order: the heap, not the call order, decides.
	h.NotifyAt(now.Add(2*time.Hour), "b")
	h.NotifyAt(now.Add(time.Hour), "a")
	// Both due in one firing: each time is still its own change,
	// earlier first.
	c.advance(3 * time.Hour)
	h.fire()
	a, seqA := changedAt(h, "a")
	b, seqB := changedAt(h, "b")
	if a != 1 || b != 1 {
		t.Fatalf("versions a=%d b=%d; want 1 each", a, b)
	}
	if seqA >= seqB {
		t.Errorf("a changed at %d, b at %d; want a first", seqA, seqB)
	}
	h.fire()
	if a, _ := changedAt(h, "a"); a != 1 {
		t.Errorf("a = %d after firing again; want 1", a)
	}
}

func TestNotifyAtStoresATimeAndTopicOnce(t *testing.T) {
	h := newHub(t, "a", "b")
	c := withClock(t, h)
	at := time.Now().Add(time.Hour)
	h.NotifyAt(at, "a")
	h.NotifyAt(at, "a", "a")
	// The same instant without its monotonic reading is the same time.
	h.NotifyAt(at.Round(0), "a")
	if n := pendingLen(h); n != 1 {
		t.Fatalf("%d pending; want 1", n)
	}
	h.NotifyAt(at, "b")
	if n := pendingLen(h); n != 2 {
		t.Fatalf("%d pending after another topic at the same time; want 2", n)
	}
	c.advance(2 * time.Hour)
	h.fire()
	if v := h.version("a", 1<<20); v != 1 {
		t.Errorf("a = %d; want 1", v)
	}
	if v := h.version("b", 1<<20); v != 1 {
		t.Errorf("b = %d; want 1", v)
	}
}

func TestCloseStopsNotifyAt(t *testing.T) {
	h := newHub(t, "a")
	c := withClock(t, h)
	h.NotifyAt(c.now().Add(time.Hour), "a")
	h.Close()
	if n := pendingLen(h); n != 0 {
		t.Errorf("%d pending after Close; want 0", n)
	}
	c.advance(2 * time.Hour)
	h.fire()
	if v := h.version("a", 1<<20); v != 0 {
		t.Errorf("version %d: a firing after Close notified", v)
	}
	h.NotifyAt(c.now().Add(-time.Hour), "a")
	h.NotifyAt(c.now().Add(time.Hour), "a")
	if v, n := h.version("a", 1<<20), pendingLen(h); v != 0 || n != 0 {
		t.Errorf("after Close, NotifyAt gave version %d and %d pending; want nothing", v, n)
	}
}

func TestNotifyAtOnANilHub(t *testing.T) {
	var h *Hub
	h.NotifyAt(time.Now(), "a")
	h.NotifyAt(time.Now().Add(time.Hour), "a")
}

func TestNotifyAtSkipsBadTopics(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := newHub(t, "a")
	h.NotifyAt(time.Now().Add(time.Hour), "nope", "Bad Name", "a/bad key")
	if n := pendingLen(h); n != 0 {
		t.Errorf("%d pending; want 0", n)
	}
	for _, want := range []string{"nope", "Bad Name"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("no log naming %q: %q", want, buf.String())
		}
	}
}

// Past the bound the latest-due time is dropped: the new one if it is
// the latest, else the latest already pending. It is logged once.
func TestNotifyAtIsBounded(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := newHub(t, "a")
	withClock(t, h)
	h.maxPending = 3
	now := time.Now()
	for _, d := range []time.Duration{time.Hour, 3 * time.Hour, 2 * time.Hour} {
		h.NotifyAt(now.Add(d), "a")
	}
	h.NotifyAt(now.Add(4*time.Hour), "a")
	if got := pendingTimes(h); len(got) != 3 || got[4*time.Hour] {
		t.Errorf("pending %v; want the 4h time dropped", got)
	}
	h.NotifyAt(now.Add(30*time.Minute), "a")
	got := pendingTimes(h)
	if len(got) != 3 || !got[30*time.Minute] || got[3*time.Hour] {
		t.Errorf("pending %v; want 30m, 1h, 2h with 3h dropped", got)
	}
	if n := strings.Count(buf.String(), "NotifyAt dropped"); n != 1 {
		t.Errorf("logged %d times; want once: %q", n, buf.String())
	}
	// The earliest is still what the timer waits for.
	h.mu.Lock()
	first := time.Until(h.pending[0].at).Round(time.Minute)
	h.mu.Unlock()
	if first != 30*time.Minute {
		t.Errorf("earliest pending is %v away; want 30m", first)
	}
}
