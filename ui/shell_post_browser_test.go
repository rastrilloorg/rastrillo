//go:build browser

package ui

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

func TestShellPostRedirectDoesNotReportUncaughtErrors(t *testing.T) {
	for _, destination := range []string{
		"stage", "column", "topbar", "sidebar", "console",
	} {
		t.Run(destination, func(t *testing.T) {
			shellPostRedirect(t, destination, false)
		})
	}
	t.Run("slow script", func(t *testing.T) {
		shellPostRedirect(t, "stage", true)
	})
}

func shellPostRedirect(
	t *testing.T, destination string, slowScript bool,
) {
	t.Helper()
	source, _ := Layout("sidebar")
	target, _ := Layout(destination)
	account := `{{define "account"}}` +
		`<form method="post" action="/signout">` +
		`<button id="signout">Sign out</button></form>{{end}}`
	pages := map[string]string{
		"/": shellLayoutPage(t, source, "ltr",
			`{{define "view"}}index{{end}}`, account),
		"/signin": shellLayoutPage(t, target, "ltr"),
	}
	var posts, delayed atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		mux := shellAssets(t, pages)
		mux.HandleFunc("POST /signout", func(
			response http.ResponseWriter, request *http.Request,
		) {
			posts.Add(1)
			http.Redirect(response, request, "/signin", http.StatusSeeOther)
		})
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			if request.URL.Path == "/theme.css" {
				theme, _ := ThemeCSS("day")
				response.Header().Set("Content-Type", "text/css")
				response.Write(theme)
				return
			}
			if slowScript && request.URL.Path == "/transition.js" &&
				strings.HasSuffix(request.Referer(), "/signin") {
				delayed.Add(1)
				time.Sleep(200 * time.Millisecond)
			}
			mux.ServeHTTP(response, request)
		})
	})
	rig.Run(chromedp.EmulateViewport(390, 844),
		send(emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
			Features: []*emulation.MediaFeature{{
				Name: "prefers-reduced-motion", Value: "no-preference",
			}},
		}))
	visit(t, rig.Context(), rig.Origin+"/")
	revealed(t, rig.Context())
	rig.Run(chromedp.Click(chromedp.CSS(`[rst-shell-profile] summary`)))
	rig.Run(chromedp.Click(chromedp.CSS("#signout")))
	settleUntil(t, rig.Context(), loaded(`location.pathname === "/signin"`))
	revealed(t, rig.Context())
	settleUntil(t, rig.Context(),
		`!document.documentElement.matches(":active-view-transition")`)
	if posts.Load() != 1 {
		t.Fatalf("signout received %d POSTs, want one", posts.Load())
	}
	if slowScript && delayed.Load() != 1 {
		t.Fatalf("delayed %d destination script loads, want one",
			delayed.Load())
	}
	rig.Screen("body", "POST redirect to "+destination)
}
