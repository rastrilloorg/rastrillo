//go:build browser

package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/harness"
)

// With no JavaScript, a real browser submits the text and the three
// hidden inputs exactly as rendered, the ISO code is on the page beside
// the input (spec §12 item 15), and the server reads the text in the
// rendering locale: an English page's "US$ 1,284.5" is 128450 cents.
func TestFieldMoneySubmitsWithNoScript(t *testing.T) {
	got := make(chan string, 1)
	rig := harness.New(t, func(string) http.Handler {
		mux := http.NewServeMux()
		tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
		tmpl = template.Must(tmpl.New("page").Parse(`<!doctype html><html lang="en"><body><form rst-form method="post" action="/save">{{template "field-money" dict "Name" "Price" "Label" "Price" "Value" "US$ 1,284.5" "Locale" "en" "Currency" "USD"}}<button id="save" type="submit">Save</button></form></body></html>`))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { tmpl.ExecuteTemplate(w, "page", nil) })
		mux.HandleFunc("POST /save", func(w http.ResponseWriter, r *http.Request) {
			p := form.Parse(r, form.Field{Name: "Price", Kind: form.Money, Currency: "USD", LocaleEcho: true, Required: true})
			m, ok := p.Minor("Price")
			got <- fmt.Sprintf("%d %v %v", m, ok, p.Errors())
			w.Write([]byte("ok"))
		})
		return mux
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var label string
	mustRun(t, ctx, emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#save", chromedp.ByQuery),
		chromedp.Text("label[for='Price']", &label, chromedp.ByQuery),
		chromedp.Click("#save", chromedp.ByQuery))
	if !strings.Contains(label, "USD") {
		t.Errorf("the ISO code is not shown with the input: label %q", label)
	}
	select {
	case s := <-got:
		if s != "128450 true map[]" {
			t.Errorf("server read %q, want 128450 cents with no errors: the default reading normalises US$ 1,284.5", s)
		}
	case <-ctx.Done():
		t.Fatal("the form was never submitted")
	}
}
