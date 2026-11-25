//go:build browser

package galleryrig

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// styledPage serves a page whose one stylesheet colours its body, and
// breaks the stylesheet's first broken responses: the body ends short
// of its Content-Length, which Chrome fails as
// net::ERR_CONTENT_LENGTH_MISMATCH and, unlike an empty answer, does
// not quietly ask for again. A stylesheet path other than /s.css is a
// 404, which Chrome does not report as a failure of its own.
func styledPage(sheet string, broken int32, served *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/page":
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(response, `<!doctype html>`+
				`<link rel="stylesheet" href="%s"><p id="ready">here`, sheet)
		case "/s.css":
			response.Header().Set("Content-Type", "text/css; charset=utf-8")
			if served.Add(1) <= broken {
				response.Header().Set("Content-Length", "100")
				response.WriteHeader(http.StatusOK)
				_, _ = response.Write([]byte("body {"))
				return
			}
			_, _ = response.Write([]byte("body { color: rgb(1, 2, 3) }"))
		default:
			http.NotFound(response, request)
		}
	})
}

func TestLoadStyledTriesADroppedStylesheetOnceMore(t *testing.T) {
	var served atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		return styledPage("/s.css", 1, &served)
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 30*time.Second)
	defer cancel()
	LoadStyled(t, rig, ctx, "a stylesheet broken once",
		chromedp.EmulateViewport(800, 600), rig.Origin+"/page",
		chromedp.WaitReady(chromedp.CSS(`#ready`)))
	color, err := chromedp.Run(ctx, chromedp.Evaluate[string](
		`getComputedStyle(document.body).color`))
	if err != nil {
		t.Fatal(err)
	}
	if color != "rgb(1, 2, 3)" || served.Load() != 2 {
		t.Errorf("after LoadStyled the body is %s, from %d answers for"+
			" the stylesheet; want rgb(1, 2, 3) from 2", color, served.Load())
	}
}

func TestLoadStyledNamesWhatChromeDropped(t *testing.T) {
	var served atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		return styledPage("/s.css", 2, &served)
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 30*time.Second)
	defer cancel()
	dropped, err := loadStyled(t, rig, ctx,
		chromedp.EmulateViewport(800, 600), rig.Origin+"/page",
		chromedp.WaitReady(chromedp.CSS(`#ready`)))
	want := rig.Origin + "/s.css: net::ERR_CONTENT_LENGTH_MISMATCH"
	if err != nil || len(dropped) != 1 || dropped[0] != want {
		t.Errorf("loadStyled = %q, %v; want [%q], nil", dropped, err, want)
	}
}

func TestLoadStyledRefusesAStylesheetChromeDidNotDrop(t *testing.T) {
	var served atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		return styledPage("/missing.css", 0, &served)
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 30*time.Second)
	defer cancel()
	dropped, err := loadStyled(t, rig, ctx,
		chromedp.EmulateViewport(800, 600), rig.Origin+"/page",
		chromedp.WaitReady(chromedp.CSS(`#ready`)))
	if err == nil || !strings.Contains(err.Error(), "answered 404") {
		t.Errorf("loadStyled = %q, %v; want an error, since a 404 is no"+
			" reason to load again", dropped, err)
	}
}

// One more try in all: a navigation Chrome drops uses it, so a
// stylesheet it drops on that second load fails, naming both, with no
// third load.
func TestLoadStyledTriesOnceForANavigationAndAStylesheet(t *testing.T) {
	var pages, sheets atomic.Int32
	styled := styledPage("/s.css", 1<<30, &sheets)
	rig := harness.New(t, func(string) http.Handler {
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			if request.URL.Path == "/page" && pages.Add(1) == 1 {
				response.WriteHeader(http.StatusNoContent)
				return
			}
			styled.ServeHTTP(response, request)
		})
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 30*time.Second)
	defer cancel()
	err := loadStyledErr(t, rig, ctx, chromedp.EmulateViewport(800, 600),
		rig.Origin+"/page", chromedp.WaitReady(chromedp.CSS(`#ready`)))
	if err == nil || !strings.Contains(err.Error(), "dropped a stylesheet") ||
		!strings.Contains(err.Error(), "after Chrome dropped the navigation") {
		t.Errorf("loadStyledErr = %v; want the stylesheet named, after the"+
			" navigation", err)
	}
	if pages.Load() != 2 || sheets.Load() != 1 {
		t.Errorf("/page asked for %d times and /s.css %d; want 2 and 1",
			pages.Load(), sheets.Load())
	}
}
