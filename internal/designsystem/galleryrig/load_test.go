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

// A 204 is how a test makes Chrome abort a navigation on purpose:
// Page.navigate answers it with net::ERR_ABORTED, the error the fleet's
// runners give now and then under load (rastrillo#233).
func abortFirst(page string, served *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == "/page" && served.Add(1) == 1 {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(page))
	})
}

func TestLoadTriesAnAbortedNavigationOnceMore(t *testing.T) {
	var served atomic.Int32
	rig := harness.New(t, func(string) http.Handler {
		return abortFirst(`<!doctype html><p id="ready">here</p>`, &served)
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 30*time.Second)
	defer cancel()
	Load(t, ctx, "the page", 800, 600, rig.Origin+"/page",
		chromedp.WaitReady(chromedp.CSS(`#ready`)))
	if got := served.Load(); got != 2 {
		t.Fatalf("/page was asked for %d times, want 2: one aborted, one"+
			" loaded", got)
	}
}

func TestLoadFailsWhenThePageNavigatesItself(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler {
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			switch request.URL.Path {
			case "/start":
				_, _ = response.Write(
					[]byte(`<!doctype html><p id="ready">start</p>`))
			case "/page":
				// The page's own script asks for a navigation before the
				// test's has finished: a bug Load must not paper over.
				_, _ = response.Write([]byte(`<!doctype html>
					<script>location.replace("/gone")</script>
					<img src="/slow">`))
			case "/gone":
				response.WriteHeader(http.StatusNoContent)
			case "/slow":
				time.Sleep(2 * time.Second)
			}
		})
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 8*time.Second)
	defer cancel()
	err := chromedp.Do(ctx, chromedp.Navigate(rig.Origin+"/start"))
	if err != nil {
		t.Fatal(err)
	}
	failure := loadErr(ctx, 800, 600, rig.Origin+"/page",
		chromedp.WaitReady(chromedp.CSS(`#never`)))
	if failure == nil || !strings.Contains(failure.Error(), "/gone") {
		t.Fatalf("want a failure naming the page's own navigation to"+
			" /gone, got %v", failure)
	}
}

func TestOnlyAnAbortChromeMadeOnItsOwnIsTriedAgain(t *testing.T) {
	dropped := &chromedp.LoadError{ErrorText: aborted}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"an abort", dropped, true},
		{"an abort the page caused",
			fmt.Errorf("%w (the page asked for a navigation of its own"+
				" to /gone)", pageNavigated{dropped}), false},
		{"another load error",
			&chromedp.LoadError{ErrorText: "net::ERR_CONNECTION_REFUSED"},
			false},
		{"a timeout", context.DeadlineExceeded, false},
		{"none", nil, false},
	}
	for _, example := range cases {
		if got := worthAnotherTry(example.err); got != example.want {
			t.Errorf("%s: worthAnotherTry is %v, want %v", example.name, got,
				example.want)
		}
	}
}
