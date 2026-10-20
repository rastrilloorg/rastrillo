package provision_test

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/carlos"
	"amadan.net/rastrillo/rastrillo/provision"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

// rig is a hub app (bdf/hub, holding product.test) and a tenant app
// (bdf/tenant) on one fake platform, with one tenant instance to probe.
type rig struct {
	p    *provisiontest.Platform
	host string
	hub  *provision.Console
	ten  *provision.Console
}

// newRig releases both apps at the given versions and has the tenant serve
// /<slug>/api/version as well as /api/version. It creates nothing yet, so a
// test can still make the instance unready first.
func newRig(t *testing.T, hubVersion, tenantVersion, slug string) *rig {
	t.Helper()
	p := provisiontest.New()
	p.Release("bdf", "hub", hubVersion)
	p.Release("bdf", "tenant", tenantVersion)
	p.Serve("bdf", "hub", provisiontest.Hub())
	p.Serve("bdf", "tenant", provisiontest.Tenant(slug))
	return &rig{
		p:    p,
		host: "acme." + domain,
		hub:  newConsole(t, hubConfig(p)),
		ten:  newConsole(t, tenantConfig(p)),
	}
}

func (r *rig) create(t *testing.T) *rig {
	t.Helper()
	if _, err := r.ten.Create(context.Background(), provision.CreateRequest{Host: r.host}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

func (r *rig) publish(t *testing.T) *rig {
	t.Helper()
	if _, err := r.hub.PublishPaths(context.Background(), product, fixed(map[string]string{"acme": r.host})); err != nil {
		t.Fatalf("PublishPaths: %v", err)
	}
	return r
}

func (r *rig) check(t *testing.T) provision.Route {
	t.Helper()
	got, err := provision.CheckRoute(context.Background(), r.p.Client(), product, "acme", r.host)
	if err != nil {
		t.Fatalf("CheckRoute: %v", err)
	}
	return got
}

func (r *rig) ready(t *testing.T) (bool, provision.Answer) {
	t.Helper()
	ok, a, err := provision.Ready(context.Background(), r.p.Client(), r.host)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	return ok, a
}

func TestReadyWaitsForTheInstance(t *testing.T) {
	r := newRig(t, "v1", "v2", "")
	r.p.Unready(2)
	r.create(t)
	for i := 0; i < 2; i++ {
		ok, a := r.ready(t)
		if ok || a.Status != 503 {
			t.Fatalf("probe %d: ready %v, status %d; want not ready, the pending 503", i+1, ok, a.Status)
		}
	}
	ok, a := r.ready(t)
	if !ok || a.Instance != r.host || a.Build != "test" || a.Status != 200 {
		t.Errorf("third probe: ready %v, %+v; want ready, 200, Instance %s, Build test", ok, a, r.host)
	}
	if a.Version != "v2" {
		t.Errorf("Version = %q, want the tenant release v2", a.Version)
	}
}

func TestReadyIgnoresARedirect(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t)
	r.p.Serve("bdf", "tenant", func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "https://"+product+"/acme/api/version", http.StatusFound)
		})
	})
	ok, a := r.ready(t)
	if ok || a.Status != http.StatusFound {
		t.Errorf("ready %v, status %d; want not ready, the 302 itself", ok, a.Status)
	}
	for _, c := range r.p.Calls() {
		if strings.Contains(c, product) {
			t.Errorf("the probe followed the redirect: %q", c)
		}
	}
}

func TestReadyRefusesAnotherInstancesAnswer(t *testing.T) {
	r := newRig(t, "v1", "v2", "").create(t)
	r.p.Serve("bdf", "tenant", func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(carlos.HostHeader, "other.bdf.oncarlos.test")
			w.Write([]byte("test"))
		})
	})
	ok, a := r.ready(t)
	if ok || a.Status != 200 || a.Instance != "other.bdf.oncarlos.test" {
		t.Errorf("ready %v, %+v; want not ready, 200 naming other.bdf.oncarlos.test", ok, a)
	}
}

