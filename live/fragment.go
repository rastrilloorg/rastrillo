package live

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"strings"
	"unicode"
)

// Notice is the reserved region name for the page's flash message. A
// render takes the flash only when it shows this region (TakesFlash).
const Notice = "notice"

const (
	headerFragment = "Rastrillo-Fragment"
	headerPage     = "Rastrillo-Page"
	headerRegions  = "Rastrillo-Regions"
	headerLocation = "Rastrillo-Location"
)

// maxRegions bounds the names one request may list, repeats included.
// A page has a handful of regions; without a bound, a header near
// net/http's 1 MB limit holds tens of thousands of names, and every
// route that calls Wanted would spend seconds on each such request.
const maxRegions = 32

// Wanted reports the regions a request asks for in Rastrillo-Fragment
// (space-separated, each listed once). ok is false when the header is
// absent, when it lists more than 32 names, or when any name is not a
// region name, so the jobs poll's legacy value "1" stays the jobs
// poll's and a bad request is answered as a full page rather than as a
// guess.
func Wanted(r *http.Request) (regions []string, ok bool) {
	var seen map[string]bool
	count := 0
	for _, v := range r.Header.Values(headerFragment) {
		// Scan one name at a time and stop at the 33rd. Splitting the
		// whole value first (strings.Fields) would build a slice of
		// every name in a header near net/http's 1 MB limit before
		// refusing it, which is the work the cap exists to avoid.
		for {
			v = strings.TrimLeftFunc(v, unicode.IsSpace)
			if v == "" {
				break
			}
			end := strings.IndexFunc(v, unicode.IsSpace)
			if end < 0 {
				end = len(v)
			}
			n := v[:end]
			v = v[end:]
			count++
			if count > maxRegions || !validRegion(n) {
				return nil, false
			}
			if seen == nil {
				seen = make(map[string]bool, 8)
			}
			// A header of one name repeated would otherwise render that
			// region once per repeat.
			if !seen[n] {
				seen[n] = true
				regions = append(regions, n)
			}
		}
	}
	if count == 0 {
		return nil, false
	}
	return regions, true
}

// Elsewhere answers a GET or HEAD that was meant for another page: the
// form's redirect went somewhere other than the page it was sent from,
// so it writes 204 and Rastrillo-Location (which the shim reads as
// "navigate there") and returns true; the caller returns. Call it
// first, before the flash is taken, so the page navigated to still
// shows the notice. Other methods never match: a POST is not a redirect
// target, and answering one with a 204 would hide a write's result.
func Elsewhere(w http.ResponseWriter, r *http.Request) bool {
	if !navigable(r) {
		return false
	}
	from := r.Header.Get(headerPage)
	if from == "" || from == r.URL.Path {
		return false
	}
	navigate(w, r)
	return true
}

// NoStore marks the response uncacheable and adds Rastrillo-Fragment to
// Vary, keeping what is already there. The same URL answers with a
// region or a whole page by header; a cache that did not know would
// serve one for the other, or an old region for a refetch.
func NoStore(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	for _, v := range h.Values("Vary") {
		for _, f := range strings.Split(v, ",") {
			f = strings.TrimSpace(f)
			if f == "*" || strings.EqualFold(f, headerFragment) {
				return
			}
		}
	}
	h.Add("Vary", headerFragment)
}

// TakesFlash reports whether this render shows the notice region, and
// so may take the flash: a full page always does, a fragment request
// only when it names Notice. A stream-triggered refresh never asks for
// it; if it took the flash it would eat a message it never displays.
func TakesFlash(r *http.Request) bool {
	regions, ok := Wanted(r)
	return !ok || contains(regions, Notice)
}

// Has reports whether the template defines a region:<name> for any of
// names other than Notice. Every layout defines the notice, so a match
// on it alone says nothing about whether this is the page that was
// asked: a form whose 422 renders another page would be answered with
// an empty notice, and the person would see nothing happen. Ask it
// before taking the flash. When it says no, a GET or HEAD is answered
// by navigating (Respond with nothing), and the page navigated to must
// still find the flash; any other method gets the whole page, with its
// status, as the browser would show it.
func Has(t *template.Template, names []string) bool {
	for _, n := range names {
		if n != Notice && t.Lookup("region:"+n) != nil {
			return true
		}
	}
	return false
}

// ExecuteRegions renders, in order, each of names that the template
// defines as region:<name>, and skips the rest. The prefix is the
// whole safety rule: a name from a request reaches only templates the
// page marked as regions, never "layout" or a partial. It buffers
// everything and writes nothing on error, because html/template writes
// as it executes and a failure halfway would leave a torn fragment
// under a 200. It is for apps with their own template map; ui.Pages
// has a method that checks the localizer first.
func ExecuteRegions(w io.Writer, t *template.Template, names []string, data any) (rendered []string, err error) {
	var b bytes.Buffer
	for _, n := range names {
		region := t.Lookup("region:" + n)
		if region == nil {
			continue
		}
		if err := region.Execute(&b, data); err != nil {
			return nil, err
		}
		rendered = append(rendered, n)
	}
	if _, err := b.WriteTo(w); err != nil {
		return nil, err
	}
	return rendered, nil
}

// WriteRegions is what an app with its own template map calls to answer
// a fragment request: it renders the regions (ExecuteRegions), sets
// Rastrillo-Regions, and only then writes the body. The order matters.
// Passing the ResponseWriter straight to ExecuteRegions would commit
// the headers with the first byte, and Answer's Rastrillo-Regions
// would be dropped silently. Nothing is sent on error, so the caller
// can write its 500. When no region was rendered, a GET or HEAD gets
// Answer's 204 and Rastrillo-Location, and no body.
func WriteRegions(w http.ResponseWriter, r *http.Request, t *template.Template, names []string, data any) error {
	var b bytes.Buffer
	rendered, err := ExecuteRegions(&b, t, names, data)
	if err != nil {
		return err
	}
	return Respond(w, r, rendered, b.Bytes())
}

// Respond is WriteRegions' second half, for callers that rendered the
// regions themselves (ui.Pages): headers first through Answer, then the
// body, if any region was rendered.
func Respond(w http.ResponseWriter, r *http.Request, rendered []string, body []byte) error {
	Answer(w, r, rendered)
	if len(rendered) == 0 {
		return nil
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, err := w.Write(body)
	return err
}

// Answer sets Rastrillo-Regions to what was rendered. When nothing was
// (a GET that named only regions this page lacks) it answers 204 and
// Rastrillo-Location, as Elsewhere does, and the caller writes no
// body. WriteRegions calls it for you; call it yourself only when you
// write the body some other way, and then before the first byte, since
// headers set after it are lost. Use Elsewhere before anything.
// A POST that renders nothing falls through with the empty header,
// because navigating would hide that the write happened.
func Answer(w http.ResponseWriter, r *http.Request, rendered []string) {
	w.Header().Set(headerRegions, strings.Join(rendered, " "))
	if len(rendered) == 0 && navigable(r) {
		navigate(w, r)
	}
}

func navigable(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

// navigate is uncacheable because a 204 is heuristically cacheable
// (RFC 9110 §15.1): a shared cache could replay "navigate there" to a
// plain navigation of the same URL, which would then show nothing.
func navigate(w http.ResponseWriter, r *http.Request) {
	NoStore(w)
	w.Header().Set(headerLocation, r.URL.RequestURI())
	w.WriteHeader(http.StatusNoContent)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// validRegion is ^[a-z][a-z0-9-]{0,63}$. The first character is a
// letter, so the jobs shim's legacy value "1" is never a region.
func validRegion(s string) bool {
	if len(s) == 0 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
