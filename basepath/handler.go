package basepath

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Handler serves next under c's base path. On the product origin it
// answers only for /<slug> and below: it strips the base from r.URL,
// records it for Base and Path, and adds it back to the Location and
// Rastrillo-Location headers next sends.
func (c Config) Handler(next http.Handler) http.Handler {
	base := "/" + c.Slug
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameHost(r.Host, c.Origin) {
			next.ServeHTTP(w, r)
			return
		}
		serveProduct(w, r, base, next)
	})
}

// serveProduct serves a request on the product origin.
func serveProduct(w http.ResponseWriter, r *http.Request, base string, next http.Handler) {
	// Clean the escaped form, so %2F, %3F and %23 stay bytes of a
	// segment: a canonical redirect that decoded them would turn an
	// escape into a delimiter, and /files/a%2Fb would route as
	// /files/a/b (localemw.go:72-89).
	esc := r.URL.EscapedPath()
	clean := cleanPath(esc)

	// This instance answers only for its own slug, and fails closed
	// when a path map sends it someone else's: the hub's pages, or
	// another tenant's, must not render as this tenant's.
	if clean != base && !strings.HasPrefix(clean, base+"/") {
		http.NotFound(w, r)
		return
	}

	if clean != esc {
		// 301 is the permanent move. It turns a POST into a GET in
		// every browser, though, so a form posted to an unclean path
		// would lose its body and look as if it saved; 308 keeps the
		// method and body (localemw.go:41-51). The Location is written
		// directly: it is already public, and must not get the base
		// twice.
		status := http.StatusMovedPermanently
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			status = http.StatusPermanentRedirect
		}
		to := clean
		if r.URL.RawQuery != "" {
			to += "?" + r.URL.RawQuery
		}
		w.Header().Set("Location", to)
		w.WriteHeader(status)
		return
	}

	restEsc := strings.TrimPrefix(clean, base)
	if restEsc == "" {
		restEsc = "/"
	}
	rest, err := url.PathUnescape(restEsc)
	if err != nil {
		// EscapedPath only returns valid escapes, so this is a request
		// net/http should already have refused.
		http.Error(w, "bad request path", http.StatusBadRequest)
		return
	}

	// Shallow copy, the http.StripPrefix shape (localemw.go:66-96):
	// only the context and the URL change. RequestURI keeps what the
	// browser sent, base included, so a return path built from it is
	// public.
	ctx := context.WithValue(r.Context(), ctxKey{}, state{base: base, original: Original(r)})
	r2 := r.WithContext(ctx)
	u := *r.URL
	u.Path = rest
	// net/url uses RawPath when it is a valid encoding of Path, and
	// recomputes it otherwise, so an escaped slash survives the strip.
	u.RawPath = restEsc
	r2.URL = &u

	rw := &rewriter{ResponseWriter: w, base: base}
	next.ServeHTTP(rw, r2)
	rw.finish()
}

// cleanPath is path.Clean keeping a trailing slash, the way net/http's
// ServeMux cleans, so the canonical form here is the one the mux would
// redirect to anyway.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}

// rewritten are the headers that carry an app path for the browser to
// follow.
var rewritten = [...]string{"Location", "Rastrillo-Location"}

// rewriter adds the base to Location and Rastrillo-Location as the
// response is committed. It is a writer and not a helper for callers to
// remember because some redirects are written by no app code: ServeMux's
// trailing-slash and clean-path 301s, and http.Redirect resolving a
// relative target against the stripped path. One known gap:
// http.Redirect's fallback link in the body comes from its argument, so
// it lacks the base; browsers follow the header.
//
// It is modelled on perf's request writer (perf/perf.go:395-460), and
// passes Flush, Hijack and Unwrap through the same way.
type rewriter struct {
	http.ResponseWriter
	base      string
	committed bool
	hijacked  bool
}

// fix rewrites the headers in place, once per response.
func (w *rewriter) fix() {
	h := w.Header()
	for _, k := range rewritten {
		vs := h[k]
		if len(vs) == 0 {
			continue
		}
		// A new slice, so a caller that saved the old one keeps it.
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = Join(w.base, v)
		}
		h[k] = out
	}
}

// commit runs fix the first time the response's headers go out.
func (w *rewriter) commit() {
	if w.committed || w.hijacked {
		return
	}
	w.committed = true
	w.fix()
}

func (w *rewriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols && !w.committed && !w.hijacked {
		// An informational response goes out now and the real one
		// follows, from the same header map. Put the app's values back
		// afterwards, so the final response is rewritten from what the
		// app set, not from what a 103 already carried: otherwise
		// every 1xx would add the base again.
		h := w.Header()
		var saved [len(rewritten)][]string
		for i, k := range rewritten {
			saved[i] = h[k]
		}
		w.fix()
		w.ResponseWriter.WriteHeader(status)
		for i, k := range rewritten {
			if saved[i] == nil {
				delete(h, k)
			} else {
				h[k] = saved[i]
			}
		}
		return
	}
	w.commit()
	w.ResponseWriter.WriteHeader(status)
}

func (w *rewriter) Write(b []byte) (int, error) {
	w.commit()
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (w *rewriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *rewriter) Flush() { _ = w.FlushError() }

// FlushError commits first: a flush sends the headers.
func (w *rewriter) FlushError() error {
	w.commit()
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack hands the connection over. The handler writes its own bytes
// from then on, so no header is rewritten, now or in finish.
func (w *rewriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

// finish rewrites the headers of a handler that returned without
// writing anything, before net/http sends its implicit 200.
func (w *rewriter) finish() { w.commit() }
