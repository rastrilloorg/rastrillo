// Package perf measures every request against a time budget and says
// where the time went, as middleware an app mounts with Options.Wrap.
//
// Each response carries a Server-Timing header — the time to its first
// byte, and a span for each part of the work the handler marked with
// Span — so the numbers are in the browser's network panel, on the
// request that was slow, with nothing else to run. Each request also
// leaves a Sample in a bounded ring (Recorder), which Groups turns into
// per-route p50/p95 for a dashboard of your own.
//
// Two numbers, kept apart on purpose. The budget is judged on time to
// FIRST BYTE, because that is the wait a person feels; Total runs to
// the end of the handler, and folding it in would turn every download,
// stream or long poll into a budget failure it is not. A read (GET or
// HEAD) is held to Budget; a write has no budget, because a write that
// sends an email or charges a card is supposed to take the time it
// takes.
//
// The first request a process serves is held to ColdBudget instead,
// and its Server-Timing carries the process's startup time: an app that
// hibernates between visits (every CARLOS app) pays its whole boot on
// someone's click, and that has to be visible as its own number rather
// than as a slow page nobody can reproduce warm.
//
// A request's grouping name (its Screen) is the route PATTERN, never the
// path: /orders/{id}, not /orders/RXS1. A path can carry a capability
// token or a reference, and an over-budget warning is a log line that
// outlives the request; Tito Go learned that the first time a slow-page
// log printed a buyer's order-page token.
//
// Ported from Tito Go's internal/perf. Tito's two-level router→instance
// timing and its route-word screen naming stay in Tito.
package perf

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Capacity is how many samples a Recorder keeps: the most recent, in a
// ring, so a busy process's memory is bounded by construction.
const Capacity = 2048

// Default budgets, measured to first byte. Tito's, from its own screens:
// 150ms is where a warm page stops feeling instant, and 500ms is what a
// cold boot may add before it becomes the thing people complain about.
const (
	DefaultBudget     = 150 * time.Millisecond
	DefaultColdBudget = 500 * time.Millisecond
)

// Sample is one request.
type Sample struct {
	At     time.Time          `json:"at"`
	Screen string             `json:"screen"`
	Method string             `json:"method"`
	Label  string             `json:"label,omitempty"`
	Status int                `json:"status"`
	TTFB   float64            `json:"ttfb_ms"`
	Total  float64            `json:"total_ms"`
	Spans  map[string]float64 `json:"spans_ms,omitempty"`
	// Startup is the process's boot time, on the cold request only.
	Startup float64 `json:"startup_ms,omitempty"`
	Budget  float64 `json:"budget_ms"`
	Cold    bool    `json:"cold"`
	Over    bool    `json:"over_budget"`
}

// Options configures Middleware. The zero value is usable.
type Options struct {
	// Screen names a request for grouping. Nil uses the pattern the
	// ServeMux matched (r.Pattern), or "unmatched". Whatever it returns
	// lands in log lines: never return anything taken from the path.
	Screen func(r *http.Request) string
	// Skip leaves a request unmeasured. Nil skips /healthz,
	// /api/version and anything under /static/ — probes and assets,
	// which would crowd real pages out of the ring.
	Skip func(r *http.Request) bool
	// Budget is the first-byte budget for a GET or HEAD; zero means
	// DefaultBudget. ColdBudget is the same for the first request the
	// process serves; zero means DefaultColdBudget.
	Budget, ColdBudget time.Duration
	// Started is when the process started. The boot time the cold
	// request reports is from Started to the moment the middleware is
	// applied to the app's handler — under Options.Wrap, after the
	// database is open, migrations have run and the routes are built,
	// which is when the app has finished starting. Measuring to the
	// first request instead would count however long the process then
	// sat idle. Zero reports no startup span (the request is still held
	// to ColdBudget).
	Started time.Time
	// Logger receives the over-budget warnings. Nil is slog.Default().
	Logger *slog.Logger

	screenIsDefault bool
}