func TestReadyRefusesAnAnonymous200(t *testing.T) {
	r := newRig(t, "v1", "v2", "").create(t)
	r.p.Serve("bdf", "tenant", func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("test")) })
	})
	ok, a := r.ready(t)
	if ok || a.Status != 200 || a.Instance != "" {
		t.Errorf("ready %v, %+v; want not ready, a 200 that names no instance", ok, a)
	}
}

func TestReadyReportsATransportError(t *testing.T) {
	r := newRig(t, "v1", "v2", "").create(t)
	boom := errors.New("connection refused")
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, boom })}
	ok, _, err := provision.Ready(context.Background(), hc, r.host)
	if ok || !errors.Is(err, boom) {
		t.Errorf("Ready = %v, %v; want false and the transport's error", ok, err)
	}
}

func TestReadyRefusesAHostThatIsNotBare(t *testing.T) {
	p := provisiontest.New()
	for _, h := range []string{"", "acme.bdf.oncarlos.test/x", "evil.test@acme.bdf.oncarlos.test", "a b.test"} {
		ok, _, err := provision.Ready(context.Background(), p.Client(), h)
		if ok || err == nil {
			t.Errorf("Ready(%q) = %v, %v; want false and an error", h, ok, err)
		}
	}
	if len(p.Calls()) != 0 {
		t.Errorf("a refused host reached the network: %v", p.Calls())
	}
}

func TestWaitReadyReturnsTheAnswer(t *testing.T) {
	r := newRig(t, "v1", "v2", "")
	r.p.Unready(2)
	r.create(t)
	a, err := provision.WaitReady(context.Background(), r.p.Client(), r.host, 10*time.Millisecond)
	if err != nil || a.Instance != r.host {
		t.Errorf("WaitReady = %+v, %v; want the instance's answer", a, err)
	}
}

func TestWaitReadyGivesUpWithTheContext(t *testing.T) {
	r := newRig(t, "v1", "v2", "")
	r.p.Unready(1 << 30)
	r.create(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	a, err := provision.WaitReady(ctx, r.p.Client(), r.host, 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the context's deadline", err)
	}
	if a.Status != 503 {
		t.Errorf("last answer status = %d, want the pending 503", a.Status)
	}
}

func TestCheckRouteRouted(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t).publish(t)
	got := r.check(t)
	if got.State != provision.Routed || got.Prefix.Instance != r.host {
		t.Errorf("Route = %+v, want Routed with the tenant answering the prefix", got)
	}
	if got.Err() != nil {
		t.Errorf("Err = %v, want nil", got.Err())
	}
}

func TestCheckRouteFallsThroughOffBox(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t).publish(t)
	r.p.OtherBox(r.host)
	got := r.check(t)
	if got.State != provision.FellThrough || got.Prefix.Version != "v1" {
		t.Errorf("Route = %+v, want FellThrough with the hub's v1 on the prefix", got)
	}
	if got.Tenant.Version != "v2" || got.Hub.Version != "v1" {
		t.Errorf("Tenant %+v, Hub %+v; want their own versions v2 and v1", got.Tenant, got.Hub)
	}
}

func TestCheckRouteSameVersionStillNeedsTheHostHeader(t *testing.T) {
	r := newRig(t, "abc1234", "abc1234", "acme").create(t).publish(t)
	if got := r.check(t); got.State != provision.Routed {
		t.Fatalf("on the box: State = %v, want Routed", got.State)
	}
	r.p.OtherBox(r.host)
	if got := r.check(t); got.State != provision.RoutePending {
		t.Errorf("off the box, same version: State = %v, want RoutePending (the version cannot tell the apps apart)", got.State)
	}
}

