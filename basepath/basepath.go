// Package basepath serves a whole app under /<slug> on another app's
// origin, such as https://example.com/acme, the way a hub product puts
// each tenant instance under its own path.
//
// The platform's edge passes a path-routed request on unchanged, prefix
// and Host included (path-routing spec §6), and one binary serves every
// tenant. So the app's routes stay root-relative, and the prefix is per
// process: Config.Handler strips it on the way in and puts it back on
// the way out. Three contracts follow:
//
//   - Headers carry app paths. A Location or Rastrillo-Location that
//     starts with exactly one / is the app's own path, and the
//     middleware adds the base to it, once. An absolute URL is how an
//     app leaves itself, and is never touched.
//   - Bodies carry public paths. HTML cannot be rewritten cheaply, so
//     every root-absolute URL in a page goes through Path (or the
//     templates' path func), and basepathtest.Leaks finds the ones that
//     do not.
//   - r.RequestURI is never changed. Only r.URL loses the prefix, as
//     with http.StripPrefix, so code that hands the hub a return path
//     builds it from what the browser asked for. Original returns it.
package basepath

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// DirectHost is what an instance does with a request on any host other
// than the product origin's: its own hostname, or Host carlos.
type DirectHost int

const (
	// RedirectDirect sends visitors to the same page under the product
	// path, where the hub session lives.
	RedirectDirect DirectHost = iota
	// ServeDirect serves a read-only public view at the root.
	ServeDirect
)

// Config is a base path. The zero value is off.
type Config struct {
	// Origin is the product origin the base path lives on, scheme and
	// host only: https://example.com.
	Origin string
	// Slug is the base path's one segment: acme serves /acme.
	Slug string
	// DirectHost says what the instance's own hostname does.
	DirectHost DirectHost
}

// Enabled reports whether c asks for a base path at all.
func (c Config) Enabled() bool { return c.Origin != "" || c.Slug != "" }

