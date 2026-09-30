package auth

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// renderSignin is the page body and title an app renders for st.
func renderSignin(t *testing.T, st SigninState) string {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(ui.Funcs()).ParseFS(ui.Templates(), "*.html"))
	var b strings.Builder
	data := map[string]any{"State": st, "Brand": map[string]any{"Name": "Harbour"}}
	for _, name := range []string{"signin-title", "signin"} {
		if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

// An address an admission wrapper refused with AnswerAsSent and one
// Begin sent a link to must render byte-identical pages, in both modes
// and with or without the Keymail one-tap's expect: the rendered page is
// the last place a refusal could show, and a difference there is a
// membership oracle the cookies and redirects were built not to be.
func TestAnAdmissionWrapperRendersTheSameSentPage(t *testing.T) {
	for _, screen := range []bool{false, true} {
		for _, expect := range []string{"", "keymail"} {
			t.Run("SigninScreen="+strconv.FormatBool(screen)+" expect="+expect, func(t *testing.T) {
				a, _ := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
				wireKeymail(a, newKeymailFake())
				form := url.Values{"address": {"ada@example.com"}}
				if expect != "" {
					form.Set("expect", expect)
				}
				ab, rb := newBrowser(), newBrowser()
				sent := ab.do(a.Begin, http.MethodPost, "/signin", form)
				refused := rb.do(a.AnswerAsSent, http.MethodPost, "/signin", form)
				pa := renderSignin(t, stateAt(a, ab, sent.Header().Get("Location")))
				pr := renderSignin(t, stateAt(a, rb, refused.Header().Get("Location")))
				if pa != pr {
					t.Fatalf("a sent link and a refusal render different pages:\n sent:    %s\n refused: %s", pa, pr)
				}
				if screen && !strings.Contains(pa, "<bdi>ada@example.com</bdi>") {
					t.Fatalf("screen on, the Sent page does not name the address:\n%s", pa)
				}
			})
		}
	}
}
