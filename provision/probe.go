package provision

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/carlos"
)

// ProbeTimeout bounds one probe request.
const ProbeTimeout = 5 * time.Second

// maxBuild is how much of a probe's body is kept as Answer.Build. The version
// body is a short string; the rest of a catch-all's page is not wanted.
const maxBuild = 128

// Answer is what one probe of /api/version got.
type Answer struct {
	// URL is what was asked, query included.
	URL string
	// Status is 0 when there was no answer.
	Status int
	// Version is the X-Carlos-Version header: the release of the route the
	// edge served the request from.
	Version string
	// Build is the body's first 128 bytes, trimmed.
	Build string
	// Instance is the Rastrillo-Host header: the instance that answered, as
	// carlos.NameInstance has it name itself.
	Instance string
}

// bareHost refuses a host that is empty or that the URL parser would read as
// something else, so a "/", "@" or "?" in it cannot send a probe somewhere
// the caller did not name. It is checked before anything is asked or
// compared: an empty target would otherwise match the Rastrillo-Host an
// anonymous catch-all does not send, and read as the tenant.
func bareHost(host string) error {
	if u, err := url.Parse("https://" + host + "/"); err != nil || host == "" || u.Host != host || u.User != nil {
		return fmt.Errorf("provision: %q is not a bare hostname", host)
	}
	return nil
}

// probe asks https://<host><path> for its version. Redirects are never
// followed: one is the answer, and following it would read another host's 200
// as this one's.
//
// The query is fresh each time because the edge's response cache keys on the
// requested host (edge.go:1255-1257): an answer cached before the path map
// was published would otherwise be read back as one after it, and a
// fall-through would look like a route, or a route like a fall-through.
func probe(ctx context.Context, hc *http.Client, host, path string) (Answer, error) {
	var tok [4]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return Answer{}, err
	}
	target := "https://" + host + path + "?provision=" + hex.EncodeToString(tok[:])
	a := Answer{URL: target}
	if err := bareHost(host); err != nil {
		return a, err
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return a, err
	}
	resp, err := noRedirect(hc).Do(req)
	if err != nil {
		return a, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBuild))
	a.Status = resp.StatusCode
	a.Version = resp.Header.Get("X-Carlos-Version")
	a.Instance = resp.Header.Get(carlos.HostHeader)
	a.Build = strings.TrimSpace(string(body))
	return a, err
}

// Ready says whether the instance at host answers on its own host. Only a
// 200 whose Rastrillo-Host names host counts: the edge's pending page is a
// 503, a redirect is not an answer, and a catch-all's 200 or another
// instance's names nobody or somebody else. A tenant built without
// carlos.NameInstance never reads ready.
//
// A transport error comes back with false; treat it as not ready yet. hc is
// http.DefaultClient when nil.
func Ready(ctx context.Context, hc *http.Client, host string) (bool, Answer, error) {
	a, err := probe(ctx, hc, host, "/api/version")
	if err != nil {
		return false, a, err
	}
	return a.Status == http.StatusOK && a.Instance == host, a, nil
}

// WaitReady probes every interval until the instance is ready or ctx ends.
// At the deadline it returns the last answer it got and an error wrapping
// ctx.Err(). A probe that fails is a probe that is not ready; only the
// context stops the wait.
func WaitReady(ctx context.Context, hc *http.Client, host string, every time.Duration) (Answer, error) {
	var last Answer
	if err := bareHost(host); err != nil {
		// No answer could ever make this host ready; waiting out the
		// deadline would only delay the caller's error.
		return last, err
	}
	err := poll(ctx, every, func() bool {
		ok, a, err := Ready(ctx, hc, host)
		if err == nil {
			// A probe cut short by the deadline has no answer to report, and
			// would overwrite the last real one with zeros.
			last = a
		}
		return ok
	})
	if err != nil {
		return last, fmt.Errorf("provision: %s is not ready (last answer %d): %w", host, last.Status, err)
	}
	return last, nil
}

