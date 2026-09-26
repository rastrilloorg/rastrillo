// Package edge is the deployment host's front door: TLS termination
// (autocert), a host→backend reverse proxy, and a live-reloading route table.
//
// Ported from Eleven's router.go (elevenmessenger/messenger: router.go;
// design in its docs/instance-router.md) — the identity-free shape: the
// atomically-swapped host→upstream map, the retrying unix dialer that masks
// instance restarts, autocert with the route table as HostPolicy, the
// registry fingerprint watcher, SIGHUP reload, the unknown-host fallback, and
// the hibernation-activator hook — with titogo internal/carlos/router.go's
// additions carried in: -dev (plain-HTTP mode, so multi-instance routing runs
// locally with no DNS/TLS) and the first-observed-fingerprint reload. Dropped
// on port: titogo's Stripe relay, auth/identity, root-domain platformIndex;
// Eleven's mesh (its wildcard DNS-01 certs were later ported back —
// wildcard.go). New here: TCP upstreams (Route.Addr) so whole-app tenants sit
// behind the edge without unix-socket support, and the static serving class
// stub (static.go).
package edge

import (
	"github.com/carlosframework/platform/internal/cliflag"

	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/carlosframework/platform/internal/apperr"
	"github.com/carlosframework/platform/internal/edgesfile"
	"github.com/carlosframework/platform/internal/gate"
	"github.com/carlosframework/platform/internal/provision"
	"github.com/carlosframework/platform/internal/registry"
	"github.com/carlosframework/platform/internal/release"
	"github.com/carlosframework/platform/internal/unixdial"
)

// Default cache sizing for EnableCache: 4096 entries is comfortably above a
// single box's route × Vary-variant count, and 1 MiB caps a single cached
// body well above a typical HTML page while still bounding worst-case memory
// (4096 * 1MiB = 4GiB ceiling, never actually reached in practice since most
// bodies are far smaller).
const (
	DefaultCacheMaxEntries   = 4096
	DefaultCacheMaxBodyBytes = 1 << 20 // 1 MiB
)

// DefaultCacheMaxTTL is the ceiling every cached entry's TTL is clamped to,
// however long the app's own s-maxage or max-age asks for. It is what makes spec decision
// 4 true — "a lost purge self-heals within a bounded window": without a
// ceiling, an app setting max-age=31536000 turns one dropped purge call into a
// permanently wrong page. Five minutes is short enough that a missed purge is
// a blip and long enough that a crawler sweep still rides the cache.
const DefaultCacheMaxTTL = 5 * time.Minute

// Activator is the hibernation seam (implemented by the activator package):
// given a hibernate-flagged instance route, it wakes (or joins an in-progress
// wake of) the backing process before letting the request through, and
// reports whether it wrote the response. nil = no hibernation, pure proxy.
type Activator interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, rt registry.Route, proxy http.Handler) bool
	RetireExcept(keep map[string]bool)
}

// Edge terminates TLS (autocert) and reverse-proxies each configured host to
// its backend. The host→backend map is swapped atomically, so reloads and
// zero-downtime deploys never drop a live connection.
type Edge struct {
	reg       *registry.Registry
	upstreams atomic.Pointer[map[string]upstream]
	// act, when non-nil, is the hibernation activator: hibernate-flagged
	// instance routes stop being always-on backends and become on-demand
	// processes woken per request. Nil keeps the proxy-only behavior; with it
	// set, unflagged routes are still proxied straight (the flag is per host).
	act Activator
	// gate is the password curtain (internal/gate) for routes whose row
	// carries a gate hash. Always constructed: the check is one string
	// compare on up.route.Gate for every ungated host, and the limiter it
	// carries is shared across the box's gated hosts.
	gate *gate.Gate
	// fallbackURL, when set, is where requests for unknown hosts are redirected
	// (e.g. the apex marketing site) instead of a bare 404 — a wildcard DNS
	// record then makes typo'd or retired subdomains land somewhere useful. Note
	// the limit: over HTTPS this only helps hosts that still HAVE a cert (e.g. a
	// removed instance); a never-served host fails the TLS handshake first,
	// because HostPolicy refuses to mint certs for unknown hosts (anything else
	// would let strangers burn the ACME rate limit). The :80 path always works.
	//
	// Hosts under a platform domain are the exception (pending.go, #174): they
	// get the 503 pending page, which reaches this URL only through its
	// script, after the retry window. curl, crawlers and no-JS clients see the
	// 503 and never the redirect.
	fallbackURL string
	// platformDomains and pendingBody are the "not here YET" answer
	// (pending.go, #174): an unknown host strictly under one of these gets a
	// 503 retry page instead of unknownHost's 404 or fallback redirect. Both
	// are set together by setPlatformDomains, at start; nil domains (every
	// test that builds an Edge with New, and a deployment with neither env
	// set) keep the old answer for every host.
	platformDomains []string
	pendingBody     []byte
	// steeringHost is this deployment's DNS-steering hostname
	// (edge.<apps-domain>), or "" when the deployment does not steer — which
	// is every deployment today. Set once at construction from the
	// environment; see steeringprobe.go for why it is a serving concern at
	// all.
	steeringHost string
	// Static serving config (EnableStatic): the deployment store the site
	// tars are fetched from, the release pubkey channel pointers must verify
	// against, and the on-box cache the trees are extracted into. staticStore
	// nil = static serving not configured; static routes answer 404 (the
	// route can still exist and mint its cert before the config lands).
	staticStore    release.Store
	staticPub      ed25519.PublicKey
	staticCacheDir string
	staticRecheck  time.Duration
	// usageMeter, when non-nil, is where ServeHTTP records the request/byte
	// count of every route it serves (see EnableUsage). Atomic because it is
	// armed by the agent role after Edge construction, mirroring how
	// tunnelLoop attaches via the ready hook — serving may already be
	// underway.
	usageMeter atomic.Pointer[UsageMeter]
	// errSink, when set, receives edge-observed errors (an upstream 5xx)
	// — spec 2026-09-07 §3.2. Nil disables emission; nothing else changes.
	errSink atomic.Pointer[errorSinkRef]
	// analyticsMeter, when set, receives one observation per completed
	// response for an app whose analytics are switched on (analytics.go,
	// spec 2026-09-07 §3.3), and analyticsOn is that switch map keyed
	// sqid+"/"+app. Both are atomic for the same reason usageMeter is: the
	// agent role arms the meter after construction, and the telemetry pass
	// replaces the switch map while serving is underway. Nil meter or nil
	// map = the edge observes nothing, which is every `carlos edge` box and
	// every app nobody has switched on.
	analyticsMeter atomic.Pointer[analyticsMeterRef]
	analyticsOn    atomic.Pointer[map[string]bool]
	// tunnels, when non-nil, is the reverse-channel hub (tunnel.go): remote
	// hosts register on its reserved hostname and routes with Tunnel set are
	// proxied over the channel they name. nil = tunnel serving not configured
	// on this box: a route's Tunnel binding is then ignored and it falls back
	// to its socket/addr, or goes unserved if it has neither — the same
	// nil-means-disabled convention as act/cache/staticStore.
	tunnels *TunnelHub
	// door, when non-nil, is the fleet store door (storedoor.go): the five
	// release.Store operations served over HTTPS under /store/ on the SAME
	// reserved hostname the hub answers on, so a fleet box needs no AWS
	// credentials on the deployment bucket. It exists only when BOTH halves
	// it needs are configured — the hub (its only credential source) and the
	// deployment store (EnableStatic's handle, reused, never a second
	// client) — so armStoreDoor is called from both EnableXxx, whichever
	// lands second. nil = /store/ falls through to the hub's own 404, the
	// same nil-means-disabled convention as act/cache/staticStore.
	door *storeDoor
	// fleetAccounts is the reload-time fleet→account-sqid snapshot the door's
	// ACL is derived from, refreshed on the SAME pass that feeds the hub's
	// dynamic auth table (Reload, below) so a box's credentials and its
	// tenancy can never come from two different reads of the registry.
	// Atomic: written by Reload while requests are being served.
	fleetAccounts atomic.Pointer[map[string]string]
	// cache, when non-nil, answers cacheable GET/HEAD requests without ever
	// invoking the hibernation activator or the backend — the mitigation
	// this whole feature exists for. nil disables caching entirely.
	cache *Cache
	// logN counts occurrences per throttle key so logThrottled can rate-limit
	// a repeating complaint. Two callers today: Carlos-Purge complaints, which
	// are driven by app responses on the request hot path and would otherwise
	// be one journal line per request, and Reload's unparseable-health line,
	// which repeats on every reload for as long as the bad row exists. Lazily
	// created under logMu.
	logMu sync.Mutex
	logN  map[string]int
	// reloadHook, when set (OnReload), runs after every successful route
	// swap — the wildcard manager recomputes its parent set from it. Atomic
	// because registration happens in RunWithActivator after Watch may
	// already be reloading.
	reloadHook atomic.Pointer[func()]
	// probers owns the health-probe goroutines for multi-upstream addr routes
	// (prober.go) and the live set multiProxy picks from. Created in New,
	// BEFORE the startup Reload — Reload calls Sync on it unconditionally and
	// a nil *proberSet panics by design, so a construction path that skipped
	// this would be a wiring bug that fails loudly rather than a fleet that
	// quietly 503s. Never stopped in production (the edge lives as long as the
	// process); tests Stop it so probe goroutines don't outlive them.
	probers *proberSet
	// gzipOn is the CARLOS_EDGE_GZIP kill switch, read once in New. When
	// false, ServeHTTP never constructs a gzipWriter and the writer stack is
	// byte-for-byte what it was before compression existed (compress.go).
	gzipOn bool
	// issuer, when set, gates certificate ISSUANCE on this box holding the
	// deployment's housekeeper lease (certsync.go). It arrives through
	// WithIssuerGate because internal/host imports this package and never the
	// reverse. nil = ungated, which is `carlos edge` without the agent and
	// every single-box deployment's behaviour to date: mint as always.
	// Serving is never gated by it.
	issuer IssuerGate
	// contends reports whether this box takes part in the issuer election
	// at all (WithIssuerContender) — the cert backfill's eligibility, a
	// coarser question than the gate above answers. nil = does not contend.
	contends func() bool
	// box is this box's own label — the SAME derivation its edges file is
	// published under (internal/host's metricsBoxLabel), handed down through
	// WithBoxLabel for the reason the issuer gate is: that package imports
	// this one and never the reverse. It is what the home map excludes its own
	// hosts by; "" (bare `carlos edge`) leaves the map offering nothing.
	box string
	// homes, when non-nil, is the mirror of control/edges/ plus the mesh keys
	// (homemap.go): who homes a host this box does not serve, and what proves
	// an inbound hop. Atomic because it is armed in RunWithActivatorReady after
	// construction, and read on the request path while that is happening.
	homes atomic.Pointer[homeMap]
	// backhaulTransports caches the outbound TLS transport of each backhaul
	// target, keyed by the home box's EIP — see backhaulTransport. Bounded by
	// the number of BOXES in the deployment, never by hostnames: a catch-all
	// row in another box's edges file makes every subdomain under it a
	// backhaul target, and keying per host would let a stranger's stream of
	// invented hostnames grow this map without limit.
	backhaulTransports sync.Map
	// backhaulInflight counts the hops currently in flight toward each home
	// box — the containment half of the egress ruling (backhaulAcquire).
	// Keyed by BOX for the same reason backhaulTransports is keyed by EIP: a
	// catch-all row in another box's edges file makes every invented
	// subdomain a backhaul target, so a per-host key would let a stranger
	// grow this map without limit. Bounded by the deployment's box count.
	backhaulInflight sync.Map // box → *atomic.Int64
}

// RunOption configures the Edge that Run/RunWithActivator* builds, before it
// serves anything. Variadic rather than another positional parameter: the
// composing caller (internal/host's agent role) hands host-owned state DOWN
// into this package, and every existing call site keeps compiling unchanged.
type RunOption func(*Edge)

// WithIssuerGate hands the edge the deployment's certificate-issuer gate —
// the housekeeper's, in the agent role. Without it the edge issues exactly
// as it always has. See IssuerGate (certsync.go).
func WithIssuerGate(g IssuerGate) RunOption { return func(e *Edge) { e.issuer = g } }

// WithIssuerContender tells the edge whether this box CONTENDS in the
// deployment's issuer election at all — a different question from holding
// it right now (the gate above answers that, moment to moment). The cert
// backfill (certsync.go, #318) is allowed exactly on contenders: an
// agent-run box whose housekeeper mode is "never" (a fleet box grandfathered
// with the bucket env) carries a gate func that can never be held, and its
// disk holds a customer's certs — not this deployment's to replicate. nil —
// no agent handed one down — reads as "does not contend".
func WithIssuerContender(c func() bool) RunOption { return func(e *Edge) { e.contends = c } }

// WithBoxLabel hands the edge this box's own label, the one its edges file is
// published under (internal/host's metricsBoxLabel — the same call the edges
// publisher makes, so the two can never disagree about which file is ours).
// Without it the home map excludes nothing and therefore offers nothing: an
// edge that cannot recognise its own hosts must not hand out dial targets.
// See homemap.go.
func WithBoxLabel(label string) RunOption { return func(e *Edge) { e.box = label } }

// upstream is one served host: the handler for its backend, plus — when the
// activator manages it — the route itself, so a request can trigger the wake.
type upstream struct {
	proxy http.Handler
	route registry.Route
	// hibernating marks an activator-managed instance: the edge itself knows
	// whether its process is live (it's the edge's child), so "dead backend"
	// detection is the activator's state table, not a failed dial.
	hibernating bool
	// viaTunnel marks a host THIS box proxies over a reverse channel (a
	// Tunnel-backed or fleet-placed route with a hub to carry it). It is the
	// build-time decision, not the row's columns: the fleet box that serves
	// the app carries the same fleet placement on its row and serves it from
	// the socket, and must file what it sees (emitEdgeError).
	viaTunnel bool
	// alias marks an entry mapped in from route_aliases: it shares the
	// primary's proxy/route/hibernating verbatim (so hibernation wakes the
	// primary's actEntry and #152's per-primary prober state applies), and
	// the flag exists so wildcard parent derivation (currentHosts) never
	// treats a customer hostname as a platform host.
	alias bool
	// paths, when non-nil, splits this hostname's traffic by leading URL
	// segment: a bare first segment ("paulca") mapped to the HOSTNAME of the
	// route that serves it, resolved against the same live map at request
	// time. Nil for every host that does not use prefix routing, which is
	// every host on the platform until a claim publishes a map — the nil
	// check in lookupUpstreamPath is the entire cost this feature imposes on
	// everyone else.
	//
	// Hostnames rather than resolved upstreams, deliberately: an upstream
	// captured here would be a stale copy of the target's entry the moment
	// the target's route changed within the same map build.
	paths map[string]string
}

// New builds an Edge over reg and performs the startup reload. act may be nil
// (no hibernation).
func New(reg *registry.Registry, act Activator) (*Edge, error) {
	e := &Edge{reg: reg, act: act, gate: gate.New(), probers: newProberSet(), gzipOn: gzipEnabled(), steeringHost: steeringProbeHostFromEnv()}
	if err := e.Reload(); err != nil {
		return nil, err
	}
	return e, nil
}

// EnableStatic turns on the static serving class: routes with Kind=="static"
// resolve their app's channel pointer against store (verified with pub) and
// serve the extracted tree from cacheDir. Call Reload afterwards so existing
// static routes pick up real handlers; without EnableStatic they 404.
//
// Kind=="status" routes (statusHandlerFor, status.go) share this same store
// handle rather than getting their own EnableXxx entry point: it's the same
// deployment bucket, just a fixed unsigned three-key menu (status/index.html,
// status/fleet.json, status/status.css — see internal/status's PublishFleet)
// instead of a signed, versioned app tree. pub and cacheDir are meaningless
// to the status class (no signature to verify, nothing to extract to disk),
// so it only ever reads e.staticStore off this call.
func (e *Edge) EnableStatic(store release.Store, pub ed25519.PublicKey, cacheDir string) {
	e.staticStore = store
	e.staticPub = pub
	e.staticCacheDir = cacheDir
	e.staticRecheck = staticRecheck
	e.armStoreDoor()
}

// EnableCache wires a response cache into the edge. Called once at startup
// (RunWithActivator or Run); nil is a valid no-op, matching the package's
// existing nil-means-disabled convention for Activator.
func (e *Edge) EnableCache(cache *Cache) {
	e.cache = cache
}

// EnableTunnels wires the reverse-channel hub in: the edge answers hub.Host()
// itself (registration) and routes with Tunnel set proxy over their named
// channel. Call before Reload, same contract as EnableStatic — without a
// Reload afterwards, tunnel routes stay skipped from the last table build.
func (e *Edge) EnableTunnels(hub *TunnelHub) {
	e.tunnels = hub
	e.armStoreDoor()
}

// EnableUsage arms per-route request/byte metering. Called by the agent
// role after construction (mirroring how tunnelLoop attaches via the ready
// hook); the bare `carlos edge` role never calls it, so pure-proxy edges
// meter nothing. Safe to call after serving has started (atomic).
func (e *Edge) EnableUsage(m *UsageMeter) { e.usageMeter.Store(m) }

// ErrorSink is where the edge files an error it observed on the serving
// path. apperr.Sink satisfies it; the interface lives here so this package
// depends on the contract, not the buffer.
type ErrorSink interface {
	Add(acct, app string, r apperr.Record)
}

