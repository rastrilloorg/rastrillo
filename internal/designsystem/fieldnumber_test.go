package designsystem

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/form"
)

// A field-number sample shows the error its own value produces, as
// field-money's do (fieldmoney_test.go): a gallery that showed a
// refusal the field never gives would teach the wrong field.
func TestFieldNumberSamplesShowTheirValuesOwnError(t *testing.T) {
	english := rastrillo.BaseCatalog()
	errored := 0
	for _, state := range fieldNumberDoc().States {
		data := state.Data.(map[string]any)
		text := func(key string) string {
			value, _ := data[key].(string)
			return value
		}
		values := url.Values{"P": {text("Value")}}
		parsed := form.Parse(&http.Request{Method: http.MethodPost,
			Form: values, PostForm: values},
			form.Field{Name: "P", Kind: form.Number, Locale: text("Locale")})
		want := ""
		if key, ok := parsed.Errors()["P"]; ok {
			want = english[key]
			for name, arg := range parsed.ErrorArgs()["P"] {
				want = strings.ReplaceAll(want, "{"+name+"}", fmt.Sprint(arg))
			}
			errored++
		}
		if got := text("Error"); got != want {
			t.Errorf("field-number %q: value %q shows %q, the field says %q",
				state.State, text("Value"), got, want)
		}
	}
	if errored == 0 {
		t.Fatal("no field-number sample shows an error")
	}
}
