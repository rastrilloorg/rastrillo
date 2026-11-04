package chattest

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"

	genlocales "chat/gen/locales"
	"chat/internal/chat"
)

var hashedStylesheet = regexp.MustCompile(`/static/tokens\.[0-9a-f]{16}\.css`)

// The home page lists the rooms, and there are none in a new database.
func TestIndexRenders(t *testing.T) {
	t.Parallel()
	recorder := get(t, newApp(t), "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /: status %d, want 200. Behind sign-in this "+
			"placeholder test fails: repoint or delete it", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "No rooms yet") {
		t.Errorf("GET /: no empty room list:\n%s", body)
	}
	catalog := rastrillo.BaseCatalogs()[genlocales.DefaultLocale]
	skip := template.HTMLEscapeString(catalog["rastrillo.ui.shell_skip"])
	if !strings.Contains(body, skip) {
		t.Errorf("GET /: no skip link in %s, the default language:\n%s",
			genlocales.DefaultLocale, body)
	}
}

func TestIndexLinksFingerprintedStylesheet(t *testing.T) {
	t.Parallel()
	handler := newApp(t)
	href := hashedStylesheet.FindString(get(t, handler, "/").Body.String())
	if href == "" {
		t.Fatal("the index page links no fingerprinted stylesheet. Behind " +
			"sign-in it redirects: point this test at a public page")
	}
	recorder := get(t, handler, href)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", href, recorder.Code)
	}
	const year = "public, max-age=31536000, immutable"
	if got := recorder.Header().Get("Cache-Control"); got != year {
		t.Errorf("GET %s: Cache-Control = %q, want immutable year", href, got)
	}
}

// The bare name is never cached long, so a changed file shows on reload.
func TestBareAssetNameStaysFresh(t *testing.T) {
	t.Parallel()
	recorder := get(t, newApp(t), "/static/tokens.css")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /static/tokens.css: status %d, want 200",
			recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}

// A panic's page is the framework's error-page partial, not net/http's.
func TestErrorPageRendersFrameworkPartial(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	chat.ErrorPage(recorder, request,
		http.StatusInternalServerError, "abc123")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("ErrorPage: status %d, want %d", recorder.Code,
			http.StatusInternalServerError)
	}
	catalog := rastrillo.BaseCatalogs()[genlocales.DefaultLocale]
	title := template.HTMLEscapeString(catalog["rastrillo.ui.error_500_title"])
	body := recorder.Body.String()
	if !strings.Contains(body, title) || !strings.Contains(body, "abc123") {
		t.Errorf("ErrorPage: no 500 title or ref:\n%s", body)
	}
}
