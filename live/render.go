package live

import (
	"bytes"
	"net/http"
	"sync"
)

// Render answers a request for a page whose templates are t, run from
// entry ("layout"), as the whole page or as the regions a fragment
// request names, in the render order the package doc gives. status is
// the response's status, 0 meaning 200; a fragment answer keeps it.
//
// data builds the page data. Render calls it once, with whether this
// render shows the notice and so may take the flash, and never touches
// the flash itself. It is not called at all when the answer is a
// navigation (Elsewhere, or a GET for regions the page lacks), so the
// flash survives for the page navigated to.
//
// Everything is rendered into a buffer first. An error from data or
// from a template is returned with nothing written, so the caller can
// send its 500. An error writing the buffer to w can come after the
// status and headers are committed, when a 500 is too late.
// ui.Pages.Render is the same for an app built on ui.Pages.
func Render(
	w http.ResponseWriter, r *http.Request, status int, t Templates,
	entry string, data func(takeFlash bool) (any, error),
) error {
	if Elsewhere(w, r) {
		return nil
	}
	// Every page carries the notice region, so any of them may answer
	// as a fragment, and a cache must never hand one out for the other.
	NoStore(w)
	names, frag := Wanted(r)
	if frag && !Has(t, names) {
		if navigable(r) {
			return Respond(w, r, nil, nil)
		}
		// A form whose error render is another page (a 422 that
		// renders "new" for a form on the list): the whole page, with
		// its status, which live.js shows as the browser would. An
		// empty notice in its place would show nothing happening.
		frag = false
	}
	// Decided only now: a request that ended in a navigation above
	// leaves the flash for the page it lands on, and a stream refetch
	// that does not show the notice must not eat a message.
	d, err := data(!frag || TakesFlash(r))
	if err != nil {
		return err
	}
	if frag {
		// WriteRegions sets Rastrillo-Regions before the first body
		// byte, and statusWriter sends the status with that byte:
		// written here, the status would commit the headers without it.
		return WriteRegions(&statusWriter{ResponseWriter: w, status: status}, r, t, names, d)
	}
	b := pageBuffers.Get().(*bytes.Buffer)
	defer func() {
		if b.Cap() <= maxPooledBuffer {
			b.Reset()
			pageBuffers.Put(b)
		}
	}()
	b.Reset()
	// Buffered because html/template writes as it executes: a failure
	// halfway would leave half a page on the wire under a 200.
	if err := t.ExecuteTemplate(b, entry, d); err != nil {
		return err
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	_, err = b.WriteTo(w)
	return err
}

// pageBuffers holds Render's whole-page buffers between requests.
var pageBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxPooledBuffer keeps one unusually large page from pinning its
// buffer in the pool for the life of the process.
const maxPooledBuffer = 1 << 20

// statusWriter writes status, when it is not 0, just before the first
// body byte, unless something wrote a status first (Respond's 204).
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote && s.status != 0 {
		s.WriteHeader(s.status)
	}
	s.wrote = true
	return s.ResponseWriter.Write(b)
}
