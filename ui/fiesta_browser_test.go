//go:build browser

package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The date field's button opens the calendar through Fiesta's seam,
// window.fiestaCalendar, on a page the column shell renders through
// ui.Pages, so the scripts are the ones pageScripts chose: fiesta.js,
// calendar.js and datetime.js. Nothing names the old seam any more; a
// datetime.js that still looked for it would find no calendar and fall
// back to the browser's own picker, which draws no grid.
func TestTheCalendarOpensThroughFiestaCalendar(t *testing.T) {
	layout, ok := Layout("column")
	if !ok {
		t.Fatal("no column layout")
	}
	asset := func(name string) string {
		return "/" + strings.TrimPrefix(name, "static/")
	}
	page := renderPagesWith(t, layout,
		map[string]string{"date": shellContents["date"]},
		shellSetup{asset: asset})["date"]
	for _, script := range []string{"fiesta.js", "calendar.js", "datetime.js"} {
		if !strings.Contains(page, `src="/`+script+`"`) {
			t.Fatalf("the date page does not load %s", script)
		}
	}
	mux := http.NewServeMux()
	stylesheets(t, mux)
	vendoredScripts(t, mux)
	mux.HandleFunc("GET /{$}", func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, page)
	})
	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var seams string
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(chromedp.CSS(`[rst-dtp-pick]`)),
		harness.Into(&seams, chromedp.Evaluate[string](
			`typeof window.fiestaCalendar + " " +
			  typeof window.rastrilloCalendar`)))
	if seams != "function undefined" {
		t.Fatalf("fiestaCalendar and rastrilloCalendar are %q, want "+
			"\"function undefined\"", seams)
	}
	mustRun(t, ctx, chromedp.Click(chromedp.CSS(`[rst-dtp-pick]`)),
		chromedp.WaitVisible(chromedp.CSS(`[rst-cal] [role="gridcell"]`)))
}
