package perf

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// countingHandler counts slog records and keeps their text, so a test
// can assert both how often the warning fired and what it said.
type countingHandler struct {
	mu    sync.Mutex
	n     int
	lines bytes.Buffer
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	h.lines.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		h.lines.WriteString(" " + a.String())
		return true
	})
	h.lines.WriteString("\n")
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func warm(c *Recorder) { c.served = true }

// The budget is judged on the first byte; Total runs on to the end of
// the handler. A 200ms first byte on a GET is over a 150ms budget, and
// a second of work after it must not move the first-byte number.
func TestBudgetMeasuresFirstByteSeparatelyFromTotal(t *testing.T) {
	var c Recorder
	warm(&c)
	logs := &countingHandler{}
	opts := Options{Budget: DefaultBudget, ColdBudget: DefaultColdBudget, Logger: slog.New(logs), Screen: defaultScreen}
	r := httptest.NewRequest("GET", "/orders/RXS1?token=secret", nil)
	r.Pattern = "GET /orders/{id}"
	w, r2 := c.begin(httptest.NewRecorder(), r, opts)
	w.start = w.start.Add(-200 * time.Millisecond)
	Label(r2, "acme")
	w.WriteHeader(201)
	first := w.sample.TTFB
	w.start = w.start.Add(-time.Second)
	w.finish(r2)

	s := c.Snapshot()[0]
	if !s.Over || s.Status != 201 || s.Budget != 150 || s.Label != "acme" || s.Screen != "GET /orders/{id}" {
		t.Fatalf("sample: %+v", s)
	}
	if s.TTFB != first || s.Total < s.TTFB+900 {
		t.Fatalf("first byte and total mixed: %+v", s)
	}
	out := logs.lines.String()
	if strings.Contains(out, "RXS1") || strings.Contains(out, "secret") || !strings.Contains(out, "request over performance budget") {
		t.Fatalf("log line: %s", out)
	}
}

// A stream that starts fast is not a slow page, however long it runs:
// its first flush is its first byte.
func TestStreamingDoesNotTurnFastFirstByteIntoBudgetFailure(t *testing.T) {
	var c Recorder
	warm(&c)
	rec := httptest.NewRecorder()
	w, r2 := c.begin(rec, httptest.NewRequest("GET", "/", nil), Options{Budget: DefaultBudget, Screen: defaultScreen})
	if err := w.FlushError(); err != nil {
		t.Fatal(err)
	}
	w.start = w.start.Add(-time.Second)
	w.finish(r2)
	s := c.Snapshot()[0]
	if s.Over || s.Total < 1000 || !rec.Flushed {
		t.Fatalf("%+v flushed=%t", s, rec.Flushed)
	}
}

// The first request a process serves is cold: held to ColdBudget, and
// its header carries the startup time. The second is warm again.
func TestColdStartIsItsOwnNumber(t *testing.T) {
	var c Recorder
	h := Middleware(&c, Options{Started: time.Now().Add(-600 * time.Millisecond)})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	var headers []string
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		headers = append(headers, rec.Header().Get("Server-Timing"))
	}
	ss := c.Snapshot()
	if !ss[0].Cold || ss[0].Budget != 500 || ss[0].Startup < 600 || !ss[0].Over {
		t.Fatalf("cold: %+v", ss[0])
	}
	if !strings.Contains(headers[0], "startup;dur=") {
		t.Fatalf("cold header %q has no startup span", headers[0])
	}
	if ss[1].Cold || ss[1].Budget != 150 || ss[1].Startup != 0 || strings.Contains(headers[1], "startup") {
		t.Fatalf("warm: %+v header %q", ss[1], headers[1])
	}
}

// A write has no budget: sending the email is supposed to take the time
// it takes.
func TestWritesHaveNoBudget(t *testing.T) {
	var c Recorder
	warm(&c)
	w, r2 := c.begin(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), Options{Budget: DefaultBudget, Screen: defaultScreen})
	w.start = w.start.Add(-time.Second)
	w.WriteHeader(303)
	w.finish(r2)
	if s := c.Snapshot()[0]; s.Budget != 0 || s.Over {
		t.Fatalf("%+v", s)
	}
}

