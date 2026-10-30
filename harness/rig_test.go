//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The origin chicken-and-egg, resolved: build gets the localhost
// origin before the server exists, the page renders it, and the
// browser reads it back off the page it navigated — one loop through
// listener, handler and Chromium proving the boot order.
func TestNewHandsBuildTheOriginBeforeServing(t *testing.T) {
	var gotOrigin string
	r := New(t, func(origin string) http.Handler {
		gotOrigin = origin
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>rig</title></head><body><p id="origin">%s</p></body></html>`, origin)
		})
		return mux
	})
	if !strings.HasPrefix(r.Origin, "http://localhost:") {
		t.Fatalf("rig origin %q is not a localhost origin — an IP is not a WebAuthn RP ID", r.Origin)
	}
	if gotOrigin != r.Origin {
		t.Fatalf("build was handed %q, the rig navigates %q", gotOrigin, r.Origin)
	}
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible(chromedp.CSS("#origin")),
	)
	onPage := Read(r, chromedp.Text(chromedp.CSS("#origin")))
	if onPage != r.Origin {
		t.Fatalf("page shows %q, want %q", onPage, r.Origin)
	}
}

// WithCoarsePointer is the only way a drive gets a phone's pointer:
// CDP's touch emulation leaves (pointer: coarse) false in this engine.
// The default rig is the control — if it already reported coarse, the
// option's true would prove nothing about the option.
func TestWithCoarsePointerMakesThePrimaryPointerCoarse(t *testing.T) {
	build := func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>pointer</title></head><body><p>pointer</p></body></html>`)
		})
		return mux
	}
	read := func(r *Rig) (coarse, hover bool) {
		r.Run(chromedp.Navigate(r.Origin + "/"))
		coarse = Evaluate[bool](r, `matchMedia("(pointer: coarse)").matches`)
		hover = Evaluate[bool](r, `matchMedia("(hover: hover)").matches`)
		return coarse, hover
	}
	if coarse, _ := read(New(t, build)); coarse {
		t.Fatal("CONTROL: a default rig already reports a coarse pointer, so the option's reading below proves nothing")
	}
	coarse, hover := read(New(t, build, WithCoarsePointer()))
	if !coarse {
		t.Error("WithCoarsePointer: (pointer: coarse) is false; every touch leg would measure desktop density")
	}
	if hover {
		t.Error("WithCoarsePointer: (hover: hover) is true; a phone's primary pointer cannot hover")
	}
}

// Into reads the page between two steps of one script: the count it
// keeps is the one before the second click, not after it.
func TestIntoKeepsAValueReadBetweenSteps(t *testing.T) {
	page := `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<title>count</title></head><body><button id="add" ` +
		`onclick="this.dataset.n = +(this.dataset.n ?? 0) + 1">` +
		`add</button></body></html>`
	rig := New(t, servePage(page))
	count := `+(document.querySelector("#add").dataset.n ?? 0)`
	var between int
	rig.Run(
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.Click(chromedp.CSS("#add")),
		Into(&between, chromedp.Evaluate[int](count)),
		chromedp.Click(chromedp.CSS("#add")),
	)
	if between != 1 {
		t.Errorf("Into kept %d, want 1, the count between the clicks", between)
	}
	if after := Evaluate[int](rig, count); after != 2 {
		t.Errorf("after both clicks the count is %d, want 2", after)
	}
}

// servePage serves one page at the root.
func servePage(page string) func(origin string) http.Handler {
	return func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}",
			func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type",
					"text/html; charset=utf-8")
				fmt.Fprint(response, page)
			})
		return mux
	}
}

// plainPage is a page that does nothing on its own.
const plainPage = `<!doctype html><html lang="en"><head>` +
	`<meta charset="utf-8"><title>plain</title></head>` +
	`<body><p>plain</p></body></html>`