// Recorder is the ring of recent samples. The zero value is ready; one
// per process, shared by every request.
type Recorder struct {
	mu      sync.Mutex
	samples []Sample
	next    int
	served  bool

	// lostRoute is the one warning that a request was served but its
	// route pattern never reached the middleware: something between it
	// and the mux copied the request. Once, because it is a wiring
	// mistake, not an event.
	lostRoute sync.Once

	// Over-budget logging is throttled twice: once a minute per
	// screen, and 120 lines a minute in all. A slow dependency makes
	// every page slow at once, and the log is where someone goes to
	// find out why — it must not be the thing that drowns.
	logWindow time.Time
	logCount  int
	lastLog   map[string]time.Time
}

// Snapshot returns the samples held, oldest first.
func (c *Recorder) Snapshot() []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Sample, 0, len(c.samples))
	if len(c.samples) == Capacity {
		out = append(out, c.samples[c.next:]...)
		return append(out, c.samples[:c.next]...)
	}
	return append(out, c.samples...)
}

// Groups summarises the samples held; see the package function.
func (c *Recorder) Groups() []Group { return Groups(c.Snapshot()) }

// Group is one screen, method and label's requests.
type Group struct {
	Screen   string  `json:"screen"`
	Method   string  `json:"method"`
	Label    string  `json:"label,omitempty"`
	Requests int     `json:"requests"`
	Over     int     `json:"over_budget"`
	P50      float64 `json:"p50_ms"`
	P95      float64 `json:"p95_ms"`
	Total95  float64 `json:"total_p95_ms"`
}

// Groups buckets samples by screen, method and label, with first-byte
// p50 and p95 and total p95, slowest p95 first.
func Groups(samples []Sample) []Group {
	type key struct{ screen, method, label string }
	buckets := map[key][]Sample{}
	for _, s := range samples {
		k := key{s.Screen, s.Method, s.Label}
		buckets[k] = append(buckets[k], s)
	}
	out := make([]Group, 0, len(buckets))
	for k, ss := range buckets {
		g := Group{Screen: k.screen, Method: k.method, Label: k.label, Requests: len(ss)}
		ttfb := make([]float64, 0, len(ss))
		total := make([]float64, 0, len(ss))
		for _, s := range ss {
			if s.Over {
				g.Over++
			}
			ttfb = append(ttfb, s.TTFB)
			total = append(total, s.Total)
		}
		g.P50 = percentile(ttfb, 50)
		g.P95 = percentile(ttfb, 95)
		g.Total95 = percentile(total, 95)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].P95 != out[j].P95 {
			return out[i].P95 > out[j].P95
		}
		a, b := out[i], out[j]
		return a.Label+"\x00"+a.Method+"\x00"+a.Screen < b.Label+"\x00"+b.Method+"\x00"+b.Screen
	})
	return out
}

// percentile is nearest-rank: the smallest value with at least p% of
// the values at or below it. -1 for no values, so an empty group reads
// as "no data" rather than as a suspiciously fast zero.
func percentile(values []float64, p int) float64 {
	if len(values) == 0 {
		return -1
	}
	sort.Float64s(values)
	return values[(len(values)*p+99)/100-1]
}

func (c *Recorder) add(s Sample, log *slog.Logger) {
	c.mu.Lock()
	if len(c.samples) < Capacity {
		c.samples = append(c.samples, s)
	} else {
		c.samples[c.next] = s
	}
	c.next = (c.next + 1) % Capacity
	key := s.Label + "\x00" + s.Method + "\x00" + s.Screen
	emit := s.Over && (c.lastLog == nil || s.At.Sub(c.lastLog[key]) >= time.Minute)
	if s.At.Sub(c.logWindow) >= time.Minute {
		c.logWindow = s.At
		c.logCount = 0
	}
	emit = emit && c.logCount < 120
	if emit {
		c.logCount++
		if len(c.lastLog) >= Capacity {
			for k, at := range c.lastLog {
				if s.At.Sub(at) >= time.Minute {
					delete(c.lastLog, k)
				}
			}
		}
		if c.lastLog == nil {
			c.lastLog = map[string]time.Time{}
		}
		c.lastLog[key] = s.At
	}
	c.mu.Unlock()
	if emit && log != nil {
		log.Warn("request over performance budget",
			"screen", s.Screen, "method", s.Method, "label", s.Label, "status", s.Status,
			"ttfb_ms", s.TTFB, "total_ms", s.Total, "budget_ms", s.Budget, "cold", s.Cold, "spans_ms", s.Spans)
	}
}