func TestCheckRouteHubNamedByItsHeader(t *testing.T) {
	r := newRig(t, "abc1234", "abc1234", "acme").create(t).publish(t)
	// A hub that answers every path under a prefix, as a locale catch does.
	r.p.Serve("bdf", "hub", provisiontest.Tenant("acme"))
	r.p.OtherBox(r.host)
	got := r.check(t)
	if got.State != provision.FellThrough || got.Prefix.Instance != product {
		t.Errorf("Route = %+v, want FellThrough with the hub naming itself on the prefix", got)
	}
}

func TestCheckRouteWithoutBasePath(t *testing.T) {
	r := newRig(t, "v1", "v2", "").create(t).publish(t)
	got := r.check(t)
	if got.State != provision.NoBasePath || got.Prefix.Status != 404 {
		t.Errorf("Route = %+v, want NoBasePath, the tenant's 404 on the prefix", got)
	}
}

func TestCheckRouteThirdInstanceIsPending(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t)
	hub := provisiontest.Hub()
	r.p.Serve("bdf", "hub", func(host string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/acme/api/version" {
				w.Header().Set(carlos.HostHeader, "third.bdf.oncarlos.test")
				http.NotFound(w, req)
				return
			}
			hub(host).ServeHTTP(w, req)
		})
	})
	got := r.check(t)
	if got.State != provision.RoutePending || got.Prefix.Instance != "third.bdf.oncarlos.test" {
		t.Errorf("Route = %+v, want RoutePending, a third instance on the prefix", got)
	}
}

func TestCheckRouteTenantErrorIsPending(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t).publish(t)
	tenant := provisiontest.Tenant("")
	r.p.Serve("bdf", "tenant", func(host string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/acme/api/version" {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			tenant(host).ServeHTTP(w, req)
		})
	})
	got := r.check(t)
	if got.State != provision.RoutePending || got.Prefix.Status != 503 {
		t.Errorf("Route = %+v, want RoutePending, not NoBasePath, on the tenant's 503", got)
	}
}

func TestCheckRouteBeforePublishIsFallThrough(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t)
	got := r.check(t)
	if got.State != provision.FellThrough {
		t.Errorf("Route = %+v, want FellThrough, the hub answering an unpublished prefix", got)
	}
}

func TestCheckRouteReportsATransportError(t *testing.T) {
	boom := errors.New("connection refused")
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, boom })}
	got, err := provision.CheckRoute(context.Background(), hc, product, "acme", "acme."+domain)
	if !errors.Is(err, boom) || got.State != provision.RoutePending {
		t.Errorf("CheckRoute = %+v, %v; want RoutePending and the transport's error", got, err)
	}
}

func TestRouteStateStrings(t *testing.T) {
	for s, want := range map[provision.RouteState]string{
		provision.RoutePending: "pending", provision.Routed: "routed",
		provision.FellThrough: "fell through", provision.NoBasePath: "no base path",
	} {
		if s.String() != want {
			t.Errorf("RouteState(%d).String() = %q, want %q", int(s), s.String(), want)
		}
	}
}

func TestRouteErrMapsEachState(t *testing.T) {
	for _, c := range []struct {
		state provision.RouteState
		want  error
	}{
		{provision.FellThrough, provision.ErrFellThrough},
		{provision.NoBasePath, provision.ErrNoBasePath},
		{provision.RoutePending, provision.ErrNotRouted},
		{provision.Routed, nil},
	} {
		if got := (provision.Route{State: c.state}).Err(); got != c.want {
			t.Errorf("Route{%v}.Err() = %v, want %v", c.state, got, c.want)
		}
	}
}

// queryLog is a handler wrapper that records each request's raw query,
// which Calls() leaves out.
type queryLog struct {
	mu sync.Mutex
	qs []string
}

func (l *queryLog) wrap(h func(string) http.Handler) func(string) http.Handler {
	return func(host string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l.mu.Lock()
			l.qs = append(l.qs, r.URL.RawQuery)
			l.mu.Unlock()
			h(host).ServeHTTP(w, r)
		})
	}
}

var freshQuery = regexp.MustCompile(`^provision=[0-9a-f]{8}$`)