type errorSinkRef struct{ ErrorSink }

// EnableErrors wires the error sink in. Called once at startup by the
// agent; nil is a valid no-op.
func (e *Edge) EnableErrors(s ErrorSink) {
	if s == nil {
		e.errSink.Store(nil)
		return
	}
	e.errSink.Store(&errorSinkRef{s})
}

// emitEdgeError files one edge-observed 5xx for rt. The path is sanitised
// (query and token segments off) before it goes on the record. A 502, 503
// or 504 is filed under Availability (apperr/availability.go): named and
// so fingerprinted by status and cause, never by path, with the path kept
// only as the occurrence's sample. cause is what a proxy ErrorHandler
// noted; none means the upstream wrote the status itself. A failure whose
// client had already gone is not filed at all.
//
// When this box proxied the request over a reverse channel (viaTunnel),
// the upstream is not the app: it is another box's edge (the agent's, the
// only tunnel client), which files what it observes serving the app —
// including deciding NOT to file a wake 503 its activator answered
// (MarkAnswered). So this box files only what it noted itself, the hop
// failing; a status that merely passed through would be the same failure
// filed twice, and a 502/503 labelled "the app answered it itself" when the
// app never saw the request. The box at the far end serves the same route
// from its socket (viaTunnel false) and files everything as usual.
func (e *Edge) emitEdgeError(rt registry.Route, viaTunnel bool, r *http.Request, status int, cause string) {
	ref := e.errSink.Load()
	if ref == nil || cause == causeClientGone || (viaTunnel && cause == "") {
		return
	}
	path := apperr.SanitizePath(r.URL.Path)
	rec := apperr.Record{
		Host:    rt.Host,
		Source:  apperr.SourceEdge,
		Kind:    apperr.KindHTTP,
		Name:    fmt.Sprintf("%d %s %s", status, r.Method, path),
		Path:    path,
		Status:  status,
		Release: rt.Version,
	}
	if apperr.UnavailableStatus(status) {
		if cause == "" {
			cause = apperr.CauseUpstream
		}
		rec.Kind, rec.Cause, rec.Name = apperr.KindUnavailable, cause, apperr.AvailabilityName(status, cause)
	}
	ref.Add(release.RouteAccount(rt), rt.App, rec)
}

// proxyCause classifies why a proxy could not get an answer for r. The
// client's own context is checked before the error itself: a client that
// left surfaces as a canceled dial or read whatever the instance was doing.
// Leaving soon after asking is the client's business and is not filed;
// leaving after clientGoneGrace means the instance kept it waiting, and is.
func proxyCause(r *http.Request, err error) string {
	if r.Context().Err() != nil {
		if waited(r) >= clientGoneGrace {
			return apperr.CauseAbandoned
		}
		return causeClientGone
	}
	var ne net.Error
	switch {
	case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENOENT):
		return apperr.CauseRefused
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return apperr.CauseTimeout
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return apperr.CauseReset
	}
	return apperr.CauseProxy
}

// tunnelCause is proxyCause for a proxy whose far side is a fleet box's
// tunnel: every failure but the client's own leaving is the hop's, not
// the instance's.
func tunnelCause(r *http.Request, err error) string {
	if c := proxyCause(r, err); c == causeClientGone || c == apperr.CauseAbandoned {
		return c
	}
	return apperr.CauseTunnel
}

// defaultProxyError is httputil.ReverseProxy's own default ErrorHandler —
// the same log line, the same bare 502 — plus the cause note, for the
// proxies that had no handler of their own.
func defaultProxyError(classify func(*http.Request, error) string) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		noteCause(r, classify(r, err))
		log.Printf("http: proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
	}
}

// armStoreDoor builds (or clears) the fleet store door from whatever is
// configured now. Called from both EnableStatic and EnableTunnels because the
// door needs one thing from each and the two are wired in either order at
// startup (enableStaticFromEnv runs first today, but a caller composing an
// Edge by hand — every test does — must not have to know that). A deployment
// with no release pubkey configured therefore has no store handle and so no
// door: /store/ then reaches the hub, which 404s it, byte-identically.
func (e *Edge) armStoreDoor() {
	if e.tunnels == nil || e.staticStore == nil {
		e.door = nil
		return
	}
	e.door = &storeDoor{hub: e.tunnels, store: e.staticStore, fleetAccount: e.fleetAccount}
}

// fleetAccount resolves an authenticated fleet name to its owning account's
// sqid from the last reload's snapshot. A fleet the snapshot does not carry
// (created since the last reload, or deleted) has no tenancy, so the door
// answers it nothing — the same convergence bound as its channel credentials,
// which come from the same pass.
func (e *Edge) fleetAccount(fleet string) (string, bool) {
	m := e.fleetAccounts.Load()
	if m == nil {
		return "", false
	}
	sqid, ok := (*m)[fleet]
	if !ok || sqid == "" {
		return "", false
	}
	return sqid, true
}

// SetStaticRecheck tunes how long a static route trusts a resolved channel
// pointer before re-reading it (default 60s; 0 = every request). Call after
// EnableStatic and before Reload; handlers built on the next reload use it.
func (e *Edge) SetStaticRecheck(d time.Duration) { e.staticRecheck = d }

// staticHandlerFor builds the handler for one static route: the real serving
// class when EnableStatic has configured it, else the pre-config 404 (the
// route exists — and its cert can be minted — before the bytes can flow).
func (e *Edge) staticHandlerFor(rt registry.Route) http.Handler {
	if e.staticStore == nil || len(e.staticPub) == 0 || rt.App == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "static serving not configured", http.StatusNotFound)
		})
	}
	h := newStaticHandler(rt, e.staticStore, e.staticPub, e.staticCacheDir)
	if ss, ok := h.(*staticSite); ok {
		ss.recheck = e.staticRecheck
	}
	return h
}

// statusHandlerFor builds the handler for one status route: the real
// bucket-backed handler (status.go) once EnableStatic has wired a store, else
// the pre-config 404 — same cert-first shape as staticHandlerFor, so a
// status route's cert can be minted before the store config lands.
func (e *Edge) statusHandlerFor(rt registry.Route) http.Handler {
	if e.staticStore == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "status serving not configured", http.StatusNotFound)
		})
	}
	return newStatusHandler(e.staticStore)
}

// Reload rebuilds the host→backend map from the registry and swaps it in
// atomically. Safe to call while serving.
func (e *Edge) Reload() error {
	routes, err := e.reg.List()
	if err != nil {
		return err
	}
	m := make(map[string]upstream, len(routes))
	// The hosts the activator should manage after this reload — feeds both the
	// per-upstream gate and the retire pass below.
	managed := make(map[string]bool)
	// The probing jobs this reload's route set implies — one entry per addr
	// route that carries a health config, fed to proberSet.Sync below. Hosts
	// absent from it have their probers stopped, exactly as RetireExcept
	// retires activator children for hosts absent from `managed`.
	probeSpecs := make(map[string]probeSpec)
	for _, t := range routes {
		// A sidecar is a member that serves nothing: it binds no socket, and
		// nothing — inside the box or outside it — may ever dial it. Refused
		// here on KIND, as the first act of the loop and ahead of every other
		// check, rather than left to fall through the dispatch switch for want
		// of a Socket/Addr/Tunnel.
		//
		// That omission is not a guarantee. The fleet arm below binds on
		// placement alone (`isFleetPlaced && e.tunnels != nil`, no kind, no
		// backend column), and a provisioned Eleven sidecar IS fleet-placed
		// (region "fleet/<name>") — so the row this feature creates reaches a
		// working internet-facing proxy by shape. This refusal is the
		// structural guarantee that survives a future arm, column or
		// placement finding the row: no map entry, so no proxy; and no map
		// entry means Allowed says no, so no ACME issuance either.
		if t.IsSidecar() {
			continue
		}
		// Only serve ready routes: a 'creating'/'failed' entry has no working
		// backend, so proxying it would just stall → 502 — AND, since Allowed
		// is the autocert HostPolicy, serving it would burn an ACME issuance
		// on a host that can't answer yet.
		if t.Status != "ready" {
			continue
		}
		var proxy http.Handler
		// viaTunnel: this host's bytes leave the box over a reverse channel,
		// which is what disqualifies it from hibernation below.
		viaTunnel := false
		fleet, isFleetPlaced := registry.FleetPlacement(t.Region)
		switch {
		// Both reverse proxies get the Carlos-Purge response hook
		// (purge_header.go): the app declares its own cache invalidations on
		// the response it is already returning, and the edge applies them and
		// strips the header before any client sees it. The hook closes over e
		// rather than e.cache, because EnableCache runs AFTER New's startup
		// Reload — capturing e.cache here would capture nil and the channel
		// would be permanently inert.
		//
		// The whole ROUTE goes in, not just its host: Carlos-Purge-Host lets a
		// response redirect its declaration at a sibling route, and the edge
		// validates that against this route's App and Account. Those two are
		// captured (the tenancy the edge resolved this request to); the sibling
		// lookup is not — it re-reads e.current() per response, so a route
		// added after this proxy was built still validates.
		//
		// Static and status routes get no hook: their responses are the
		// edge's own, built from bucket bytes by static.go/status.go, with no
		// upstream in a position to declare anything.
		//
		// The tunnel case comes FIRST, ahead of socket/addr, because a route
		// can carry both: `carlos route -tunnel` is a narrow UPDATE that does
		// not clear socket/addr, so an operator moving a local app onto a
		// remote host leaves the old column set — and the binding they just
		// typed is the one they meant. It is guarded on e.tunnels rather than
		// skipping unconditionally so that an edge with no hub configured
		// keeps serving such a route from its old socket/addr (degrade to the
		// previous backend, never take the host off the air); a tunnel-only
		// route on such an edge matches nothing and falls to
		// `default: continue`, un-served and un-certed, until the hub lands.
		//
		// That degrade path only covers a HUB-LESS edge (e.tunnels == nil).
		// A hub-configured edge whose client set doesn't (yet) include this
		// route's channel name still takes this branch — e.tunnels != nil is
		// all this case checks — and the built proxy 502s every request, even
		// though t.Addr may hold a perfectly working backend. Deliberate: once
		// a hub exists, a `-tunnel` binding is the newest operator intent and
		// wins outright rather than silently falling back to a stale Addr.
		case t.Tunnel != "" && e.tunnels != nil:
			p := tunnelProxy(t.Tunnel, e.tunnels)
			p.ModifyResponse = e.purgeModifier(t)
			proxy = p
			viaTunnel = true
		// Fleet placement (Region == "fleet/<name>", registry.FleetPlacement) is
		// a customer-fleet route: its bytes leave the box over the SAME reverse
		// channel a `-tunnel` binding uses, just resolved dynamically per
		// request rather than to one fixed name — see fleetProxy. Guarded on
		// e.tunnels for the identical reason the Tunnel case above is: a
		// fleet-placed route on a hub-less edge degrades to its old Socket/Addr
		// (or goes unserved) rather than the box silently going dark for a
		// config gap that isn't its fault. viaTunnel is shared with the Tunnel
		// case on purpose — a fleet-served instance is exactly as un-wakeable
		// locally as a `-tunnel`-bound one; see the hibernation comment below.
		case isFleetPlaced && e.tunnels != nil:
			p := fleetProxy(fleet, t.FleetBox, e.tunnels)
			p.ModifyResponse = e.purgeModifier(t)
			proxy = p
			viaTunnel = true
		case t.Socket != "":
			p := unixProxy(t.Socket)
			p.ModifyResponse = e.purgeModifier(t)
			proxy = p
		// TCP upstreams. The gate is AddrList (registry), not t.Addr, so a
		// multi-upstream route — which writes the Addrs column and leaves the
		// legacy Addr empty — is served at all; AddrList folds the legacy
		// single Addr into a one-element list, so nothing about the old shape
		// moves. Position in the dispatch order is unchanged (tunnel > socket >
		// addrs > static > status).
		//
		// Health is what selects the proxy, not the addr COUNT:
		//
		//   - no health config → tcpProxy, byte-identical to the pre-#152
		//     path. An operator who never asked for probing gets exactly the
		//     behavior they had, including the stdlib 502 on a dead backend.
		//   - health config → multiProxy, whether the route names one upstream
		//     or five. A single PROBED upstream is on the dashboard and answers
		//     503 + Retry-After when it is down, which is the point of asking
		//     for a probe on a one-box route.
		//
		// A route with several addrs but no health config takes the unprobed
		// path on addrs[0]: without a probe there is no live set to balance
		// over, and inventing one would mean load-balancing onto upstreams
		// nothing is checking. It is a config gap, not a serving failure —
		// mint always writes both columns together (provision), so this is
		// only reachable via a hand-written row.
		case len(t.AddrList()) > 0:
			addrs := t.AddrList()
			var p *httputil.ReverseProxy
			switch {
			case t.Health == "":
				p = tcpProxy(addrs[0])
			default:
				var h provision.Health
				if err := json.Unmarshal([]byte(t.Health), &h); err != nil {
					// Never a dead route: a health column we cannot parse costs
					// the route its probing, not its service. Logged rather
					// than swallowed — it is a config error someone has to fix,
					// and a route silently losing its HA is worse than a log
					// line — but throttled, because a bad row is a STANDING
					// condition that would otherwise print on every reload for
					// as long as it exists.
					e.logThrottled("health-parse "+t.Host, fmt.Sprintf(
						"route %s: unparseable health config (%v) — serving %s unprobed", t.Host, err, addrs[0]))
					p = tcpProxy(addrs[0])
					break
				}
				// Raw parse, deliberately un-normalized: Sync runs every spec
				// through Health.WithDefaults itself, and normalizing here too
				// would just be a second place to keep in step with it.
				probeSpecs[t.Host] = probeSpec{Host: t.Host, Addrs: addrs, Health: h}
				p = multiProxy(t.Host, e.probers)
			}
			p.ModifyResponse = e.purgeModifier(t)
			proxy = p
		case t.Kind == "static":
			proxy = e.staticHandlerFor(t)
		case t.Kind == "status":
			proxy = e.statusHandlerFor(t)
		default:
			continue // no backend to reach
		}
		// The deploy watch's proof and the human "verify with the binary you
		// built" tell (carlos-deploy spec §7): the version this route is
		// serving, from the row the adopt pass stamps AFTER the restart.
		// Server:-header-grade disclosure, ruled public 2026-08-08.
		//
		// Freshness contract: on every UNCACHED response, this is live —
		// Reload rebuilds these handlers on every registry change, and
		// Fingerprint is content-accurate since #15 closed, so an uncached
		// header can't go stale. A CACHE HIT bypasses this handler entirely
		// (ServeHTTP's GetFor/writeCached path, ahead of the wrapped proxy)
		// and replays whatever header maybeStore froze at store time, so it
		// can lag a promote by up to the cache's TTL (clamped to
		// DefaultCacheMaxTTL, 5 minutes) or until a Carlos-Purge evicts the
		// entry — Reload never purges e.cache. That lag is one-directional
		// and fails safe for the watch: a stale entry only ever carries the
		// OLD version, so it can read as "not yet" but never false-positive
		// as the new one. A watcher that needs the fresh answer within the
		// TTL window must bust the cache (e.g. a unique query string).

		// Static and status routes are excluded structurally, not because a
		// Version is expected on them today (nothing adopts either, so the
		// column is empty): the static handler stamps its OWN per-request
		// version and must be the only voice on that header, and both classes
		// have exits that are deliberately unstamped — static's "never
		// resolved" 502 and its pre-EnableStatic 404. A stale row Version
		// wrapping those would put a version on a response that is serving
		// none, which is exactly the false "verified" the header exists to
		// prevent.
		if t.Version != "" && t.Kind != "static" && t.Kind != "status" {
			proxy = versionHeader(t.Version, proxy)
		}
		// Hibernation is per-host: only an instance whose route carries the
		// hibernate flag is activator-managed; unflagged instances and sibling
		// services are proxied straight. With an activator wired but no route
		// flagged, the edge therefore behaves exactly as it does without one —
		// so the binary + env can ship fleet-wide first, and the flag flips
		// one instance at a time.
		//
		// A tunnel-served host is never activator-managed, however it is
		// flagged: the process lives on the far side of the channel and this
		// edge cannot start it, so waking would mean launching a SECOND,
		// local copy of the app beside the remote one. Leaving such a host out
		// of `managed` is also what retires the local child a repointed route
		// left running (RetireExcept, below) — the app moved to the remote
		// host, so the process it left behind here should go. A fleet-placed
		// route is the identical case (viaTunnel is set for both): whichever
		// fleet box ends up serving it wakes its OWN local instance — that
		// box's own activator, a separate process on a separate machine — so
		// this platform edge must never manage it either. The lease race is
		// the backstop if the fallback rule (fleetProxy) ever routes two
		// requests to two different boxes for the same route in a gap.
		hib := e.act != nil && t.Kind == "instance" && t.Hibernate && !viaTunnel
		if hib {
			managed[t.Host] = true
		}
		// A hibernate-flagged instance route on a box with no activator has
		// nothing on this box that would ever spawn its process: the row is
		// real and the socket path is derived, but only an armed activator
		// starts the child that binds it, so a request that reaches this
		// route here falls into unixProxy's dial and comes back as the
		// stdlib ReverseProxy's bare, bodiless 502 — a status that reads as
		// "your backend is broken" about a backend nothing here will start.
		//
		// Diagnostic only, and deliberately careful about what it claims. It
		// says what this box knows — an eligible route, no activator — and
		// not that every request 502s, because it cannot see whether
		// something else is answering: a process started outside the
		// platform could be bound to that socket already. The one deployment
		// shape it was written for is a box whose /etc/carlos/host.env was
		// hand-assembled and never given CARLOS_HIBERNATE_BUCKET, which the
		// region-host bring-up docs make easy to do.
		//
		// The advice names both roles rather than guessing which one is
		// running, because the fix differs and only one of them has one:
		// `carlos edge` standalone passes a nil activator regardless of the
		// environment (RunWithActivator), so an unconditional "set this
		// variable" would send that operator after something that cannot
		// help them. Saying both costs one clause and is true either way.
		//
		// Throttled: a standing condition, true on every reload for as long
		// as the row and the missing activator both exist. Keyed per host —
		// a value the box itself controls, per logThrottled's rule. Not a
		// refusal to serve: dropping the route would only trade a 502 for a
		// 404, and the row may yet be served once an activator arms.
		if e.act == nil && t.Kind == "instance" && t.Hibernate && !viaTunnel {
			e.logThrottled("no-activator "+t.Host, fmt.Sprintf(
				"route %s is a hibernating instance but this box has no activator, so nothing here will create %s or start its process. If this box runs `carlos agent`, set CARLOS_HIBERNATE_BUCKET in /etc/carlos/host.env and restart carlos-edge; `carlos edge` on its own never runs instances.",
				t.Host, t.Socket))
		}
		m[t.Host] = upstream{proxy: proxy, route: t, hibernating: hib, viaTunnel: viaTunnel}
	}
	// Path routes, folded BEFORE aliases so an alias of a prefix-routed
	// hostname inherits its map along with everything else it copies — an
	// alias is meant to be indistinguishable from its primary, and a
	// hostname that routed by prefix under one name but not the other would
	// be a trap.
	//
	// Targets are recorded as hostnames, not resolved here: every route is
	// already in m by this point, but resolving now would freeze a copy of
	// the target's entry, and the target's own row may still be rewritten by
	// the alias fold below.
	paths, err := e.reg.ListPaths()
	if err != nil {
		return err
	}
	for _, p := range paths {
		up, ok := m[p.Host]
		if !ok {
			// The hostname has no route on this box. The reconcile pass
			// should not have produced this row; nothing to attach it to.
			continue
		}
		if up.paths == nil {
			up.paths = make(map[string]string)
		}
		up.paths[p.Prefix] = p.Target
		m[p.Host] = up
	}
	// Attached hostnames (#151): each alias resolves to the primary's OWN
	// entry — same proxy value, same route (so activator keying, version
	// header, and purge tenancy are all the primary's). Routes always win
	// a collision; an alias whose primary isn't in m (not ready, not
	// local) is skipped — never a 502 route to nowhere. Errors abort the
	// reload like every other registry read here: a transient failure must
	// leave the old table serving.
	aliases, err := e.reg.ListAliases()
	if err != nil {
		return err
	}
	for _, a := range aliases {
		if _, taken := m[a.Alias]; taken {
			log.Printf("edge: alias %s shadowed by a real route — skipped", a.Alias)
			continue
		}
		up, ok := m[a.Host]
		if !ok {
			continue
		}
		up.alias = true
		m[a.Alias] = up
	}
	// Fleet channel auth: registry-backed, so this refreshes on the SAME poll
	// that rebuilds routes — Task 1's Fingerprint moves on every fleet
	// mutation (including token rotation), and the edge's existing 2s
	// Watch->Reload loop (below, Watch) is the whole convergence bound for a
	// rotated or detached box. No hub configured (e.tunnels == nil, the vast
	// majority of boxes today) means no fleet channels either — same
	// nil-means-disabled convention as act/cache/staticStore. Errors here
	// abort the WHOLE reload (routes included), matching e.reg.List() above:
	// a transient registry read failure must leave both tables exactly as
	// they were, not blank out fleet auth while routes move on.
	if e.tunnels != nil {
		auth, err := e.reg.FleetChannelAuth()
		if err != nil {
			return err
		}
		e.tunnels.SetDynamicAuth(auth)
		// The store door's tenancy map, from the SAME pass: a box's
		// credentials and the account its reads are scoped to must never come
		// from two different reads of the registry. ListFleets goes over a
		// fresh connection (like FleetChannelAuth above), so a fleet the
		// console created moments ago is visible here rather than stuck
		// behind a stale WAL snapshot on the long-lived handle. Same
		// all-or-nothing error contract as everything else in Reload.
		fleets, err := e.reg.ListFleets()
		if err != nil {
			return err
		}
		accounts := make(map[string]string, len(fleets))
		for _, f := range fleets {
			accounts[f.Name] = f.Account
		}
		e.fleetAccounts.Store(&accounts)
	}
	// Probers BEFORE the map swap — the opposite order to RetireExcept below,
	// and deliberately so. multiProxy answers 503 + Retry-After when the
	// prober has nothing live for its host, and a prober this Sync has not
	// installed yet is indistinguishable from one whose upstreams are all
	// down. Swapping first would therefore open a window — however short —
	// in which a request for a newly added (or newly re-specced) multi-upstream
	// route reaches a handler whose live set is empty and gets a 503 for a
	// perfectly healthy fleet. Syncing first cannot cause the mirror-image
	// problem, because a prober is optimistic at birth (prober.go rule 1): the
	// new host's upstreams are all live the instant Sync returns, before any
	// request can reach the handler that reads them.
	//
	// The reverse case — a route REMOVED by this reload — loses its prober
	// before its handler, so a request landing in that window sees an empty
	// live set and gets 503 instead of proxying a route that is about to stop
	// existing. That is the trade, and it is the right way round: a 503 for a
	// host being deleted is a moment early, a 503 for a host being added is
	// simply wrong.
	e.probers.Sync(probeSpecs)
	e.upstreams.Store(&m)
	// Reconcile the activator with the new route set: a host un-flagged (or
	// removed) since the last reload may still have a live child holding its
	// socket and lease — retire it so a systemd unit can take over cleanly.
	// After the map swap above, so no new request re-enters the activator for
	// a host being retired.
	if e.act != nil {
		e.act.RetireExcept(managed)
	}
	if fn := e.reloadHook.Load(); fn != nil {
		(*fn)()
	}
	return nil
}

