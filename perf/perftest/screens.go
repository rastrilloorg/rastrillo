package perftest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/perf"
)

// ScreenConfig is what Screens measures and how to reach it.
type ScreenConfig struct {
	// Routes is the app's chi router, walked for the inventory.
	Routes chi.Routes
	// Handler is what requests go through: rastrillo.Handler over the
	// app's options, so the measurement includes production's chrome.
	Handler http.Handler
	// Paths gives each pattern with a parameter a concrete path whose
	// row the test has seeded.
	Paths map[string]string
	// Expect overrides the 200 a pattern must answer.
	Expect map[string]int
	// Signin signs the client in once, for screens behind a session.
	Signin func(c *http.Client, base string)
	// Opaque lists the screens inside handlers chi cannot see into.
	Opaque []Opaque
}

// Opaque is one handler chi cannot walk, such as a generated
// http.ServeMux, and the screens it serves.
type Opaque struct {
	Patterns []string
	Screens  []OpaqueScreen
}

// OpaqueScreen is one screen inside an Opaque group. Key is its screen
// name, "GET /bookmarks/{id}", which is what a perf record names.
type OpaqueScreen struct {
	Key    string
	Path   string
	Expect int
}

var called atomic.Bool

// Screens is the module's one screen inventory: every GET route the
// router serves is measured, exempted, or skipped with a reason, and a
// record naming no screen fails here, because only here is the list
// complete. Never call it from a parallel test: a perf test sharing the
// machine with its siblings measures them.
func Screens(t testing.TB, cfg ScreenConfig) {
	t.Helper()
	if !called.CompareAndSwap(false, true) {
		t.Fatal("perftest.Screens called twice in one test binary: it is the module's one inventory, so stale records could not be judged")
	}
	if testing.Short() {
		t.Skip("perf: screens are measured in the perf lane, never under -short")
	}
	screens(t, cfg, load(t), budgetfile.Enforce(os.Getenv))
}

type target struct {
	screen string
	path   string
	expect int
	limit  time.Duration
	exempt bool
}

func screens(t testing.TB, cfg ScreenConfig, f *budgetfile.File, enforce bool) {
	t.Helper()
	targets, problems := inventory(cfg, f)
	for _, p := range problems {
		t.Errorf("%s", p)
	}
	if len(problems) > 0 {
		t.FailNow()
	}

	srv := httptest.NewServer(cfg.Handler)
	defer closeBounded(srv)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if cfg.Signin != nil {
		cfg.Signin(client, srv.URL)
	}

	for _, tg := range targets {
		med, worst, err := measure(client, srv.URL+tg.path, tg.expect, tg.limit)
		if errors.Is(err, errHung) {
			// The handler is still running and nothing can stop it; every
			// number after this one would include its goroutine.
			srv.CloseClientConnections()
			t.Errorf("%s: no complete response within %s; a handler that ignores its context stops the sweep here", tg.screen, 3*tg.limit+time.Second)
			t.FailNow()
		}
		if err != nil {
			t.Errorf("%s: %v", tg.screen, err)
			continue
		}
		over := med > tg.limit || worst > 3*tg.limit
		msg := fmt.Sprintf("%s: median %s, worst %s, budget %s", tg.screen, med.Round(time.Microsecond), worst.Round(time.Microsecond), tg.limit)
		switch {
		case over && enforce:
			say("%s: over budget", msg)
			t.Errorf("%s: over budget", msg)
		case over:
			say("%s: over budget (reported only; timing fails on CI)", msg)
		case tg.exempt && med*10 < tg.limit*6:
			say("%s: under 60%% of its record, consider lowering it", msg)
		default:
			say("%s", msg)
		}
	}
}