// Middleware measures every request next serves that opts.Skip does
// not skip, recording into c.
func Middleware(c *Recorder, opts Options) func(http.Handler) http.Handler {
	if opts.Budget <= 0 {
		opts.Budget = DefaultBudget
	}
	if opts.ColdBudget <= 0 {
		opts.ColdBudget = DefaultColdBudget
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Skip == nil {
		opts.Skip = defaultSkip
	}
	if opts.Screen == nil {
		opts.Screen = defaultScreen
		opts.screenIsDefault = true
	}
	return func(next http.Handler) http.Handler {
		// Here, not when Middleware was called: Wrap applies this to a
		// handler the framework has only just finished building, and
		// that building is the start-up being measured.
		var boot time.Duration
		if !opts.Started.IsZero() {
			boot = time.Since(opts.Started)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opts.Skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			q, r2 := c.begin(w, r, opts, boot)
			defer q.finish(r2)
			next.ServeHTTP(q, r2)
		})
	}
}

func defaultSkip(r *http.Request) bool {
	p := r.URL.Path
	return p == "/healthz" || p == "/api/version" || strings.HasPrefix(p, "/static/")
}

func defaultScreen(r *http.Request) string {
	// A CONNECT is the one request ServeMux answers with a redirect
	// whose Pattern is the concrete destination path rather than a
	// registered pattern — /orders/secret/, token and all. No page an
	// app serves is a CONNECT, so it is never named.
	if r.Pattern != "" && r.Method != http.MethodConnect {
		return r.Pattern
	}
	return "unmatched"
}

type contextKey struct{}

// request is the ResponseWriter wrapper: it stamps the first byte, adds
// Server-Timing before the headers go, and keeps Flush and Hijack
// working through it.
type request struct {
	http.ResponseWriter
	recorder  *Recorder
	opts      *Options
	start     time.Time
	mu        sync.Mutex
	sample    Sample
	committed bool
	startup   time.Duration
}

func (c *Recorder) begin(w http.ResponseWriter, r *http.Request, opts Options, boot time.Duration) (*request, *http.Request) {
	s := Sample{At: time.Now().UTC(), Method: method(r.Method)}
	c.mu.Lock()
	s.Cold = !c.served
	c.served = true
	c.mu.Unlock()
	budget := opts.Budget
	if s.Cold {
		budget = opts.ColdBudget
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		s.Budget = ms(budget)
	}
	q := &request{ResponseWriter: w, recorder: c, opts: &opts, start: time.Now(), sample: s}
	if s.Cold {
		q.startup = boot
	}
	r2 := r.WithContext(context.WithValue(r.Context(), contextKey{}, q))
	// A mux outside this one — the framework's own, under Options.Wrap —
	// has already set Pattern (to "/"). Cleared on this copy, so the
	// pattern read back is the app mux's or nothing: an inherited "/"
	// would group every route as "/" when middleware between here and
	// the app mux copies the request, and the lost-route warning could
	// never fire.
	r2.Pattern = ""
	return q, r2
}

// method folds anything that is not a standard method into OTHER, so a
// client inventing methods cannot mint unbounded groups.
func method(m string) string {
	switch m {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT":
		return m
	}
	return "OTHER"
}

func state(r *http.Request) *request {
	q, _ := r.Context().Value(contextKey{}).(*request)
	return q
}

// Label tags the request's sample with a grouping label of the app's
// choosing — the account or tenant it resolved, say — so Groups can
// split one route's numbers by it. Like Screen, it lands in log lines.
// A no-op on a request the middleware did not measure.
func Label(r *http.Request, label string) {
	if q := state(r); q != nil {
		q.mu.Lock()
		q.sample.Label = label
		q.mu.Unlock()
	}
}

