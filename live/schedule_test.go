package live

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

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

// waitFor polls until cond holds. The deadline is generous: it bounds
// a hang under -race on a loaded runner, not how promptly a timer fires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNotifyAtFiresAtTheTime(t *testing.T) {
	h := newHub(t, "a")
	s := h.register()
	defer h.unregister(s)
	h.NotifyAt(time.Now().Add(30*time.Millisecond), "a")
	if v := h.version("a", 1<<20); v != 0 {
		t.Fatalf("version %d before the time; want 0", v)
	}
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
	now := time.Now()
	// Scheduled out of order: the heap, not the call order, decides.
	h.NotifyAt(now.Add(60*time.Millisecond), "b")
	h.NotifyAt(now.Add(30*time.Millisecond), "a")
	waitFor(t, "both times to fire", func() bool {
		a, _ := changedAt(h, "a")
		b, _ := changedAt(h, "b")
		return a == 1 && b == 1
	})
	// Even if a slow runner finds both due in one firing, each time is
	// its own change, earlier first.
	_, seqA := changedAt(h, "a")
	_, seqB := changedAt(h, "b")
	if seqA >= seqB {
		t.Errorf("a changed at %d, b at %d; want a first", seqA, seqB)
	}
	time.Sleep(50 * time.Millisecond)
	a, _ := changedAt(h, "a")
	b, _ := changedAt(h, "b")
	if a != 1 || b != 1 {
		t.Errorf("versions a=%d b=%d after both fired; want 1 each", a, b)
	}
}

func TestNotifyAtStoresATimeAndTopicOnce(t *testing.T) {
	h := newHub(t, "a", "b")
	at := time.Now().Add(30 * time.Millisecond)
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
	waitFor(t, "the time to fire", func() bool { return pendingLen(h) == 0 })
	if v := h.version("a", 1<<20); v != 1 {
		t.Errorf("a = %d; want 1", v)
	}
	if v := h.version("b", 1<<20); v != 1 {
		t.Errorf("b = %d; want 1", v)
	}
}

func TestCloseStopsNotifyAt(t *testing.T) {
	h := newHub(t, "a")
	h.NotifyAt(time.Now().Add(30*time.Millisecond), "a")
	h.Close()
	if n := pendingLen(h); n != 0 {
		t.Errorf("%d pending after Close; want 0", n)
	}
	time.Sleep(100 * time.Millisecond)
	if v := h.version("a", 1<<20); v != 0 {
		t.Errorf("version %d: the timer fired after Close", v)
	}
	h.NotifyAt(time.Now().Add(-time.Hour), "a")
	h.NotifyAt(time.Now().Add(time.Hour), "a")
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
	h.Close()
}
