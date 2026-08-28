package rastrillo

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/carlosframework/rastrillo/csrf"
)

// LocaleCookie is the stored-preference cookie the resolution chain
// consults right after the path prefix, before Accept-Language. Design doc §10
// names "a stored preference" without naming the mechanism; a cookie is the
// only one that survives §9's zero-JS baseline.
const LocaleCookie = "rastrillo_locale"

type localeCtxKey struct{}
type localesCtxKey struct{}

// Middleware resolves this request's locale and puts it, and this set,
// on the request context for LocaleFrom/T/Tf.
//
// Precedence: URL path prefix, then the stored-preference cookie, then
// Accept-Language, then the default. The original design doc (§10) put
// the cookie last; that was reversed on 2026-08-28 when the framework
// started writing the cookie itself (SwitchHandler) — a stored choice
// that Accept-Language could override on the next request would make
// the switcher decorative.
//
// A matched locale prefix is stripped from the path before the app's
// mux sees it, so one route serves every locale. §10's zero-JS locale
// switch is "a plain link to the same path under a different locale
// prefix", which only works if /fr/orders and /orders reach the same
// handler.
func (l *Locales) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, rest := l.splitPrefix(r.URL.Path)
		if code == "" {
			if c, err := r.Cookie(LocaleCookie); err == nil && l.Has(c.Value) {
				code = c.Value
			}
		}
		if code == "" {
			code = l.negotiate(r.Header.Get("Accept-Language"))
		}
		if code == "" {
			code = l.def
		}

		ctx := context.WithValue(r.Context(), localeCtxKey{}, code)
		ctx = context.WithValue(ctx, localesCtxKey{}, l)
		// Shallow copy (the http.StripPrefix shape), never mutate: only
		// the context and sometimes the URL change, so a full r.Clone —
		// which also deep-copies Header — buys nothing but ~15-20 allocs
		// on every request. Copy the URL itself before writing to it, so
		// the caller's URL, and any rewritten path, never leaks out.
		r2 := r.WithContext(ctx)
		if rest != "" {
			u := *r.URL
			u.Path = rest
			u.RawPath = ""
			// EscapedPath() recomputes from Path when RawPath is empty,
			// which decodes any %2F back to a literal slash:
			// /fr/files/a%2Fb would then route as /files/a/b (404)
			// instead of matching the same route as the unprefixed
			// /files/a%2Fb. Carry the locale-stripped RawPath over
			// instead, but only when the locale segment provably lines
			// up in both forms — i.e. RawPath actually starts with the
			// literal, unescaped "/"+code+"/". Otherwise the locale
			// boundary in Path (decoded) may not be the same boundary
			// in RawPath (still encoded): a raw path like
			// /fr%2Fxx/files/a decodes to /fr/xx/files/a, so splitPrefix
			// matches "fr" and trims it from Path — but TrimPrefix on
			// the raw form would only strip "/fr", leaving "%2Fxx/..."
			// with no leading slash. That still decode-equals the
			// trimmed Path, so net/url's validity check passes it
			// through, and EscapedPath/RequestURI/String all come out
			// non-absolute. The clean lossy fallback (RawPath stays "",
			// re-encode from Path) is correct there.
			if strings.HasPrefix(r.URL.RawPath, "/"+code+"/") {
				u.RawPath = strings.TrimPrefix(r.URL.RawPath, "/"+code)
			}
			r2.URL = &u
		}
		next.ServeHTTP(w, r2)
	})
}

// splitPrefix returns the declared locale named by the path's first
// segment and the path with that segment removed, or ("", "") when the
// first segment is not a declared locale.
func (l *Locales) splitPrefix(p string) (code, rest string) {
	if !strings.HasPrefix(p, "/") {
		return "", ""
	}
	seg := p[1:]
	rest = "/"
	if i := strings.Index(seg, "/"); i >= 0 {
		seg, rest = seg[:i], seg[i:]
	}
	if seg == "" || !l.Has(seg) {
		return "", ""
	}
	return seg, rest
}

// negotiate picks the best declared locale for an Accept-Language
// header. Prefs are walked in q order (highest first, declaration order
// breaking ties); for each pref, exact tag match is tried before
// primary-subtag match, so an "fr-CA" request lands on a declared "fr"
// or "fr-informal". The nesting must stay pref-outer, match-kind-inner:
// the reverse (every pref's exact match before any pref's subtag match)
// would let a low-q exact match beat a high-q subtag match.
func (l *Locales) negotiate(header string) string {
	type pref struct {
		tag string
		q   float64
	}
	var prefs []pref
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			for _, p := range strings.Split(part[i+1:], ";") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						q = f
					}
				}
			}
		}
		if tag == "" || q <= 0 {
			continue
		}
		prefs = append(prefs, pref{strings.ToLower(tag), q})
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].q > prefs[j].q })

	for _, p := range prefs {
		for _, c := range l.codes {
			if strings.EqualFold(c, p.tag) {
				return c
			}
		}
		for _, c := range l.codes {
			if strings.EqualFold(primarySubtag(c), primarySubtag(p.tag)) {
				return c
			}
		}
	}
	return ""
}

