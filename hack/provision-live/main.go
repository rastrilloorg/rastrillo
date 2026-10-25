// Command provision-live checks, from outside, what the provision package
// reads from the Carlos console: whether a create is taken as 201 or 202,
// what the edge answers before the instance's route exists, and that a
// repeated create is the 409 provision reads as ErrHostTaken. The fake in
// provisiontest encodes those answers as read from the platform's handlers;
// only the running console shows which of them it gives.
//
// It has two modes, because the same binary is what the check creates
// instances of:
//
//	provision-live check        run from DOCS-T1; reads PROVISION_CONSOLE,
//	                            PROVISION_ACCOUNT, PROVISION_APP,
//	                            PROVISION_TOKEN and PROVISION_DOMAIN, creates
//	                            one instance and prints one JSON object
//	provision-live --socket S   what the platform runs: a minimal rastrillo
//	                            app that names its instance on /api/version
//
// The check never deletes the instance it creates. Deleting is a person's
// act (spec section 7.5), and the approval for a run lists it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/provision"

	// Serve refuses to start without the default locale's pack linked, and
	// an instance that exits at boot reads on the platform as one that
	// never becomes ready (the failure issue #61 hit).
	_ "amadan.net/rastrillo/rastrillo/locales/en"
)

// readyWithin is how long the check waits for the instance to answer. The
// pending 503 is expected first; an instance that is still not up after this
// is a finding, not something to wait out.
const readyWithin = 3 * time.Minute

// checkConfig is what check needs: the five PROVISION_* values and the
// interval between Ready probes (2 seconds in main, milliseconds in the
// test).
type checkConfig struct {
	Console, Account, App, Token, Domain string
	Every                                time.Duration
}

// result is the object the check prints. Everything the person approving
// the run wants to compare with the fake is here: each console exchange,
// each probe, and what the two repeated creates came to.
type result struct {
	OK   bool   `json:"ok"`
	Host string `json:"host"`
	// Create is how the first create was taken: "created" (201) or
	// "accepted" (202, the host claim is on).
	Create    string              `json:"create,omitempty"`
	Exchanges []exchange          `json:"exchanges"`
	Probes    []probe             `json:"probes"`
	Instance  *provision.Instance `json:"instance,omitempty"`
	Repeat    string              `json:"repeat"`
	Resume    string              `json:"resume"`
	Error     string              `json:"error,omitempty"`
}

