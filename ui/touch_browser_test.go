//go:build browser

package ui

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// sizingDoc is a page carrying the stylesheet, a theme, the shim and
// busy.js (light dismiss and the busy spinner, which the row-menu legs
// drive), and the scripts that enhance controls (select.js and
// datetime.js change what a field renders, calendar.js draws the grid
// the calendar legs measure). No CSP: this is a test page.
func sizingDoc(title, body string) string {
	return `<!doctype html><html lang="en" dir="ltr"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width"><title>` + title + `</title>` +
		`<link rel="stylesheet" href="/tokens.css"><link rel="stylesheet" href="/theme.css">` +
		`<script defer src="/rastrillo.js"></script><script defer src="/busy.js"></script><script defer src="/select.js"></script>` +
		`<script defer src="/calendar.js"></script><script defer src="/datetime.js"></script>` +
		`</head><body>` + body + `</body></html>`
}

// sizingMux serves pages by path ("/" is the root only) and the assets
// sizingDoc links.
func sizingMux(t *testing.T, pages map[string]string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	stylesheets(t, mux)
	for name, body := range map[string][]byte{
		"rastrillo.js": ShimJS(), "busy.js": BusyJS(), "select.js": SelectJS(), "datetime.js": DatetimeJS(), "calendar.js": CalendarJS(),
	} {
		body := body
		mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(body)
		})
	}
	for path, html := range pages {
		html, pattern := html, "GET "+path
		if path == "/" {
			pattern = "GET /{$}"
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		})
	}
	return mux
}

// sizingRig boots one browser, touch or mouse. Two rigs rather than one
// switched: the pointer is a launch flag (harness.WithCoarsePointer).
func sizingRig(t *testing.T, coarse bool, pages map[string]string, opts ...harness.Option) *harness.Rig {
	t.Helper()
	if coarse {
		opts = append(opts, harness.WithCoarsePointer())
	}
	return harness.New(t, func(string) http.Handler { return sizingMux(t, pages) }, opts...)
}

// requirePointer is §10's control: a touch leg that is really running
// on a fine pointer would pass on desktop sizes, so it fails first.
func requirePointer(t *testing.T, ctx context.Context, coarse bool) {
	t.Helper()
	var got bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`matchMedia("(pointer: coarse)").matches`, &got)); err != nil {
		t.Fatalf("reading the pointer: %v", err)
	}
	if got != coarse {
		t.Fatalf("(pointer: coarse) is %v, this leg needs %v; the rig is not the device this leg claims to measure", got, coarse)
	}
}

// tokensJS reads the four type tokens as pixels, the body size, and the
// two halves of the query, so a leg can say which half it exercised.
const tokensJS = `(() => {
  const probe = v => { const s = document.createElement("span"); s.style.fontSize = "var(" + v + ")"; document.body.appendChild(s); const px = parseFloat(getComputedStyle(s).fontSize); s.remove(); return px; };
  return JSON.stringify({Lg: probe("--rst-fs-lg"), Base: probe("--rst-fs-base"), Sm: probe("--rst-fs-sm"), Xs: probe("--rst-fs-xs"),
    Body: parseFloat(getComputedStyle(document.body).fontSize), Coarse: matchMedia("(pointer: coarse)").matches, Width: innerWidth});
})()`

type typeReading struct {
	Lg, Base, Sm, Xs, Body float64
	Coarse                 bool
	Width                  int
}

// fontsJS reads every text-entry control's computed size, visible or
// not — a size is computed for a field inside a closed menu too, and a
// field that zooms when its menu opens is the bug.
const fontsJS = `(() => {
  const TEXTY = 'input:not([type=checkbox]):not([type=radio]):not([type=hidden]):not([type=range]):not([type=submit]):not([type=button]):not([type=color]):not([type=file]), select, textarea';
  const out = [];
  document.querySelectorAll(TEXTY).forEach(el => out.push({
    Name: el.id || el.name || el.getAttribute("aria-label") || el.outerHTML.slice(0, 80),
    Px: parseFloat(getComputedStyle(el).fontSize)}));
  return JSON.stringify(out);
})()`

type fontReading struct {
	Name string
	Px   float64
}
