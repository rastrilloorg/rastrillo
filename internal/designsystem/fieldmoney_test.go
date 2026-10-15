package designsystem

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/form"
	// The samples are en fields. Without the en pack the parse below
	// would read them in the fallback's conventions and prove nothing
	// about what an English page does with the text.
	_ "amadan.net/rastrillo/rastrillo/locales/en"
)

// A field-money sample shows the error its own value produces in the
// default (normalising) field, and no error where the value is
// accepted. The first draft showed "Use a point for decimals" beside
// "12,50", which an en field reads as 12.50: the gallery taught the
// opposite of what the field does, and nothing failed.
func TestFieldMoneySamplesShowTheirValuesOwnError(t *testing.T) {
	en := rastrillo.BaseCatalog()
	var checked, errored int
	for _, fam := range families() {
		for _, doc := range fam.Partials {
			if doc.Name != "field-money" {
				continue
			}
			for _, s := range doc.States {
				d, ok := s.Data.(map[string]any)
				if !ok {
					t.Fatalf("field-money %q: Data is %T, want a map this test can read", s.State, s.Data)
				}
				str := func(k string) string { v, _ := d[k].(string); return v }
				v := url.Values{"P": {str("Value")}}
				p := form.Parse(&http.Request{Method: http.MethodPost, Form: v, PostForm: v},
					form.Field{Name: "P", Kind: form.Money, Currency: str("Currency"), Locale: str("Locale")})
				want := ""
				if key, ok := p.Errors()["P"]; ok {
					want = en[key]
					for k, arg := range p.ErrorArgs()["P"] {
						want = strings.ReplaceAll(want, "{"+k+"}", fmt.Sprint(arg))
					}
					errored++
				}
				if got := str("Error"); got != want {
					t.Errorf("field-money %q: value %q shows error %q, but the field says %q", s.State, str("Value"), got, want)
				}
				checked++
			}
		}
	}
	if checked == 0 || errored == 0 {
		t.Fatalf("checked %d field-money samples, %d with an error; the gallery lost the samples this test is about", checked, errored)
	}
}