// maxSlug is the longest RASTRILLO_SLUG: 56 characters, a hyphen and a
// 6-character suffix fill a 63-character DNS label.
const maxSlug = 56

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,54}[a-z0-9])?$`)

// labelRe is Plan 4's instance label, <slug>-<6 lower-case base32>
// (spec §7.2 step 1, provision.NewLabel), which owns the shape. The slug
// part is 1 to provision.MaxSlug (25) characters: NewLabel cuts a longer
// slug before the suffix, so accepting one here would only ever read a
// cut slug, and the instance would 404 at its own /<slug>.
var labelRe = regexp.MustCompile(`^([a-z0-9](?:[a-z0-9-]{0,23}[a-z0-9])?)-[a-z2-7]{6}$`)

// Validate reports whether c can serve. The zero Config is off, and
// valid.
func (c Config) Validate() error {
	if c == (Config{}) {
		return nil
	}
	if err := validOrigin(c.Origin); err != nil {
		return err
	}
	if !slugRe.MatchString(c.Slug) {
		return fmt.Errorf("rastrillo/basepath: slug %q is not 1 to %d lower-case letters, digits and inner hyphens; set RASTRILLO_SLUG to one that is", c.Slug, maxSlug)
	}
	if c.DirectHost != RedirectDirect && c.DirectHost != ServeDirect {
		return fmt.Errorf("rastrillo/basepath: DirectHost %d is neither RedirectDirect nor ServeDirect", c.DirectHost)
	}
	return nil
}

func validOrigin(origin string) error {
	if origin == "" {
		return errors.New("rastrillo/basepath: a base path needs the product origin; set the app's origin to the https://host the hub is served at")
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(origin, "#") {
		return fmt.Errorf("rastrillo/basepath: origin %q is not a scheme and host, such as https://example.com; set the app's origin to one, with no path", origin)
	}
	return nil
}

// FromEnv reads a base path from RASTRILLO_SLUG, or from the instance's
// label in CARLOS_HOST when RASTRILLO_SLUG_FROM_HOST is "1". With
// neither set it returns the zero Config: off.
//
// The host form is opt-in because an ordinary app whose hostname happens
// to end in a hyphen and six base32 letters must not move under a path.
func FromEnv(origin string, getenv func(string) string) (Config, error) {
	slug := getenv("RASTRILLO_SLUG")
	fromHost := getenv("RASTRILLO_SLUG_FROM_HOST") == "1"
	switch {
	case slug == "" && !fromHost:
		return Config{}, nil
	case slug != "" && fromHost:
		return Config{}, errors.New("rastrillo/basepath: both RASTRILLO_SLUG and RASTRILLO_SLUG_FROM_HOST are set; set one")
	case fromHost:
		host := getenv("CARLOS_HOST")
		if host == "" {
			return Config{}, errors.New("rastrillo/basepath: RASTRILLO_SLUG_FROM_HOST=1 but CARLOS_HOST is empty; set RASTRILLO_SLUG instead outside an instance")
		}
		var err error
		if slug, err = SlugFromHost(host); err != nil {
			return Config{}, err
		}
	}
	c := Config{Origin: origin, Slug: slug}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// SlugFromHost returns the slug in an instance hostname's first label,
// acme-x7k2pq.bdf.oncarlos.com giving acme. The slug is 1 to 25
// characters, the most provision.NewLabel keeps.
func SlugFromHost(host string) (string, error) {
	label, _, _ := strings.Cut(host, ".")
	m := labelRe.FindStringSubmatch(label)
	if m == nil {
		return "", fmt.Errorf("rastrillo/basepath: host %q does not start with an instance label, <slug>-<6 letters>, slug at most 25 characters; set RASTRILLO_SLUG instead", host)
	}
	return m[1], nil
}

// Join puts base in front of p when p is a root-absolute path: one that
// starts with exactly one /. Anything else (a relative path, an absolute
// or scheme-relative URL, or /\, which browsers read as //) is returned
// unchanged, and so is every p when base is empty. "/" gives base itself,
// and "/?q" or "/#f" give base followed by the query or fragment.
//
// This is the one copy of the prefix rule: Path, the header rewrite and
// the templates' path func all call it. A second copy would drift, and
// one that prefixed //host/x would turn a link to another site into a
// path on the hub.
func Join(base, p string) string {
	if base == "" || len(p) == 0 || p[0] != '/' {
		return p
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return p
	}
	if p == "/" || p[1] == '?' || p[1] == '#' {
		return base + p[1:]
	}
	return base + p
}

// authority is a host and port, compared the way a browser sends them.
type authority struct{ host, port string }

// originAuthority is origin's host and port, and the port its scheme
// implies when a Host leaves it off. ok is false for an origin Validate
// refuses.
func originAuthority(origin string) (a authority, defaultPort string, ok bool) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return authority{}, "", false
	}
	defaultPort = "443"
	if u.Scheme == "http" {
		defaultPort = "80"
	}
	return receivedAuthority(u.Host, defaultPort), defaultPort, true
}

// receivedAuthority splits a Host header. A browser may send the port or
// leave the default off, any case, and a trailing dot (example.com. is
// the same name, fully qualified).
func receivedAuthority(hostport, defaultPort string) authority {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]"), ""
	}
	if port == "" {
		port = defaultPort
	}
	return authority{strings.TrimSuffix(strings.ToLower(host), "."), port}
}

// sameHost reports whether received, a request's Host, names origin.
//
// It reads Host and never X-Forwarded-Host: the edge preserves Host
// (edge.go:2402), and a header any client can send must not choose
// whether the app strips a base.
func sameHost(received, origin string) bool {
	o, def, ok := originAuthority(origin)
	return ok && receivedAuthority(received, def) == o
}

type ctxKey struct{}

// state is what Handler records about a request.
type state struct {
	base     string
	original string
	direct   bool
	platform bool
}

func from(r *http.Request) (state, bool) {
	s, ok := r.Context().Value(ctxKey{}).(state)
	return s, ok
}

// Base is the base path r was served under, such as /acme, or "" when
// it was not.
func Base(r *http.Request) string {
	s, _ := from(r)
	return s.base
}

// Path is p as a page must link to it: Join(Base(r), p). Use it for
// every root-absolute URL in a body. Never pass its result to
// http.Redirect or a Location header, which the middleware prefixes
// itself: the base would appear twice.
func Path(r *http.Request, p string) string { return Join(Base(r), p) }

// Original is the request target as the browser sent it, base included:
// r.RequestURI, which Handler leaves alone. Outside the middleware it is
// r.RequestURI, or r.URL's when that is empty, as in a test-built
// request.
func Original(r *http.Request) string {
	if s, ok := from(r); ok {
		return s.original
	}
	if r.RequestURI != "" {
		return r.RequestURI
	}
	return r.URL.RequestURI()
}

// OnDirectHost reports whether r came in on a host other than the
// product origin's, such as the instance's own hostname.
func OnDirectHost(r *http.Request) bool {
	s, _ := from(r)
	return s.direct
}

// PathRouted reports whether r is served under a base path setup at
// all: under the base, or on the direct host.
func PathRouted(r *http.Request) bool { return Base(r) != "" || OnDirectHost(r) }

// PlatformCall reports whether r is the platform's own call to the
// instance, such as a wake check or a health probe, which is answered at
// the root and never redirected.
func PlatformCall(r *http.Request) bool {
	s, _ := from(r)
	return s.platform
}

// ProductURL is the absolute URL of r's page on the product origin, or
// "" when there is none.
func ProductURL(r *http.Request) string { return "" }
