//go:build browser

package measure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Run by hand (go test -tags browser ./internal/measure/) with
// RASTRILLO_CHROME pointing at Chromium; the smoke round is what proves
// Measure against a real deploy.
func TestMeasureWeighsAPageAndRunsAxe(t *testing.T) {
	chrome := os.Getenv("RASTRILLO_CHROME")
	if chrome == "" {
		t.Skip("RASTRILLO_CHROME is not set")
	}
	axeJS, err := os.ReadFile("../../../../ui/testdata/axe/axe.min.js")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write([]byte(`<!doctype html><html lang="en"><title>t</title>
<link rel="stylesheet" href="/a.css"><script src="/a.js"></script>
<main><h1>Hello</h1><img src="/missing.png"></main></html>`))
	})
	mux.HandleFunc("/a.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte("body { margin: 0 }"))
	})
	mux.HandleFunc("/a.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte("console.log('a')"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	shots := t.TempDir()
	measurement, err := Measure(context.Background(),
		Options{Chrome: chrome, ShotsDir: shots, AxeJS: axeJS},
		Mapping{Pages: map[string]string{"home": server.URL + "/"}},
		[]Role{{Name: "home"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(measurement.Pages) != 1 {
		t.Fatalf("pages = %+v", measurement.Pages)
	}
	home := measurement.Pages[0]
	if home.Failure != "" {
		t.Fatalf("home failed: %s", home.Failure)
	}
	if home.Weight.Decoded[CSS] != 18 || home.Weight.Decoded[JS] != 16 {
		t.Errorf("weight = %+v", home.Weight)
	}
	if home.Axe["serious"]+home.Axe["critical"] == 0 {
		t.Errorf("axe found nothing on an image without alt: %v", home.Axe)
	}
	if measurement.Headers["x-content-type-options"] != "nosniff" {
		t.Errorf("headers = %v", measurement.Headers)
	}
	if !measurement.DeployOK {
		t.Error("DeployOK is false for a home page that answered")
	}
	if _, err := os.Stat(shots + "/home-390.png"); err != nil {
		t.Error(err)
	}
}

// After signing in, an app may land on /admin; measuring /admin#overdue
// from there is a jump within the page, which requests no document.
// Each measured page must be a real load.
func TestMeasureLoadsAFragmentPageAfterSignIn(t *testing.T) {
	chrome := os.Getenv("RASTRILLO_CHROME")
	if chrome == "" {
		t.Skip("RASTRILLO_CHROME is not set")
	}
	axeJS, err := os.ReadFile("../../../../ui/testdata/axe/axe.min.js")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /signin", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html lang="en"><title>in</title><main>
<form method="post" action="/signin"><label>User <input id="u" name="u"></label>
<label>Password <input id="p" name="p" type="password"></label>
<button id="go">Sign in</button></form></main></html>`))
	})
	mux.HandleFunc("POST /signin", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1", Path: "/"})
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		// An admin page may well have its own password field (adding a
		// member), which must not read as a failed sign-in.
		w.Write([]byte(`<!doctype html><html lang="en"><title>admin</title><main><h1 id="overdue">Overdue</h1>
<form method="post" action="/members"><label>New password <input name="p" type="password"></label></form></main></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	measurement, err := Measure(context.Background(),
		Options{Chrome: chrome, AxeJS: axeJS},
		Mapping{
			Pages: map[string]string{"overdue": server.URL + "/admin#overdue"},
			SignIn: SignIn{URL: server.URL + "/signin", Username: "a", Password: "b",
				UsernameSelector: "#u", PasswordSelector: "input[name=p]", SubmitSelector: "#go"},
		},
		[]Role{{Name: "overdue", SignedIn: true}})
	if err != nil {
		t.Fatal(err)
	}
	if page := measurement.Pages[0]; page.Failure != "" || page.Weight.Wire[HTML] == 0 {
		t.Errorf("overdue = %+v, want a measured load", page)
	}
}

// Some apps sign in without leaving the page: the form posts to its own
// URL and the answer is the signed-in page. The password field going is
// as good a sign as the URL changing.
func TestMeasureSignsInWhereTheFormStaysPut(t *testing.T) {
	chrome := os.Getenv("RASTRILLO_CHROME")
	if chrome == "" {
		t.Skip("RASTRILLO_CHROME is not set")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("s"); err == nil && c.Value == "1" {
			// A hash-routed app may hide its sign-in form rather than
			// remove it; a hidden password field is not a sign-in form.
			w.Write([]byte(`<!doctype html><html lang="en"><title>in</title><main><h1>Dashboard</h1>
<form hidden><input id="p" type="password"></form></main></html>`))
			return
		}
		w.Write([]byte(`<!doctype html><html lang="en"><title>out</title><main>
<form method="post" action="/"><label>User <input id="u" name="u"></label>
<label>Password <input id="p" name="p" type="password"></label>
<button id="go">Sign in</button></form></main></html>`))
	})
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1", Path: "/"})
		w.Write([]byte(`<!doctype html><html lang="en"><title>in</title><main><h1>Dashboard</h1></main></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	measurement, err := Measure(context.Background(),
		Options{Chrome: chrome, AxeJS: []byte("window.axe={run:()=>Promise.resolve({violations:[]})}")},
		Mapping{
			Pages: map[string]string{"dashboard": server.URL + "/"},
			SignIn: SignIn{URL: server.URL + "/", Username: "a", Password: "b",
				UsernameSelector: "#u", PasswordSelector: "#p", SubmitSelector: "#go"},
		},
		[]Role{{Name: "dashboard", SignedIn: true}})
	if err != nil {
		t.Fatal(err)
	}
	if page := measurement.Pages[0]; page.Failure != "" {
		t.Errorf("dashboard = %q, want a measured page", page.Failure)
	}
}
