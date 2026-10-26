package live

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func allowAll(*http.Request, string) bool { return true }

func newHub(t *testing.T, names ...string) *Hub {
	t.Helper()
	topics := map[string]Allow{}
	for _, n := range names {
		topics[n] = allowAll
	}
	h, err := New(Config{Path: "/live", Topics: topics})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNotifyRaisesOnlyTheNamedTopics(t *testing.T) {
	h := newHub(t, "a", "b", "c")
	h.Notify("a")
	h.Notify("a")
	h.Notify("b")
	// A start sequence past every change reads the true counters.
	const later = 1 << 20
	for topic, want := range map[string]uint64{"a": 2, "b": 1, "c": 0} {
		if got := h.version(topic, later); got != want {
			t.Errorf("version(%q) = %d, want %d", topic, got, want)
		}
	}
}

func TestNotifyCountsADuplicateOnce(t *testing.T) {
	h := newHub(t, "a")
	h.Notify("a", "a")
	if got := h.version("a", 1<<20); got != 1 {
		t.Fatalf("version = %d, want 1", got)
	}
}

func TestKeysAreSeparateTopics(t *testing.T) {
	h := newHub(t, "notes")
	h.Notify("notes/1")
	h.Notify("notes/1")
	h.Notify("notes/1")
	h.Notify("notes")
	if got := h.version("notes/1", 1<<20); got != 3 {
		t.Errorf("notes/1 = %d, want 3", got)
	}
	if got := h.version("notes/2", 1<<20); got != 0 {
		t.Errorf("notes/2 = %d, want 0", got)
	}
	if got := h.version("notes", 1<<20); got != 1 {
		t.Errorf("notes = %d, want 1", got)
	}
}

func TestVersionDoesNotGrowTheHub(t *testing.T) {
	h := newHub(t, "notes")
	for i := 0; i < 100; i++ {
		h.version("notes/zzz", 0)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.topics) != 0 {
		t.Fatalf("reading versions created %d topic entries", len(h.topics))
	}
}

func TestNotifySkipsUndeclaredTopics(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := newHub(t, "a")
	h.Notify("nope", "Bad Name", "nope/1", "", "a")
	if got := h.version("a", 1<<20); got != 1 {
		t.Errorf("a = %d, want 1", got)
	}
	if got := h.version("nope", 1<<20); got != 0 {
		t.Errorf("nope = %d, want 0", got)
	}
	if !strings.Contains(buf.String(), "nope") {
		t.Errorf("no log naming the skipped topic: %q", buf.String())
	}
}

func TestNewRefusesBadConfig(t *testing.T) {
	ok := map[string]Allow{"a": allowAll}
	cases := map[string]Config{
		"empty path":    {Topics: ok},
		"bad name":      {Path: "/live", Topics: map[string]Allow{"Bad Name": allowAll}},
		"leading digit": {Path: "/live", Topics: map[string]Allow{"1a": allowAll}},
		"keyed name":    {Path: "/live", Topics: map[string]Allow{"a/b": allowAll}},
		"too long":      {Path: "/live", Topics: map[string]Allow{strings.Repeat("a", 33): allowAll}},
		"nil allow":     {Path: "/live", Topics: map[string]Allow{"a": nil}},
	}
	for name, cfg := range cases {
		if h, err := New(cfg); err == nil || h != nil {
			t.Errorf("%s: New = %v, %v; want an error", name, h, err)
		}
	}
	if _, err := New(Config{Path: "/live", Topics: map[string]Allow{strings.Repeat("a", 32): allowAll}}); err != nil {
		t.Errorf("32-character name refused: %v", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	h := newHub(t, "a")
	c := h.cfg
	if c.MaxStreams != 1000 || c.Lifetime != 5*time.Minute || c.Heartbeat != 15*time.Second || c.WriteDeadline != 5*time.Second {
		t.Fatalf("defaults = %+v", c)
	}
	h2, err := New(Config{Path: "/live", MaxStreams: 3, Lifetime: time.Second, Heartbeat: time.Second, WriteDeadline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if h2.cfg.MaxStreams != 3 || h2.cfg.Lifetime != time.Second {
		t.Fatalf("explicit values overwritten: %+v", h2.cfg)
	}
}

func TestNewDoesNotAliasTheTopicsMap(t *testing.T) {
	topics := map[string]Allow{"a": allowAll}
	h, err := New(Config{Path: "/live", Topics: topics})
	if err != nil {
		t.Fatal(err)
	}
	topics["b"] = allowAll
	h.Notify("b")
	if got := h.version("b", 1<<20); got != 0 {
		t.Fatalf("a topic added after New was accepted: %d", got)
	}
}

func TestBootIsTwelveHexAndNewPerHub(t *testing.T) {
	a, b := newHub(t, "a"), newHub(t, "a")
	for _, h := range []*Hub{a, b} {
		id := h.Boot()
		if len(id) != 12 || strings.Trim(id, "0123456789abcdef") != "" {
			t.Errorf("boot %q is not 12 lowercase hex", id)
		}
	}
	if a.Boot() == b.Boot() {
		t.Error("two hubs share a boot id")
	}
}

func TestParseTopic(t *testing.T) {
	good := map[string][2]string{
		"a":                            {"a", ""},
		"notes/42":                     {"notes", "42"},
		"my-topic/Ab_c-9":              {"my-topic", "Ab_c-9"},
		strings.Repeat("a", 32):        {strings.Repeat("a", 32), ""},
		"a/" + strings.Repeat("k", 64): {"a", strings.Repeat("k", 64)},
	}
	for in, want := range good {
		name, key, ok := parseTopic(in)
		if !ok || name != want[0] || key != want[1] {
			t.Errorf("parseTopic(%q) = %q, %q, %v", in, name, key, ok)
		}
	}
	for _, in := range []string{
		"", "A", "1a", "-a", "a b", "a/", "/1", "a/b/c", "a/b c", "a/b@1", "a@1",
		strings.Repeat("a", 33), "a/" + strings.Repeat("k", 65), "a\n", "é",
	} {
		if _, _, ok := parseTopic(in); ok {
			t.Errorf("parseTopic(%q) accepted", in)
		}
	}
}

func TestParseToken(t *testing.T) {
	topic, v, ok := parseToken("notes/42@7")
	if !ok || topic != "notes/42" || v != 7 {
		t.Errorf("got %q %d %v", topic, v, ok)
	}
	if _, v, ok := parseToken("a@18446744073709551615"); !ok || v != 1<<64-1 {
		t.Errorf("max uint64: %d %v", v, ok)
	}
	for _, in := range []string{
		"", "a", "a@", "@1", "a@-1", "a@+1", "a@1.5", "a@x", "a@18446744073709551616",
		"A@1", "a/@1", "a@1@2", "a @1", "a@ 1", "a@0x10",
	} {
		if _, _, ok := parseToken(in); ok {
			t.Errorf("parseToken(%q) accepted", in)
		}
	}
}

func TestNotifyWakesEverySubscriber(t *testing.T) {
	h := newHub(t, "a")
	s1, s2 := h.register(), h.register()
	h.Notify("a")
	for i, s := range []*subscriber{s1, s2} {
		select {
		case <-s.wake:
		default:
			t.Errorf("subscriber %d not woken", i)
		}
	}
	h.unregister(s2)
	h.Notify("a")
	select {
	case <-s1.wake:
	default:
		t.Error("remaining subscriber not woken")
	}
	select {
	case <-s2.wake:
		t.Error("unregistered subscriber woken")
	default:
	}
}

func TestNotifyDoesNotWakeForSkippedTopics(t *testing.T) {
	h := newHub(t, "a")
	s := h.register()
	h.Notify("nope")
	select {
	case <-s.wake:
		t.Fatal("woken by a topic that was skipped")
	default:
	}
}

func TestNotifyNeverBlocksOnAFullSubscriber(t *testing.T) {
	h := newHub(t, "a")
	s := h.register()
	s.wake <- struct{}{} // already full: a stream that has not caught up
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			h.Notify("a")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on a subscriber whose channel is full")
	}
	if got := h.version("a", 1<<20); got != 10 {
		t.Fatalf("version = %d, want 10", got)
	}
}

func TestConcurrentNotifyAndRead(t *testing.T) {
	h := newHub(t, "a", "b")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := h.register()
			defer h.unregister(s)
			for j := 0; j < 200; j++ {
				h.Notify("a", "b/1")
				h.version("a", uint64(j))
			}
		}()
	}
	wg.Wait()
	if got := h.version("a", 1<<20); got != 1600 {
		t.Fatalf("a = %d, want 1600", got)
	}
}
