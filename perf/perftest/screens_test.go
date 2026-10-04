package perftest

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// fakeT records failures without stopping the real test, so a fence
// can be shown going red. FailNow ends the goroutine screens runs on,
// exactly as testing.T's does.
type fakeT struct {
	testing.TB
	mu     sync.Mutex
	errs   []string
	failed bool
}

func (f *fakeT) Helper() {}
func (f *fakeT) Errorf(s string, a ...any) {
	f.mu.Lock()
	f.errs = append(f.errs, fmt.Sprintf(s, a...))
	f.failed = true
	f.mu.Unlock()
}
func (f *fakeT) Fatalf(s string, a ...any) { f.Errorf(s, a...); runtime.Goexit() }
func (f *fakeT) Fatal(a ...any)            { f.Errorf("%v", fmt.Sprint(a...)); runtime.Goexit() }
func (f *fakeT) FailNow()                  { f.failed = true; runtime.Goexit() }
func (f *fakeT) Logf(string, ...any)       {}

func run(cfg ScreenConfig, records string, enforce bool) *fakeT {
	f, err := budgetfile.Parse(strings.NewReader(records))
	if err != nil {
		panic(err)
	}
	ft := &fakeT{}
	done := make(chan struct{})
	go func() { defer close(done); screens(ft, cfg, f, enforce) }()
	<-done
	return ft
}

func router(routes map[string]http.HandlerFunc) chi.Router {
	r := chi.NewRouter()
	for p, h := range routes {
		r.Get(p, h)
	}
	return r
}

func ok(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }

func sleepy(d time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { time.Sleep(d); w.Write([]byte("ok")) }
}

func cfg(r chi.Router) ScreenConfig { return ScreenConfig{Routes: r, Handler: r} }

func errs(ft *fakeT) string { return strings.Join(ft.errs, "\n") }

// Every test runs in parallel: each drives its own server and fake T,
// and run serially this file alone was 19s, twice the package budget.
func TestFastScreenPasses(t *testing.T) {
	t.Parallel()
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": ok})), "", true); ft.failed {
		t.Fatal(errs(ft))
	}
}

func TestSlowScreenFailsWhenEnforcing(t *testing.T) {
	t.Parallel()
	r := router(map[string]http.HandlerFunc{"/slow": sleepy(200 * time.Millisecond)})
	if ft := run(cfg(r), "", true); !ft.failed {
		t.Fatal("200ms screen passed a 150ms budget")
	}
	if ft := run(cfg(r), "", false); ft.failed {
		t.Fatalf("without CI it must report, not fail: %v", errs(ft))
	}
}

func TestRecordRaisesTheLimit(t *testing.T) {
	t.Parallel()
	r := router(map[string]http.HandlerFunc{"/slow": sleepy(200 * time.Millisecond)})
	if ft := run(cfg(r), "perf GET /slow 400ms renders a report\n", true); ft.failed {
		t.Fatal(errs(ft))
	}
}

func TestOneSlowRequestInAPassingMedianFails(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	h := func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 6 {
			time.Sleep(500 * time.Millisecond) // over 3 x 150ms
		}
		w.Write([]byte("ok"))
	}
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": h})), "", true); !ft.failed {
		t.Fatal("a 500ms request inside a fast median passed")
	}
}

// TTFB is what the client sees: a header written at once but buffered
// by the server does not excuse a body that arrives 300ms later.
func TestBufferedHeaderLateBodyFails(t *testing.T) {
	t.Parallel()
	h := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte("late"))
	}
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": h})), "", true); !ft.failed {
		t.Fatal("a buffered WriteHeader hid a 300ms body")
	}
}

// A response that reached the client quickly is within budget however
// long its body takes, as package perf rules in production: a download
// that started at once is not a slow screen.
func TestFlushedHeaderThenSlowBodyPasses(t *testing.T) {
	t.Parallel()
	h := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte("rest"))
	}
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": h})), "", true); ft.failed {
		t.Fatal(errs(ft))
	}
}

func TestErrorPageIsNotMeasured(t *testing.T) {
	t.Parallel()
	h := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/signin", http.StatusSeeOther) }
	c := cfg(router(map[string]http.HandlerFunc{"/": h}))
	ft := run(c, "", true)
	if !ft.failed || !strings.Contains(errs(ft), "error page") {
		t.Fatalf("303 was measured: %v", errs(ft))
	}
	c.Expect = map[string]int{"/": http.StatusSeeOther}
	if ft := run(c, "", true); ft.failed {
		t.Fatalf("Expect did not accept its status: %v", errs(ft))
	}
}

func opaqueRouter() chi.Router {
	r := chi.NewRouter()
	inner := http.NewServeMux()
	inner.HandleFunc("GET /bookmarks/{id}", ok)
	r.Handle("/bookmarks/*", inner)
	return r
}

func TestOpaqueMountMustBeGroupedOrSkipped(t *testing.T) {
	t.Parallel()
	if ft := run(cfg(opaqueRouter()), "", true); !ft.failed {
		t.Fatal("an opaque mount with no group passed")
	}
	c := cfg(opaqueRouter())
	c.Opaque = []Opaque{{Patterns: []string{"/bookmarks/*"}, Screens: []OpaqueScreen{{Key: "GET /bookmarks/{id}", Path: "/bookmarks/1"}}}}
	if ft := run(c, "perf GET /bookmarks/{id} 300ms generated screen\n", true); ft.failed {
		t.Fatalf("grouped opaque mount, with a record on its key: %v", errs(ft))
	}
	if ft := run(cfg(opaqueRouter()), "perf GET /bookmarks/* skip generated, measured elsewhere\n", true); ft.failed {
		t.Fatalf("skipped opaque mount: %v", errs(ft))
	}
}