// OnReload registers fn to run after every successful Reload — after the new
// route set is swapped in, so fn sees the post-reload table via current().
func (e *Edge) OnReload(fn func()) {
	e.reloadHook.Store(&fn)
}

// logThrottled logs msg the first time key occurs and every 300th time after
// that — the same first-then-every-300th shape as the registry watcher below,
// generalized out of logPurgeIssue (purge_header.go) when Reload gained a
// second line with the same problem.
//
// The problem both share: the condition is a STANDING one, not an event. A
// malformed Carlos-Purge header repeats on every response the app writes; an
// unparseable health column repeats on every reload for as long as the row
// exists. Either would fill a journal with the same sentence. The first
// occurrence is always logged, so nothing is silently swallowed, and the
// running count travels with each line so the rate stays visible.
//
// Keys must be built from values the box itself controls (a route's own host,
// a complaint class) — never from remote input, or the map becomes a memory
// growth vector.
func (e *Edge) logThrottled(key, msg string) {
	e.logMu.Lock()
	if e.logN == nil {
		e.logN = make(map[string]int)
	}
	e.logN[key]++
	n := e.logN[key]
	e.logMu.Unlock()
	if n == 1 || n%300 == 0 {
		log.Printf("edge: %s (occurrence #%d)", msg, n)
	}
}

func (e *Edge) current() map[string]upstream {
	if p := e.upstreams.Load(); p != nil {
		return *p
	}
	return nil
}

// currentHosts lists the hosts of platform-minted routes — rows the console
// minted directly (Platform) or the provisioning reconciler minted from a
// bucket record (Provisioned) — the wildcard manager's view of the route
// table for parent derivation. Not every served host: parents are derived
// from these hosts, and if a custom domain shaped like x.y.<apps-domain>
// ever entered the route table by some other path, it would mint a wildcard
// parent and burn a Let's Encrypt issuance on something the wildcard was
// never meant to cover. Provisioned rows are admitted since 2026-08-24: a
// box whose routes are ALL records-provisioned (every multi-region edge, and
// any new account on a single box) derived no parents at all and could not
// serve a single mirrored wildcard — found on the first three-region
// flagship roll. A provisioned CUSTOM domain (a static record like
// story.carlosframework.com) is harmless here: parentsFor only mints a
// parent for hosts exactly two labels under the apps domain, and custom
// domains under the apps domain are refused at the console. Static parents
// (e.g. tito's go.tito.io) come from CARLOS_WILDCARD_DOMAINS via env, so
// they are unaffected by this filter.
//
// Attached hostnames (#151) are excluded by the !up.alias half: an alias
// entry carries the PRIMARY's route row verbatim, Platform flag included, so
// without the flag a customer domain would inherit its platform primary's
// trust and mint a parent for a zone the platform doesn't own.
func (e *Edge) currentHosts() []string {
	m := e.current()
	out := make([]string, 0, len(m))
	for h, up := range m {
		if (up.route.Platform || up.route.Provisioned) && !up.alias {
			out = append(out, h)
		}
	}
	return out
}

// deploymentHosts is currentHosts plus every exact platform-class name the
// home map says another box homes — the input the wildcard manager's parent
// set is built from (#300). Both halves apply the same class bound (the
// publisher marks what currentHosts would admit); a box with no home map
// (no deployment store) is its own deployment and gets currentHosts alone.
// Not to be confused with deploymentHost (singular): that is the cert
// gates' admission predicate and admits more (aliases, the tunnel host).
func (e *Edge) deploymentHosts() []string {
	own := e.currentHosts()
	hm := e.homes.Load()
	if hm == nil {
		return own
	}
	seen := make(map[string]bool, len(own))
	for _, h := range own {
		seen[h] = true
	}
	for _, h := range hm.Hosts() {
		if !seen[h] {
			seen[h] = true
			own = append(own, h)
		}
	}
	return own
}

// UpstreamStates is the probe table as the rest of the process may see it:
// route host -> each upstream's current state, in the route's addr order,
// for probed (multi-upstream) routes only. A route with no health config
// holds no prober and is absent, as is every host on an edge that probes
// nothing at all — the common shape, which reports an empty map.
//
// The agent's status heartbeat is the caller (internal/host wires this in from
// the edge-ready callback, the same seam tunnelLoop arrives through) and it
// calls this once per tick, taking the WHOLE table each time rather than
// diffing: everything returned is a copy proberSet.State built under the
// per-host lock, so serializing it while probes keep running is safe, and a
// route that stopped being probed is simply gone from the next call rather
// than lingering as a stale entry someone has to remember to evict.
func (e *Edge) UpstreamStates() map[string][]UpstreamState {
	return e.probers.State()
}

// Allowed reports whether host is configured — used as the autocert
// HostPolicy, so a certificate is only ever requested for a host we serve.
func (e *Edge) Allowed(host string) bool {
	// Never certable directly: refuse the wildcard label itself rather than
	// resting on autocert's own IDN rejection of "*" to keep the bounded set.
	if strings.HasPrefix(host, "*.") {
		return false
	}
	// The reserved tunnel hostname is not a registry route, but remote hosts
	// dial it over TLS on this same 443 listener — so it must be Allowed, and
	// autocert will mint for it. That is intended: it is a hostname the
	// operator configured on this box, not a stranger-supplied SNI.
	if e.tunnels != nil && host == e.tunnels.Host() {
		return true
	}
	// Deliberately the EXACT map only — not lookupUpstream's wildcard
	// fallback. A catch-all alias ("*.<domain>") is a bounded, registry-backed
	// name; autocert's per-host HostPolicy gates a NEW per-host ACME order,
	// which is unbounded (labels are attacker-chosen) and shared across every
	// tenant on the box (one ACME account, one rate-limit bucket). Coverage
	// for a one-label host under a catch-all comes from the WILDCARD cert
	// (wrapGetCertificate calls certFor before ever reaching autocert); this
	// exact check only decides per-host HTTP-01 fallback for ROUTED hosts —
	// widening it to the wildcard fallback would let a stranger's SNI
	// <anything>.<domain> drive a fresh order and exhaust the box's shared
	// ACME quota for every tenant on it.
	_, ok := e.current()[host]
	return ok
}

// deploymentHost reports whether host is served HERE (Allowed) or is exactly
// named by another box's edges file (homeMap.KnownHost) — the admission bound
// for the three cert-adjacent gates in certsync.go: issuerHostPolicy's order
// gate and the HTTP-01 and tls-alpn-01 read-throughs.
//
// The widening exists because Allowed alone was the wrong bound the moment a
// deployment grew a second edge (#277): DNS for a home-boxed route can point
// at any edge, the backhaul then dials the home box with the real SNI, the
// home box orders — and the CA validates against whichever edge DNS names,
// which refused every challenge for a host outside its OWN registry. Five
// such failures burn the name's Let's Encrypt per-hostname budget in minutes
// while the route is down. Both halves are registry-backed and
// deployment-controlled: the home map is folded from control/edges/<box>.json,
// which each box writes from its own registry (internal/host's edgesFilePass),
// so a stranger cannot add a name to it without a control-bucket write.
//
// EXACT only, on both halves: Allowed refuses the wildcard key and consults
// the exact route map, KnownHost refuses "*." and skips Home()'s one-label
// fallback. That is PR #180's rule, unchanged and merely widened from "this
// box's routes" to "this deployment's exactly-named routes" — an
// attacker-chosen label under a catch-all must never drive an ACME order or a
// bucket read.
//
// Both halves take the same normalized form: serverName/requestHost lowercase
// and punycode into the shape registry rows, and therefore edges files, are
// written in. e.homes may hold nil before the map arms; KnownHost is
// nil-receiver-safe, matching Home's pattern.
func (e *Edge) deploymentHost(host string) bool {
	return e.Allowed(host) || e.homes.Load().KnownHost(host)
}

// lookupIn resolves host to its upstream entry within a given map — the
// HOST half of the request-routing lookup (NOT Allowed's autocert HostPolicy
// — see the comment there). ServeHTTP does not call it directly: it calls
// lookupUpstreamPath, which wraps this and then resolves a leading path
// segment for the hostnames that carry a prefix map. An exact match (a real route, or a plain #151
// alias landed in the same map) always wins; on a miss, a one-label wildcard
// key folds in: "*.<parent>" is the shape Task 3's reconcile pass derives
// from a claim's catch-all row, and it lands in the SAME map as every other
// alias (up.alias = true, primary's route + proxy verbatim) so this fallback
// resolves to the primary's own entry — the identical map value the
// #151/#152 agreed rule requires, not a value that merely behaves the same.
// A wildcard matches exactly one label, mirroring the TLS cert: host[i:]
// keeps the LEADING dot of the FIRST label only, so a two-label host
// (a.b.woodstar.app) composes "*.b.woodstar.app" and misses, and the apex
// itself (woodstar.app) composes "*.app" — never its own "*.woodstar.app" —
// so it can never match its own wildcard.
func lookupIn(m map[string]upstream, host string) (upstream, bool) {
	if up, ok := m[host]; ok {
		return up, true
	}
	if i := strings.IndexByte(host, '.'); i > 0 {
		if up, ok := m["*"+host[i:]]; ok {
			return up, true
		}
	}
	return upstream{}, false
}

// lookupUpstream resolves a hostname alone. Kept as the primitive because not
// every caller is answering "where does this REQUEST go" — the :80->:443
// routability check in httpFallbackHandler asks only whether this box serves
// the hostname at all, and giving that question a path would make a
// hostname's routability depend on which URL happened to ask.
func (e *Edge) lookupUpstream(host string) (upstream, bool) {
	return lookupIn(e.current(), host)
}

