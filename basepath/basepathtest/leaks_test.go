package basepathtest_test

import (
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath/basepathtest"
)

func TestLeaks(t *testing.T) {
	body := []byte(`<a href="/acme">a</a><a href="/acme/x">b</a><a href="/acme?q">c</a><a href="#main">d</a>
<link href="/static/t.css"><form action="/_theme"></form><img src='/logo.png'>
<a href="https://example.com/">e</a><a href="//cdn.example/x">f</a><div data-poll="/jobs/1/fragment"></div>
<button formaction="/acme/x/delete"></button><a href="/acmex">g</a>`)
	want := []string{`href="/static/t.css"`, `action="/_theme"`, `src='/logo.png'`, `data-poll="/jobs/1/fragment"`, `href="/acmex"`}
	if got := basepathtest.Leaks(body, "/acme"); !reflect.DeepEqual(got, want) {
		t.Errorf("Leaks(body, /acme) =\n\t%q\nwant\n\t%q", got, want)
	}
	if got := basepathtest.Leaks(body, ""); got != nil {
		t.Errorf("Leaks(body, \"\") = %q, want nil", got)
	}
}
