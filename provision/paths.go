package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// MaxPaths is the most prefixes the platform keeps on one hostname.
const MaxPaths = 10000

// ErrEmptyMap: the build produced no prefixes and nothing was sent.
//
// There is no way to override it. An empty build is far more often a
// failed or empty listing than a decision, and publishing it removes every
// tenant's address at once. The cost is that when the last live tenant
// fails, its prefix stays published, and may still reach that instance,
// until a person clears the map with a call to the same door
// ({"paths": {}}). That is a rare and visible state; a wrongly empty
// publish is a silent outage of every tenant.
var ErrEmptyMap = errors.New("provision: the path map is empty, so nothing was sent")

// Published is what the platform says of a paths PUT.
type Published struct {
	// Prefixes is the count the platform now holds.
	Prefixes int
	// Changed is false when the map was already the one published.
	Changed bool
}

// PublishPaths sends the hub's whole path map for product, the hostname the
// hub holds. build lists the tenants and returns prefix to target host.
//
// The platform's PUT replaces the map, so the listing and the PUT run
// under one lock per product hostname: two signups building from listings
// taken a moment apart would otherwise send their maps in either order,
// and the older would drop the newer's prefix. The lock is shared by every
// Console in the process. Across processes nothing serializes; the
// periodic publish repairs a map a second process overwrote.
//
// Nothing is sent when build fails (its error is returned as it is), when
// the map is empty (ErrEmptyMap), or when a prefix or target is one the
// platform would refuse (ErrBadRequest), so a bad map is never half
// applied and a failed listing is never read as "no tenants". A 500 or 502
// is retried once, since a wholesale PUT of the same map is safe to
// repeat; a second one, or a 429, is a *RetryError.
func (c *Console) PublishPaths(ctx context.Context, product string, build func(context.Context) (map[string]string, error)) (Published, error) {
	host, ok := normalHost(product)
	if !ok {
		return Published{}, badRequest("hostname %q is not a normalised hostname", product)
	}
	unlock, err := lockHost(ctx, host)
	if err != nil {
		return Published{}, err
	}
	defer unlock()

	paths, err := build(ctx)
	if err != nil {
		return Published{}, err
	}
	if len(paths) == 0 {
		return Published{}, ErrEmptyMap
	}
	if err := checkPaths(paths); err != nil {
		return Published{}, err
	}

	body := struct {
		Paths map[string]string `json:"paths"`
	}{paths}
	path := c.appPath("domains", host, "paths")
	for attempt := 1; ; attempt++ {
		status, hdr, msg, raw, err := c.call(ctx, http.MethodPut, path, body)
		if err != nil {
			// The caller gave up: its deadline, not the attempt's.
			if cerr := ctx.Err(); cerr != nil {
				return Published{}, cerr
			}
			return Published{}, &RetryError{Status: status, Message: msg, Err: err}
		}
		switch {
		case status == http.StatusOK:
			var out struct {
				Prefixes int  `json:"prefixes"`
				Changed  bool `json:"changed"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				// Not the handler's answer, so whether the map landed is
				// unknown.
				return Published{}, &RetryError{Status: status, Message: msg, Err: err}
			}
			return Published{Prefixes: out.Prefixes, Changed: out.Changed}, nil
		case (status == http.StatusInternalServerError || status == http.StatusBadGateway) && attempt == 1:
			continue
		case status == http.StatusTooManyRequests || status >= 500:
			return Published{}, retryError(status, hdr, msg)
		}
		return Published{}, &APIError{Status: status, Message: msg}
	}
}

// hostLocks holds one lock per product hostname, for the life of the
// process. It is package-level, not a Console field, so two Consoles for
// one hub serialize too. A hub holds a handful of product hostnames, so
// the map does not grow.
var hostLocks sync.Map // hostname -> chan struct{}

// lockHost takes the hostname's lock, or gives up with the context's error.
// A channel, not a Mutex: a publisher queued behind a slow listing ends
// with its own deadline instead of outliving it.
func lockHost(ctx context.Context, host string) (unlock func(), err error) {
	v, _ := hostLocks.LoadOrStore(host, make(chan struct{}, 1))
	ch := v.(chan struct{})
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func badRequest(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBadRequest}, args...)...)
}

// checkPaths refuses what the platform would refuse, before any call, so
// a map with one bad entry is named here and never half sent. Prefixes are
// checked in order so the error is the same on every run.
func checkPaths(paths map[string]string) error {
	if len(paths) > MaxPaths {
		return badRequest("%d path prefixes, at most %d", len(paths), MaxPaths)
	}
	prefixes := make([]string, 0, len(paths))
	for p := range paths {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		if !validPrefix(p) {
			return badRequest("path prefix %q is not a single bare segment", p)
		}
		if h, ok := normalHost(paths[p]); !ok || h != paths[p] {
			return badRequest("path prefix %q: target %q is not a normalised hostname", p, paths[p])
		}
	}
	return nil
}

// validPrefix is the platform's rule for a path prefix: one bare segment of
// letters, digits and "-_.~", at most 255 bytes. "." and ".." are out
// because any path cleaner between a browser and the edge removes them, so
// a row keyed on either could never be reached.
func validPrefix(p string) bool {
	if p == "" || len(p) > 255 || p == "." || p == ".." {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == '~':
		default:
			return false
		}
	}
	return true
}

// normalHost is the platform's NormalizeAliasHost: lower case, one trailing
// dot dropped, at most 253 bytes, a dot inside, and every label a DNS label.
// A target must also equal its own normal form, so the platform never
// stores a different string from the one this package sent.
func normalHost(raw string) (string, bool) {
	h := strings.TrimSuffix(strings.ToLower(raw), ".")
	if h == "" || len(h) > 253 || !strings.Contains(h, ".") {
		return "", false
	}
	for _, label := range strings.Split(h, ".") {
		if !dnsLabel.MatchString(label) {
			return "", false
		}
	}
	return h, true
}
