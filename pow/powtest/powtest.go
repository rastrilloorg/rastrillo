// Package powtest fills a rendered pow challenge the way the shipped
// browser module would, for tests that drive a protected form over
// HTTP without a browser.
//
// It contains a solver. That is acceptable here and nowhere else: a
// hashcash solver is ten lines for anyone who wants one, so the risk
// was never secrecy; it was an app solving its own challenges in
// production, and an import path ending in powtest makes that obvious
// in review.
package powtest

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"testing"

	"amadan.net/rastrillo/rastrillo/pow"
)

var hiddenField = regexp.MustCompile(`<input type="hidden" name="(pow_[a-z]+)" value="([^"]*)"`)

// Fill copies the first challenge in page into form and solves it
// unbound. The honeypot is left empty, as a person leaves it.
func Fill(t testing.TB, page []byte, form url.Values) url.Values {
	t.Helper()
	return FillBound(t, page, form, "")
}

// FillBound is Fill for a bound form: binding is the value the work is
// tied to, exactly as the [data-pow-binding] input would hold it.
func FillBound(t testing.TB, page []byte, form url.Values, binding string) url.Values {
	t.Helper()
	out := url.Values{}
	for k, v := range form {
		out[k] = append([]string(nil), v...)
	}
	seen := map[string]bool{}
	for _, m := range hiddenField.FindAllSubmatch(page, -1) {
		name := string(m[1])
		if seen[name] {
			break // the second form on the page starts here
		}
		seen[name] = true
		out.Set(name, html.UnescapeString(string(m[2])))
	}
	if !seen["pow_nonce"] {
		t.Fatalf("powtest: no pow challenge in the page")
	}
	bits, _ := strconv.Atoi(out.Get("pow_difficulty"))
	if bits > 0 {
		nonce := out.Get("pow_nonce")
		for i := 0; ; i++ {
			c := strconv.Itoa(i)
			if pow.Verify(nonce, binding, c, bits) {
				out.Set("pow_counter", c)
				break
			}
			if i > 1<<26 {
				t.Fatalf("powtest: no solution at %d bits", bits)
			}
		}
	}
	return out
}