func TestMountedServeMuxIsOpaque(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	inner := http.NewServeMux()
	inner.HandleFunc("GET /x", ok)
	r.Mount("/bookmarks", inner)
	ft := run(cfg(r), "", true)
	if !ft.failed || !strings.Contains(errs(ft), "/bookmarks/*") {
		t.Fatalf("a Mount of a ServeMux must be reported as opaque: %v", errs(ft))
	}
}

func TestOpaqueGroupValidation(t *testing.T) {
	t.Parallel()
	screen := []OpaqueScreen{{Key: "GET /bookmarks/{id}", Path: "/bookmarks/1"}}
	cases := map[string][]Opaque{
		"two owners":    {{Patterns: []string{"/bookmarks/*"}, Screens: screen}, {Patterns: []string{"/bookmarks/*"}, Screens: []OpaqueScreen{{Key: "GET /bookmarks/new", Path: "/bookmarks/new"}}}},
		"empty group":   {{Patterns: []string{"/bookmarks/*"}}},
		"duplicate key": {{Patterns: []string{"/bookmarks/*"}, Screens: append(screen, screen[0])}},
		"bad key":       {{Patterns: []string{"/bookmarks/*"}, Screens: []OpaqueScreen{{Key: "/bookmarks/{id}", Path: "/bookmarks/1"}}}},
		"stale pattern": {{Patterns: []string{"/bookmarks/*", "/gone/*"}, Screens: screen}},
	}
	for name, groups := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := cfg(opaqueRouter())
			c.Opaque = groups
			if ft := run(c, "", true); !ft.failed {
				t.Fatal("accepted")
			}
		})
	}
}

func TestStalePerfRecordFails(t *testing.T) {
	t.Parallel()
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": ok})), "perf GET /gone 1s removed screen\n", true); !ft.failed {
		t.Fatal("a record naming no screen passed")
	}
}

func TestParamWithoutPathFails(t *testing.T) {
	t.Parallel()
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/notes/{id}": ok})), "", true); !ft.failed {
		t.Fatal("a {param} route with no Paths entry was skipped silently")
	}
}

func TestEventStreamIsMeasuredToFirstByteAndClosed(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{"text/event-stream", "text/event-stream; charset=utf-8"} {
		h := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
		start := time.Now()
		if ft := run(cfg(router(map[string]http.HandlerFunc{"/events": h})), "", true); ft.failed {
			t.Fatalf("SSE %q: %v", ct, errs(ft))
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("SSE %q hung the sweep", ct)
		}
	}
}

// Review focus 4: a handler that ignores its context stops the sweep,
// before or after its headers, and nothing after it is measured.
func TestHandlerIgnoringItsContextStopsTheSweep(t *testing.T) {
	t.Parallel()
	for name, stuck := range map[string]func(block chan struct{}) http.HandlerFunc{
		"before headers": func(block chan struct{}) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) { <-block }
		},
		"after headers": func(block chan struct{}) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-block
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			block := make(chan struct{})
			defer close(block)
			var later atomic.Int32
			after := func(w http.ResponseWriter, r *http.Request) { later.Add(1); w.Write([]byte("ok")) }
			start := time.Now()
			ft := run(cfg(router(map[string]http.HandlerFunc{"/stuck": stuck(block), "/zzz": after})), "", true)
			if !ft.failed {
				t.Fatal("a stuck handler passed")
			}
			if n := later.Load(); n != 0 {
				t.Fatalf("the sweep measured %d request(s) beside a stuck handler", n)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatalf("sweep took %s; the deadline is 3 x 150ms + 1s plus a 2s close bound", time.Since(start))
			}
		})
	}
}

// A sign-in that never answers must fail the sweep in bounded time, not
// hang the lane until go test's own timeout.
func TestHangingSigninStopsTheSweep(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	defer close(block)
	r := router(map[string]http.HandlerFunc{"/": ok})
	r.Post("/signin", func(w http.ResponseWriter, req *http.Request) { <-block })
	c := cfg(r)
	c.Signin = func(cl *http.Client, base string) { cl.PostForm(base+"/signin", nil) }
	start := time.Now()
	ft := run(c, "", true)
	if !ft.failed || !strings.Contains(errs(ft), "Signin") {
		t.Fatalf("a hanging sign-in did not fail the sweep: %v", errs(ft))
	}
	if time.Since(start) > signinBound+5*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
}

// A stream that has answered but ignores its cancellation keeps running
// after the client lets go; it must stop the sweep like any other hung
// handler, before anything else is measured beside it.
func TestStreamIgnoringCancellationStopsTheSweep(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	defer close(block)
	var later atomic.Int32
	r := router(map[string]http.HandlerFunc{
		"/events": func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-block
		},
		"/zzz": func(w http.ResponseWriter, req *http.Request) { later.Add(1); w.Write([]byte("ok")) },
	})
	ft := run(cfg(r), "", true)
	if !ft.failed {
		t.Fatal("a stream that never finished passed")
	}
	if n := later.Load(); n != 0 {
		t.Fatalf("measured %d request(s) beside a stream still running", n)
	}
}
