package live

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// watch is one topic a stream follows and the version its client is
// known to have. stale marks a version from another boot, which says
// nothing about this process's counters, so the topic counts as
// changed whatever its number.
type watch struct {
	topic string
	known uint64
	stale bool
}

// Handler is the change stream. Mount it at Config.Path behind the
// app's own auth: Allow sees the request that auth produced.
//
// The request is GET ?boot=<boot>&t=<topic@version>…. Every topic is
// checked before the stream subscribes, so a refused viewer never
// holds a place, and a single bad topic refuses the stream rather than
// serving the rest, which would let a page quietly lose updates. The
// stream subscribes before it reads versions, so a change landing
// between catch-up and the live relay still wakes it.
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(h.serveStream)
}

func (h *Hub) serveStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		refuse(w, http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	watches, ok := parseWatches(q["t"], q.Get("boot") != h.boot)
	if !ok {
		refuse(w, http.StatusBadRequest)
		return
	}
	for _, wt := range watches {
		name, key, _ := parseTopic(wt.topic)
		allow, declared := h.cfg.Topics[name]
		if !declared || !allow(r, key) {
			refuse(w, http.StatusForbidden)
			return
		}
	}
	h.resume(watches, r.Header.Get("Last-Event-ID"))

	fl, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError)
		return
	}
	sub := h.admit()
	if sub == nil {
		refuse(w, http.StatusServiceUnavailable)
		return
	}
	defer h.unregister(sub)

	rc := http.NewResponseController(w)
	// net/http writes the closing chunk after the handler returns. A
	// fresh deadline for it keeps the last write's deadline, long past
	// after a quiet spell, from failing that write and turning a clean
	// end into a truncated stream. A closure, because a plain defer
	// would read time.Now() here rather than at the return.
	defer func() { rc.SetWriteDeadline(time.Now().Add(h.writeDeadline)) }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	rc.SetWriteDeadline(time.Now().Add(h.writeDeadline))
	fl.Flush()

	// write is every byte's one exit. The deadline, re-armed per write,
	// is what bounds a write blocked on a client that stopped reading:
	// Close cannot interrupt a write, so without it a slow client could
	// hold shutdown's drain open (serve.go # Timeouts). The deadline's
	// own error is ignored so a writer without deadlines still streams;
	// a write or flush error ends the stream.
	//
	// done is checked before every write because select picks at random
	// among ready cases: a pending wake or heartbeat can win over a
	// closed done, and to a slow client that keeps reading, each write
	// after Close could take up to WriteDeadline, again and again, past
	// the shutdown drain.
	write := func(text string) bool {
		select {
		case <-h.done:
			return false
		default:
		}
		rc.SetWriteDeadline(time.Now().Add(h.writeDeadline))
		if _, err := io.WriteString(w, text); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	heartbeat := time.NewTimer(h.heartbeat)
	defer heartbeat.Stop()
	// send writes a change for every topic that moved past what the
	// client has, all in one flush. Versions are re-read on every wake,
	// so a wake Notify dropped because one was already pending loses
	// nothing.
	send := func() bool {
		text := h.changes(watches)
		if text == "" {
			return true
		}
		heartbeat.Reset(h.heartbeat)
		return write(text)
	}

	if !send() {
		return
	}
	lifetime := time.NewTimer(h.lifetime)
	defer lifetime.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case <-lifetime.C:
			// Ends cleanly, with no event: the client reconnects, and
			// auth runs again, which is how a revoked session loses its
			// stream.
			return
		case <-heartbeat.C:
			if !write(": ping\n\n") {
				return
			}
			heartbeat.Reset(h.heartbeat)
		case <-sub.wake:
			if !send() {
				return
			}
		}
	}
}

