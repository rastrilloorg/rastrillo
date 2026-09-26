// Package background tracks work an app starts and nobody waits for —
// the send after the request returned, the sweep on a ticker, the fuse
// armed a minute out — so that shutting down has exactly one thing to
// call before the database closes.
//
// In production the outliving is the point: the request returns, the
// email goes out after. At shutdown, and in every test, it is a bug
// with a disguise. The teardown closes the database under a goroutine
// still using it; the goroutine's error ("sql: database is closed")
// lands on a finished testing.T, and Go turns that into a panic naming
// whichever test owned the T — so a fuse armed by one test reads as the
// failure of whatever ran a minute later, and only sometimes. Tito Go
// met it as "Log in goroutine after TestScheduledMessageCanBeMovedAnd
// SentEarly has completed: messages: sweep: list due: sql: database is
// closed" (2026-08-11), after its outbox had already grown a WaitGroup
// of its own for the same reason. A WaitGroup cannot cover an armed
// time.AfterFunc, and waiting out a sixty-second fuse in teardown is
// not an option, so timers are held and STOPPED instead.
//
// The rule that makes it work is that background work starts through
// a Group — Go, After or Loop — and never with a bare go statement or
// time.AfterFunc. Untracked is the fence that holds a package to it.
package background

import (
	"context"
	"sync"
	"time"
)

// Group is one owner's background work: what is running, what is
// armed, and whether anything new may start. The zero value is ready
// to use. A Group must not be copied after first use.
type Group struct {
	mu      sync.Mutex
	stopped bool
	// done is closed by Stop, so a loop waiting out its interval
	// leaves on the Group's word alone, even when its own context is
	// never cancelled (a test teardown that forgot to).
	done   chan struct{}
	timers map[*time.Timer]struct{}
	wg     sync.WaitGroup
}

// Done is closed when Stop is called. Created on first ask, so a Group
// nobody started anything on carries nothing.
func (g *Group) Done() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.doneLocked()
}

func (g *Group) doneLocked() chan struct{} {
	if g.done == nil {
		g.done = make(chan struct{})
	}
	return g.done
}

// Go runs fn on a goroutine Stop waits for, and reports whether it
// started one. After Stop it never does, which is what keeps a late
// request from reopening work the teardown has already accounted for.
//
// A caller holding a single-flight "running" flag must clear it when
// Go returns false, or a stopped Group looks permanently busy.
func (g *Group) Go(fn func()) bool {
	g.mu.Lock()
	if g.stopped {
		g.mu.Unlock()
		return false
	}
	g.wg.Add(1)
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		fn()
	}()
	return true
}

// After is time.AfterFunc with a way back: the timer is held so Stop
// can disarm it, and fn, once it fires, is waited for exactly as Go's
// work is. It returns nil after Stop; a caller that keeps the timer
// nil-checks it before stopping it, so nil simply means nothing armed.
func (g *Group) After(d time.Duration, fn func()) *time.Timer {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return nil
	}
	if g.timers == nil {
		g.timers = map[*time.Timer]struct{}{}
	}
	var t *time.Timer
	t = time.AfterFunc(d, func() {
		g.mu.Lock()
		delete(g.timers, t)
		if g.stopped {
			// Stop got here first: it stopped this timer and lost the
			// race with the runtime, or it is holding the lock right
			// now. Either way the database is on its way out; do
			// nothing.
			g.mu.Unlock()
			return
		}
		g.wg.Add(1)
		g.mu.Unlock()
		defer g.wg.Done()
		fn()
	})
	g.timers[t] = struct{}{}
	return t
}

// Loop runs pass once now — something that was down over a due moment
// catches up at start — and then once every interval, until ctx is
// cancelled or the Group stops.
//
// A pass runs to completion however the loop is ending: a sweep is a
// short read and half of one is worth nothing, so the checks are
// between passes, not inside them. Stop waits for the pass in flight,
// which is the point of it.
func (g *Group) Loop(ctx context.Context, every time.Duration, pass func()) {
	done := g.Done()
	g.Go(func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			pass()
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
			}
		}
	})
}

// Stop disarms every pending timer, refuses any new work, and waits for
// what is already running to finish. Call it before closing whatever
// the work uses. Idempotent, and safe on a Group that never started
// anything.
//
// Waiting rather than abandoning is deliberate: the problem is not the
// log line, it is work racing a closing database, and it can fail the
// status write that records an email went out. An armed timer is
// stopped rather than waited for, which for a process that is going
// away is the same outcome a restart has always had.
func (g *Group) Stop() {
	g.mu.Lock()
	if !g.stopped {
		g.stopped = true
		close(g.doneLocked())
		for t := range g.timers {
			t.Stop()
		}
		clear(g.timers)
	}
	g.mu.Unlock()
	g.wg.Wait()
}
