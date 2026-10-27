package live

import (
	"context"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

type ctxKey struct{}

type baseKey struct{}

// WithBase returns r recording base, the path prefix the request was
// served under (such as /acme), so View.Head can point the page at the
// stream under it. The middleware that strips the base calls it
// (rastrillo.Handler does, for Options.BasePath): live imports only the
// standard library, so it cannot ask basepath. An empty base returns r.
func WithBase(r *http.Request, base string) *http.Request {
	if base == "" {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), baseKey{}, base))
}

// Middleware records the hub's sequence when the request starts, before
// any handler queries anything. Versions rendered later are read
// against that point, so a write that lands mid-request shows as newer
// and costs one extra fetch, never a missed change. A nil hub passes
// the request through.
func (h *Hub) Middleware(next http.Handler) http.Handler {
	if h == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKey{}, h.currentSeq())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// View is a request's view of the hub, for templates. The zero View
// renders nothing, so a page without live streams is static.
type View struct {
	hub   *Hub
	start uint64
	base  string
}

// View returns the request's View. Without Middleware the start is 0,
// which reads every changed topic as one older than it is: conservative
// by one fetch, never wrong the other way. A nil hub gives the zero
// View.
func (h *Hub) View(r *http.Request) View {
	if h == nil {
		return View{}
	}
	start, _ := r.Context().Value(ctxKey{}).(uint64)
	base, _ := r.Context().Value(baseKey{}).(string)
	return View{hub: h, start: start, base: base}
}

// Attrs renders the data-live and data-live-boot attributes for a
// region that shows these topics. A topic that is undeclared or
// malformed is logged and left out, since the client could only have
// the stream refuse it. If none remain, Attrs renders nothing.
func (v View) Attrs(topics ...string) template.HTMLAttr {
	if v.hub == nil {
		return ""
	}
	tokens := make([]string, 0, len(topics))
	for _, t := range topics {
		name, _, ok := parseTopic(t)
		if _, declared := v.hub.cfg.Topics[name]; !ok || !declared {
			slog.Warn("live: Attrs skipped an undeclared or malformed topic", "topic", t)
			continue
		}
		tokens = append(tokens, t+"@"+strconv.FormatUint(v.hub.version(t, v.start), 10))
	}
	if len(tokens) == 0 {
		return ""
	}
	// Topics and the boot ID are restricted to [A-Za-z0-9/_@ -], so
	// there is nothing to escape.
	return template.HTMLAttr(`data-live="` + strings.Join(tokens, " ") + `" data-live-boot="` + v.hub.boot + `"`)
}

// Head renders the meta tag that tells the page script where the
// stream is. paused and unconfirmed are the page's already-translated
// notes. No tag means no stream. Under a base (WithBase) the path
// carries it: the app mounts the stream at Config.Path, but the browser
// must ask for /acme/live, since a bare /live is the hub's, or another
// tenant's.
func (v View) Head(paused, unconfirmed string) template.HTML {
	if v.hub == nil {
		return ""
	}
	return template.HTML(`<meta name="rastrillo-live" content="` + html.EscapeString(withBase(v.base, v.hub.cfg.Path)) +
		`" data-paused="` + html.EscapeString(paused) +
		`" data-unconfirmed="` + html.EscapeString(unconfirmed) + `">`)
}

// withBase is basepath.Join's rule, copied because live imports only
// the standard library: only a path starting with exactly one / is the
// app's own. Prefixing //host/x or /\host/x, which browsers read as
// another origin, would send the page to a path that serves nothing.
func withBase(base, p string) string {
	if base == "" || len(p) == 0 || p[0] != '/' {
		return p
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return p
	}
	return base + p
}