func TestProbesCarryAFreshQuery(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t)
	var tenant, hub queryLog
	r.p.Serve("bdf", "tenant", tenant.wrap(provisiontest.Tenant("acme")))
	r.p.Serve("bdf", "hub", hub.wrap(provisiontest.Hub()))

	r.ready(t)
	r.ready(t)
	if len(tenant.qs) != 2 {
		t.Fatalf("tenant saw %d requests, want 2", len(tenant.qs))
	}
	for _, q := range tenant.qs {
		if !freshQuery.MatchString(q) {
			t.Errorf("Ready probe query = %q, want provision= and 8 hex digits", q)
		}
	}
	if tenant.qs[0] == tenant.qs[1] {
		t.Errorf("two probes carried the same query %q, so the edge's cache could answer the second", tenant.qs[0])
	}

	// Unpublished, the prefix reaches the hub; the other two probes go to the
	// tenant's host and the hub's.
	tenant.qs, hub.qs = nil, nil
	r.check(t)
	if len(hub.qs) != 2 || len(tenant.qs) != 1 {
		t.Fatalf("CheckRoute made %d hub and %d tenant requests, want 2 and 1", len(hub.qs), len(tenant.qs))
	}
	for _, q := range append(append([]string(nil), hub.qs...), tenant.qs...) {
		if !freshQuery.MatchString(q) {
			t.Errorf("CheckRoute probe query = %q, want provision= and 8 hex digits", q)
		}
	}
	if hub.qs[0] == hub.qs[1] {
		t.Errorf("the hub's two probes carried the same query %q", hub.qs[0])
	}
}

func TestWaitRoutedReturnsTheRoute(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme").create(t).publish(t)
	got, err := provision.WaitRouted(context.Background(), r.p.Client(), product, "acme", r.host, 10*time.Millisecond)
	if err != nil || got.State != provision.Routed {
		t.Errorf("WaitRouted = %+v, %v; want Routed", got, err)
	}
}

func TestWaitRoutedReportsWhyAtTheDeadline(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*testing.T) *rig
		want  error
	}{
		{"fell through", func(t *testing.T) *rig {
			r := newRig(t, "v1", "v2", "acme").create(t).publish(t)
			r.p.OtherBox(r.host)
			return r
		}, provision.ErrFellThrough},
		{"no base path", func(t *testing.T) *rig {
			return newRig(t, "v1", "v2", "").create(t).publish(t)
		}, provision.ErrNoBasePath},
		{"cannot tell", func(t *testing.T) *rig {
			r := newRig(t, "abc1234", "abc1234", "acme").create(t).publish(t)
			r.p.OtherBox(r.host)
			return r
		}, provision.ErrNotRouted},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := c.setup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			got, err := provision.WaitRouted(ctx, r.p.Client(), product, "acme", r.host, 50*time.Millisecond)
			if !errors.Is(err, c.want) || !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err = %v, want %v wrapping the context's deadline", err, c.want)
			}
			if got.State == provision.Routed {
				t.Errorf("Route = %+v, want not routed", got)
			}
		})
	}
}

func TestCheckRouteRefusesAnEmptyTarget(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme")
	// The hub answers every path with an anonymous 200, as a catch-all does:
	// with an empty host it would equal the empty Rastrillo-Host.
	r.p.Serve("bdf", "hub", func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("test")) })
	})
	for _, c := range []struct{ product, slug, host string }{
		{product, "acme", ""},
		{product, "acme", "acme.bdf.oncarlos.test/x"},
		{product, "", "acme.bdf.oncarlos.test"},
		{"", "acme", "acme.bdf.oncarlos.test"},
	} {
		got, err := provision.CheckRoute(context.Background(), r.p.Client(), c.product, c.slug, c.host)
		if err == nil || got.State == provision.Routed {
			t.Errorf("CheckRoute(%q, %q, %q) = %+v, %v; want an error and not Routed", c.product, c.slug, c.host, got, err)
		}
		if _, err := provision.WaitRouted(context.Background(), r.p.Client(), c.product, c.slug, c.host, time.Millisecond); err == nil {
			t.Errorf("WaitRouted(%q, %q, %q) succeeded", c.product, c.slug, c.host)
		}
	}
	if len(r.p.Calls()) != 0 {
		t.Errorf("a refused target reached the network: %v", r.p.Calls())
	}
}