// exchange is one console attempt, as provision's Observe saw it.
type exchange struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Status  int         `json:"status"`
	Header  http.Header `json:"header,omitempty"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// probe is one Ready probe of the new instance's /api/version.
type probe struct {
	At       string `json:"at"`
	Status   int    `json:"status"`
	Version  string `json:"version,omitempty"`
	Instance string `json:"instance,omitempty"`
	Build    string `json:"build,omitempty"`
	Error    string `json:"error,omitempty"`
}

// check creates one instance and reads back what the console and the edge
// say. It returns the result as far as it got, with an error naming the
// step that did not give the expected answer. hc is the client for the
// console and the probes: http.DefaultClient in main, the fake's in the test,
// so the test never reaches a network.
func check(ctx context.Context, cfg checkConfig, hc *http.Client) (result, error) {
	res := result{Exchanges: []exchange{}, Probes: []probe{}}
	var mu sync.Mutex
	c, err := provision.New(provision.Config{
		Console: cfg.Console, Account: cfg.Account, App: cfg.App, Token: cfg.Token, HTTP: hc,
		Observe: func(ex provision.Exchange) {
			e := exchange{Method: ex.Method, Path: ex.Path, Status: ex.Status, Header: ex.Header.Clone(), Message: ex.Message}
			// A cookie in a response is not what the run is about, and the
			// output is pasted into a thread.
			e.Header.Del("Set-Cookie")
			if ex.Err != nil {
				e.Error = ex.Err.Error()
			}
			mu.Lock()
			res.Exchanges = append(res.Exchanges, e)
			mu.Unlock()
		},
	})
	if err != nil {
		return res, err
	}
	fail := func(format string, a ...any) (result, error) {
		err := fmt.Errorf(format, a...)
		res.Error = err.Error()
		return res, err
	}

	label, err := provision.NewLabel("live")
	if err != nil {
		return fail("label: %w", err)
	}
	res.Host = label + "." + cfg.Domain
	created, err := c.Create(ctx, provision.CreateRequest{Host: res.Host, Message: "provision-live check"})
	if err != nil {
		return fail("create: %w", err)
	}
	res.Create = "created"
	if created.Accepted {
		res.Create = "accepted"
	}

	start := time.Now()
	wait, cancel := context.WithTimeout(ctx, readyWithin)
	defer cancel()
	for {
		ok, a, perr := provision.Ready(wait, hc, res.Host)
		p := probe{At: time.Since(start).Round(time.Millisecond).String(), Status: a.Status,
			Version: a.Version, Instance: a.Instance, Build: a.Build}
		if perr != nil {
			p.Error = perr.Error()
		}
		res.Probes = append(res.Probes, p)
		if ok {
			break
		}
		select {
		case <-wait.Done():
			return fail("%s is not ready after %v (last status %d)", res.Host, readyWithin, a.Status)
		case <-time.After(cfg.Every):
		}
	}

	in, err := c.Instance(ctx, res.Host)
	if err != nil {
		return fail("instance: %w", err)
	}
	res.Instance = &in

	// The same host again, with no Resume: the platform answers 409, and
	// the GET finds the instance, which is the collision Create reports.
	if _, err := c.Create(ctx, provision.CreateRequest{Host: res.Host}); !errors.Is(err, provision.ErrHostTaken) {
		return fail("a repeated create gave %v, want ErrHostTaken", err)
	}
	res.Repeat = "host taken"

	// With Resume the host is the operation's identity: the instance is
	// ours, and nothing is posted.
	again, err := c.Create(ctx, provision.CreateRequest{Host: res.Host, Resume: true})
	if err != nil || !again.Landed {
		return fail("a resumed create gave %+v, %v, want Landed", again, err)
	}
	res.Resume = "landed"

	res.OK = true
	return res, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "check" {
		os.Exit(runCheck())
	}
	if err := serve(); err != nil {
		slog.Error("provision-live", "err", err)
		os.Exit(1)
	}
}

// runCheck is main's check mode: environment in, one JSON object out. The
// exit status is 1 when the run is not ok, so a script can tell.
func runCheck() int {
	cfg := checkConfig{
		Console: os.Getenv("PROVISION_CONSOLE"), Account: os.Getenv("PROVISION_ACCOUNT"),
		App: os.Getenv("PROVISION_APP"), Token: os.Getenv("PROVISION_TOKEN"),
		Domain: os.Getenv("PROVISION_DOMAIN"), Every: 2 * time.Second,
	}
	for name, v := range map[string]string{
		"PROVISION_CONSOLE": cfg.Console, "PROVISION_ACCOUNT": cfg.Account, "PROVISION_APP": cfg.App,
		"PROVISION_TOKEN": cfg.Token, "PROVISION_DOMAIN": cfg.Domain,
	} {
		if v == "" {
			fmt.Fprintf(os.Stderr, "provision-live: %s is not set\n", name)
			return 2
		}
	}
	res, err := check(context.Background(), cfg, http.DefaultClient)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if eerr := enc.Encode(res); eerr != nil {
		fmt.Fprintln(os.Stderr, "provision-live:", eerr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "provision-live:", err)
		return 1
	}
	return 0
}

// serve is what the platform runs: `<binary> --socket <path> --db <path>`.
// NameInstance makes /api/version carry Rastrillo-Host, which is how Ready
// tells this instance from a catch-all's 200.
func serve() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	opts, err := rastrillo.Resolve(rastrillo.Options{Mux: http.NewServeMux(), Logger: logger, NameInstance: true})
	if err != nil {
		return err
	}
	return rastrillo.Serve(opts)
}
