//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The Money sample is server-rendered text (spec §9, decision 2:
// formatting happens on the server, and the browser's Intl is at most
// a progressive enhancement): with no JavaScript, the Spanish page
// still shows the amounts written the Spanish way, and the Form
// page's field-money control is a plain input with its hidden locale
// and currency, same as prefs_browser_test.go and index_browser_test.go
// already hold the rest of the gallery to.
//
// Each sample's live widget sits in its own iframe (page.go's ds-view),
// a separate document querySelector cannot reach from the page that
// frames it. Both samples' frames are also "standalone preview"
// documents in their own right (previewTitle), so this drives them
// directly at their own URL instead of reaching across the frame
// boundary with contentDocument, the way the header-rule sweep does
// for a check that has to walk every frame on the page at once.
func TestMoneySamplesWithNoScript(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	off := noScripts(t, ctx)

	moneyID := anchorID("format", "money")
	var formats string
	if err := chromedp.Run(off,
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "es", "formats/"+moneyID+"-0.html")),
		chromedp.Text("dl", &formats, chromedp.ByQuery)); err != nil {
		t.Fatalf("es money sample, scripts off: %v", err)
	}
	requireScriptsOff(t, off, "es money sample, scripts off")
	if !strings.Contains(formats, "1284,50 €") {
		t.Errorf("Spanish money sample with no script: %q", formats)
	}

	fieldMoneyID := anchorID("partial", "field-money")
	var form string
	if err := chromedp.Run(off,
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "es", "form/"+fieldMoneyID+"-0.html")),
		chromedp.OuterHTML("[name='fm_bare']", &form, chromedp.ByQuery)); err != nil {
		t.Fatalf("es field-money bare sample, scripts off: %v", err)
	}
	requireScriptsOff(t, off, "es field-money bare sample, scripts off")
	if !strings.Contains(form, `inputmode="decimal"`) {
		t.Errorf("field-money with no script: %s", form)
	}
}
