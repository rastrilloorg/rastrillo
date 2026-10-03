package main

// The scaffold's budget files: the perf lane's tests and the empty
// exemptions file. Kept beside new.go's other templates, delivered once
// and app-owned from then on.

// perfTestTemplate is internal/<pkg>test/perf_test.go. %[1]s is the
// module name, %[2]s the app package.
const perfTestTemplate = `//go:build perf

package %[2]stest

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/perf/perftest"

	%[2]s "%[1]s/internal/%[2]s"
)

// The perf lane: make perf, serial and uncached, failing only on CI.
// Every GET screen the router serves is held to 150ms to first byte; a
// new route fails here until it is measured or has a record in
// .rastrillo/budgets.txt. Never t.Parallel() in this file: a perf test
// sharing the machine measures its neighbours.
func TestPerfScreens(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r, err := %[2]s.Router(d, testOrigin, logger)
	if err != nil {
		t.Fatal(err)
	}
	opts := rastrillo.Options{Logger: logger}
	%[2]s.Configure(&opts, %[2]s.Mux(r), time.Now())
	h, closeAll, err := rastrillo.Handler(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	// Screens behind sign-in need Signin; a {param} route needs a Paths
	// entry whose row this test seeds first.
	perftest.Screens(t, perftest.ScreenConfig{Routes: r, Handler: h})
}

// A cold start, from exec to the first byte of "/", held to 500ms.
func TestPerfBoot(t *testing.T) {
	perftest.Boot(t, perftest.BootConfig{
		Prepare: schema.Path,
		Build: func(path string) (http.Handler, func(), error) {
			started := time.Now()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			d, err := db.Open(path, logger)
			if err != nil {
				return nil, nil, err
			}
			mux, err := %[2]s.App(d, testOrigin, logger)
			if err != nil {
				d.Close()
				return nil, nil, err
			}
			opts := rastrillo.Options{Logger: logger}
			%[2]s.Configure(&opts, mux, started)
			h, closeAll, err := rastrillo.Handler(opts)
			if err != nil {
				d.Close()
				return nil, nil, err
			}
			return h, func() { closeAll(); d.Close() }, nil
		},
		Path: "/",
	})
}
`

const budgetsTxtTemplate = `# Exceptions to this app's budgets. Empty is the goal.
#
# Defaults: 5,000 source and 8,000 test lines per directory; 10s per
# test package; 150ms to first byte per GET screen; 500ms cold boot.
# A record exists only where something cannot meet them, and every
# record says why. docs/site/testing.md has the full grammar.
#
#   size  <dir>             <source>|-  <test>|-  <reason>
#   time  <dir>             <duration>            <reason>
#   perf  <METHOD> <path>   <duration>|skip       <reason>
#   boot                    <duration>            <reason>
`
