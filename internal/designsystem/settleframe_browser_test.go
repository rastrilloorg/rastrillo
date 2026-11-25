//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// A preview frame whose document Chrome fails once is loaded once more,
// and settles. The first answer ends short of its Content-Length, which
// Chrome fails as net::ERR_CONTENT_LENGTH_MISMATCH and leaves the frame
// on an error page that never settles.
func TestSettleFrameLoadsADroppedFrameOnceMore(t *testing.T) {
	var served atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			switch request.URL.Path {
			case "/page":
				_, _ = response.Write([]byte(`<!doctype html>` +
					`<iframe id="sample" src="/frame"></iframe>`))
			case "/frame":
				if served.Add(1) == 1 {
					response.Header().Set("Content-Length", "500")
					response.WriteHeader(http.StatusOK)
					_, _ = response.Write([]byte("<!doctype html><title>"))
					return
				}
				_, _ = response.Write([]byte(`<!doctype html>` +
					`<title>sample</title><p>here`))
			default:
				http.NotFound(response, request)
			}
		})
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	failed, stop := watchFailures(rig, ctx)
	defer stop()
	if err := chromedp.Do(ctx, chromedp.Navigate(rig.Origin+"/page"),
		chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	title := settleFrame(t, ctx, failed,
		previewFrame{Sel: "#sample", Of: "the sample"})
	if title != `"sample"` || served.Load() != 2 {
		t.Errorf("settled on %s after %d answers; want \"sample\" after 2",
			title, served.Load())
	}
}

// A preview frame that does not settle because the tree refused one of
// its requests fails, naming the status, and is not loaded again:
// Chrome reports a 404 stylesheet as net::ERR_ABORTED, the same text
// as a drop, so only the status tells the two apart.
func TestSettleFrameDoesNotReloadARefusedStylesheet(t *testing.T) {
	var served atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			switch request.URL.Path {
			case "/page":
				_, _ = response.Write([]byte(`<!doctype html>` +
					`<iframe id="sample" src="/frame"></iframe>`))
			case "/frame":
				served.Add(1)
				_, _ = response.Write([]byte(`<!doctype html>` +
					`<link rel="stylesheet" href="/missing.css">` +
					`<title>sample</title><p>here`))
			default:
				http.NotFound(response, request)
			}
		})
	})
	rig.Allow(http.MethodGet, "/missing.css", http.StatusNotFound)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	failed, stop := watchFailures(rig, ctx)
	defer stop()
	if err := chromedp.Do(ctx, chromedp.Navigate(rig.Origin+"/page"),
		chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	_, err := settleFrameErr(t, ctx, failed,
		previewFrame{Sel: "#sample", Of: "the sample"})
	if err == nil || !strings.Contains(err.Error(), "/missing.css: HTTP 404") ||
		served.Load() != 1 {
		t.Errorf("settleFrameErr = %v after %d answers for the frame; want"+
			" the 404 named after 1", err, served.Load())
	}
}