// poll calls try until it returns true or ctx ends, waiting every between
// calls. It returns ctx.Err() in the second case.
func poll(ctx context.Context, every time.Duration, try func() bool) error {
	if every <= 0 {
		// Zero would spin on the network as fast as it answers.
		every = time.Second
	}
	for {
		if try() {
			return nil
		}
		t := time.NewTimer(every)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// RouteState is what CheckRoute decided about the product path.
type RouteState int

const (
	// RoutePending: not yet known. The path map may not be live, the tenant
	// may still be starting, or the answers cannot tell the hub from the
	// tenant.
	RoutePending RouteState = iota
	// Routed: the product path reached the tenant.
	Routed
	// FellThrough: the product path was answered by the hub. This is what
	// the edge does when a prefix is unlisted or its target is not a ready
	// route of the same account on the same box (spec section 7.4;
	// edge.go:1238-1240), so it names a tenant whose path will not work. It
	// is not final on its own: a publish takes a few seconds to go live, so
	// callers keep checking until their own deadline.
	FellThrough
	// NoBasePath: the tenant answered the prefix with a 404, so it does not
	// serve /<slug>/api/version. The tenant needs the base path set.
	NoBasePath
)

func (s RouteState) String() string {
	switch s {
	case RoutePending:
		return "pending"
	case Routed:
		return "routed"
	case FellThrough:
		return "fell through"
	case NoBasePath:
		return "no base path"
	}
	return fmt.Sprintf("RouteState(%d)", int(s))
}

// The errors a route's state stands for. Route.Err maps to them; an app
// turns them into its own sentences.
var (
	ErrFellThrough = errors.New("provision: the product path reaches the hub, not the tenant")
	ErrNoBasePath  = errors.New("provision: the tenant does not answer under its base path")
	ErrNotRouted   = errors.New("provision: the product path has not been seen to reach the tenant")
)

// Route is CheckRoute's finding, with the three answers it rests on. Prefix
// is the only one set when the state is Routed.
type Route struct {
	State RouteState
	// Prefix is the answer at https://<product>/<slug>/api/version.
	Prefix Answer
	// Tenant is the answer at the tenant's own host, Hub the one at the
	// product's root.
	Tenant, Hub Answer
}

// Err is the error for the state: nil for Routed. It is the one place the
// states meet their errors, for WaitRouted and for a caller that checks once
// per refresh.
func (r Route) Err() error {
	switch r.State {
	case Routed:
		return nil
	case FellThrough:
		return ErrFellThrough
	case NoBasePath:
		return ErrNoBasePath
	}
	return ErrNotRouted
}

// CheckRoute asks whether https://<product>/<slug>/ reaches the tenant at
// host and not the hub that holds product.
//
// Only a 200 from the instance that names itself proves a route. The version
// header names a release, not an instance, and two apps can carry the same
// one, so it only tells the other states apart, and only when the tenant's
// version and the hub's differ. When they do not, or when a third instance
// answered, the state stays RoutePending. A 404 that names the tenant is
// NoBasePath whatever the versions are.
//
// An empty or malformed host, product or slug is refused before any request.
// A transport error returns the route as far as it got, pending, with the
// error.
func CheckRoute(ctx context.Context, hc *http.Client, product, slug, host string) (Route, error) {
	var r Route
	var err error
	if err = checkTarget(product, slug, host); err != nil {
		return r, err
	}
	prefixPath := "/" + url.PathEscape(slug) + "/api/version"
	if r.Prefix, err = probe(ctx, hc, product, prefixPath); err != nil {
		return r, err
	}
	if r.Prefix.Status == http.StatusOK && r.Prefix.Instance == host {
		r.State = Routed
		return r, nil
	}
	if r.Tenant, err = probe(ctx, hc, host, "/api/version"); err != nil {
		return r, err
	}
	if r.Hub, err = probe(ctx, hc, product, "/api/version"); err != nil {
		return r, err
	}
	distinct := r.Tenant.Version != "" && r.Hub.Version != "" && r.Tenant.Version != r.Hub.Version
	switch {
	case r.Prefix.Instance == host && r.Prefix.Status == http.StatusNotFound:
		// The tenant named itself on a 404: it answered, and has no such
		// path. This holds whatever the two versions are, so it is read
		// before the generic non-empty header below, which would take it for
		// a third instance. The tenant's own 503 is not this: it is not up
		// yet, and falls to the default.
		r.State = NoBasePath
	case r.Prefix.Instance != "" && r.Prefix.Instance == r.Hub.Instance:
		// The hub names itself on the prefix, which it can do for a path it
		// catches (a locale prefix), whatever its version is.
		r.State = FellThrough
	case r.Prefix.Instance != "":
		// Some third instance answered; nothing here says where it is.
		r.State = RoutePending
	case distinct && r.Prefix.Version == r.Hub.Version:
		r.State = FellThrough
	case distinct && r.Prefix.Version == r.Tenant.Version && r.Prefix.Status == http.StatusNotFound:
		// The tenant is what answered, and it has no such path. A 5xx or a
		// 429 from it falls to the default: it is not up, rather than not
		// built for the prefix.
		r.State = NoBasePath
	default:
		r.State = RoutePending
	}
	return r, nil
}

// checkTarget refuses a route question that cannot be answered: the tenant
// host must be a bare hostname, since CheckRoute proves a route by an answer
// that names it, and the slug must be a prefix PublishPaths would send. A
// slug with a slash or a dot segment probes some other path, or one a path
// cleaner rewrites, so its answer would be read as this tenant's route.
func checkTarget(product, slug, host string) error {
	if !validPrefix(slug) {
		return fmt.Errorf("provision: slug %q is not a single bare path segment", slug)
	}
	if err := bareHost(product); err != nil {
		return err
	}
	return bareHost(host)
}

// WaitRouted checks every interval until the path is routed or ctx ends. At
// the deadline it returns the last route it found and its Err wrapping
// ctx.Err(), so the record can say why: a fall-through, a tenant without its
// base path, or an answer that could not be told.
func WaitRouted(ctx context.Context, hc *http.Client, product, slug, host string, every time.Duration) (Route, error) {
	var last Route
	if err := checkTarget(product, slug, host); err != nil {
		return last, err
	}
	err := poll(ctx, every, func() bool {
		r, err := CheckRoute(ctx, hc, product, slug, host)
		if err == nil {
			// As in WaitReady: a check cut short by the deadline is not a
			// finding, and must not replace the last real one.
			last = r
		}
		return err == nil && r.State == Routed
	})
	if err != nil {
		return last, fmt.Errorf("%w: %w", last.Err(), err)
	}
	return last, nil
}
