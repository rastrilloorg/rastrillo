//go:build browser

package harness

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// allowance is one expected probe: a request the app makes on purpose
// that would otherwise read as a failure. Kass's precedent, and its
// only one: the signed-out boot asks /api/me, is told 401, and that is
// how the app finds out to show the sign-in screen.
type allowance struct {
	method string
	path   string
	status int
}

// Rig is one app under one browser: a real server on a localhost
// origin, a real Chromium, a CDP virtual authenticator, and the
// watchers that make a silent failure impossible.
type Rig struct {
	// Origin is the app's trustworthy origin, "http://localhost:PORT".
	// Always navigate this, never a 127.0.0.1 URL: an IP is not a
	// WebAuthn RP ID. Passkey apps set RPID "localhost" in test wiring.
	Origin string

	t   *testing.T
	ctx context.Context

	mu          sync.Mutex
	problems    []string
	allows      []allowance
	allowedText []string
	requests    map[network.RequestID]requestInfo
}

// requestInfo is what a later network event cannot tell us about
// itself: a response and a load failure carry only a RequestID, so the
// method and URL come from correlating network.EventRequestWillBeSent.
type requestInfo struct{ method, url string }

type config struct {
	withoutPRFAtCreation bool
}

// Option adjusts what New builds.
type Option func(*config)

// New boots the rig. Order is the point: a passkey app needs its
// origin before building its handler (csrf, webauthn config), but the
// port doesn't exist until a listener does — so New binds the
// listener first, computes origin, calls build(origin), serves on that
// listener, and only then launches Chromium with the virtual
// authenticator attached (kass's proven options, verbatim).
//
// Cleanups are registered so t.Cleanup's LIFO tears the browser down
// before the server it is talking to.
func New(t *testing.T, build func(origin string) http.Handler, opts ...Option) *Rig {
	t.Helper()
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	chrome := ChromePath(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("harness: listen: %v", err)
	}
	origin := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)

	srv := httptest.NewUnstartedServer(build(origin))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Flag("headless", true),
		chromedp.NoSandbox,
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	t.Cleanup(func() {
		// Graceful browser shutdown first, then the contexts.
		_ = chromedp.Cancel(ctx)
		cancelCtx()
		cancelAlloc()
	})

	r := &Rig{
		Origin:   origin,
		t:        t,
		ctx:      ctx,
		requests: map[network.RequestID]requestInfo{},
	}

	// The browser probes /favicon.ico on its own; pre-allowed so every
	// app doesn't rediscover it as a mysterious 404.
	r.Allow(http.MethodGet, "/favicon.ico", http.StatusNotFound)
	r.watch()

	var boot []chromedp.Action
	if cfg.withoutPRFAtCreation {
		// Registered before Navigate ever runs, in the main world —
		// see WithoutPRFAtCreation for why this exact shape.
		boot = append(boot, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(prfShimJS).Do(ctx)
			return err
		}))
	}
	boot = append(boot, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := webauthn.Enable().Do(ctx); err != nil {
			return err
		}
		_, err := webauthn.AddVirtualAuthenticator(&webauthn.VirtualAuthenticatorOptions{
			Protocol:            webauthn.AuthenticatorProtocolCtap2,
			Ctap2version:        webauthn.Ctap2versionCtap21,
			Transport:           webauthn.AuthenticatorTransportInternal,
			HasResidentKey:      true,
			HasUserVerification: true,
			HasPrf:              true,
			IsUserVerified:      true,
			// The CDP default is true — but the Go zero value would
			// silently send false, and every ceremony would hang
			// waiting for a presence that never comes. Explicit,
			// always: the one trap in an otherwise clean cdproto
			// mapping.
			AutomaticPresenceSimulation: true,
		}).Do(ctx)
		return err
	}))
	if err := chromedp.Run(r.ctx, boot...); err != nil {
		t.Fatalf("harness: browser boot: %v", err)
	}
	return r
}

// Context is the rig's chromedp context — derive a deadline from it
// when a drive wants a tighter budget than the test binary's own
// timeout (ui/browser_test.go does exactly that).
func (r *Rig) Context() context.Context { return r.ctx }

// Run drives the browser: plain chromedp actions, no DSL — kass's
// drive is a top-to-bottom script and that is the right amount of
// framework. On failure it fails the test with whatever was on
// screen: a failure report always includes what a person would have
// seen.
func (r *Rig) Run(actions ...chromedp.Action) {
	r.t.Helper()
	if err := chromedp.Run(r.ctx, actions...); err != nil {
		r.t.Fatalf("harness: drive failed: %v\non screen:\n%s", err, r.onScreen("body"))
	}
}

// onScreen reads sel's innerText (falling back to the whole body)
// best-effort — it feeds failure reports, so its own errors are
// swallowed rather than masking the real one.
func (r *Rig) onScreen(sel string) string {
	var text string
	expr := fmt.Sprintf(`((document.querySelector(%q) ?? document.body)?.innerText ?? "")`, sel)
	_ = chromedp.Run(r.ctx, chromedp.Evaluate(expr, &text))
	return text
}