// lookupUpstreamPath resolves a request: the hostname exactly as
// lookupUpstream does, then — only for a hostname carrying a prefix map — the
// first path segment.
//
// Both lookups read ONE snapshot of the map. Calling lookupUpstream and then
// current() again would let a reload land between them and resolve a prefix
// against a different table than the host came from.
//
// Every miss falls through to the hostname's own route rather than failing:
// an unlisted prefix, a dangling target, a request for "/". A prefix map is a
// migration in progress, and the un-migrated answer is always the safe one.
//
// ATTRIBUTION: a path hit returns the TARGET's upstream entry wholesale, so
// everything downstream that reads up.route reads the target's row, not the
// requested hostname's. Four consequences, named here because this is where a
// reader will look for them:
//
//  1. Usage metering bills the TARGET's route, account and app (the
//     UsageMeter wrap in ServeHTTP keys on up.route.Host). Correct: the
//     target's backend did the work and burned the bytes. Pinned by
//     TestPathRoutedRequestMetersAgainstTheTarget — a later "fix" that
//     attributes to the requested hostname would double-count nothing and
//     bill the wrong app.
//  2. The X-Carlos-Version response header reports the TARGET's release.
//     Correct: it names the code that produced the response.
//  3. Purge tenancy is the TARGET's app and account (purgeModifier is built
//     from up.route), while the response CACHE keys on the REQUESTED host.
//     Those two disagree, and closing that is what the path-routing branch
//     in purgeHostInTenancy exists for — see below.
//  4. Activator keying and hibernation are the TARGET's (up.hibernating and
//     up.route come from the same entry), so a request for a flipped prefix
//     wakes the target instance, not the requested hostname's. Correct: the
//     target is the process that has to be awake to answer.
//
// How (3) is closed, and what an app must do. The cache keys on the requested
// host (CacheKey{Host: host} in ServeHTTP) while purgeModifier is built from
// the target's route, so a path-routed app emitting a bare Carlos-Purge still
// purges under go.tito.io while its entry is stored under ti.to, and drops
// nothing. The fix is the documented escape hatch: name the hostname the
// request arrived on, Carlos-Purge-Host: ti.to. The override check used to
// refuse that — it required the override to name a host in the SAME app, and
// path routing exists precisely to cross an app boundary — and now allows it
// when a prefix on the named host selects this route. Account is still
// required. Aliases never hit any of this: an alias key maps to the primary's
// own route, so the app comparison passes trivially.
//
// So an app can emit Carlos-Purge-Host set to the Host header it received,
// unconditionally, from shared code. The inbound Host is preserved end to end,
// so on an ordinary request that equals rt.Host and takes resolvePurgeHosts'
// own-host branch; on a path-routed one it names the front hostname and takes
// the path-routing branch. Nothing in the app has to know which it is.
func (e *Edge) lookupUpstreamPath(host, urlPath string) (upstream, bool) {
	m := e.current()
	up, ok := lookupIn(m, host)
	if !ok || up.paths == nil {
		return up, ok
	}
	// BOTH FORMS, cleaned first. The prefix map is consulted for the
	// path.Clean'd first segment and then, if that missed and the two differ,
	// for the raw one. When both name a prefix, the cleaned one wins.
	//
	// Neither form alone is safe, and they fail in OPPOSITE directions. The
	// invariant being protected is one-way: during a reversible dual-run
	// migration, the app an account was flipped AWAY from must never answer
	// for that account. Serving a not-yet-flipped account from the NEW app is
	// the benign direction — at worst a 404 while the migration runs.
	//
	//   - RAW alone misses "//paulca/foo". Rails and most other backends
	//     collapse that to "/paulca/foo" and serve it, so the request falls
	//     through to the legacy app, which then answers for a flipped
	//     account. One character, silent.
	//   - CLEANED alone misses "/paulca/../bob", which resolves to "bob". A
	//     backend that does NOT collapse ".." — or that routes on the first
	//     segment with a glob like "/:account/*rest" — then serves paulca's
	//     page from the legacy app. The same bypass, reached the other way.
	//
	// Taking a hit from either form closes both: "//paulca/foo" hits on the
	// cleaned form, "/paulca/../bob" hits on the raw one, and a flipped
	// account reaches its new app whatever the backend does with the odd
	// shape. The cost is that "/paulca/../bob" reaches the NEW app carrying
	// "/paulca/../bob", which a normalising target resolves to "/bob" and
	// 404s — an account it was not given. Benign direction, and deliberate.
	//
	// Residue, measured rather than assumed. None of it crosses the
	// invariant above, and it is written down so nobody has to re-derive it:
	//
	//   - "/%2e%2e/bob": r.URL.Path is already percent-DECODED, so this
	//     cleans to "/bob" and matches "bob". The upstream receives the raw
	//     "/%2e%2e/bob" and will most likely 404 it. Harmless.
	//   - "/paulca%2ffoo": decodes to "/paulca/foo", so both forms match
	//     "paulca" and the request reaches paulca's new app — while a strict
	//     backend treats the upstream path as the SINGLE segment
	//     "paulca/foo". A mis-flip toward the NEW app: the safe direction.
	//   - A prefix literally named "." or ".." cannot exist: validPathPrefix
	//     (internal/domains/claims.go) refuses both, so the raw form of
	//     "/../bob" ("..") can never match anything.
	//
	// Do not collapse this back to a single form. Whichever one you keep,
	// you reopen the bypass the other one closes.
	//
	// The scope of the guarantee, stated so it is not over-read: the two
	// forms model percent-decoding and dot-segment removal, and the
	// invariant holds across every combination of a backend doing or not
	// doing those two. It is NOT a claim that no normalisation exists. A
	// backend that folds case ("/PAULCA/foo"), strips servlet-style path
	// parameters ("/paulca;jsessionid=x/foo"), accepts backslash
	// separators, strips trailing dots, or double-decodes could resolve a
	// path neither form matches into a flipped account's subtree. Rails
	// does none of these — Rack keeps ";" in PATH_INFO and routing is
	// case-sensitive — so Tito is not exposed, but a future consumer with
	// different normalisation would need this reasoning redone rather than
	// inherited.
	//
	// No guard is needed for the empty segment ("/", "//", ""): validPathPrefix
	// refuses an empty prefix, so no row can be keyed on one and both lookups
	// below simply miss, falling through to the host's own route.
	seg := firstSegmentClean(urlPath)
	raw := firstSegment(urlPath)
	target, ok := up.paths[seg]
	if !ok && raw != seg {
		target, ok = up.paths[raw]
	}
	if !ok {
		return up, true
	}
	// m[target], not lookupIn(m, target): the TARGET is resolved exactly,
	// while the host half above also accepts a one-label wildcard key. The
	// asymmetry is deliberate and it is documented rather than removed —
	// route_paths targets are hostnames the reconcile pass verified against a
	// real local route (aliases.go), and a real route is always an exact key,
	// so a target reachable ONLY through "*.parent" cannot be produced by any
	// supported writer. If one ever were, it would miss here and fall through
	// to the host's own route, which is this function's safe direction
	// anyway.
	tu, ok := m[target]
	if !ok {
		return up, true
	}
	// ONE HOP, deliberately: if tu itself carries a prefix map, it is NOT
	// re-resolved against this request's path. A prefix selects a route, and
	// the selected route's own map belongs to ITS hostname's traffic, not to
	// this one's. Do not add recursion here — chaining buys nothing the
	// publisher cannot express by pointing the prefix at the final target
	// directly, and it would introduce a cycle to detect.
	return tu, true
}

// firstSegment returns the first path segment of an absolute URL path,
// without slashes, splitting the path EXACTLY as it arrived:
// "/paulca/awesomeconf" -> "paulca". "" for "/", for the empty string, and
// for anything not starting with "/".
//
// Returning the segment rather than testing a string prefix is what makes
// matching segment-bounded: "/paulcarson" yields "paulcarson", which is not
// equal to "paulca", so a longer account name can never be dragged across a
// migration by a shorter one.
//
// This is the RAW half of the matcher. See lookupUpstreamPath's match block
// for why both halves exist and why neither is safe alone.
func firstSegment(p string) string {
	if p == "" || p[0] != '/' {
		return ""
	}
	p = p[1:]
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	return p
}

// firstSegmentClean is firstSegment over the path.Clean'd path, so leading
// empty segments and dot segments resolve before the split:
//
//	"//paulca/foo"   -> "paulca"   (leading empty segments collapse)
//	"/./paulca"      -> "paulca"   (dot segment collapses)
//	"/paulca/../bob" -> "bob"      (".." resolves)
//	"/paulca/.."     -> ""         (resolves to "/", the host's own root)
//	"/paulca/"       -> "paulca"   (a trailing slash is not a second segment)
//
// This is the CLEANED half of the matcher; the raw half is firstSegment.
func firstSegmentClean(p string) string {
	if p == "" || p[0] != '/' {
		return ""
	}
	// Clean of a rooted path is rooted, and is "/" when everything resolves
	// away — so the slice below is always safe and yields "" for the root.
	return firstSegment(path.Clean(p))
}

func (e *Edge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// The deployment's steering health check (steeringprobe.go), answered
	// FIRST — ahead of the tunnel hub, the hop, routing and, decisively, the
	// unknown-host fallback. A -fallback edge redirects an unknown host with
	// a 302, which Route53 scores as healthy: reached in that order, the one
	// mechanism that withdraws a dead edge would report every edge alive
	// forever. Inert (one comparison against "") unless this deployment
	// steers.
	if e.steeringProbe(w, r, host) {
		return
	}
	// The reserved tunnel hostname is answered by the hub itself — before the
	// route lookup, so registration cannot be intercepted by a route, and
	// after the host:port split, so a dialer sending "tunnel.host:443" still
	// lands here. It wins over a registry route of the same name: the operator
	// owns both names, and a shadowed route is visible (its host stops
	// answering) where a shadowed hub would be a locked-out fleet.
	//
	// Being ahead of the cache path is load-bearing too, not just tidy: the
	// hub ends its handshake in a hijack, so it must be handed the REAL
	// ResponseWriter, never a recordingResponseWriter wrapping it.
	if e.tunnels != nil && host == e.tunnels.Host() {
		// The fleet store door (storedoor.go) shares this hostname, under
		// /store/ — the same host gate as registration, which is the whole
		// reason it is reachable at all: it is not a registry route, so no
		// other host can ever reach it. /register and everything else stay
		// the hub's. Ahead of the hub rather than behind it because the hub
		// 404s any path that is not /register.
		if e.door != nil && strings.HasPrefix(r.URL.Path, storeDoorPrefix) {
			e.door.ServeHTTP(w, r)
			return
		}
		e.tunnels.ServeHTTP(w, r)
		return
	}
	// Backhaul hop acceptance (homemap.go). Deliberately placed HERE — after
	// the reserved tunnel hostname's branch above, which stays byte-for-byte
	// what it was (that hostname is not a registry route, so it can never
	// appear in any box's edges file and can never be a hop's target), and
	// before the route lookup, so that a verified hop naming a host we do not
	// serve is answered by the stale-map refusal rather than the ordinary
	// unknown-host answer.
	//
	// On a request carrying no X-Carlos-Hop this is one Header.Get and
	// nothing else.
	r, hopped := e.acceptHop(r, host)

	up, ok := e.lookupUpstreamPath(host, r.URL.Path)
	if !ok {
		if hopped {
			// A PROVEN hop from a sibling edge, for a host that has moved (or
			// never lived) here: its map is stale. 503 + Retry-After: 2, and
			// this return is the structural half of "one hop, ever" — the
			// backhaul dial below lives further down this same branch, so an
			// accepted hop physically cannot reach it. (e.backhaul opens with
			// the same refusal anyway, so moving this branch cannot open a
			// forwarding loop either — see its doc.)
			e.homes.Load().staleHopRefusal(w, host)
			return
		}
		// Nothing here serves this host — but another box in the deployment
		// may (backhaul, below). It answers false when no box homes the host,
		// or when nothing could prove the hop, and today's unknown-host answer
		// then follows exactly as it always has.
		if e.backhaul(w, r, host) {
			return
		}
		e.unknownHost(w, r)
		return
	}

	// A login POST re-resolves its upstream by the path in `next`, not by
	// its own (spec §4.2). On a hostname that routes by path prefix,
	// lookupUpstreamPath keys on the first segment; "/.carlos/gate" has no
	// segment that can ever be a prefix, so a login POST falls through to
	// the FRONT host's route — the password would be judged by the wrong
	// row (or proxied to an app that never asked for it, which the
	// ungated-route branch below then refuses), and the cookie would be
	// signed for the wrong host. `next` is the path the visitor was refused
	// on, so resolving by it lands on the route that put up the page. On a
	// host with no prefix map it resolves to the same route either way.
	//
	// Placed AFTER the lookup above and its !ok branch, which stay
	// byte-for-byte what they were, because this reads the request BODY: a
	// host this box does not serve may be backhauled to the box that homes
	// it (e.backhaul, above), and that reverse-proxies this very request —
	// draining its body here would send the home box a ContentLength it
	// cannot fill, and the hop would 502. Nothing is read until the host is
	// known to be ours. Placed before the meter so every step below,
	// billing included, sees one settled up.
	//
	// A body that will not parse — over gate.MaxFormBody, or malformed — is
	// answered 413 here and never reaches Admit, so it spends no limiter
	// token and no bcrypt comparison. Refusing junk must not cost a real
	// visitor their attempts.
	loginPost := r.Method == http.MethodPost && r.URL.Path == gate.LoginPath
	if loginPost {
		r.Body = http.MaxBytesReader(w, r.Body, gate.MaxFormBody)
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}
		// Resolve on the PATH of `next`, never on the raw string. The 401
		// page's next is SafeNext(r.URL.RequestURI()), so it carries the
		// query the visitor was refused on and it is percent-ENCODED —
		// while the prefix map is keyed on decoded segments, the way
		// r.URL.Path arrives for an ordinary request. Splitting the raw
		// string makes firstSegment("/paulca?tab=1") "paulca?tab=1", which
		// matches nothing and drops the login onto the FRONT route: the
		// wrong hash judges the password and the cookie is signed for the
		// wrong host. Parsing first drops the query and the fragment and
		// decodes the segments, so both shapes land where the refusal did.
		//
		// A miss cannot happen — SafeNext yields a path on this host, and
		// the worst case "/" resolves to the host's own route, the one
		// already in up — but keeping the first up is the honest answer if
		// the table changed underneath.
		next := gate.SafeNext(r.PostForm.Get("next"))
		lookupPath := "/"
		if u, err := url.ParseRequestURI(next); err == nil && u.Path != "" {
			lookupPath = u.Path
		}
		if target, hit := e.lookupUpstreamPath(host, lookupPath); hit {
			up = target
		}
	}

	// Observe every route this box serves except the platform's own status
	// host (not billable, and not a member's app) — wrapping w here, once,
	// before the upgrade/cache branching below, is what makes all three
	// dispatch shapes (hibernating, cacheable, plain) plus the upgrade path
	// pass through this single site: the defer fires after whichever branch
	// actually served the request. Deliberately placed after the
	// tunnel-hostname and store-door returns above (edge.go:531-544) — that
	// hostname isn't a registry route, so up.route isn't defined yet there,
	// and neither one is billable traffic.
	//
	// The observing writer is installed UNCONDITIONALLY now (spec
	// 2026-09-07 §3.3) — the usage meter is one of its two consumers, the
	// errors sink the other — because a box that meters nothing still has
	// errors to file. Meter and sink are each nil-safe, so a pure-proxy
	// `carlos edge` pays one wrapper and no more.
	//
	// hopped is the ATTRIBUTION half of the backhaul egress ruling
	// (2026-08-24): a request that arrived over a verified hop is metered
	// here exactly once, as it always was, and additionally annotated into
	// the row's subset hop counters — so every backhauled byte is attributed
	// to the route, account and app that caused it, on the box that holds
	// the authoritative row. The sending edge stays meterless by design: it
	// has no route row for the host and so no identity to bill. Nothing
	// about the wrapper stack moves for this — recorder above the
	// compressor, this counter below it — because the annotation changes
	// WHAT is written at drain, never WHERE the bytes are counted.
	var bytes int64
	ob := &countingWriter{ResponseWriter: w, n: &bytes}
	w = ob
	if up.route.Kind != "status" {
		// The outcome rides the request so the two things only the inner
		// layers know reach this defer: the activator marks the wake-path
		// 503s it writes itself (MarkAnswered, answered.go) — already the
		// member's platform event, so not filed again (spec §3.2, "exactly
		// once") — and a proxy ErrorHandler notes WHY it answered 502/503
		// (noteCause), which is what groups Availability by cause.
		var out *outcome
		r, out = withOutcome(r)
		defer func() {
			if m := e.usageMeter.Load(); m != nil {
				m.Record(up.route, 1, bytes, hopped)
			}
			if ob.status >= 500 && !out.answered {
				e.emitEdgeError(up.route, up.viaTunnel, r, ob.status, out.cause)
			}
			e.observeAnalytics(up.route, r, ob, bytes)
		}()
	}

	// The password gate (internal/gate; spec 2026-09-07-password-gate-design
	// §4.1). Placed HERE and the neighbours are load-bearing: after the
	// meter, so the login page's bytes bill to the route that caused them;
	// before the upgrade branch, so a WebSocket handshake is gated; before
	// the cache check, so a cache hit is only ever served to a request that
	// already passed (the LRU keys on host and path, not on cookies); and
	// before the activator, so a stranger — a crawler, a scanner, a guessed
	// hostname — never wakes a sleeping instance. Admit strips the cookie
	// on the way through, so the app never sees it.
	//
	// A login POST that lands on an UNGATED route is a 404 and is never
	// proxied (spec §4.2): the form is only ever rendered by a gated route,
	// so this is a stray or a probe, and forwarding a field named
	// "password" to an app that never asked for it is the one outcome to
	// rule out. Every other method leaves the path ordinary.
	if loginPost && up.route.Gate == "" {
		http.NotFound(w, r)
		return
	}
	if up.route.Gate != "" && !e.gate.Admit(w, r, up.route.Host, up.route.Gate) {
		return
	}

	// A Connection: Upgrade request (a WebSocket handshake, typically GET)
	// must reach the activator/proxy with the real ResponseWriter, completely
	// untouched by caching: the stdlib reverse proxy's upgrade path type-
	// asserts the ResponseWriter to http.Hijacker to take over the raw
	// connection, and once upgraded, the open WebSocket is also what keeps a
	// hibernating instance's idle clock from firing (the activator's inflight
	// counting — see internal/activator/activator.go's actEntry.inflight doc
	// comment). So this path skips caching entirely and stays byte-for-byte
	// identical to pre-cache behavior, rather than relying on
	// recordingResponseWriter's Hijack forwarding below.
	if isUpgradeRequest(r) {
		if up.hibernating {
			if e.act.ServeHTTP(w, r, up.route, up.proxy) {
				return
			}
		}
		up.proxy.ServeHTTP(w, r)
		return
	}

	// Response compression (compress.go). Placed AFTER the upgrade branch, so
	// an upgrading request keeps the exact writer stack it has always had —
	// purge_header.go's contract records what wrapping the raw ResponseWriter
	// on that path has already cost this package — and BEFORE the cache check,
	// so a cache hit compresses on replay like any other delivery.
	//
	// The recorders constructed below wrap THIS writer, so they record
	// identity bytes and the cache keeps storing identity bodies; the usage
	// meter's countingWriter is below it, so the metered count is the
	// compressed one. This defer is registered after the meter's, so LIFO
	// closes the gzip stream — trailer included — before Record reads it.
	if e.gzipOn {
		if gw := newGzipWriter(w, r); gw != nil {
			w = gw
			defer gw.finish()
		}
	}

	// Cache check happens before the hibernation branch below — a hit must
	// never reach the activator, since avoiding pointless wakes for
	// bot/crawler/repeat traffic is the entire point of this cache.
	//
	// Kind=="status" is exempt from the shared cache in both directions: the
	// status host caches for itself (status.go's per-key store cache) and its
	// /ledgers/ objects carry a long immutable Cache-Control so browsers and
	// CDNs cache them. Admitting them HERE would be a different thing
	// entirely — this LRU is process-global and shared across every host, and
	// a public ledger chain is unbounded in length, so an anonymous walker
	// could evict every other tenant's cached responses at will. An evicted
	// entry for a hibernating route means the next request wakes the instance
	// instead of being answered from cache, which is the invariant this cache
	// exists to hold: the status host caches for itself; its responses never
	// enter the shared response cache.
	cacheable := e.cache != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		up.route.Kind != "status"
	var key CacheKey
	if cacheable {
		key = CacheKey{Host: host, Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		// GetFor, not get: the variant this request belongs to depends on the
		// response's Vary header, which doesn't exist yet — GetFor matches r
		// against the Vary names each stored variant recorded for itself, so
		// the entry it returns is one maybeStore stored under exactly these
		// request headers.
		if resp, hit := e.cache.GetFor(key, r); hit {
			writeCached(w, resp)
			return
		}
	}

	if up.hibernating {
		// Wake (or join an in-progress wake) before proxying; the activator
		// also stamps this request's activity for the idle clock. Note a bare
		// TLS handshake never wakes anything — only a proxied request does.
		// The response is always recorded (even when not cacheable, in which
		// case maybeStore is a no-op on rec.cacheable) so this branch stays a
		// single shape regardless of method.
		rec := &recordingResponseWriter{ResponseWriter: w, request: r, cacheable: cacheable, maxBody: e.cacheMaxBody()}
		if e.act.ServeHTTP(rec, r, up.route, up.proxy) {
			e.maybeStore(key, rec)
			return
		}
		up.proxy.ServeHTTP(rec, r)
		e.maybeStore(key, rec)
		return
	}

	if cacheable {
		rec := &recordingResponseWriter{ResponseWriter: w, request: r, cacheable: true, maxBody: e.cacheMaxBody()}
		up.proxy.ServeHTTP(rec, r)
		e.maybeStore(key, rec)
		return
	}
	up.proxy.ServeHTTP(w, r)
}

