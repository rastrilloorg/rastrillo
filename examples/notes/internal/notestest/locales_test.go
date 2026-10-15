// locales_test.go is the example in three offered locales (spec §10,
// §12 item 8): the hand-written notes index (templates/layout.html)
// and a generated bookmarks screen (templates/genlayout.html, so both
// of this app's layouts render through ui.Pages), plus the job-status
// fragment endpoint (render.go's fragPages, a second ui.Pages with its
// own Entry), all from one template set, rendered by the request's
// locale.
package notestest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"

	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
)

// The example in three locales: the notes index, the bookmarks list
// and the job-status fragment, rendered in English and Spanish by the
// request's locale, with Arabic checked for direction. App keys
// (this app ships no locales/<code>.toml of its own) and the hand
// screens' own literal strings have no Spanish or Arabic catalog, so
// they stay in English; the framework's chrome (the switcher) renders
// in each reader's language.
func TestNotesInThreeLocales(t *testing.T) {
	ts := newAppWith(t, "en", []string{"en", "es", "ar"})
	cl := newClient(t, ts)
	cl.signup("erin@example.com", "hunter2222").Body.Close()
	cl.postForm("/bookmarks", url.Values{"Title": {"Go"}, "Link": {"https://go.dev"}}).Body.Close()
	for _, title := range exportTitles {
		cl.postForm("/notes", url.Values{"title": {title}, "body": {"Body of " + title}}).Body.Close()
	}

	for _, c := range []struct{ index, bookmarks, locale string }{
		{"/", "/bookmarks", "en"},
		{"/es/", "/es/bookmarks", "es"},
	} {
		want := rastrillo.BaseCatalogs()[c.locale]["rastrillo.ui.shell_language"]
		idx := body(t, cl.get(c.index))
		if !strings.Contains(idx, want) {
			t.Errorf("%s: missing %q in:\n%s", c.index, want, idx)
		}
		bm := body(t, cl.get(c.bookmarks))
		if !strings.Contains(bm, want) {
			t.Errorf("%s: missing %q in:\n%s", c.bookmarks, want, bm)
		}
	}
	// The real embedded layouts rendered strictly, in the request's
	// language and direction, with the switcher — both of them: the
	// hand index (layout.html) and the generated bookmarks list
	// (genlayout.html).
	esIndex := body(t, cl.get("/es/"))
	wantContains(t, esIndex, `<html lang="es" dir="ltr">`)
	wantContains(t, esIndex, "rst-locale")
	wantContains(t, body(t, cl.get("/ar/bookmarks")), `<html lang="ar" dir="rtl">`)

	// The fragment endpoint renders through ui.Pages too: fragPages, a
	// second instance built because a fragment's Entry ("job-status")
	// differs from every other page's ("layout") — render.go's
	// newFragPages doc comment. Polled under /es/ while the job is
	// still running (five notes' 300ms-apiece pace, as export_test.go's
	// own TestExportFragmentWhileRunning times it).
	start := cl.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()
	frag := cl.get("/es" + jobPath + "/fragment")
	fragBody := body(t, frag)
	if frag.StatusCode != http.StatusOK {
		t.Fatalf("es fragment status = %d, want 200; body=%s", frag.StatusCode, fragBody)
	}
	wantContains(t, fragBody, "data-poll")
	pollJobDone(t, cl, jobPath).Body.Close()

	one := newAppWith(t, "en", nil)
	oneCl := newClient(t, one)
	oneCl.signup("mo@example.com", "hunter2222").Body.Close()
	if b := body(t, oneCl.get("/")); strings.Contains(b, "rst-locale") || !strings.Contains(b, `<html lang="en" dir="ltr">`) {
		t.Errorf("a one-locale notes has a switcher or the wrong language:\n%.400s", b)
	}
}

func wantContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("missing %q in:\n%s", want, body)
	}
}