// A slug PublishPaths would refuse names no prefix of the map, and a probe
// of it asks some other path: "a/b" another tenant's, ".." the hub's root
// after any path cleaner. Each is refused before a request, by the same rule
// PublishPaths uses.
func TestCheckRouteRefusesASlugPublishPathsWould(t *testing.T) {
	r := newRig(t, "v1", "v2", "acme")
	host := "acme.bdf.oncarlos.test"
	for _, slug := range []string{"a/b", "/acme", "acme/", ".", "..", "acme?x=1", "acme#x", "ac me", strings.Repeat("a", 256)} {
		if got, err := provision.CheckRoute(context.Background(), r.p.Client(), product, slug, host); err == nil || got.State == provision.Routed {
			t.Errorf("CheckRoute(slug %q) = %+v, %v; want an error", slug, got, err)
		}
		// Bounded, so a WaitRouted that polls a bad slug fails here instead
		// of hanging the run; a refusal returns long before it.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		if _, err := provision.WaitRouted(ctx, r.p.Client(), product, slug, host, time.Millisecond); err == nil || ctx.Err() != nil {
			t.Errorf("WaitRouted(slug %q) = %v, want a refusal before its context ended", slug, err)
		}
		cancel()
	}
	if len(r.p.Calls()) != 0 {
		t.Errorf("a refused slug reached the network: %d calls, first %v", len(r.p.Calls()), r.p.Calls()[0])
	}
}

func TestWaitReadyRefusesABadHostAtOnce(t *testing.T) {
	p := provisiontest.New()
	start := time.Now()
	_, err := provision.WaitReady(context.Background(), p.Client(), "", time.Hour)
	if err == nil || time.Since(start) > time.Second {
		t.Errorf("WaitReady(\"\") = %v after %v; want an error at once", err, time.Since(start))
	}
}

// tenantAtPrefix is a tenant whose own host answers and whose prefix answers
// with status, naming itself in Rastrillo-Host.
func tenantAtPrefix(status int) func(string) http.Handler {
	own := provisiontest.Tenant("")
	return func(host string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path != "/acme/api/version" {
				own(host).ServeHTTP(w, req)
				return
			}
			w.Header().Set(carlos.HostHeader, host)
			http.Error(w, "no", status)
		})
	}
}

func TestCheckRouteTenantNaming404IsNoBasePath(t *testing.T) {
	for _, c := range []struct{ name, hub, tenant string }{
		{"distinct versions", "v1", "v2"},
		{"identical versions", "abc1234", "abc1234"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.hub, c.tenant, "acme").create(t).publish(t)
			r.p.Serve("bdf", "tenant", tenantAtPrefix(http.StatusNotFound))
			got := r.check(t)
			if got.State != provision.NoBasePath || got.Prefix.Instance != r.host {
				t.Errorf("Route = %+v, want NoBasePath, the tenant naming itself on a 404", got)
			}
		})
	}
}

func TestCheckRouteTenant503NamingItselfIsPending(t *testing.T) {
	for _, c := range []struct{ name, hub, tenant string }{
		{"distinct versions", "v1", "v2"},
		{"identical versions", "abc1234", "abc1234"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.hub, c.tenant, "acme").create(t).publish(t)
			r.p.Serve("bdf", "tenant", tenantAtPrefix(http.StatusServiceUnavailable))
			got := r.check(t)
			if got.State != provision.RoutePending || got.Prefix.Status != 503 {
				t.Errorf("Route = %+v, want RoutePending on the tenant's own 503", got)
			}
		})
	}
}