// isUpgradeRequest reports whether r is a protocol-upgrade request (e.g. a
// WebSocket handshake) per the Connection header's token list (RFC 7230
// §6.7: comma-separated, case-insensitive — "Upgrade", "keep-alive, Upgrade",
// etc. all qualify). Connection is list-valued (RFC 9110 §5.3), so this reads
// every line via joinHeader rather than r.Header.Get, which returns only the
// first — the same class of bug joinHeader's doc comment describes for Vary
// and Cache-Control, here on the request's Connection header instead.
func isUpgradeRequest(r *http.Request) bool {
	for _, token := range strings.Split(joinHeader(r.Header, "Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "Upgrade") {
			return true
		}
	}
	return false
}

// cacheMaxBody reports the body-size ceiling recordingResponseWriter should
// enforce, or 0 when caching is disabled (recordingResponseWriter is never
// constructed with cacheable=true in that case, so the value is unused).
func (e *Edge) cacheMaxBody() int {
	if e.cache == nil {
		return 0
	}
	return e.cache.maxBodyBytes
}

// maybeStore inspects a recorded response and caches it if it qualifies:
// 2xx status, a Cache-Control that names a positive shared-cache lifetime
// (s-maxage, else max-age — see parseMaxAge), and none of the per-visitor
// markers below. This cache is shared across every visitor of a
// host and caching is unconditional per route (not opt-in per app), so each
// refusal here is the only thing standing between one visitor's response and
// everyone else's:
//
//   - Cache-Control: private — the app itself saying "one visitor only".
//   - Set-Cookie — the response carries per-visitor state (a session id, a
//     CSRF token); replaying it would hand that cookie to later visitors.
//   - an Authorization header on the REQUEST — the response is presumed
//     private per RFC 9111 §3.5, unless Cache-Control explicitly says public,
//     which is the app asserting the response is safe to share.
//   - Vary: Cookie — varying by cookie would defeat correctness per-visitor.
//   - Vary: * — RFC 9111 §4.1 / RFC 9110 §12.5.5: the response depends on
//     something request headers can't capture, so it is never reusable from
//     a shared cache.
//
// The stored TTL is clamped to DefaultCacheMaxTTL so a lost purge always
// self-heals within a bounded window.
//
// One refusal is NOT made here because it has to be made earlier, where the
// route is still in hand: Kind=="status" responses are never admitted at
// all. See the cacheable computation in ServeHTTP for why.
func (e *Edge) maybeStore(key CacheKey, rec *recordingResponseWriter) {
	if e.cache == nil || !rec.cacheable || rec.truncated {
		return
	}
	if rec.status < 200 || rec.status >= 300 {
		return
	}
	cc := joinHeader(rec.Header(), "Cache-Control")
	ttl, ok := parseMaxAge(cc)
	if !ok || ttl <= 0 {
		return
	}
	if len(rec.Header().Values("Set-Cookie")) > 0 {
		return
	}
	if len(rec.request.Header.Values("Authorization")) > 0 && !hasCCDirective(cc, "public") {
		return
	}
	if ttl > DefaultCacheMaxTTL {
		ttl = DefaultCacheMaxTTL
	}
	// The refusals below read the RAW joined field value, before any
	// canonicalization, and compare case-insensitively: whatever normalizing
	// happens downstream, "Vary: *" and "Vary: cookie" must be refused in every
	// spelling and on every line they can arrive in.
	vary := joinHeader(rec.Header(), "Vary")
	if vary != "" {
		for _, h := range strings.Split(vary, ",") {
			h = strings.TrimSpace(h)
			if h == "*" || strings.EqualFold(h, "Cookie") {
				return
			}
		}
		// The same computation GetFor replays at lookup time — both call
		// varyValue, which canonicalizes the name list itself, so the store key
		// and the lookup key can never drift on either count.
		key.VaryValue = varyValue(rec.request, vary)
	}
	// vary is handed to Put as well as consumed here: Put canonicalizes it the
	// same way varyValue just did and stores it with the entry, as part of its
	// key, so a later GetFor can replay this exact selection against a later
	// request — and so a response that varies by different headers, or doesn't
	// vary at all, becomes a variant beside this one rather than a replacement
	// for it.
	e.cache.Put(key, vary, &CachedResponse{
		StatusCode: rec.status,
		Header:     rec.Header().Clone(),
		Body:       rec.body,
		StoredAt:   time.Now(),
		TTL:        ttl,
	})
}

// joinHeader returns every line of one header field as a single comma-joined
// value. RFC 9110 §5.3: a field may legally appear on multiple lines and that
// is identical in meaning to one comma-joined line — which is exactly what
// http.Header.Add produces, and exactly how layered middleware emits
// list-valued fields (compression adds Vary: Accept-Encoding, the session
// layer separately adds Vary: Cookie; one handler sets Cache-Control:
// max-age=600 and an auth wrapper adds Cache-Control: private).
//
// Every Cache-Control and Vary question this file asks goes through here
// because http.Header.Get returns ONLY the first line: reading either field
// with Get made every later line invisible, so a second line saying "private"
// or "Cookie" silently failed to refuse the response. Joining with ", " keeps
// the result a well-formed field value for the comma-splitting parsers below.
func joinHeader(h http.Header, name string) string {
	return strings.Join(h.Values(name), ", ")
}

// canonicalVaryNames reduces a Vary field value to the set of header names it
// selects on, in one canonical form: split on commas (a Vary may be comma-
// joined from several lines — see joinHeader), trimmed, empty tokens dropped,
// case-canonicalized, deduplicated and sorted.
//
// Everything downstream — the Vary component of the cache key, the specificity
// count, and the order varyValue renders values in — is derived from THIS list
// rather than from the raw field value, so spellings that mean the same thing
// share one cache slot instead of splitting the URL's variant allowance into
// near-duplicates: "Vary: accept-encoding" and "Vary: Accept-Encoding" are one
// scheme, and so are "Vary: A, B" and "Vary: B, A" (RFC 9110 §5.1: field names
// are case-insensitive; §12.5.5 gives the Vary list no ordering meaning).
//
// A Vary of only separators ("Vary: , , ,") yields no names at all, which is
// exactly a response that selects on nothing: it canonicalizes to "", takes the
// no-Vary path through Put and matches(), and has specificity 0.
func canonicalVaryNames(vary string) []string {
	var names []string
	for _, h := range strings.Split(vary, ",") {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		h = textproto.CanonicalMIMEHeaderKey(h)
		if slices.Contains(names, h) {
			continue
		}
		names = append(names, h)
	}
	slices.Sort(names)
	return names
}

// canonicalVary renders canonicalVaryNames back as a field value. This is the
// form stored in cacheFullKey.Vary, so key equality is spelling-independent.
func canonicalVary(vary string) string {
	return strings.Join(canonicalVaryNames(vary), ", ")
}

// varyValue concatenates r's values for each header the response's Vary list
// names, in canonical name order, so the same selecting headers with the same
// request values always produce the same bucket (standard HTTP Vary semantics;
// RFC 7234 §4.1). Both the store path (maybeStore) and the lookup path
// (cacheEntry.matches) render through this one function, so the two can never
// disagree about either the canonicalization or the rendering.
//
// Each value is LENGTH-PREFIXED — "Name=<len>:<value>;" — which is what makes
// the rendering injective for a given name list. Without the prefix the
// separators were forgeable from inside a header value: under Vary: A, B a
// request sending A: "u;B=v", B: "w" rendered "A=u;B=v;B=w;", byte-identical to
// A: "u", B: "v;B=w" — two different requests sharing one cache slot, so
// whichever stored first had its body served to the other. A length prefix
// cannot be forged from within the value it measures: the reader takes exactly
// len bytes, so no content can be mistaken for structure.
//
// The request side reads all of a header's lines (joinHeader), so two requests
// that differ only in how they split a varied-on header still land in the same
// bucket, which is what RFC 9110 §5.3 says they mean.
func varyValue(r *http.Request, vary string) string {
	var b strings.Builder
	for _, h := range canonicalVaryNames(vary) {
		v := joinHeader(r.Header, h)
		b.WriteString(h)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(len(v)))
		b.WriteByte(':')
		b.WriteString(v)
		b.WriteByte(';')
	}
	return b.String()
}

// parseMaxAge extracts the freshness lifetime a SHARED cache should use from a
// Cache-Control header. Returns ok=false for "no-store", "no-cache", "private",
// or a missing/malformed lifetime — any of which mean "don't cache this in a
// shared cache."
//
// s-maxage wins over max-age when both are present (RFC 9111 §5.2.2.10: it
// overrides max-age for a shared cache, and a private cache ignores it). That
// is the only directive pair an origin has for telling the two caches apart,
// and it is what makes a purge effective end to end: "s-maxage=120, max-age=0"
// asks the edge to hold the response for two minutes and every browser to
// revalidate, so a purge here is not left with a client-side tail it cannot
// reach. Reading only max-age left an origin one number for both caches — a
// long one bought edge caching at the cost of stale browsers after a purge, a
// short one freshened browsers by giving up the traffic the cache exists to
// absorb, and the correct pair above read as max-age=0 and turned edge caching
// off entirely (issue #403; tito PRO-4758/PRO-4759).
//
// max-age alone still means exactly what it did. Nothing can have depended on
// s-maxage being ignored, because ignoring it is what made it unusable.
//
// The whole header is scanned before any lifetime is honored, so a
// "max-age=600, private" (directives in either order, and "private" in its
// field-name form private="Set-Cookie") is refused rather than cached because
// max-age happened to come first — and, for the same reason, the two spellings
// "s-maxage=120, max-age=0" and "max-age=0, s-maxage=120" mean the same thing.
// A malformed argument on EITHER directive refuses the response whichever order
// it arrives in, rather than falling back to the other one: an origin that
// cannot state its lifetime is not asking for a guess.
//
// s-maxage is deliberately NOT read as an override of the Authorization
// refusal in maybeStore, though RFC 9111 §3.5 permits that: making a response
// to an authenticated request shareable stays an explicit "public", because
// declining to cache is the safe direction and the origin already has the word
// for it.
func parseMaxAge(cc string) (time.Duration, bool) {
	var ttl, shared time.Duration
	found, sharedFound := false, false
	for name, arg := range ccDirectives(cc) {
		switch name {
		case "no-store", "no-cache", "private":
			return 0, false
		case "max-age":
			secs, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil {
				return 0, false
			}
			ttl, found = time.Duration(secs)*time.Second, true
		case "s-maxage":
			secs, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil {
				return 0, false
			}
			shared, sharedFound = time.Duration(secs)*time.Second, true
		}
	}
	if sharedFound {
		return shared, true
	}
	if !found {
		return 0, false
	}
	return ttl, true
}

// hasCCDirective reports whether cc carries the named directive.
func hasCCDirective(cc, want string) bool {
	for name := range ccDirectives(cc) {
		if name == want {
			return true
		}
	}
	return false
}

// ccDirectives iterates a Cache-Control header as (lowercased name, argument)
// pairs; the argument is "" for a bare directive. One parse shape for every
// directive question this file asks.
func ccDirectives(cc string) func(func(string, string) bool) {
	return func(yield func(string, string) bool) {
		for _, part := range strings.Split(cc, ",") {
			name, arg, _ := strings.Cut(strings.TrimSpace(part), "=")
			if !yield(strings.ToLower(strings.TrimSpace(name)), arg) {
				return
			}
		}
	}
}