func primarySubtag(tag string) string {
	if i := strings.Index(tag, "-"); i >= 0 {
		return tag[:i]
	}
	return tag
}

// LocaleFrom returns the locale Middleware resolved for r, or "" if the
// request never went through it.
func LocaleFrom(r *http.Request) string {
	code, _ := r.Context().Value(localeCtxKey{}).(string)
	return code
}

// T translates key in the request's resolved locale — the lookup an
// action calls. Outside a request that went through Middleware it
// returns the key verbatim rather than guessing a locale.
func T(r *http.Request, key string) string {
	l, ok := r.Context().Value(localesCtxKey{}).(*Locales)
	if !ok {
		return key
	}
	return l.T(LocaleFrom(r), key)
}

// Tf is T plus {name} placeholder interpolation. See (*Locales).Tf for
// the accepted argument forms.
func Tf(r *http.Request, key string, args ...any) string {
	l, ok := r.Context().Value(localesCtxKey{}).(*Locales)
	if !ok {
		return interpolate(key, args)
	}
	return l.Tf(LocaleFrom(r), key, args...)
}

// LocaleSwitchPath is the framework route the language switcher POSTs
// to (spec §2.4). Mounted by Serve whenever Options.Locales is set.
const LocaleSwitchPath = "/_locale"

// LocaleItem is one entry of the language switcher: the declared code,
// its autonym (rastrillo.ui.locale_name in that locale, or the code
// when no catalog names it), a plain link to the same path under that
// locale's prefix, and whether it is the request's locale.
type LocaleItem struct {
	Code    string
	Name    string
	Href    string
	Current bool
}

// LocaleItems builds the switcher's data for r. Empty when the request
// never went through Middleware or the app declares one locale — the
// partial renders nothing for an empty list, so a one-locale app can
// call it unconditionally.
func LocaleItems(r *http.Request) []LocaleItem {
	l, ok := r.Context().Value(localesCtxKey{}).(*Locales)
	if !ok || len(l.codes) < 2 {
		return nil
	}
	cur := LocaleFrom(r)
	rest := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		rest += "?" + r.URL.RawQuery
	}
	items := make([]LocaleItem, 0, len(l.codes))
	for _, c := range l.codes {
		items = append(items, LocaleItem{
			Code:    c,
			Name:    l.autonym(c),
			Href:    "/" + c + rest,
			Current: c == cur,
		})
	}
	return items
}

// autonym is the language's own name for itself, and deliberately not a
// T lookup: T falls back through the default locale and the base
// English catalog, so a declared locale the framework does not ship —
// "fr", say — would come back labelled "English", which is exactly the
// entry a reader looking for their language must be able to find. Only
// a catalog for that very locale can name it, so this consults the
// app's catalog for the code, then the framework's, then gives up and
// shows the code.
func (l *Locales) autonym(code string) string {
	const key = "rastrillo.ui.locale_name"
	if v, ok := l.app[code][key]; ok {
		return v
	}
	if v, ok := l.fw[code][key]; ok {
		return v
	}
	return code
}

// SwitchHandler answers POST /_locale: it stores the chosen locale in
// LocaleCookie and 303s to the return path under that locale's prefix.
// Same-origin is checked the way every mutating route in this
// framework checks it (csrf.SameOrigin), with the origin taken from the
// request itself — the handler has no configured origin and needs
// none, because the check is "did a page of ours submit this".
func (l *Locales) SwitchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		if !csrf.SameOrigin(r, scheme+"://"+r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		code := r.PostFormValue("locale")
		if !l.Has(code) {
			http.Error(w, "unknown locale", http.StatusBadRequest)
			return
		}
		ret := r.PostFormValue("return")
		if ret == "" {
			ret = "/"
		}
		if !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") || strings.HasPrefix(ret, "/\\") {
			http.Error(w, "bad return path", http.StatusBadRequest)
			return
		}
		// A return path that already carries a locale prefix loses it,
		// so switching from /fr/orders lands on /en/orders, not
		// /en/fr/orders.
		if _, rest := l.splitPrefix(strings.SplitN(ret, "?", 2)[0]); rest != "" {
			if i := strings.Index(ret, "?"); i >= 0 {
				rest += ret[i:]
			}
			ret = rest
		}
		http.SetCookie(w, &http.Cookie{
			Name: LocaleCookie, Value: code, Path: "/",
			MaxAge: 365 * 24 * 3600, HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: scheme == "https",
		})
		http.Redirect(w, r, "/"+code+ret, http.StatusSeeOther)
	})
}
