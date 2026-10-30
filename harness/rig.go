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

	"github.com/chromedp/cdproto/cdp"
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

	tap *protocolTap
}

// requestInfo is what a later network event cannot tell us about
// itself: a response and a load failure carry only a RequestID, so the
// method and URL come from correlating network.EventRequestWillBeSent.
type requestInfo struct{ method, url string }

type config struct {
	withoutPRFAtCreation bool
	withScrollbars       bool
	coarsePointer        bool
	hover                bool
}

// Option adjusts what New builds.
type Option func(*config)

// WithScrollbars leaves the browser's scrollbars where the platform
// draws them.
//
// chromedp's headless defaults hide them — --hide-scrollbars, copied
// from Puppeteer — and that is invisible and harmless right up until a
// drive's SUBJECT is the scrollbar. A browser that draws none takes no
// width out of the layout, so a drive measuring whether a scrollbar
// shifts the page would measure zero either way and agree with whatever
// the stylesheet claimed. ui's scrollbar-gutter drive is exactly that
// drive, and it fails rather than passes if this option stops working:
// its control page asks for the shift and requires to see it.
func WithScrollbars() Option {
	return func(c *config) { c.withScrollbars = true }
}

// WithCoarsePointer launches Chromium with a touch screen as its
// primary pointer, so (pointer: coarse) matches and (hover: hover) does
// not. It is a launch option because nothing later can do it:
// Emulation.setTouchEmulationEnabled, a mobile setDeviceMetricsOverride
// and setEmulatedMedia all leave matchMedia("(pointer: coarse)") false
// in this Chromium, while these Blink settings, read once at launch,
// make it true. A phone drive that silently ran on a fine pointer would
// pass at desktop sizes, so check the media query before measuring.
func WithCoarsePointer() Option {
	return func(c *config) { c.coarsePointer = true }
}

// WithHover launches Chromium with a mouse that can hover, so
// (hover: hover) matches. Headless Chromium's default is (hover: none)
// even on a desktop, measured here: a drive that read a rule inside
// @media (hover: hover) on the default rig would read nothing and pass
// for the wrong reason. 4 is Blink's PointerType fine and 2 its
// HoverType hover; the available* pair has to agree with the primary.
func WithHover() Option {
	return func(c *config) { c.hover = true }
}

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
	if cfg.withScrollbars {
		// After the defaults, so it wins: chromedp.Headless set this
		// true and the flag map keeps the last value written.
		allocOpts = append(allocOpts, chromedp.Flag("hide-scrollbars", false))
	}
	if cfg.hover {
		allocOpts = append(allocOpts, chromedp.Flag("blink-settings",
			"primaryPointerType=4,availablePointerTypes=4,primaryHoverType=2,availableHoverTypes=2"))
	}
	if cfg.coarsePointer {
		// 2 is Blink's PointerType coarse and 1 its HoverType none; the
		// available* pair has to agree or any-pointer disagrees with
		// pointer, which no real phone does.
		allocOpts = append(allocOpts, chromedp.Flag("blink-settings",
			"primaryPointerType=2,availablePointerTypes=2,primaryHoverType=1,availableHoverTypes=1"))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	tap := &protocolTap{}
	ctx, cancelCtx := chromedp.NewContext(allocCtx,
		chromedp.WithDebugf(tap.read))
	t.Cleanup(func() {
		// Graceful browser shutdown first, then the contexts.
		_ = chromedp.Cancel(ctx)
		cancelCtx()
		cancelAlloc()
	})

	rig := &Rig{
		Origin:   origin,
		t:        t,
		ctx:      ctx,
		requests: map[network.RequestID]requestInfo{},
		tap:      tap,
	}

	// The browser probes /favicon.ico on its own; pre-allowed so every
	// app doesn't rediscover it as a mysterious 404.
	rig.Allow(http.MethodGet, "/favicon.ico", http.StatusNotFound)
	rig.watch()

	var boot []chromedp.Action[chromedp.Void]
	if cfg.withoutPRFAtCreation {
		// Registered before Navigate ever runs, in the main world —
		// see WithoutPRFAtCreation for why this exact shape.
		boot = append(boot, chromedp.Func(addPRFShim))
	}
	boot = append(boot, chromedp.Func(addVirtualAuthenticator))
	if err := chromedp.Do(rig.ctx, boot...); err != nil {
		t.Fatalf("harness: browser boot: %v", err)
	}
	return rig
}