// writeCached replays a cached response verbatim onto w.
func writeCached(w http.ResponseWriter, resp *CachedResponse) {
	h := w.Header()
	for k, v := range resp.Header {
		h[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(resp.Body)
}

// recordingResponseWriter captures a response as it's written so it can be
// cached afterward, while still passing bytes through to the real client
// immediately (no buffering delay for the requester). Recording stops
// (silently, not an error) once the body exceeds maxBody — that response is
// simply never cached, per maybeStore's rec.truncated check.
type recordingResponseWriter struct {
	http.ResponseWriter
	request   *http.Request
	cacheable bool
	maxBody   int
	status    int
	body      []byte
	truncated bool
	wroteHdr  bool
}

func (r *recordingResponseWriter) WriteHeader(status int) {
	// 1xx responses (103 Early Hints, forwarded by httputil.ReverseProxy)
	// precede the final status; latching one would record it as THE status
	// and maybeStore's 2xx check would then refuse the real response that
	// follows. Pass them through and keep waiting for the final one.
	if status < 200 {
		r.ResponseWriter.WriteHeader(status)
		return
	}
	if !r.wroteHdr {
		r.status = status
		r.wroteHdr = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recordingResponseWriter) Write(p []byte) (int, error) {
	if !r.wroteHdr {
		r.WriteHeader(http.StatusOK)
	}
	if r.cacheable && !r.truncated {
		if len(r.body)+len(p) > r.maxBody {
			r.truncated = true
			r.body = nil
		} else {
			r.body = append(r.body, p...)
		}
	}
	return r.ResponseWriter.Write(p)
}

// Hijack forwards to the underlying ResponseWriter's http.Hijacker, when it
// implements one — the same optional-interface pattern the stdlib itself
// uses. ServeHTTP special-cases upgrade requests before ever constructing a
// recordingResponseWriter (see isUpgradeRequest), so this method is not
// expected to be called on the hot path today; it exists so the wrapper type
// is never itself the reason a hijack fails, matching http.ResponseWriter
// wrapper conventions generally.
func (r *recordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("edge: underlying ResponseWriter does not support http.Hijacker")
	}
	return hj.Hijack()
}

// Flush forwards to the underlying ResponseWriter's http.Flusher, when it
// implements one — needed so a recorded (non-upgrade) response can still
// stream incrementally to the real client, e.g. Server-Sent Events, instead
// of buffering until the handler returns.
func (r *recordingResponseWriter) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// unknownHost answers a request for a host we don't serve: a redirect to the
// fallback (302 — a typo'd host must not be cached as permanently moved), or
// the classic 404 when no fallback is configured.
//
// A host under one of this deployment's own domains is answered with the
// pending page instead (pending.go): it may be a host created a moment ago
// that this edge has not heard about yet, and a dead end for it strands the
// visitor where a retry would have worked.
func (e *Edge) unknownHost(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if e.underPlatformDomain(host) {
		e.pendingHost(w, r)
		return
	}
	if e.fallbackURL != "" {
		http.Redirect(w, r, e.fallbackURL, http.StatusFound)
		return
	}
	http.Error(w, "unknown host", http.StatusNotFound)
}

// httpFallbackHandler is the non-ACME :80 handler passed to
// autocert.Manager.HTTPHandler (RunWithActivatorReady): known hosts get the
// classic redirect to https (GET/HEAD only, 302); unknown hosts go to the
// fallback URL instead of a redirect into a doomed TLS handshake (no cert
// will ever exist for them).
//
// This gate uses lookupUpstream (and, since the backhaul, the home map), NOT
// Allowed: "does something answer this host on 443?" is a ROUTING question
// (lookupUpstream's job, including its one-label wildcard-alias fallback, plus
// the deployment's other edges), not an issuance question (Allowed's job,
// deliberately exact-only — see its own doc comment). A wildcard-alias
// host like paul.woodstar.app resolves fine on 443 via the wildcard cert, so
// plain http:// must redirect it rather than 404. This is safe to widen past
// Allowed's exact match because a redirect grants nothing: it's a 302 to the
// SAME host on 443, never a new ACME order — the attacker-supplied-label
// hazard that keeps Allowed narrow does not apply here.
//
// The ACME HTTP-01 challenge path is untouched by this: m.HTTPHandler (the
// caller) checks the /.well-known/acme-challenge/ prefix — and runs its OWN
// hostPolicy, which IS Allowed — before ever falling through to this
// handler, so a challenge request never reaches lookupUpstream at all.
func (e *Edge) httpFallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// The steering hostname answers here too (ServeHTTP's own first
		// branch, and the same reasoning): the health check is HTTPS today,
		// but a :80 probe — or an operator's curl — must not be handed this
		// edge's fallback redirect for a name that is deployment
		// infrastructure.
		if e.steeringProbe(w, r, host) {
			return
		}
		// The reserved tunnel hostname is answered by the hub itself on 443
		// (ServeHTTP, and Allowed's same check for issuance) but is not a
		// registry route, so lookupUpstream alone never sees it — without this,
		// plain http://<tunnel-host>/ fell through to unknownHost instead of
		// redirecting like every other routable host.
		routable := e.tunnels != nil && host == e.tunnels.Host()
		if !routable {
			_, routable = e.lookupUpstream(host)
		}
		if !routable {
			// A host this box does not serve but another box HOMES: 443
			// backhauls it (the hop, edge.go's backhaul), so :80 must send the
			// visitor there rather than 404 an app that is up — a first-time
			// visitor typing a bare hostname lands on whichever edge DNS
			// picked, which in a steered deployment is routinely not the home.
			// Home() is the same wildcard-capable lookup the dial uses, is
			// nil-safe (no deployment store ⇒ no answer), and excludes this
			// box's own hosts, so a single-box deployment is untouched.
			//
			// No hop logic here on purpose: the hop dials 443, so a verified
			// hop can never arrive on this handler at all.
			_, _, routable = e.homes.Load().Home(host)
		}
		if !routable {
			e.unknownHost(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Use HTTPS", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusFound)
	})
}

// Watch polls the registry for writes from other processes (`carlos add`
// etc., without needing rights to signal us) every 2s and reloads on change.
// It polls a cheap fingerprint of the routes table rather than PRAGMA
// data_version: the pragma proved blind to cross-process commits on Eleven's
// production box (a modernc/sqlite WAL-visibility quirk under long-lived
// connections). registry.Fingerprint opens a FRESH connection per poll — a
// brand-new connection reads the latest committed state by construction
// (see registry.List's comment for the stale-WAL-snapshot history). Opening
// SQLite costs about a millisecond; at one poll every two seconds that is
// nothing. Returns when stop closes; SIGHUP-forced reloads live in Run.
func (e *Edge) Watch(stop <-chan struct{}) {
	log.Printf("edge: registry watcher armed (2s fingerprint poll)")
	// No baseline-only first tick: the first observed fingerprint also
	// triggers a reload, so a route added between the edge's startup reload
	// and this poll can't be silently baselined away. (Eleven's version has
	// that gap; one redundant reload at startup is the cost of closing it.)
	var last string
	errs := 0
	reloadErrs := 0
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		fp, err := e.reg.Fingerprint()
		if err != nil {
			errs++
			if errs == 1 || errs%300 == 0 {
				log.Printf("edge: registry-watch poll error (#%d): %v", errs, err)
			}
			continue
		}
		errs = 0
		if fp == last {
			continue
		}
		// last is baselined only on a SUCCESSFUL reload: a fleet detach is a
		// revocation, and if Reload fails on this tick, the fingerprint it
		// was meant to apply must not be discarded — baselining it here
		// (before Reload runs) would make the change invisible to every
		// later tick until some UNRELATED write moves the fingerprint again,
		// silently sitting on a revocation that never took effect. Leaving
		// last unchanged means the very next tick sees the same fp != last
		// and retries.
		if err := e.Reload(); err != nil {
			reloadErrs++
			if reloadErrs == 1 || reloadErrs%300 == 0 {
				log.Printf("edge: registry-watch reload failed (#%d): %v", reloadErrs, err)
			}
			continue
		}
		reloadErrs = 0
		last = fp
		log.Printf("edge: registry changed — reloaded (%d route(s))", len(e.current()))
	}
}

// versionHeader wraps next so every response also carries
// X-Carlos-Version: ver — the deploy watch's proof (carlos-deploy spec §7)
// that this route is serving the version it was told to. It wraps the
// finished handler rather than reaching inside it, so it never interferes
// with a ReverseProxy's ModifyResponse purge-header hook or the activator's
// dispatch — both already ran (or will run inside next) by the time this
// sets the header.
func versionHeader(ver string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Carlos-Version", ver)
		next.ServeHTTP(w, r)
	})
}

// UnixProxy is unixProxy for callers outside this package — the scheduled-work
// runner in internal/host, which delivers a tick to a route's own socket and
// must NOT take it through the edge's cache, compression or usage layers: a
// tick is platform traffic, not a visitor's request.
func UnixProxy(socketPath string) http.Handler { return unixProxy(socketPath) }

// unixProxy reverse-proxies to a backend listening on a unix socket,
// preserving the client's Host header (so the backend sees the real hostname)
// and passing WebSocket upgrades straight through.
func unixProxy(socketPath string) *httputil.ReverseProxy {
	dialer := &net.Dialer{}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			setForwarded(pr)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "carlos" // placeholder; the unix dialer ignores it
			pr.Out.Host = pr.In.Host   // backend sees the real hostname
			scrubInboundPurgeHeader(pr)
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialUnixWithRetry(ctx, dialer, socketPath)
			},
			// Bound idle keep-alives so proxies replaced on reload don't leak
			// their pooled connections + reader goroutines on the long-running
			// edge.
			MaxIdleConns:    32,
			IdleConnTimeout: 90 * time.Second,
		},
		ErrorHandler: defaultProxyError(proxyCause),
	}
}

// tcpProxy reverse-proxies to a TCP upstream at http://addr with the same
// Host preservation and idle bounds as unixProxy — this is what lets
// whole-app tenants sit behind the edge without unix-socket support.
func tcpProxy(addr string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			setForwarded(pr)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = addr
			pr.Out.Host = pr.In.Host // backend sees the real hostname
			scrubInboundPurgeHeader(pr)
		},
		Transport: &http.Transport{
			MaxIdleConns:    32,
			IdleConnTimeout: 90 * time.Second,
		},
		ErrorHandler: defaultProxyError(proxyCause),
	}
}

// errNoLiveUpstream is the sentinel multiProxy's dialer returns when a probed
// route has nothing left to try — an empty live set, or every live upstream
// refusing the dial inside this one request. multiProxy's ErrorHandler turns
// it into 503 + Retry-After: 2, the same "nobody is home right now, come back
// shortly" answer errFleetDark gets, rather than the ReverseProxy default's
// 502 (which reads as "your backend is broken" and is what an UNPROBED addr
// route still correctly returns).
//
// It never escapes the route it came from: each multiProxy has its own
// ErrorHandler, and the only thing that can produce this error is that
// proxy's own DialContext.
var errNoLiveUpstream = errors.New("edge: no live upstream")

// multiDialHost is the placeholder authority multiProxy's Rewrite puts on the
// outbound URL. Nothing ever resolves it: DialContext ignores the address
// entirely and dials the live set instead. `.invalid` is the reserved TLD for
// exactly this (RFC 2606), so if it ever DID reach a resolver — a wiring
// mistake — it fails immediately and visibly rather than hitting somebody's
// wildcard DNS.
const multiDialHost = "multi.invalid"

// multiDialTimeout bounds ONE dial attempt. Per attempt, not per request: a
// route with three upstreams whose first two are black-holed must still reach
// the third, and a single request-wide budget would spend it all on the first
// dead box. Three seconds is long enough for a loaded box on a real network to
// complete a handshake and short enough that a fully dark route answers its
// 503 well inside any sane client timeout.
const multiDialTimeout = 3 * time.Second

// multiProxy reverse-proxies a probed addr route across its live upstreams
// (prober.go). Same Host preservation and purge-header discipline as tcpProxy;
// what it adds is where the bytes go.
//
// It takes the host and not the route's addr list because the addr list is not
// its input: the prober owns it (Reload hands the same list to Sync), and the
// only set this proxy may dial is the LIVE one. Closing over the configured
// addrs as well would create a second, staler copy of the route's upstreams
// and invite exactly the fallback the design forbids — dialing a box the probe
// has already declared dead.
//
// Selection happens in DialContext, not in Rewrite, and that is the whole
// design. A dial is the last moment at which NOTHING has been sent: the
// request body is untouched and no response has begun, so a refused upstream
// can be ejected and the next one tried inside the same request, with the
// client none the wiser. Retrying one layer up (at RoundTrip) would mean
// replaying a request that may already have had its body consumed, which is
// only safe for a subset of requests — this way it is safe for all of them.
//
// The live set is read PER REQUEST rather than captured, mirroring
// fleetProxy/tunnelProxy: an upstream that fails away (or rejoins) between two
// requests is picked up with no Reload needed.
//
// Keep-alives to upstreams are off, which is a real cost paid for two real
// properties. The stdlib pools connections by the outbound URL's authority,
// and every request through this proxy carries the same placeholder one — so a
// pooled connection would be reused regardless of WHICH upstream it goes to.
// That would (a) pin a route to whichever upstream happened to answer first,
// making the round-robin below decorative, and (b) survive an Eject: the
// upstream the prober just took out of rotation would keep serving traffic
// over the connection already in the pool. Correct failover is worth a TCP
// handshake per request to a backend that is, in every deployment this exists
// for, on the same network. The unprobed tcpProxy path keeps its pool
// untouched.
func multiProxy(host string, p *proberSet) *httputil.ReverseProxy {
	// Per-ROUTE cursor: closed over by this proxy alone, so two routes cannot
	// interleave their rotations (a global counter would make a two-upstream
	// route's balance depend on the request rate of every other route on the
	// box). Reload builds a new proxy, so the cursor resets with the route's
	// upstream set — which is what you want, since the set it was counting
	// over no longer exists.
	var cursor atomic.Uint64
	dialer := &net.Dialer{Timeout: multiDialTimeout}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			setForwarded(pr)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = multiDialHost // placeholder; the dialer below ignores it
			pr.Out.Host = pr.In.Host        // backend sees the real hostname
			scrubInboundPurgeHeader(pr)
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				live := p.Live(host)
				if len(live) == 0 {
					return nil, errNoLiveUpstream
				}
				// Start where the last request left off, then walk the whole
				// snapshot once. Snapshot rather than re-reading Live per
				// attempt: each Eject below shrinks the live set, and re-reading
				// would skip untried upstreams as the list moved under us.
				//
				// The index arithmetic stays in uint64 all the way to the
				// modulo. Converting the counter to int first would wrap
				// NEGATIVE once it passed 2^31 on a 32-bit box (the Pi fleet),
				// and Go's modulo keeps the sign — so the very next request
				// after that wrap would index out of range and panic the edge.
				start := cursor.Add(1) - 1
				n := uint64(len(live))
				var lastErr error
				for i := range live {
					if err := ctx.Err(); err != nil {
						// The client went away (or the request deadline
						// expired) mid-loop. Stop, and do NOT eject: a dial we
						// abandoned says nothing about the upstream's health,
						// and marking it down here would take a healthy box out
						// of rotation because one user hit stop.
						return nil, err
					}
					addr := live[(start+uint64(i))%n]
					conn, err := dialer.DialContext(ctx, network, addr)
					if err == nil {
						return conn, nil
					}
					lastErr = err
					if ctx.Err() != nil {
						return nil, err // as above: our own cancellation, not the upstream's fault
					}
					// The fast path the probe cadence cannot match: a box that
					// died since the last tick is out of rotation now, for
					// every other in-flight request too, instead of collecting
					// failures until fail_after.
					p.Eject(host, addr)
				}
				return nil, fmt.Errorf("%w for %s: %d upstream(s) refused, last: %v",
					errNoLiveUpstream, host, len(live), lastErr)
			},
			// No MaxIdleConns/IdleConnTimeout twin of tcpProxy's: with
			// keep-alives off nothing is ever pooled, so the idle bounds that
			// stop a replaced proxy leaking pooled connections have nothing to
			// bound. Every connection this transport opens belongs to exactly
			// one request and closes with it.
			DisableKeepAlives: true, // see the pooling note above
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, errNoLiveUpstream) {
				noteCause(r, apperr.CauseNoUpstream)
				w.Header().Set("Retry-After", "2")
				http.Error(w, "no healthy upstream", http.StatusServiceUnavailable)
				return
			}
			// Anything else is a real proxy failure (upstream hung up
			// mid-response, malformed reply): same shape as
			// httputil.ReverseProxy's own default handler, log and 502.
			noteCause(r, proxyCause(r, err))
			log.Printf("edge: multi-upstream %s proxy error: %v", host, err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

// tunnelProxy reverse-proxies over a named reverse channel (TunnelHub).
// Same Host preservation and purge-header discipline as tcpProxy — a
// tunnel-backed host is exactly as untrusted as an addr-backed one. The
// scheme is https because the h2 ClientConn requires it; no dial happens
// (the channel already exists), so the value is otherwise inert. No channel
// is resolved here either: hub.transportFor looks it up per request, so a
// proxy built before a reconnect uses the fresh channel afterwards, and a
// disconnected one fails fast into the stdlib proxy's 502 rather than hanging.
func tunnelProxy(name string, hub *TunnelHub) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			setForwarded(pr)
			pr.Out.URL.Scheme = "https"
			pr.Out.URL.Host = pr.In.Host // h2 :authority = the real hostname
			pr.Out.Host = pr.In.Host
			scrubInboundPurgeHeader(pr)
		},
		Transport:    hub.transportFor(name),
		ErrorHandler: defaultProxyError(tunnelCause),
	}
}

// errFleetDark is the sentinel fleetProxy's Transport returns when fleet has
// no connected channel at all — neither the route's bound box nor any
// fallback. fleetProxy's ErrorHandler recognizes it and answers a clean 503
// with Retry-After: 2 instead of the ReverseProxy default (a 502 with a
// "context deadline exceeded"-flavored body that reads like a broken backend
// rather than "no box is home right now, try again shortly").
var errFleetDark = errors.New("edge: fleet has no connected channel")

// fleetChannelFor picks which channel name to dial for one request against a
// fleet-placed route, per the brief's binding/fallback rule:
//
//  1. boxLabel (the route's bound box, Route.FleetBox) if its channel
//     "<fleet>/<boxLabel>" is connected.
//  2. else the first name in ConnectedOfFleet(fleet)'s sorted order — ANY
//     connected box of the fleet. That box's own activator wakes its
//     instance locally; a bound-but-different box racing it awake is the
//     lease's job to arbitrate, not this edge's.
//  3. else "" — the fleet is dark.
//
// boxLabel may be "" (a route that wants a fleet but has no box assigned
// yet), which just skips straight to the fallback step.
func fleetChannelFor(fleet, boxLabel string, hub *TunnelHub) string {
	if boxLabel != "" {
		if bound := fleet + "/" + boxLabel; hub.State(bound) == "connected" {
			return bound
		}
	}
	if names := hub.ConnectedOfFleet(fleet); len(names) > 0 {
		return names[0]
	}
	return ""
}