// changes advances watches to the hub's current versions and returns
// one change event per topic that moved. Each event's ID is built after
// only its own topic advanced, so an ID never claims a version whose
// event the client may not have received.
func (h *Hub) changes(watches []watch) string {
	cur := h.counters(watches)
	var b strings.Builder
	for i := range watches {
		wt := &watches[i]
		if !wt.stale && cur[i] <= wt.known {
			continue
		}
		wt.known, wt.stale = cur[i], false
		b.WriteString("event: change\ndata: ")
		b.WriteString(token(wt.topic, wt.known))
		b.WriteString("\nid: ")
		b.WriteString(h.boot)
		for _, o := range watches {
			b.WriteByte(' ')
			b.WriteString(token(o.topic, o.known))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

// counters reads every watched topic's counter under one lock, so one
// pass of changes sees one consistent moment.
func (h *Hub) counters(watches []watch) []uint64 {
	cur := make([]uint64, len(watches))
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, wt := range watches {
		if st := h.topics[wt.topic]; st != nil {
			cur[i] = st.counter
		}
	}
	return cur
}

// parseWatches reads the t parameters. A topic named twice keeps the
// lower version: the client opens with the lowest any region holds,
// and the lower one is the one still owed a fetch.
func parseWatches(raw []string, stale bool) ([]watch, bool) {
	if len(raw) == 0 || len(raw) > maxTopics {
		return nil, false
	}
	watches := make([]watch, 0, len(raw))
	index := make(map[string]int, len(raw))
	for _, s := range raw {
		topic, v, ok := parseToken(s)
		if !ok {
			return nil, false
		}
		if i, dup := index[topic]; dup {
			watches[i].known = min(watches[i].known, v)
			continue
		}
		index[topic] = len(watches)
		watches = append(watches, watch{topic: topic, known: v, stale: stale})
	}
	return watches, true
}

// resume applies a Last-Event-ID, which the edge (or the browser's own
// reconnect) sends in place of the URL's versions: it is what the
// client was last told, so it is at least as new. It applies only when
// its boot is this process's, and only to topics the URL names: the
// URL decides what the stream follows, and those were the topics just
// checked. A malformed ID is ignored rather than refused, because the
// URL's versions are a correct, if older, starting point.
func (h *Hub) resume(watches []watch, id string) {
	fields := strings.Fields(id)
	if len(fields) == 0 || fields[0] != h.boot {
		return
	}
	versions := make(map[string]uint64, len(fields)-1)
	for _, f := range fields[1:] {
		topic, v, ok := parseToken(f)
		if !ok {
			return
		}
		versions[topic] = v
	}
	for i := range watches {
		if v, ok := versions[watches[i].topic]; ok {
			watches[i].known, watches[i].stale = v, false
		}
	}
}

// admit subscribes a stream unless the hub is closed or full. Both
// checks and the registration share one lock so concurrent opens
// cannot overshoot MaxStreams.
func (h *Hub) admit() *subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subs) >= h.cfg.MaxStreams {
		return nil
	}
	s := &subscriber{wake: make(chan struct{}, 1)}
	h.subs[s] = struct{}{}
	return s
}

// Close refuses new streams and tells every open one to end. A stream
// waiting for changes ends at once; one blocked writing to a slow
// client ends when that write's deadline passes, within WriteDeadline.
// It is safe to call more than once. Call it when shutdown starts,
// before the server waits for in-flight requests: open streams would
// otherwise hold that wait until their lifetime ran out. On a nil hub
// it does nothing, so shutdown code need not know whether the app uses
// live.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

// maxTopics bounds one stream's topics, so one request cannot make
// thousands of Allow calls, or make every wake re-read thousands of
// counters. A page names each topic once, with the lowest version its
// regions hold, so 64 is far more than a page shows.
const maxTopics = 64

func token(topic string, v uint64) string {
	return topic + "@" + strconv.FormatUint(v, 10)
}

// refuse answers with the status text alone: the client is a script
// that acts on the status, and a reason in the body would tell a
// prober which topics exist.
func refuse(w http.ResponseWriter, code int) {
	http.Error(w, http.StatusText(code), code)
}
