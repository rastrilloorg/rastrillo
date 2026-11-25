package csrf

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// FuzzSameOrigin feeds the three headers an attacker's page controls
// some of, and checks that a request is admitted only on evidence a
// browser would give for this origin: a same-origin or typed
// navigation, the exact Origin, or a Referer on this origin. A
// cross-site Sec-Fetch-Site must refuse whatever else came with it.
func FuzzSameOrigin(f *testing.F) {
	const origin = "https://app.example.com"
	f.Add("", origin, "")
	f.Add("", "", origin+"/form")
	f.Add("cross-site", origin, origin+"/form")
	f.Add("same-site", origin, "")
	f.Add("", "null", origin)
	f.Add("", "", "https://app.example.com@evil.example/")
	f.Add("", "", "https://app.example.com.evil.example/")
	f.Add("", "", "HTTPS://app.example.com/form")
	f.Add("", "", "//app.example.com/form")
	f.Add("", "", "https://app.example.com:443/")
	f.Add("", "", "https://u@app.example.com/form")
	f.Add("", "", "https://app.example.com@app.example.com/form")
	f.Fuzz(func(t *testing.T, site, originHeader, referer string) {
		request := httptest.NewRequest("POST", "http://app.test/form", nil)
		request.Header["Sec-Fetch-Site"] = []string{site}
		request.Header["Origin"] = []string{originHeader}
		request.Header["Referer"] = []string{referer}
		if !SameOrigin(request, origin) {
			return
		}
		switch {
		case site == "same-origin" || site == "none":
		case site != "":
			t.Fatalf("Sec-Fetch-Site %q admitted", site)
		case originHeader != "":
			if originHeader != origin {
				t.Fatalf("Origin %q admitted", originHeader)
			}
		case !onOrigin(referer, origin):
			t.Fatalf("Referer %q admitted", referer)
		}
	})
}

// onOrigin is what a browser means by a Referer on origin: its scheme,
// in either case, and its authority, which ends at the path, query or
// fragment and drops any userinfo up to the last "@". Userinfo names
// no origin, so "https://u@app.example.com/" is still on this one.
func onOrigin(referer, origin string) bool {
	wantScheme, wantHost, _ := strings.Cut(origin, "://")
	scheme, rest, ok := strings.Cut(referer, "://")
	if !ok || !strings.EqualFold(scheme, wantScheme) {
		return false
	}
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	return authority == wantHost
}
