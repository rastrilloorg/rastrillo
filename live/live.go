// Package live is an in-memory hub of per-topic change counters. An app
// declares its topics, calls Notify after a write commits, and renders
// each live region with the versions of its topics (View.Attrs); a
// stream handler (added separately) tells open pages which topics
// moved, and the page fetches those regions again. The stream carries
// topic names and versions, never data.
//
// Two decisions shape the package, and both exist to stop one person's
// activity leaking to another.
//
// Topics are checked per viewer. A topic is a declared name with an
// optional key ("notes/42"), and each name carries an Allow check that
// says whether this request may follow that key. Without keys, one
// person's write to a private list would make every other person's
// page fetch again, and so tell them someone wrote. Declaring names up
// front also means a client cannot grow the hub's maps by asking for
// topics that do not exist.
//
// No shared counter is exposed. Each topic has its own version, so a
// viewer sees only the versions of topics they may follow and learns
// nothing about anyone else's writes from the gaps between numbers.
// The hub does keep one internal sequence, but only to order a topic's
// last change against the start of a request; it never leaves the
// process.
//
// Fragment requests (fragment.go) let one URL answer with a page's
// regions alone. A renderer calls, in this order: Elsewhere (a form's
// redirect went to another page: navigate, before anything is taken);
// NoStore; Wanted; then, if Has says the page defines the regions
// asked for, TakesFlash to decide whether to take the flash, and
// WriteRegions (or ui.Pages.WriteRegions) to answer, or the full page
// when Wanted was not ok. WriteRegions renders into a buffer and sets
// Rastrillo-Regions before the first body byte; writing regions
// straight to the ResponseWriter would lose that header. The flash is
// looked up after Has so a request that ends in a navigation leaves it
// for the page navigated to.
//
// The package imports only the standard library: ui and the root
// package import live, never the reverse.
package live

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Allow says whether the request may follow the topic with this key.
// The key is empty for an unkeyed topic.
type Allow func(r *http.Request, key string) bool

// Config declares the hub. Path is required; zero values elsewhere take
// the defaults noted on each field.
type Config struct {
	// Path is where the app mounts the stream handler. Pages learn it
	// from the meta tag View.Head renders.
	Path string
	// Topics maps each declared topic name to its per-viewer check.
	Topics map[string]Allow
	// MaxStreams caps open streams per process (default 1000).
	MaxStreams int
	// Lifetime bounds one connection so a revoked session loses its
	// stream at the next reconnect (default 5 minutes).
	Lifetime time.Duration
	// Heartbeat is the quiet-stream comment interval (default 15s).
	Heartbeat time.Duration
	// WriteDeadline is re-armed per write so a slow client cannot hold
	// a stream, or shutdown, open (default 5s).
	WriteDeadline time.Duration
}

// Hub holds the topic versions and the open streams' wake channels.
type Hub struct {
	cfg  Config
	boot string

	mu sync.Mutex
	// seq orders changes against request starts. It is never exposed:
	// see the package doc.
	seq    uint64
	topics map[string]*topicState
	subs   map[*subscriber]struct{}
	// closed refuses new streams once Close runs; done tells open ones
	// to end.
	closed bool
	done   chan struct{}
	// warned is the topic names Attrs has logged, so a typo in a
	// template warns once rather than on every render (warnOnce).
	warned map[string]bool

	// Copied from cfg so tests can shrink them, as in jobs.Handlers.
	lifetime      time.Duration
	heartbeat     time.Duration
	writeDeadline time.Duration
}

type topicState struct {
	counter   uint64
	changedAt uint64 // seq of the last change
}

// subscriber is one open stream's wake-up. Capacity 1 and a
// non-blocking send mean a stream that has not caught up simply has a
// wake pending, and Notify never waits for it.
type subscriber struct {
	wake chan struct{}
}