func addPRFShim(ctx context.Context, target *chromedp.Target) error {
	_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
		page.AddScriptToEvaluateOnNewDocumentParams{Source: prfShimJS})
	return err
}

func addVirtualAuthenticator(
	ctx context.Context, target *chromedp.Target,
) error {
	_, err := cdp.Call(ctx, target, webauthn.Enable, webauthn.EnableParams{})
	if err != nil {
		return err
	}
	_, err = cdp.Call(ctx, target, webauthn.AddVirtualAuthenticator,
		webauthn.AddVirtualAuthenticatorParams{
			Options: &webauthn.VirtualAuthenticatorOptions{
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
			},
		})
	return err
}

// Context is the rig's chromedp context — derive a deadline from it
// when a drive wants a tighter budget than the test binary's own
// timeout (ui/browser_test.go does exactly that).
func (rig *Rig) Context() context.Context { return rig.ctx }

// Run drives the browser: plain chromedp steps, no DSL — kass's
// drive is a top-to-bottom script and that is the right amount of
// framework. On failure it fails the test with whatever was on
// screen: a failure report always includes what a person would have
// seen.
func (rig *Rig) Run(steps ...chromedp.Action[chromedp.Void]) {
	rig.t.Helper()
	if err := chromedp.Do(rig.ctx, steps...); err != nil {
		rig.failDrive(err)
	}
}

// Read runs one action that returns a value, such as chromedp.Text or
// chromedp.Title, and returns the value. It fails the test the way
// Run does.
func Read[T any](rig *Rig, action chromedp.Action[T]) T {
	rig.t.Helper()
	value, err := chromedp.Run(rig.ctx, action)
	if err != nil {
		rig.failDrive(err)
	}
	return value
}

// Evaluate runs a JavaScript expression in the page and returns its
// result decoded as T. It fails the test the way Run does.
func Evaluate[T any](
	rig *Rig, expression string, options ...chromedp.EvaluateOption,
) T {
	rig.t.Helper()
	return Read(rig, chromedp.Evaluate[T](expression, options...))
}

// Into is a step that runs a value action and keeps what it returns in
// out, so a script can read the page between its steps and still stop
// at, and report, the first one that fails:
//
//	var count int
//	rig.Run(
//		chromedp.Click(chromedp.CSS("#more")),
//		harness.Into(&count, chromedp.Evaluate[int](`rows.length`)),
//		chromedp.Click(chromedp.CSS("#less")),
//	)
func Into[T any](
	out *T, action chromedp.Action[T],
) chromedp.Action[chromedp.Void] {
	keep := func(ctx context.Context, target *chromedp.Target) error {
		value, err := action(ctx, target)
		if err != nil {
			return err
		}
		*out = value
		return nil
	}
	return chromedp.Func(keep)
}

func (rig *Rig) failDrive(err error) {
	rig.t.Helper()
	rig.t.Fatalf("harness: drive failed: %v\non screen:\n%s",
		err, rig.onScreen("body"))
}

// onScreen reads sel's innerText (falling back to the whole body)
// best-effort — it feeds failure reports, so its own errors are
// swallowed rather than masking the real one.
func (rig *Rig) onScreen(sel string) string {
	expr := fmt.Sprintf(`((document.querySelector(%q) ?? document.body)?.innerText ?? "")`, sel)
	text, _ := chromedp.Run(rig.ctx, chromedp.Evaluate[string](expr))
	return text
}