// fleetProxy reverse-proxies a fleet-placed route (Region == "fleet/<fleet>",
// registry.FleetPlacement) over whichever channel fleetChannelFor picks —
// resolved PER REQUEST, mirroring tunnelProxy/transportFor above, so a bound
// box reconnecting (or a fallback box's channel dropping mid-outage) between
// two requests is picked up with no Reload needed. Same Host preservation
// and purge-header discipline as tunnelProxy; the only difference is the
// no-channel-at-all case, which answers 503 + Retry-After via ErrorHandler
// instead of falling into ReverseProxy's default 502 (see errFleetDark).
func fleetProxy(fleet, boxLabel string, hub *TunnelHub) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			setForwarded(pr)
			pr.Out.URL.Scheme = "https"
			pr.Out.URL.Host = pr.In.Host // h2 :authority = the real hostname
			pr.Out.Host = pr.In.Host
			scrubInboundPurgeHeader(pr)
		},
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			name := fleetChannelFor(fleet, boxLabel, hub)
			if name == "" {
				return nil, errFleetDark
			}
			return hub.transportFor(name).RoundTrip(req)
		}),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, errFleetDark) {
				noteCause(r, apperr.CauseFleetDark)
				w.Header().Set("Retry-After", "2")
				http.Error(w, "fleet unreachable: no connected box", http.StatusServiceUnavailable)
				return
			}
			// Any other transport failure (e.g. the picked channel died between
			// fleetChannelFor's check and the RoundTrip call) — same shape as
			// httputil.ReverseProxy's own default handler: log and 502.
			noteCause(r, tunnelCause(r, err))
			log.Printf("edge: fleet %s proxy error: %v", fleet, err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

// ---- backhaul: the SENDING half of one-hop edge-to-edge forwarding ----------
//
// homemap.go holds the map (which OTHER box homes a host) and the RECEIVING
// half (what proves an inbound hop, and what a stale one is answered with).
// This is the other end: a request that landed on this edge for a host homed
// elsewhere is forwarded to that box's edge — over ordinary, publicly-verified
// TLS, exactly once — instead of dead-ending in the unknown-host answer.
//
// Three things are deliberately NOT done here, and each is a decision rather
// than an omission:
//
//   - NO METERING. The one usage meter (ServeHTTP, above) sits BELOW a
//     successful lookupUpstream, so a backhauled request misses it here by
//     construction while the HOME edge — which does serve the route — records
//     the request and its bytes exactly once. A second Record at this site
//     would double-count every backhauled request against the tenant. What
//     that leaves genuinely open is EGRESS ATTRIBUTION: the bytes cross the
//     internet twice and the second leg is billed to this box's account while
//     the meter runs on the home box's route. That is a unit-economics
//     question — the plan's Task 7 ruling — and not one to guess at from
//     inside a proxy.
//
//   - NO CACHING AND NO COMPRESSION. This branch returns before ServeHTTP's
//     cache and gzip wrapping, so a backhauled response is neither stored nor
//     re-encoded here. That is load-bearing, not lazy: a tenant's Carlos-Purge
//     is honoured and stripped by the HOME edge's purgeModifier
//     (purge_header.go), so a copy cached on THIS box could never be
//     invalidated by the app that owns it. The home edge's own cache and
//     compression still apply and reach the client through the hop — as does
//     X-Carlos-Version, which the home edge stamps from the route's own row
//     (versionHeader is per-route, and this box has no row for the host), so
//     `carlos deploy`'s verification works through a backhaul with nothing
//     added here.
//
//   - NO RETRY, AND NO SECOND TARGET. Home() answers with one box or none.
//     A failed dial is a 502 (see backhaulProxy's ErrorHandler), not a
//     scan for somewhere else to try: the process backing a home-boxed route
//     exists on exactly ONE box, so there is no second copy a retry could
//     reach.
//
// Single-box inertness: with one platform box Home() never offers a target
// (own-box exclusion, homemap.go's foldHomes), so e.backhaul returns false on
// every request and this whole path is dead code until a second box publishes
// its edges file.

// backhaulMaxInflight is the containment ruling of 2026-08-24: at most this
// many backhauled requests may be in flight toward any ONE home box at a
// time, counted on the SENDING edge. At the limit the sender answers 503 +
// Retry-After: 2 (backhaul, below) rather than dialling.
//
// A concurrency bound and not a rate: cross-region egress is (concurrent
// streams × per-stream bandwidth), so a request-rate cap would neither bound
// a handful of long-lived streams — a hop carries WebSockets and has no
// whole-body deadline by design — nor tolerate a legitimate burst. This
// degrades the other way round: short requests flow at full rate forever, and
// only slow, sustained streams pile into the cap.
//
// Sender-side because every backhauled byte traverses BOTH legs (home →
// sender, sender → client), so one cap here bounds both boxes' egress at
// once; the home box needs no twin, its inbound hops being bounded by the sum
// of its siblings' caps.
//
// A var, not a const, so a test can shorten it — the same reason backhaulPort
// is one. There is deliberately no "off" value: an unbounded cross-region
// path is the thing this closes.
//
// Atomic rather than a plain int because it is READ on the request path by
// every backhauling goroutine: it is written once today, before any listener
// binds, but a future env re-read (or a second Edge composed in one process)
// would turn a plain int into a real data race rather than a stale read.
var backhaulMaxInflight atomic.Int64

const defaultBackhaulMaxInflight = 64

func init() { backhaulMaxInflight.Store(defaultBackhaulMaxInflight) }

// backhaulInflightFromEnv reads CARLOS_BACKHAUL_MAX_INFLIGHT. Unset,
// unparseable or ≤ 0 all mean the default: a typo'd knob must not silently
// uncap (or, worse, zero-cap) a deployment's backhaul.
func backhaulInflightFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("CARLOS_BACKHAUL_MAX_INFLIGHT"))
	if raw == "" {
		return defaultBackhaulMaxInflight
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Printf("edge: CARLOS_BACKHAUL_MAX_INFLIGHT=%q is not a positive integer — using the default of %d concurrent backhauled requests per home box", raw, defaultBackhaulMaxInflight)
		return defaultBackhaulMaxInflight
	}
	return n
}

// backhaulAcquire reserves one in-flight slot toward box, or reports
// saturation. releaseSlot must be called exactly once when the hop's
// ServeHTTP returns — a streaming or upgraded response holds its slot for the
// life of the stream, deliberately: a long-lived stream IS sustained
// cross-region egress, which is the thing being bounded.
//
// The counter is per home BOX and the map is never keyed by anything a
// stranger controls (backhaulInflight's own doc). The count is incremented
// FIRST and rolled back on refusal, so the cap can never be exceeded: two
// requests racing at the limit both see the excess and both refuse, and a
// request arriving inside another's rollback window can be refused while a
// slot is in fact free. Both errors are in the safe direction — a refusal is
// a 503 the client retries in two seconds, where an over-admission would be
// cross-region bytes the cap exists to prevent.
func (e *Edge) backhaulAcquire(box string) (releaseSlot func(), ok bool) {
	// Load first, like backhaulTransport: LoadOrStore would allocate a
	// counter per backhauled request just to throw it away on the hit path.
	v, hit := e.backhaulInflight.Load(box)
	if !hit {
		v, _ = e.backhaulInflight.LoadOrStore(box, new(atomic.Int64))
	}
	n := v.(*atomic.Int64)
	if n.Add(1) > backhaulMaxInflight.Load() {
		n.Add(-1)
		return nil, false
	}
	// Once-guarded because a slot released twice would let the cap drift
	// upwards for the life of the process — the one failure mode of a
	// hand-released semaphore, and cheaper to make impossible than to audit.
	var once sync.Once
	return func() { once.Do(func() { n.Add(-1) }) }, true
}

// backhaulPort is the port a home edge answers on. Always 443 in production —
// an edges file publishes a bare EIP (Task 1) and every edge's front door is
// 443 — and a var only so a test can point the dial at an httptest server, the
// same reason homeMapInterval is one.
var backhaulPort = "443"

// backhaulRootCAs is the trust anchor set the hop's TLS handshake verifies the
// home edge's certificate against. nil — the system trust store, standard
// verification — is the production answer and the only one: the home edge
// presents the SAME publicly-trusted certificate it presents to any browser
// (Part 2's bucket-distributed certs), so a hop needs no private CA, no
// pinning, and above all no skipped verification. A var only so a test can
// trust a fake home edge's self-signed cert.
//
// The bootstrap window this implies is stated rather than papered over: a box
// that has just joined and whose cert for a host has not yet been distributed
// presents something the dialer will not verify, so the hop fails the
// handshake and the client gets a 502 until the cert mirror catches up. That
// is the correct failure — the alternative, dialing without verification,
// would make the hop's TLS decorative — and it heals on its own within a
// mirror poll.
var backhaulRootCAs *x509.CertPool

// The hop's timeouts, per the plan. Deliberately no whole-body deadline: a
// backhauled response may be a stream or an upgraded WebSocket that is
// supposed to stay open for hours (the fleetapi client's ruling, same
// reasoning), so the only bounds are on getting connected and on the home edge
// beginning to answer.
const (
	backhaulDialTimeout   = 5 * time.Second
	backhaulHeaderTimeout = 30 * time.Second
)

// backhaul forwards a request for a host this box does not serve to the box
// that does, and reports whether it answered. false means "not mine to
// forward" and the caller falls through to the unchanged unknown-host answer:
// either no box homes the host, or nothing could prove the hop.
//
// The hopVerified guard is FIRST, before the map is even consulted. ServeHTTP
// already refuses an arrived hop above this call (that is the structural half
// of "one hop, ever"), so this line is unreachable today — which is exactly
// why it is here: if this call is ever moved, or that branch reordered, a hop
// that arrived from a sibling edge fails closed with the stale-map refusal
// instead of being forwarded onward. A forwarding loop must take TWO mistakes,
// not one.
func (e *Edge) backhaul(w http.ResponseWriter, r *http.Request, host string) bool {
	m := e.homes.Load()
	if hopVerified(r.Context()) {
		m.staleHopRefusal(w, host)
		return true
	}
	eip, box, ok := m.Home(host)
	if !ok {
		return false
	}
	// Sign with the mirror's cached pair, .next preferred (signingKey, mesh.go)
	// — never a store read on the request path. NO KEY MEANS NO DIAL: signHop
	// would happily HMAC under an empty key, the home edge would refuse the
	// result, and the request would have crossed a region to be served as an
	// ordinary client with its forwarding chain stripped. Falling through to
	// the unknown-host answer is both cheaper and honest about the deployment
	// being mid-bootstrap.
	key := signingKey(m.meshKeys())
	if len(key) == 0 {
		e.logThrottled("backhaul-no-mesh-key", fmt.Sprintf("%s is homed on %s but no mesh key is mirrored — not dialling (the request gets the unknown-host answer until the key lands)", host, box))
		return false
	}
	// Containment (the egress ruling, 2026-08-24), between the last guard and
	// the dial: a saturated home box is answered here, with no dial and no
	// bytes crossing a region. 503 + Retry-After: 2 is this package's
	// established "come back shortly" (staleHopRefusal, fleetProxy) and is
	// deliberately NOT the unknown-host answer — that is a 302 on a fallback
	// edge, which would lie to a legitimate client about where its app lives.
	// The log key is per home BOX, like "backhaul-error "+box below and for
	// the same bounded-key reason.
	releaseSlot, ok := e.backhaulAcquire(box)
	if !ok {
		e.logThrottled("backhaul-saturated "+box,
			fmt.Sprintf("backhaul to %s (%s) is at its in-flight cap (%d) — answering 503 + Retry-After: 2", box, eip, backhaulMaxInflight.Load()))
		w.Header().Set("Retry-After", "2")
		http.Error(w, "temporarily busy", http.StatusServiceUnavailable)
		return true
	}
	// Held until the hop's ServeHTTP returns, which for a stream or an
	// upgraded connection is the life of the stream (backhaulAcquire).
	defer releaseSlot()
	// The MAC is over the CONCRETE request host — never the wildcard map key
	// that may have matched it — because the home edge verifies against its own
	// literal request host (hopMAC canonicalizes both, mesh.go).
	auth := signHop(key, host, time.Now())
	backhaulProxy(host, eip, box, auth, e.backhaulTransport(eip), e.logThrottled).ServeHTTP(w, r)
	return true
}

// backhaulTransport is the cached outbound transport for one home box. The
// TRANSPORT is what is cached and the ReverseProxy above is what is rebuilt
// per request: a proxy value is two closures, while a transport owns the
// connection pool, and a cross-region TCP+TLS handshake on every backhauled
// page is a real, visible cost.
//
// Keyed by EIP and NOT by host, which is both the cheaper and the safer half
// of that trade:
//
//   - Nothing about the connection is per-host. The dial goes to the box, and
//     the TLS ServerName is derived per request from the outbound URL's
//     authority — backhaulProxy sets it to the real hostname — so one
//     transport serves every host homed on one box while each still gets its
//     own SNI, its own certificate verification, and (the stdlib pools by that
//     same authority) its own pooled connections.
//   - Per host, this map would be unbounded. A catch-all row in another box's
//     edges file homes every subdomain under it, so a stranger asking for a
//     stream of invented hostnames would mint a transport apiece. Per box it
//     is bounded by the deployment, and the pooled connections inside it are
//     bounded by MaxIdleConns and IdleConnTimeout as usual.
//
// A host whose home MOVES therefore lands on the new box's transport, with the
// old box's pooled connections left to idle out — no equivalent of the pinning
// multiProxy pays DisableKeepAlives to avoid, because the authority a
// connection is pooled under names the host and the transport it lives in
// names the box.
//
// HTTP/2 is not attempted, and that is on purpose: ForceAttemptHTTP2 stays
// false (the stdlib disables the automatic upgrade anyway once TLSClientConfig
// or a custom dialer is set), so the hop speaks HTTP/1.1 and
// httputil.ReverseProxy's protocol-switch path — the one that carries a
// WebSocket through — works exactly as it does for every other proxy in this
// file.
func (e *Edge) backhaulTransport(eip string) *http.Transport {
	if tr, ok := e.backhaulTransports.Load(eip); ok {
		return tr.(*http.Transport)
	}
	dialer := &net.Dialer{Timeout: backhaulDialTimeout}
	tr := &http.Transport{
		// The address is ignored, multiProxy-style: it is the outbound URL's
		// authority, which is the HOST (for SNI, verification and pooling) and
		// never where the bytes go. The home edge is at its EIP.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(eip, backhaulPort))
		},
		// No ServerName: the stdlib fills it from the outbound URL's authority,
		// which is exactly the host being asked for. RootCAs nil is the system
		// trust store — standard verification, see backhaulRootCAs.
		TLSClientConfig: &tls.Config{
			RootCAs:    backhaulRootCAs,
			MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout:   backhaulDialTimeout,
		ResponseHeaderTimeout: backhaulHeaderTimeout,
		// PER-HOST as well as in total, which is where this diverges from
		// unixProxy/tcpProxy's bare MaxIdleConns: their pool key is one constant
		// localhost authority, so the stdlib default of 2 idle conns per host is
		// really 2 per BACKEND. Here the key is the hostname, so the default
		// would cap each backhauled host at 2 — and the third concurrent request
		// to a busy host would pay the cross-region TCP+TLS handshake this cache
		// exists to avoid. Equal to MaxIdleConns so the total is the only bound.
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}
	// Two requests for a box nobody has dialled yet can both build one; the
	// loser's transport is simply dropped, having opened nothing and started no
	// goroutine.
	if existing, loaded := e.backhaulTransports.LoadOrStore(eip, tr); loaded {
		return existing.(*http.Transport)
	}
	return tr
}

// backhaulProxy is the hop itself: an ordinary reverse proxy to the home box,
// carrying the client's Host through unchanged so the home edge routes on the
// real hostname, and carrying the two hop headers that make it a hop rather
// than an anonymous request from a stranger with a data-centre IP.
//
// The outbound URL names the HOST, not the EIP, and the transport's dialer is
// what turns that into bytes at the home box (backhaulTransport). The
// authority is therefore doing three jobs at once and all three want the
// hostname: the TLS ServerName the home edge selects a certificate by, the
// name that certificate is verified against, and the key the connection is
// pooled under.
//
// It takes the signature ready-made instead of minting one inside Rewrite, so
// that the "no key, no dial" decision (backhaul, above) and the signing happen
// at the SAME read of the mirror's key pair — a Rewrite that signed for itself
// could still reach for a key that had gone away since the guard.
//
// X-Forwarded-For is setForwarded's single-element chain and nothing more: the
// client that dialled THIS edge, with neither this edge's own address nor the
// home EIP appended. That is the contract homemap.go's setForwarded documents
// from the receiving end — the home edge restores this chain verbatim on a
// verified hop, so the app's last XFF element is the real client, exactly as
// it would be if the request had landed on the home box directly. (The restore
// branch cannot fire on THIS side: backhaul only runs when the request did not
// arrive as a verified hop.)
//
// logThrottled is e.logThrottled, threaded through rather than closed over
// directly: this is a free function (built fresh per request, unlike the
// cached backhaulTransport) so it takes no *Edge, and a dial failure is a
// STANDING condition for as long as a home box is unreachable — an
// unthrottled log.Printf here would fill the journal with one line per
// failed request for the whole outage. Keyed "backhaul-error "+box (per home
// box, not per host — a wildcard home behind one unreachable box would
// otherwise mint a fresh throttle bucket per invented hostname).
func backhaulProxy(host, eip, box, auth string, tr http.RoundTripper, logThrottled func(key, msg string)) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			setForwarded(pr)
			pr.Out.URL.Scheme = "https"
			pr.Out.URL.Host = host   // SNI + verification + pool key; the dial is the transport's
			pr.Out.Host = pr.In.Host // the home edge routes on the real hostname
			pr.Out.Header.Set(hopHeader, "1")
			pr.Out.Header.Set(hopAuthHeader, auth)
			// The same discipline every other Rewrite here keeps, and needed for
			// the same reason one hop further along: the home edge scrubs it
			// again before the app, but a header this edge forwards is a header
			// this edge vouched for.
			scrubInboundPurgeHeader(pr)
		},
		Transport: tr,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// 502, not the 503 + Retry-After that multiProxy and fleetProxy
			// answer with: those two KNOW nobody is home (an empty live set, no
			// connected channel), while this one believed a specific box was
			// there and could not reach it. A failed dial, a TLS handshake
			// against a cert the mirror has not distributed yet, or a home edge
			// that hung up mid-response are all genuinely bad gateways — and the
			// cert case heals itself on the next mirror poll.
			logThrottled("backhaul-error "+box, fmt.Sprintf("backhaul %s → %s (%s): %v", host, box, eip, err))
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