// New validates cfg and returns a hub with a fresh boot ID. A new boot
// ID per hub is what tells a client that the process restarted and
// every topic must count as changed.
func New(cfg Config) (*Hub, error) {
	if cfg.Path == "" {
		return nil, errors.New("live: Config.Path is required")
	}
	topics := make(map[string]Allow, len(cfg.Topics))
	for name, allow := range cfg.Topics {
		if !validName(name) {
			return nil, fmt.Errorf("live: topic name %q must match ^[a-z][a-z0-9-]{0,31}$", name)
		}
		if allow == nil {
			return nil, fmt.Errorf("live: topic %q has a nil Allow", name)
		}
		topics[name] = allow
	}
	// Copied so the app cannot declare topics after New, past the
	// validation above.
	cfg.Topics = topics
	if cfg.MaxStreams == 0 {
		cfg.MaxStreams = 1000
	}
	if cfg.Lifetime == 0 {
		cfg.Lifetime = 5 * time.Minute
	}
	if cfg.Heartbeat == 0 {
		cfg.Heartbeat = 15 * time.Second
	}
	if cfg.WriteDeadline == 0 {
		cfg.WriteDeadline = 5 * time.Second
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf("live: boot id: %w", err)
	}
	return &Hub{
		cfg:           cfg,
		boot:          hex.EncodeToString(b[:]),
		topics:        map[string]*topicState{},
		subs:          map[*subscriber]struct{}{},
		warned:        map[string]bool{},
		done:          make(chan struct{}),
		lifetime:      cfg.Lifetime,
		heartbeat:     cfg.Heartbeat,
		writeDeadline: cfg.WriteDeadline,
	}, nil
}

// Boot returns the ID that is new each time New runs, or "" for a nil
// hub.
func (h *Hub) Boot() string {
	if h == nil {
		return ""
	}
	return h.boot
}

// Notify raises the named topics' versions and wakes every open
// stream. Call it after the write commits. It never blocks. An
// undeclared or malformed topic is logged and skipped: a typo in one
// call must not take down the request that made the write. On a nil
// hub it does nothing, so an app whose hub is optional can call it
// after every commit without a panic in the request that just wrote.
func (h *Hub) Notify(topics ...string) {
	if h == nil {
		return
	}
	valid := make([]string, 0, len(topics))
	seen := make(map[string]bool, len(topics))
	for _, t := range topics {
		name, _, ok := parseTopic(t)
		if !ok {
			slog.Warn("live: Notify skipped a malformed topic", "topic", t)
			continue
		}
		if _, declared := h.cfg.Topics[name]; !declared {
			slog.Warn("live: Notify skipped an undeclared topic", "topic", t)
			continue
		}
		if !seen[t] {
			seen[t] = true
			valid = append(valid, t)
		}
	}
	if len(valid) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	for _, t := range valid {
		st := h.topics[t]
		if st == nil {
			st = &topicState{}
			h.topics[t] = st
		}
		st.counter++
		st.changedAt = h.seq
	}
	for s := range h.subs {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// version is the version to render for a topic in a request that began
// at startSeq. A topic that changed after the request began reads one
// lower than its counter, so the page looks stale and fetches once
// more rather than missing a write that landed between the request's
// start and its queries. It reads without creating state: a viewer
// asking about keys nobody wrote must not grow the map.
func (h *Hub) version(topic string, startSeq uint64) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.topics[topic]
	if st == nil {
		return 0
	}
	if st.changedAt <= startSeq {
		return st.counter
	}
	return st.counter - 1
}

// currentSeq is the internal sequence a request's middleware records.
func (h *Hub) currentSeq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

func (h *Hub) register() *subscriber {
	s := &subscriber{wake: make(chan struct{}, 1)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) unregister(s *subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// parseTopic splits "name" or "name/key" and checks both parts against
// the wire format.
func parseTopic(s string) (name, key string, ok bool) {
	name, key, hasKey := strings.Cut(s, "/")
	if !validName(name) {
		return "", "", false
	}
	if hasKey && !validKey(key) {
		return "", "", false
	}
	return name, key, true
}

// parseToken splits "topic@version".
func parseToken(s string) (topic string, v uint64, ok bool) {
	i := strings.LastIndexByte(s, '@')
	if i < 0 {
		return "", 0, false
	}
	topic, num := s[:i], s[i+1:]
	if _, _, ok := parseTopic(topic); !ok {
		return "", 0, false
	}
	// Digits only: ParseUint alone would also take a leading "+".
	if num == "" || strings.Trim(num, "0123456789") != "" {
		return "", 0, false
	}
	v, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return topic, v, true
}

// validName is ^[a-z][a-z0-9-]{0,31}$.
func validName(s string) bool {
	if len(s) == 0 || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// validKey is ^[A-Za-z0-9_-]{1,64}$.
func validKey(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