// Span starts timing one named part of the request's work and returns
// the func that ends it; call it with defer. Spans of the same name add
// up, so a loop of queries is one "db" span. Each name becomes a
// Server-Timing metric, so keep names to a few fixed words
// ("db", "render") — never a value. A span that ends after the first
// byte counts toward Total but misses the header, which has gone.
// A no-op on a request the middleware did not measure.
func Span(r *http.Request, name string) func() {
	q := state(r)
	if q == nil {
		return func() {}
	}
	start := time.Now()
	var once sync.Once
	return func() {
		once.Do(func() {
			d := ms(time.Since(start))
			q.mu.Lock()
			if q.sample.Spans == nil {
				q.sample.Spans = map[string]float64{}
			}
			q.sample.Spans[name] += d
			q.mu.Unlock()
		})
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (w *request) WriteHeader(status int) {
	// 1xx other than 101 are informational: more headers follow, and
	// the real response has not started.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.commit(status) {
		w.ResponseWriter.WriteHeader(status)
	}
}

// commit stamps the first byte and writes Server-Timing while headers
// can still change. It reports whether this call was the first.
func (w *request) commit(status int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		return false
	}
	w.committed = true
	w.sample.Status = status
	w.sample.TTFB = ms(time.Since(w.start))
	names := make([]string, 0, len(w.sample.Spans))
	for name := range w.sample.Spans {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "app;dur=%.3f", w.sample.TTFB)
	for _, name := range names {
		fmt.Fprintf(&b, ", %s;dur=%.3f", name, w.sample.Spans[name])
	}
	if w.startup > 0 {
		w.sample.Startup = ms(w.startup)
		fmt.Fprintf(&b, ", startup;dur=%.3f", w.sample.Startup)
	}
	w.Header().Add("Server-Timing", b.String())
	return true
}

func (w *request) Write(b []byte) (int, error) {
	w.commit(http.StatusOK)
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (w *request) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *request) Flush() { _ = w.FlushError() }

// FlushError commits first: a stream's first flush IS its first byte,
// which is why a fast-starting stream passes its budget however long
// it then runs.
func (w *request) FlushError() error {
	w.commit(http.StatusOK)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack hands the connection over; nothing may be written after, so
// the sample is closed as a 101 without touching headers.
func (w *request) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.mu.Lock()
		if !w.committed {
			w.committed = true
			w.sample.Status = http.StatusSwitchingProtocols
			w.sample.TTFB = ms(time.Since(w.start))
		}
		w.mu.Unlock()
	}
	return conn, rw, err
}

// finish records the sample. A panic is recorded as a 500 and then
// re-raised untouched: the recovery above this middleware owns the
// response, and measuring must never change what a failure looks like.
func (w *request) finish(r *http.Request) {
	p := recover()
	w.mu.Lock()
	committed := w.committed
	w.mu.Unlock()
	if !committed {
		if p != nil {
			w.mu.Lock()
			w.committed = true
			w.sample.Status = http.StatusInternalServerError
			w.sample.TTFB = ms(time.Since(w.start))
			w.mu.Unlock()
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}
	w.mu.Lock()
	w.sample.Screen = w.opts.Screen(r)
	// A request that was served — not a 404 or 405, which match no
	// route — yet reached here with no pattern had its pattern set on a
	// copy: middleware between this and the mux called r.WithContext.
	// Every route would then read as one "unmatched" group, so say so,
	// once, rather than let the numbers mislead quietly.
	lost := w.opts.screenIsDefault && r.Pattern == "" &&
		w.sample.Status != http.StatusNotFound && w.sample.Status != http.StatusMethodNotAllowed
	w.sample.Total = ms(time.Since(w.start))
	first := w.sample.TTFB
	if w.sample.Cold {
		first += ms(w.startup)
	}
	w.sample.Over = w.sample.Budget > 0 && first > w.sample.Budget
	s := w.sample
	// The ring keeps its own copy of the spans: a Span ended late, by
	// a goroutine the handler left behind, must not write into a
	// sample a reader of the ring is looking at.
	if s.Spans != nil {
		spans := make(map[string]float64, len(s.Spans))
		for k, v := range s.Spans {
			spans[k] = v
		}
		s.Spans = spans
	}
	w.mu.Unlock()
	if lost {
		w.recorder.lostRoute.Do(func() {
			w.opts.Logger.Warn("perf: a request was served but its route pattern never reached perf.Middleware; " +
				"mount it last in Options.Wrap, directly around the mux, or every route groups as \"unmatched\"")
		})
	}
	w.recorder.add(s, w.opts.Logger)
	if p != nil {
		panic(p)
	}
}
