package flash_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/flash"
)

func TestAFlashBelongsToTheAppThatSetIt(t *testing.T) {
	// One httptest.Server: a front that sends /acme/... and /bob/... through two basepath handlers
	// (slugs acme and bob) and everything else to a root handler. A cookiejar client:
	// POST /acme/save does flash.Set(w, r, "notice", "Saved."), 303 to /acme/.
	// GET / and GET /bob/ call flash.Take: neither sees a flash.
	// GET /acme/ calls flash.Take: "Saved."; a second GET /acme/ sees none (the clear matched the path).
	// POST /hub-save at the root sets "Hub." and 303s to /; before GET / takes it, GET /acme/ sees
	// no flash and leaves the hub's cookie alone, and GET / then shows "Hub." once.
	// With both pending at once, each app shows its own notice once.
	// At the root, Set and Take behave as before (rastrillo_flash, Path "/").

	// Every app is the same code: POST /save sets the flash named by ?msg=
	// and redirects to "/", GET / reports what Take returned.
	app := http.NewServeMux()
	app.HandleFunc("POST /save", func(w http.ResponseWriter, r *http.Request) {
		flash.Set(w, r, "notice", r.URL.Query().Get("msg"))
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	app.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		f, ok := flash.Take(w, r)
		if !ok {
			io.WriteString(w, "none")
			return
		}
		io.WriteString(w, f.Kind+": "+f.Message)
	})

	// The server's URL is the product origin each basepath handler
	// checks Host against, so the front is built after it starts.
	front := http.NewServeMux()
	srv := httptest.NewServer(front)
	defer srv.Close()
	origin := srv.URL
	for _, slug := range []string{"acme", "bob"} {
		h := basepath.Config{Origin: origin, Slug: slug}.Handler(app)
		front.Handle("/"+slug, h)
		front.Handle("/"+slug+"/", h)
	}
	front.Handle("/", app)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	do := func(method, path string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(method, origin+path, strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	// saves posts a flash to base and checks it redirected back to the page.
	saves := func(base, msg, wantName, wantPath string) {
		t.Helper()
		resp, _ := do("POST", base+"/save?msg="+url.QueryEscape(msg))
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST %s/save = %d, want 303", base, resp.StatusCode)
		}
		want := base
		if want == "" {
			want = "/"
		}
		if loc := resp.Header.Get("Location"); loc != want {
			t.Errorf("POST %s/save redirects to %q, want %q", base, loc, want)
		}
		cs := resp.Cookies()
		if len(cs) != 1 || cs[0].Name != wantName || cs[0].Path != wantPath {
			t.Errorf("POST %s/save set %v, want one cookie %s with Path=%s", base, cs, wantName, wantPath)
		}
	}
	sees := func(path, want string) {
		t.Helper()
		resp, body := do("GET", path)
		if body != want {
			t.Errorf("GET %s showed %q, want %q", path, body, want)
		}
		if want == "none" && len(resp.Cookies()) != 0 {
			t.Errorf("GET %s with no flash of its own wrote %v; it must leave other apps' cookies alone", path, resp.Cookies())
		}
	}

	saves("/acme", "Saved.", "rastrillo_flash_acme", "/acme")
	sees("/", "none")
	sees("/bob/", "none")
	sees("/acme/", "notice: Saved.")
	sees("/acme/", "none")

	saves("", "Hub.", "rastrillo_flash", "/")
	sees("/acme/", "none")
	sees("/bob/", "none")
	sees("/", "notice: Hub.")
	sees("/", "none")

	saves("/acme", "For acme.", "rastrillo_flash_acme", "/acme")
	saves("/bob", "For bob.", "rastrillo_flash_bob", "/bob")
	saves("", "For the hub.", "rastrillo_flash", "/")
	sees("/bob/", "notice: For bob.")
	sees("/", "notice: For the hub.")
	sees("/acme/", "notice: For acme.")
	for _, p := range []string{"/", "/acme/", "/bob/"} {
		sees(p, "none")
	}
}
