package jobs

import (
	"io"
	"net/http"
	"time"
)

// streamTick is how often an events stream re-reads its job. One
// second is deliberately coarse: the stream's job is to beat a
// 2-second poll without inventing a broadcast primitive — each open
// stream costs one Jobs.Get per tick and nothing else.
const streamTick = time.Second

// streamTTL bounds any one events connection. A stream that outlives
// it closes cleanly and the client reconnects (EventSource does so on
// its own) or falls back to polling — no single connection may hold a
// goroutine for a job's whole lifetime.
const streamTTL = 5 * time.Minute

// streamHeartbeat is the comment-ping cadence on a quiet stream: it
// keeps intermediaries from timing out an idle-looking connection and
// surfaces a dead client at the next write instead of never.
const streamHeartbeat = 15 * time.Second

// streamWriteDeadline is the per-write idle deadline, re-armed before
// every event and heartbeat via http.NewResponseController — the
// serve.go # Timeouts recipe, which is what lets a stream coexist
// with serve's zero WriteTimeout default.
const streamWriteDeadline = 30 * time.Second

// Events is GET /jobs/{id}/events: a Server-Sent Events stream that
// tells the shim when to re-fetch the fragment it already knows how
// to swap — SSE never carries HTML, so the one rendering path stays.
// The handler observes Jobs.Get snapshots on an internal tick; a
// changed snapshot becomes "event: update" (data: the advisory
// Status), a job that left Running becomes "event: done" (data: its
// Location, "" if none) and a close, and a job that vanished — swept
// or expired — becomes "event: gone" and a close. Quiet stretches
// carry a ": ping" comment heartbeat. The stream ends on its own
// after streamTTL, and immediately when the request context does
// (client gone, server draining) — which is what keeps a graceful
// shutdown's drain budget safe with streams open.
func (h *Handlers) Events(w http.ResponseWriter, r *http.Request) {
	job, ok := h.lookup(w, r)
	if !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	// write is every byte's one exit: deadline armed, then the bytes,
	// then the flush. The deadline error is deliberately ignored — a
	// writer that cannot set deadlines (a test recorder) still streams,
	// just unbounded — while a write error ends the handler.
	write := func(text string) bool {
		rc.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
		if _, err := io.WriteString(w, text); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	event := func(name, data string) bool {
		return write("event: " + name + "\ndata: " + data + "\n\n")
	}

	ticker := time.NewTicker(h.tick)
	defer ticker.Stop()
	ttl := time.NewTimer(h.ttl)
	defer ttl.Stop()
	heartbeat := time.NewTimer(h.heartbeat)
	defer heartbeat.Stop()

	// last is the snapshot the client is presumed current with: the
	// page render that handed out this URL. Only a change against it is
	// worth an update event.
	last := job
	for {
		cur, ok := h.cfg.Jobs.Get(job.ID, job.Owner)
		if !ok {
			event("gone", "")
			return
		}
		if cur.Status != Running {
			event("done", cur.Location)
			return
		}
		if cur.Status != last.Status || cur.Progress != last.Progress || cur.Err != last.Err {
			if !event("update", string(cur.Status)) {
				return
			}
			last = cur
			heartbeat.Reset(h.heartbeat)
		}
		select {
		case <-r.Context().Done():
			return
		case <-ttl.C:
			return
		case <-heartbeat.C:
			if !write(": ping\n\n") {
				return
			}
			heartbeat.Reset(h.heartbeat)
		case <-ticker.C:
		}
	}
}