// dialUnixWithRetry masks the brief gap during an instance restart: while the
// old process has exited and the new one hasn't rebound the socket, dials fail
// with ENOENT/ECONNREFUSED. Retry those for a short window so no in-flight
// request is dropped; any other error fails fast. The window must exceed a
// deploy's socket-absent gap — the instance drain (10s) plus rebind — so an
// in-flight request is never 502'd mid-deploy; a sustained-down backend hangs
// this long before 502 (rare: systemd Restart=always makes "down" transient).
func dialUnixWithRetry(ctx context.Context, d *net.Dialer, socketPath string) (net.Conn, error) {
	const window = 13 * time.Second
	deadline := time.Now().Add(window)
	for {
		conn, err := unixdial.Checked(ctx, d, socketPath)
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil || !isRetryableDialErr(err) || time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func isRetryableDialErr(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}

// Run is the `carlos edge` verb: the shared front door for every instance,
// sibling service, and static site on the box.
func Run(args []string) {
	RunWithActivator(args, nil, NewCache(DefaultCacheMaxEntries, DefaultCacheMaxBodyBytes))
}

// RunWithActivator is Run with an injected hibernation activator — the seam
// `carlos agent` (internal/host) uses to compose edge + activator in one
// process. act may be nil: pure proxy, exactly Run's behavior. cache may
// also be nil, disabling response caching entirely.
func RunWithActivator(args []string, act Activator, cache *Cache) {
	RunWithActivatorReady(args, act, cache, nil)
}

// RunWithActivatorReady is RunWithActivator plus a ready hook: ready (when
// non-nil) is called with the fully configured Edge — every EnableXxx done,
// tunnel hub armed if configured — immediately before the process blocks on
// its listeners. It is the seam an in-process caller uses to reach the live
// Edge (e.g. to read tunnel state for status reporting) without racing the
// configuration it needs. It runs on the calling goroutine, so a hook that
// blocks holds the front door shut: hand off to a goroutine and return.
//
// opts configure the Edge the moment it exists — before any EnableXxx, and
// long before the TLS listener — so a knob the composing caller owns
// (WithIssuerGate) is in place for the construction decisions that read it.
// The ready hook is the wrong home for those: it fires once, at the end, for
// state a caller wants to REACH.
func RunWithActivatorReady(args []string, act Activator, cache *Cache, ready func(*Edge), opts ...RunOption) {
	dataDir := envOr("CARLOS_DATA_DIR", "/data")
	fs := flag.NewFlagSet("edge", flag.ExitOnError)
	registryPath := fs.String("registry", filepath.Join(dataDir, "registry.db"), "registry SQLite path (default $CARLOS_DATA_DIR/registry.db)")
	acmeCache := fs.String("acme-cache", filepath.Join(dataDir, "acme"), "autocert certificate cache dir (default $CARLOS_DATA_DIR/acme)")
	httpsAddr := fs.String("https", ":443", "HTTPS listen address")
	httpAddr := fs.String("http", ":80", "HTTP listen address (ACME HTTP-01 challenge + redirect to https; in --dev mode, the only listener)")
	fallbackURL := fs.String("fallback", "", "redirect requests for unknown hosts here, e.g. an apex marketing site; empty keeps the plain 404")
	dev := fs.Bool("dev", false, "plain-HTTP mode: serve routes on --http with no TLS/ACME (local development only)")
	fs.Usage = cliflag.Usage(fs, "")
	_ = fs.Parse(args)

	reg, err := registry.Open(*registryPath)
	if err != nil {
		log.Fatalf("edge: open registry %s: %v", *registryPath, err)
	}

	// act is nil for `carlos edge` — the pure-proxy role; `carlos agent`
	// injects the hibernation activator (edge + activator in one process).
	e, err := New(reg, act)
	if err != nil {
		log.Fatalf("edge: load routes: %v", err)
	}
	for _, opt := range opts {
		opt(e)
	}
	e.fallbackURL = *fallbackURL
	e.setPlatformDomains(platformDomainsFromEnv(os.Getenv))
	e.EnableCache(cache)

	// Static serving lights up when the environment carries a deployment
	// store and the release pubkey; otherwise static routes 404 until it does.
	if err := enableStaticFromEnv(e, dataDir); err != nil {
		log.Fatalf("edge: static serving: %v", err)
	}

	// Reverse channels light up when the environment names a reserved
	// hostname and at least one client; otherwise tunnel routes stay skipped.
	if err := enableTunnelsFromEnv(e); err != nil {
		log.Fatalf("edge: tunnels: %v", err)
	}

	// The backhaul in-flight cap, read from the environment once here rather
	// than per request (backhaulMaxInflight). Inert on a single-box
	// deployment, where nothing is ever backhauled at all.
	backhaulMaxInflight.Store(int64(backhaulInflightFromEnv()))

	go e.Watch(make(chan struct{})) // process-lifetime; never stopped

	// Reload routes on SIGHUP too (an operator's explicit "now").
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			if err := e.Reload(); err != nil {
				log.Printf("edge: reload failed: %v", err)
				continue
			}
			log.Printf("edge: reloaded (%d route(s))", len(e.current()))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	// The Edge is fully configured here — every EnableXxx applied and the
	// route table rebuilt behind them — and nothing is listening yet.
	if ready != nil {
		ready(e)
	}

	if *dev {
		// ReadHeaderTimeout guards against Slowloris-style slow-header
		// connection exhaustion. No ReadTimeout/WriteTimeout — those would
		// break long-lived proxied WebSockets.
		srv := &http.Server{Addr: *httpAddr, Handler: e, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			log.Printf("edge: DEV MODE — plain HTTP, serving %d route(s) on %s (registry %s)",
				len(e.current()), *httpAddr, *registryPath)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("edge: http listener %s: %v", *httpAddr, err)
			}
		}()
		<-stop
		log.Printf("edge: draining and shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return
	}

	certCache, err := newCertCache(*acmeCache)
	if err != nil {
		log.Fatalf("edge: %v", err)
	}
	// The RAW local cache, kept before the publishing wrapper goes on. The
	// mirror writes INWARDS through this handle and reads it as its last
	// serving resort; going through the wrapper instead would have the issuer
	// re-publishing its own material to the bucket on every poll.
	localCerts := certCache
	// Publish every issuance artifact to the control bucket, and gate
	// issuance on being the box that may write there (certsync.go): per-host
	// certs, the ACME account keys, HTTP-01 tokens, and — through the same
	// handle below — the DNS-01 wildcards. One call because it is one
	// decision: a box with no store to publish to also has no mirror to
	// serve from, so it stays ungated and keeps issuing for itself.
	certCache = e.armCertIssuance(certCache)

	// The other side of that decision (certsync.go): the mirror of
	// control/certs/, which is what makes the gate safe to shut. nil when
	// there is no deployment store — armCertIssuance has then cleared the
	// gate too, and this box issues and serves entirely for itself.
	mirror := e.newCertMirror(localCerts)
	if mirror != nil {
		// One synchronous pass before anything listens: a box that has just
		// booted must not spend its first 30 seconds handshaking with nothing,
		// and the wildcard manager's first pass (below) reads what this lands
		// in the local cache.
		ctx, cancel := context.WithTimeout(context.Background(), certMirrorBootTimeout)
		if err := mirror.Refresh(ctx); err != nil {
			log.Printf("edge: first cert mirror refresh failed: %v — serving from this box's own cache until a later poll lands", err)
		}
		cancel()
	}

	// The home map and the mesh keys (homemap.go), armed beside the cert
	// mirror and on the same terms: nil when there is no deployment store to
	// mirror from, one synchronous refresh before anything listens so the
	// first request does not meet an empty map, then the 30s loop. Stored
	// before either listener binds, so ServeHTTP never sees a half-armed edge.
	if hm := e.newHomeMap(); hm != nil {
		ctx, cancel := context.WithTimeout(context.Background(), homeMapBootTimeout)
		if err := hm.Refresh(ctx); err != nil {
			log.Printf("edge: first home map refresh failed: %v — hosts homed on another box are unreachable from here until a later poll lands", err)
		}
		cancel()
		e.homes.Store(hm)
		go hm.run(make(chan struct{})) // process-lifetime; never stopped, like the cert mirror's
	}

	m := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  certCache,
		// Registry membership AND the issuance gate — see issuerHostPolicy
		// (certsync.go) for why the gate cannot be relaxed for cached names,
		// and for why a transient double-holder is deliberately not fenced.
		HostPolicy: e.issuerHostPolicy(),
	}
	tlsCfg := m.TLSConfig()
	// The handshake chain, built inside out: autocert, then the mirror (which
	// also carries the direct local-cache last resort behind it), then the
	// wildcard. See certMirror.wrapGetCertificate for why the mirror sits in
	// FRONT of autocert rather than behind it.
	getCert := tlsCfg.GetCertificate
	if mirror != nil {
		getCert = mirror.wrapGetCertificate(getCert)
	}
	// The per-host path, captured before the wildcard wrap goes on top of it:
	// this is what the cert warm below calls. Calling the WRAPPED chain would
	// let the wildcard answer for a covered name and order nothing.
	perHostCert := getCert
	// Wildcard certs (wildcard.go, ACME DNS-01): covered names — one label
	// under a per-account or configured parent — are served the wildcard;
	// everything else (custom domains, canary hosts, parents not yet
	// obtained) stays on autocert's per-host HTTP-01 above. nil = not
	// configured = exactly the pre-wildcard edge.
	var wm *wildcardManager
	if w := newWildcardFromEnv(e, certCache); w != nil {
		wm = w
		getCert = wrapGetCertificate(w, getCert)
		e.OnReload(w.refresh)
		attachWildcardToMirror(mirror, w)
		// A remote box's edges file landing (or leaving) can add or drop a
		// parent (#300): re-derive on the home map's poll, not the 12h tick.
		e.homes.Load().setHostsWake(w.refresh)
		w.refresh()
		go w.run()
		log.Printf("edge: wildcard certs on (static %v, apps domain %q, delegation zone %q)",
			splitDomains(os.Getenv("CARLOS_WILDCARD_DOMAINS")), os.Getenv("PLATFORM_APPS_DOMAIN"), w.delegationZone)
	}
	// Cert warm (#299 stage 3, certwarm.go): the platform ORDERS and RENEWS
	// custom domains' certs instead of waiting for a visitor's handshake.
	//
	// It calls perHostCert — the chain as it stood BEFORE the wildcard wrap —
	// because the warm's set already excludes every wildcard-covered name,
	// and a wrapped call would be answered by the wildcard and order nothing.
	//
	// Armed only with a deployment store: an agent-less `carlos edge` is
	// ungated (issuerGate nil means yes), and would otherwise start placing
	// proactive ACME orders on a hobbyist's box.
	if e.staticStore != nil {
		appsDomain := strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_APPS_DOMAIN")))
		parents := func() []string { return nil }
		if wm != nil {
			parents = wm.currentParents
		}
		envIPs := splitDomains(os.Getenv("PLATFORM_EDGE_IPS"))
		store := e.staticStore
		edgeIPs := func() []string {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return edgesfile.EdgeIPs(ctx, store, envIPs, time.Now())
		}
		warm := newCertWarm(
			func() []string { return e.warmSet(appsDomain, parents) },
			perHostCert,
			dnsGate(warmResolver, edgeIPs),
			e.issuerGate,
			store,
		)
		go warm.run()
		log.Printf("edge: cert warm on (%s pass) — custom domains are ordered and renewed proactively, not on first handshake", warmInterval)
	}
	tlsCfg.GetCertificate = getCert
	if mirror != nil {
		// Started last: every field the loop reads (wake above) is in place.
		go mirror.run(make(chan struct{})) // process-lifetime; never stopped
	}

	// The non-ACME :80 handler. See httpFallbackHandler's doc comment for the
	// Allowed-vs-lookupUpstream split.
	httpFallback := e.httpFallbackHandler()

	// :80's ACME path, outermost first: the mirror answers any edge's HTTP-01
	// challenge, autocert answers its own live orders, and everything else
	// falls through to the redirect handler.
	acmeAndFallback := m.HTTPHandler(httpFallback)
	if mirror != nil {
		acmeAndFallback = mirror.challengeHandler(acmeAndFallback)
	}

	// ReadHeaderTimeout guards the public front door against Slowloris-style
	// slow-header connection exhaustion. No ReadTimeout/WriteTimeout — those
	// would break long-lived proxied WebSockets.
	httpSrv := &http.Server{Addr: *httpAddr, Handler: acmeAndFallback, ReadHeaderTimeout: 10 * time.Second}
	httpsSrv := &http.Server{Addr: *httpsAddr, Handler: e, TLSConfig: tlsCfg, ReadHeaderTimeout: 10 * time.Second}

	// :80 serves the ACME HTTP-01 challenge and redirects everything else.
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("edge: http listener %s: %v", *httpAddr, err)
		}
	}()
	go func() {
		log.Printf("edge: serving %d route(s) on %s (registry %s, acme cache %s)",
			len(e.current()), *httpsAddr, *registryPath, *acmeCache)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("edge: https listener %s: %v", *httpsAddr, err)
		}
	}()

	// Graceful shutdown on SIGTERM/SIGINT (e.g. an edge binary upgrade): stop
	// accepting and drain in-flight HTTP before exiting. Proxied WebSockets are
	// hijacked (not drained); they reconnect through the new edge.
	<-stop
	log.Printf("edge: draining and shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpsSrv.Shutdown(ctx)
	_ = httpSrv.Shutdown(ctx)
}

// enableStaticFromEnv configures static serving when the environment names a
// deployment store (CARLOS_DEPLOYMENT_BUCKET for S3, else
// CARLOS_DEPLOYMENT_DIR for a directory) and CARLOS_RELEASE_PUBKEY. It
// deliberately duplicates the CLI's ten-line store-open helper — the edge
// can't import package main. Missing config is not an error: static routes
// simply keep answering 404, exactly the pre-A8 behavior.
func enableStaticFromEnv(e *Edge, dataDir string) error {
	pubB64 := os.Getenv("CARLOS_RELEASE_PUBKEY")
	bucket := os.Getenv("CARLOS_DEPLOYMENT_BUCKET")
	dir := os.Getenv("CARLOS_DEPLOYMENT_DIR")
	if pubB64 == "" || (bucket == "" && dir == "") {
		return nil
	}
	pub, err := release.ParsePublicKey(pubB64, "CARLOS_RELEASE_PUBKEY")
	if err != nil {
		return err
	}
	var store release.Store
	if bucket != "" {
		s, err := release.NewS3Store(context.Background(), bucket)
		if err != nil {
			return err
		}
		store = s
	} else {
		store = release.NewDirStore(dir)
	}
	e.EnableStatic(store, pub, filepath.Join(dataDir, "static"))
	// Rebuild the route table so static routes that loaded as 404 stubs in
	// New's startup reload get real handlers.
	return e.Reload()
}

// enableTunnelsFromEnv arms the reverse-channel hub when the environment
// names a reserved hostname (CARLOS_TUNNEL_HOST); CARLOS_TUNNEL_CLIENTS
// ("name=token[,name=token]") names its STATIC authorized clients, which may
// legitimately be empty. No CARLOS_TUNNEL_HOST is not an error: the box
// simply serves no tunnels at all, and routes with Tunnel set keep whatever
// local backend they have (Reload's tunnel case is guarded on the hub) — no
// fleet channels either, since those need a hub to land on just as much as a
// static client does.
//
// A host with no static clients configured IS still valid, unlike before
// fleet channels existed: the hub also serves customer-fleet boxes, whose
// credentials come from the registry (Reload -> SetDynamicAuth), not this
// env var — a box that only ever hosts fleet channels legitimately runs with
// CARLOS_TUNNEL_CLIENTS unset. The registration endpoint this arms is not
// "no one can ever authenticate against" in that case; it is "no STATIC
// client can," which the operator chose on purpose.
//
// Static channel names are validated here, at the only place one enters the
// process from config: a name the CLI would refuse (registry.ValidTunnelName,
// what `carlos route -tunnel` enforces) can never match a route's Tunnel
// column, so such a client would authenticate, connect, and silently carry no
// traffic at all. Fail the boot instead, naming the entry. Dynamic (fleet)
// channel names are validated separately, at the registry writer
// (AddFleetBox) — they are never parsed from this env var.
func enableTunnelsFromEnv(e *Edge) error {
	host := os.Getenv("CARLOS_TUNNEL_HOST")
	if host == "" {
		return nil
	}
	clients, err := parseTunnelClients(os.Getenv("CARLOS_TUNNEL_CLIENTS"))
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(clients)) {
		if !registry.ValidTunnelName(name) {
			return fmt.Errorf("CARLOS_TUNNEL_CLIENTS: %q is not a valid tunnel name — want [a-z0-9-]{1,32}, and it must match a route's --tunnel to carry anything", name)
		}
	}
	e.EnableTunnels(NewTunnelHub(host, clients))
	// Rebuild the route table so tunnel-backed routes that were skipped (or
	// left on their old socket/addr) by the startup reload pick up the hub,
	// and so fleet channel auth (SetDynamicAuth) gets its first load.
	if err := e.Reload(); err != nil {
		return fmt.Errorf("reload after EnableTunnels: %w", err)
	}
	log.Printf("edge: tunnel hub armed on %s (%d static client(s))", host, len(clients))
	if len(clients) == 0 {
		// Zero static clients is legitimate (doc comment above), but it is
		// ALSO exactly what a typo'd CARLOS_TUNNEL_CLIENTS var name produces
		// (the env var silently reads empty rather than failing to parse) —
		// an operator staring at logs needs this distinguishable from "I
		// meant to run fleet-channels-only," not inferred from a client
		// count buried in the line above.
		log.Printf("edge: WARNING: no static tunnel clients configured — fleet channels only")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