func inventory(cfg ScreenConfig, f *budgetfile.File) ([]target, []string) {
	var problems []string
	owner := map[string]int{}
	keys := map[string]bool{}
	for i, g := range cfg.Opaque {
		if len(g.Screens) == 0 {
			problems = append(problems, fmt.Sprintf("Opaque group %d lists no screens; to leave a mount unmeasured, record `perf GET <pattern> skip <reason>` instead", i))
		}
		for _, p := range g.Patterns {
			if prev, dup := owner[p]; dup {
				problems = append(problems, fmt.Sprintf("%s is in Opaque groups %d and %d; each mount belongs to exactly one", p, prev, i))
			}
			owner[p] = i
		}
		for _, s := range g.Screens {
			if !strings.HasPrefix(s.Key, "GET /") {
				problems = append(problems, fmt.Sprintf("Opaque screen key %q must be a screen name such as \"GET /bookmarks/{id}\"", s.Key))
			}
			if keys[s.Key] {
				problems = append(problems, fmt.Sprintf("Opaque screen key %s is declared twice", s.Key))
			}
			keys[s.Key] = true
		}
	}

	walked := map[string]bool{}
	var routes []string
	err := chi.Walk(cfg.Routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && !walked[route] {
			walked[route] = true
			routes = append(routes, route)
		}
		return nil
	})
	if err != nil {
		problems = append(problems, "chi.Walk: "+err.Error())
	}
	sort.Strings(routes)

	known := map[string]bool{}
	limit := func(screen string) (time.Duration, bool, bool) {
		if rec, ok := f.Perfs[screen]; ok {
			return rec.Limit, rec.Skip, true
		}
		return perf.DefaultBudget, false, false
	}
	var targets []target
	for _, route := range routes {
		screen := "GET " + route
		known[screen] = true
		lim, skip, exempt := limit(screen)
		_, grouped := owner[route]
		if skip {
			continue
		}
		if strings.HasSuffix(route, "/*") || grouped {
			if !grouped {
				problems = append(problems, fmt.Sprintf("%s is a mounted handler chi cannot see inside: list its screens in an Opaque group, or record `perf %s skip <reason>`", screen, screen))
			}
			continue
		}
		p := route
		if strings.Contains(route, "{") {
			if cfg.Paths[route] == "" {
				problems = append(problems, fmt.Sprintf("%s has a parameter and no Paths entry: give it a concrete path whose row the test seeds", screen))
				continue
			}
			p = cfg.Paths[route]
		}
		exp := http.StatusOK
		if s, ok := cfg.Expect[route]; ok {
			exp = s
		}
		targets = append(targets, target{screen, p, exp, lim, exempt})
	}
	for _, g := range cfg.Opaque {
		for _, p := range g.Patterns {
			if !walked[p] {
				problems = append(problems, fmt.Sprintf("Opaque group names %s, which the router does not serve: the group is stale", p))
			}
		}
		for _, s := range g.Screens {
			known[s.Key] = true
			lim, skip, exempt := limit(s.Key)
			if skip {
				continue
			}
			exp := s.Expect
			if exp == 0 {
				exp = http.StatusOK
			}
			targets = append(targets, target{s.Key, s.Path, exp, lim, exempt})
		}
	}
	for screen, rec := range f.Perfs {
		if !known[screen] {
			problems = append(problems, fmt.Sprintf("%s line %d: perf record for %s, which is not a screen any more: delete it", budgetfile.FileName, rec.Line, screen))
		}
	}
	sort.Strings(problems)
	return targets, problems
}

var errHung = errors.New("handler did not finish")

// measure makes 10 requests, discards 2, and returns the median and the
// worst of the other 8. TTFB is httptrace's GotFirstResponseByte: what a
// client observes, so a header the server buffered buys nothing.
func measure(c *http.Client, url string, expect int, limit time.Duration) (time.Duration, time.Duration, error) {
	var got []time.Duration
	for i := 0; i < 10; i++ {
		ttfb, err := once(c, url, expect, 3*limit+time.Second)
		if err != nil {
			return 0, 0, err
		}
		if i >= 2 {
			got = append(got, ttfb)
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	return (got[3] + got[4]) / 2, got[len(got)-1], nil
}

func once(c *http.Client, url string, expect int, deadline time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	var first time.Time
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotFirstResponseByte: func() { first = time.Now() }})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, errHung
		}
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expect {
		return 0, fmt.Errorf("answered %d, want %d: not measuring an error page (seed its rows, sign in with Signin, or set Expect)", resp.StatusCode, expect)
	}
	// A stream is measured to its first byte and then let go; anything
	// else is read to the end, so a handler that stalls mid-body is
	// caught by the same deadline as one that never answers.
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "text/event-stream" {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			if ctx.Err() != nil {
				return 0, errHung
			}
			return 0, err
		}
	}
	return first.Sub(start), nil
}

// closeBounded closes the test server without waiting forever on a
// handler that will not return.
func closeBounded(srv *httptest.Server) {
	done := make(chan struct{})
	go func() { srv.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