func TestPanicIsRecordedAndRethrown(t *testing.T) {
	var c Recorder
	h := Middleware(&c, Options{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	func() {
		defer func() {
			if recover() != "boom" {
				t.Fatal("panic swallowed or changed")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	if got := c.Snapshot()[0].Status; got != 500 {
		t.Fatalf("status=%d", got)
	}
}

func TestInformationalAndImplicitResponses(t *testing.T) {
	var c Recorder
	warm(&c)
	w, r2 := c.begin(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), Options{Screen: defaultScreen})
	w.WriteHeader(103)
	if w.committed {
		t.Fatal("103 committed the final response")
	}
	w.Write([]byte("body"))
	w.finish(r2)
	if s := c.Snapshot()[0]; s.Status != 200 || s.Budget != 0 || s.Over {
		t.Fatalf("%+v", s)
	}
}

func TestImplicitWritePreservesContentTypeDetection(t *testing.T) {
	var c Recorder
	rec := httptest.NewRecorder()
	h := Middleware(&c, Options{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("plain body"))
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Result().Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type=%q", got)
	}
}

func TestRingIsBoundedAndPercentilesAreOrdered(t *testing.T) {
	var c Recorder
	for i := 1; i <= Capacity+10; i++ {
		c.add(Sample{Screen: "GET /", Method: "GET", TTFB: float64(i)}, nil)
	}
	s := c.Snapshot()
	if len(s) != Capacity || s[0].TTFB != 11 || s[len(s)-1].TTFB != Capacity+10 {
		t.Fatalf("len=%d first=%v last=%v", len(s), s[0], s[len(s)-1])
	}
	var g Recorder
	for i := 1; i <= 100; i++ {
		g.add(Sample{Screen: "GET /", TTFB: float64(i), Total: float64(i * 2)}, nil)
	}
	row := g.Groups()[0]
	if row.P50 != 50 || row.P95 != 95 || row.Total95 != 190 || row.Requests != 100 {
		t.Fatalf("%+v", row)
	}
	if p := percentile(nil, 95); p != -1 {
		t.Fatalf("empty percentile = %v, want -1", p)
	}
}

// One screen logs once a minute; everything together logs at most 120
// lines a minute; and logging resumes when the minute turns.
func TestLogsAreRateLimitedAndResume(t *testing.T) {
	var c Recorder
	logs := &countingHandler{}
	log := slog.New(logs)
	now := time.Now()
	s := Sample{At: now, Label: "a", Screen: "GET /", Over: true}
	c.add(s, log)
	c.add(s, log)
	if logs.n != 1 {
		t.Fatalf("same-screen logs=%d", logs.n)
	}
	for i := 0; i < Capacity+1; i++ {
		s.Label = strings.Repeat("x", i%50) + string(rune('a'+i%26)) + time.Duration(i).String()
		c.add(s, log)
	}
	if logs.n != 120 {
		t.Fatalf("global log limit=%d", logs.n)
	}
	s.At = now.Add(time.Minute)
	c.add(s, log)
	if logs.n != 121 {
		t.Fatalf("logging did not resume: %d", logs.n)
	}
}

func TestRecorderConcurrentRequests(t *testing.T) {
	var c Recorder
	h := Middleware(&c, Options{Logger: slog.New(&countingHandler{})})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer Span(r, "db")()
		w.WriteHeader(200)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
				c.Groups()
			}
		}()
	}
	wg.Wait()
	if len(c.Snapshot()) != 200 {
		t.Fatal("samples lost")
	}
}

// The screen is the ServeMux's pattern, never the path — a path can
// carry a token or a reference, and the screen reaches log lines. A
// request the mux did not match is "unmatched", not its path.
func TestScreenIsThePatternNeverThePath(t *testing.T) {
	var c Recorder
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{token}", func(w http.ResponseWriter, r *http.Request) {})
	h := Middleware(&c, Options{})(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/secret-token-abcdef", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/nowhere/secret-token-abcdef", nil))
	ss := c.Snapshot()
	if ss[0].Screen != "GET /orders/{token}" || ss[1].Screen != "unmatched" {
		t.Fatalf("screens %q, %q", ss[0].Screen, ss[1].Screen)
	}
	for _, s := range ss {
		if strings.Contains(s.Screen, "secret") {
			t.Fatalf("a path value reached the screen: %q", s.Screen)
		}
	}
}

// Server-Timing names the first byte and every span, in a stable order,
// and spans of one name add up.
func TestServerTimingCarriesSpans(t *testing.T) {
	var c Recorder
	warm(&c)
	h := Middleware(&c, Options{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 3; i++ {
			end := Span(r, "db")
			time.Sleep(2 * time.Millisecond)
			end()
			end() // ending twice counts once
		}
		defer Span(r, "render")()
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	st := rec.Header().Get("Server-Timing")
	if !strings.HasPrefix(st, "app;dur=") || !strings.Contains(st, ", db;dur=") {
		t.Fatalf("Server-Timing = %q", st)
	}
	s := c.Snapshot()[0]
	if s.Spans["db"] < 6 || s.Spans["db"] > 1000 {
		t.Fatalf("db span = %v, want three ~2ms spans added once each", s.Spans["db"])
	}
	// render ended after the first byte: in the sample, not the header.
	if _, ok := s.Spans["render"]; !ok || strings.Contains(st, "render") {
		t.Fatalf("render span: sample %v header %q", s.Spans, st)
	}
}

// Probes and assets are not measured, and a request outside the
// middleware makes Span and Label no-ops.
func TestSkipAndUnmeasured(t *testing.T) {
	var c Recorder
	h := Middleware(&c, Options{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, p := range []string{"/healthz", "/api/version", "/static/app.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Header().Get("Server-Timing") != "" {
			t.Errorf("%s was measured", p)
		}
	}
	if n := len(c.Snapshot()); n != 0 {
		t.Fatalf("%d samples from skipped paths", n)
	}
	r := httptest.NewRequest("GET", "/", nil)
	Span(r, "db")()
	Label(r, "x")
}

type hijackWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestHijackDoesNotWriteAfterTakingConnection(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var c Recorder
	rec := httptest.NewRecorder()
	w, r2 := c.begin(hijackWriter{rec, a}, httptest.NewRequest("GET", "/ws", nil), Options{Screen: defaultScreen})
	conn, _, err := w.Hijack()
	if err != nil || conn != a {
		t.Fatalf("%v %v", conn, err)
	}
	w.finish(r2)
	if c.Snapshot()[0].Status != 101 || rec.Header().Get("Server-Timing") != "" {
		t.Fatal("wrote after hijack")
	}
}

func BenchmarkRequestRecording(b *testing.B) {
	var c Recorder
	h := Middleware(&c, Options{Logger: slog.New(&countingHandler{})})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	r := httptest.NewRequest("GET", "/orders/RXS1", nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}
