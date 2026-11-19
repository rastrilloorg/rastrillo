package live

import (
	"container/heap"
	"log/slog"
	"time"
)

// maxPending bounds NotifyAt's scheduled (time, topic) pairs, about
// 100 bytes each, so a megabyte at most. An app past it is scheduling
// far more than one process should hold in memory, or a loop is
// scheduling the same thing under new keys.
const maxPending = 10_000

// NotifyAt notifies topics at the wall-clock time at, for a change no write
// makes: a post that goes live at its time, an offer that ends. Call it
// at boot for every stored future time, and whenever a schedule
// changes. A restart forgets every pending time, but it also gives the
// hub a new boot ID, so every open page fetches its regions once
// anyway; the boot-time calls cover the times still ahead. There is no
// cancel: a rescheduled item's old time still fires, and costs the
// pages that show it one fetch that finds nothing new.
//
// A time at or before now notifies at once, synchronously, before
// NotifyAt returns. Later times wait on one timer, armed for the
// earliest; each due time is its own change, in time order, as if
// Notify had been called then. The same topic at the same time is
// stored once. Topics are checked as Notify checks them, when NotifyAt
// is called.
//
// At most 10,000 (time, topic) pairs wait. Past that the latest-due is
// dropped, the new one or one already waiting, and the first drop is
// logged: the soonest are the updates someone is about to watch for,
// and a far one has the most chances to be scheduled again, by a
// schedule change or the next boot, before it is due. A dropped time
// loses only its live update; pages still show the change when next
// loaded.
//
// After Close it does nothing, and on a nil hub it does nothing.
func (h *Hub) NotifyAt(at time.Time, topics ...string) {
	if h == nil {
		return
	}
	// Wall clock only. A time from time.Now().Add keeps a monotonic
	// reading, and Go compares two such times by it: after the wall
	// clock steps back, the elapsed time would call a publication due
	// while it is still ahead on the wall, pages would fetch data not yet
	// published, and nothing would announce the real moment. Stripped,
	// the due check, the heap's order and the dedup key all read the wall.
	at = at.Round(0)
	valid := h.validTopics("NotifyAt", topics)
	if len(valid) == 0 {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if !at.After(h.now()) {
		h.raiseLocked(valid)
		h.mu.Unlock()
		return
	}
	dropped := false
	for _, t := range valid {
		dropped = !h.scheduleLocked(at, t) || dropped
	}
	warn := dropped && !h.warnedFull
	if warn {
		h.warnedFull = true
	}
	h.armLocked()
	h.mu.Unlock()
	// Logged outside the lock: a slow log handler must not stall every
	// render reading versions.
	if warn {
		slog.Warn("live: NotifyAt dropped the latest-due time past its bound; further drops are not logged", "max", h.maxPending)
	}
}

type pending struct {
	at    time.Time
	topic string
}

// pendingKey is a pending pair for dedup. The time is UnixNano, since
// two Times for one instant (with and without a monotonic reading)
// are not == to each other.
type pendingKey struct {
	at    int64
	topic string
}

func (p pending) key() pendingKey { return pendingKey{p.at.UnixNano(), p.topic} }

// scheduleLocked adds one pair, or finds it already there. It reports
// false when the bound dropped a pair, this one or another.
func (h *Hub) scheduleLocked(at time.Time, topic string) bool {
	p := pending{at, topic}
	if h.scheduled[p.key()] {
		return true
	}
	if len(h.pending) < h.maxPending {
		heap.Push(&h.pending, p)
		h.scheduled[p.key()] = true
		return true
	}
	// A heap keeps only its minimum in place, so the latest-due is a
	// scan. It runs only past the bound, which a working app never
	// reaches.
	last := 0
	for i := range h.pending {
		if h.pending[i].at.After(h.pending[last].at) {
			last = i
		}
	}
	if !at.Before(h.pending[last].at) {
		return false
	}
	delete(h.scheduled, h.pending[last].key())
	h.pending[last] = p
	heap.Fix(&h.pending, last)
	h.scheduled[p.key()] = true
	return false
}

// armLocked points the timer at the earliest pending time, or stops it
// when nothing is pending.
func (h *Hub) armLocked() {
	if len(h.pending) == 0 {
		if h.timer != nil {
			h.timer.Stop()
		}
		return
	}
	d := h.pending[0].at.Sub(h.now())
	if h.timer == nil {
		h.timer = time.AfterFunc(d, h.fire)
		return
	}
	h.timer.Reset(d)
}

// fire raises every due time's topics, one change per time, earliest
// first, then re-arms. A firing that finds nothing due (a Reset that
// raced it, or a wall clock set back) only re-arms.
func (h *Hub) fire() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	now := h.now()
	for len(h.pending) > 0 && !h.pending[0].at.After(now) {
		at := h.pending[0].at
		var due []string
		for len(h.pending) > 0 && h.pending[0].at.Equal(at) {
			p := heap.Pop(&h.pending).(pending)
			delete(h.scheduled, p.key())
			due = append(due, p.topic)
		}
		h.raiseLocked(due)
	}
	h.armLocked()
}

// stopScheduleLocked is Close's part: stop the timer and forget every
// pending time.
func (h *Hub) stopScheduleLocked() {
	if h.timer != nil {
		h.timer.Stop()
	}
	h.pending = nil
	clear(h.scheduled)
}

// pendingHeap is a min-heap by time, for container/heap.
type pendingHeap []pending

func (q pendingHeap) Len() int           { return len(q) }
func (q pendingHeap) Less(i, j int) bool { return q[i].at.Before(q[j].at) }
func (q pendingHeap) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *pendingHeap) Push(x any)        { *q = append(*q, x.(pending)) }
func (q *pendingHeap) Pop() any {
	old := *q
	p := old[len(old)-1]
	*q = old[:len(old)-1]
	return p
}
